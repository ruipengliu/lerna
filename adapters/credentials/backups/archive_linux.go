// Package backups stores private immutable ciphertext snapshots.
package backups

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"golang.org/x/sys/unix"
	"io"
	"lerna/credentials"
	"os"
	"syscall"
	"time"
)

const maxSnapshot = 1048576

type Archive struct {
	root *os.Root
	id   string
	mu   chan struct{}
}

func Open(dir, id string) (*Archive, error) {
	if len(id) == 0 || len(id) > 64 {
		return nil, credentials.Invalid
	}
	for _, c := range id {
		if c < 33 || c > 126 {
			return nil, credentials.Invalid
		}
	}
	st, e := os.Lstat(dir)
	if e != nil || !st.IsDir() || st.Mode().Perm() != 0700 {
		return nil, credentials.Unavailable
	}
	owner, ok := st.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, credentials.Unavailable
	}
	root, e := os.OpenRoot(dir)
	if e != nil {
		return nil, credentials.Unavailable
	}
	// Bind references to this physical archive directory, not just a reusable
	// configuration label. A copied directory requires explicit reconciliation.
	identity := sum([]byte(fmt.Sprintf("%s:%d:%d", id, owner.Dev, owner.Ino)))
	return &Archive{root: root, id: identity, mu: make(chan struct{}, 1)}, nil
}
func (a *Archive) ID() string   { return a.id }
func (a *Archive) Close() error { a.mu <- struct{}{}; defer func() { <-a.mu }(); return a.root.Close() }
func owned(f *os.File) bool {
	st, e := f.Stat()
	if e != nil || !st.Mode().IsRegular() || st.Mode().Perm() != 0600 {
		return false
	}
	v, ok := st.Sys().(*syscall.Stat_t)
	return ok && v.Uid == uint32(os.Geteuid()) && v.Nlink == 1
}
func validID(id string) bool {
	if len(id) != 64 {
		return false
	}
	_, e := hex.DecodeString(id)
	return e == nil
}
func sum(raw []byte) string { h := sha256.Sum256(raw); return hex.EncodeToString(h[:]) }
func (a *Archive) locked(ctx context.Context, fn func() error) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	select {
	case a.mu <- struct{}{}:
		defer func() { <-a.mu }()
	case <-ctx.Done():
		return credentials.Unavailable
	}
	f, e := a.root.OpenFile("lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return credentials.Unavailable
	}
	defer f.Close()
	if !owned(f) {
		return credentials.Unavailable
	}
	for {
		e = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if e == nil {
			break
		}
		if !errors.Is(e, unix.EAGAIN) && !errors.Is(e, unix.EWOULDBLOCK) {
			return credentials.Unavailable
		}
		select {
		case <-ctx.Done():
			return credentials.Unavailable
		case <-time.After(5 * time.Millisecond):
		}
	}
	defer unix.Flock(int(f.Fd()), unix.LOCK_UN)
	if ctx.Err() != nil {
		return credentials.Unavailable
	}
	return fn()
}
func (a *Archive) read(name string, limit int64) ([]byte, error) {
	f, e := a.root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if os.IsNotExist(e) {
		return nil, credentials.Missing
	}
	if e != nil {
		return nil, credentials.Unavailable
	}
	defer f.Close()
	if !owned(f) {
		return nil, credentials.Unavailable
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return nil, credentials.Unavailable
	}
	return raw, nil
}
func (a *Archive) syncDir() error {
	d, e := a.root.Open(".")
	if e != nil {
		return credentials.Unavailable
	}
	defer d.Close()
	if d.Sync() != nil {
		return credentials.Unavailable
	}
	return nil
}
func (a *Archive) write(name string, raw []byte) error {
	// Only the stable lock holder uses this staging file. A previous interrupted
	// write is never authoritative and can be discarded.
	if e := a.root.Remove("next"); e != nil && !os.IsNotExist(e) {
		return credentials.Unavailable
	}
	f, e := a.root.OpenFile("next", os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if e != nil {
		return credentials.Unavailable
	}
	_, e = f.Write(raw)
	if e == nil {
		e = f.Sync()
	}
	closed := f.Close()
	if e != nil || closed != nil {
		return credentials.Unavailable
	}
	if a.root.Rename("next", name) != nil {
		return credentials.Unavailable
	}
	return a.syncDir()
}
func (a *Archive) Put(ctx context.Context, id string, raw []byte) (string, error) {
	if !validID(id) || len(raw) == 0 || len(raw) > maxSnapshot {
		return "", credentials.Invalid
	}
	digest := sum(raw)
	e := a.locked(ctx, func() error {
		if _, e := a.read(id+".deleted", 64); e != credentials.Missing {
			if e != nil {
				return credentials.Unavailable
			}
			return credentials.Conflict
		}
		prior, e := a.read(id+".json", maxSnapshot)
		if e == nil {
			if sum(prior) != digest {
				return credentials.Conflict
			}
			return a.syncDir()
		}
		if e != credentials.Missing {
			return e
		}
		d, e := a.root.Open(".")
		if e != nil {
			return credentials.Unavailable
		}
		entries, e := d.ReadDir(66)
		d.Close()
		if e != nil && e != io.EOF {
			return credentials.Unavailable
		}
		if len(entries) >= 64 {
			return credentials.Exhausted
		}
		return a.write(id+".json", raw)
	})
	if e != nil {
		return "", e
	}
	return digest, nil
}
func (a *Archive) Check(ctx context.Context, id, digest string) error {
	if !validID(id) || !validID(digest) {
		return credentials.Invalid
	}
	return a.locked(ctx, func() error {
		if _, e := a.read(id+".deleted", 64); e != credentials.Missing {
			if e != nil {
				return credentials.Unavailable
			}
			if _, e = a.read(id+".json", maxSnapshot); e == credentials.Missing {
				return credentials.Missing
			}
			return credentials.Unavailable
		}
		raw, e := a.read(id+".json", maxSnapshot)
		if e != nil {
			return e
		}
		if sum(raw) != digest {
			return credentials.Unavailable
		}
		return nil
	})
}
func (a *Archive) Delete(ctx context.Context, id, digest string) error {
	if !validID(id) || !validID(digest) {
		return credentials.Invalid
	}
	return a.locked(ctx, func() error {
		tomb, e := a.read(id+".deleted", 64)
		if e == nil {
			if string(tomb) != digest {
				return credentials.Conflict
			}
		} else {
			if e != credentials.Missing {
				return e
			}
			raw, e := a.read(id+".json", maxSnapshot)
			if e != nil {
				return e
			}
			if sum(raw) != digest {
				return credentials.Conflict
			}
			if e = a.write(id+".deleted", []byte(digest)); e != nil {
				return e
			}
		}
		if e = a.root.Remove(id + ".json"); e != nil && !os.IsNotExist(e) {
			return credentials.Unavailable
		}
		return a.syncDir()
	})
}
