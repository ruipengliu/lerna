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
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 实际HTTP只返回一份合同草稿；不代表供应商自然语言质量或最终计费能力。
func TestConfiguredModelUsesOriginalHistoryAuthorizationAndActualFee(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredModel(t, driver)
		})
	}
}

func runConfiguredModel(t *testing.T, driver string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var active atomic.Pointer[App]
	var sends atomic.Int32
	physical := make(chan []byte, 1)
	reply := api.Raw(map[string]any{
		"id": "actual-contract-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(map[string]any{
			"draft":    map[string]any{"kind": "fail", "reason_local_id": "reason", "reason_code": "contract_declined"},
			"contents": []any{map[string]any{"local_id": "reason", "media_type": "text/plain", "body": "本合同探针不建议任何工具行动或成果完成。", "disclosed_sources": []any{}}},
		}))}}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}},
	})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxJSONBytes+1))
		if err != nil {
			t.Error(err)
			return
		}
		a := active.Load()
		call, err := a.Model.Call(r.Context(), r.Header.Get("X-Harness-Call-ID"))
		if err != nil || call.Status != "send_started" {
			t.Errorf("request lacked a durable original provider marker: %v", err)
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
			t.Error("inaccurate physical model body")
			return
		}
		facts, err := a.Task.ContextFacts(r.Context(), a.Store, a.Scope, a.UserAuth, input.Snapshot.TaskRef.ObjectID)
		if err != nil {
			t.Error(err)
			return
		}
		raw, err := a.query(r.Context(), "budget.read", facts.Task.TaskID, task.BudgetReadInput{TaskID: facts.Task.TaskID})
		var budget task.BudgetReadResponse
		if err != nil || api.Decode(raw, &budget) != nil || budget.Task == nil || len(budget.Task.Reservations) != 1 || budget.Task.Reservations[0].BindingState != "bound" || len(budget.Task.Reservations[0].Units) != 1 || budget.Task.Reservations[0].Units[0].OriginalReserved == "0" {
			t.Errorf("physical request before nonzero Task reservation: %v", err)
			return
		}
		decisionRef := budget.Task.Reservations[0].SourceRef
		view, e := a.Brain.Get(r.Context(), a.Store, a.Scope, a.ServiceAuth, decisionRef.ObjectID)
		if e != nil || view.CallID != call.CallID || !view.Decision.SendStarted || view.Decision.PhysicalRequestCount != 1 {
			t.Errorf("outbound without original Brain decision marker: %v", e)
		}
		raw, e = a.query(r.Context(), "grant.use.get", modelUseID(decisionRef.ObjectID), governance.IDInput{ID: modelUseID(decisionRef.ObjectID)})
		var use governance.UseReceipt
		if e != nil || api.Decode(raw, &use) != nil || use.Decision != "allowed" || use.TargetRef.ObjectID != decisionRef.ObjectID {
			t.Errorf("outbound without original allowed Use: %v", e)
		}
		physical <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(reply)
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
	defer a.Close()
	active.Store(a)
	command := func(method, target string, payload any) api.Receipt {
		t.Helper()
		var expected *uint64
		if method == "submission.withdraw" {
			value := uint64(1)
			expected = &value
		}
		out, e := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(api.Command{ExpectedRevision: expected, Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: target, Method: method, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(payload)}))
		if e != nil || out.Error != nil {
			t.Fatalf("%s: %v %+v", method, e, out.Error)
		}
		return out
	}
	publish := func(body string) api.ContentRef {
		t.Helper()
		ref, e := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "text/plain", []byte(body), []api.ContentRef{}, []api.ContentRef{})
		if e != nil {
			t.Fatal(e)
		}
		return ref
	}
	sessionID, branchID := api.NewID("session"), api.NewID("branch")
	command("session.create", a.Scope.OwnerID, interaction.CreateSessionInput{SessionID: sessionID, DefaultBranchID: branchID, ConfigRef: a.TaskPolicy.PolicyRef})
	submit := func(content api.ContentRef, attachments []api.ContentRef) interaction.SubmissionOutput {
		t.Helper()
		view, e := a.Interaction.ReadSession(ctx, a.Store, a.Scope, a.UserAuth, sessionID, interaction.ReadInput{})
		if e != nil || len(view.Branches) != 1 {
			t.Fatalf("session %v", e)
		}
		br := view.Branches[0]
		r := command("session.submit_goal", sessionID, interaction.GoalInput{SessionRef: a.Scope.Ref(sessionID, view.Session.Revision), BranchRef: a.Scope.Ref(branchID, br.Revision), ExpectedBranchRevision: br.Revision, ContentRef: content, AttachmentRefs: attachments, PolicyRef: a.TaskPolicy.PolicyRef, Budget: []api.Amount{{Unit: "USD", Value: "1"}}, TaskDeadline: api.Time(time.Now().Add(5 * time.Minute))})
		var out interaction.SubmissionOutput
		if api.Decode(r.Output, &out) != nil {
			t.Fatal("submission receipt invalid")
		}
		return out
	}
	historyBody := "原分支的历史：报告应引用这里的说明。"
	prior := submit(publish(historyBody), []api.ContentRef{})
	command("submission.withdraw", prior.SubmissionRef.ObjectID, interaction.WithdrawInput{Reason: "contract setup preserves history without creating a first Task"})
	goalBody, attachmentBody := "请根据原历史和附件判断是否能够继续。", "准确附件：事实应独立验证。"
	goal, attachment := publish(goalBody), publish(attachmentBody)
	submission := submit(goal, []api.ContentRef{attachment})
	original, e := a.Interaction.ReadSubmission(ctx, a.Store, a.Scope, a.UserAuth, submission.SubmissionRef.ObjectID)
	if e != nil || original.Command == nil {
		t.Fatalf("original submission command missing %v", e)
	}
	taskID := original.Command.TargetID
	for {
		if e = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 200); e != nil {
			t.Fatal(e)
		}
		got, e := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if e != nil && !api.IsCode(e, "not_found") {
			t.Fatal(e)
		}
		if e == nil && got.Status == "failed" && !got.AccountingOpen {
			if got.ResultRef != nil || len(got.Budget) != 1 || got.Budget[0].Spent != "0.00024" || got.Budget[0].Reserved != "0" {
				t.Fatalf("actual failure/fee differs: %+v", got)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("original pipeline did not finish: %+v %v", got, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if sends.Load() != 1 {
		t.Fatalf("physical attempts=%d", sends.Load())
	}
	actual := <-physical
	var wire struct {
		Messages []struct {
			Content string `json:"content"`
		} `json:"messages"`
	}
	var input struct {
		Snapshot  api.Snapshot `json:"snapshot"`
		Goal      string       `json:"goal_utf8"`
		Materials []struct {
			Ref  api.ContentRef `json:"ref"`
			Body string         `json:"body_utf8"`
		} `json:"materials"`
	}
	if json.Unmarshal(actual, &wire) != nil || json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil {
		t.Fatal("physical body invalid")
	}
	bodies := map[string]bool{}
	for _, m := range input.Materials {
		if api.Hash([]byte(m.Body)) != m.Ref.Hash || uint64(len(m.Body)) != m.Ref.ByteLength {
			t.Fatal("material bytes differ")
		}
		bodies[m.Body] = true
	}
	if !bodies[historyBody] || !bodies[goalBody] || !bodies[attachmentBody] {
		t.Fatal("physical request omitted original history, goal or attachment")
	}
	budgetRaw, e := a.query(ctx, "budget.read", taskID, task.BudgetReadInput{TaskID: taskID})
	var finalBudget task.BudgetReadResponse
	if e != nil || api.Decode(budgetRaw, &finalBudget) != nil || finalBudget.Task == nil || len(finalBudget.Task.Reservations) != 1 {
		t.Fatalf("original reservation missing: %v", e)
	}
	completed, e := a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, finalBudget.Task.Reservations[0].SourceRef.ObjectID)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := a.query(ctx, "grant.settlement.read", modelUseID(completed.Decision.DecisionID), governance.IDInput{ID: modelUseID(completed.Decision.DecisionID)})
	var settled governance.UseSettlement
	if e != nil || api.Decode(raw, &settled) != nil || !settled.UsageFinal || !settled.SpendingClosed || len(settled.CumulativeUsage) != 1 || settled.CumulativeUsage[0].Value != "0.00024" || len(settled.RemainingReserved) != 0 {
		t.Fatalf("original grant settlement missing: %+v %v", settled, e)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, e := reopened.Brain.Get(ctx, reopened.Store, reopened.Scope, reopened.ServiceAuth, completed.Decision.DecisionID)
	if e != nil || !api.Equal(after, completed) || sends.Load() != 1 {
		t.Fatalf("reopen changed original decision/request: %v", e)
	}
}

func contractModelConfig(endpoint string) *ModelConfig {
	return &ModelConfig{ProfileRef: api.ComponentRef{ComponentID: api.NewID("modelprofile"), Version: "1"}, Endpoint: endpoint, Model: "contract-model", Receiver: "contract-model-owner", Location: "cloud", CredentialEnv: "HARNESS_CONTRACT_MODEL_CREDENTIAL", CredentialID: "contract-v1", TokenizerContract: "utf8-byte-upper-bound-v1", InputUSDPerMillion: "2", CachedInputUSDPerMillion: "1", OutputUSDPerMillion: "4", BillingFinal: true, ContextLimit: 250000, MaxInputTokens: 240000, MaxOutputTokens: 1000, SafetyMargin: 64, MaxInputBytes: api.MaxJSONBytes, RequestTimeoutSeconds: 5, MaxResponseBytes: api.MaxJSONBytes, MaxConcurrent: 1, AllowHTTPForLoopback: true}
}

func TestConfiguredModelLostReplyPreservesOriginalCallAndOpenAccounting(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		connection, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = connection.Close()
	}))
	defer server.Close()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, "sqlite")
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
	defer a.Close()
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "text/plain", []byte("这次真实HTTP故意丢失原回复，保留未知费用。"), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	taskID := api.NewID("task")
	receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: taskID, Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}, RequirementCandidates: []api.RequirementCandidate{}})}))
	if err != nil || receipt.Error != nil {
		t.Fatalf("submit %v %+v", err, receipt.Error)
	}
	read := func(app *App) (task.BudgetReadResponse, brain.View) {
		t.Helper()
		raw, e := app.query(ctx, "budget.read", taskID, task.BudgetReadInput{TaskID: taskID})
		var budget task.BudgetReadResponse
		if e != nil || api.Decode(raw, &budget) != nil || budget.Task == nil {
			t.Fatalf("budget %v", e)
		}
		var view brain.View
		if len(budget.Task.Reservations) == 1 {
			view, e = app.Brain.Get(ctx, app.Store, app.Scope, app.ServiceAuth, budget.Task.Reservations[0].SourceRef.ObjectID)
			if e != nil && !api.IsCode(e, "not_found") {
				t.Fatal(e)
			}
		}
		return budget, view
	}
	var budget task.BudgetReadResponse
	var view brain.View
	for {
		if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 100); err != nil {
			t.Fatal(err)
		}
		budget, view = read(a)
		if view.Decision.Status == "provider_result_unknown" {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if sends.Load() != 1 || view.Decision.PhysicalRequestCount != 1 || view.Decision.UsageFinal || !budget.Task.AccountingOpen || len(budget.Task.Budget) != 1 || budget.Task.Budget[0].Reserved == "0" || budget.Task.Budget[0].Spent != "0" {
		t.Fatalf("unknown original expense was closed or repeated: %+v %+v attempts=%d", view, budget, sends.Load())
	}
	originalReservation := budget.Task.Reservations[0]
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	// 重开是独立观察阶段；不刷新原 Task、命令、证明或 Claim 的任何业务期限。
	cancel()
	ctx, reopenCancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer reopenCancel()
	reopenStarted := time.Now()
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	t.Logf("original_reopen_observer elapsed=%s", time.Since(reopenStarted))
	if err = runtime.Drain(ctx, reopened.Store, reopened.Scope, reopened.Registry, 100); err != nil {
		t.Fatal(err)
	}
	afterBudget, after := read(reopened)
	if sends.Load() != 1 || after.CallID != view.CallID || after.Decision.PhysicalRequestCount != 1 || !afterBudget.Task.AccountingOpen || len(afterBudget.Task.Reservations) != 1 || afterBudget.Task.Reservations[0].ReservationID != originalReservation.ReservationID || afterBudget.Task.Reservations[0].Units[0].OriginalReserved != originalReservation.Units[0].OriginalReserved {
		t.Fatal("reopen sent again, replaced the original reservation or closed unknown accounting")
	}
}

func TestConfiguredModelMissingCredentialFailsBeforeOutbound(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { sends.Add(1) }))
	defer server.Close()
	ctx := context.Background()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	cfg.Model = contractModelConfig(server.URL)
	t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "")
	app, err := OpenApp(ctx, cfg, true)
	if app != nil {
		_ = app.Close()
		t.Fatal("missing credential opened a model responsibility")
	}
	if !api.IsCode(err, "unsupported") || sends.Load() != 0 {
		t.Fatalf("missing credential triggered outbound or lost classification: %v", err)
	}
}
