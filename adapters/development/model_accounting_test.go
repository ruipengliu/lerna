package development

import (
	"context"
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
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestClosedOriginalModelSourceRetainsKnownInvoiceAndSettlesOriginalUse(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runOriginalModelInvoice(t, driver, false, false, false) })
	}
}

func TestPreviouslyPublishedModelInvoiceKeepsOriginalRefAfterSourceClosure(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runOriginalModelInvoice(t, driver, true, false, false) })
	}
}

func TestClosedOriginalModelSourceCannotTurnLostReplyIntoZeroFinalUsage(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runOriginalModelInvoice(t, driver, false, true, false) })
	}
}

func TestOriginalModelUseSettlesAfterModelConfigurationIsDisabled(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runOriginalModelInvoice(t, driver, false, false, true) })
	}
}

func runOriginalModelInvoice(t *testing.T, driver string, legacy, unknown, disabled bool) {
	t.Helper()
	if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
		t.Skip("requires actual PostgreSQL")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var active atomic.Pointer[App]
	var sends atomic.Int32
	var goal api.ContentRef
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		a := active.Load()
		// 在真实原模型请求已发送之后撤回原正文，回复的已知费用仍须结清。
		expected := uint64(1)
		if !legacy {
			closed, err := a.Dispatcher.Command(r.Context(), a.UserAuth, api.Raw(api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: goal.ContentID, Method: "content.close", ExpectedRevision: &expected, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: goal, Reason: "original source withdrawn after the single actual model request"})}))
			if err != nil || closed.Error != nil || closed.Stage != "applied" {
				t.Errorf("actual source close: %+v %v", closed, err)
			}
		}
		if unknown {
			connection, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			if err = connection.Close(); err != nil {
				t.Error(err)
			}
			return
		}
		model := api.Raw(map[string]any{"draft": brain.Draft{Kind: "fail", ReasonLocalID: "reason", ReasonCode: "original_model_declined"}, "contents": []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "This proposal cannot authorize an action.", DisclosedSources: []api.ContentRef{}}}})
		_, _ = w.Write(api.Raw(map[string]any{"id": "known-original-model-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(model)}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}}))
	}))
	t.Cleanup(server.Close)
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
	t.Cleanup(func() {
		if err := a.Close(); err != nil {
			t.Error(err)
		}
	})
	goal, err = a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "text/plain", []byte("The exact original source must not be read after withdrawal."), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	active.Store(a)
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: api.NewID("task"), Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}})}
	originalReceipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
	if err != nil || originalReceipt.Stage != "applied" || originalReceipt.Error != nil {
		t.Fatalf("original Task: %+v %v", originalReceipt, err)
	}
	var view brain.View
	for step := 0; step < 32; step++ {
		work, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{task.JobAdvance, task.JobDispatchDecision, brain.JobAdvance}, 1, 30*time.Second)
		if err != nil || status != runtime.Committed {
			t.Fatalf("original work claim: status=%s err=%v", status, err)
		}
		if len(work) == 0 {
			select {
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
			continue
		}
		handler, _ := a.Registry.Job(work[0].Job.Kind)
		if err = handler(ctx, a.Store, a.Scope, work[0]); err != nil {
			t.Fatalf("original %s: %v", work[0].Job.Kind, err)
		}
		if work[0].Job.Kind == brain.JobAdvance {
			view, err = a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, work[0].Job.SourceRef.ObjectID)
			if err != nil {
				t.Fatal(err)
			}
			if (view.Decision.Status == "cancelled" || legacy && view.Decision.Status == "completed") && view.Decision.UsageFinal || unknown && view.Decision.Status == "provider_result_unknown" {
				break
			}
		}
	}
	if !unknown && ((view.Decision.Status != "cancelled" && !(legacy && view.Decision.Status == "completed")) || !view.Decision.UsageFinal || len(view.Decision.Usage) != 1 || view.Decision.Usage[0].Value != "0.00024") || view.Decision.PhysicalRequestCount != 1 || sends.Load() != 1 || unknown && (view.Decision.Status != "provider_result_unknown" || view.Decision.UsageFinal) {
		t.Fatalf("source withdrawal changed original known facts: %+v physical=%d", view, sends.Load())
	}
	var earlier api.UsageSnapshot
	if legacy {
		// 通过旧Content-only端口实际出版原DAG账单，再撤源并用新装配恢复。
		old, err := brain.New(brain.Config{Profiles: []brain.Profile{a.Profile}, Content: legacyModelContent{Content: brainContent{a}}, Engine: a.Engine, Gate: brainGate{a}})
		if err != nil {
			t.Fatal(err)
		}
		earlier, err = old.Usage(ctx, a.Store, a.Scope, a.Scope.Ref(view.Decision.DecisionID, view.Decision.Revision))
		if err != nil || len(earlier.ProofRefs) != 1 {
			t.Fatalf("original legacy invoice publication: %+v %v", earlier, err)
		}
		expected := uint64(1)
		closed, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: goal.ContentID, Method: "content.close", ExpectedRevision: &expected, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: goal, Reason: "original source withdrawn after original invoice publication"})}))
		if err != nil || closed.Error != nil || closed.Stage != "applied" {
			t.Fatalf("actual source close after invoice: %+v %v", closed, err)
		}
	}
	current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, command.TargetID)
	if err != nil {
		t.Fatal(err)
	}
	stop := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: current.TaskID, Method: "task.cancel", ExpectedRevision: &current.Revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: current.TaskID, Reason: "close original goal after its source is withdrawn"})}
	if receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(stop)); err != nil || receipt.Error != nil {
		t.Fatalf("original cancellation: %+v %v", receipt, err)
	}
	if disabled {
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
		cfg.Model = nil
		a, err = OpenApp(ctx, cfg, false)
		if err != nil {
			t.Fatal(err)
		}
	}
	originalHold := current.Budget[0].Reserved
	usage, err := a.Brain.Usage(ctx, a.Store, a.Scope, a.Scope.Ref(view.Decision.DecisionID, view.Decision.Revision))
	if err != nil || len(usage.ProofRefs) != 1 || !unknown && (!usage.SpendingClosed || !usage.UsageFinal) || unknown && (usage.SpendingClosed || usage.UsageFinal) {
		t.Fatalf("closed original source stranded an already-known model invoice: %+v %v", usage, err)
	}
	if legacy && !api.Equal(usage, earlier) {
		t.Fatalf("minimum accounting replaced an original applied invoice: old=%+v new=%+v", earlier, usage)
	}
	if !legacy && !unknown {
		// 原invoice已applied后，仅推进Runtime Tx时钟到原30min上传窗口之后。
		// 真实账本/命令/字节不变；恢复应查询原回执，不能重做已过期上传。
		store, memoryStore := a.Store, a.Memory.Store
		future := modelInvoiceClockStore{Store: store, offset: 31 * time.Minute}
		a.Store, a.Memory.Store = future, future
		replayed, replayErr := a.Brain.Usage(ctx, future, a.Scope, usage.SourceRef)
		a.Store, a.Memory.Store = store, memoryStore
		if replayErr != nil || !api.Equal(replayed, usage) {
			t.Fatalf("applied original invoice cannot recover after its frozen upload window: %+v %v", replayed, replayErr)
		}
	}
	facts, err := a.Brain.AccountingFacts(ctx, a.Store, a.Scope, a.ServiceAuth, usage.SourceRef)
	if err != nil {
		t.Fatal(err)
	}
	publication := brain.Publication{ContentID: usage.ProofRefs[0].ContentID, MediaType: "application/vnd.harness.usage-proof+json", ProcessedSources: []api.ContentRef{view.SnapshotRef}, DisclosedSources: []api.ContentRef{}}
	for _, mutation := range []struct {
		name string
		edit func(*brain.AccountingFacts)
	}{
		{"original amount", func(f *brain.AccountingFacts) { f.Proof.Snapshot.Cumulative[0].Value = "0.01" }},
		{"original call", func(f *brain.AccountingFacts) { f.Proof.CallID = api.NewID("call") }},
		{"original use", func(f *brain.AccountingFacts) { f.UseRefs[0].ObjectID = api.NewID("use") }},
		{"original command", func(f *brain.AccountingFacts) { f.OriginalCommand.ObjectID = api.NewID("command") }},
		{"original task", func(f *brain.AccountingFacts) { f.TaskRef.ObjectID = api.NewID("task") }},
		{"physical count", func(f *brain.AccountingFacts) { f.Proof.PhysicalRequests = 2 }},
		{"billing finality", func(f *brain.AccountingFacts) { f.Proof.Snapshot.UsageFinal = !f.Proof.Snapshot.UsageFinal }},
	} {
		var forged brain.AccountingFacts
		if err = api.Decode(api.Raw(facts), &forged); err != nil {
			t.Fatal(err)
		}
		mutation.edit(&forged)
		if _, err = brain.AccountingContent(brainContent{a}).PublishUsageProof(ctx, a.Scope, a.ServiceAuth, publication, forged); !api.IsCode(err, "forbidden") {
			t.Fatalf("forged %s acquired an invoice: %v", mutation.name, err)
		}
	}
	for _, actor := range []runtime.Auth{a.UserAuth, {TenantID: a.Scope.TenantID, SubjectID: a.Scope.OwnerID, CredentialGeneration: a.ServiceAuth.CredentialGeneration, Roles: []string{"service"}}, {TenantID: api.NewID("tenant"), SubjectID: a.Scope.OwnerID, CredentialGeneration: a.ServiceAuth.CredentialGeneration, Roles: []string{"usage_reporter"}}} {
		if _, err = brain.AccountingContent(brainContent{a}).PublishUsageProof(ctx, a.Scope, actor, publication, facts); !api.IsCode(err, "forbidden") {
			t.Fatalf("unqualified original invoice authority: %v", err)
		}
	}
	if sends.Load() != 1 {
		t.Fatal("rejected invoice claims started another original model request")
	}
	if !legacy {
		body, err := a.Memory.Read(ctx, a.Scope, a.ServiceAuth, usage.ProofRefs[0], "brain_usage_proof")
		var originalProof brain.UsageProof
		if err != nil || api.Decode(body, &originalProof) != nil || originalProof.CallID != view.CallID || originalProof.PhysicalRequests != 1 || !originalProof.SendStarted || originalProof.Snapshot.UsageFinal == unknown || !api.Equal(originalProof.Snapshot.Cumulative, usage.Cumulative) {
			t.Fatalf("authority-only invoice lost original call/finality/amount: %+v %v", originalProof, err)
		}
	}
	// 领取原预留创建时的账务作业；其真实Brain端口同时核对原Grant Use。
	for step := 0; step < 32; step++ {
		work, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{task.JobBilling}, 1, 30*time.Second)
		if err != nil || status != runtime.Committed {
			t.Fatalf("original billing claim: status=%s err=%v", status, err)
		}
		if len(work) != 0 {
			handler, _ := a.Registry.Job(work[0].Job.Kind)
			if err = handler(ctx, a.Store, a.Scope, work[0]); err != nil {
				t.Fatalf("original billing responsibility: %v", err)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(100 * time.Millisecond):
		}
	}
	current, err = a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, current.TaskID)
	if err != nil || current.Status != "cancelled" || sends.Load() != 1 || !unknown && (current.AccountingOpen || current.Budget[0].Spent != "0.00024" || current.Budget[0].Reserved != "0") || unknown && (!current.AccountingOpen || current.Budget[0].Spent != "0" || current.Budget[0].Reserved != originalHold || originalHold == "0") {
		t.Fatalf("original Task fee did not settle exactly: %+v sends=%d err=%v", current, sends.Load(), err)
	}
	useID := facts.UseRefs[0].ObjectID
	raw, err := a.Dispatcher.Query(ctx, a.ServiceAuth, api.Raw(api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, QueryID: api.NewID("query"), TargetID: useID, Method: "grant.settlement.read", Payload: api.Raw(governance.IDInput{ID: useID})}))
	var settlement governance.UseSettlement
	if err != nil || api.Decode(raw, &settlement) != nil || !api.Equal(settlement.CumulativeUsage, usage.Cumulative) || !unknown && (!settlement.SpendingClosed || !settlement.UsageFinal || len(settlement.RemainingReserved) != 0) || unknown && (settlement.SpendingClosed || settlement.UsageFinal || len(settlement.RemainingReserved) != 1 || settlement.RemainingReserved[0].Value != originalHold) {
		t.Fatalf("original Grant Use fee did not settle exactly: %+v %v", settlement, err)
	}
	if _, err = a.Memory.Read(ctx, a.Scope, a.UserAuth, goal, "brain.input"); !api.IsCode(err, "forbidden") {
		t.Fatalf("minimum accounting restored ordinary source reading: %v", err)
	}
	if _, err = a.Memory.Read(ctx, a.Scope, a.UserAuth, usage.ProofRefs[0], "brain.input"); !api.IsCode(err, "forbidden") {
		t.Fatalf("minimum accounting invoice acquired ordinary model-input permission: %v", err)
	}
	if _, err = a.Memory.Read(ctx, a.Scope, a.UserAuth, usage.ProofRefs[0], "brain_usage_proof"); !api.IsCode(err, "forbidden") {
		t.Fatalf("ordinary subject acquired authority-only invoice: %v", err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	again, err := reopened.Brain.Usage(ctx, reopened.Store, reopened.Scope, reopened.Scope.Ref(view.Decision.DecisionID, view.Decision.Revision))
	if err != nil || !api.Equal(again, usage) || sends.Load() != 1 {
		t.Fatalf("reopen replaced original invoice/request: %+v %v", again, err)
	}
	if receipt, err := reopened.Dispatcher.Lookup(ctx, reopened.UserAuth, command.CommandID); err != nil || !api.Equal(receipt, originalReceipt) {
		t.Fatalf("minimum accounting changed original submit receipt: %+v %v", receipt, err)
	}
	if unknown {
		after, err := reopened.Task.Read(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, command.TargetID)
		if err != nil || !after.AccountingOpen || after.Budget[0].Spent != "0" || after.Budget[0].Reserved != originalHold {
			t.Fatalf("reopen converted an unknown original cost to known zero: %+v %v", after, err)
		}
	}
	t.Logf("verified original IDs tenant=%s owner=%s task=%s command=%s decision=%s call=%s use=%s usage_revision=%d invoice=%s invoice_hash=%s spent=%s reserved=%s final=%v accounting_open=%v model_profile_digest=%s", a.Scope.TenantID, a.Scope.OwnerID, command.TargetID, command.CommandID, view.Decision.DecisionID, view.CallID, useID, usage.UsageRevision, usage.ProofRefs[0].ContentID, usage.ProofRefs[0].Hash, current.Budget[0].Spent, current.Budget[0].Reserved, usage.UsageFinal, current.AccountingOpen, facts.ModelProfileRef.Digest)
}

// 旧profile只有原Content合同，不启用可选最低账务出版端口。
type legacyModelContent struct{ brain.Content }

type modelInvoiceClockStore struct {
	runtime.Store
	offset time.Duration
}

func (s modelInvoiceClockStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error {
		return fn(modelInvoiceClockTx{Tx: tx, offset: s.offset})
	})
}

type modelInvoiceClockTx struct {
	runtime.Tx
	offset time.Duration
}

func (tx modelInvoiceClockTx) Now(ctx context.Context) (time.Time, error) {
	now, err := tx.Tx.Now(ctx)
	return now.Add(tx.offset), err
}
