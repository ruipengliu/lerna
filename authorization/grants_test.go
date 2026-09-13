package authorization_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"lerna/adapters/josegrant"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type grantClock struct{ now time.Time }

func (c *grantClock) Now() (time.Time, error) { return c.now, nil }

type grantFixture struct {
	s      *authorization.Service
	g      *authorization.GrantAuthority
	db     *sqliteauth.Store
	token  string
	clock  *grantClock
	spec   *wire.SignedGrantSpec
	cfg    authorization.GrantConfig
	crypto *josegrant.Adapter
}

func newGrantFixture(t *testing.T) *grantFixture {
	t.Helper()
	ctx := context.Background()
	db, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := &grantClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	s, err := authorization.New(db, c, config())
	if err != nil {
		t.Fatal(err)
	}
	token, err := s.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"read"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	id, err := s.NewOperation(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: id, Command: &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: scope}}}}}})
	if err != nil {
		t.Fatal(err)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	crypto, err := josegrant.New("key1", key, map[string]*ecdsa.PublicKey{"key1": &key.PublicKey})
	if err != nil {
		t.Fatal(err)
	}
	cfg := authorization.GrantConfig{Issuer: "local-authority", MaxTTL: time.Hour, IOTimeout: time.Second, MaxDepth: 4, MaxRecords: 64}
	g, err := s.SignedGrants(cfg, crypto)
	if err != nil {
		t.Fatal(err)
	}
	return &grantFixture{s, g, db, token, c, &wire.SignedGrantSpec{Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSha256: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), Scope: scope, NotBefore: c.now.Unix(), DelegationDepth: 2, Units: 8, Mode: "continuous"}, cfg, crypto}
}
func (f *grantFixture) request(t *testing.T, kind string, revision uint64) *wire.GrantMutation {
	t.Helper()
	id, err := f.s.NewOperation(context.Background(), f.token)
	if err != nil {
		t.Fatal(err)
	}
	return &wire.GrantMutation{OperationId: id, ExpectedRevision: revision, Kind: kind, Spec: f.spec}
}
func TestSignedGrantIssueReplay(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	request := f.request(t, "ISSUE", 1)
	receipt, err := f.g.Mutate(ctx, f.token, request)
	if err != nil {
		t.Fatal(err)
	}
	if receipt.Material == "" || receipt.Revision != 2 {
		t.Fatalf("missing committed material: %+v", receipt)
	}
	replay, err := f.g.Mutate(ctx, f.token, request)
	if err != nil || replay.Material != receipt.Material || replay.GrantId != receipt.GrantId {
		t.Fatalf("replay: %+v %v", replay, err)
	}
	record, err := f.g.Get(ctx, f.token, receipt.GrantId)
	if err != nil || record.Spec.Units != 8 {
		t.Fatalf("record: %+v %v", record, err)
	}
}

func TestSignedGrantChecksPresenterAndCurrentPolicy(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	r, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if err != nil {
		t.Fatal(err)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256}
	action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	if _, err = f.g.Verify(ctx, r.Material, p, action); err != nil {
		t.Fatal(err)
	}
	p.Presenter = "impostor"
	if _, err = f.g.Verify(ctx, r.Material, p, action); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("wrong presenter accepted: %v", err)
	}
	p.Presenter = "node"
	id, _ := f.s.NewOperation(ctx, f.token)
	_, err = f.s.Execute(ctx, f.token, authorization.Mutation{Namespace: "local", OperationID: id, Command: &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.g.Verify(ctx, r.Material, p, action); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("signature bypassed current policy: %v", err)
	}
}

func TestGrantDerivationReservesSharedQuotaAndAncestorRevocation(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	root, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if err != nil {
		t.Fatal(err)
	}
	child := f.request(t, "DERIVE", 2)
	child.GrantId = root.GrantId
	child.ExpectedGrantRevision = 2
	child.Spec.Units = 5
	child.Spec.DelegationDepth = 1
	r, err := f.g.Mutate(ctx, f.token, child)
	if err != nil {
		t.Fatal(err)
	}
	over := f.request(t, "DERIVE", 3)
	over.GrantId = root.GrantId
	over.ExpectedGrantRevision = 2
	over.Spec.Units = 4
	over.Spec.DelegationDepth = 0
	if _, err = f.g.Mutate(ctx, f.token, over); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("overallocated: %v", err)
	}
	revoke := f.request(t, "REVOKE", 3)
	revoke.Spec = nil
	revoke.GrantId = root.GrantId
	revoke.ExpectedGrantRevision = 2
	if _, err = f.g.Mutate(ctx, f.token, revoke); err != nil {
		t.Fatal(err)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256}
	if _, err = f.g.Verify(ctx, r.Material, p, &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("ancestor revoke bypassed: %v", err)
	}
}

func TestGrantSelfUseAndDelegationShareRemainingUnits(t *testing.T) {
	f := newGrantFixture(t)
	ctx := context.Background()
	root, err := f.g.Mutate(ctx, f.token, f.request(t, "ISSUE", 1))
	if err != nil {
		t.Fatal(err)
	}
	p := authorization.GrantPresentation{Namespace: "local", Subject: "admin", Audience: "receiver", Presenter: "node", CertificateSHA256: f.spec.CertificateSha256, OperationID: "business-1", SemanticSHA256: strings.Repeat("a", 64)}
	action := &wire.AuthorizationAction{Resource: "root", Action: "read", Purpose: "task", Location: "local"}
	use, err := f.g.ReserveUse(ctx, root.Material, p, action, 5)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := f.g.ReserveUse(ctx, root.Material, p, action, 5)
	if err != nil || replay != use {
		t.Fatalf("double reservation: %+v %v", replay, err)
	}
	child := f.request(t, "DERIVE", 2)
	child.GrantId = root.GrantId
	child.ExpectedGrantRevision = 2
	child.Spec.Units = 4
	child.Spec.DelegationDepth = 0
	if _, err = f.g.Mutate(ctx, f.token, child); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("parent self-use ignored: %v", err)
	}
	p.SemanticSHA256 = strings.Repeat("b", 64)
	if _, err = f.g.ReserveUse(ctx, root.Material, p, action, 5); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("operation reinterpreted: %v", err)
	}
}
