package platform

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"github.com/ruipengliu/lerna/api"
	"os"
	"path/filepath"
)

// OpenDevelopmentKey 保留本地开发身份；生产配置必须来自受信密钥目录。
func OpenDevelopmentKey(path, tenant, issuer string, purposes []string) (*Keyring, error) {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return nil, e
	}
	b, e := os.ReadFile(path)
	if os.IsNotExist(e) {
		private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		der, err := x509.MarshalECPrivateKey(private)
		if err != nil {
			return nil, err
		}
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if os.IsExist(err) {
			b, e = os.ReadFile(path)
		} else {
			if err != nil {
				return nil, err
			}
			b = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
			_, e = f.Write(b)
			if e == nil {
				e = f.Sync()
			}
			ce := f.Close()
			if e == nil {
				e = ce
			}
			if e == nil {
				dir, err := os.Open(filepath.Dir(path))
				if err != nil {
					return nil, err
				}
				e = dir.Sync()
				ce = dir.Close()
				if e == nil {
					e = ce
				}
			}
		}
	}
	if e != nil {
		return nil, e
	}
	info, e := os.Lstat(path)
	if e != nil {
		return nil, e
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, api.E("forbidden", "private_key_file_permissions")
	}
	block, rest := pem.Decode(b)
	if block == nil || len(rest) > 0 {
		return nil, api.E("invalid_request", "invalid_key_file")
	}
	private, e := x509.ParseECPrivateKey(block.Bytes)
	if e != nil || private.Curve != elliptic.P256() {
		return nil, api.E("invalid_request", "invalid_key_file")
	}
	return &Keyring{Keys: map[string]RegisteredKey{"development-es256": {TenantID: tenant, Issuer: issuer, Purposes: purposes, Public: &private.PublicKey, Private: private}}}, nil
}
func StableDevelopmentID(prefix, name string) string {
	return prefix + "_" + api.Hash([]byte("harness-development/" + name))[7:39]
}
