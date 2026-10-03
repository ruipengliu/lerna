// Package governance 提供受信内置组件和精确文件规则评测的真实宿主。
// 本包不执行上传的代码，也不充当授权或正式数据资格的权威。
package governance

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"syscall"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type Publication struct {
	ID               string
	MediaType        string
	ProcessedSources []api.ContentRef
	DisclosedSources []api.ContentRef
}

// Content 必须绑定宿主认证身份，并核验准确来源和当前用途；IO 在 Tx 外。
type Content interface {
	Read(context.Context, api.ContentRef, string) ([]byte, error)
	Publish(context.Context, Publication, []byte) (api.ContentRef, error)
}

const maxFileBytes = 2 << 20
const maxRecords = 20000

type workspace struct {
	root    *os.Root
	scope   runtime.Scope
	content Content
	clock   func() time.Time
}

func openWorkspace(path string, scope runtime.Scope, content Content, clock func() time.Time) (*workspace, error) {
	if goruntime.GOOS != "linux" {
		return nil, api.E("unsupported", "reference_host_requires_verified_linux_flock")
	}
	if !filepath.IsAbs(path) || !api.ValidID(scope.TenantID) || !api.ValidID(scope.OwnerID) || content == nil || clock == nil {
		return nil, api.E("invalid_request", "trusted_private_host_config_required")
	}
	if e := os.MkdirAll(path, 0700); e != nil {
		return nil, e
	}
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !info.IsDir() || info.Mode().Perm()&0077 != 0 || stat.Uid != uint32(os.Geteuid()) {
		return nil, api.E("forbidden", "host_root_must_be_private_owned_directory")
	}
	root, e := os.OpenRoot(path)
	if e != nil {
		return nil, e
	}
	w := &workspace{root, scope, content, clock}
	e = w.lock(context.Background(), func() error {
		var original struct {
			TenantID string `json:"tenant_id"`
			OwnerID  string `json:"owner_id"`
		}
		e := w.readJSON("identity.json", &original)
		if e == nil {
			if original.TenantID != scope.TenantID || original.OwnerID != scope.OwnerID {
				return api.E("forbidden", "host_root_authority_changed")
			}
			return nil
		}
		if !errors.Is(e, os.ErrNotExist) {
			return e
		}
		original.TenantID = scope.TenantID
		original.OwnerID = scope.OwnerID
		return w.writeJSON("identity.json", original)
	})
	if e != nil {
		_ = root.Close()
		return nil, e
	}
	return w, nil
}

func (w *workspace) lock(ctx context.Context, fn func() error) (err error) {
	bounded, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	f, e := w.root.OpenFile("host.lock", os.O_CREATE|os.O_RDWR, 0600)
	if e != nil {
		return e
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	for {
		e = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if e == nil {
			break
		}
		if e != syscall.EWOULDBLOCK && e != syscall.EAGAIN {
			return e
		}
		select {
		case <-bounded.Done():
			return bounded.Err()
		case <-time.After(5 * time.Millisecond):
		}
	}
	defer func() {
		if e := syscall.Flock(int(f.Fd()), syscall.LOCK_UN); err == nil {
			err = e
		}
	}()
	return fn()
}

func id(prefix string, v any) string {
	d, e := api.Digest(v)
	if e != nil {
		panic(e)
	}
	return prefix + "_" + strings.TrimPrefix(d, "sha256:")[:32]
}
func directory(v any) string {
	d, e := api.Digest(v)
	if e != nil {
		panic(e)
	}
	return strings.TrimPrefix(d, "sha256:")
}
func (w *workspace) mkdir(name string) error { return w.root.MkdirAll(name, 0700) }
func (w *workspace) read(name string) (b []byte, err error) {
	info, e := w.root.Lstat(name)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Size() > maxFileBytes {
		return nil, api.E("invalid_state", "host_file_not_bounded_regular_file")
	}
	f, e := w.root.Open(name)
	if e != nil {
		return nil, e
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	b, err = io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if len(b) > maxFileBytes {
		return nil, api.E("invalid_state", "host_file_limit")
	}
	return b, err
}
func (w *workspace) readJSON(name string, v any) error {
	b, e := w.read(name)
	if e != nil {
		return e
	}
	return api.Decode(b, v)
}
func (w *workspace) writeJSON(name string, v any) error {
	b, e := api.Canonical(api.Raw(v))
	if e != nil {
		return e
	}
	return w.write(name, b)
}
func (w *workspace) syncDir(name string) (err error) {
	f, e := w.root.Open(name)
	if e != nil {
		return e
	}
	defer func() {
		if e := f.Close(); err == nil {
			err = e
		}
	}()
	return f.Sync()
}
func (w *workspace) write(name string, b []byte) (err error) {
	if len(b) > maxFileBytes {
		return api.E("invalid_request", "host_file_limit")
	}
	parent := filepath.Dir(name)
	if e := w.mkdir(parent); e != nil {
		return e
	}
	temp := name + "." + api.NewID("pending")
	f, e := w.root.OpenFile(temp, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	if _, e = f.Write(b); e == nil {
		e = f.Sync()
	}
	if ce := f.Close(); e == nil {
		e = ce
	}
	if e != nil {
		_ = w.root.Remove(temp)
		return e
	}
	if e = w.root.Rename(temp, name); e != nil {
		_ = w.root.Remove(temp)
		return e
	}
	return w.syncDir(parent)
}
func (w *workspace) exact(ctx context.Context, r api.ContentRef, purpose string) ([]byte, error) {
	if e := api.ValidateRecord("ContentRef", r); e != nil {
		return nil, e
	}
	if r.TenantID != w.scope.TenantID || r.ByteLength > maxFileBytes {
		return nil, api.E("forbidden", "content_scope_or_limit")
	}
	b, e := w.content.Read(ctx, r, purpose)
	if e != nil {
		return nil, e
	}
	if uint64(len(b)) != r.ByteLength || api.Hash(b) != r.Hash {
		return nil, api.E("forbidden", "exact_content_bytes_mismatch")
	}
	return b, nil
}
func (w *workspace) proof(ctx context.Context, kind string, v any, sources []api.ContentRef) (api.ContentRef, error) {
	b, e := api.Canonical(api.Raw(v))
	if e != nil {
		return api.ContentRef{}, e
	}
	ref, e := w.content.Publish(ctx, Publication{ID: id("proof", []any{kind, api.Hash(b)}), MediaType: "application/json", ProcessedSources: sources, DisclosedSources: []api.ContentRef{}}, b)
	if e != nil {
		return ref, e
	}
	if ref.TenantID != w.scope.TenantID || ref.OwnerID != w.scope.OwnerID || ref.Hash != api.Hash(b) || ref.ByteLength != uint64(len(b)) {
		return api.ContentRef{}, api.E("forbidden", "proof_content_binding_changed")
	}
	return ref, nil
}
