// Package josegrant binds fixed ES256 JOSE to trusted host-provided keys.
package josegrant

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"encoding/base64"
	"encoding/json"
	jose "github.com/go-jose/go-jose/v4"
	"lerna/authorization"
	"lerna/internal/jsonvalue"
	"math/big"
	"strings"
	"time"
)

const Type = "harness-grant+jwt;v=1"

type Adapter struct {
	clock  authorization.Clock
	until  map[string]time.Time
	kid    string
	signer jose.Signer
	keys   map[string]*ecdsa.PublicKey
}

func New(kid string, private *ecdsa.PrivateKey, trusted map[string]*ecdsa.PublicKey) (*Adapter, error) {
	invalid := func() (*Adapter, error) { return nil, &authorization.Error{Code: authorization.Invalid} }
	if kid == "" || len(kid) > 128 || private == nil || private.D == nil || private.Curve != elliptic.P256() || private.D.Sign() <= 0 || private.D.Cmp(elliptic.P256().Params().N) >= 0 || len(trusted) == 0 || len(trusted) > 64 {
		return invalid()
	}
	validPublic := func(key *ecdsa.PublicKey) bool {
		return key != nil && key.Curve == elliptic.P256() && key.X != nil && key.Y != nil && key.Curve.IsOnCurve(key.X, key.Y)
	}
	if !validPublic(&private.PublicKey) {
		return invalid()
	}
	x, y := elliptic.P256().ScalarBaseMult(private.D.Bytes())
	if x.Cmp(private.X) != 0 || y.Cmp(private.Y) != 0 {
		return invalid()
	}
	signingPublic := trusted[kid]
	if !validPublic(signingPublic) || signingPublic.X.Cmp(x) != 0 || signingPublic.Y.Cmp(y) != 0 {
		return invalid()
	}
	keys := map[string]*ecdsa.PublicKey{}
	for id, key := range trusted {
		if id == "" || len(id) > 128 || !validPublic(key) {
			return invalid()
		}
		keys[id] = &ecdsa.PublicKey{Curve: key.Curve, X: new(big.Int).Set(key.X), Y: new(big.Int).Set(key.Y)}
	}
	owned := &ecdsa.PrivateKey{PublicKey: *keys[kid], D: new(big.Int).Set(private.D)}
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.ES256, Key: owned}, (&jose.SignerOptions{}).WithType(Type).WithHeader("kid", kid))
	if err != nil {
		return invalid()
	}

	return &Adapter{signer: signer, keys: keys}, nil
}
func (a *Adapter) Sign(ctx context.Context, payload []byte) (string, error) {
	if err := a.current(ctx, a.kid); err != nil {
		return "", err
	}
	signed, err := a.signer.Sign(payload)
	if err != nil {
		return "", &authorization.Error{Code: authorization.Unavailable}
	}
	return signed.CompactSerialize()
}

func (a *Adapter) Verify(ctx context.Context, material string) ([]byte, error) {
	denied := func() ([]byte, error) { return nil, &authorization.Error{Code: authorization.Denied} }
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(material) > 32768 {
		return denied()
	}
	parts := strings.Split(material, ".")
	if len(parts) != 3 {
		return denied()
	}
	for _, part := range parts {
		raw, err := base64.RawURLEncoding.Strict().DecodeString(part)
		if err != nil || base64.RawURLEncoding.EncodeToString(raw) != part {
			return denied()
		}
	}
	header, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return denied()
	}
	value, err := jsonvalue.Decode(header)
	if err != nil {
		return denied()
	}
	obj, ok := value.(map[string]any)
	if !ok || len(obj) != 3 || obj["alg"] != "ES256" || obj["typ"] != Type {
		return denied()
	}
	kid, ok := obj["kid"].(string)
	if !ok {
		return denied()
	}
	if err := a.current(ctx, kid); err != nil {
		return nil, err
	}
	key := a.keys[kid]
	if key == nil {
		return denied()
	}
	signed, err := jose.ParseSignedCompact(material, []jose.SignatureAlgorithm{jose.ES256})
	if err != nil {
		return denied()
	}
	payload, err := signed.Verify(key)
	if err != nil || !json.Valid(payload) {
		return denied()
	}
	return payload, nil
}
