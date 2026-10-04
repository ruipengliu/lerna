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
)

var ErrIntegrity = d.ErrObjectIntegrity
var ErrUnavailable = d.ErrUnavailable
var keyPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

type Store struct {
	root      *os.Root
	directory *os.File
	mu        sync.Mutex
	closed    bool
	closeErr  error
}

// Open requires an existing, trusted absolute directory. It never creates a
// root implicitly, follows a root symlink, or accepts caller paths as keys.
func Open(path string) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrUnavailable
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return nil, ErrUnavailable
	}
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, ErrUnavailable
	}
	directory, err := root.Open(".")
	if err != nil {
		closeErr := root.Close()
		return nil, errors.Join(ErrUnavailable, closeErr)
	}
	return &Store{root: root, directory: directory}, nil
}
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.closeErr = errors.Join(s.root.Close(), s.directory.Close())
	}
	return s.closeErr
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

// Put installs one exact object without replacing an existing directory entry.
// temporary is an already persisted publication-attempt identity.
func (s *Store) Put(ctx context.Context, key, temporary, hash string, length int64, data []byte) error {
	if err := s.Ready(ctx); err != nil {
		return err
	}
	if !keyPattern.MatchString(key) || !temporaryPattern.MatchString(temporary) || !strings.HasPrefix(temporary, key+".") {
		return ErrUnavailable
	}
	if err := verify(data, hash, length); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrUnavailable
	}
	file, err := s.root.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0600)
	if err != nil {
		return errors.Join(ErrUnavailable, err)
	}
	closeFile := firstClose(file.Close)
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
		err = file.Sync()
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
	// Both a newly installed entry and an identical preexisting entry must have
	// a confirmed directory sync and an independent exact read observation.
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
	if err := s.Ready(ctx); err != nil {
		return nil, err
	}
	if !keyPattern.MatchString(key) || length < 0 || length > MaxBytes {
		return nil, ErrUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrUnavailable
	}
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
	info, statErr := file.Stat()
	if statErr != nil || !info.Mode().IsRegular() {
		return nil, errors.Join(ErrUnavailable, statErr, file.Close())
	}
	data, readErr := io.ReadAll(io.LimitReader(file, MaxBytes+1))
	if err = errors.Join(readErr, file.Close()); err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if int64(len(data)) > MaxBytes {
		return nil, ErrIntegrity
	}
	return data, nil
}

const MaxBytes int64 = 256 * 1024

var temporaryPattern = regexp.MustCompile(`^[a-f0-9]{64}\.[1-9][0-9]{0,18}\.tmp$`)

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
