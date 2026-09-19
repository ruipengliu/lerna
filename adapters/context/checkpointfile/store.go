// Package checkpointfile atomically replaces bounded private checkpoint files.
// Hosts own the small fixed filename inventory. This is not a peer file API.
package checkpointfile

import (
	"bytes"
	"context"
	"errors"
	"golang.org/x/sys/unix"
	"io"
	"os"
	"strings"
	"syscall"
	"time"
)

const MaxBytes = 1 << 20

var Invalid = errors.New("invalid checkpoint file")
var Conflict = errors.New("checkpoint changed")
var Unavailable = errors.New("checkpoint file unavailable")

type Store struct{ root *os.Root }

func Open(path string) (*Store, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0022 != 0 {
		return nil, Invalid
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	if !ok || owner.Uid != uint32(os.Geteuid()) {
		return nil, Invalid
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, Unavailable
	}
	return &Store{root}, nil
}
func (s *Store) Close() error { return s.root.Close() }

func privateFile(info os.FileInfo) bool {
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	return ok && owner.Uid == uint32(os.Geteuid()) && owner.Nlink == 1
}

func nameValid(name string) bool {
	if len(name) == 0 || len(name) > 128 || !strings.HasSuffix(name, ".json") {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

func (s *Store) lock(ctx context.Context) (func(), error) {
	f, err := s.root.OpenFile(".checkpoint.lock", os.O_CREATE|os.O_RDWR|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, Unavailable
	}
	info, err := f.Stat()
	if err != nil || !privateFile(info) {
		f.Close()
		return nil, Invalid
	}
	for {
		if err = ctx.Err(); err != nil {
			f.Close()
			return nil, err
		}
		err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB)
		if err == nil {
			return func() { unix.Flock(int(f.Fd()), unix.LOCK_UN); f.Close() }, nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			f.Close()
			return nil, Unavailable
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Millisecond):
		}
	}
}
func (s *Store) syncDir() error {
	dir, err := s.root.Open(".")
	if err != nil {
		return Unavailable
	}
	defer dir.Close()
	if dir.Sync() != nil {
		return Unavailable
	}
	return nil
}

// Replace compares a previously read image under an interprocess lock. A retry
// of the exact replacement is harmless after an unknown result. One reserved
// .pending file per host-owned name bounds interrupted staging; it is never read
// as the authoritative checkpoint. The caller closes Store after all work ends.
func (s *Store) Replace(ctx context.Context, name string, previous, next []byte) error {
	if !nameValid(name) || len(previous) == 0 || len(previous) > MaxBytes || len(next) == 0 || len(next) > MaxBytes {
		return Invalid
	}
	previous = append([]byte(nil), previous...)
	next = append([]byte(nil), next...)
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	unlock, err := s.lock(ctx)
	if err != nil {
		return err
	}
	defer unlock()
	original, err := s.root.OpenFile(name, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0)
	if err != nil {
		return Unavailable
	}
	defer original.Close()
	info, err := original.Stat()
	if err != nil || !privateFile(info) || info.Size() > MaxBytes {
		return Invalid
	}
	current, err := io.ReadAll(io.LimitReader(original, MaxBytes+1))
	if err != nil || len(current) > MaxBytes {
		return Unavailable
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if bytes.Equal(current, next) {
		if original.Sync() != nil {
			return Unavailable
		}
		return s.syncDir()
	}
	if !bytes.Equal(current, previous) {
		return Conflict
	}
	pending := name + ".pending"
	if info, err := s.root.Lstat(pending); err == nil {
		if !privateFile(info) || info.Size() > MaxBytes {
			return Invalid
		}
		if s.root.Remove(pending) != nil {
			return Unavailable
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return Unavailable
	}
	staged, err := s.root.OpenFile(pending, os.O_CREATE|os.O_EXCL|os.O_WRONLY|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return Unavailable
	}
	defer staged.Close()
	defer s.root.Remove(pending)
	for offset := 0; offset < len(next); {
		if err = ctx.Err(); err != nil {
			return err
		}
		end := min(offset+32768, len(next))
		n, err := staged.Write(next[offset:end])
		if err != nil || n != end-offset {
			return Unavailable
		}
		offset = end
	}
	if staged.Sync() != nil {
		return Unavailable
	}
	if staged.Close() != nil {
		return Unavailable
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if s.root.Rename(pending, name) != nil {
		return Unavailable
	}
	return s.syncDir()
}
