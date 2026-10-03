package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 夹具只登记本地预授权；真实Grant使用由装配桥接另行验证。
// seal使用实际ES256并将准确JWS与出版意图存入同一SQLite事务。
type localProofFixture struct{ keys *platform.Keyring }
type sealedSource struct {
	Claims             platform.ProofClaims `json:"claims"`
	JWS                string               `json:"jws"`
	PublicationPending bool                 `json:"publication_pending"`
}

func (f *localProofFixture) install(t *testing.T, scope runtime.Scope) {
	t.Helper()
	var err error
	f.keys, err = platform.NewDevelopmentKey(scope.TenantID, scope.OwnerID, []string{"control", "task_closure"})
	if err != nil {
		t.Fatal(err)
	}
}
func (f *localProofFixture) seal(ctx context.Context, tx runtime.Tx, claims platform.ProofClaims) (api.ContentRef, error) {
	signed, err := f.keys.Sign("development-es256", claims)
	if err != nil {
		return api.ContentRef{}, err
	}
	ref := api.ContentRef{TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(signed)), MediaType: "application/jose", ByteLength: uint64(len(signed))}
	err = tx.Create(ctx, "task.test_source_proofs", ref.ContentID, claims.ObjectRef.ObjectID, sealedSource{claims, signed, true})
	return ref, err
}
func (f *localProofFixture) SealControl(ctx context.Context, tx runtime.Tx, control api.ControlSnapshot) (api.ContentRef, error) {
	digest, err := api.Digest(control)
	if err != nil {
		return api.ContentRef{}, err
	}
	return f.seal(ctx, tx, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: tx.Scope().OwnerID, Purpose: "control", ObjectRef: tx.Scope().Ref(control.TaskID, control.GoalRevision), Digest: digest, ControlRevision: control.ControlRevision, WindowID: control.WindowID, IssuedAt: control.IssuedAt, StartBefore: control.StartBefore})
}
func (f *localProofFixture) SealClosureTx(ctx context.Context, tx runtime.Tx, view task.ClosureView) (api.ContentRef, error) {
	digest, err := api.Digest(view)
	if err != nil {
		return api.ContentRef{}, err
	}
	issued, err := api.ParseTime(view.IssuedAt)
	if err != nil {
		return api.ContentRef{}, err
	}
	return f.seal(ctx, tx, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: tx.Scope().OwnerID, Purpose: "task_closure", ObjectRef: view.TaskRef, Digest: digest, IssuedAt: view.IssuedAt, StartBefore: api.Time(issued.Add(time.Minute))})
}
func (f *localProofFixture) AuthorizeAction(ctx context.Context, tx runtime.Tx, auth runtime.Auth, intent task.OperationIntent) error {
	if !auth.HasRole("service") {
		return api.E("forbidden", "fixture_prepared_action_required")
	}
	return tx.Create(ctx, "task.test_authorized_actions", intent.OperationID, intent.TaskRef.ObjectID, struct {
		IntentHash string `json:"intent_hash"`
	}{intent.IntentHash})
}

func TestClosureRequiresExplicitSourceProofSealer(t *testing.T) {
	h := newHarness(t, task.Ports{})
	current := h.submit(t)
	_, err := h.service.Closure(context.Background(), h.store, h.scope, h.auth, h.scope.Ref(current.TaskID, current.Revision))
	if !api.IsCode(err, "unsupported") {
		t.Fatalf("goal text accepted as closure proof: %v", err)
	}
}

func TestControlWindowHasSignedSourceAndRejectsMissingSealer(t *testing.T) {
	rule := fixtureRule()
	gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	proof := &localProofFixture{}
	h := newHarness(t, task.Ports{Gate: gate, ControlProof: proof, ClosureProof: proof, ActionAuthorization: proof}, rule)
	proof.install(t, h.scope)
	gate.service = governance.New(h.store, governance.Options{})
	current := readyTask(t, h, rule)
	prepared := h.prepared(current, "0")
	prepared.Snapshot.Purpose = "decide"
	prepared.Snapshot.CoverageRef = current.CurrentCoverageRef
	if _, err := h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	a := preparedAction(h, "1", "signed_test")
	consumption, err := h.service.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prepared.DecisionID, Kind: "act", ReasonRef: current.GoalRef, Actions: []task.PreparedAction{a}}, nil)
	if err != nil || consumption.Outcome != "adopted" {
		t.Fatalf("admission %+v %v", consumption, err)
	}
	current, err = h.service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	in := task.ControlWindowInput{TaskID: current.TaskID, ExpectedGoalRevision: current.GoalRevision, ExpectedControlRevision: current.ControlRevision, ReceiverID: h.scope.OwnerID, OperationRef: h.scope.Ref(a.OperationID, 1)}
	c := h.command("task.control_window", current.TaskID, nil, in)
	r, err := h.dispatch.Command(context.Background(), h.trusted(), api.Raw(c))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("control window %+v %v", r, err)
	}
	var window api.ControlSnapshot
	if err = api.Decode(r.Output, &window); err != nil {
		t.Fatal(err)
	}
	if api.Equal(window.ProofRef, current.GoalRef) {
		t.Fatal("goal text used as source proof")
	}
	var original sealedSource
	if _, err = h.store.Read(context.Background(), h.scope, "task.test_source_proofs", window.ProofRef.ContentID, 1, &original); err != nil {
		t.Fatal(err)
	}
	if _, err = proof.keys.Verify(original.JWS, original.Claims, time.Now()); err != nil {
		t.Fatal(err)
	}
	w := window
	w.ProofRef = api.ContentRef{}
	digest, _ := api.Digest(w)
	if digest != original.Claims.Digest || !original.PublicationPending {
		t.Fatal("window proof did not bind exact persisted control")
	}
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, Rules: []api.RuleDefinition{rule}}, task.Ports{Gate: gate, ActionAuthorization: proof})
	if err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	if err = s.Register(registry); err != nil {
		t.Fatal(err)
	}
	h.dispatch.Registry = registry
	r, err = h.dispatch.Command(context.Background(), h.trusted(), api.Raw(h.command("task.control_window", current.TaskID, nil, in)))
	if err != nil || r.Stage != "rejected" || r.Error.Code != "unsupported" {
		t.Fatalf("unsealed control opened: %+v %v", r, err)
	}
}
