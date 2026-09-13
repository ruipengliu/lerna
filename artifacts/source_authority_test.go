package artifacts_test

import (
	"context"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"testing"
	"time"
)

type authorizedSource struct {
	saved       artifacts.SourceAuthority
	extraAction string
}

func (*authorizedSource) Check(context.Context, *wire.ContentSource, string, string, string, int64) error {
	return artifacts.Error("PERMISSION_DENIED")
}
func (s *authorizedSource) CheckAuthorized(_ context.Context, view artifacts.SourceAuthority, b artifacts.Binding, _ *wire.ContentSource, action, purpose, location string, _ int64) error {
	s.saved = view
	if s.extraAction != "" {
		action = s.extraAction
	}
	_, e := view.Authorize(b.Token, &wire.AuthorizationAction{Resource: "root", Action: "content." + action, Purpose: purpose, Location: location})
	return e
}
func TestSourceUsesScopedAuthorityWithoutReopeningTransaction(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	policy := &authorizedSource{}
	service, e := artifacts.New(h.auth, h.blobs, policy, artifacts.Config{Inline: 16, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if e != nil {
		t.Fatal(e)
	}
	op, e := h.auth.NewOperation(ctx, h.token)
	if e != nil {
		t.Fatal(e)
	}
	request := &wire.ContentRequest{Method: "PUT", OperationId: op, Data: []byte("hello"), Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: "research", Sources: []*wire.ContentSource{{Kind: "memory", Key: "source", Revision: 1}}, AcquiredAt: h.now.Unix(), MediaType: "text/plain", Size: 5, Sha256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", RetainUntil: h.now.Add(time.Minute).Unix()}}
	out, e := service.Call(ctx, h.binding, request)
	if e != nil {
		t.Fatalf("shared authority %v", e)
	}
	if policy.saved == nil {
		t.Fatal("source view not supplied")
	}
	if _, e = policy.saved.Identity(h.token); e == nil {
		t.Fatal("view escaped its callback")
	}
	policy.extraAction = "ungranted"
	if _, e = service.Call(ctx, h.binding, &wire.ContentRequest{Method: "READ", Ref: out.Record.Ref, Purpose: "research", Limit: 1}); e == nil {
		t.Fatal("source gained ungranted action")
	}
}
