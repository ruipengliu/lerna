package artifacts_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"testing"
	"time"
)

// The content authority must retain explicit purpose/location and an isolated
// durable partition without replacing the existing task runtime partition.
func TestContentTransactionUsesExplicitScope(t *testing.T) {
	ctx := context.Background()
	st, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	a, err := authorization.New(st, authorization.SystemClock{}, authorization.Config{CredentialTTL: time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	token, err := a.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	err = a.UpdateContent(ctx, func(tx authorization.ContentTransaction) error {
		_, err := tx.Authorize(token, &wire.AuthorizationAction{Resource: "root", Action: "content.read", Purpose: "research", Location: "device"})
		return err
	})
	if !authorization.Is(err, authorization.Denied) {
		t.Fatalf("administrator bypassed content policy: %v", err)
	}
}

func TestPublishedContentSurvivesReopenAndReplays(t *testing.T) {
	h := newHarness(t)
	op, err := h.auth.NewOperation(context.Background(), h.token)
	if err != nil {
		t.Fatal(err)
	}
	in := &wire.ContentRequest{Method: "PUT", OperationId: op, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "research", Sources: []*wire.ContentSource{{Kind: "input", Key: "source", Revision: 1}}, AcquiredAt: h.now.Unix(), MediaType: "text/plain", Size: 5, Sha256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", RetainUntil: h.now.Add(time.Minute).Unix()}, Data: []byte("hello")}
	first, err := h.content.Call(context.Background(), h.binding, in)
	if err != nil {
		t.Fatal(err)
	}
	h.reopen(t)
	again, err := h.content.Call(context.Background(), h.binding, in)
	if err != nil || !proto.Equal(first.Record.Ref, again.GetRecord().GetRef()) {
		t.Fatalf("replay: %v %v", again, err)
	}
	out, err := h.content.Call(context.Background(), h.binding, &wire.ContentRequest{Method: "READ", Ref: first.Record.Ref, Purpose: "research", Limit: 5})
	if err != nil || string(out.GetData()) != "hello" {
		t.Fatalf("read: %v %v", out, err)
	}
}

func TestReplayAfterCleanupDoesNotRevealDeletedMetadata(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	op, _ := h.auth.NewOperation(ctx, h.token)
	in := &wire.ContentRequest{Method: "PUT", OperationId: op, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "research", Sources: []*wire.ContentSource{{Kind: "input", Key: "source", Revision: 1}}, AcquiredAt: h.now.Unix(), MediaType: "text/private-sensitive", Size: 5, Sha256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824", RetainUntil: h.now.Add(time.Minute).Unix()}, Data: []byte("hello")}
	first, err := h.content.Call(ctx, h.binding, in)
	if err != nil {
		t.Fatal(err)
	}
	del, _ := h.auth.NewOperation(ctx, h.token)
	_, err = h.content.Call(ctx, h.binding, &wire.ContentRequest{Method: "DELETE", OperationId: del, Ref: first.Record.Ref, ExpectedRevision: 1, Purpose: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.content.Clean(ctx); err != nil {
		t.Fatal(err)
	}
	out, err := h.content.Call(ctx, h.binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: op, Purpose: "research"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Record.State != "cleaned" || out.Record.Spec.MediaType != "" || out.Record.Spec.Sha256 != "" {
		t.Fatalf("deleted metadata remains: %v", out.Record)
	}
}
