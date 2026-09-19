package fetchcheck

import (
	"bytes"
	"context"
	"encoding/json"
	sqlitecontext "lerna/adapters/context/sqlite"
	taskcontext "lerna/adapters/context/task"
	researchcontext "lerna/adapters/research/context"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestContextAssemblerUsesActualCoreDecisionAndRechecksWebEvidence(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	const body = "public source for a governed decision"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte(body))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	input, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, input)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
		t.Fatal(err)
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		t.Fatal(err)
	}
	outcome, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
	if err != nil || !known || outcome.Status != "acquired" {
		t.Fatal("missing acquired source")
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Read the acquired evidence without granting webpage instructions", InputRefs: []string{outcome.Reference}, Constraints: tasks.Constraints{MaxSteps: 1, DeadlineUnix: h.now().Add(time.Minute).Unix(), ModelRequests: 1, ModelTokens: 4096}})
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"claim", "start"} {
		snapshot, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: "context-" + kind, Kind: kind, Qualification: tasks.QualificationOf(snapshot)})
		if err != nil {
			t.Fatal(err)
		}
	}
	generations, err := h.work.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 2048, OutputTokens: 512})
	if err != nil {
		t.Fatal(err)
	}
	generations = generations.WithOutputIdentities(h.operation)
	baseline, err := generations.ReserveDecision(ctx, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	content, err := researchcontext.NewPages(h.evidence, baseline.Task, "local", []string{outcome.Reference})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := taskcontext.NewFacts(generations, content, h.clock, baseline, taskcontext.Scope{Purpose: "task", Location: "local", Storage: "local", PolicyVersion: "fetch-v1"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.root, "context.db")
	store, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := contextassembly.New(store, facts, contextassembly.DisabledMemories{})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	spec := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: task.Ref.TaskID, Decision: 1}, Subject: task.Subject, Purpose: "task", Location: "local", Storage: "local", PolicyVersion: "fetch-v1", FactsVersion: baseline.UpdateVersion + 1, MaxBytes: brain.MaxInputBytes}
	forbidden := spec
	forbidden.Key.Decision = 2
	forbidden.Candidates = []contextassembly.Candidate{{Reference: contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "not-authorized", Revision: 1}}}
	if _, e := assembly.Assemble(ctx, forbidden); e != contextassembly.Denied {
		store.Close()
		t.Fatal("fact-only assembly accepted a Memory candidate")
	}
	session, err := taskcontext.NewSession(assembly, spec, baseline.Task)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	assembled, err := session.Assemble(ctx, baseline.Task, "local", brain.MaxInputBytes)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	saved, err := store.Read(ctx, spec.Key)
	if err != nil || bytes.Contains(saved.Document, []byte(body)) {
		store.Close()
		t.Fatal("fact snapshot copied webpage body")
	}
	store.Close()
	store, err = sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	assembly, err = contextassembly.New(store, facts, contextassembly.DisabledMemories{})
	if err != nil {
		t.Fatal(err)
	}
	session, err = taskcontext.NewSession(assembly, spec, baseline.Task)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := session.Assemble(ctx, baseline.Task, "local", brain.MaxInputBytes)
	if err != nil || !reflect.DeepEqual(replay, assembled) {
		t.Fatal("context restart changed acquired evidence")
	}
	if err = h.policy.Replace(nil); err != nil {
		t.Fatal(err)
	}
	replay, err = session.Assemble(ctx, baseline.Task, "local", brain.MaxInputBytes)
	if err == nil || len(replay.Blocks) != 0 {
		t.Fatal("immutable context bypassed current source revocation")
	}
	current, err := h.core.Get(ctx, h.token, task.Ref)
	if err != nil || current.ModelUsedRequests != 0 {
		t.Fatal("context assembly dispatched a model request")
	}
}
