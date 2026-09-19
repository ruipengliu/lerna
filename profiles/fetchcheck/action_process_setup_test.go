package fetchcheck

import (
	"context"
	"encoding/json"
	catalogauth "lerna/adapters/catalog/auth"
	sqlitecatalog "lerna/adapters/catalog/sqlite"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/brain"
	"lerna/catalog"
	"lerna/fetch"
	"lerna/tasks"
	"path/filepath"
	"testing"
	"time"
)

func prepareActionAnswerProcess(t *testing.T, ctx context.Context, h *harness, configure ...func(*tasks.ActionPort)) (tasks.RunSnapshot, string) {
	final, calls := runActionProcess(t, ctx, h, configure...)

	if final.Task.State != "RUNNING" || final.Actions == nil || final.Actions.AnswerQualification == nil || len(final.Actions.Actions) != 1 || calls != 1 {
		queries, actions := 0, 0
		if final.Actions != nil {
			queries, actions = len(final.Actions.Queries), len(final.Actions.Actions)
		}
		t.Fatalf("same-task answer handoff: state=%s waiting=%v queries=%d actions=%d model_calls=%d", final.Task.State, final.Task.WaitingReasons, queries, actions, calls)
	}
	result, known, err := h.attempts.Outcome(ctx, "local", final.Actions.Actions[0].OperationID)
	if err != nil || !known || result.Status != "acquired" {
		t.Fatal("missing action evidence")
	}
	return final, result.Reference
}

func runActionProcess(t *testing.T, ctx context.Context, h *harness, configure ...func(*tasks.ActionPort)) (tasks.RunSnapshot, int) {
	t.Helper()
	t.Cleanup(h.close)
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
	raw, _ := json.Marshal(map[string]any{"url": h.urls[0], "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	input, err := h.put(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	modelBudget := uint32(2)
	task, err := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Acquire bounded evidence", InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: 2, ModelRequests: modelBudget, ModelTokens: 16384, DeadlineUnix: h.now().Add(time.Minute).Unix()}})
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
	port, err := h.work.Actions(tasks.ActionLimits{MaxOperations: 2, MaxQueries: 32, InputTokens: 8192, OutputTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = port.Initialize(ctx, tasks.QualificationOf(run)); err != nil {
		t.Fatal(err)
	}
	for _, bind := range configure {
		bind(port)
	}
	model := &fetchActionModel{}
	env := &fetchActionHost{h: h, directory: directory, answerNext: true}
	runner, err := brain.NewActions(model, env, port)
	if err != nil {
		t.Fatal(err)
	}
	final, err := runner.Run(ctx, task.Ref)

	if err != nil {
		t.Fatal(err)
	}
	return final, model.calls
}
