package brain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/conformance/testkit"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type gateEntryKey struct{}
type preparedEntryKey struct{}

// 此端口夹具只证明每个新领取的 ctx 准备与 pure gate 边界；Task/预算、
// 原回执及模型的一次真实 HTTP 使用真实负责方。外部签名来源由宿主合同另验。
type entryPreparedGate struct {
	store runtime.Store
	scope runtime.Scope
	task  *task.Service
	calls int
	fault error
}

func (g *entryPreparedGate) PrepareGate(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in brain.DecideInput, _ *brain.Encoding) (context.Context, error) {
	if scope != g.scope || ctx.Value(gateEntryKey{}) == nil || ctx.Value(preparedEntryKey{}) != nil {
		return ctx, api.E("forbidden", "new_entry_inherited_old_preparation")
	}
	status, err := g.store.Within(ctx, scope, []string{"task"}, func(tx runtime.Tx) error {
		return g.task.CheckDecisionTx(ctx, tx, auth, in.DecisionID)
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ctx, err
	}
	g.calls++
	if g.fault != nil {
		return ctx, g.fault
	}
	return context.WithValue(ctx, preparedEntryKey{}, ctx.Value(gateEntryKey{})), nil
}

func (g *entryPreparedGate) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in brain.DecideInput, _ *brain.Encoding) error {
	if entry := ctx.Value(gateEntryKey{}); entry != nil && ctx.Value(preparedEntryKey{}) != entry {
		return api.E("dependency_unavailable", "current_entry_preparation_required")
	}
	return g.task.CheckDecisionTx(ctx, tx, auth, in.DecisionID)
}

func preparedGateFixture(t *testing.T, driver string) (*taskProviderFixture, *entryPreparedGate) {
	t.Helper()
	var originalEngine brain.Engine
	f := originalTaskProviderEngine(t, driver, false, func(engine brain.Engine) brain.Engine {
		originalEngine = engine
		return engine
	})
	profile := originalEngine.(interface{ Profile() brain.Profile }).Profile()
	g := &entryPreparedGate{store: f.store, scope: f.scope, task: f.task}
	var err error
	f.brain, err = brain.New(brain.Config{Profiles: []brain.Profile{profile}, Content: f.content, Engine: originalEngine, Gate: g, Participants: []string{"task"}})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = runtime.NewRegistry()
	if err = f.brain.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	// 每个领取只有新 entry；没有保存在 worker 根 ctx 的正面证明。
	f.registry.SetContextFactory(func(ctx context.Context, flow runtime.Flow) context.Context {
		if flow.Kind == "job" {
			return context.WithValue(ctx, gateEntryKey{}, api.NewID("entry"))
		}
		return ctx
	})
	dispatch := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
	if receipt, err := dispatch.Command(context.Background(), f.auth, api.Raw(f.command)); err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original decision admission %+v %v", receipt, err)
	}
	return f, g
}

func TestBrainEachNewJobPreparesCurrentGateBeforeOriginalSingleHTTP(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			f, g := preparedGateFixture(t, driver)
			view := drainBrainUntil(t, f.store, f.scope, f.auth, f.brain, f.registry, f.command.TargetID, "completed")
			if g.calls < 3 || f.posts.Load() != 1 || view.Decision.PhysicalRequestCount != 1 || !view.Decision.UsageFinal || !api.Equal(view.Decision.Usage, []api.Amount{{Unit: "USD", Value: "0.00024"}}) {
				t.Fatalf("new entry failed to retain the original model identity calls=%d posts=%d %+v", g.calls, f.posts.Load(), view)
			}
			calls := g.calls
			g.fault = context.Canceled
			jobs := testkit.ObserveJobs(f.store)
			jobs.ForbidChanges = true
			dispatch := runtime.Dispatcher{Store: jobs, OwnerID: f.scope.OwnerID, Registry: f.registry}
			original, err := dispatch.Lookup(context.Background(), f.auth, f.command.CommandID)
			if err != nil || original.Stage != "applied" {
				t.Fatalf("completed original receipt %+v %v", original, err)
			}
			// 原 Decision 已完成；终态控制须准确拒绝，不产生取消事实或新责任。
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), TargetID: f.command.TargetID, Method: "brain.cancel", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(brain.CancelInput{DecisionID: f.command.TargetID, TaskRef: view.TaskRef})}
			if receipt, err := dispatch.Command(context.Background(), f.auth, api.Raw(command)); err != nil || receipt.Stage != "rejected" || receipt.Error == nil || receipt.Error.Code != "invalid_state" || receipt.Error.Reason != "decision_completed" {
				t.Fatalf("completed cancellation reported a false transition %+v %v", receipt, err)
			}
			if err := runtime.Drain(context.Background(), jobs, f.scope, f.registry, 20); err != nil || g.calls != calls || f.posts.Load() != 1 {
				t.Fatalf("terminal recovery acquired new materials calls=%d %v", g.calls, err)
			}
			if current, err := f.brain.Get(context.Background(), jobs, f.scope, f.auth, f.command.TargetID); err != nil || !api.Equal(current, view) {
				t.Fatalf("terminal cancellation changed original Decision facts %+v %v", current, err)
			}
			if current, err := dispatch.Lookup(context.Background(), f.auth, f.command.CommandID); err != nil || !api.Equal(current, original) {
				t.Fatalf("terminal cancellation changed original receipt %+v %v", current, err)
			}
		})
	}
}

func TestBrainNextJobSourceFailurePreservesOriginalUnsentDecisionAndCause(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			f, g := preparedGateFixture(t, driver)
			ctx := context.Background()
			handler, _ := f.registry.Job(brain.JobAdvance)
			claim := func() runtime.Work {
				works, status, err := f.store.Claim(ctx, f.scope, api.NewID("worker"), []string{brain.JobAdvance}, 1, time.Minute)
				if err != nil || status != runtime.Committed || len(works) != 1 {
					t.Fatalf("original work claim %s %v", status, err)
				}
				return works[0]
			}
			if err := handler(ctx, f.store, f.scope, claim()); err != nil {
				t.Fatal(err)
			}
			before, err := f.brain.Get(ctx, f.store, f.scope, f.auth, f.command.TargetID)
			if err != nil || before.Publication != "encoded" || f.posts.Load() != 0 {
				t.Fatalf("first entry did not seal original encoding %+v %v", before, err)
			}
			work := claim()
			g.fault = context.Canceled
			if err = handler(ctx, f.store, f.scope, work); !errors.Is(err, context.Canceled) {
				t.Fatalf("source failure lost its original cause %v", err)
			}
			after, err := f.brain.Get(ctx, f.store, f.scope, f.auth, f.command.TargetID)
			if err != nil || after.Publication != "encoded" || after.CallID != before.CallID || after.CancelRequested || after.Decision.SendStarted || f.posts.Load() != 0 {
				t.Fatalf("old positive preparation authorized a new job %+v %v", after, err)
			}
			g.fault = nil
			if err = handler(ctx, f.store, f.scope, work); err != nil {
				t.Fatal(err)
			}
			closed := drainBrainUntil(t, f.store, f.scope, f.auth, f.brain, f.registry, f.command.TargetID, "completed")
			if closed.CallID != before.CallID || f.posts.Load() != 1 {
				t.Fatalf("recovery rebuilt the original call %+v", closed)
			}
		})
	}
}
