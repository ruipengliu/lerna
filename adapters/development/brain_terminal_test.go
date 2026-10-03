package development

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 真实供应商边界收到一次请求后，本人通过公开 Dispatcher 取消原 Task；
// 原回复仍会迟到，Brain、Task 投递 Job 与实际费用必须各自封闭。
func TestTerminalTaskClosesOriginalBrainDeliveryAndActualLateFee(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			var active atomic.Pointer[App]
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxJSONBytes+1))
				if err != nil {
					t.Error(err)
					return
				}
				var wire struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				var input struct {
					Snapshot api.Snapshot `json:"snapshot"`
				}
				if json.Unmarshal(body, &wire) != nil || len(wire.Messages) != 2 || json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil {
					t.Error("invalid original physical body")
					return
				}
				a := active.Load()
				current, err := a.Task.Read(r.Context(), a.Store, a.Scope, a.UserAuth, input.Snapshot.TaskRef.ObjectID)
				if err != nil {
					t.Error(err)
					return
				}
				stop := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: current.TaskID, Method: "task.cancel", ExpectedRevision: &current.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: current.TaskID, Reason: "cancel during the one original physical model call"})}
				if receipt, err := a.Dispatcher.Command(r.Context(), a.UserAuth, api.Raw(stop)); err != nil || receipt.Stage != "applied" {
					t.Errorf("public Task negative control %+v %v", receipt, err)
					return
				}
				model := map[string]any{"draft": brain.Draft{Kind: "fail", ReasonLocalID: "reason", ReasonCode: "late_original_reply"}, "contents": []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "original output arrived after cancellation", DisclosedSources: []api.ContentRef{}}}}
				_, _ = w.Write(api.Raw(map[string]any{"id": "late-original-http-response", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(model))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}}))
			}))
			defer server.Close()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "synthetic-contract-token")
			cfg.Model = contractModelConfig(server.URL)
			a, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if err := a.Close(); err != nil {
					t.Error(err)
				}
			}()
			active.Store(a)
			goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "text/plain", []byte("one original free-text goal"), []api.ContentRef{}, []api.ContentRef{})
			if err != nil {
				t.Fatal(err)
			}
			id := api.NewID("task")
			submit := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}})}
			receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(submit))
			if err != nil || receipt.Stage != "applied" {
				t.Fatalf("original public submit %+v %v", receipt, err)
			}
			var final api.Task
			for {
				if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 200); err != nil {
					t.Fatal(err)
				}
				final, err = a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
				if err != nil {
					t.Fatal(err)
				}
				if final.Status == "cancelled" && !final.AccountingOpen {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("original cancellation/fee did not finish %+v", final)
				case <-time.After(20 * time.Millisecond):
				}
			}
			if final.ResultRef != nil || final.Budget[0].Spent != "0.00024" || final.Budget[0].Reserved != "0" || posts.Load() != 1 {
				t.Fatalf("cancellation changed the original work/use %+v calls=%d", final, posts.Load())
			}
			raw, err := a.query(ctx, "budget.read", id, task.BudgetReadInput{TaskID: id})
			var budget task.BudgetReadResponse
			if err != nil || api.Decode(raw, &budget) != nil || budget.Task == nil || len(budget.Task.Reservations) != 1 {
				t.Fatalf("original budget %+v %v", budget, err)
			}
			decisionRef := budget.Task.Reservations[0].SourceRef
			view, err := a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, decisionRef.ObjectID)
			if err != nil || view.Decision.Status != "cancelled" || !view.Decision.UsageFinal || view.Decision.ProposalRef != nil || view.Decision.PhysicalRequestCount != 1 {
				t.Fatalf("original Brain fact %+v %v", view, err)
			}
			time.Sleep(1050 * time.Millisecond)
			works, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{task.JobDispatchDecision}, 1, time.Minute)
			if err != nil || status != runtime.Committed {
				t.Fatalf("original delivery claim %s %v", status, err)
			}
			if len(works) == 1 {
				handler, _ := a.Registry.Job(task.JobDispatchDecision)
				if err = handler(ctx, a.Store, a.Scope, works[0]); err != nil {
					t.Fatalf("terminal original delivery could not close: %v", err)
				}
			}
			// 查询原消费责任的下一次 due，不能在每次领取后立即再次查询冒充 Done。
			time.Sleep(1050 * time.Millisecond)
			works, status, err = a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{task.JobDispatchDecision, brain.JobAdvance, task.JobBilling}, 10, time.Minute)
			if err != nil || status != runtime.Committed || len(works) != 0 {
				t.Fatalf("closed original decision delivery or known billing kept recurring jobs=%d %s %v", len(works), status, err)
			}
			stopID := stableID("command", "brain-cancel/"+decisionRef.ObjectID)
			negative, err := a.Store.LookupCommand(ctx, a.Scope, stopID)
			if err != nil || negative.Receipt.Stage != "applied" || negative.Command.Method != "brain.cancel" {
				t.Fatalf("negative delivery lacked its original command %+v %v", negative.Receipt, err)
			}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			a, err = OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			if recovered, err := a.Dispatcher.Command(ctx, a.ServiceAuth, api.Raw(negative.Command)); err != nil || !api.Equal(recovered, negative.Receipt) {
				t.Fatalf("reopened negative command changed its original input/expiry %+v %v", recovered, err)
			}
			view, err = a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, decisionRef.ObjectID)
			if err != nil || view.Decision.Status != "cancelled" || !view.Decision.UsageFinal || view.Decision.PhysicalRequestCount != 1 || posts.Load() != 1 {
				t.Fatalf("reopen resurrected original decision %+v %v", view, err)
			}
			original, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(submit))
			if err != nil || !api.Equal(original, receipt) || posts.Load() != 1 {
				t.Fatalf("original submit replay changed %+v %v", original, err)
			}
		})
	}
}
