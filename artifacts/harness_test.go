package artifacts_test

import (
	"context"
	"lerna/adapters/filecontent"
	"lerna/adapters/sqliteauth"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{ at time.Time }

func (c *clock) Now() (time.Time, error) { return c.at, nil }

type sources struct{}

func (sources) Check(_ context.Context, s *wire.ContentSource, action, purpose, location string, until int64) error {
	if s.Kind != "input" || s.Key != "source" || s.Revision != 1 || purpose != "research" || location != "device" {
		return artifacts.Error("PERMISSION_DENIED")
	}
	return nil
}

type harness struct {
	root, token string
	now         time.Time
	st          *sqliteauth.Store
	blobs       *filecontent.Store
	auth        *authorization.Service
	content     *artifacts.Service
	binding     artifacts.Binding
}

func newHarness(t *testing.T) *harness {
	h := &harness{root: t.TempDir(), now: time.Now().Truncate(time.Second)}
	h.open(t)
	var err error
	h.token, err = h.auth.Bootstrap(context.Background(), "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	h.binding = artifacts.Binding{Token: h.token, Namespace: "local", Location: "device", Recipient: "device"}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"content.store", "content.process", "content.discover", "content.disclose", "content.retain", "content.delete"}, Purposes: []string{"research"}, Locations: []string{"device"}, ExpiresUnix: h.now.Add(time.Hour).Unix()}
	commands := []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "content", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "content", Subject: "admin", Mode: "continuous", Scope: scope}}}}
	for i, c := range commands {
		c.ExpectedRevision = uint64(i)
		op, err := h.auth.NewOperation(context.Background(), h.token)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.auth.Execute(context.Background(), h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: c}); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { h.st.Close(); h.blobs.Close() })
	return h
}
func (h *harness) open(t *testing.T) {
	var err error
	h.st, err = sqliteauth.Open(filepath.Join(h.root, "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	h.auth, err = authorization.New(h.st, &clock{h.now}, authorization.Config{CredentialTTL: time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	h.blobs, err = filecontent.Open(filepath.Join(h.root, "content"))
	if err != nil {
		t.Fatal(err)
	}
	h.content, err = artifacts.New(h.auth, h.blobs, sources{}, artifacts.Config{Inline: 16, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
}
func (h *harness) reopen(t *testing.T) { h.st.Close(); h.blobs.Close(); h.open(t) }
