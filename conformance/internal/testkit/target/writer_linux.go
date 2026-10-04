//go:build linux

package target

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"syscall"
)

var ErrWriterActive = errors.New("test target writer already active")

func acquireWriter(ctx context.Context, path string) (func() error, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, errors.New("target requires a regular file, not a symlink or device")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return nil, err
	}
	closeFile := firstFileClose(file.Close)
	var closeParent func() error
	release := func() error {
		var parentErr error
		if closeParent != nil {
			parentErr = closeParent()
		}
		return errors.Join(lifetimeCause("writer file close unknown", closeFile()), lifetimeCause("writer parent directory close unknown", parentErr))
	}
	fail := func(err error) (func() error, error) {
		if closeErr := release(); closeErr != nil {
			return release, errors.Join(lifetimeCause("writer acquisition failed", err), closeErr)
		}
		return nil, lifetimeCause("writer acquisition failed", err)
	}
	info, err := file.Stat()
	if err != nil {
		return fail(err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || stat.Nlink != 1 {
		return fail(errors.New("target requires regular file without hard links"))
	}
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			break
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
	parent, err := os.Open(filepath.Dir(path))
	if err != nil {
		return fail(err)
	}
	closeParent = firstFileClose(parent.Close)
	if err = errors.Join(file.Sync(), parent.Sync(), closeParent()); err != nil {
		return fail(err)
	}
	return release, nil
}
