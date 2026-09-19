// Package localsource reads exact local files registered by a trusted
// host. Registration and replacement are not reachable from extraction input.
package localsource

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"golang.org/x/sys/unix"
	"google.golang.org/protobuf/proto"
	"io"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
	"lerna/memory"
	"os"
	"path/filepath"
	"sync"
)

// Entry fixes authenticated provenance, file digest and allowed scope outside
// the untrusted source body. It is host configuration, not a user request.
type Entry struct {
	// Encoding is selected by the trusted observation reader, never the body.
	Encoding                                string
	Ref                                     *wire.ContentSource
	Path, SHA256, Speaker, Method, Fragment string
	Restrictions                            extraction.Restrictions
}
type Source struct {
	clock   memory.Clock
	mu      sync.RWMutex
	entries map[key]Entry
}
type key struct {
	kind, name string
	revision   uint64
}

func New(entries []Entry, clock memory.Clock) (*Source, error) {
	if clock == nil {
		return nil, memory.Invalid
	}
	s := &Source{clock: clock}
	if err := s.Replace(entries); err != nil {
		return nil, err
	}
	return s, nil
}
func copyBounds(b extraction.Restrictions) extraction.Restrictions {
	b.Storage = append([]string(nil), b.Storage...)
	b.Processing = append([]string(nil), b.Processing...)
	b.Purposes = append([]string(nil), b.Purposes...)
	b.Recipients = append([]string(nil), b.Recipients...)
	return b
}
func (s *Source) Replace(entries []Entry) error {
	if len(entries) > 256 {
		return memory.Invalid
	}
	next := map[key]Entry{}
	for _, e := range entries {
		if e.Encoding != "" && e.Encoding != "text" && e.Encoding != "choice" {
			return memory.Invalid
		}
		digest, err := hex.DecodeString(e.SHA256)
		if err != nil || len(digest) != 32 || e.Ref == nil || e.Ref.Kind == "" || len(e.Ref.Kind) > 128 || e.Ref.Key == "" || len(e.Ref.Key) > 256 || e.Ref.Revision == 0 || !filepath.IsAbs(e.Path) || len(e.Path) > 4096 || e.Speaker == "" || len(e.Speaker) > 512 || e.Method == "" || len(e.Method) > 128 || e.Fragment == "" || len(e.Fragment) > 512 {
			return memory.Invalid
		}
		if _, err = extraction.IntersectRestrictions(1, []extraction.Restrictions{e.Restrictions}); err != nil {
			return memory.Invalid
		}
		k := key{e.Ref.Kind, e.Ref.Key, e.Ref.Revision}
		if _, exists := next[k]; exists {
			return memory.Invalid
		}
		e.Ref = proto.Clone(e.Ref).(*wire.ContentSource)
		e.Restrictions = copyBounds(e.Restrictions)
		e.SHA256 = hex.EncodeToString(digest)
		next[k] = e
	}
	s.mu.Lock()
	s.entries = next
	s.mu.Unlock()
	return nil
}
func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}
func subset(xs, allowed []string) bool {
	if len(xs) == 0 {
		return false
	}
	for _, x := range xs {
		if !contains(allowed, x) {
			return false
		}
	}
	return true
}

// readFile refuses special files before I/O; reads are bounded to 4096 bytes.
// A manifest digest mismatch means the exact registered revision is unavailable.
func readFile(ctx context.Context, e Entry) ([]byte, error) {
	if ctx.Err() != nil {
		return nil, memory.Unavailable
	}
	f, err := os.OpenFile(e.Path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return nil, memory.Denied
	}
	defer f.Close()
	var st unix.Stat_t
	if unix.Fstat(int(f.Fd()), &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Mode&0777 != 0600 || st.Uid != uint32(os.Getuid()) || st.Nlink != 1 || st.Size > 4096 {
		return nil, memory.Denied
	}
	raw, err := io.ReadAll(io.LimitReader(f, 4097))
	if err != nil || len(raw) > 4096 || ctx.Err() != nil {
		return nil, memory.Unavailable
	}
	sum := sha256.Sum256(raw)
	if hex.EncodeToString(sum[:]) != e.SHA256 {
		return nil, memory.Denied
	}
	return raw, nil
}
func (s *Source) Read(ctx context.Context, refs []*wire.ContentSource, location, purpose string) ([]extraction.Material, []extraction.Restrictions, error) {
	if len(refs) == 0 || len(refs) > 16 {
		return nil, nil, memory.Invalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now, err := s.clock.Now()
	if err != nil {
		return nil, nil, memory.Unavailable
	}
	var materials []extraction.Material
	var bounds []extraction.Restrictions
	for _, ref := range refs {
		if ref == nil {
			return nil, nil, memory.Invalid
		}
		e, ok := s.entries[key{ref.Kind, ref.Key, ref.Revision}]
		if !ok || e.Restrictions.RetainUntil <= now.Unix() || !contains(e.Restrictions.Processing, location) || !contains(e.Restrictions.Purposes, purpose) {
			return nil, nil, memory.Denied
		}
		raw, err := readFile(ctx, e)
		if err != nil {
			return nil, nil, err
		}
		fragment := e.Fragment
		material := extraction.Material{Source: &wire.MemorySource{Ref: proto.Clone(e.Ref).(*wire.ContentSource), Method: e.Method, Fragment: &fragment}, Speaker: e.Speaker}
		if e.Encoding == "choice" {
			value, err := jsonvalue.Decode(raw)
			object, ok := value.(map[string]any)
			if err != nil || !ok || len(object) != 3 {
				return nil, nil, memory.Denied
			}
			event, eventOK := object["event"].(string)
			task, taskOK := object["task"].(string)
			format, formatOK := object["format"].(string)
			if !eventOK || !taskOK || !formatOK || event == "" || len(event) > 256 || task == "" || len(task) > 256 || format == "" || len(format) > 256 {
				return nil, nil, memory.Denied
			}
			choice := extraction.Choice{Event: event, Task: task, Format: format}
			material.Choice = &choice
		} else {
			material.Text = string(raw)
		}
		materials = append(materials, material)
		bounds = append(bounds, copyBounds(e.Restrictions))
	}
	return materials, bounds, nil
}
func (s *Source) Validate(ctx context.Context, refs []*wire.ContentSource, b extraction.Restrictions) error {
	if len(refs) == 0 || len(refs) > 16 {
		return memory.Invalid
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	now, err := s.clock.Now()
	if err != nil {
		return memory.Unavailable
	}
	for _, ref := range refs {
		if ref == nil {
			return memory.Invalid
		}
		e, ok := s.entries[key{ref.Kind, ref.Key, ref.Revision}]
		if !ok || e.Restrictions.RetainUntil <= now.Unix() || b.RetainUntil > e.Restrictions.RetainUntil || b.RetainUntil <= 0 || !subset(b.Storage, e.Restrictions.Storage) || !subset(b.Processing, e.Restrictions.Processing) || !subset(b.Purposes, e.Restrictions.Purposes) || !subset(b.Recipients, e.Restrictions.Recipients) {
			return memory.Denied
		}
		if _, err := readFile(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
