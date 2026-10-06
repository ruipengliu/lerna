// Package keys 只向受信 I/O 提供精确目标绑定的平台凭据；调用方不得记录返回字节。
package keys

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type FileBased struct{ path string }

// OpenFileBased 只打开明确指定的已有仓库；不查询默认搜索列表、不创建或解锁。
func OpenFileBased(path string) (*FileBased, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, command.Fail("CREDENTIAL_STORE_UNAVAILABLE")
	}
	if err := nativeOpen(path); err != nil {
		return nil, err
	}
	return &FileBased{path: path}, nil
}

func (s *FileBased) Resolve(ctx context.Context, b *v1.ApiTargetBinding) ([]byte, error) {
	if ctx.Err() != nil || s == nil || s.path == "" || b == nil || b.UserId == "" || b.Origin == "" || b.Account == "" || b.Resource == "" || b.CredentialRef.GetName().GetUserId() != b.UserId {
		return nil, command.Fail("CREDENTIAL_UNAVAILABLE")
	}
	return nativeRead(s.path, credentialAccount(b))
}

func credentialAccount(b *v1.ApiTargetBinding) string {
	return command.SemanticFingerprint("api-credential-binding-v1", b)
}

// SyntheticKeychain 是隔离的合成数据验证仓库；路径和密码完全由此创建，清理只作用于自身资源。
type SyntheticKeychain struct {
	store    *FileBased
	dir      string
	password []byte
}

func NewSyntheticKeychain() (*SyntheticKeychain, error) {
	dir, e := os.MkdirTemp("", "lerna-synthetic-keychain-")
	if e != nil {
		return nil, command.Fail("CREDENTIAL_STORE_UNAVAILABLE")
	}
	var random [32]byte
	if _, e = rand.Read(random[:]); e != nil {
		_ = os.Remove(dir)
		return nil, command.Fail("CREDENTIAL_STORE_UNAVAILABLE")
	}
	password := []byte(hex.EncodeToString(random[:]))
	clear(random[:])
	path := filepath.Join(dir, "synthetic.keychain-db")
	if e = nativeCreate(path, password); e != nil {
		clear(password)
		_ = os.RemoveAll(dir)
		return nil, e
	}
	return &SyntheticKeychain{store: &FileBased{path: path}, dir: dir, password: password}, nil
}

func (s *SyntheticKeychain) Store() *FileBased { return s.store }
func (s *SyntheticKeychain) Path() string      { return s.store.path }
func (s *SyntheticKeychain) Put(b *v1.ApiTargetBinding, secret []byte) error {
	if s == nil || s.dir == "" || b == nil || len(secret) < 16 || len(secret) > 16384 {
		return command.Fail("CREDENTIAL_UNAVAILABLE")
	}
	return nativePut(s.store.path, credentialAccount(b), secret)
}

// PutForExecutable 仅在隔离仓库新增合成项目，显式授权指定测试程序与创建者。
func (s *SyntheticKeychain) PutForExecutable(b *v1.ApiTargetBinding, secret []byte, executable string) error {
	if s == nil || s.dir == "" || b == nil || len(secret) < 16 || len(secret) > 16384 || !filepath.IsAbs(executable) || filepath.Clean(executable) != executable {
		return command.Fail("CREDENTIAL_UNAVAILABLE")
	}
	info, e := os.Stat(executable)
	if e != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0111 == 0 {
		return command.Fail("CREDENTIAL_UNAVAILABLE")
	}
	return nativePutForExecutable(s.store.path, credentialAccount(b), secret, executable)
}
func (s *SyntheticKeychain) Lock() error {
	return nativeLock(s.store.path)
}
func (s *SyntheticKeychain) Remove(b *v1.ApiTargetBinding) error {
	if s == nil || s.dir == "" || b == nil {
		return command.Fail("CREDENTIAL_UNAVAILABLE")
	}
	return nativeRemove(s.store.path, credentialAccount(b))
}
func (s *SyntheticKeychain) Unlock() error {
	return nativeUnlock(s.store.path, s.password)
}
func (s *SyntheticKeychain) Close() error {
	if s == nil || s.dir == "" {
		return nil
	}
	e := nativeDelete(s.store.path)
	clear(s.password)
	s.password = nil
	if e != nil {
		return e
	}
	dir := s.dir
	s.dir = ""
	if e = os.RemoveAll(dir); e != nil {
		return command.Fail("CREDENTIAL_STORE_UNAVAILABLE")
	}
	return nil
}
