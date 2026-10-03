package task_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 此边界仅观察实际事务的首次锁；约束来自 storage.md 的全局顺序。
// 已取得的锁可重读，GetVersion/List 只读路由不算行锁；不替换持久事实。
type orderedTaskStore struct{ runtime.Store }
type orderedTaskTx struct {
	runtime.Tx
	locks   map[string]bool
	command bool
	domain  bool
	jobs    bool
}

func (s orderedTaskStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error { return fn(&orderedTaskTx{Tx: tx, locks: map[string]bool{}}) })
}
func (tx *orderedTaskTx) Get(ctx context.Context, ns, id string, out any) (uint64, error) {
	key := ns + "/" + id
	if !tx.locks[key] {
		if ns == "task.tasks" && (tx.domain || tx.jobs) {
			return 0, fmt.Errorf("upstream Task first locked after domain or Job: %s", id)
		}
		if ns != "task.tasks" && tx.jobs {
			return 0, fmt.Errorf("domain first locked after Job: %s", ns)
		}
	}
	rev, err := tx.Tx.Get(ctx, ns, id, out)
	if err == nil {
		tx.locks[key] = true
		if ns != "task.tasks" {
			tx.domain = true
		}
	}
	return rev, err
}
func (tx *orderedTaskTx) Create(ctx context.Context, ns, id, parent string, out any) error {
	if tx.jobs {
		return fmt.Errorf("domain created after Job: %s", ns)
	}
	err := tx.Tx.Create(ctx, ns, id, parent, out)
	if err == nil {
		tx.locks[ns+"/"+id] = true
		if ns != "task.tasks" {
			tx.domain = true
		}
	}
	return err
}
func (tx *orderedTaskTx) LoadCommand(ctx context.Context, id string) (runtime.StoredCommand, error) {
	if !tx.command && (tx.domain || tx.jobs) {
		return runtime.StoredCommand{}, fmt.Errorf("original command first locked after domain or Job")
	}
	tx.command = true
	return tx.Tx.LoadCommand(ctx, id)
}
func (tx *orderedTaskTx) Raise(ctx context.Context, kind, key string, ref api.ObjectRef, at time.Time) (api.Job, error) {
	tx.jobs = true
	return tx.Tx.Raise(ctx, kind, key, ref, at)
}
func (tx *orderedTaskTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		tx2 := *tx
		tx2.Tx = inner
		tx2.locks = map[string]bool{}
		for key, value := range tx.locks {
			tx2.locks[key] = value
		}
		err := fn(&tx2)
		if err == nil {
			tx.locks, tx.command, tx.domain, tx.jobs = tx2.locks, tx2.command, tx2.domain, tx2.jobs
		}
		return err
	})
}

func TestTaskCancellationClosesCompleteSubtreeBeforePublishingControlJobs(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			rule := fixtureRule()
			gate := &evidenceBridge{rules: map[string]api.RuleDefinition{rule.RuleRef.ComponentID: rule}}
			proof := &localProofFixture{}
			ports := task.Ports{Gate: gate, ActionAuthorization: proof}
			var h *harness
			if driver == "postgres" {
				dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("requires actual PG")
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
				h = harnessForStore(t, store, ports, rule)
			} else {
				h = newHarness(t, ports, rule)
			}
			proof.install(t, h.scope)
			gate.service = governance.New(h.store, governance.Options{})
			root := readyTask(t, h, rule)
			children := []api.Task{}
			for range 2 {
				root, _ = h.service.Read(context.Background(), h.store, h.scope, h.auth, root.TaskID)
				in := task.DelegateInput{DelegationID: api.NewID("delegation"), ParentTaskRef: h.scope.Ref(root.TaskID, root.Revision), ParentGoalRevision: root.GoalRevision, GoalRef: h.content("literal bounded internal child goal"), InputRefs: []api.ContentRef{}, AgentBindingRef: h.scope.Ref(api.NewID("binding"), 1), PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "1"}}, Deadline: root.Deadline, PolicyRef: h.policy.PolicyRef, ReceiverID: h.scope.OwnerID, Internal: true}
				delegated, err := h.service.Delegate(context.Background(), h.store, h.scope, h.auth, h.command("trusted.delegate", in.DelegationID, nil, in), in)
				if err != nil || delegated.ChildTaskRef == nil {
					t.Fatalf("actual child delegation %+v %v", delegated, err)
				}
				child, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, delegated.ChildTaskRef.ObjectID)
				if err != nil {
					t.Fatal(err)
				}
				decision := h.prepared(child, "0")
				if _, err = h.service.PrepareDecision(context.Background(), h.store, h.scope, h.trusted(), decision); err != nil {
					t.Fatal(err)
				}
				a := preparedAction(h, "0", "safe_original_child_check")
				a.SafeRequirementCheck = true
				if out, err := h.service.ConsumeProposal(context.Background(), h.store, h.scope, h.trusted(), task.Proposal{DecisionID: decision.DecisionID, Kind: "act", ReasonRef: child.GoalRef, Actions: []task.PreparedAction{a}}, nil); err != nil || len(out.AdmittedOperationIDs) != 1 {
					t.Fatalf("actual child action %+v %v", out, err)
				}
				children = append(children, child)
			}
			root, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, root.TaskID)
			if err != nil {
				t.Fatal(err)
			}
			h.dispatch.Store = orderedTaskStore{Store: h.store}
			c := h.command("task.cancel", root.TaskID, &root.Revision, task.ControlInput{TaskID: root.TaskID, Reason: "cancel original complete subtree"})
			r, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(c))
			if err != nil || r.Stage != "applied" {
				t.Fatalf("legal subtree cancellation violated lock contract %+v %v", r, err)
			}
			for _, child := range children {
				got, err := h.service.Read(context.Background(), h.store, h.scope, h.auth, child.TaskID)
				if err != nil || got.Status != "cancelled" || got.OpenEffects.UnresolvedCount != 0 {
					t.Fatalf("original child work not closed %+v %v", got, err)
				}
			}
			jobs, status, err := h.store.Claim(context.Background(), h.scope, api.NewID("worker"), []string{task.JobControl}, 10, time.Minute)
			if err != nil || status != runtime.Committed || len(jobs) != 2 {
				t.Fatalf("actual child control responsibilities missing %d %s %v", len(jobs), status, err)
			}
		})
	}
}

func TestPendingGoalCommandsFollowOriginalCommandAndTaskLockOrder(t *testing.T) {
	for _, kind := range []string{"steer", "input"} {
		t.Run(kind, func(t *testing.T) {
			content := &contentBridge{}
			h := newHarness(t, task.Ports{Content: content})
			configureContent(t, h, content)
			schema := api.Object(map[string]any{"path": api.String()}, "path")
			digest, _ := api.Digest(schema)
			schemaRef := api.ComponentRef{ComponentID: api.NewID("schema"), Version: "1", Digest: digest}
			service, err := task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: schemaRef, Schema: schema}}}, task.Ports{Content: content})
			if err != nil {
				t.Fatal(err)
			}
			h.service = service
			h.dispatch.Registry = runtime.NewRegistry()
			if err = service.Register(h.dispatch.Registry); err != nil {
				t.Fatal(err)
			}
			current := h.submit(t)
			answer, err := content.Publish(context.Background(), h.scope, api.NewID("upload"), "application/json", []byte(`{"path":"reports/original-goal.md"}`))
			if err != nil {
				t.Fatal(err)
			}
			var c api.Command
			if kind == "steer" {
				source := h.scope.Ref(api.NewID("submission"), 1)
				c = h.command("task.steer", current.TaskID, nil, task.SteerInput{TaskID: current.TaskID, BaseGoalRevision: current.GoalRevision, AmendmentRef: answer, SourceSubmissionRef: source, PrepareDeadline: api.Time(time.Now().Add(time.Minute))})
			} else {
				goalRevision := current.GoalRevision
				request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: h.scope.Ref(current.TaskID, current.Revision), GoalRevision: &goalRevision, Purpose: "clarify_goal", QuestionRef: current.GoalRef, AnswerSchemaRef: schemaRef, PreviewRefs: []api.ContentRef{current.GoalRef}, ExpiresAt: api.Time(time.Now().Add(time.Minute)), State: "pending"}
				var ref api.ObjectRef
				if _, err = h.store.Within(context.Background(), h.scope, []string{"task"}, func(tx runtime.Tx) error {
					var e error
					ref, e = service.CreateInputTx(context.Background(), tx, h.trusted(), current.TaskID, request)
					return e
				}); err != nil {
					t.Fatal(err)
				}
				c = h.command("task.input", current.TaskID, nil, task.InputAnswer{TaskID: current.TaskID, RequestRef: ref, GoalRevision: current.GoalRevision, AnswerRef: answer})
			}
			h.store = orderedTaskStore{Store: h.store}
			h.dispatch.Store = h.store
			r, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(c))
			if err != nil || r.Stage != "accepted" {
				t.Fatalf("original goal command admission %+v %v", r, err)
			}
			job := task.JobSteer
			if kind == "input" {
				job = task.JobInput
			}
			drainKind(t, h, job)
			r, err = h.dispatch.Lookup(context.Background(), h.auth, c.CommandID)
			if err != nil || r.Stage != "applied" {
				t.Fatalf("original goal command not decided %+v %v", r, err)
			}
			updated, err := service.Read(context.Background(), h.store, h.scope, h.auth, current.TaskID)
			if err != nil || updated.GoalRevision != current.GoalRevision+1 {
				t.Fatalf("accurate amended goal missing %+v %v", updated, err)
			}
			replay, err := h.dispatch.Command(context.Background(), h.auth, api.Raw(c))
			if err != nil || !api.Equal(replay, r) {
				t.Fatal("original goal replay changed its result")
			}
		})
	}
}
