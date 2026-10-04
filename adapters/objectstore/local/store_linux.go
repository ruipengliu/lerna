//go:build linux

// Package local implements immutable bounded objects on a trusted Linux volume.
package local

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	d "github.com/ruipengliu/lerna/domain/content"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"
)

var ErrIntegrity = d.ErrObjectIntegrity
var ErrUnavailable = d.ErrUnavailable
var ErrCloseTimeout = errors.New("local object active invocation drain unconfirmed")
var keyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var temporaryPattern = regexp.MustCompile(`^[a-f0-9]{64}\.[1-9][0-9]{0,18}\.tmp$`)

const MaxBytes int64 = 256 * 1024

type native struct {
	openDirectory func(*os.Root) (*os.File, error)
	closeRoot     func(*os.Root) error
	closeFile     func(*os.File) error
	syncFile      func(*os.File) error
}

func actualNative() native {
	return native{openDirectory: func(root *os.Root) (*os.File, error) { return root.Open(".") }, closeRoot: func(root *os.Root) error { return root.Close() }, closeFile: func(file *os.File) error { return file.Close() }, syncFile: func(file *os.File) error { return file.Sync() }}
}

type Store struct {
	root                      *os.Root
	directory                 *os.File
	ops                       native
	gate                      chan struct{}
	mu                        sync.Mutex
	closing                   bool
	rootClose, directoryClose func() error
	files                     map[*os.File]func() error
	childCloseErr             error
}

func Open(path string) (*Store, error) { return open(path, actualNative()) }
func open(path string, ops native) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnavailable
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if resolved != path {
		return nil, ErrUnavailable
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	holder := &Store{root: root, ops: ops, gate: make(chan struct{}, 1), files: map[*os.File]func() error{}}
	holder.gate <- struct{}{}
	holder.rootClose = firstClose(func() error { return ops.closeRoot(root) })
	directory, err := ops.openDirectory(root)
	if directory != nil {
		holder.directory = directory
		holder.directoryClose = firstClose(func() error { return ops.closeFile(directory) })
	}
	if err != nil {
		closeErr := holder.Close()
		if closeErr != nil {
			return holder, errors.Join(ErrUnavailable, err, closeErr)
		}
		return nil, errors.Join(ErrUnavailable, err)
	}
	return holder, nil
}
func (s *Store) Ready(ctx context.Context) error {
	if ctx == nil {
		return ErrUnavailable
	}
	if _, finite := ctx.Deadline(); !finite {
		return ErrUnavailable
	}
	return ctx.Err()
}
func (s *Store) acquire(ctx context.Context) error {
	if err := s.Ready(ctx); err != nil {
		return err
	}
	s.mu.Lock()
	closing := s.closing
	s.mu.Unlock()
	if closing {
		return ErrUnavailable
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.gate:
	}
	s.mu.Lock()
	closing = s.closing
	s.mu.Unlock()
	if closing || ctx.Err() != nil {
		s.gate <- struct{}{}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return ErrUnavailable
	}
	return nil
}
func (s *Store) release() { s.gate <- struct{}{} }
func (s *Store) track(file *os.File) func() error {
	closeFile := firstClose(func() error {
		err := s.ops.closeFile(file)
		s.mu.Lock()
		defer s.mu.Unlock()
		if err != nil {
			s.childCloseErr = errors.Join(s.childCloseErr, err)
		} else {
			delete(s.files, file)
		}
		return err
	})
	s.mu.Lock()
	s.files[file] = closeFile
	s.mu.Unlock()
	return closeFile
}

// CloseContext seals new calls and bounds only the drain wait. Native Sync and
// Close cannot be canceled by a context; their invocation remains owned until
// it actually returns. A drain timeout is retryable after that real return.
func (s *Store) CloseContext(ctx context.Context) error {
	if ctx == nil {
		return ErrCloseTimeout
	}
	if _, finite := ctx.Deadline(); !finite {
		return ErrCloseTimeout
	}
	if ctx.Err() != nil {
		return errors.Join(ErrCloseTimeout, ctx.Err())
	}
	s.mu.Lock()
	s.closing = true
	s.mu.Unlock()
	select {
	case <-ctx.Done():
		return errors.Join(ErrCloseTimeout, ctx.Err())
	case <-s.gate:
	}
	defer s.release()
	if ctx.Err() != nil {
		return errors.Join(ErrCloseTimeout, ctx.Err())
	}
	s.mu.Lock()
	children := make([]func() error, 0, len(s.files))
	for _, closeFile := range s.files {
		children = append(children, closeFile)
	}
	s.mu.Unlock()
	for _, closeFile := range children {
		_ = closeFile()
	}
	var rootErr, directoryErr error
	if s.rootClose != nil {
		rootErr = s.rootClose()
	}
	if s.directoryClose != nil {
		directoryErr = s.directoryClose()
	}
	s.mu.Lock()
	childErr := s.childCloseErr
	s.mu.Unlock()
	return errors.Join(rootErr, directoryErr, childErr)
}
func (s *Store) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return s.CloseContext(ctx)
}
func (s *Store) Put(ctx context.Context, key, temporary, hash string, length int64, data []byte) error {
	if !keyPattern.MatchString(key) || !temporaryPattern.MatchString(temporary) || !strings.HasPrefix(temporary, key+".") {
		return ErrUnavailable
	}
	if err := verify(data, hash, length); err != nil {
		return err
	}
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	file, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	closeFile := s.track(file)
	for start := 0; start < len(data); {
		if err = ctx.Err(); err != nil {
			break
		}
		end := min(start+64*1024, len(data))
		var n int
		n, err = file.Write(data[start:end])
		start += n
		if err != nil {
			break
		}
		if n == 0 {
			err = io.ErrShortWrite
			break
		}
	}
	if err == nil {
		var staged []byte
		staged, err = s.read(temporary)
		if err == nil {
			err = verify(staged, hash, length)
		}
	}
	if err == nil {
		err = s.ops.syncFile(file)
	}
	err = errors.Join(err, closeFile())
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	err = s.root.Link(temporary, key)
	if err != nil && !errors.Is(err, os.ErrExist) {
		return errors.Join(ErrUnavailable, err)
	}
	if err = s.directory.Sync(); err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	actual, err := s.read(key)
	if err != nil {
		return err
	}
	if err = verify(actual, hash, length); err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = s.root.Remove(temporary); err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	if err = s.directory.Sync(); err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	return ctx.Err()
}
func (s *Store) Read(ctx context.Context, key, hash string, length int64) ([]byte, error) {
	if !keyPattern.MatchString(key) || length < 0 || length > MaxBytes {
		return nil, ErrUnavailable
	}
	if err := s.acquire(ctx); err != nil {
		return nil, err
	}
	defer s.release()
	data, err := s.read(key)
	if err != nil {
		return nil, err
	}
	if err = verify(data, hash, length); err != nil {
		return nil, err
	}
	if err = ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}
func (s *Store) read(key string) ([]byte, error) {
	file, err := s.root.OpenFile(key, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	closeFile := s.track(file)
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(ErrUnavailable, statErr, closeFile())
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err = errors.Join(readErr, closeFile()); err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if int64(len(data)) > MaxBytes {
		return nil, ErrIntegrity
	}
	return data, nil
}
func verify(data []byte, hash string, length int64) error {
	if length < 0 || length > MaxBytes || int64(len(data)) != length {
		return ErrIntegrity
	}
	sum := sha256.Sum256(data)
	if hash != "sha256:"+hex.EncodeToString(sum[:]) {
		return ErrIntegrity
	}
	return nil
}
func firstClose(fn func() error) func() error {
	var once sync.Once
	var err error
	return func() error { once.Do(func() { err = fn() }); return err }
}
