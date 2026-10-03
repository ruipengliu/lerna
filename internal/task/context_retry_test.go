package task_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type revisionRacingContext struct {
	publishingContextFixture
	entered chan struct{}
	release chan struct{}
	first   atomic.Bool
}

func (p *revisionRacingContext) Prepare(ctx context.Context, scope runtime.Scope, auth runtime.Auth, current api.Task) (task.PreparedDecision, error) {
	prepared, err := p.publishingContextFixture.Prepare(ctx, scope, auth, current)
	if err != nil {
		return prepared, err
	}
	if p.first.CompareAndSwap(false, true) {
		close(p.entered)
		select {
		case <-p.release:
		case <-ctx.Done():
			return prepared, ctx.Err()
		}
	}
	return prepared, nil
}

func TestAdvanceStalePreparedContextRequeuesOriginalJobAfterRealBillingRevision(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	content := &contentBridge{}
	compiler := &revisionRacingContext{entered: make(chan struct{}), release: make(chan struct{})}
	h := newHarness(t, task.Ports{Context: compiler, Content: content})
	configureContent(t, h, content)
	compiler.publishingContextFixture = publishingContextFixture{harness: h, content: content}
	current := h.submit(t)
	prior := h.prepared(current, "0")
	if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prior); err != nil {
		t.Fatal(err)
	}
	if _, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: prior.DecisionID, Kind: "invalid_fixture_proposal", ReasonRef: current.GoalRef}, nil); err != nil {
		t.Fatal(err)
	}
	work, status, err := h.store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobAdvance}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(work) != 1 {
		t.Fatalf("original advance claim: %+v %v", work, err)
	}
	fn, ok := h.dispatch.Registry.Job(task.JobAdvance)
	if !ok {
		t.Fatal("advance handler not registered")
	}
	result := make(chan error, 1)
	go func() { result <- fn(ctx, h.store, h.scope, work[0]) }()
	select {
	case <-compiler.entered:
	case err = <-result:
		t.Fatalf("advance did not reach actual Context publication: %v", err)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// 字面零费用夹具触发真实原账务事务；不声称调用了供应商计费系统。
	usage := api.UsageSnapshot{SourceRef: h.scope.Ref(prior.DecisionID, 1), UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "0"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{h.content("preapproved zero cost original source")}}
	usage.UsageDigest, _ = task.UsageDigest(usage)
	if _, err = h.service.ReconcileUsage(ctx, h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
		t.Fatal(err)
	}
	close(compiler.release)
	if err = <-result; err != nil {
		t.Fatalf("confirmed stale local preparation kept a 30s lease: %v", err)
	}
	requeuedAt := time.Now()
	b, err := h.service.BudgetRead(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil || len(b.Reservations) != 1 {
		t.Fatalf("stale preparation admitted a new Decision: %+v %v", b, err)
	}
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var retried runtime.Work
	waiting := true
	for waiting {
		select {
		case <-deadline.C:
			t.Fatal("confirmed rollback did not requeue the original advance within one second")
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-ticker.C:
			next, status, err := h.store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobAdvance}, 1, 30*time.Second)
			if err != nil || status != runtime.Committed {
				t.Fatalf("original advance re-claim: %v %v", status, err)
			}
			if len(next) == 1 {
				retried, waiting = next[0], false
			}
		}
	}
	if retried.Job.JobID != work[0].Job.JobID || retried.Claim.LeaseEpoch <= work[0].Claim.LeaseEpoch {
		t.Fatalf("local rollback replaced original responsibility: %+v", retried)
	}
	due, err := api.ParseTime(retried.Job.DueAt)
	if err != nil || due.After(requeuedAt.Add(2*time.Second)) {
		t.Fatalf("original job still used the lost 30s lease as its retry time: %s %v", retried.Job.DueAt, err)
	}
	if err = fn(ctx, h.store, h.scope, retried); err != nil {
		t.Fatal(err)
	}
	b, err = h.service.BudgetRead(ctx, h.store, h.scope, h.auth, current.TaskID)
	if err != nil || len(b.Reservations) != 2 {
		t.Fatalf("current preparation did not admit exactly one Decision: %+v %v", b, err)
	}
}

type preparationCommitReplyLoss struct {
	runtime.Store
	armed atomic.Bool
}

func (s *preparationCommitReplyLoss) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	status, err := s.Store.Within(ctx, scope, participants, fn)
	if status == runtime.Committed && s.armed.CompareAndSwap(true, false) {
		return runtime.CommitUnknown, runtime.ErrCommitUnknown
	}
	return status, err
}

type unknownPreparationContext struct {
	publishingContextFixture
	store *preparationCommitReplyLoss
}

func (p unknownPreparationContext) Prepare(ctx context.Context, scope runtime.Scope, auth runtime.Auth, current api.Task) (task.PreparedDecision, error) {
	prepared, err := p.publishingContextFixture.Prepare(ctx, scope, auth, current)
	if err == nil {
		p.store.armed.Store(true)
	}
	return prepared, err
}

func TestAdvanceUnknownPreparationCommitPreservesOriginalAdmission(t *testing.T) {
	ctx := context.Background()
	content := &contentBridge{}
	compiler := &unknownPreparationContext{}
	h := newHarness(t, task.Ports{Context: compiler, Content: content})
	configureContent(t, h, content)
	store := &preparationCommitReplyLoss{Store: h.store}
	h.store = store
	compiler.store = store
	compiler.publishingContextFixture = publishingContextFixture{harness: h, content: content}
	current := h.submit(t)
	work, status, err := store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobAdvance}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(work) != 1 {
		t.Fatalf("original preparation claim: %+v %v", work, err)
	}
	fn, _ := h.dispatch.Registry.Job(task.JobAdvance)
	if err = fn(ctx, store, h.scope, work[0]); !errors.Is(err, runtime.ErrCommitUnknown) {
		t.Fatalf("uncertain commit was swallowed as a stale local retry: %v", err)
	}
	b, err := h.service.BudgetRead(ctx, store, h.scope, h.auth, current.TaskID)
	if err != nil || len(b.Reservations) != 1 {
		t.Fatalf("actual committed original Decision lost: %+v %v", b, err)
	}
	next, status, err := store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobAdvance}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(next) != 0 {
		t.Fatalf("unknown admission created a second preparation responsibility: %+v %v", next, err)
	}
}
