package josegrant_test

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"lerna/adapters/authorization/josegrant"
	"testing"
)

func TestSignerRequiresMatchingTrustedKey(t *testing.T) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, keys := range map[string]map[string]*ecdsa.PublicKey{"missing": nil, "empty": {}, "different-kid": {"other": &key.PublicKey}, "mismatch": {"key": &other.PublicKey}} {
		t.Run(name, func(t *testing.T) {
			if _, err := josegrant.New("key", key, keys); err == nil {
				t.Fatal("enabled signer with unusable trust mapping")
			}
		})
	}
}
