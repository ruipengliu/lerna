//go:build darwin || linux

package egressio

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// FileLock 按规范化数据库路径串行化所有宿主实例的实际出口使用与撤销封闭。
// 锁文件不得删除或替换；进程退出由内核释放持有权。
type FileLock struct{ path string }

func NewFileLock(database string) (*FileLock, error) {
	path, e := filepath.Abs(database)
	if e != nil {
		return nil, e
	}
	path, e = filepath.EvalSymlinks(path)
	if e != nil {
		return nil, e
	}
	info, e := os.Stat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("egress requires a regular database file")
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); !ok || stat.Nlink != 1 {
		return nil, fmt.Errorf("hard-linked database aliases are unsupported")
	}
	return &FileLock{path: path + ".egress-lock"}, nil
}
func (l *FileLock) Enter(ctx context.Context) (func(), error) {
	file, e := os.OpenFile(l.path, os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return nil, e
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if e = ctx.Err(); e != nil {
			file.Close()
			return nil, e
		}
		e = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			return func() { _ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN); _ = file.Close() }, nil
		}
		if !errors.Is(e, syscall.EWOULDBLOCK) && !errors.Is(e, syscall.EAGAIN) {
			file.Close()
			return nil, e
		}
		select {
		case <-ctx.Done():
			file.Close()
			return nil, ctx.Err()
		case <-ticker.C:
		}
	}
}
