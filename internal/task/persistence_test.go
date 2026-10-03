package task_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestSubmitLostCommitReplyRecoversOriginalReceiptAfterReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reply-lost.sqlite")
	var armed atomic.Bool
	store, err := sqlite.Open(path, sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
		if phase == sqlite.AfterCommit && armed.CompareAndSwap(true, false) {
			return errors.New("injected commit reply loss")
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	h := harnessForStore(t, store, task.Ports{})
	id := api.NewID("task")
	c := h.command("task.submit", id, nil, task.SubmitInput{OrchestratorID: h.scope.OwnerID, GoalRef: h.content("unchanged exact original target"), PolicyRef: h.policy.PolicyRef, Deadline: api.Time(time.Now().Add(time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
	armed.Store(true)
	first, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(c))
	if err != nil || first.Stage != "applied" {
		t.Fatalf("commit unknown recovery %+v %v", first, err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	h.store = reopened
	h.dispatch.Store = reopened
	second, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(c))
	if err != nil || !api.Equal(first, second) {
		t.Fatalf("reopen changed original receipt %+v %v", second, err)
	}
	read, err := h.service.Read(context.Background(), reopened, h.scope, h.auth, id)
	if err != nil || read.Revision != 1 || read.GoalRevision != 1 {
		t.Fatalf("duplicate created or changed Task %+v %v", read, err)
	}
	b, err := h.query("task.list", h.scope.OwnerID, task.TaskListInput{Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	var list api.Page[api.Task]
	if err = api.Decode(b, &list); err != nil || len(list.Items) != 1 {
		t.Fatalf("created repeated original target %s %v", b, err)
	}
}

type replyLostPublisher struct {
	*contentBridge
	loseReply bool
	published api.ContentRef
}

func (p *replyLostPublisher) Publish(ctx context.Context, scope runtime.Scope, id, media string, body []byte) (api.ContentRef, error) {
	ref, err := p.contentBridge.Publish(ctx, scope, id, media, body)
	if err != nil {
		return ref, err
	}
	p.published = ref
	if p.loseReply {
		p.loseReply = false
		return api.ContentRef{}, api.E("dependency_unavailable", "content_reply_lost")
	}
	return ref, nil
}
func resultForPublisher(t *testing.T, h *harness, gate *evidenceBridge, rule api.RuleDefinition) api.Result {
	t.Helper()
	gate.service = governance.New(h.store, governance.Options{})
	current := readyTask(t, h, rule)
	artifact := h.content("independently observed artifact fixture")
	now := api.Time(time.Now())
	check := api.ConditionResult{CheckID: api.NewID("check"), TaskID: current.TaskID, GoalRevision: current.GoalRevision, RequirementID: current.Requirements[0].RequirementID, RequirementRevision: 1, ArtifactRef: artifact, RuleRef: rule.RuleRef, EvaluatorRef: rule.RuleRef, Verdict: "pass", Applicability: "usable", Basis: "verified", EvidenceRefs: []api.ContentRef{h.content("accurate observation fixture")}, ScopeRef: h.content("fixed path/hash scope"), ObservedAt: now, CheckedAt: now}
	if _, err := h.service.RecordCheck(context.Background(), h.store, h.scope, h.trusted(), check); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Complete(context.Background(), h.store, h.scope, h.trusted(), task.CompleteInput{TaskID: current.TaskID, ExpectedGoalRevision: current.GoalRevision, ArtifactRefs: []api.ContentRef{artifact}})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestResultPublicationLostReplyReusesOriginalContentAndResult(t *testing.T) {
	rule := fixtureRule()
	gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	publisher := &replyLostPublisher{contentBridge: &contentBridge{}, loseReply: true}
	h := newHarness(t, task.Ports{Gate: gate, Content: publisher}, rule)
	configureContent(t, h, publisher.contentBridge)
	result := resultForPublisher(t, h, gate, rule)
	drainKind(t, h, task.JobPublishResult)
	firstCopy := publisher.published
	if !api.ValidID(firstCopy.ContentID) {
		t.Fatal("reply loss did not follow actual durable Content publication")
	}
	body, err := publisher.Read(context.Background(), h.scope, h.auth, firstCopy)
	var copyResult api.Result
	if err == nil {
		err = api.Decode(body, &copyResult)
	}
	if err != nil || !api.Equal(copyResult, result) {
		t.Fatalf("published bytes differ from original Result %v", err)
	}
	pending, err := h.service.Result(context.Background(), h.store, h.scope, h.auth, result.TaskID, task.ResultInput{})
	if err != nil || pending.Publication != "pending" || pending.ContentRef != nil || !api.Equal(pending.Result, result) {
		t.Fatalf("unknown publication invented receipt %+v %v", pending, err)
	}
	time.Sleep(1050 * time.Millisecond)
	drainKind(t, h, task.JobPublishResult)
	complete, err := h.service.Result(context.Background(), h.store, h.scope, h.auth, result.TaskID, task.ResultInput{})
	if err != nil || complete.Publication != "published" || complete.ContentRef == nil || !api.Equal(*complete.ContentRef, firstCopy) || !api.Equal(complete.Result, result) {
		t.Fatalf("recovery minted second identity %+v %v", complete, err)
	}
}

func TestStaleResultPublisherCannotPublishOrFinishAfterClaimReplacement(t *testing.T) {
	rule := fixtureRule()
	gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	content := &contentBridge{}
	h := newHarness(t, task.Ports{Gate: gate, Content: content}, rule)
	configureContent(t, h, content)
	result := resultForPublisher(t, h, gate, rule)
	old, status, err := h.store.Claim(context.Background(), h.scope, api.NewID("worker"), []string{task.JobPublishResult}, 1, 20*time.Millisecond)
	if err != nil || status != runtime.Committed || len(old) != 1 {
		t.Fatalf("old claim %+v %v", old, err)
	}
	time.Sleep(30 * time.Millisecond)
	newWork, status, err := h.store.Claim(context.Background(), h.scope, api.NewID("worker"), []string{task.JobPublishResult}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(newWork) != 1 || newWork[0].Claim.LeaseEpoch <= old[0].Claim.LeaseEpoch {
		t.Fatalf("replacement claim %+v %v", newWork, err)
	}
	handler, _ := h.dispatch.Registry.Job(task.JobPublishResult)
	if err = handler(context.Background(), h.store, h.scope, old[0]); !errors.Is(err, runtime.ErrClaimLost) {
		t.Fatalf("old publisher was not fenced: %v", err)
	}
	files := 0
	err = filepath.WalkDir(content.objectRoot, func(_ string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type().IsRegular() {
			files++
		}
		return nil
	})
	if err != nil || files != 0 {
		t.Fatalf("stale worker wrote external Content bytes files=%d %v", files, err)
	}
	if err = handler(context.Background(), h.store, h.scope, newWork[0]); err != nil {
		t.Fatal(err)
	}
	view, err := h.service.Result(context.Background(), h.store, h.scope, h.auth, result.TaskID, task.ResultInput{})
	if err != nil || view.Publication != "published" || view.ContentRef == nil || !api.Equal(view.Result, result) {
		t.Fatalf("new publisher did not finish original responsibility %+v %v", view, err)
	}
}

func TestPostgresTaskLifecycleEvidenceAndCumulativeAccounting(t *testing.T) {
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("HARNESS_TEST_POSTGRES_DSN is not configured")
	}
	store, err := postgres.Open(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	rule := fixtureRule()
	gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
	proof := &localProofFixture{}
	h := harnessForStore(t, store, task.Ports{Gate: gate, ClosureProof: proof, ControlProof: proof, ActionAuthorization: proof}, rule)
	proof.install(t, h.scope)
	gate.service = governance.New(store, governance.Options{})
	current := h.submit(t)
	prepared := h.prepared(current, "10")
	if _, err = h.service.PrepareDecision(context.Background(), store, h.scope, h.trusted(), prepared); err != nil {
		t.Fatal(err)
	}
	usageProof := h.content("literal accounting fixture proof")
	apply := func(revision uint64, amount string, closed bool) {
		t.Helper()
		u := api.UsageSnapshot{SourceRef: h.scope.Ref(prepared.DecisionID, revision), UsageRevision: revision, Cumulative: []api.Amount{{Unit: "USD", Value: amount}}, SpendingClosed: closed, UsageFinal: closed, ProofRefs: []api.ContentRef{usageProof}}
		u.UsageDigest, _ = task.UsageDigest(u)
		if _, err := h.service.ReconcileUsage(context.Background(), store, h.scope, h.trusted(), "brain_decision", u); err != nil {
			t.Fatal(err)
		}
	}
	apply(1, "6", false)
	apply(1, "6", false)
	b, err := h.service.BudgetRead(context.Background(), store, h.scope, h.auth, current.TaskID)
	if err != nil || b.Budget[0].Spent != "6" || b.Budget[0].Reserved != "4" {
		t.Fatalf("PG cumulative6 %+v %v", b, err)
	}
	current, err = h.service.Read(context.Background(), store, h.scope, h.auth, current.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	r, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(h.command("task.cancel", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "cancel while accounting open"})))
	if err != nil || r.Stage != "applied" {
		t.Fatalf("PG cancel %+v %v", r, err)
	}
	apply(2, "8", true)
	apply(3, "9", true)
	b, err = h.service.BudgetRead(context.Background(), store, h.scope, h.auth, current.TaskID)
	if err != nil || b.Budget[0].Spent != "9" || b.Budget[0].Reserved != "0" || b.AccountingOpen {
		t.Fatalf("PG late delta %+v %v", b, err)
	}
	current = readyTask(t, h, rule)
	artifact := h.content("preapproved independently observed report fixture")
	now := api.Time(time.Now())
	check := api.ConditionResult{CheckID: api.NewID("check"), TaskID: current.TaskID, GoalRevision: current.GoalRevision, RequirementID: current.Requirements[0].RequirementID, RequirementRevision: 1, ArtifactRef: artifact, RuleRef: rule.RuleRef, EvaluatorRef: rule.RuleRef, Verdict: "pass", Applicability: "usable", Basis: "verified", EvidenceRefs: []api.ContentRef{h.content("preapproved observation")}, ScopeRef: h.content("accurate path/hash scope"), ObservedAt: now, CheckedAt: now}
	if _, err = h.service.RecordCheck(context.Background(), store, h.scope, h.trusted(), check); err != nil {
		t.Fatal(err)
	}
	result, err := h.service.Complete(context.Background(), store, h.scope, h.trusted(), task.CompleteInput{TaskID: current.TaskID, ExpectedGoalRevision: current.GoalRevision, ArtifactRefs: []api.ContentRef{artifact}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := h.service.Result(context.Background(), store, h.scope, h.auth, current.TaskID, task.ResultInput{})
	if err != nil || !api.Equal(result, view.Result) || view.Publication != "pending" {
		t.Fatalf("PG original Result %+v %v", view, err)
	}
	closure, err := h.service.Closure(context.Background(), store, h.scope, h.auth, h.scope.Ref(current.TaskID, current.Revision))
	if err != nil || !closure.GoalWorkClosed || !closure.EffectsClosed || closure.ProofRef.ContentID == current.GoalRef.ContentID {
		t.Fatalf("PG closure %+v %v", closure, err)
	}
}
