//go:build fault

package fault_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G4、G5、G10、G11、V4
func TestReasonerDriverConfigurationRecoversExactCommittedCommand(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) { runDriverPlanningBoundary(t, "configuration", mode) })
	}
}

// 规则：G1、G2、G3、G4、G5、G10、G11、V4
func TestReasonerDriverNextRequestRecoversBeforePositionCheckpoint(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) { runDriverPlanningBoundary(t, "next-request", mode) })
	}
}

// 规则：G1、G2、G3、G4、G5、G10、G11、V4
func TestReasonerDriverPositionCheckpointRecoversCommittedNextRequest(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) { runDriverPlanningBoundary(t, "position-checkpoint", mode) })
	}
}

func runDriverPlanningBoundary(t *testing.T, window string, mode sqlite.FaultMode) {
	t.Helper()
	target := simulator.New("idempotent")
	targetServer := httptest.NewServer(target)
	defer targetServer.Close()
	provider := simulator.NewModelProvider()
	var action *v1.Proposal
	modelServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Error(err)
			http.Error(w, "body", 500)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var view struct {
			Facts struct {
				Facts []json.RawMessage `json:"facts"`
			} `json:"facts"`
		}
		if err = json.Unmarshal(body, &view); err != nil || len(view.Facts.Facts) == 0 {
			t.Errorf("actual snapshot unavailable: %v", err)
			http.Error(w, "snapshot", 500)
			return
		}
		snapshot := new(v1.ContextSnapshot)
		if err = protojson.Unmarshal(view.Facts.Facts[0], snapshot); err != nil {
			t.Error(err)
			http.Error(w, "snapshot", 500)
			return
		}
		output := action
		for _, fact := range snapshot.ProgressFacts {
			if proto.Equal(fact.CapabilityRef, action.Step.CapabilityRef) && fact.EffectOutcome == "APPLIED" && !fact.EvidenceConflict && !fact.ExecutionReportPending {
				output = &v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "SATISFIED", OperationId: fact.OperationRef.Name, EvidenceRefs: fact.EvidenceRefs}}, BasisRefs: fact.EvidenceRefs}
				break
			}
		}
		encoded, err := protojson.Marshal(output)
		if err != nil {
			t.Error(err)
			http.Error(w, "output", 500)
			return
		}
		provider.Output = string(encoded)
		provider.ServeHTTP(w, r)
	}))
	defer modelServer.Close()
	path := filepath.Join(t.TempDir(), "driver-planning.db")
	h, err := assembly.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	configuration, initial, proposal := preparePlanningDriver(t, h, targetServer.URL, modelServer.URL)
	action = proposal
	encoded, err := proto.Marshal(configuration)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path+".configuration", encoded, 0600); err != nil {
		t.Fatal(err)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	child := exec.Command(os.Args[0], "-test.run=^TestReasonerDriverPlanningBoundaryChild$", "-test.v")
	child.Env = append(os.Environ(), "LERNA_DRIVER_PLANNING_DB="+path, "LERNA_DRIVER_PLANNING_WINDOW="+window, "LERNA_DRIVER_PLANNING_MODE="+string(mode))
	output, err := child.CombinedOutput()
	if mode == sqlite.LoseReceipt {
		if err != nil {
			t.Fatalf("child: %v %s", err, output)
		}
	} else {
		var exited *exec.ExitError
		if !errors.As(err, &exited) || exited.ExitCode() != sqlite.CrashExitCode {
			t.Fatalf("fault missed: %v %s", err, output)
		}
	}
	t.Logf("actual child window=%s mode=%s error=%v output=%s", window, mode, err, output)
	// 先读取真实负责方公共状态；此装配只查询，不运行启动恢复。
	store, err := sqlite.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	owner := tasks.New(store, "u", "d")
	decisions := durable.New(store, "u", "d")
	receipt, err := decisions.QueryReceipt(ctx, caller, configuration.Header.Identity)
	if err != nil {
		t.Fatal(err)
	}
	driver, err := owner.QueryReasonerDriver(ctx, caller, configuration.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	var originalAction *v1.Operation
	var originalCall *v1.ModelCall
	var committedNext *v1.ContextSnapshot
	if window == "configuration" {
		if mode == sqlite.CrashBeforeCommit {
			if driver != nil || receipt.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND {
				t.Fatalf("uncommitted config leaked: %v %v", driver, receipt)
			}
		} else if driver == nil || receipt.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(driver.Ref, receipt.Receipt.ResultRef) || !proto.Equal(driver.ConfiguredBy, configuration.Header.Identity) || !driver.Enabled {
			t.Fatalf("committed config lost: %v %v", driver, receipt)
		}
		requests, effects := target.Snapshot()
		if provider.Calls() != 0 || len(provider.Bills()) != 0 || len(requests) != 0 || len(effects) != 0 {
			t.Fatal("configuration fault unexpectedly executed")
		}
		t.Logf("public config receipt=%s driver=%s independent model_calls=%d bills=%d target_calls=%d effects=%d", protojson.Format(receipt), protojson.Format(driver), provider.Calls(), len(provider.Bills()), len(requests), len(effects))
	} else {
		baseline := new(v1.ReasonerDriver)
		encoded, err := os.ReadFile(path + ".baseline")
		if err != nil {
			t.Fatal(err)
		}
		if err = proto.Unmarshal(encoded, baseline); err != nil {
			t.Fatal(err)
		}
		originalCall = new(v1.ModelCall)
		encoded, err = os.ReadFile(path + ".original-call")
		if err != nil {
			t.Fatal(err)
		}
		if err = proto.Unmarshal(encoded, originalCall); err != nil {
			t.Fatal(err)
		}
		if baseline.AdmissionRef == nil || baseline.OutcomeRef == nil || !proto.Equal(baseline.RequestRef, initial.RequestRef) {
			t.Fatalf("fault armed outside actual ACTION checkpoint: %v", baseline)
		}
		admitted, err := owner.QueryAdmission(ctx, caller, baseline.AdmissionRef)
		if err != nil {
			t.Fatal(err)
		}
		executionOwner, err := ledger.New(store, "u", "d/ledger", "d")
		if err != nil {
			t.Fatal(err)
		}
		originalAction, err = executionOwner.QueryOperation(ctx, caller, admitted.OperationId)
		if err != nil || originalAction == nil || originalAction.Lifecycle != "SETTLED" || originalAction.Dispatch != "SEALED" || originalAction.Effect.Outcome != "APPLIED" {
			t.Fatalf("fault preceded actual target completion: %v %v", originalAction, err)
		}
		actualPlanning, err := owner.QueryPlanning(ctx, caller, configuration.TaskId)
		if err != nil {
			t.Fatal(err)
		}
		nextIdentity := admissionHeader("driver-next:" + baseline.Ref.Name.LocalId + ":" + initial.RequestRef.Name.LocalId).Identity
		nextReceipt, err := decisions.QueryReceipt(ctx, caller, nextIdentity)
		if err != nil {
			t.Fatal(err)
		}
		if window == "next-request" && mode == sqlite.CrashBeforeCommit {
			if nextReceipt.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_NOT_FOUND || !proto.Equal(actualPlanning.Snapshot.RequestRef, initial.RequestRef) || actualPlanning.Snapshot.PlanningGeneration != initial.PlanningGeneration {
				t.Fatalf("uncommitted next request leaked: %v %v", nextReceipt, actualPlanning)
			}
		} else {
			if nextReceipt.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || nextReceipt.Receipt.GetDecision() != v1.Decision_DECISION_ACCEPTED {
				t.Fatalf("next request did not commit: %v", nextReceipt)
			}
			committedNext, err = owner.QuerySnapshot(ctx, caller, nextReceipt.Receipt.ResultRef)
			if err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(committedNext, actualPlanning.Snapshot) || committedNext.PlanningGeneration != initial.PlanningGeneration+1 || proto.Equal(committedNext.RequestRef, initial.RequestRef) {
				t.Fatalf("wrong committed next request: %v %v", committedNext, actualPlanning)
			}
		}
		if window == "position-checkpoint" && mode != sqlite.CrashBeforeCommit {
			if !proto.Equal(driver.RequestRef, committedNext.RequestRef) || driver.AdmissionRef != nil || driver.OutcomeRef != nil {
				t.Fatalf("committed position lost: %v", driver)
			}
		} else if !proto.Equal(driver.RequestRef, initial.RequestRef) || !proto.Equal(driver.AdmissionRef, baseline.AdmissionRef) || !proto.Equal(driver.OutcomeRef, baseline.OutcomeRef) {
			t.Fatalf("missed old driver position gap: %v", driver)
		}
		requests, effects := target.Snapshot()
		if len(requests) != 1 || len(effects) != 1 || provider.Calls() != 1 || len(provider.Bills()) != 1 {
			t.Fatalf("fault actual counts target=%d effects=%d model=%d bills=%d", len(requests), len(effects), provider.Calls(), len(provider.Bills()))
		}
		if requests[0].Operation != originalAction.Ref.Name.LocalId || requests[0].Attempt != originalAction.Execution.Attempt.Ref.Name.LocalId || requests[0].ExternalKey != originalAction.Execution.Attempt.ExternalKey || requests[0].Send != "1" {
			t.Fatalf("fault target identity mismatch: %+v", requests)
		}
		t.Logf("public targeted gap next_receipt=%s driver=%s planning=%s action=%s original_call=%s actual_requests=%+v effects=%+v model_calls=%d bills=%d", protojson.Format(nextReceipt), protojson.Format(driver), protojson.Format(actualPlanning), protojson.Format(originalAction), protojson.Format(originalCall), requests, effects, provider.Calls(), len(provider.Bills()))
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = assembly.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = h.Close() }()
	replay, err := h.Tasks.ConfigureReasonerDriver(ctx, caller, configuration)
	requireAccepted(t, replay, err)
	if mode != sqlite.CrashBeforeCommit && !proto.Equal(replay, receipt.Receipt) {
		t.Fatalf("configuration identity changed: %v %v", replay, receipt)
	}
	state, err := h.Tasks.AdvanceReasonerTask(ctx, caller, &v1.AdvanceReasonerTaskRequest{TaskId: configuration.TaskId, Limit: 16})
	if err != nil || state.State != "COMPLETED" {
		t.Fatalf("driver did not complete: %v %v", state, err)
	}
	if originalAction != nil {
		after, err := h.Ledger.QueryOperation(ctx, caller, originalAction.Ref.Name)
		if err != nil || !proto.Equal(after.AdmissionRef, originalAction.AdmissionRef) || !proto.Equal(after.Execution, originalAction.Execution) {
			t.Fatalf("recovery replaced original action/send/key: %v %v", after, err)
		}
		call, err := h.Tasks.QueryModelCall(ctx, caller, initial.RequestRef, 0)
		if err != nil || !proto.Equal(call.Ref, originalCall.Ref) || !proto.Equal(call.InputRef, originalCall.InputRef) || !proto.Equal(call.AdmissionRef, originalCall.AdmissionRef) {
			t.Fatalf("recovery replaced original model position: %v %v", call, err)
		}
	}
	if committedNext != nil && !proto.Equal(state.RequestRef, committedNext.RequestRef) {
		t.Fatalf("recovery replaced committed next request: %v %v", state, committedNext)
	}
	result, err := h.Tasks.QueryResult(ctx, caller, configuration.TaskId)
	if err != nil || result.GetOutcome() != "SUCCEEDED" || len(result.OperationRefs) != 3 {
		t.Fatalf("core result: %v %v", result, err)
	}
	planning, err := h.Tasks.QueryPlanning(ctx, caller, configuration.TaskId)
	if err != nil || planning.Snapshot.PlanningGeneration != initial.PlanningGeneration+1 || !proto.Equal(state.RequestRef, planning.Snapshot.RequestRef) {
		t.Fatalf("nonunique next request: %v %v", planning, err)
	}
	for _, ref := range []*v1.Ref{initial.RequestRef, state.RequestRef} {
		request, err := h.Tasks.QueryProposalRequest(ctx, caller, ref)
		if err != nil || len(request.OutcomeRefs) != 1 || len(request.ModelOperationRefs) != 1 {
			t.Fatalf("request responsibility: %v %v", request, err)
		}
		extra, err := h.Tasks.QueryModelCall(ctx, caller, ref, 1)
		if err != nil || extra != nil {
			t.Fatalf("extra position: %v %v", extra, err)
		}
	}
	requests, effects := target.Snapshot()
	if provider.Calls() != 2 || len(provider.Bills()) != 2 || len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("actual final counts models=%d bills=%d calls=%d effects=%d", provider.Calls(), len(provider.Bills()), len(requests), len(effects))
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = assembly.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	stable, err := h.Tasks.QueryResult(ctx, caller, configuration.TaskId)
	if err != nil || !proto.Equal(stable, result) || provider.Calls() != 2 || len(provider.Bills()) != 2 {
		t.Fatalf("unstable restart: %v %v", stable, err)
	}
	requests, effects = target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("restart repeated target")
	}
	t.Logf("public final driver=%s result=%s actual requests=%+v effects=%+v model_calls=%d bills=%d", protojson.Format(state), protojson.Format(result), requests, effects, provider.Calls(), len(provider.Bills()))
}

func preparePlanningDriver(t *testing.T, h *assembly.Harness, target, model string) (*v1.ConfigureReasonerDriverCommand, *v1.ContextSnapshot, *v1.Proposal) {
	t.Helper()
	ctx := context.Background()
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	submitted, err := h.Sessions.SubmitGoal(ctx, caller, &v1.SubmitGoalCommand{Identity: admissionHeader("planning-goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "create record"})
	if err != nil {
		t.Fatal(err)
	}
	if err = h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	received, err := h.Durable.QueryReceipt(ctx, caller, submitted.Identity)
	if err != nil {
		t.Fatal(err)
	}
	task := received.Receipt.TaskRef
	parameters, err := h.Content.Stage(ctx, caller, &v1.SubmitGoalCommand{Identity: admissionHeader("planning-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{"destination":"archive"}`})
	if err != nil {
		t.Fatal(err)
	}
	fee := int64(30)
	capability := &v1.Capability{Action: "CREATE", Resource: target, UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api", AdapterRef: &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "adapter", ObjectKind: "adapter", LocalId: "simulator-idempotent"}, Revision: 1, SchemaId: "lerna.v1.Adapter"}, Unit: "USD_MICRO", FeeCeiling: &fee, RateBasisRef: parameters, MaxSends: 1, ParameterSchemaJson: []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)}
	r, err := h.Tasks.ConfigureCapability(ctx, caller, &v1.ConfigureCapabilityCommand{Header: admissionHeader("planning-action-cap"), Capability: capability})
	requireAccepted(t, r, err)
	actionRef := r.ResultRef
	actualCap, err := h.Tasks.QueryCapability(ctx, caller, actionRef)
	if err != nil {
		t.Fatal(err)
	}
	r, err = h.Tasks.AcceptRequirements(ctx, caller, &v1.AcceptRequirementsCommand{Header: admissionHeader("planning-requirements"), TaskRef: task, InputVersion: 1, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1, TargetRecord: &v1.TargetRecordAssertion{CapabilityRef: actionRef, ParametersRef: parameters}}}})
	requireAccepted(t, r, err)
	for _, c := range []*v1.ConfigureBudgetCommand{{Header: admissionHeader("planning-user-budget"), Unit: "USD_MICRO", Limit: 100}, {Header: admissionHeader("planning-task-budget"), TaskId: task.Name, Unit: "USD_MICRO", Limit: 80}} {
		r, err = h.Budget.Configure(ctx, caller, c)
		requireAccepted(t, r, err)
	}
	grant := func(id, action, resource string) *v1.Ref {
		r, err := h.Grants.Configure(ctx, caller, &v1.ConfigureGrantCommand{Header: admissionHeader(id), Grant: &v1.Grant{Subject: task.Name, Permissions: []*v1.PermissionClause{{Action: action, Resource: resource, UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api"}}, ValidFromUnixMs: 1, ValidUntilUnixMs: 4000000000000, UseMode: "CONTINUOUS", UsePoolId: id}})
		requireAccepted(t, r, err)
		return r.ResultRef
	}
	actionGrant := grant("planning-action-grant", "CREATE", target)
	capability = proto.Clone(capability).(*v1.Capability)
	capability.Action = "MODEL_INFER"
	capability.Resource = model
	capability.AdapterRef.Name.LocalId = "model-reference-v1"
	capability.ParameterSchemaJson = nil
	capability.SchemaDigest = ""
	r, err = h.Tasks.ConfigureCapability(ctx, caller, &v1.ConfigureCapabilityCommand{Header: admissionHeader("planning-model-cap"), Capability: capability})
	requireAccepted(t, r, err)
	modelRef := r.ResultRef
	modelGrant := grant("planning-model-grant", "MODEL_INFER", model)
	snapshot, err := h.Tasks.RequestProposal(ctx, caller, &v1.RequestProposalCommand{Header: admissionHeader("planning-initial"), TaskId: task.Name})
	if err != nil {
		t.Fatal(err)
	}
	config := &v1.ConfigureReasonerDriverCommand{Header: admissionHeader("planning-driver-config"), TaskId: task.Name, Policy: &v1.ReasonerDriverPolicy{Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, ModelCapabilityRef: modelRef, ModelGrantRef: modelGrant, Actions: []*v1.ReasonerActionAuthority{{CapabilityRef: actionRef, GrantRef: actionGrant}}}}
	return config, snapshot, &v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "create", CapabilityRef: actionRef, SchemaDigest: actualCap.SchemaDigest, ParametersRef: parameters}, BasisRefs: []*v1.Ref{parameters}}
}

// 规则：G3、V4
func TestReasonerDriverPlanningBoundaryChild(t *testing.T) {
	path := os.Getenv("LERNA_DRIVER_PLANNING_DB")
	if path == "" {
		t.Skip("child only")
	}
	h, err := assembly.Open(path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	raw, err := os.ReadFile(path + ".configuration")
	if err != nil {
		t.Fatal(err)
	}
	configuration := new(v1.ConfigureReasonerDriverCommand)
	if err = proto.Unmarshal(raw, configuration); err != nil {
		t.Fatal(err)
	}
	mode := sqlite.FaultMode(os.Getenv("LERNA_DRIVER_PLANNING_MODE"))
	caller := &v1.Caller{UserId: "u", IssuerId: "host"}
	window := os.Getenv("LERNA_DRIVER_PLANNING_WINDOW")
	if window == "configuration" {
		ctx, err := sqlite.WithFault(context.Background(), "tasks.planning", mode)
		if err != nil {
			t.Fatal(err)
		}
		r, err := h.Tasks.ConfigureReasonerDriver(ctx, caller, configuration)
		if mode != sqlite.LoseReceipt || err == nil || r != nil {
			t.Fatalf("configuration fault did not fire: %v %v", r, err)
		}
		fmt.Printf("actual configuration lost receipt: %v\n", err)
		return
	}
	r, err := h.Tasks.ConfigureReasonerDriver(context.Background(), caller, configuration)
	requireAccepted(t, r, err)
	var baseline *v1.ReasonerDriver
	for stage := 0; stage < 8; stage++ {
		baseline, err = h.Tasks.AdvanceReasonerTask(context.Background(), caller, &v1.AdvanceReasonerTaskRequest{TaskId: configuration.TaskId, Limit: 1})
		if err != nil {
			t.Fatal(err)
		}
		if baseline.AdmissionRef != nil {
			break
		}
	}
	if baseline == nil || baseline.AdmissionRef == nil || baseline.OutcomeRef == nil {
		t.Fatalf("actual driver never admitted action: %v", baseline)
	}
	call, err := h.Tasks.QueryModelCall(context.Background(), caller, baseline.RequestRef, 0)
	if err != nil || call == nil {
		t.Fatalf("original model call missing: %v %v", call, err)
	}
	for name, message := range map[string]proto.Message{".baseline": baseline, ".original-call": call} {
		encoded, err := proto.Marshal(message)
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(path+name, encoded, 0600); err != nil {
			t.Fatal(err)
		}
	}
	occurrence := uint64(1)
	if window == "position-checkpoint" {
		occurrence = 2
	}
	ctx, err := sqlite.WithFaultOnOccurrence(context.Background(), "tasks.planning", mode, occurrence)
	if err != nil {
		t.Fatal(err)
	}
	state, err := h.Tasks.AdvanceReasonerTask(ctx, caller, &v1.AdvanceReasonerTaskRequest{TaskId: configuration.TaskId, Limit: 1})
	if mode != sqlite.LoseReceipt || !sqlite.FaultTriggered(ctx) {
		t.Fatalf("next-request fault did not fire: %v %v", state, err)
	}
	fmt.Printf("actual next-request lost receipt state=%v error=%v\n", state, err)
}
