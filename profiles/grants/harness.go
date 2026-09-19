// Package grants verifies the signed-grant authority with real local storage.
package grants

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/authorization/josegrant"
	authlocal "lerna/adapters/authorization/local"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"math/big"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() (time.Time, error) { c.mu.Lock(); defer c.mu.Unlock(); return c.now, nil }
func (c *clock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }
func authConfig() authorization.Config {
	return authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: 2 * time.Minute, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second}
}
func grantConfig() authorization.GrantConfig {
	return authorization.GrantConfig{Issuer: "fixture-authority", MaxTTL: time.Hour, IOTimeout: time.Second, MaxDepth: 4, MaxRecords: 64}
}

type harness struct {
	db         *sqliteauth.Store
	s          *authorization.Service
	g          *authorization.GrantAuthority
	client     *sdk.AuthorizationClient
	token, dir string
	c          *clock
	spec       *wire.SignedGrantSpec
	key        *ecdsa.PrivateKey
	crypto     *josegrant.Adapter
}

func setup(ctx context.Context, dir string) (*harness, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	data, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	if err = os.WriteFile(filepath.Join(dir, "signer.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: data}), 0600); err != nil {
		return nil, err
	}
	c := &clock{now: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	h, err := open(dir, "", c)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			h.db.Close()
		}
	}()
	h.token, err = h.s.Bootstrap(ctx, "local", "admin")
	if err != nil {
		return nil, err
	}
	h.client = sdk.NewAuthorizationClient(authlocal.BindGrants(h.s, h.g, h.token), "local")
	now, _ := c.Now()
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "node"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &certKey.PublicKey, certKey)
	if err != nil {
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)
	if _, err = cert.Verify(x509.VerifyOptions{Roots: roots, CurrentTime: now, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		return nil, err
	}
	digest := sha256.Sum256(cert.Raw)
	h.spec = &wire.SignedGrantSpec{Subject: "admin", Audience: "receiver", Presenter: cert.Subject.CommonName, CertificateSha256: base64.RawURLEncoding.EncodeToString(digest[:]), Scope: &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"read"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: now.Add(time.Hour).Unix()}, NotBefore: now.Unix(), Units: 8, DelegationDepth: 3, Mode: "continuous"}
	id, err := h.s.NewOperation(ctx, h.token)
	if err != nil {
		return nil, err
	}
	policyScope := proto.Clone(h.spec.Scope).(*wire.AuthorizationScope)
	policyScope.Actions = []string{"read", "write"}
	policyScope.Purposes = []string{"task", "advertising"}
	policyScope.Locations = []string{"local", "cloud"}
	_, err = h.s.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: id, Command: &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: policyScope}}}}}})
	if err != nil {
		return nil, err
	}
	ok = true
	return h, nil
}
func open(dir, token string, c *clock) (*harness, error) {
	db, err := sqliteauth.Open(filepath.Join(dir, "authority.db"))
	if err != nil {
		return nil, err
	}
	h, err := assemble(db, dir, token, c)
	if err != nil {
		db.Close()
		return nil, err
	}
	h.db = db
	return h, nil
}
func assemble(store authorization.Store, dir, token string, c *clock) (*harness, error) {
	s, err := authorization.New(store, c, authConfig())
	if err != nil {
		return nil, err
	}
	key, err := josegrant.LoadPrivateKey(filepath.Join(dir, "signer.pem"))
	if err != nil {
		return nil, err
	}
	crypto, err := josegrant.New("signer-1", key, map[string]*ecdsa.PublicKey{"signer-1": &key.PublicKey})
	if err != nil {
		return nil, err
	}
	g, err := s.SignedGrants(grantConfig(), crypto)
	if err != nil {
		return nil, err
	}
	return &harness{s: s, g: g, client: sdk.NewAuthorizationClient(authlocal.BindGrants(s, g, token), "local"), dir: dir, token: token, c: c, key: key, crypto: crypto}, nil
}
func (h *harness) request(ctx context.Context, kind string, revision uint64) (*wire.GrantMutation, error) {
	id, err := h.s.NewOperation(ctx, h.token)
	if err != nil {
		return nil, err
	}
	return &wire.GrantMutation{OperationId: id, Kind: kind, ExpectedRevision: revision, Spec: proto.Clone(h.spec).(*wire.SignedGrantSpec)}, nil
}
func (h *harness) issue(ctx context.Context) (*wire.GrantReceipt, error) {
	r, err := h.request(ctx, "ISSUE", 1)
	if err != nil {
		return nil, err
	}
	return h.client.MutateGrant(ctx, r)
}
func (h *harness) present() authorization.GrantPresentation {
	return authorization.GrantPresentation{Namespace: "local", Subject: h.spec.Subject, Audience: h.spec.Audience, Presenter: h.spec.Presenter, CertificateSHA256: h.spec.CertificateSha256}
}
func action() *wire.AuthorizationAction {
	return &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
}
func require(ok bool, why string) error {
	if !ok {
		return fmt.Errorf("%s", why)
	}
	return nil
}
func expect(err error, code authorization.Code) error {
	if !authorization.Is(err, code) {
		return fmt.Errorf("want %s, got %v", code, err)
	}
	return nil
}
