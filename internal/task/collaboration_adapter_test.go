package task_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 只注入发送和第一次查询答复丢失；所有接纳及查询均由真实Dispatcher裁决。
type lostSessionDelivery struct {
	dispatch  *runtime.Dispatcher
	loseSend  bool
	loseQuery bool
	sent      api.Command
}

func (p *lostSessionDelivery) Send(ctx context.Context, scope runtime.Scope, auth runtime.Auth, c api.Command) (api.Receipt, error) {
	if p.sent.CommandID != "" && !api.Equal(p.sent, c) {
		return api.Receipt{}, api.E("idempotency_conflict", "session_input_changed")
	}
	p.sent = c
	r, err := p.dispatch.Command(ctx, auth, api.Raw(c))
	if p.loseSend {
		p.loseSend = false
		return api.Receipt{}, api.E("dependency_unavailable", "injected_session_reply_loss")
	}
	return r, err
}
func (p *lostSessionDelivery) Lookup(ctx context.Context, scope runtime.Scope, auth runtime.Auth, owner, id string) (api.Receipt, error) {
	r, err := p.dispatch.Lookup(ctx, auth, id)
	if p.loseQuery && err == nil {
		p.loseQuery = false
		return api.Receipt{}, api.E("dependency_unavailable", "injected_lookup_reply_loss")
	}
	return r, err
}

func TestLocalCollaborationRestoresOriginalActualSessionCommand(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, task.Ports{})
	h.auth.Roles = []string{"browser"}
	identity := &platform.DevIdentity{Store: h.store, OwnerID: h.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: h.auth, TokenHash: api.Hash([]byte("explicit-collaboration-fixture-identity"))}}}
	if err := identity.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	is, err := interaction.New(interaction.Config{}, interaction.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if err = is.Register(registry); err != nil {
		t.Fatal(err)
	}
	h.dispatch.Registry = registry
	delivery := &lostSessionDelivery{dispatch: h.dispatch, loseSend: true, loseQuery: true}
	adapter, err := collaboration.New(collaboration.Config{Store: h.store, Registry: registry, OwnerID: h.scope.OwnerID, Auth: h.trusted(), SubjectGate: identityBoundary{}, Participants: []string{"collaboration", "platform"}, Delivery: delivery})
	if err != nil {
		t.Fatal(err)
	}
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, Participants: []string{"task", "platform"}}, task.Ports{Collaboration: adapter, Gate: identityBoundary{}})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	if err = adapter.BindTask(s); err != nil {
		t.Fatal(err)
	}
	if err = s.Register(registry); err != nil {
		t.Fatal(err)
	}
	id := api.NewID("child")
	original := h.command("child.create", id, nil, task.ChildCreateInput{ChildID: id, SessionOwnerID: h.scope.OwnerID, SessionConfigRef: h.policy.PolicyRef, AgentBindingRef: h.scope.Ref(api.NewID("binding"), 1), InstallLockRef: h.policy.PolicyRef, AccessScopeRef: h.content("explicit same owner access scope"), PrepareDeadline: api.Time(time.Now().Add(10 * time.Minute))})
	r, err := h.dispatch.Command(ctx, h.auth, api.Raw(original))
	if err != nil || r.Stage != "accepted" {
		t.Fatalf("prepare %+v %v", r, err)
	}
	drainKind(t, h, task.JobChildPrepare)
	handle, err := s.ChildRead(ctx, h.store, h.scope, h.auth, id)
	if err != nil || handle.State != "preparing" {
		t.Fatalf("lost reply guessed session open: %+v %v", handle, err)
	}
	if delivery.sent.CommandID != handle.SessionCommandRef.ObjectID {
		t.Fatal("session used a replacement command identity")
	}
	var in interaction.CreateSessionInput
	if err = api.Decode(delivery.sent.Payload, &in); err != nil {
		t.Fatal(err)
	}
	actual, err := is.ReadSession(ctx, h.store, h.scope, h.auth, in.SessionID, interaction.ReadInput{})
	if err != nil || actual.Session.State != "open" {
		t.Fatalf("real Session was not durably accepted: %+v %v", actual, err)
	}
	time.Sleep(1050 * time.Millisecond)
	drainKind(t, h, task.JobChildPrepare)
	handle, err = s.ChildRead(ctx, h.store, h.scope, h.auth, id)
	if err != nil || handle.State != "open" || handle.ChildSessionRef == nil || handle.ChildSessionRef.ObjectID != actual.Session.SessionID {
		t.Fatalf("recovery changed original Session: %+v %v", handle, err)
	}
	r, err = h.dispatch.Lookup(ctx, h.auth, original.CommandID)
	if err != nil || r.Stage != "applied" {
		t.Fatalf("original child command not applied: %+v %v", r, err)
	}
	remote := api.NewID("owner")
	inCreate := task.ChildCreateInput{ChildID: api.NewID("child"), SessionOwnerID: remote, SessionConfigRef: h.policy.PolicyRef, AgentBindingRef: h.scope.Ref(api.NewID("binding"), 1), InstallLockRef: h.policy.PolicyRef, AccessScopeRef: h.content("unconfigured remote access"), PrepareDeadline: original.ExpiresAt}
	r, err = h.dispatch.Command(ctx, h.auth, api.Raw(h.command("child.create", inCreate.ChildID, nil, inCreate)))
	if err != nil || r.Stage != "rejected" || r.Error.Code != "unsupported" {
		t.Fatalf("unconfigured remote created responsibility: %+v %v", r, err)
	}
}

type authenticatedEvidenceGate struct{ *evidenceBridge }

func (authenticatedEvidenceGate) CheckSubjectTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth) error {
	return identityBoundary{}.CheckSubjectTx(ctx, tx, auth)
}

func TestLocalCollaborationTransferWaitsForOriginalTaskSteerConsumption(t *testing.T) {
	ctx := context.Background()
	rule := fixtureRule()
	h := newHarness(t, task.Ports{})
	h.auth.Roles = []string{"browser"}
	identity := &platform.DevIdentity{Store: h.store, OwnerID: h.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: h.auth, TokenHash: api.Hash([]byte("explicit-transfer-fixture-identity"))}}}
	if err := identity.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	is, err := interaction.New(interaction.Config{}, interaction.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if err = is.Register(registry); err != nil {
		t.Fatal(err)
	}
	h.dispatch.Registry = registry
	adapter, err := collaboration.New(collaboration.Config{Store: h.store, Registry: registry, OwnerID: h.scope.OwnerID, Auth: h.trusted(), SubjectGate: identityBoundary{}, Participants: []string{"collaboration", "task", "platform"}})
	if err != nil {
		t.Fatal(err)
	}
	content := &contentBridge{}
	configureContent(t, h, content)
	gate := &evidenceBridge{service: governance.New(h.store, governance.Options{}), rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	answerSchema := api.Object(map[string]any{"path": api.String()}, "path")
	answerDigest, _ := api.Digest(answerSchema)
	answerRef := api.ComponentRef{ComponentID: api.NewID("schema"), Version: "1.0.0", Digest: answerDigest}
	s, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, Rules: []api.RuleDefinition{rule}, AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: answerRef, Schema: answerSchema}}, Participants: []string{"task", "governance", "platform"}}, task.Ports{Content: content, Gate: authenticatedEvidenceGate{gate}, Collaboration: adapter})
	if err != nil {
		t.Fatal(err)
	}
	h.service = s
	if err = adapter.BindTask(s); err != nil {
		t.Fatal(err)
	}
	if err = s.Register(registry); err != nil {
		t.Fatal(err)
	}
	parent := readyTask(t, h, rule)
	childID, binding := api.NewID("child"), h.scope.Ref(api.NewID("binding"), 1)
	r, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("child.create", childID, nil, task.ChildCreateInput{ChildID: childID, SessionOwnerID: h.scope.OwnerID, SessionConfigRef: h.policy.PolicyRef, AgentBindingRef: binding, InstallLockRef: h.policy.PolicyRef, AccessScopeRef: h.content("allowed internal goal scope"), PrepareDeadline: api.Time(time.Now().Add(10 * time.Minute))})))
	if err != nil || r.Stage != "accepted" {
		t.Fatalf("prepare handle: %+v %v", r, err)
	}
	drainKind(t, h, task.JobChildPrepare)
	handle, err := s.ChildRead(ctx, h.store, h.scope, h.auth, childID)
	if err != nil {
		t.Fatal(err)
	}
	delegation := task.DelegateInput{DelegationID: api.NewID("delegation"), ParentTaskRef: h.scope.Ref(parent.TaskID, parent.Revision), ParentGoalRevision: parent.GoalRevision, GoalRef: h.content("original internal child goal"), InputRefs: []api.ContentRef{}, AgentBindingRef: binding, PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "3"}}, Deadline: parent.Deadline, PolicyRef: h.policy.PolicyRef, ReceiverID: h.scope.OwnerID, Internal: true}
	r, err = h.dispatch.Command(ctx, h.auth, api.Raw(h.command("child.send", childID, &handle.Revision, task.ChildSendInput{ChildID: childID, Mode: "new_goal", Delegation: &delegation})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("internal goal: %+v %v", r, err)
	}
	handle, err = s.ChildRead(ctx, h.store, h.scope, h.auth, childID)
	if err != nil {
		t.Fatal(err)
	}
	d, err := s.DelegationRead(ctx, h.store, h.scope, h.auth, delegation.DelegationID)
	if err != nil || d.ChildTaskRef == nil {
		t.Fatalf("original internal mapping: %+v %v", d, err)
	}
	child, err := s.Read(ctx, h.store, h.scope, h.auth, d.ChildTaskRef.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	amendment, err := content.Publish(ctx, h.scope, api.NewID("upload"), "text/plain", []byte("literal same owner additional criterion"))
	if err != nil {
		t.Fatal(err)
	}
	r, err = h.dispatch.Command(ctx, h.auth, api.Raw(h.command("child.send", childID, &handle.Revision, task.ChildSendInput{ChildID: childID, Mode: "continue_existing", ExpectedActiveDelegationRef: handle.ActiveDelegationRef, DelegationRef: handle.ActiveDelegationRef, ParentGoalRevision: parent.GoalRevision, Kind: "steer", ChildGoalRevision: child.GoalRevision, ContentRef: &amendment})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("accept transfer: %+v %v", r, err)
	}
	drainKind(t, h, task.JobChildTransfer)
	before, err := s.Read(ctx, h.store, h.scope, h.auth, child.TaskID)
	if err != nil || before.GoalRevision != child.GoalRevision {
		t.Fatalf("accepted transfer invented consumed steer: %+v %v", before, err)
	}
	drainKind(t, h, task.JobSteer)
	time.Sleep(1050 * time.Millisecond)
	drainKind(t, h, task.JobChildTransfer)
	after, err := s.Read(ctx, h.store, h.scope, h.auth, child.TaskID)
	if err != nil || after.GoalRevision != child.GoalRevision+1 {
		t.Fatalf("original steer did not consume exactly once: %+v %v", after, err)
	}
	body, err := content.Read(ctx, h.scope, h.auth, after.GoalRef)
	if err != nil {
		t.Fatal(err)
	}
	var document api.GoalDocument
	if err = api.Decode(body, &document); err != nil || len(document.AmendmentRefs) != 1 || !api.Equal(document.AmendmentRefs[0], amendment) || !api.Equal(document.InitialGoalRef, child.GoalRef) {
		t.Fatalf("transferred original amendment was lost: %+v %v", document, err)
	}
	request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: h.scope.Ref(after.TaskID, after.Revision), GoalRevision: &after.GoalRevision, Purpose: "clarify_goal", QuestionRef: h.content("provide an exact revised path"), AnswerSchemaRef: answerRef, PreviewRefs: []api.ContentRef{after.GoalRef}, ExpiresAt: api.Time(time.Now().Add(10 * time.Minute)), State: "pending"}
	var requestRef api.ObjectRef
	status, err := h.store.Within(ctx, h.scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		var err error
		requestRef, err = s.CreateInputTx(ctx, tx, h.trusted(), after.TaskID, request)
		return err
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("original child request: %s %v", status, err)
	}
	badAnswer, err := content.Publish(ctx, h.scope, api.NewID("upload"), "application/json", []byte(`{"path":"/bad","unexpected":true}`))
	if err != nil {
		t.Fatal(err)
	}
	handle, err = s.ChildRead(ctx, h.store, h.scope, h.auth, childID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = h.dispatch.Command(ctx, h.auth, api.Raw(h.command("child.send", childID, &handle.Revision, task.ChildSendInput{ChildID: childID, Mode: "continue_existing", ExpectedActiveDelegationRef: handle.ActiveDelegationRef, DelegationRef: handle.ActiveDelegationRef, ParentGoalRevision: parent.GoalRevision, Kind: "answer_request", RequestRef: &requestRef, AnswerRef: &badAnswer})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("bad answer transfer enqueue: %+v %v", r, err)
	}
	drainKind(t, h, task.JobChildTransfer)
	drainKind(t, h, task.JobInput)
	time.Sleep(1050 * time.Millisecond)
	drainKind(t, h, task.JobChildTransfer)
	badView, err := s.InputRequestRead(ctx, h.store, h.scope, h.auth, requestRef.ObjectID, 0)
	if err != nil || badView.Request.State != "pending" {
		t.Fatalf("rejected original input consumed request: %+v %v", badView, err)
	}
	answer, err := content.Publish(ctx, h.scope, api.NewID("upload"), "application/json", []byte(`{"path":"/updated-report.md"}`))
	if err != nil {
		t.Fatal(err)
	}
	handle, err = s.ChildRead(ctx, h.store, h.scope, h.auth, childID)
	if err != nil {
		t.Fatal(err)
	}
	r, err = h.dispatch.Command(ctx, h.auth, api.Raw(h.command("child.send", childID, &handle.Revision, task.ChildSendInput{ChildID: childID, Mode: "continue_existing", ExpectedActiveDelegationRef: handle.ActiveDelegationRef, DelegationRef: handle.ActiveDelegationRef, ParentGoalRevision: parent.GoalRevision, Kind: "answer_request", RequestRef: &requestRef, AnswerRef: &answer})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("answer transfer: %+v %v", r, err)
	}
	drainKind(t, h, task.JobChildTransfer)
	drainKind(t, h, task.JobInput)
	time.Sleep(1050 * time.Millisecond)
	drainKind(t, h, task.JobChildTransfer)
	view, err := s.InputRequestRead(ctx, h.store, h.scope, h.auth, requestRef.ObjectID, 0)
	if err != nil || view.Request.State != "answered" || view.Request.ConsumedBy == "" {
		t.Fatalf("transferred answer did not consume original child request: %+v %v", view, err)
	}
	final, err := s.Read(ctx, h.store, h.scope, h.auth, after.TaskID)
	if err != nil || final.GoalRevision != after.GoalRevision+1 {
		t.Fatalf("original answer transfer created an extra task or goal: %+v %v", final, err)
	}
}
