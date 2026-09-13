// Package executioncontent checks controlled JSON input and retains execution
// evidence under the input's source, location and retention restrictions.
package executioncontent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"sync"
	"time"
)

type Content interface {
	Call(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)
}
type Clock interface{ Now() (time.Time, error) }
type Adapter struct {
	content Content
	binding artifacts.Binding
	clock   Clock
	bounds  *readBounds
}

type readBounds struct {
	mu    sync.Mutex
	sizes map[string]uint64
}

func New(c Content, b artifacts.Binding, clock Clock) *Adapter {
	return &Adapter{content: c, binding: b, clock: clock, bounds: &readBounds{sizes: map[string]uint64{}}}
}

// RememberInput records only the length returned by an input write. It grants
// no read authority: Read checks the actual record and current permission for
// every chunk, rejecting incorrect hints instead of returning partial input.
func (a *Adapter) RememberInput(record *wire.ContentRecord) {
	if record.GetRef() == nil || record.GetState() != "available" || record.GetRef().GetNamespace() != a.binding.Namespace || record.GetSpec().GetMediaType() != "application/json" {
		return
	}
	size := record.GetSpec().GetSize()
	if size == 0 || size > 32768 {
		return
	}
	ref := answers.Reference(record.Ref)
	if _, err := answers.ParseReference(ref); err != nil {
		return
	}
	a.bounds.mu.Lock()
	defer a.bounds.mu.Unlock()
	if len(a.bounds.sizes) < 8 {
		a.bounds.sizes[ref] = size
	}
}

func (a *Adapter) metadata(ctx context.Context, token, ref string, c execution.Capability) (artifacts.Binding, *wire.ContentRecord, error) {
	b := a.binding
	b.Token = token
	b.Recipient = c.Location
	r, e := answers.ParseReference(ref)
	if e != nil {
		return b, nil, e
	}
	out, e := a.content.Call(ctx, b, &wire.ContentRequest{Method: "GET", Ref: r, Purpose: c.Purpose})
	if e != nil {
		return b, nil, e
	}
	m := out.GetRecord()
	if m.GetState() != "available" || m.GetSpec().GetResource() != c.Resource || m.GetSpec().GetPurpose() != c.Purpose || m.GetSpec().GetMediaType() != "application/json" || m.GetSpec().GetSize() > 32768 {
		return b, nil, &authorization.Error{Code: authorization.Denied}
	}
	return b, m, nil
}

// WithContent binds the same immutable references to another governed consumer.
// Only verified lengths are shared; every read checks current Content authority.
func (a *Adapter) WithContent(content Content) *Adapter {
	copy := *a
	copy.content = content
	return &copy
}
func (a *Adapter) Read(ctx context.Context, token, ref string, c execution.Capability) ([]byte, error) {
	b := a.binding
	b.Token, b.Recipient = token, c.Location
	id, err := answers.ParseReference(ref)
	if err != nil {
		return nil, err
	}
	a.bounds.mu.Lock()
	size, known := a.bounds.sizes[ref]
	a.bounds.mu.Unlock()
	if !known {
		_, m, err := a.metadata(ctx, token, ref, c)
		if err != nil {
			return nil, err
		}
		size = m.Spec.Size
	}
	data := []byte{}
	for offset := uint64(0); offset < size; {
		n := min(uint64(16384), size-offset)
		out, err := a.content.Call(ctx, b, &wire.ContentRequest{Method: "READ", Ref: id, Purpose: c.Purpose, Offset: offset, Limit: uint32(n)})
		if err != nil {
			return nil, err
		}
		m := out.GetRecord()
		spec := m.GetSpec()
		if m.GetState() != "available" || m.GetRef() == nil || answers.Reference(m.Ref) != ref || spec.GetResource() != c.Resource || spec.GetPurpose() != c.Purpose || spec.GetMediaType() != "application/json" || spec.GetSize() != size || uint64(len(out.GetData())) != n {
			return nil, &authorization.Error{Code: authorization.Denied}
		}
		data = append(data, out.Data...)
		offset += n
	}
	// Empty inputs must keep taking the metadata path; no READ could revalidate
	// their current availability and authority on a cached zero-length path.
	if size > 0 {
		a.bounds.mu.Lock()
		if len(a.bounds.sizes) < 8 {
			a.bounds.sizes[ref] = size
		}
		a.bounds.mu.Unlock()
	}
	return data, nil
}
func (a *Adapter) Save(ctx context.Context, token, op, ref string, c execution.Capability, output, evidence []byte) (string, error) {
	b, m, e := a.metadata(ctx, token, ref, c)
	if e != nil {
		return "", e
	}
	now, e := a.clock.Now()
	if e != nil {
		return "", e
	}
	body, e := json.Marshal(struct {
		Output   json.RawMessage `json:"output"`
		Evidence json.RawMessage `json:"evidence"`
	}{output, evidence})
	if e != nil {
		return "", &authorization.Error{Code: authorization.Invalid}
	}
	sum := sha256.Sum256(body)
	out, e := a.content.Call(ctx, b, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: body, Spec: &wire.ContentSpec{Kind: "artifact", Resource: c.Resource, Purpose: c.Purpose, Sources: m.Spec.Sources, AcquiredAt: now.Unix(), MediaType: "application/json", Size: uint64(len(body)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: min(m.Spec.RetainUntil, now.Add(5*time.Minute).Unix())}})
	if e != nil {
		return "", e
	}
	if out.GetRecord().GetState() != "available" {
		return "", &authorization.Error{Code: authorization.Unavailable}
	}
	return answers.Reference(out.Record.Ref), nil
}
