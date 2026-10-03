//go:build linux

package wasi

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ruipengliu/lerna/api"
	"golang.org/x/sys/unix"
)

func errorsJoin(a, b error) error { return errors.Join(a, b) }
func kernelRelease() string {
	var n unix.Utsname
	if unix.Uname(&n) != nil {
		return "unknown"
	}
	b := []byte{}
	for _, c := range n.Release {
		if c == 0 {
			break
		}
		b = append(b, byte(c))
	}
	return string(b)
}
func acquireRuntimeLock(root *os.Root) (*os.File, error) {
	f, err := openPrivateFile(root, "ownership.lock", os.O_CREATE|os.O_RDWR)
	if err != nil {
		return nil, err
	}
	if err = unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		f.Close()
		return nil, api.E("invalid_state", "wasi_root_already_owned")
	}
	return f, nil
}
func verifyPrivateRoot(path string) error {
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm() != 0700 || stat.Uid != uint32(os.Getuid()) {
		return api.E("unsupported", "wasi_root_not_private")
	}
	return nil
}
func openPrivateFile(root *os.Root, name string, flags int) (*os.File, error) {
	f, err := root.OpenFile(name, flags|unix.O_NOFOLLOW, 0600)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm() != 0600 || stat.Uid != uint32(os.Getuid()) || stat.Nlink != 1 {
		f.Close()
		return nil, api.E("unsupported", "wasi_journal_file_not_private")
	}
	return f, nil
}
func processStart(pid int) (string, error) {
	if pid <= 0 {
		return "", os.ErrNotExist
	}
	b, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", err
	}
	end := strings.LastIndexByte(string(b), ')')
	if end < 0 {
		return "", api.E("invalid_state", "wasi_process_identity_unavailable")
	}
	fields := strings.Fields(string(b)[end+1:])
	if len(fields) < 20 || fields[2] != strconv.Itoa(pid) {
		return "", api.E("invalid_state", "wasi_process_group_changed")
	}
	if fields[0] == "Z" {
		return "", os.ErrNotExist
	}
	return fields[19], nil
}
func fenceProcess(ctx context.Context, pid int, start string) error {
	if pid == 0 {
		return nil
	}
	current, err := processStart(pid)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if start == "" {
		return api.E("dependency_unavailable", "wasi_original_process_identity_unknown")
	}
	if current != start {
		return nil
	} // PID已复用，不触碰新的进程。
	if err = syscall.Kill(-pid, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		current, err = processStart(pid)
		if errors.Is(err, os.ErrNotExist) || err == nil && current != start {
			return nil
		}
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return api.E("dependency_unavailable", "wasi_actual_exit_not_confirmed")
		case <-tick.C:
		}
	}
}
func configureProcess(cmd *exec.Cmd) {
	cmd.Env = []string{}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true, Pdeathsig: syscall.SIGKILL}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 5 * time.Second
}
func trustedFileHash(path string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return "", err
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || stat.Uid != uint32(os.Getuid()) && stat.Uid != 0 || info.Size() > 64<<20 {
		return "", api.E("unsupported", "wasi_executable_not_immutable")
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
	if err != nil {
		return "", err
	}
	return api.Hash(b), nil
}
func (r *Runtime) verifyExecutables() error {
	hash, err := trustedFileHash(r.cfg.WorkerPath)
	if err != nil {
		return err
	}
	if hash != r.cfg.WorkerHash {
		return api.E("unsupported", "wasi_worker_artifact_changed")
	}
	for _, x := range []struct {
		path   string
		target *string
	}{{r.bwrap, &r.manifest.BubblewrapHash}, {r.prlimit, &r.manifest.PrlimitHash}} {
		h, err := trustedFileHash(x.path)
		if err != nil {
			return err
		}
		if *x.target != "" && *x.target != h {
			return api.E("unsupported", "wasi_launcher_artifact_changed")
		}
		if *x.target == "" {
			*x.target = h
		}
	}
	return nil
}
