package brain_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type originalTaskGate struct {
	service *task.Service
	fault   error
}

func (g *originalTaskGate) CheckTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, in brain.DecideInput, _ *brain.Encoding) error {
	if g.fault != nil {
		return g.fault
	}
	return g.service.CheckDecisionTx(ctx, tx, a, in.DecisionID)
}

type taskProviderFixture struct {
	store          runtime.Store
	scope          runtime.Scope
	auth           runtime.Auth
	content        *fileContents
	gate           *originalTaskGate
	task           *task.Service
	taskDispatcher *runtime.Dispatcher
	brain          *brain.Service
	registry       *runtime.Registry
	command        api.Command
	taskID         string
	posts          atomic.Int32
}

// 准入、账务和原命令使用实际数据库；替换的供应商边界是一次真实 HTTP 请求。
func originalTaskProvider(t *testing.T, driver string, paused bool) *taskProviderFixture {
	return originalTaskProviderEngine(t, driver, paused, func(engine brain.Engine) brain.Engine { return engine })
}

func originalTaskProviderEngine(t *testing.T, driver string, paused bool, wrap func(brain.Engine) brain.Engine) *taskProviderFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	f := &taskProviderFixture{content: &fileContents{root: root, paused: paused}}
	if driver == "postgres" {
		dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("requires actual PG")
		}
		s, err := postgres.Open(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		f.store = s
	} else {
		s, err := sqlite.Open(filepath.Join(root, "brain.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		if err = s.Migrate(ctx); err != nil {
			t.Fatal(err)
		}
		f.store = s
	}
	t.Cleanup(func() {
		if err := f.store.Close(); err != nil {
			t.Error(err)
		}
	})
	f.scope = runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: f.store.ID()}
	f.auth = runtime.Auth{TenantID: f.scope.TenantID, SubjectID: f.scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service", "task_admin"}}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.posts.Add(1)
		if r.Method != http.MethodPost || r.Header.Get("X-Harness-Call-ID") == "" {
			t.Error("physical request lacks original CallID")
		}
		model := map[string]any{"draft": brain.Draft{Kind: "fail", ReasonLocalID: "reason", ReasonCode: "original_literal_model_proposal"}, "contents": []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "The original proposal still needs the Task owner decision.", DisclosedSources: []api.ContentRef{}}}}
		_, _ = w.Write(api.Raw(map[string]any{"id": "original-task-provider-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(model))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}}))
	}))
	t.Cleanup(server.Close)
	provider, err := providers.NewOpenAI(brainProviderConfig(f.store, f.scope, server.URL))
	if err != nil {
		t.Fatal(err)
	}
	ref := provider.Profile().Ref
	policy := task.TaskPolicy{PolicyRef: ref, ContinuationLimit: 100, RepairLimit: 3, NoProgressLimit: 5, ContextRoundLimit: 3, SafeAttemptLimit: 2, MaxRequirements: 100, MaxDelegations: 128, MaxDepth: 4, CostMode: "strict", BudgetLimits: []api.Amount{{Unit: "USD", Value: "100"}}, MaxEvidenceStalenessSeconds: 300, MaxDurationSeconds: 3600}
	f.task, err = task.New(task.Config{Policies: []task.TaskPolicy{policy}}, task.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	taskRegistry := runtime.NewRegistry()
	if err = f.task.Register(taskRegistry); err != nil {
		t.Fatal(err)
	}
	f.taskDispatcher = &runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: taskRegistry}
	snapshot := providerSnapshot(t, f.scope, f.content, provider, []byte("original frozen goal for one decision"))
	f.taskID = api.NewID("task")
	submit := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: f.taskID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: f.scope.OwnerID, GoalRef: snapshot.GoalRef, PolicyRef: policy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})}
	if r, err := f.taskDispatcher.Command(ctx, f.auth, api.Raw(submit)); err != nil || r.Stage != "applied" {
		t.Fatalf("actual Task submit %+v %v", r, err)
	}
	current, err := f.task.Read(ctx, f.store, f.scope, f.auth, f.taskID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot.TaskRef = f.scope.Ref(f.taskID, current.Revision)
	snapshot.GoalRevision = current.GoalRevision
	snapshot.ControlRevision = current.ControlRevision
	snapshot.Requirements = current.Requirements
	snapshot.RequirementsDigest = current.RequirementsDigest
	snapshot.RequirementsState = current.RequirementsState
	snapshot.Purpose = "interpret_requirements"
	snapshot.PolicyRef = policy.PolicyRef
	snapshotRef, err := f.content.put(f.scope, api.NewID("snapshot"), "application/json", api.Raw(snapshot))
	if err != nil {
		t.Fatal(err)
	}
	id, commandID := api.NewID("decision"), api.NewID("command")
	prepared := task.PreparedDecision{DecisionID: id, CommandID: commandID, BrainOwnerID: f.scope.OwnerID, CostBound: []api.Amount{{Unit: "USD", Value: "1"}}, Snapshot: snapshot, SnapshotRef: snapshotRef}
	if _, err = f.task.PrepareDecision(ctx, f.store, f.scope, f.auth, prepared); err != nil {
		t.Fatal(err)
	}
	f.gate = &originalTaskGate{service: f.task}
	f.brain, err = brain.New(brain.Config{Profiles: []brain.Profile{provider.Profile()}, Content: f.content, Engine: wrap(provider), Gate: f.gate, Participants: []string{"task"}})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = runtime.NewRegistry()
	if err = f.brain.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.command = api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: commandID, TargetID: id, Method: "brain.decide", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(brain.DecideInput{DecisionID: id, TaskRef: snapshot.TaskRef, SnapshotRef: snapshotRef, SnapshotRevision: 1, ModelProfileRef: ref, UseRefs: []api.ObjectRef{}, Limits: []api.Amount{{Unit: "USD", Value: "1"}}, Deadline: current.Deadline})}
	return f
}

type upstreamBrainStore struct{ runtime.Store }
type upstreamBrainTx struct {
	runtime.Tx
	locked  map[string]bool
	domain  bool
	command bool
}

func (s upstreamBrainStore) Within(ctx context.Context, scope runtime.Scope, parts []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, parts, func(tx runtime.Tx) error { return fn(&upstreamBrainTx{Tx: tx, locked: map[string]bool{}}) })
}
func (tx *upstreamBrainTx) Get(ctx context.Context, ns, id string, out any) (uint64, error) {
	key := ns + "/" + id
	if ns == "task.tasks" && tx.domain && !tx.locked[key] {
		return 0, fmt.Errorf("Brain first locked upstream Task after Decision")
	}
	rev, err := tx.Tx.Get(ctx, ns, id, out)
	if err == nil {
		tx.locked[key] = true
		if ns != "task.tasks" {
			tx.domain = true
		}
	}
	return rev, err
}
func (tx *upstreamBrainTx) LoadCommand(ctx context.Context, id string) (runtime.StoredCommand, error) {
	if tx.domain && !tx.command {
		return runtime.StoredCommand{}, fmt.Errorf("Brain first locked original command after domain")
	}
	tx.command = true
	return tx.Tx.LoadCommand(ctx, id)
}
func (tx *upstreamBrainTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		prior := tx.Tx
		tx.Tx = inner
		defer func() { tx.Tx = prior }()
		return fn(tx)
	})
}

func TestBrainSealsAndPublishesOriginalDecisionWithTaskBeforeDecisionLocks(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			f := originalTaskProvider(t, driver, false)
			store := upstreamBrainStore{Store: f.store}
			dispatch := runtime.Dispatcher{Store: store, OwnerID: f.scope.OwnerID, Registry: f.registry}
			if r, err := dispatch.Command(context.Background(), f.auth, api.Raw(f.command)); err != nil || r.Stage != "accepted" {
				t.Fatalf("original decision admission %+v %v", r, err)
			}
			view := drainBrainUntil(t, store, f.scope, f.auth, f.brain, f.registry, f.command.TargetID, "completed")
			if f.posts.Load() != 1 || view.Decision.PhysicalRequestCount != 1 || !view.Decision.UsageFinal {
				t.Fatalf("legal ordered pipeline lost original model work %+v", view)
			}
			if r, err := dispatch.Lookup(context.Background(), f.auth, f.command.CommandID); err != nil || r.Stage != "applied" {
				t.Fatalf("original receipt not decided %+v %v", r, err)
			}
		})
	}
}
