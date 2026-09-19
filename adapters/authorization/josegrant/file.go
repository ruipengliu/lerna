package josegrant

import (
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"lerna/authorization"
	"os"
)

// LoadPrivateKey reads a host-injected, owner-only PKCS8 file. Key provisioning
// and rotation are outside this adapter; no private bytes enter the grant store.
func LoadPrivateKey(path string) (*ecdsa.PrivateKey, error) {
	unavailable := func() (*ecdsa.PrivateKey, error) { return nil, &authorization.Error{Code: authorization.Unavailable} }
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return unavailable()
	}
	f, err := os.Open(path)
	if err != nil {
		return unavailable()
	}
	defer f.Close()
	current, err := f.Stat()
	if err != nil || !os.SameFile(info, current) {
		return unavailable()
	}
	data, err := io.ReadAll(io.LimitReader(f, 8193))
	if err != nil || len(data) > 8192 {
		return unavailable()
	}
	block, rest := pem.Decode(data)
	if block == nil || len(rest) != 0 || block.Type != "PRIVATE KEY" {
		return unavailable()
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return unavailable()
	}
	ec, ok := key.(*ecdsa.PrivateKey)
	if !ok {
		return unavailable()
	}
	return ec, nil
}
