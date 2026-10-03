package interaction_test

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type contentBridge struct{ m *memory.Service }

func (b contentBridge) CheckTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, ref api.ContentRef, purpose string) error {
	_, e := b.m.CheckContentTx(ctx, tx, a, ref, purpose, "local", false)
	return e
}
func (b contentBridge) Read(ctx context.Context, scope runtime.Scope, a runtime.Auth, ref api.ContentRef, purpose string) ([]byte, error) {
	return b.m.Read(ctx, scope, a, ref, purpose)
}

type deliveryBridge struct {
	d     *runtime.Dispatcher
	drop  bool
	sends int
}

func (b *deliveryBridge) Send(ctx context.Context, scope runtime.Scope, a runtime.Auth, c api.Command) (api.Receipt, error) {
	b.sends++
	r, e := b.d.Command(ctx, a, api.Raw(c))
	if e == nil && b.drop {
		b.drop = false
		return api.Receipt{}, api.E("dependency_unavailable", "injected_reply_loss")
	}
	return r, e
}
func (b *deliveryBridge) Lookup(ctx context.Context, scope runtime.Scope, a runtime.Auth, owner, id string) (api.Receipt, error) {
	if owner != b.d.OwnerID {
		return api.Receipt{}, api.E("forbidden", "owner_mismatch")
	}
	return b.d.Lookup(ctx, a, id)
}

type closureBridge struct {
	s     *task.Service
	store runtime.Store
}

func (b closureBridge) Closure(ctx context.Context, scope runtime.Scope, a runtime.Auth, ref api.ObjectRef) (interaction.Closure, error) {
	v, e := b.s.Closure(ctx, b.store, scope, a, ref)
	return interaction.Closure{TaskRef: v.TaskRef, GoalWorkClosed: v.GoalWorkClosed, EffectsClosed: v.EffectsClosed, ClosureRef: v.ProofRef}, e
}

type requestBridge struct{ s *task.Service }

func (b requestBridge) CheckTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, ref api.ObjectRef) (interaction.RequestView, error) {
	v, e := b.s.RequestViewTx(ctx, tx, a, ref)
	return interaction.RequestView{Request: v.Request, AnswerSchema: v.AnswerSchema, Method: "task.input"}, e
}

type applicationFixture struct {
	ctx             context.Context
	store           runtime.Store
	scope           runtime.Scope
	auth            runtime.Auth
	m               *memory.Service
	cp              memory.Policy
	task            *task.Service
	s               *interaction.Service
	d               *runtime.Dispatcher
	registry        *runtime.Registry
	delivery        *deliveryBridge
	config          api.ComponentRef
	policy          api.ComponentRef
	binding         api.ObjectRef
	session, branch string
}

func newApplication(t *testing.T) *applicationFixture {
	t.Helper()
	ctx := context.Background()
	var store runtime.Store
	var e error
	if os.Getenv("HARNESS_INTERACTION_STORE") == "postgres" {
		store, e = postgres.Open(ctx, os.Getenv("HARNESS_TEST_POSTGRES_DSN"), postgres.WithMaxConnections(8))
	} else {
		store, e = sqlite.Open(filepath.Join(t.TempDir(), "application.sqlite"))
	}
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { _ = store.Close() })
	if e = store.(interface{ Migrate(context.Context) error }).Migrate(ctx); e != nil {
		t.Fatal(e)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	a := runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1, Roles: []string{"content_admin", "memory_admin"}}
	objects, e := objectstore.OpenLocal(t.TempDir(), memory.MaxContentBytes)
	if e != nil {
		t.Fatal(e)
	}
	m := memory.New(store, objects)
	values := memory.PolicyValues{Subjects: []string{a.SubjectID}, Purposes: []string{"content.write", "task.goal", "interaction.input", "interaction.preview", "interaction.snapshot"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(time.Hour)), Continuous: true}
	digest, _ := api.Digest(values)
	cp, e := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
	if e != nil {
		t.Fatal(e)
	}
	if e = m.InstallPolicy(ctx, scope, a, cp); e != nil {
		t.Fatal(e)
	}
	policy := api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: api.Hash([]byte("application-task-policy"))}
	ts, e := task.New(task.Config{Policies: []task.TaskPolicy{{PolicyRef: policy, ContinuationLimit: 100, RepairLimit: 3, NoProgressLimit: 5, ContextRoundLimit: 3, SafeAttemptLimit: 2, MaxRequirements: 100, MaxDelegations: 128, MaxDepth: 4, CostMode: "strict", BudgetLimits: []api.Amount{{Unit: "USD", Value: "100"}}, MaxEvidenceStalenessSeconds: 300, MaxDurationSeconds: 3600}}, Participants: []string{"task", "content", "interaction"}}, task.Ports{})
	if e != nil {
		t.Fatal(e)
	}
	registry := runtime.NewRegistry()
	if e = ts.Register(registry); e != nil {
		t.Fatal(e)
	}
	m.Register(registry)
	d := &runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}
	delivery := &deliveryBridge{d: d}
	zone, e := os.ReadFile("/usr/share/zoneinfo/Etc/UTC")
	if e != nil {
		t.Fatal(e)
	}
	calendar, e := interaction.NewTZDB("2026b", map[string][]byte{"Etc/UTC": zone})
	if e != nil {
		t.Fatal(e)
	}
	sessionID, branchID := api.NewID("session"), api.NewID("branch")
	binding := scope.Ref(api.NewID("binding"), 1)
	one := uint64(1)
	s, e := interaction.New(interaction.Config{DiscoveryOwnerID: scope.OwnerID, Participants: []string{"interaction", "content", "task"}, EventBindings: []interaction.EventBinding{{BindingRef: binding, Events: []interaction.EventRule{{Name: "archive", Schema: api.Raw(api.Object(map[string]any{"reason": api.Schema{"type": "string", "minLength": 1, "maxLength": 200}}, "reason")), OwnerID: scope.OwnerID, Method: "session.archive", TargetID: sessionID, AcceptForSeconds: 60, ExpectedRevision: &one}}}}}, interaction.Ports{Content: contentBridge{m}, Delivery: delivery, Closure: closureBridge{ts, store}, Requests: requestBridge{ts}, Calendar: calendar, ScheduleGate: scheduleGateBridge{policy, policy}})
	if e != nil {
		t.Fatal(e)
	}
	if e = s.Register(registry); e != nil {
		t.Fatal(e)
	}
	f := &applicationFixture{ctx: ctx, store: store, scope: scope, auth: a, m: m, cp: cp, task: ts, s: s, d: d, registry: registry, delivery: delivery, policy: policy, config: policy, binding: binding, session: sessionID, branch: branchID}
	f.command(t, "session.create", scope.OwnerID, nil, interaction.CreateSessionInput{SessionID: f.session, DefaultBranchID: f.branch, ConfigRef: policy})
	return f
}
func (f *applicationFixture) command(t *testing.T, method, target string, revision *uint64, in any) api.Receipt {
	t.Helper()
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: target, ExpectedRevision: revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(in)}
	r, e := f.d.Command(f.ctx, f.auth, api.Raw(c))
	if e != nil {
		t.Fatal(e)
	}
	if r.Stage != "applied" {
		t.Fatalf("%s %+v", method, r)
	}
	return r
}
func (f *applicationFixture) upload(t *testing.T, body string) api.ContentRef {
	t.Helper()
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(body)), ByteLength: uint64(len(body)), MediaType: "text/plain"}
	r, e := f.m.Upload(f.ctx, f.scope, f.auth, memory.PublicationRequest{ContentRef: ref, TransferID: api.NewID("upload"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: f.cp.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(30 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(10 * time.Minute))}, []byte(body))
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func (f *applicationFixture) step(t *testing.T) {
	t.Helper()
	works, status, e := f.store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{interaction.JobDispatch}, 1, 30*time.Second)
	if e != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("claim %+v %s %v", works, status, e)
	}
	h, ok := f.registry.Job(interaction.JobDispatch)
	if !ok {
		t.Fatal("dispatch handler missing")
	}
	if e = h(f.ctx, f.store, f.scope, works[0]); e != nil {
		t.Fatal(e)
	}
}

func TestSavedGoalSurvivesBusinessReplyLossWithoutCreatingSecondTask(t *testing.T) {
	f := newApplication(t)
	goal := f.upload(t, "请生成可核验报告")
	r := f.command(t, "session.submit_goal", f.session, nil, interaction.GoalInput{SessionRef: f.scope.Ref(f.session, 1), BranchRef: f.scope.Ref(f.branch, 1), ExpectedBranchRevision: 1, ContentRef: goal, AttachmentRefs: []api.ContentRef{}, PolicyRef: f.policy, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, TaskDeadline: api.Time(time.Now().Add(20 * time.Minute))})
	var out interaction.SubmissionOutput
	if e := api.Decode(r.Output, &out); e != nil {
		t.Fatal(e)
	}
	if out.State != "queued" || f.delivery.sends != 0 {
		t.Fatal("saved input promised business consumption")
	}
	f.delivery.drop = true
	f.step(t)
	v, e := f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, out.SubmissionRef.ObjectID)
	if e != nil {
		t.Fatal(e)
	}
	if v.Submission.State != "sending" || v.Command == nil {
		t.Fatalf("lost receipt erased sending %+v", v)
	}
	original := *v.Command
	receipt, e := f.d.Lookup(f.ctx, f.auth, original.CommandID)
	if e != nil || receipt.Stage != "applied" {
		t.Fatalf("business fact %+v %v", receipt, e)
	}
	f.step(t)
	v, e = f.s.ReadSubmission(f.ctx, f.store, f.scope, f.auth, out.SubmissionRef.ObjectID)
	if e != nil {
		t.Fatal(e)
	}
	if v.Submission.State != "applied" || v.Submission.TaskRef == nil || f.delivery.sends != 1 || !api.Equal(v.Command, &original) {
		t.Fatalf("recovery changed original %+v sends=%d", v, f.delivery.sends)
	}
	actual, e := f.task.Read(f.ctx, f.store, f.scope, f.auth, v.Submission.TaskRef.ObjectID)
	if e != nil || actual.SubmitCommandID != original.CommandID || actual.GoalRef != goal {
		t.Fatalf("independent Task fact %+v %v", actual, e)
	}
}
