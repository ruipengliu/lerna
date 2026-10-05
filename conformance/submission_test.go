package conformance_test

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G3、G12
func TestCommandIdentityRejectsConflictsAndKeepsOriginalDecision(t *testing.T) {
	ctx := context.Background()
	h, err := assembly.Open(filepath.Join(t.TempDir(), "lerna.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	caller := &v1.Caller{UserId: "alice", IssuerId: "a:b"}
	cmd := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "a:b", TargetDomainId: "local", CommandId: "c"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "one"}
	if _, err := h.Sessions.SubmitGoal(ctx, caller, cmd); err != nil {
		t.Fatal(err)
	}
	if err := h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	original, err := h.Durable.QueryReceipt(ctx, caller, cmd.Identity)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Credential = "rotated"
	cmd.TraceId = "new-trace"
	duplicate, err := h.Sessions.SubmitGoal(ctx, caller, cmd)
	if err != nil {
		t.Fatal(err)
	}
	if duplicate.TaskRef.Name.LocalId != original.Receipt.TaskRef.Name.LocalId || duplicate.CommitPosition != original.Receipt.CommitPosition {
		t.Fatal("retry changed original decision")
	}
	cmd.Goal = "two"
	if _, err := h.Sessions.SubmitGoal(ctx, caller, cmd); err == nil || err.Error() != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("wanted conflict: %v", err)
	}
	caller.IssuerId = "a"
	cmd.Identity.IssuerId = "a"
	cmd.Identity.CommandId = "b:c"
	if _, err := h.Sessions.SubmitGoal(ctx, caller, cmd); err != nil {
		t.Fatalf("different issuer is independent: %v", err)
	}
	if err := h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	second, err := h.Durable.QueryReceipt(ctx, caller, cmd.Identity)
	if err != nil {
		t.Fatal(err)
	}
	if second.Receipt.TaskRef.Name.LocalId == original.Receipt.TaskRef.Name.LocalId {
		t.Fatal("different command shared task")
	}
}

// 规则：G3、G12
func TestReceiptQueryDistinguishesNotFoundUnavailableAndUnauthenticated(t *testing.T) {
	ctx := context.Background()
	h, err := assembly.Open(filepath.Join(t.TempDir(), "query.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	caller := &v1.Caller{UserId: "alice", IssuerId: "cli"}
	identity := &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "unseen"}
	q, err := h.Durable.QueryReceipt(ctx, caller, identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
		t.Fatalf("not found: %v %v", q, err)
	}
	caller.UserId = "mallory"
	if _, err := h.Durable.QueryReceipt(ctx, caller, identity); err == nil || err.Error() != "PERMISSION_DENIED" {
		t.Fatalf("cross user query: %v", err)
	}
	caller.UserId = "alice"
	caller.IssuerId = "different"
	if _, err := h.Durable.QueryReceipt(ctx, caller, identity); err == nil || err.Error() != "PERMISSION_DENIED" {
		t.Fatalf("issuer spoofing: %v", err)
	}
	caller.IssuerId = "cli"
	if err := h.Close(); err != nil {
		t.Fatal(err)
	}
	q, err = h.Durable.QueryReceipt(ctx, caller, identity)
	if err != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_UNAVAILABLE || q.Error.CommandAcceptance != v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN {
		t.Fatalf("unavailable must remain unknown: %v %v", q, err)
	}
}

// 规则：G3
func TestRejectedDecisionDoesNotAppendInputAndSurvivesRetry(t *testing.T) {
	ctx := context.Background()
	h, err := assembly.Open(filepath.Join(t.TempDir(), "reject.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	caller := &v1.Caller{UserId: "alice", IssuerId: "cli"}
	cmd := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "one"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "first"}
	if _, err := h.Sessions.SubmitGoal(ctx, caller, cmd); err != nil {
		t.Fatal(err)
	}
	if err := h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	q, err := h.Durable.QueryReceipt(ctx, caller, cmd.Identity)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Session = q.Receipt.SessionRef.Name
	cmd.Identity.CommandId = "stale"
	zero := uint64(0)
	cmd.ExpectedRevision = &zero
	if _, err := h.Sessions.SubmitGoal(ctx, caller, cmd); err != nil {
		t.Fatal(err)
	}
	if err := h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	rejected, err := h.Durable.QueryReceipt(ctx, caller, cmd.Identity)
	if err != nil || rejected.Receipt.Decision != v1.Decision_DECISION_REJECTED || rejected.Receipt.Error.Code != "REVISION_CONFLICT" {
		t.Fatalf("expected rejection: %v %v", rejected, err)
	}
	session, err := h.Sessions.QuerySession(ctx, caller, cmd.Session)
	if err != nil || session.LastCommittedSeq != 1 || len(session.TaskRefs) != 1 {
		t.Fatalf("rejection mutated business facts: %v %v", session, err)
	}
	retry, err := h.Sessions.SubmitGoal(ctx, caller, cmd)
	if err != nil || retry.CommitPosition != rejected.Receipt.CommitPosition {
		t.Fatalf("rejection not immutable: %v %v", retry, err)
	}
	cmd.Identity.CommandId = "second"
	one := uint64(1)
	cmd.ExpectedRevision = &one
	if _, err := h.Sessions.SubmitGoal(ctx, caller, cmd); err != nil {
		t.Fatal(err)
	}
	if err := h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	session, err = h.Sessions.QuerySession(ctx, caller, cmd.Session)
	if err != nil || session.LastCommittedSeq != 2 || len(session.TaskRefs) != 2 {
		t.Fatalf("next valid input: %v %v", session, err)
	}
}

// 规则：G3
func TestConcurrentRetriesHaveOnePendingJobAndOneTask(t *testing.T) {
	ctx := context.Background()
	h, err := assembly.Open(filepath.Join(t.TempDir(), "concurrent.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	caller := &v1.Caller{UserId: "alice", IssuerId: "cli"}
	cmd := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "same"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "exactly one"}
	errors := make(chan error, 8)
	for i := 0; i < 8; i++ {
		go func() { _, err := h.Sessions.SubmitGoal(ctx, caller, cmd); errors <- err }()
	}
	for i := 0; i < 8; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	jobs, err := h.Durable.Pending(ctx, caller)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("duplicate jobs: %v %v", jobs, err)
	}
	for i := 0; i < 8; i++ {
		go func() { errors <- h.Sessions.ProcessPending(ctx, caller) }()
	}
	for i := 0; i < 8; i++ {
		if err := <-errors; err != nil {
			t.Fatal(err)
		}
	}
	q, err := h.Durable.QueryReceipt(ctx, caller, cmd.Identity)
	if err != nil {
		t.Fatal(err)
	}
	session, err := h.Sessions.QuerySession(ctx, caller, q.Receipt.SessionRef.Name)
	if err != nil || len(session.TaskRefs) != 1 || session.LastCommittedSeq != 1 {
		t.Fatalf("duplicate tasks: %v %v", session, err)
	}
}
