package admission_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G10、G11、R6
func TestProductionDriverConsumesReadyProposalAndReplaysOriginalPosition(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lerna")
	if out, e := exec.Command("go", "build", "-o", binary, "../../cmd/lerna").CombinedOutput(); e != nil {
		t.Fatalf("build %v %s", e, out)
	}
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"Which destination?","changesBasis":true}}`
	f := modelFixtureTarget(t, provider)
	snapshot, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("driver-existing-ready"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, snapshot.RequestRef)
	if e != nil {
		t.Fatal(e)
	}
	job, e := f.h.Durable.QueryJob(f.ctx, f.caller, request.JobRef.Name)
	if e != nil || job.State != "READY" {
		t.Fatalf("not original READY: %v %v", job, e)
	}
	configuration := writeReasonerCLIJSON(t, "driver.json", &v1.ConfigureReasonerDriverCommand{Header: header("driver-configure"), TaskId: f.task.Name, Policy: driverPolicy(f)})
	advance := writeReasonerCLIJSON(t, "advance.json", &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name})
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	invoke := func(action, file string) []byte {
		t.Helper()
		out, e := exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host", action, "--json", file).CombinedOutput()
		if e != nil {
			t.Fatalf("%s: %v %s", action, e, out)
		}
		return out
	}
	receipt := new(v1.CommandReceipt)
	if e = protojson.Unmarshal(invoke("configure-reasoner", configuration), receipt); e != nil || receipt.Error != nil {
		t.Fatalf("configuration: %v %v", receipt, e)
	}
	state := new(v1.ReasonerDriver)
	if e = protojson.Unmarshal(invoke("advance-task", advance), state); e != nil || state.State != "WAITING" || state.WaitingReason != "SESSION_REQUIRED" || !proto.Equal(state.RequestRef, snapshot.RequestRef) || state.OutcomeRef == nil || state.ContractVersion != 1 || state.ImplementationVersion != "default-v1" {
		t.Fatalf("automatic original proposal: %v %v", state, e)
	}
	original := proto.Clone(state).(*v1.ReasonerDriver)
	if e = protojson.Unmarshal(invoke("advance-task", advance), state); e != nil || !proto.Equal(original, state) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatalf("restart replay: %v %v calls=%d bills=%d", state, e, provider.Calls(), len(provider.Bills()))
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	request, e = f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, snapshot.RequestRef)
	if e != nil || len(request.ModelOperationRefs) != 1 || len(request.OutcomeRefs) != 1 {
		t.Fatalf("original request: %v %v", request, e)
	}
	job, e = f.h.Durable.QueryJob(f.ctx, f.caller, request.JobRef.Name)
	if e != nil || job.State != "COMPLETED" || job.ClaimEpoch != 1 || job.ProcessInstance == "" {
		t.Fatalf("real claim: %v %v", job, e)
	}
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, snapshot.RequestRef, 0)
	if e != nil || call.Result.GetStatus() != "COMPLETED" {
		t.Fatalf("original position: %v %v", call, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Settled != 7 || budget.Reserved != 0 {
		t.Fatalf("original charge: %v %v", budget, e)
	}
}

func driverPolicy(f *fixture) *v1.ReasonerDriverPolicy {
	return &v1.ReasonerDriverPolicy{Settings: &v1.ModelSettings{Provider: "reference", Model: "model-v1", EncoderVersion: "reference-model-v1", PolicyVersion: "m1-v1", ParametersJson: []byte(`{}`), ToolSchemasJson: []byte(`[]`), MaxOutputTokens: 100}, ModelCapabilityRef: f.capability, ModelGrantRef: f.grant}
}

// 规则：G1、G2、G3、G4、G5、G8、G9、G10、G11、R7
func TestProductionDriverExecutesActionAndRequestsNextFromActualEffect(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lerna")
	if out, e := exec.Command("go", "build", "-o", binary, "../../cmd/lerna").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	target := simulator.New("idempotent")
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var releaseOnce sync.Once
	releaseTarget := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(releaseTarget)
	target.SetWriteResponseGate(entered, release)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	parameters, e := f.h.Content.Stage(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("driver-action-parameters").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: `{"destination":"archive"}`})
	if e != nil {
		t.Fatal(e)
	}
	f.parameters = parameters
	capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	capability.Ref = nil
	capability.ApprovedBy = nil
	capability.ParameterSchemaJson = []byte(`{"type":"object","properties":{"destination":{"type":"string"}},"required":["destination"],"additionalProperties":false}`)
	configured, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("driver-action-cap"), Capability: capability})
	accepted(t, configured, e)
	f.capability = configured.ResultRef
	capability, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	targetCapability, targetGrant := f.capability, f.grant
	scopeRequirement(t, f)
	action := &v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "create", CapabilityRef: targetCapability, SchemaDigest: capability.SchemaDigest, ParametersRef: parameters}, BasisRefs: []*v1.Ref{parameters}}
	provider := simulator.NewModelProvider()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
			http.Error(w, "read", 500)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var view struct {
			Facts struct {
				Facts []json.RawMessage `json:"facts"`
			} `json:"facts"`
		}
		if e = json.Unmarshal(body, &view); e != nil || len(view.Facts.Facts) == 0 {
			t.Errorf("model received no actual snapshot: %v", e)
			http.Error(w, "snapshot", 500)
			return
		}
		snapshot := new(v1.ContextSnapshot)
		if e = protojson.Unmarshal(view.Facts.Facts[0], snapshot); e != nil {
			t.Error(e)
			http.Error(w, "snapshot", 500)
			return
		}
		output := action
		for _, fact := range snapshot.ProgressFacts {
			if proto.Equal(fact.CapabilityRef, targetCapability) && fact.EffectOutcome == "APPLIED" && !fact.EvidenceConflict && !fact.ExecutionReportPending {
				output = &v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "SATISFIED", OperationId: fact.OperationRef.Name, EvidenceRefs: fact.EvidenceRefs}}, BasisRefs: fact.EvidenceRefs}
				break
			}
		}
		encoded, e := protojson.Marshal(output)
		if e != nil {
			t.Error(e)
			http.Error(w, "output", 500)
			return
		}
		provider.Output = string(encoded)
		provider.ServeHTTP(w, r)
	}))
	defer endpoint.Close()
	capability.Ref = nil
	capability.ApprovedBy = nil
	capability.Action = "MODEL_INFER"
	capability.Resource = endpoint.URL
	capability.AdapterRef.Name.LocalId = "model-reference-v1"
	capability.ParameterSchemaJson = nil
	capability.SchemaDigest = ""
	configured, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("driver-model-cap"), Capability: capability})
	accepted(t, configured, e)
	f.capability = configured.ResultRef
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grant.UsePoolId = "driver-model"
	grant.Permissions[0].Action = "MODEL_INFER"
	grant.Permissions[0].Resource = endpoint.URL
	configured, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("driver-model-grant"), Grant: grant})
	accepted(t, configured, e)
	f.grant = configured.ResultRef
	snapshot, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("driver-action-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	policy := driverPolicy(f)
	policy.Actions = []*v1.ReasonerActionAuthority{{CapabilityRef: targetCapability, GrantRef: targetGrant}}
	configured, e = f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, &v1.ConfigureReasonerDriverCommand{Header: header("driver-action-config"), TaskId: f.task.Name, Policy: policy})
	accepted(t, configured, e)
	advance := writeReasonerCLIJSON(t, "advance.json", &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 16})
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	invoke := func() *v1.ReasonerDriver {
		t.Helper()
		out, e := exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host", "advance-task", "--json", advance).CombinedOutput()
		result := new(v1.ReasonerDriver)
		if e != nil || protojson.Unmarshal(out, result) != nil {
			t.Fatalf("advance: %v %s", e, out)
		}
		return result
	}
	type cliResult struct {
		body []byte
		err  error
	}
	firstCLI := make(chan cliResult, 1)
	go func() {
		body, err := exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host", "advance-task", "--json", advance).CombinedOutput()
		firstCLI <- cliResult{body, err}
	}()
	select {
	case <-entered:
	case <-time.After(30 * time.Second):
		t.Fatal("driver did not reach actual target")
	}
	// 独立宿主在 P5 已保存、真实目标尚未回报时只能等待原责任。
	waiting := invoke()
	if waiting.State != "WAITING" || waiting.WaitingReason != "OPERATION_PENDING" || !proto.Equal(waiting.RequestRef, snapshot.RequestRef) || provider.Calls() != 1 {
		t.Fatalf("in-flight responsibility advanced: %v", waiting)
	}
	releaseTarget()
	completed := <-firstCLI
	driver := new(v1.ReasonerDriver)
	if completed.err != nil || protojson.Unmarshal(completed.body, driver) != nil {
		t.Fatalf("first host: %v %s", completed.err, completed.body)
	}
	if driver.State != "COMPLETED" {
		t.Fatalf("production task did not complete: %v", driver)
	}
	if proto.Equal(snapshot.RequestRef, driver.RequestRef) {
		t.Fatal("actual progress did not create next request")
	}
	requests, effects := target.Snapshot()
	if provider.Calls() != 2 || len(provider.Bills()) != 2 || len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("actual counts: models=%d bills=%d calls=%d effects=%d", provider.Calls(), len(provider.Bills()), len(requests), len(effects))
	}
	again := invoke()
	if !proto.Equal(driver, again) || provider.Calls() != 2 {
		t.Fatalf("restart changed result or resampled: %v", again)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || len(result.OperationRefs) != 3 {
		t.Fatalf("core result: %v %v", result, e)
	}
	first, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, snapshot.RequestRef)
	if e != nil || len(first.OutcomeRefs) != 1 || len(first.ModelOperationRefs) != 1 {
		t.Fatalf("first responsibility: %v %v", first, e)
	}
	next, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, driver.RequestRef)
	if e != nil || len(next.OutcomeRefs) != 1 || len(next.ModelOperationRefs) != 1 {
		t.Fatalf("next responsibility: %v %v", next, e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || planning.Snapshot.PlanningGeneration != snapshot.PlanningGeneration+1 {
		t.Fatalf("nonunique continuation: %v %v", planning, e)
	}
	for _, ref := range []*v1.Ref{snapshot.RequestRef, driver.RequestRef} {
		call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, ref, 1)
		if e != nil || call != nil {
			t.Fatalf("extra position: %v %v", call, e)
		}
	}
}

// 规则：G3、G4、G5、G10、准入-7、准入-8、准入-9
func TestProductionDriverWaitsWithoutAuthorityAndNeverRetriesUnknown(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lerna")
	if out, e := exec.Command("go", "build", "-o", binary, "../../cmd/lerna").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	for _, mode := range []string{"unconfigured", "ordinary-caller", "missing-grant", "confirmation", "budget", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			provider.Output = `{"kind":"QUESTION","question":{"question":"Which destination?"}}`
			provider.Drop = mode == "unknown"
			f := modelFixtureTarget(t, provider)
			snapshot, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("waiting-original"), TaskId: f.task.Name})
			if e != nil {
				t.Fatal(e)
			}
			policy := driverPolicy(f)
			if mode == "missing-grant" {
				policy.ModelGrantRef = proto.Clone(f.grant).(*v1.Ref)
				policy.ModelGrantRef.Name.LocalId = "missing-grant"
			}
			if mode == "confirmation" {
				grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
				if e != nil {
					t.Fatal(e)
				}
				grant.Ref = nil
				grant.Issuer = nil
				grant.Status = ""
				grant.ConfirmationRequired = true
				grant.UsePoolId = "confirm-driver"
				configured, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("confirmation-driver-grant"), Grant: grant})
				accepted(t, configured, e)
				policy.ModelGrantRef = configured.ResultRef
			}
			if mode == "budget" {
				budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
				if e != nil {
					t.Fatal(e)
				}
				r, e := f.h.Budget.AdjustLimit(f.ctx, f.caller, &v1.AdjustBudgetLimitCommand{Header: header("driver-low-budget"), ExpectedRef: budget.Ref, Limit: 1, Reason: "controlled test limit"})
				accepted(t, r, e)
			}
			configuration := &v1.ConfigureReasonerDriverCommand{Header: header("waiting-driver-config"), TaskId: f.task.Name, Policy: policy}
			if mode != "unconfigured" && mode != "ordinary-caller" {
				r, e := f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, configuration)
				accepted(t, r, e)
			}
			configFile := writeReasonerCLIJSON(t, "configure.json", configuration)
			advance := writeReasonerCLIJSON(t, "advance.json", &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name})
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			if mode == "ordinary-caller" {
				out, e := exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "configure-reasoner", "--json", configFile).CombinedOutput()
				if e == nil || !bytes.Contains(out, []byte("PERMISSION_DENIED")) {
					t.Fatalf("caller elevated: %v %s", e, out)
				}
			}
			for i := 0; i < 2; i++ {
				out, e := exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host", "advance-task", "--json", advance).CombinedOutput()
				state := new(v1.ReasonerDriver)
				if e != nil || protojson.Unmarshal(out, state) != nil || state.State != "WAITING" {
					t.Fatalf("wait %d: %v %s", i, e, out)
				}
				if mode == "unconfigured" || mode == "ordinary-caller" {
					if state.WaitingReason != "CONFIGURATION_REQUIRED" || state.Ref != nil {
						t.Fatalf("unexpected responsibility: %v", state)
					}
				} else if !proto.Equal(state.RequestRef, snapshot.RequestRef) {
					t.Fatalf("replaced request: %v", state)
				}
				if mode == "unknown" && state.WaitingReason != "UNKNOWN" {
					t.Fatalf("unknown changed: %v", state)
				}
			}
			expectedCalls := 0
			if mode == "unknown" {
				expectedCalls = 1
			}
			if provider.Calls() != expectedCalls || len(provider.Bills()) != expectedCalls {
				t.Fatalf("physical calls/bills: %d %d", provider.Calls(), len(provider.Bills()))
			}
			f.h, e = assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil || !proto.Equal(planning.Snapshot.RequestRef, snapshot.RequestRef) || len(planning.AdmissionRefs) != expectedCalls {
				t.Fatalf("waiting progress changed: %v %v", planning, e)
			}
			extra, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, snapshot.RequestRef, 1)
			if e != nil || extra != nil {
				t.Fatalf("extra position: %v %v", extra, e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			expectedReserved := int64(0)
			if mode == "unknown" {
				expectedReserved = 30
			}
			if e != nil || budget.Settled != 0 || budget.Reserved != expectedReserved {
				t.Fatalf("fees lost or invented: %v %v", budget, e)
			}
		})
	}
}

// 规则：G3、G4、G5、G10、准入-8
func TestDriverResumesSamePreparedPositionAfterExplicitBudgetRepair(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"Which destination?"}}`
	f := modelFixtureTarget(t, provider)
	snapshot, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("repair-original"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e := f.h.Budget.AdjustLimit(f.ctx, f.caller, &v1.AdjustBudgetLimitCommand{Header: header("repair-limit-low"), ExpectedRef: budget.Ref, Limit: 1, Reason: "test pause"})
	accepted(t, r, e)
	policy := driverPolicy(f)
	r, e = f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, &v1.ConfigureReasonerDriverCommand{Header: header("repair-configure"), TaskId: f.task.Name, Policy: policy})
	accepted(t, r, e)
	before, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name})
	if e != nil || before.WaitingReason != "BUDGET_EXCEEDED" || provider.Calls() != 0 {
		t.Fatalf("budget gate: %v %v", before, e)
	}
	original, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, snapshot.RequestRef, 0)
	if e != nil || original == nil || original.AdmissionRef != nil {
		t.Fatalf("original prepared position: %v %v", original, e)
	}
	budget, e = f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Budget.AdjustLimit(f.ctx, f.caller, &v1.AdjustBudgetLimitCommand{Header: header("repair-limit-raised"), ExpectedRef: budget.Ref, Limit: 80, Reason: "explicit budget repair"})
	accepted(t, r, e)
	r, e = f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, &v1.ConfigureReasonerDriverCommand{Header: header("repair-resume"), TaskId: f.task.Name, Policy: policy, Replaces: before.Ref})
	accepted(t, r, e)
	after, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name})
	if e != nil || after.WaitingReason != "SESSION_REQUIRED" || !proto.Equal(after.RequestRef, snapshot.RequestRef) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatalf("explicit repair did not resume original: %v %v calls=%d", after, e, provider.Calls())
	}
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, snapshot.RequestRef, 0)
	if e != nil || !proto.Equal(call.Ref, original.Ref) || !proto.Equal(call.InputRef, original.InputRef) || call.Result == nil {
		t.Fatalf("changed position: %v %v", call, e)
	}
}

// 规则：G1、G3、G4、G6、G11
func TestDriverAdoptsOwnerRequestAfterActualQuestionAnswer(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"Which destination?","changesBasis":true}}`
	f := modelFixtureTarget(t, provider)
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	policy := driverPolicy(f)
	policy.SessionId = goal.Receipt.SessionRef.Name
	r, e := f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, &v1.ConfigureReasonerDriverCommand{Header: header("answer-driver-config"), TaskId: f.task.Name, Policy: policy})
	accepted(t, r, e)
	waiting, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name})
	if e != nil || waiting.WaitingReason != "USER_INPUT_REQUIRED" || provider.Calls() != 1 {
		t.Fatalf("original question: %v %v", waiting, e)
	}
	if waiting.QuestionRef == nil {
		t.Fatal("driver does not expose original question responsibility")
	}
	question, e := f.h.Sessions.QueryQuestion(f.ctx, f.caller, waiting.QuestionRef)
	if e != nil || question == nil {
		t.Fatalf("original question: %v %v", question, e)
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	answer, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("driver-answer"), SessionId: policy.SessionId, TaskId: f.task.Name, InputKind: "ANSWER", ContentRef: f.parameters, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion, RequestRef: question.Ref})
	accepted(t, answer, e)
	if e = f.h.Sessions.ProcessPending(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	task, e = f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Tasks.ProcessInput(f.ctx, f.caller, &v1.ProcessInputCommand{Header: header("driver-process-answer"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Outcome: "UNCHANGED", Source: "TRUSTED_TEMPLATE"})
	accepted(t, r, e)
	next, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("owner-after-answer"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	advanced, e := f.h.Tasks.QueryReasonerDriver(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(advanced.RequestRef, next.RequestRef) || advanced.WaitingReason != "USER_INPUT_REQUIRED" || provider.Calls() != 2 || len(provider.Bills()) != 2 {
		t.Fatalf("driver kept stale question/request: %v %v calls=%d", advanced, e, provider.Calls())
	}
	original, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, waiting.RequestRef)
	if e != nil || len(original.OutcomeRefs) != 1 || len(original.ModelOperationRefs) != 1 {
		t.Fatalf("changed historical request: %v %v", original, e)
	}
	owner, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, next.RequestRef)
	if e != nil || len(owner.OutcomeRefs) != 1 || len(owner.ModelOperationRefs) != 1 {
		t.Fatalf("new owner responsibility: %v %v", owner, e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || !proto.Equal(planning.Snapshot.Ref, next.Ref) {
		t.Fatalf("driver invented another request: %v %v", planning, e)
	}
}

// 规则：G1、G3、G4、G5、G6、G11、开始-2
func TestDriverContinuesOnlyAfterAcceptedControlOrRequirementsBasis(t *testing.T) {
	for _, mode := range []string{"pause-resume", "pause-resume-owner-request", "requirements", "requirements-owner-request", "unknown-pause-resume"} {
		t.Run(mode, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			provider.Output = `{"kind":"QUESTION","question":{"question":"Choose destination"}}`
			provider.Drop = mode == "unknown-pause-resume"
			f := modelFixtureTarget(t, provider)
			r, e := f.h.Tasks.ConfigureReasonerDriver(f.ctx, f.caller, &v1.ConfigureReasonerDriverCommand{Header: header("basis-driver"), TaskId: f.task.Name, Policy: driverPolicy(f)})
			accepted(t, r, e)
			old, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name})
			if e != nil || provider.Calls() != 1 {
				t.Fatalf("initial: %v %v", old, e)
			}
			if mode == "pause-resume" || mode == "pause-resume-owner-request" || mode == "unknown-pause-resume" {
				resendTaskControl(t, f, "PAUSE", "driver-pause")
				paused, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name})
				if e != nil || paused.WaitingReason != "TASK_NOT_ACTIVE" || provider.Calls() != 1 {
					t.Fatalf("pause bypassed: %v %v", paused, e)
				}
				resendTaskControl(t, f, "RESUME", "driver-resume")
			} else {
				task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
				if e != nil {
					t.Fatal(e)
				}
				r, e = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("driver-new-requirements"), TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, InputVersion: task.InputVersion, Source: "TRUSTED_TEMPLATE", Conditions: []*v1.Requirement{{ConditionId: "new-condition", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "USER_EVALUATION", RuleVersion: 1}}})
				accepted(t, r, e)
			}
			basis, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			var owner *v1.ContextSnapshot
			if mode == "pause-resume-owner-request" || mode == "requirements-owner-request" {
				owner, e = f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("new-owner-basis"), TaskId: f.task.Name})
				if e != nil {
					t.Fatal(e)
				}
			}
			next, e := f.h.Tasks.AdvanceReasonerTask(f.ctx, f.caller, &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name})
			if e != nil {
				t.Fatal(e)
			}
			if mode == "unknown-pause-resume" {
				if next.WaitingReason != "UNKNOWN" || !proto.Equal(next.RequestRef, old.RequestRef) || provider.Calls() != 1 {
					t.Fatalf("control reopened unknown: %v", next)
				}
				return
			}
			if next.WaitingReason != "SESSION_REQUIRED" || proto.Equal(next.RequestRef, old.RequestRef) || provider.Calls() != 2 || len(provider.Bills()) != 2 {
				t.Fatalf("accepted basis never continued: %v calls=%d", next, provider.Calls())
			}
			planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			if planning.Snapshot.InputVersion != basis.InputVersion || planning.Snapshot.ControlGeneration != basis.ControlGeneration || planning.Snapshot.RequirementsVersion != basis.RequirementsVersion || planning.Snapshot.PlanningGeneration != basis.PlanningGeneration+1 {
				t.Fatalf("not unique actual basis: %v", planning.Snapshot)
			}
			if owner != nil && !proto.Equal(next.RequestRef, owner.RequestRef) {
				t.Fatal("invented request instead of owner continuation")
			}
		})
	}
}
