// Package filekeys supplies versioned AES keys from a private local directory.
// The directory and its monotonically advancing usage file must not be rolled
// back with a credential database. A controlled host can still read its keys.
package filekeys

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"lerna/credentials"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

const MaxUses = 1 << 20
const maxKeys = 8
const maxFile = 8192

type key struct {
	Material []byte
	Used     uint64
}
type ring struct {
	Format    uint32
	Limit     uint64
	Active    string
	Keys      map[string]key
	Prepared  map[string]string
	Activated map[string]bool
}
type Source struct {
	mu    chan struct{}
	root  *os.Root
	limit uint64
}

func owned(f *os.File) bool {
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return false
	}
	v, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return false
	}
	return v.Uid == uint32(os.Geteuid()) && v.Nlink == 1
}
func Open(dir string, limit uint64) (*Source, error) {
	if limit == 0 || limit > MaxUses {
		return nil, credentials.Invalid
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return nil, credentials.KeyUnavailable
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, credentials.KeyUnavailable
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, credentials.KeyUnavailable
	}
	return &Source{mu: make(chan struct{}, 1), root: root, limit: limit}, nil
}
func (s *Source) Close() error { s.mu <- struct{}{}; defer func() { <-s.mu }(); return s.root.Close() }
func valid(r ring, limit uint64) bool {
	if r.Format != 1 || r.Limit != limit || len(r.Keys) == 0 || len(r.Keys) > maxKeys {
		return false
	}
	for op, activated := range r.Activated {
		if _, ok := r.Prepared[op]; !ok || !activated {
			return false
		}
	}
	if len(r.Prepared) > 32 {
		return false
	}
	for op, id := range r.Prepared {
		if !validOperation(op) || len(id) != 64 {
			return false
		}
		if _, e := hex.DecodeString(id); e != nil {
			return false
		}
	}
	if _, ok := r.Keys[r.Active]; !ok {
		return false
	}
	for id, k := range r.Keys {
		sum := sha256.Sum256(k.Material)
		if len(k.Material) != 32 || id != hex.EncodeToString(sum[:]) || k.Used > limit {
			return false
		}
	}
	return true
}
func clearRing(r ring) {
	for _, k := range r.Keys {
		clear(k.Material)
	}
}
func (s *Source) transaction(ctx context.Context, create bool, fn func(*ring) error) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	select {
	case s.mu <- struct{}{}:
		defer func() { <-s.mu }()
	case <-ctx.Done():
		return credentials.KeyUnavailable
	}
	lock, e := s.root.OpenFile("lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return credentials.KeyUnavailable
	}
	defer lock.Close()
	if !owned(lock) {
		return credentials.KeyUnavailable
	}
	for {
		e = unix.Flock(int(lock.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			break
		}
		if e != unix.EAGAIN && e != unix.EWOULDBLOCK {
			return credentials.KeyUnavailable
		}
		select {
		case <-ctx.Done():
			return credentials.KeyUnavailable
		case <-time.After(time.Millisecond * 5):
		}
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	r := ring{Format: 1, Limit: s.limit, Keys: map[string]key{}}
	f, e := s.root.OpenFile("keys.json", os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if e != nil {
		if !create || !os.IsNotExist(e) {
			return credentials.KeyUnavailable
		}
	} else {
		if !owned(f) {
			f.Close()
			return credentials.KeyUnavailable
		}
		raw, e := io.ReadAll(io.LimitReader(f, maxFile+1))
		f.Close()
		defer clear(raw)
		if e != nil || len(raw) > maxFile || json.Unmarshal(raw, &r) != nil || !valid(r, s.limit) {
			return credentials.KeyUnavailable
		}
	}
	defer clearRing(r)
	if e = fn(&r); e != nil {
		return e
	}
	if !valid(r, s.limit) {
		return credentials.KeyUnavailable
	}
	// Every operation persists the latest ring before releasing any result. A
	// crash before this return may burn a nonce; callers never reclaim it.
	raw, e := json.Marshal(r)
	defer clear(raw)
	if e != nil || len(raw) > maxFile {
		return credentials.KeyUnavailable
	}
	// An interrupted prior write may leave this private staging file. It is not
	// authoritative and can be discarded while holding the separate stable lock.
	if e = s.root.Remove("next"); e != nil && !os.IsNotExist(e) {
		return credentials.KeyUnavailable
	}
	f, e = s.root.OpenFile("next", os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return credentials.KeyUnavailable
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	closeErr := f.Close()
	if e != nil || closeErr != nil {
		return credentials.KeyUnavailable
	}
	if e = s.root.Rename("next", "keys.json"); e != nil {
		return credentials.KeyUnavailable
	}
	d, e := s.root.Open(".")
	if e != nil {
		return credentials.KeyUnavailable
	}
	e = d.Sync()
	d.Close()
	if e != nil {
		return credentials.KeyUnavailable
	}
	return nil
}

// Generate activates a fresh random key and retains all previous key versions.
func (s *Source) Generate(ctx context.Context) (string, error) {
	var id string
	e := s.transaction(ctx, true, func(r *ring) error {
		if len(r.Keys) >= maxKeys {
			return credentials.Exhausted
		}
		material := make([]byte, 32)
		if _, e := rand.Read(material); e != nil {
			return credentials.KeyUnavailable
		}
		sum := sha256.Sum256(material)
		id = hex.EncodeToString(sum[:])
		if _, ok := r.Keys[id]; ok {
			clear(material)
			return credentials.KeyUnavailable
		}
		r.Keys[id] = key{Material: material}
		r.Active = id
		return nil
	})
	if e != nil {
		return "", e
	}
	return id, nil
}
func (s *Source) Reserve(ctx context.Context) (string, []byte, []byte, error) {
	var id string
	var material, nonce []byte
	e := s.transaction(ctx, false, func(r *ring) error {
		id = r.Active
		k := r.Keys[id]
		if k.Used >= r.Limit {
			return credentials.Exhausted
		}
		k.Used++
		r.Keys[id] = k
		material = append([]byte(nil), k.Material...)
		nonce = make([]byte, 12)
		binary.BigEndian.PutUint64(nonce[4:], k.Used)
		return nil
	})
	if e != nil {
		clear(material)
		return "", nil, nil, e
	}
	return id, material, nonce, nil
}
func (s *Source) Read(ctx context.Context, id string) ([]byte, error) {
	var material []byte
	e := s.transaction(ctx, false, func(r *ring) error {
		k, ok := r.Keys[id]
		if !ok {
			return credentials.KeyUnavailable
		}
		material = append([]byte(nil), k.Material...)
		return nil
	})
	if e != nil {
		clear(material)
		return nil, e
	}
	return material, nil
}
