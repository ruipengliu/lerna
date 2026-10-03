//go:build linux

package sqlite

import (
	"context"
	"errors"
	"os"
	"syscall"
)

// Hold a kernel flock on the database inode for this writable Host's lifetime.
// SQLite uses separate POSIX byte-range locks, so this Host exclusion does not
// replace its transaction locks. Lock state disappears when the process exits.
func acquireWriter(ctx context.Context, path string) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (func() error, error) { file.Close(); return nil, err }
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() {
		return fail(errors.New("SQLite requires a regular database file"))
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return fail(errors.New("SQLite database hard links are unsupported"))
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return file.Close, nil
		}
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return fail(ErrWriterActive)
		}
		if !errors.Is(err, syscall.EINTR) {
			return fail(err)
		}
		if err = ctx.Err(); err != nil {
			return fail(err)
		}
	}
}
