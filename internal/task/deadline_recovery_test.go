package task_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestOriginalDeadlineClosesWaitingTaskAfterActualDatabaseReopen(t *testing.T) {
	for _, fixture := range []struct{ driver, state string }{{"sqlite", "waiting"}, {"sqlite", "paused"}, {"postgres", "waiting"}, {"postgres", "paused"}} {
		driver := fixture.driver
		t.Run(driver+"/"+fixture.state, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			path := filepath.Join(t.TempDir(), "original.sqlite")
			open := func() runtime.Store {
				t.Helper()
				var store runtime.Store
				var err error
				if driver == "sqlite" {
					s, openErr := sqlite.Open(path)
					if openErr == nil {
						openErr = s.Migrate(ctx)
					}
					store, err = s, openErr
				} else {
					s, openErr := postgres.Open(ctx, os.Getenv("HARNESS_TEST_POSTGRES_DSN"))
					if openErr == nil {
						openErr = s.Migrate(ctx)
					}
					store, err = s, openErr
				}
				if err != nil {
					t.Fatal(err)
				}
				return store
			}
			store := open()
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})
			h := harnessForStore(t, store, task.Ports{})
			schema := api.Object(map[string]any{"body": api.String()}, "body")
			digest, err := api.Digest(schema)
			if err != nil {
				t.Fatal(err)
			}
			schemaRef := api.ComponentRef{ComponentID: api.NewID("schema"), Version: "1.0.0", Digest: digest}
			h.service, err = task.New(task.Config{Policies: []task.TaskPolicy{h.policy}, AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: schemaRef, Schema: schema}}}, task.Ports{})
			if err != nil {
				t.Fatal(err)
			}
			h.dispatch.Registry = runtime.NewRegistry()
			if err = h.service.Register(h.dispatch.Registry); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Second)
			goal := h.content("Original target whose clarification has not been answered.")
			command := h.command("task.submit", api.NewID("task"), nil, task.SubmitInput{OrchestratorID: h.scope.OwnerID, GoalRef: goal, PolicyRef: h.policy.PolicyRef, Deadline: api.Time(deadline), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
			receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(command))
			if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
				t.Fatalf("original Submit: %+v %v", receipt, err)
			}
			current, err := h.service.Read(ctx, h.store, h.scope, h.auth, command.TargetID)
			if err != nil {
				t.Fatal(err)
			}
			// 真实准入/账务接口预置未结原来源；此夹具不配置物理模型出口。
			prepared := h.prepared(current, "10")
			if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), prepared); err != nil {
				t.Fatal(err)
			}
			usage := api.UsageSnapshot{SourceRef: h.scope.Ref(prepared.DecisionID, 1), UsageRevision: 1, Cumulative: []api.Amount{{Unit: "USD", Value: "6"}}, SpendingClosed: false, UsageFinal: false, ProofRefs: []api.ContentRef{h.content("original bounded cumulative usage source fixture")}}
			usage.UsageDigest, _ = task.UsageDigest(usage)
			if _, err = h.service.ReconcileUsage(ctx, h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
				t.Fatal(err)
			}
			current, err = h.service.Read(ctx, h.store, h.scope, h.auth, command.TargetID)
			if err != nil {
				t.Fatal(err)
			}
			request := api.InputRequest{RequestID: api.NewID("request"), TargetRef: h.scope.Ref(current.TaskID, current.Revision), GoalRevision: &current.GoalRevision, Purpose: "clarify_goal", QuestionRef: h.content("Provide the exact body."), AnswerSchemaRef: schemaRef, PreviewRefs: []api.ContentRef{goal}, ExpiresAt: current.Deadline, State: "pending"}
			status, err := h.store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
				_, err := h.service.CreateInputTx(ctx, tx, h.trusted(), current.TaskID, request)
				return err
			})
			if err != nil || status != runtime.Committed {
				t.Fatalf("original input: %+v %v", status, err)
			}
			drainKind(t, h, task.JobAdvance)
			current, err = h.service.Read(ctx, h.store, h.scope, h.auth, command.TargetID)
			if err != nil || current.RequirementsState != "awaiting_input" || current.Status != "active" {
				t.Fatalf("fixture did not enter an actual bounded input wait: %+v %v", current, err)
			}
			if fixture.state == "paused" {
				paused, pauseErr := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.pause", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "pause while the original clarification is pending"})))
				if pauseErr != nil || paused.Error != nil || paused.Stage != "applied" {
					t.Fatalf("original pause: %+v %v", paused, pauseErr)
				}
				drainKind(t, h, task.JobAdvance)
				current, err = h.service.Read(ctx, h.store, h.scope, h.auth, current.TaskID)
				if err != nil || current.Control != "paused" {
					t.Fatalf("pause did not preserve a durable wait: %+v %v", current, err)
				}
			}
			if err = h.service.RecoverDeadline(ctx, h.store, h.scope, h.auth, current.TaskID); !api.IsCode(err, "forbidden") {
				t.Fatalf("ordinary subject acquired trusted deadline maintenance: %v", err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store = open()
			h.store, h.dispatch.Store = store, store
			if store.ID() != h.scope.DatabaseID {
				t.Fatal("reopen changed original database")
			}
			if remaining := time.Until(deadline) + 20*time.Millisecond; remaining > 0 {
				select {
				case <-time.After(remaining):
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				}
			}
			drainKind(t, h, task.JobAdvance)
			closed, err := h.service.Read(ctx, h.store, h.scope, h.auth, command.TargetID)
			if err != nil || closed.Status != "failed" {
				t.Fatalf("original deadline had no durable recovery responsibility: status=%s deadline=%s err=%v", closed.Status, closed.Deadline, err)
			}
			if closed.Deadline != current.Deadline || closed.GoalRevision != current.GoalRevision || !api.Equal(closed.GoalRef, goal) || closed.ControlRevision != current.ControlRevision+1 || len(closed.WaitReasons) != 1 || !strings.Contains(closed.WaitReasons[0].ResumeCondition, "deadline_exceeded") {
				t.Fatalf("deadline recovery replaced original facts: %+v", closed)
			}
			budget, err := h.service.BudgetRead(ctx, h.store, h.scope, h.auth, closed.TaskID)
			if err != nil || !budget.AccountingOpen || budget.Budget[0].Spent != "6" || budget.Budget[0].Reserved != "4" {
				t.Fatalf("deadline released an unknown original fee: %+v %v", budget, err)
			}
			again, err := h.dispatch.Lookup(ctx, h.auth, command.CommandID)
			if err != nil || !api.Equal(again, receipt) {
				t.Fatalf("original receipt changed: %+v %v", again, err)
			}
			if _, err = h.service.Result(ctx, h.store, h.scope, h.auth, command.TargetID, task.ResultInput{}); !api.IsCode(err, "invalid_state") {
				t.Fatalf("deadline fabricated a Result: %v", err)
			}
			if err = h.service.RecoverDeadline(ctx, h.store, h.scope, h.trusted(), closed.TaskID); err != nil {
				t.Fatal(err)
			}
			drainKind(t, h, task.JobAdvance)
			stable, err := h.service.Read(ctx, h.store, h.scope, h.auth, command.TargetID)
			if err != nil || !api.Equal(stable, closed) {
				t.Fatalf("replay repeated deadline closure: %+v %v", stable, err)
			}
			usage.SourceRef.Revision, usage.UsageRevision = 2, 2
			usage.Cumulative[0].Value, usage.SpendingClosed, usage.UsageFinal = "8", true, true
			usage.UsageDigest, _ = task.UsageDigest(usage)
			if _, err = h.service.ReconcileUsage(ctx, h.store, h.scope, h.trusted(), "brain_decision", usage); err != nil {
				t.Fatal(err)
			}
			budget, err = h.service.BudgetRead(ctx, h.store, h.scope, h.auth, closed.TaskID)
			if err != nil || budget.AccountingOpen || budget.Budget[0].Spent != "8" || budget.Budget[0].Reserved != "0" {
				t.Fatalf("late original usage failed to close accurately: %+v %v", budget, err)
			}
			stable, err = h.service.Read(ctx, h.store, h.scope, h.auth, closed.TaskID)
			if err != nil || stable.Status != "failed" || stable.Deadline != closed.Deadline || !api.Equal(stable.GoalRef, closed.GoalRef) {
				t.Fatalf("late usage reopened the expired goal: %+v %v", stable, err)
			}
		})
	}
}
