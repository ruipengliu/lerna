// Package filecontent provides an owner-only, root-confined Linux content store.
package filecontent

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"lerna/artifacts"
	"os"
	"strings"
	"time"
)

type Store struct{ root *os.Root }

func Open(path string) (*Store, error) {
	if err := os.MkdirAll(path, 0700); err != nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	i, err := os.Lstat(path)
	if err != nil || !i.IsDir() || i.Mode().Perm()&0077 != 0 {
		return nil, artifacts.Error("PERMISSION_DENIED")
	}
	r, err := os.OpenRoot(path)
	if err != nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	return &Store{r}, nil
}
func (s *Store) Close() error { return s.root.Close() }
func key(k string) bool {
	if strings.HasSuffix(k, ".tmp") {
		k = strings.TrimSuffix(k, ".tmp")
	}
	b, e := hex.DecodeString(k)
	return e == nil && len(b) == 32 && k == strings.ToLower(k)
}
func (s *Store) Lock(ctx context.Context) (func(), error) {
	f, err := s.root.OpenFile(".lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	for {
		if err = ctx.Err(); err != nil {
			f.Close()
			return nil, artifacts.Error("UNAVAILABLE")
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { unix.Flock(int(f.Fd()), unix.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			f.Close()
			return nil, artifacts.Error("UNAVAILABLE")
		}
		select {
		case <-ctx.Done():
		case <-time.After(time.Millisecond * 5):
		}
	}
}
func (s *Store) sync() error {
	f, err := s.root.Open(".")
	if err != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	defer f.Close()
	if f.Sync() != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	return nil
}
func (s *Store) Put(ctx context.Context, k string, data []byte) error {
	if !key(k) || strings.HasSuffix(k, ".tmp") || len(data) > 1<<20 {
		return artifacts.Error("INVALID_ARGUMENT")
	}
	f, err := s.root.OpenFile(k+".tmp", os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	defer f.Close()
	for offset := 0; offset < len(data); {
		if ctx.Err() != nil {
			return artifacts.Error("UNAVAILABLE")
		}
		end := min(offset+32768, len(data))
		n, err := f.Write(data[offset:end])
		if err != nil || n != end-offset {
			return artifacts.Error("UNAVAILABLE")
		}
		offset = end
	}
	if f.Sync() != nil || f.Close() != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	if s.root.Rename(k+".tmp", k) != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	return s.sync()
}
func (s *Store) Read(ctx context.Context, k string, offset uint64, limit uint32, size uint64, sha string) ([]byte, error) {
	if !key(k) || size > 1<<20 || offset > size || uint64(limit) > size-offset || limit > 65536 {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	f, err := s.root.OpenFile(k, os.O_RDONLY|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, artifacts.Error("CONTENT_MISSING")
	}
	if err != nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	if info.Size() != int64(size) {
		return nil, artifacts.Error("CONTENT_CORRUPT")
	}
	hash := sha256.New()
	buf := make([]byte, 32768)
	for remaining := size; remaining > 0; {
		if ctx.Err() != nil {
			return nil, artifacts.Error("UNAVAILABLE")
		}
		n, err := io.ReadFull(f, buf[:min(uint64(len(buf)), remaining)])
		if err != nil {
			return nil, artifacts.Error("CONTENT_CORRUPT")
		}
		hash.Write(buf[:n])
		remaining -= uint64(n)
	}
	if hex.EncodeToString(hash.Sum(nil)) != sha {
		return nil, artifacts.Error("CONTENT_CORRUPT")
	}
	out := make([]byte, limit)
	if _, err = f.ReadAt(out, int64(offset)); err != nil {
		return nil, artifacts.Error("CONTENT_CORRUPT")
	}
	return out, nil
}
func (s *Store) Remove(ctx context.Context, k string) error {
	if !key(k) {
		return artifacts.Error("INVALID_ARGUMENT")
	}
	if ctx.Err() != nil {
		return artifacts.Error("UNAVAILABLE")
	}
	if err := s.root.Remove(k); err != nil && !errors.Is(err, os.ErrNotExist) {
		return artifacts.Error("UNAVAILABLE")
	}
	return s.sync()
}
func (s *Store) List(ctx context.Context, max int) ([]artifacts.Blob, error) {
	if max < 1 || max > 1024 {
		return nil, artifacts.Error("INVALID_ARGUMENT")
	}
	f, err := s.root.Open(".")
	if err != nil {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	defer f.Close()
	items, err := f.ReadDir(max + 2)
	if err != nil && err != io.EOF {
		return nil, artifacts.Error("UNAVAILABLE")
	}
	out := []artifacts.Blob{}
	for _, e := range items {
		if ctx.Err() != nil {
			return nil, artifacts.Error("UNAVAILABLE")
		}
		if e.Name() == ".lock" {
			continue
		}
		if !key(e.Name()) || e.Type()&os.ModeSymlink != 0 {
			return nil, artifacts.Error("UNAVAILABLE")
		}
		i, err := e.Info()
		if err != nil || !i.Mode().IsRegular() {
			return nil, artifacts.Error("UNAVAILABLE")
		}
		out = append(out, artifacts.Blob{Key: e.Name(), Size: uint64(i.Size())})
	}
	if len(out) > max {
		return nil, artifacts.Error("CAPACITY_EXCEEDED")
	}
	return out, nil
}
