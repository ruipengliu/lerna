package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 该纯Tx边界读取真实DevIdentity拥有的凭据记录；Initialize/Revoke均走其真实实现。
// 夹具不访问Task私表，也不替代Task的准入或费用算法。
type identityBoundary struct{}

func (identityBoundary) Authorize(context.Context, runtime.Tx, runtime.Auth, string, []api.ContentRef, []api.ObjectRef) error {
	return nil
}
func (identityBoundary) Evidence(context.Context, runtime.Tx, api.Task, []api.ObjectRef, []api.ComponentRef) error {
	return api.E("unsupported", "no_evidence_in_identity_fixture")
}
func (identityBoundary) CheckSubjectTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth) error {
	var credential struct {
		Generation uint64   `json:"generation"`
		State      string   `json:"state"`
		Roles      []string `json:"roles"`
	}
	if _, err := tx.Get(ctx, "platform.credentials", auth.SubjectID, &credential); err != nil {
		return err
	}
	if credential.State != "active" || credential.Generation != auth.CredentialGeneration || !api.Equal(credential.Roles, auth.Roles) {
		return api.E("forbidden", "credential_revoked")
	}
	return nil
}

func TestRevokedOriginalSubmitterCannotPrepareOrAdmitNewWork(t *testing.T) {
	ctx := context.Background()
	proof := &localProofFixture{}
	h := newHarness(t, task.Ports{})
	h.auth.CredentialGeneration = 7
	h.auth.Roles = []string{"browser"}
	identity := &platform.DevIdentity{Store: h.store, OwnerID: h.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: h.auth, TokenHash: api.Hash([]byte("explicit-local-test-credential"))}}}
	if err := identity.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, Participants: []string{"task", "platform"}}, task.Ports{Gate: identityBoundary{}, ActionAuthorization: proof})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	h.dispatch.Registry = runtime.NewRegistry()
	if err = s.Register(h.dispatch.Registry); err != nil {
		t.Fatal(err)
	}
	proof.install(t, h.scope)
	current := h.submit(t)
	prepared := h.prepared(current, "0")
	if _, err = s.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatalf("frozen generation 7 and original roles were not accepted: %v", err)
	}
	if err = identity.Revoke(ctx, h.auth); err != nil {
		t.Fatal(err)
	}
	current, err = s.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(current, "0")); !api.IsCode(err, "forbidden") {
		t.Fatalf("revoked submitter prepared a new decision: %v", err)
	}
	action := preparedAction(h, "0", "original_safe_check")
	action.SafeRequirementCheck = true
	out, err := s.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: current.GoalRef, Actions: []task.PreparedAction{action}}, nil)
	if err != nil || out.Outcome != "stale" || len(out.AdmittedOperationIDs) != 0 {
		t.Fatalf("old prepared decision bypassed revoked original actor: %+v %v", out, err)
	}
	facts, err := s.ContextFacts(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil || len(facts.Operations) != 0 {
		t.Fatalf("revocation leaked an operation: %+v %v", facts, err)
	}
	current, err = s.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.dispatch.Command(ctx, h.trusted(), api.Raw(h.command("task.cancel", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "trusted negative control after revocation"})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("revocation blocked negative control: %+v %v", r, err)
	}
}
