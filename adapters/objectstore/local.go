// Package objectstore 提供不可变本地介质；local_fsync 不表示跨可用区耐久。
package objectstore

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
)

type Local struct {
	root string
	max  uint64
	mu   sync.Mutex
}

func OpenLocal(root string, max uint64) (*Local, error) {
	if root == "" || max == 0 || max > memory.MaxContentBytes {
		return nil, api.E("invalid_request", "invalid_object_store_limits")
	}
	path, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(path, 0700); err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, api.E("invalid_request", "unsafe_object_root")
	}
	return &Local{root: path, max: max}, nil
}

func (s *Local) Durability() string { return "local_fsync" }

func (s *Local) Locate(ctx context.Context, ref api.ContentRef) (memory.ObjectLocation, error) {
	loc := memory.ObjectLocation{Key: key(ref), Version: ref.Hash, Durability: s.Durability()}
	_, err := s.Read(ctx, loc, ref, s.max)
	return loc, err
}

func (s *Local) path(key string) (string, error) {
	if len(key) != 64 || strings.Trim(key, "0123456789abcdef") != "" {
		return "", api.E("invalid_request", "invalid_object_key")
	}
	return filepath.Join(s.root, key), nil
}

func key(ref api.ContentRef) string {
	h := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/%d", ref.TenantID, ref.OwnerID, ref.ContentID, ref.Version)))
	return hex.EncodeToString(h[:])
}

func (s *Local) Write(ctx context.Context, ref api.ContentRef, src io.Reader) (memory.ObjectLocation, error) {
	if err := api.ValidateRecord("ContentRef", ref); err != nil {
		return memory.ObjectLocation{}, err
	}
	if ref.ByteLength > s.max {
		return memory.ObjectLocation{}, api.E("invalid_request", "content_too_large")
	}
	if err := ctx.Err(); err != nil {
		return memory.ObjectLocation{}, err
	}
	loc := memory.ObjectLocation{Key: key(ref), Version: ref.Hash, Durability: s.Durability()}
	path, err := s.path(loc.Key)
	if err != nil {
		return memory.ObjectLocation{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err = os.Lstat(path); err == nil {
		if _, err = s.Read(ctx, loc, ref, s.max); err != nil {
			return memory.ObjectLocation{}, api.E("idempotency_conflict", "immutable_bytes_changed")
		}
		return loc, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return memory.ObjectLocation{}, err
	}
	tmp, err := os.CreateTemp(s.root, ".upload-")
	if err != nil {
		return memory.ObjectLocation{}, err
	}
	defer func() { _ = tmp.Close(); _ = os.Remove(tmp.Name()) }()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(&contextReader{ctx, src}, int64(ref.ByteLength)+1))
	if err != nil {
		return memory.ObjectLocation{}, err
	}
	if uint64(n) != ref.ByteLength || "sha256:"+hex.EncodeToString(h.Sum(nil)) != ref.Hash {
		return memory.ObjectLocation{}, api.E("invalid_request", "content_hash_or_length_mismatch")
	}
	if err = tmp.Sync(); err != nil {
		return memory.ObjectLocation{}, err
	}
	if err = tmp.Close(); err != nil {
		return memory.ObjectLocation{}, err
	}
	// Link 的排他创建不会覆写另一个进程已经发布的准确版本。
	if err = os.Link(tmp.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			if _, e := s.Read(ctx, loc, ref, s.max); e != nil {
				return memory.ObjectLocation{}, api.E("idempotency_conflict", "immutable_bytes_changed")
			}
		} else {
			return memory.ObjectLocation{}, err
		}
	}
	if err = os.Remove(tmp.Name()); err != nil {
		return memory.ObjectLocation{}, err
	}
	if err = syncDirectory(s.root); err != nil {
		return memory.ObjectLocation{}, err
	}
	return loc, nil
}

func (s *Local) Read(ctx context.Context, loc memory.ObjectLocation, ref api.ContentRef, max uint64) ([]byte, error) {
	if max > s.max {
		max = s.max
	}
	if ref.ByteLength > max || loc.Version != ref.Hash || loc.Key != key(ref) {
		return nil, api.E("invalid_request", "object_location_mismatch")
	}
	path, err := s.path(loc.Key)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, api.E("gone", "content_bytes_missing")
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, api.E("dependency_unavailable", "unsafe_object_file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	b, readErr := io.ReadAll(io.LimitReader(&contextReader{ctx, f}, int64(max)+1))
	closeErr := f.Close()
	if readErr != nil {
		return nil, readErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if uint64(len(b)) != ref.ByteLength || api.Hash(b) != ref.Hash {
		return nil, api.E("dependency_unavailable", "content_corrupted")
	}
	return b, nil
}

func (s *Local) Delete(ctx context.Context, loc memory.ObjectLocation) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := s.path(loc.Key)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err = os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return syncDirectory(s.root)
}

func syncDirectory(path string) error {
	dir, err := os.Open(path)
	if err != nil {
		return err
	}
	if err = dir.Sync(); err != nil {
		_ = dir.Close()
		return err
	}
	return dir.Close()
}

type contextReader struct {
	ctx    context.Context
	source io.Reader
}

func (r *contextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.source.Read(b)
}
