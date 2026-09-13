package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/adapters/acquisitionexecution"
	"lerna/adapters/executionlocal"
	"lerna/adapters/fetchtask"
	"lerna/execution"
	"lerna/sdk"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSDKWaitingRecoveryKeepsOriginalExecutionQueryBudget(t *testing.T) {
	checkSDKWaitingRecovery(t, "sdk")
}

func TestActionHostStartsOriginalAdmittedExecution(t *testing.T) {
	checkSDKWaitingRecovery(t, "admitted")
}

func TestActionHostAdmitsOriginalDispatchedExecution(t *testing.T) {
	checkSDKWaitingRecovery(t, "dispatched")
}

func TestActionHostRejectsChangedDispatchedRequest(t *testing.T) {
	checkSDKWaitingRecovery(t, "changed")
}

func checkSDKWaitingRecovery(t *testing.T, stage string) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Actual recovered evidence."))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	raw, _ := json.Marshal(map[string]any{"url": h.urls[0], "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	input, err := h.put(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	submit, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: submit, Goal: "Acquire bounded evidence", InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: 2, ModelRequests: 2, ModelTokens: 16384, DeadlineUnix: h.now().Add(time.Minute).Unix()}})
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
	limits := tasks.ActionLimits{MaxOperations: 2, MaxQueries: 32, InputTokens: 8192, OutputTokens: 1024}
	port, err := h.work.Actions(limits)
	if err != nil {
		t.Fatal(err)
	}
	run, err = port.Initialize(ctx, tasks.QualificationOf(run))
	if err != nil {
		t.Fatal(err)
	}
	q := tasks.QualificationOf(run)
	decision, err := port.Reserve(ctx, q)
	if err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	err = port.Record(ctx, q, decision.Number, tasks.DecisionRecord{Evidence: "recorded decision", Usage: tasks.GenerationUsage{Requests: 1, Tokens: 10}, Proposal: tasks.ActionProposal{Kind: "actions", Actions: []tasks.Action{{Key: "fetch", OperationID: op, Descriptor: h.cap.Digest(), InputRef: input, ResourceVersion: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	run, err = port.Admit(ctx, q, decision.Number)
	if err != nil {
		t.Fatal(err)
	}
	action, err := port.Next(ctx, tasks.QualificationOf(run))
	if err != nil {
		t.Fatal(err)
	}
	request := execution.Request{OperationID: op, Qualification: action.Qualification, Capability: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, DescriptorSHA256: action.Descriptor, InputRef: input, ResourceVersion: 1}
	bind := func(work *tasks.WorkPort, queries *tasks.ActionPort) {
		guard, e := fetchtask.New(h.auth, work, h.core, h.token)
		if e != nil {
			t.Fatal(e)
		}
		scope, e := acquisitionQueries(h, queries, h.cap)
		if e != nil {
			t.Fatal(e)
		}
		driver, e := acquisitionexecution.NewPage(h.http, h.attempts, h.evidence, h.auth, acquisitionexecution.Config{Observations: scope, Guard: guard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: 1024, MaxRequests: 2, TaskLimit: 2, Timeout: time.Second})
		if e != nil {
			t.Fatal(e)
		}
		content, e := h.access.WithQueries(queries)
		if e != nil {
			t.Fatal(e)
		}
		h.exec, e = execution.New(h.grants, work, content, driver, h.binding, h.cap, config(), h.operation)
		if e != nil {
			t.Fatal(e)
		}
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	}
	bind(h.work, port)
	if stage == "sdk" || stage == "admitted" {
		grant, err := h.issue(ctx, request)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = h.client.Invoke(ctx, request, grant); err != nil {
			t.Fatal(err)
		}
	}
	run, err = h.core.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	expectedInitialQueries := 1
	if stage == "dispatched" || stage == "changed" {
		expectedInitialQueries = 0
	}
	if len(run.Actions.Queries) != expectedInitialQueries {
		t.Fatal("initial invocation reads were not charged")
	}
	run, err = port.Wait(ctx, tasks.QualificationOf(run), "reconciliation")
	if err != nil {
		t.Fatal(err)
	}
	// Reopen actual SQLite and content stores after admission and waiting.
	// Rebuild both consumers with the explicitly retained recovery qualification.
	root, token, urls := h.root, h.token, h.urls
	h.close()
	h, err = open(ctx, root, token, urls)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	recovery := h.work.WithActionRecovery(tasks.QualificationOf(run))
	queries, err := recovery.Actions(limits)
	if err != nil {
		t.Fatal(err)
	}
	bind(recovery, queries)
	var record execution.Record
	if stage != "sdk" {
		env := &fetchActionHost{h: h}
		recoveryAction := action
		if stage == "changed" {
			recoveryAction.ResourceVersion++
		}
		err = env.Recover(ctx, run.Task, recoveryAction)
		if stage == "changed" {
			if err == nil || requests.Load() != 0 {
				t.Fatal("changed original action was admitted or dispatched")
			}
			return
		}
		if err == nil {
			record, err = h.exec.GetInvocation(ctx, op)
		}
	} else {
		record, err = h.exec.Run(ctx, op)
	}
	if err != nil {
		t.Fatal(err)
	}
	outcome, known, err := h.attempts.Outcome(ctx, "local", op)
	if err != nil || !known || outcome.Status != "acquired" || record.Result != "SUCCESS" {
		t.Fatalf("waiting recovery failed to acquire evidence: status=%s known=%v result=%s err=%v", outcome.Status, known, record.Result, err)
	}
	after, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 1 || len(after.Actions.Queries) != 7 || after.Actions.Actions[0].Qualification != request.Qualification || record.Request != request {
		t.Fatal("recovery changed original request or query accounting")
	}
}
