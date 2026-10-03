// Package platform 提供受信配置、身份、签名、生命周期与观测接入边界。
package platform

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
)

type ProofClaims struct {
	TenantID        string        `json:"tenant_id"`
	Issuer          string        `json:"issuer"`
	Audience        string        `json:"audience"`
	Purpose         string        `json:"purpose"`
	ObjectRef       api.ObjectRef `json:"object_ref"`
	Digest          string        `json:"digest"`
	ControlRevision uint64        `json:"control_revision"`
	WindowID        string        `json:"window_id"`
	IssuedAt        string        `json:"issued_at"`
	StartBefore     string        `json:"start_before"`
}
type RegisteredKey struct {
	TenantID string
	Issuer   string
	Purposes []string
	Public   *ecdsa.PublicKey
	Private  *ecdsa.PrivateKey
}
type Keyring struct{ Keys map[string]RegisteredKey }
type jwsHeader struct {
	Algorithm string `json:"alg"`
	KeyID     string `json:"kid"`
	Type      string `json:"typ"`
}

func NewDevelopmentKey(tenant, issuer string, purposes []string) (*Keyring, error) {
	k, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, e
	}
	return &Keyring{Keys: map[string]RegisteredKey{"development-es256": {tenant, issuer, purposes, &k.PublicKey, k}}}, nil
}
func permitted(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
}
func (k *Keyring) Sign(keyID string, claims ProofClaims) (string, error) {
	key, ok := k.Keys[keyID]
	if !ok || key.Private == nil || key.Private.Curve != elliptic.P256() || key.TenantID != claims.TenantID || key.Issuer != claims.Issuer || !permitted(key.Purposes, claims.Purpose) {
		return "", api.E("forbidden", "unregistered_signing_key")
	}
	payload, e := api.Canonical(api.Raw(claims))
	if e != nil {
		return "", e
	}
	h := api.Raw(jwsHeader{"ES256", keyID, "JWS"})
	encoded := base64.RawURLEncoding.EncodeToString(h) + "." + base64.RawURLEncoding.EncodeToString(payload)
	hash := sha256.Sum256([]byte(encoded))
	r, s, e := ecdsa.Sign(rand.Reader, key.Private, hash[:])
	if e != nil {
		return "", e
	}
	sig := make([]byte, 64)
	r.FillBytes(sig[:32])
	s.FillBytes(sig[32:])
	return encoded + "." + base64.RawURLEncoding.EncodeToString(sig), nil
}
func (k *Keyring) Verify(compact string, expected ProofClaims, now time.Time) (ProofClaims, error) {
	if len(compact) > 32768 {
		return ProofClaims{}, api.E("invalid_request", "proof_too_large")
	}
	parts := strings.Split(compact, ".")
	if len(parts) != 3 {
		return ProofClaims{}, api.E("forbidden", "invalid_jws")
	}
	header, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return ProofClaims{}, api.E("forbidden", "invalid_jws")
	}
	var h jwsHeader
	if e = api.Decode(header, &h); e != nil || h.Algorithm != "ES256" || h.Type != "JWS" {
		return ProofClaims{}, api.E("forbidden", "invalid_jws_algorithm")
	}
	key, ok := k.Keys[h.KeyID]
	if !ok || key.Public == nil || key.Public.Curve != elliptic.P256() {
		return ProofClaims{}, api.E("forbidden", "unregistered_verification_key")
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[2])
	if e != nil || len(signature) != 64 {
		return ProofClaims{}, api.E("forbidden", "invalid_jws_signature")
	}
	hash := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if !ecdsa.Verify(key.Public, hash[:], new(big.Int).SetBytes(signature[:32]), new(big.Int).SetBytes(signature[32:])) {
		return ProofClaims{}, api.E("forbidden", "invalid_jws_signature")
	}
	payload, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return ProofClaims{}, api.E("forbidden", "invalid_jws")
	}
	var c ProofClaims
	if e = api.Decode(payload, &c); e != nil {
		return ProofClaims{}, e
	}
	if c.TenantID != key.TenantID || c.Issuer != key.Issuer || !permitted(key.Purposes, c.Purpose) || c.TenantID != expected.TenantID || c.Issuer != expected.Issuer || c.Audience != expected.Audience || c.Purpose != expected.Purpose || c.Digest != expected.Digest || !api.Equal(c.ObjectRef, expected.ObjectRef) || c.ControlRevision != expected.ControlRevision || c.WindowID != expected.WindowID {
		return ProofClaims{}, api.E("forbidden", "proof_binding_mismatch")
	}
	issued, e := api.ParseTime(c.IssuedAt)
	if e != nil {
		return ProofClaims{}, e
	}
	until, e := api.ParseTime(c.StartBefore)
	if e != nil {
		return ProofClaims{}, e
	}
	if issued.After(now) || !now.Before(until) || !issued.Before(until) {
		return ProofClaims{}, api.E("expired", "proof_window_expired")
	}
	return c, nil
}

// ParsePublicKey 仅接受配置中预登记的P-256坐标，不从消息取公钥或URL。
func ParsePublicKey(x, y string) (*ecdsa.PublicKey, error) {
	a, e := base64.RawURLEncoding.DecodeString(x)
	if e != nil {
		return nil, e
	}
	b, e := base64.RawURLEncoding.DecodeString(y)
	if e != nil || len(a) != 32 || len(b) != 32 {
		return nil, errors.New("invalid P-256 coordinates")
	}
	p := &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(a), Y: new(big.Int).SetBytes(b)}
	if !p.Curve.IsOnCurve(p.X, p.Y) {
		return nil, errors.New("invalid P-256 point")
	}
	return p, nil
}
func PublicJWK(k *ecdsa.PublicKey) json.RawMessage {
	a, b := make([]byte, 32), make([]byte, 32)
	k.X.FillBytes(a)
	k.Y.FillBytes(b)
	return api.Raw(struct {
		Kty string `json:"kty"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}{"EC", "P-256", base64.RawURLEncoding.EncodeToString(a), base64.RawURLEncoding.EncodeToString(b)})
}
