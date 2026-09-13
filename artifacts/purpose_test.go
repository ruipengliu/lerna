package artifacts_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

type purposeSources struct{}

func (purposeSources) Check(_ context.Context, s *wire.ContentSource, _, purpose, location string, _ int64) error {
	if s.Kind != "input" || s.Key != "source" || s.Revision != 1 || (purpose != "assist" && purpose != "research") || location != "device" {
		return artifacts.Error("PERMISSION_DENIED")
	}
	return nil
}

func TestPurposeWithdrawalPermanentlyInvalidatesOnlyOriginalArtifacts(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"content.store", "content.process", "content.discover", "content.disclose", "content.retain", "content.delete"}, Purposes: []string{"assist", "research"}, Locations: []string{"device"}, ExpiresUnix: h.now.Add(time.Hour).Unix()}
	apply := func(command *wire.AuthorizationCommand) {
		t.Helper()
		view, err := h.auth.GetPolicy(ctx, h.token)
		if err != nil {
			t.Fatal(err)
		}
		op, err := h.auth.NewOperation(ctx, h.token)
		if err != nil {
			t.Fatal(err)
		}
		command.ExpectedRevision = view.Revision
		if _, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); err != nil {
			t.Fatal(err)
		}
	}
	policy := func(s *wire.AuthorizationScope) {
		apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "content", Scope: s}}}}})
	}
	policy(scope)
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "extended", Subject: "admin", Mode: "continuous", Scope: scope}}})
	open := func() *artifacts.Service {
		service, err := artifacts.New(h.auth, h.blobs, purposeSources{}, artifacts.Config{Inline: 16, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	content := open()
	put := func(purpose string) (*wire.ContentRef, *wire.ContentRequest) {
		body := []byte("public retained body for " + purpose)
		hash := sha256.Sum256(body)
		op, err := h.auth.NewOperation(ctx, h.token)
		if err != nil {
			t.Fatal(err)
		}
		in := &wire.ContentRequest{Method: "PUT", OperationId: op, Data: body, Spec: &wire.ContentSpec{Kind: "artifact", Resource: "root", Purpose: purpose, Sources: []*wire.ContentSource{{Kind: "input", Key: "source", Revision: 1}}, AcquiredAt: h.now.Unix(), MediaType: "text/plain", Size: uint64(len(body)), Sha256: hex.EncodeToString(hash[:]), RetainUntil: h.now.Add(time.Minute).Unix()}}
		out, err := content.Call(ctx, h.binding, in)
		if err != nil {
			t.Fatal(err)
		}
		return out.Record.Ref, in
	}
	victim, original := put("assist")
	kept, _ := put("research")
	narrow := proto.Clone(scope).(*wire.AuthorizationScope)
	narrow.Purposes = []string{"research"}
	policy(narrow)
	policy(scope)
	h.reopen(t)
	content = open()
	read := func(ref *wire.ContentRef, purpose string) error {
		_, err := content.Call(ctx, h.binding, &wire.ContentRequest{Method: "READ", Ref: ref, Purpose: purpose, Limit: 1})
		return err
	}
	if err := read(victim, "assist"); err == nil {
		t.Fatal("restored policy resurrected original artifact")
	}
	if err := read(kept, "research"); err != nil {
		t.Fatalf("unrelated purpose invalidated: %v", err)
	}
	if err := content.Clean(ctx); err != nil {
		t.Fatal(err)
	}
	got, err := content.Call(ctx, h.binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: original.OperationId, Purpose: "assist"})
	if err != nil || got.Record.State != "cleaned" {
		t.Fatalf("policy cleanup: %+v %v", got, err)
	}
	if _, err := content.Call(ctx, h.binding, original); err == nil {
		t.Fatal("old write accepted after cleanup")
	}
	fresh, _ := put("assist")
	if err := read(fresh, "assist"); err != nil {
		t.Fatalf("new authorized object blocked by old invalidation: %v", err)
	}
}
