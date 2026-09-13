package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/adapters/catalogauth"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/sqlitecatalog"
	"lerna/brain"
	"lerna/catalog"
	"lerna/fetch"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestActionBrainSelectsRegisteredFetchAndCompletesOriginalTask(t *testing.T) {
	for _, oversized := range []bool{false, true} {
		name := "acquired"
		if oversized {
			name = "model_cannot_raise_limit"
		}
		t.Run(name, func(t *testing.T) { checkActionFetch(t, oversized, false, false) })
	}
}
func TestTaskFetchBudgetRejectionIsKnownThroughSDK(t *testing.T) {
	checkActionFetch(t, false, true, false)
}
func checkActionFetch(t *testing.T, oversized, exhaust, answerNext bool) {
	steps := uint32(1)
	if answerNext {
		steps = 2
	}
	if exhaust {
		steps = 3
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("actual action response"))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	source := catalog.Source{Kind: "capability", Key: "fetch", Revision: 1}
	rules := []contentpolicy.Rule{}
	for _, s := range []catalog.Source{source, {Kind: "query", Key: "fetch-goal", Revision: 1}} {
		rules = append(rules, contentpolicy.Rule{Kind: s.Kind, Key: s.Key, Revision: 1, Actions: []string{"process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(5 * time.Minute).Unix()})
	}
	policy, err := contentpolicy.New(rules)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlitecatalog.Open(filepath.Join(h.root, "fetch-catalog.db"), func(e catalog.Entry) error {
		if e.Ref.Digest != h.cap.Digest() {
			return fetch.Denied
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	entry := catalog.Entry{Source: source, Ref: catalog.Ref{Namespace: "local", Name: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, Digest: h.cap.Digest()}, Title: "Acquire bounded evidence", Category: "fetch", ResourceType: "web", Purpose: "task", Location: "local", Resource: "root", Preconditions: "Authorized source and bounded input", Effects: "Retain actual HTTP evidence", Unsupported: "Arbitrary sources", Guarantees: "Original operation recovery", Available: true, Capability: h.cap}
	if _, err = store.Replace(ctx, 0, []catalog.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	directory, err := catalog.New(store, catalogauth.Adapter{Authority: h.auth, Policy: policy, Clock: h.clock, QuerySource: catalog.Source{Kind: "query", Key: "fetch-goal", Revision: 1}}, catalog.Config{MaxScan: 1, MaxPage: 1, MaxCandidates: 1, Timeout: time.Second}, catalog.QueryContext{Namespace: "local", Subject: "operator", Token: h.token, Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	input, err := h.put(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	modelBudget := uint32(1)
	if answerNext {
		modelBudget = 2
	}
	task, err := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Acquire bounded evidence", InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: steps, ModelRequests: modelBudget, ModelTokens: 16384, DeadlineUnix: h.now().Add(time.Minute).Unix()}})
	if err != nil {
		t.Fatal(err)
	}
	run, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"claim", "start"} {
		run, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(run)})
		if err != nil {
			t.Fatal(err)
		}
	}
	port, err := h.work.Actions(tasks.ActionLimits{MaxOperations: steps, MaxQueries: 32, InputTokens: 8192, OutputTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = port.Initialize(ctx, tasks.QualificationOf(run)); err != nil {
		t.Fatal(err)
	}
	model := &fetchActionModel{oversized: oversized, exhaust: exhaust}
	env := &fetchActionHost{h: h, directory: directory, answerNext: answerNext}
	runner, err := brain.NewActions(model, env, port)
	if err != nil {
		t.Fatal(err)
	}
	final, err := runner.Run(ctx, task.Ref)
	if exhaust {
		if err != nil || len(final.Actions.Actions) != 3 || hits.Load() != 2 || model.calls != 1 {
			t.Fatalf("exhaustion loop: state=%s err=%v requests=%d", final.Task.State, err, hits.Load())
		}
		denied := final.Actions.Actions[2]
		record, err := h.client.GetInvocation(ctx, denied.OperationID)
		if err != nil || record.Result != "FAILURE" || record.Effect != "NOT_OCCURRED" || record.Phase != "NOT_STARTED" || record.Reference == "" {
			t.Fatal("budget refusal became unknown")
		}
		raw, err := h.access.Read(ctx, h.token, record.Reference, h.cap)
		var document struct {
			Output   struct{ Status, Reference string }
			Evidence struct{ Requests uint32 }
		}
		if err != nil || json.Unmarshal(raw, &document) != nil || document.Output.Status != "limit_exceeded" || document.Output.Reference != "" || document.Evidence.Requests != 0 {
			t.Fatal("budget refusal lost finite facts")
		}
		budget, err := h.attempts.Budget(ctx, task.Ref)
		if err != nil || budget.Charged != 2 {
			t.Fatal("rejection changed previous reservations")
		}
		if _, err = h.exec.Run(ctx, denied.OperationID); err != nil || hits.Load() != 2 {
			t.Fatal("rejection replay dispatched HTTP")
		}
		return
	}
	if oversized {
		if err != nil || final.Task.State != "WAITING" || hits.Load() != 0 || model.calls != 1 || len(final.Actions.Actions) != 0 || final.Task.ModelUsedRequests != 1 {
			t.Fatal("model limit expansion was not rejected and charged")
		}
		return
	}
	if answerNext {
		if err != nil || final.Task.State != "RUNNING" || final.Actions.AnswerQualification == nil || model.calls != 1 || hits.Load() != 1 {
			t.Fatalf("failed answer handoff: %v", err)
		}
		version := final.Task.Version
		queries := len(final.Actions.Queries)
		h.clock.advance(20 * time.Second)
		replay, err := runner.Run(ctx, task.Ref)
		if err != nil || replay.Task.Version != version || len(replay.Actions.Queries) != queries || model.calls != 1 || hits.Load() != 1 {
			t.Fatal("answer handoff reentry consumed work")
		}
		finishActionAnswer(t, h, port, replay)
		return
	}
	if err != nil || final.Task.State != "COMPLETED" {
		t.Fatalf("action fetch: state=%s err=%v execution=%v", final.Task.State, err, env.lastError)
	}
	if model.calls != 1 || hits.Load() != 1 || len(final.Actions.Actions) != 1 || final.Task.ModelUsedRequests != 1 {
		t.Fatal("incorrect original task/model/network accounting")
	}
	action := final.Actions.Actions[0]
	result, known, err := h.attempts.Outcome(ctx, "local", action.OperationID)
	if err != nil || !known || result.Mode != "http" || result.Requests != 1 {
		t.Fatal("missing actual HTTP outcome")
	}
	evidence, err := h.evidence.Read(ctx, result.Reference)
	if err != nil || string(evidence.Body) != "actual action response" {
		t.Fatal("missing original response")
	}
	if _, err = runner.Run(ctx, task.Ref); err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || hits.Load() != 1 {
		t.Fatal("completed run redispatched")
	}
}

func TestActionBrainHandsSettledEvidenceToAnswerStage(t *testing.T) {
	checkActionFetch(t, false, false, true)
}

func finishActionAnswer(t *testing.T, h *harness, port *tasks.ActionPort, run tasks.RunSnapshot) {
	t.Helper()
	answer, err := processResearchAnswer(context.Background(), h, port, run, decisionFixtureModel{evidence: true}, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	if answer.Status != "answerable" {
		t.Fatalf("unexpected fixture answer: %+v", answer)
	}
}
