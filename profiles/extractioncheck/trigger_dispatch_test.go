package extractioncheck

import (
	"context"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	executionlocal "lerna/adapters/execution/local"
	extractionauth "lerna/adapters/extraction/auth"
	extractionexecution "lerna/adapters/extraction/execution"
	extractioninputs "lerna/adapters/extraction/inputs"
	localextraction "lerna/adapters/extraction/rules"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/authorization"
	"lerna/execution"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/sdk"
	"lerna/tasks"
	"testing"
	"time"
)

func enableScan(t *testing.T, ctx context.Context, h *harness) {
	t.Helper()
	snapshot, err := h.db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	scope := proto.Clone(snapshot.State.Rules[0].Scope).(*wire.AuthorizationScope)
	scope.Actions = append(scope.Actions, "memory.scan")
	for _, command := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "scan", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "scan", Subject: "operator", Scope: scope, Mode: "continuous"}}}} {
		snapshot, err = h.db.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		command.ExpectedRevision = snapshot.State.Revision
		op, e := h.operation(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); e != nil {
			t.Fatal(e)
		}
	}
}

func TestTriggerDispatchSubmitsOriginalCoreTaskAndRechecksCancellation(t *testing.T) {
	checkTriggeredExecution(t, true, false, false)
}
func TestBoundTriggerTaskActuallyExtractsLocalCandidate(t *testing.T) {
	checkTriggeredExecution(t, false, false, false)
}
func TestTriggeredTaskAutomaticallySavesMemory(t *testing.T) {
	checkTriggeredExecution(t, false, true, false)
}
func TestCancelledTriggerCannotSaveRetainedCandidate(t *testing.T) {
	checkTriggeredExecution(t, false, true, true)
}
func TestRevokedScanCannotSaveRetainedCandidate(t *testing.T) {
	checkTriggeredExecution(t, false, true, true, func(ctx context.Context, h *harness) error {
		if err := revokeSaveAction(ctx, h, "memory.scan"); err != nil {
			return err
		}
		assertOrdinaryExtractionAllowed(t, ctx, h)
		return nil
	})
}

func TestExpiredTriggerCannotSaveRetainedCandidate(t *testing.T) {
	checkTriggeredExecution(t, false, true, true, func(ctx context.Context, h *harness) error {
		h.clock.advance(10 * time.Minute)
		assertOrdinaryExtractionAllowed(t, ctx, h)
		return nil
	})
}

func assertOrdinaryExtractionAllowed(t *testing.T, ctx context.Context, h *harness) {
	t.Helper()
	a, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	for _, phase := range []string{"read", "extract", "retain", "save", "disclose"} {
		if err = a.Check(ctx, b, phase); err != nil {
			t.Fatalf("ordinary %s permission unexpectedly denied: %v", phase, err)
		}
	}
}

func checkTriggeredExecution(t *testing.T, cancelBefore, save, cancelBeforeSave bool, beforeSave ...func(context.Context, *harness) error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	enableScan(t, ctx, h)
	auth, err := extractionauth.New(h.auth, extractionauth.Scope{Namespace: "local", Resource: "root", Purpose: "task", Location: "local"})
	if err != nil {
		t.Fatal(err)
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	registry, err := extraction.NewTriggerRegistry(h.candidates, auth, h.clock, b, "task")
	if err != nil {
		t.Fatal(err)
	}
	spec := extraction.TriggerSpec{ID: "source-scan", Condition: "source.changed", Sources: []extraction.SourceScope{{Kind: "note", Key: "one"}}, MaxRounds: 2, MaxSteps: 3, ExpiresUnix: h.now().Add(10 * time.Minute).Unix()}
	if err = registry.Register(ctx, spec); err != nil {
		t.Fatal(err)
	}
	dispatcher, err := extraction.NewTriggerDispatcher(h.candidates, auth, h.clock, b, h.core, triggerInputs{h}, h.operation)
	if err != nil {
		t.Fatal(err)
	}
	event := &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}
	first, err := dispatcher.Dispatch(ctx, spec.ID, event)
	if err != nil {
		t.Fatal(err)
	}
	second, err := dispatcher.Dispatch(ctx, spec.ID, event)
	if err != nil || first.Ref != second.Ref || first.Version != second.Version {
		t.Fatalf("duplicate trigger replaced task: %+v %v", second, err)
	}
	round, err := h.candidates.GetTriggerRound(ctx, "local", "operator", spec.ID, event)
	if err != nil {
		t.Fatal(err)
	}
	original, err := h.core.Submit(ctx, h.token, round.Submission)
	if err != nil || original.Ref != first.Ref {
		t.Fatalf("original Core submission mismatch: %v", err)
	}
	guard, err := extraction.NewTriggerGuard(h.candidates, auth, h.clock, b, spec.ID)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := extractionexecution.New(h.candidates, guard, h.source, localextraction.Rules{}, h.clock, b, "task")
	if err != nil {
		t.Fatal(err)
	}
	bound, err := extractionexecution.BindTrigger(driver, h.candidates, h.core, spec.ID, event)
	if err != nil {
		t.Fatal(err)
	}
	var active execution.Driver = bound
	var saved *extraction.Saver
	var memoryStore *sqlitememory.Store
	if save {
		memoryStore, saved = triggerSaver(t, h, guard)
		if cancelBeforeSave {
			active = cancelBeforeSaving{Driver: bound, h: h, trigger: spec.ID}
			if len(beforeSave) != 0 {
				active = beforeMemorySave{Driver: bound, h: h, change: beforeSave[0]}
			}
		}
		active, err = extractionexecution.WithMemory(active, saved)
		if err != nil {
			t.Fatal(err)
		}
	}
	h.exec, err = execution.New(h.grants, h.work, h.access, active, h.binding, h.cap, config(), h.operation)
	if err != nil {
		t.Fatal(err)
	}
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	snapshot, err := h.core.Load(ctx, first.Ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"claim", "start"} {
		snapshot, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(snapshot)})
		if err != nil {
			t.Fatal(err)
		}
	}
	controls, err := h.core.Controls(controlLimits())
	if err != nil {
		t.Fatal(err)
	}
	if _, err = controls.PrepareDisposition(ctx, h.token, first.Ref); err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	request := execution.Request{OperationID: op, Qualification: tasks.QualificationOf(snapshot), Capability: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, DescriptorSHA256: h.cap.Digest(), InputRef: round.Submission.InputRefs[0], ResourceVersion: 1}
	otherOp, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	other, err := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: otherOp, Goal: "Independent task", InputRefs: round.Submission.InputRefs, Constraints: round.Submission.Constraints})
	if err != nil {
		t.Fatal(err)
	}
	wrong := request
	wrong.Qualification.Ref = other.Ref
	input := []byte(`{"sources":[{"kind":"note","key":"one","revision":1}]}`)
	if err = bound.Start(ctx, execution.Call{Request: wrong, Input: input}); err != memory.Denied {
		t.Fatalf("another task borrowed trigger: %v", err)
	}
	wrong = request
	wrong.InputRef = "content:replacement"
	if err = bound.Start(ctx, execution.Call{Request: wrong, Input: input}); err != memory.Denied {
		t.Fatalf("replacement trigger input: %v", err)
	}
	for _, body := range []string{`{"Sources":[{"kind":"note","key":"one","revision":1}]}`, `{"sources":[{"kind":"note","key":"one","key":"one","revision":1}]}`, `{"sources":[{"kind":"note","key":"one","revision":1},{"kind":"note","key":"one","revision":1}]}`} {
		if err = bound.Start(ctx, execution.Call{Request: request, Input: []byte(body)}); err != memory.Invalid {
			t.Fatalf("ambiguous trigger input: %v", err)
		}
	}
	grant, err := h.issue(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	if cancelBefore {
		if err = h.candidates.CancelTrigger(ctx, "local", "operator", spec.ID); err != nil {
			t.Fatal(err)
		}
	}
	outcome, err := h.exec.Run(ctx, request.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	if cancelBeforeSave {
		if outcome.Result == "SUCCESS" || outcome.Reference != "" {
			t.Fatal("cancelled trigger saved successfully")
		}
		candidate, e := h.candidates.Inspect(ctx, "local", request.OperationID)
		if e != nil || !candidate.Committed {
			t.Fatalf("lost original candidate effect: %v", e)
		}
		if _, e = saved.Inspect(ctx, request.OperationID); e != memory.Missing {
			t.Fatalf("cancelled trigger created save intent: %v", e)
		}
		changes, e := memoryStore.ReadChanges(ctx, "local", "personal", 0, 16)
		if e != nil || len(changes) != 0 {
			t.Fatalf("cancelled trigger saved Memory: %v", e)
		}
		return
	}
	if cancelBefore {
		if outcome.Result == "SUCCESS" || outcome.Reference != "" {
			t.Fatalf("cancelled scan published result: %s/%s", outcome.Result, outcome.Effect)
		}
		if _, err = h.candidates.Lookup(ctx, "local", request.OperationID); err != memory.Missing {
			t.Fatalf("cancelled scan retained candidate: %v", err)
		}
	} else {
		if err = h.exec.Drain(ctx, 16); err != nil {
			t.Fatal(err)
		}
		task, e := h.core.Get(ctx, h.token, first.Ref)
		if e != nil || task.State != "COMPLETED" || outcome.Result != "SUCCESS" {
			t.Fatalf("triggered execution: %s %v", task.State, e)
		}
		candidate, e := h.candidates.Lookup(ctx, "local", request.OperationID)
		if e != nil || candidate.Candidate.Kind != "preference" || candidate.Candidate.Value != "concise" {
			t.Fatalf("actual triggered extraction: %+v %v", candidate, e)
		}
		if save {
			state, e := saved.Inspect(ctx, request.OperationID)
			if e != nil || state.State != "committed" || state.Receipt == nil {
				t.Fatalf("triggered Memory save: %s %v", state.State, e)
			}
			row, e := memoryStore.Read(ctx, state.Receipt.Ref, 1)
			if e != nil || len(row.Document) == 0 {
				t.Fatalf("triggered Memory body missing: %v", e)
			}
			record := new(wire.MemoryRecord)
			if e = protojson.Unmarshal(row.Document, record); e != nil || record.GetSpec().GetKind() != "preference" || string(record.GetSpec().GetContent().GetJson()) != `{"attribute":"format","value":"concise"}` || len(record.GetSpec().GetSources()) != 1 || !proto.Equal(record.Spec.Sources[0].Ref, event) {
				t.Fatalf("triggered Memory content or provenance mismatch: %v", e)
			}
			replay, e := saved.Save(ctx, request.OperationID)
			if e != nil || replay.Receipt == nil || *replay.Receipt != *state.Receipt {
				t.Fatalf("triggered save replay: %v", e)
			}
			changes, e := memoryStore.ReadChanges(ctx, "local", "personal", 0, 16)
			if e != nil || len(changes) != 1 {
				t.Fatalf("duplicate triggered save: %v", e)
			}
		}
		if err = h.candidates.CancelTrigger(ctx, "local", "operator", spec.ID); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = dispatcher.Dispatch(ctx, spec.ID, event); err != memory.Denied {
		t.Fatalf("cancelled dispatch: %v", err)
	}
}

// The reference host creates a controlled metadata input, never a source body
// or model-generated policy. Its lifetime is bounded by the trigger deadline.
type triggerInputs struct{ h *harness }

func (p triggerInputs) Prepare(ctx context.Context, r extraction.TriggerRecord, event *wire.ContentSource, operation string) (tasks.Submission, error) {
	preparer, err := extractioninputs.New(p.h.content, p.h.clock, memory.Binding{Token: p.h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}, "root", p.h.operation)
	if err != nil {
		return tasks.Submission{}, err
	}
	return preparer.Prepare(ctx, r, event, operation)
}
