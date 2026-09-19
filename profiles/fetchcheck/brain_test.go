package fetchcheck

import (
	"context"
	"encoding/json"
	contentpolicy "lerna/adapters/content/policy"
	sqlitecontext "lerna/adapters/context/sqlite"
	taskcontext "lerna/adapters/context/task"
	researchcontext "lerna/adapters/research/context"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

func TestBrainPublishesGovernedFetchAnswerAndRejectsMidGenerationRevocation(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		name := "publish"
		if revoke {
			name = "revoke"
		}
		t.Run(name, func(t *testing.T) { checkBrainFetch(t, revoke, false) })
	}
}
func checkBrainFetch(t *testing.T, revoke, evidence bool) {
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
	defer store.Close()
	assembly, err := contextassembly.New(store, facts, contextassembly.DisabledMemories{})
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	spec := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: task.Ref.TaskID, Decision: 1}, Subject: task.Subject, Purpose: "task", Location: "local", Storage: "local", PolicyVersion: "fetch-v1", FactsVersion: baseline.UpdateVersion + 1, MaxBytes: brain.MaxInputBytes}
	session, err := taskcontext.NewSession(assembly, spec, baseline.Task)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	_, err = session.Assemble(ctx, baseline.Task, "local", brain.MaxInputBytes)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}

	// This goal source is separate from the web authorship/provenance.
	rules := []contentpolicy.Rule{}
	for _, source := range []*wire.ContentSource{{Kind: "web", Key: "start", Revision: 1}, {Kind: "web", Key: "final", Revision: 1}, {Kind: "task-goal", Key: "inline", Revision: 1}} {
		rules = append(rules, contentpolicy.Rule{Kind: source.Kind, Key: source.Key, Revision: source.Revision, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(10 * time.Minute).Unix()})
	}
	if err = h.policy.Replace(rules); err != nil {
		t.Fatal(err)
	}
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	output, err := answers.NewContentAccess(h.content, h.policy, binding, &wire.ContentSource{Kind: "task-goal", Key: "inline", Revision: 1}, "task", h.clock, 5*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	model := decisionFixtureModel{evidence: evidence}
	if revoke {
		model.beforeReturn = func() error { return h.policy.Replace(nil) }
	}
	newBrain := brain.NewAnswer
	if evidence {
		newBrain = brain.NewEvidenceAnswer
	}
	answerBrain, err := newBrain(model, session, output, generations, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: 1024, SettlementTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	decision := tasks.DecisionInput{Task: baseline.Task, Work: baseline.Work[0], Generation: baseline.Generations[len(baseline.Generations)-1]}
	proposal, decisionErr := answerBrain.Decide(ctx, decision)
	if revoke {
		if decisionErr == nil || proposal.Result != "" {
			t.Fatal("revoked generation published an answer")
		}
		if err = h.policy.Replace(rules); err != nil {
			t.Fatal(err)
		}
		_, err = h.content.Call(ctx, binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: decision.Generation.OutputOperation, Purpose: "task"})
		if artifacts.Code(err) != "PERMISSION_DENIED" {
			t.Fatal("revoked model result was saved")
		}
	} else {
		if decisionErr != nil || proposal.Kind != "answer" || proposal.Result == "" {
			t.Fatalf("Brain proposal: %v", decisionErr)
		}
		change := tasks.WorkChange{ChangeID: "publish-fetch-answer", Kind: "complete", Qualification: decision.Generation.Qualification, Proposal: proposal}
		port, err := answers.BindContextPort(generations, output, "local", session)
		if evidence {
			if err != nil {
				t.Fatal(err)
			}
			if _, mismatch := port.Recover(ctx, task.Ref); mismatch == nil {
				t.Fatal("ordinary answer recovery accepted evidence contract")
			}
			port, err = answers.BindEvidencePort(generations, output, "local", session)
		}
		if err != nil {
			t.Fatal(err)
		}
		if evidence {
			for replay := 0; replay < 2; replay++ {
				if _, err = port.Recover(ctx, task.Ref); err != nil {
					t.Fatal(err)
				}
			}
		} else {
			if err = generations.PreparePublication(ctx, change); err != nil {
				t.Fatal(err)
			}
			if _, err = generations.Commit(ctx, change); err != nil {
				t.Fatal(err)
			}
		}
		ref, err := answers.ParseReference(proposal.Result)
		if err != nil {
			t.Fatal(err)
		}
		record, err := h.content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
		if err != nil {
			t.Fatal(err)
		}
		read, err := h.content.Call(ctx, binding, &wire.ContentRequest{Method: "READ", Ref: ref, Purpose: "task", Limit: uint32(record.Record.Spec.Size)})
		if err != nil {
			t.Fatal(err)
		}
		if evidence {
			var result brain.EvidenceAnswer
			if json.Unmarshal(read.Data, &result) != nil || result.Status != "answerable" || len(result.Claims) != 1 || result.Claims[0].Citations[0].Quote != body {
				t.Fatal("evidence answer changed")
			}
		} else {
			var storedAnswer brain.Answer
			if json.Unmarshal(read.Data, &storedAnswer) != nil || storedAnswer.Text != "Fixture answer" || len(storedAnswer.Sources) != 1 || storedAnswer.Sources[0] != outcome.Reference {
				t.Fatal("published answer changed its original evidence citation")
			}
		}
		if record.GetRecord().GetSpec().GetRetainUntil() > h.now().Add(5*time.Minute).Unix() {
			t.Fatal("answer extended retention")
		}
		found := false
		for _, source := range record.Record.Spec.Sources {
			if source.Kind == "web" && source.Key == "start" {
				found = true
			}
		}
		if !found {
			t.Fatal("answer lost web source lineage")
		}
		current, err := h.core.Get(ctx, h.token, task.Ref)
		if err != nil || current.State != "COMPLETED" {
			t.Fatal("Brain answer did not complete Core task")
		}
		if err = h.policy.Replace(nil); err != nil {
			t.Fatal(err)
		}
		if _, err = h.content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"}); artifacts.Code(err) != "PERMISSION_DENIED" {
			t.Fatal("derived answer ignored source revocation")
		}
	}
	current, err := h.core.Get(ctx, h.token, task.Ref)
	if err != nil || current.ModelUsedRequests != 1 || current.ModelReservedRequests != 0 {
		t.Fatal("fixture generation did not settle its original reservation")
	}
}

// This local protocol fixture is not a language model or quality benchmark.
// It tests accounting/publication with deterministic output and a revocation hook.
func TestEvidenceBrainPublishesAndRecoversGovernedAnswer(t *testing.T) {
	for _, revoke := range []bool{false, true} {
		name := "recover"
		if revoke {
			name = "revoke"
		}
		t.Run(name, func(t *testing.T) { checkBrainFetch(t, revoke, true) })
	}
}
