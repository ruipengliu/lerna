package sqlitememory

import (
	"context"
	"errors"
	"os"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
	"lerna/memory"
)

// Restore owns a quarantined database exclusively. It deliberately does not
// implement Memory.Store or promote the backing store's ordinary data methods.
// This entry point is for explicit backup restoration, not an ordinary restart.
type Restore struct{ store *Store }

// OpenRestored requires an existing private backup and persists quarantine
// before returning. Historical state never authorizes automatic activation.
// The host must keep this path out of normal runtime configuration on failure.
func OpenRestored(path string) (*Restore, error) {
	store, err := openStore(path, true)
	if err != nil {
		return nil, err
	}
	return &Restore{store: store}, nil
}

func (r *Restore) Close() error { return r.store.Close() }

// Status is private host maintenance metadata, not a user disclosure endpoint.
func (r *Restore) Status(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	var quarantined bool
	if err := r.store.db.QueryRowContext(ctx, `SELECT quarantined FROM memory_recovery WHERE id=1`).Scan(&quarantined); err != nil || !quarantined {
		return "", memory.Unavailable
	}
	return "quarantined", nil
}

func privateDatabase(f *os.File) bool {
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 {
		return false
	}
	owner, ok := info.Sys().(*syscall.Stat_t)
	return ok && owner.Uid == uint32(os.Geteuid()) && owner.Nlink == 1
}

// Ordinary stores share a lock for their full lifetime; restoration requires
// exclusive access. A concurrent opener cannot slip between marking quarantine
// and checking it, and maintenance cannot proceed over an active runtime.
func lockDatabase(ctx context.Context, f *os.File, exclusive bool) error {
	mode := unix.LOCK_SH
	if exclusive {
		mode = unix.LOCK_EX
	}
	for {
		if ctx.Err() != nil {
			return memory.Unavailable
		}
		err := unix.Flock(int(f.Fd()), mode|unix.LOCK_NB)
		if err == nil {
			return nil
		}
		if !errors.Is(err, unix.EWOULDBLOCK) {
			return memory.Unavailable
		}
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Millisecond):
		}
	}
}
