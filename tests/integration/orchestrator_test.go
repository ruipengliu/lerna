package integration_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	"github.com/ruipengliu/lerna/internal/host"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
	filedb "github.com/ruipengliu/lerna/internal/storage/sqlite"
)

var taskScope = durable.Scope{TenantID: o.ID("tenant", "tests"), OwnerID: o.ID("orchestrator", "tests")}
var taskCaller = api.Caller{TenantID: taskScope.TenantID, ActorID: o.ID("actor", "tests")}

// The collaborators keep their own original identities and outcomes. They are
// domain-port fixtures, not evidence for real Brain/Executor implementations.
type taskFixture struct {
	rootTask                              string
	propose                               func(api.DecisionRequest) *api.Proposal
	verify                                func(api.VerificationInput) *api.VerificationBundle
	reconcileCalls                        []api.OriginalCall
	extractionCalls                       []api.OriginalCall
	reconcileError                        error
	mu                                    sync.Mutex
	content                               map[string][]byte
	publications                          map[string]api.ContentRef
	decisions                             map[string]api.DecisionRecord
	requests                              map[string]api.DecisionRequest
	operations                            map[string]api.Operation
	invokes                               map[string]api.Invoke
	physicalDecisions, physicalOperations int
	policy                                api.TaskPolicy
	goal, artifact                        api.ContentRef
	ready                                 bool
	generation                            string
	mode                                  string
	deny                                  map[string]bool
	usage                                 map[string]api.Billing
	closures                              map[string]api.RuntimeBudgetClosure
	allocations                           map[string]api.RuntimeBudgetAllocation
	errors                                []error
	confirmation                          *api.ConfirmationRecord
	block                                 chan struct{}
	beforeSubmit                          func()
}

func newTaskFixture() *taskFixture {
	f := &taskFixture{content: map[string][]byte{}, publications: map[string]api.ContentRef{}, decisions: map[string]api.DecisionRecord{}, requests: map[string]api.DecisionRequest{}, operations: map[string]api.Operation{}, invokes: map[string]api.Invoke{}, ready: true, generation: "g1", mode: "complete", deny: map[string]bool{}, usage: map[string]api.Billing{}, closures: map[string]api.RuntimeBudgetClosure{}, allocations: map[string]api.RuntimeBudgetAllocation{}}
	f.goal = f.put("goal", []byte("produce the declared artifact"))
	f.artifact = f.put("artifact", []byte("fixed artifact"))
	ref := func(id string) api.ComponentRef {
		return api.ComponentRef{ID: o.ID("component", id), Version: "1.0.0", Digest: "sha256:" + strings.Repeat("a", 64)}
	}
	unit := "usd"
	effect := "read_only"
	f.policy = api.TaskPolicy{Ref: ref("policy"), BrainID: o.ID("brain", "tests"), ModelProfileRef: ref("model"), Requirements: []api.Requirement{{RequirementID: o.ID("requirement", "quality"), Kind: "quality", SourceRef: f.goal, RuleRef: ref("quality"), Required: true}}, Capabilities: []api.CapabilityFixture{{CapabilityRef: ref("capability"), BindingRef: api.BindingRef{BindingID: o.ID("binding", "tests"), Revision: 1}, InputSchema: map[string]json.RawMessage{}, EffectClass: &effect}}, UsageAuthorizationRefs: []api.AuthorizationRef{{Kind: "grant", OwnerID: o.ID("security", "tests"), ID: o.ID("grant", "tests"), Revision: 1}}, DecisionUpperBound: []api.Amount{{Unit: unit, Amount: "1"}}, BudgetMode: "strict", MaxContinuations: 8, MaxRepairs: 3, MaxNoProgress: 3, MaxActions: 4, MaxContextRequests: 4, MaxOutputTokens: 1000, MaxPolls: 40, InputTimeout: time.Minute}
	f.policy.CoverageBinding = api.VerificationBinding{RuleRef: ref("quality"), EvaluatorRef: api.ComponentRef{ID: o.ID("evaluator", "test"), Version: "1.0.0", Digest: "sha256:" + strings.Repeat("b", 64)}}
	f.policy.VerificationBindings = []api.VerificationBinding{{RuleRef: f.policy.CoverageBinding.RuleRef, EvaluatorRef: f.policy.CoverageBinding.EvaluatorRef, Basis: "assessed"}}
	return f
}
func (f *taskFixture) put(id string, body []byte) api.ContentRef {
	h := sha256.Sum256(body)
	ref := api.ContentRef{TenantID: taskScope.TenantID, OwnerID: o.ID("content", "tests"), ContentID: o.ID("content", id, hex.EncodeToString(h[:])), Version: 1, Hash: "sha256:" + hex.EncodeToString(h[:]), MediaType: "application/json", ByteLength: int64(len(body))}
	f.content[ref.ContentID] = append([]byte(nil), body...)
	return ref
}

type taskAuthority struct{ f *taskFixture }

func (a taskAuthority) AdmissionReady() bool { a.f.mu.Lock(); defer a.f.mu.Unlock(); return a.f.ready }
func (a taskAuthority) Current(_ context.Context, caller api.Caller, access api.Access, _ time.Time) error {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	if caller.TenantID != taskScope.TenantID {
		return &api.Failure{Detail: api.Error{Code: "forbidden", Message: "tenant mismatch", Retry: "after_change"}}
	}
	if access.Method == "task.submit" && a.f.beforeSubmit != nil {
		a.f.beforeSubmit()
	}
	if a.f.deny[access.TargetID] {
		return &api.Failure{Detail: api.Error{Code: "forbidden", Message: "disclosure withdrawn", Retry: "after_change"}}
	}
	return nil
}
func (a taskAuthority) DisclosureGeneration(context.Context, api.Caller) (string, error) {
	a.f.mu.Lock()
	defer a.f.mu.Unlock()
	return a.f.generation, nil
}
func (a taskAuthority) ControlProof(_ context.Context, _ api.Caller, g api.TaskGate, id string, now time.Time) (api.ControlSnapshot, error) {
	return api.ControlSnapshot{Gate: g, ExecutorID: id, IssuedAt: now.UTC().Format(time.RFC3339Nano), StartBefore: now.Add(5 * time.Second).UTC().Format(time.RFC3339Nano), OrchestratorProof: "fixture-original-proof"}, nil
}

type taskClock struct{ f *taskFixture }

func (v taskClock) Ready() bool    { return taskAuthority{v.f}.AdmissionReady() }
func (v taskClock) Now() time.Time { return time.Now().UTC() }

type taskContent struct{ f *taskFixture }

func (v taskContent) Read(_ context.Context, _ api.Caller, ref api.ContentRef) ([]byte, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	b, ok := v.f.content[ref.ContentID]
	if !ok {
		return nil, &api.Failure{Detail: api.Error{Code: "not_found", Message: "content absent", Retry: "none"}}
	}
	return append([]byte(nil), b...), nil
}
func (v taskContent) Write(_ context.Context, _ api.Caller, id, media string, body []byte) (api.ContentRef, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if ref, ok := v.f.publications[id]; ok {
		h := sha256.Sum256(body)
		if ref.Hash != "sha256:"+hex.EncodeToString(h[:]) {
			return api.ContentRef{}, errors.New("publication changed")
		}
		return ref, nil
	}
	ref := v.f.put(id, body)
	ref.MediaType = media
	v.f.publications[id] = ref
	return ref, nil
}

type taskPolicy struct{ f *taskFixture }

func (v taskPolicy) Resolve(context.Context, api.Caller, api.ComponentRef, api.ContentRef, []string) (api.TaskPolicy, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	return v.f.policy, nil
}

type taskBrain struct{ f *taskFixture }

func (v taskBrain) Decide(_ context.Context, _ api.Caller, _ api.OriginalCall, in api.DecisionRequest) (api.DecisionRecord, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if d, ok := v.f.decisions[in.DecisionID]; ok {
		return d, nil
	}
	v.f.requests[in.DecisionID] = in
	v.f.physicalDecisions++
	p := api.Proposal{Kind: "complete", Rationale: "candidate requires independent verification", EvidenceRefs: []api.ContentRef{}, Assumptions: []string{}, ArtifactRef: &v.f.artifact, RequirementRefs: rawPtr(o.Raw([]string{v.f.policy.Requirements[0].RequirementID})), CompletionBasis: stringPtr("assessed"), AssessmentRefs: &[]api.ContentRef{}}
	if v.f.mode == "act" && in.SnapshotRevision == 1 {
		cap := v.f.policy.Capabilities[0]
		a := api.ActionInvoke{ActionKey: "first", Type: "invoke", Purpose: "read declared artifact", RequirementRefs: []string{v.f.policy.Requirements[0].RequirementID}, EvidenceRefs: []api.ContentRef{}, CapabilityRef: cap.CapabilityRef, BindingRef: cap.BindingRef, Arguments: map[string]json.RawMessage{"query": o.Raw("test")}}
		actions := []json.RawMessage{o.Raw(a)}
		p = api.Proposal{Kind: "act", Rationale: "read first", EvidenceRefs: []api.ContentRef{}, Assumptions: []string{}, Actions: &actions}
	}
	if v.f.mode == "input" && in.SnapshotRevision == 1 {
		p = api.Proposal{Kind: "request_input", Rationale: "clarify", EvidenceRefs: []api.ContentRef{}, Assumptions: []string{}, Question: stringPtr("请选择"), Options: &[]string{"A", "B"}, RequiredMaterialRefs: &[]api.ContentRef{v.f.artifact}, RequirementRefs: rawPtr(o.Raw([]string{}))}
	}
	if v.f.propose != nil {
		if custom := v.f.propose(in); custom != nil {
			p = *custom
		}
	}
	d := api.DecisionRecord{DecisionID: in.DecisionID, Revision: 1, SnapshotRevision: in.SnapshotRevision, Status: "completed", Proposal: &p, ModelCall: &api.ModelCall{ModelCallID: o.ID("model_call", in.DecisionID), State: "returned", Usage: []api.Amount{{Unit: "usd", Amount: "0.2"}}, UsageFinal: true}}
	if v.f.mode == "pending" {
		d.Status = "running"
		d.Proposal = nil
	}
	v.f.decisions[in.DecisionID] = d
	return d, nil
}
func (v taskBrain) Read(_ context.Context, _ api.Caller, _, id string) (api.DecisionRecord, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	d, ok := v.f.decisions[id]
	if !ok {
		return d, &api.Failure{Detail: api.Error{Code: "not_found", Message: "decision absent", Retry: "none"}}
	}
	return d, nil
}
func (v taskBrain) Cancel(_ context.Context, _ api.Caller, _ api.OriginalCall, id string) (api.DecisionRecord, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	d, ok := v.f.decisions[id]
	if !ok {
		d = api.DecisionRecord{DecisionID: id, SnapshotRevision: 1}
	}
	d.Revision++
	d.Status = "cancelled"
	d.Proposal = nil
	v.f.decisions[id] = d
	return d, nil
}

type taskActions struct{ f *taskFixture }

func (v taskActions) Prepare(_ context.Context, _ api.Caller, _ api.Task, a api.ActionInvoke) (api.ActionPreparation, error) {
	return api.ActionPreparation{Action: a, ExecutorID: o.ID("executor", "tests"), ProviderID: o.ID("provider", "tests"), ResourceID: o.ID("resource", "tests"), EffectClass: "read_only", UpperBound: []api.Amount{{Unit: "usd", Amount: "1"}}, AuthorizationRefs: v.f.policy.UsageAuthorizationRefs}, nil
}

type taskExecutor struct{ f *taskFixture }

func (v taskExecutor) Invoke(_ context.Context, _ api.Caller, _ api.OriginalCall, in api.Invoke) (api.Operation, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if op, ok := v.f.operations[in.OperationID]; ok {
		return op, nil
	}
	v.f.physicalOperations++
	v.f.invokes[in.OperationID] = in
	op := api.Operation{OperationID: in.OperationID, Revision: 1, ExecutionState: "closed", Effect: "not_applied", MayApplyLater: o.Raw(false), Attempts: []api.Attempt{}, EvidenceRefs: []api.ContentRef{v.f.artifact}, ResultRef: &v.f.artifact, Usage: []api.Amount{{Unit: "usd", Amount: "0.3"}}, UsageFinal: true, NextAction: "none"}
	if v.f.mode == "unknown" {
		op.Effect = "unknown"
		op.MayApplyLater = o.Raw("unknown")
	}
	v.f.operations[in.OperationID] = op
	return op, nil
}
func (v taskExecutor) Read(_ context.Context, _ api.Caller, _, id string) (api.Operation, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	op, ok := v.f.operations[id]
	if !ok {
		return op, &api.Failure{Detail: api.Error{Code: "not_found", Message: "operation absent", Retry: "none"}}
	}
	return op, nil
}
func (v taskExecutor) Cancel(_ context.Context, _ api.Caller, _ api.OriginalCall, id string) (api.Operation, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	op, ok := v.f.operations[id]
	if ok {
		return op, nil
	}
	op = api.Operation{OperationID: id, Revision: 1, ExecutionState: "closed", Effect: "not_started", MayApplyLater: o.Raw(false), Attempts: []api.Attempt{}, EvidenceRefs: []api.ContentRef{}, Usage: []api.Amount{{Unit: "usd", Amount: "0"}}, UsageFinal: true, NextAction: "none"}
	v.f.operations[id] = op
	return op, nil
}
func (v taskExecutor) Control(_ context.Context, _ api.Caller, _ api.OriginalCall, s api.ControlSnapshot) (api.ControlObservation, error) {
	return api.ControlObservation{ExecutorID: s.ExecutorID, TaskID: s.Gate.TaskID, GoalRevision: s.Gate.GoalRevision, ControlRevision: s.Gate.ControlRevision, Enforced: true}, nil
}
func (v taskExecutor) AttachEvidence(context.Context, api.Caller, api.OriginalCall, string, []api.ContentRef) error {
	return nil
}

type taskBilling struct{ f *taskFixture }

func (v taskBilling) Read(_ context.Context, _ api.Caller, q api.BillingQuery) (api.Billing, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if b, ok := v.f.usage[q.ObjectID]; ok {
		b.Usage = append([]api.Amount(nil), b.Usage...)
		return b, nil
	}
	usage := "0.2"
	kind := "model_call"
	if q.ObjectKind == "operation" {
		usage = "0.3"
		kind = "operation"
	}
	proof := v.f.put("billing-"+q.ObjectID, []byte(q.ObjectID+usage))
	b := api.Billing{Source: api.SourceKey{OwnerID: q.ObjectOwnerID, Kind: kind, ID: o.ID(kind, q.ObjectID)}, Revision: 1, Digest: o.Hash([]string{q.ObjectID, usage}), Usage: []api.Amount{{Unit: "usd", Amount: usage}}, Final: true, ProofRef: proof}
	v.f.usage[q.ObjectID] = b
	b.Usage = append([]api.Amount(nil), b.Usage...)
	return b, nil
}
func (v taskBilling) Allocation(_ context.Context, _ api.Caller, _, id string) (api.RuntimeBudgetAllocation, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	a, ok := v.f.allocations[id]
	if !ok {
		return a, &api.Failure{Detail: api.Error{Code: "not_found", Message: "allocation absent", Retry: "none"}}
	}
	return a, nil
}
func (v taskBilling) Closure(_ context.Context, _ api.Caller, _, id string) (api.RuntimeBudgetClosure, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	a, ok := v.f.closures[id]
	if !ok {
		return a, &api.Failure{Detail: api.Error{Code: "not_found", Message: "closure absent", Retry: "none"}}
	}
	return a, nil
}
func (v taskBilling) Reconcile(_ context.Context, _ api.Caller, call api.OriginalCall) (api.JobAck, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	v.f.reconcileCalls = append(v.f.reconcileCalls, call)
	if v.f.reconcileError != nil {
		return api.JobAck{}, v.f.reconcileError
	}
	return api.JobAck{JobID: o.ID("job", "reconcile"), ResourceID: o.ID("task", "parent")}, nil
}

type taskVerifier struct{ f *taskFixture }

func (v taskVerifier) Prepare(_ context.Context, _ api.Caller, in api.VerificationInput) (api.VerificationBundle, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if v.f.verify != nil {
		if custom := v.f.verify(in); custom != nil {
			return *custom, nil
		}
	}
	req := in.Task.Requirements[0]
	rule := req.RuleRef
	evaluator := api.ComponentRef{ID: o.ID("evaluator", "test"), Version: "1.0.0", Digest: "sha256:" + strings.Repeat("b", 64)}
	report := v.f.put("report-"+in.Task.TaskID, []byte(in.Task.TaskID))
	verdict := "pass"
	if v.f.mode == "coverage_missing" {
		verdict = "unknown"
	}
	coverage := api.CoverageEvidence{InputDigest: in.InputDigest, TaskID: in.Task.TaskID, GoalRevision: in.Task.GoalRevision, GoalRef: in.Task.GoalRef, RequirementsDigest: o.Hash(in.Task.Requirements), RuleRef: rule, EvaluatorRef: evaluator, ReportRef: report, Verdict: verdict, Limitations: []string{"fixture only"}}
	check := api.CheckEvidence{InputDigest: in.InputDigest, CheckID: o.ID("check", in.Task.TaskID, fmt.Sprint(in.Task.GoalRevision), in.Artifacts[0].Hash), TaskID: in.Task.TaskID, GoalRevision: in.Task.GoalRevision, RuleRef: rule, Result: api.ConditionResult{RequirementID: req.RequirementID, GoalRevision: in.Task.GoalRevision, ArtifactRef: in.Artifacts[0], Verdict: "pass", Basis: "assessed", EvidenceRefs: []api.ContentRef{report}, EvaluatorRef: evaluator}, ReportRef: report, ComponentCheckIDs: []string{}}
	bundle := api.VerificationBundle{Coverage: &coverage, Checks: []api.CheckEvidence{check}, Actions: []api.ActionInvoke{}, Limitations: []string{}}
	if v.f.mode == "acceptance" {
		bundle.Checks[0].Result.Verdict = "unknown"
		bundle.AcceptanceRequirements = []string{req.RequirementID}
	}
	return bundle, nil
}

type taskConfirmation struct{ f *taskFixture }

func (v taskConfirmation) Read(context.Context, api.Caller, api.Command, api.ObjectRef) (api.ConfirmationRecord, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	if v.f.confirmation == nil {
		return api.ConfirmationRecord{}, errors.New("confirmation absent")
	}
	return *v.f.confirmation, nil
}
func (f *taskFixture) ports() api.OrchestratorPorts {
	return api.OrchestratorPorts{Authority: taskAuthority{f}, Clock: taskClock{f}, Content: taskContent{f}, Policies: taskPolicy{f}, Brain: taskBrain{f}, Actions: taskActions{f}, Executor: taskExecutor{f}, Billing: taskBilling{f}, Verification: taskVerifier{f}, Confirmation: taskConfirmation{f}, Collaboration: taskCollaboration{f}, Context: taskContext{f}, Extraction: taskExtraction{f}}
}
func rawPtr(v json.RawMessage) *json.RawMessage { return &v }
func stringPtr(v string) *string                { return &v }
func newTaskService(t *testing.T, s *suite, f *taskFixture) *host.TaskService {
	t.Helper()
	q := filedb.TaskQueries()
	if s.driver == "postgres" {
		q = pg.TaskQueries()
	}
	limits := o.DefaultLimits()
	limits.Backoff = 10 * time.Millisecond
	service, e := host.NewTaskService(s.store, q, f.ports(), host.TaskServiceOptions{Scope: taskScope, ServiceID: o.ID("service", "tasks"), Limits: limits, OnError: func(e error) { f.mu.Lock(); f.errors = append(f.errors, e); f.mu.Unlock() }})
	if e != nil {
		t.Fatal(e)
	}
	if e = service.Recover(ctx()); e != nil {
		t.Fatal(e)
	}
	return service
}
func submitTask(t *testing.T, s api.Orchestrator, f *taskFixture, budget string) api.Task {
	t.Helper()
	input := api.TaskSubmitInput{OrchestratorID: taskScope.OwnerID, GoalRef: f.goal, Constraints: []string{}, PolicyRef: f.policy.Ref, Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Budget: []api.BudgetLimit{{Unit: "usd", Limit: budget}}}
	command := api.Command{CommandID: durable.NewID("command"), Method: "task.submit", TargetID: taskScope.OwnerID, ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Payload: o.Raw(input)}
	r, e := s.Execute(ctx(), taskCaller, command)
	if e != nil || r.Stage != "applied" {
		t.Fatalf("submit: %s %v %+v", r.Stage, e, r.Failure)
	}
	var task api.Task
	if e = json.Unmarshal(r.Output, &task); e != nil {
		t.Fatal(e)
	}
	return task
}
func runTaskService(t *testing.T, s *host.TaskService) {
	t.Helper()
	run, cancel := context.WithCancel(ctx())
	done := make(chan error, 1)
	go func() { done <- s.Run(run) }()
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(10 * time.Second):
			t.Error("task runner did not drain")
		}
	})
}
func readTask(t *testing.T, s api.Orchestrator, id string) api.Task {
	t.Helper()
	r, e := s.Query(ctx(), taskCaller, api.Query{Method: "task.read", TargetID: id, Payload: o.Raw(api.TaskReadInput{})})
	if e != nil {
		t.Fatal(e)
	}
	var task api.Task
	if e = json.Unmarshal(r.Value, &task); e != nil {
		t.Fatal(e)
	}
	return task
}
func awaitTask(t *testing.T, s api.Orchestrator, f *taskFixture, id string, predicate func(api.Task) bool) api.Task {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		task := readTask(t, s, id)
		if predicate(task) {
			return task
		}
		time.Sleep(50 * time.Millisecond)
	}
	f.mu.Lock()
	errs := append([]error{}, f.errors...)
	f.mu.Unlock()
	t.Fatalf("task did not reach predicate: %+v errors=%v", readTask(t, s, id), errs)
	return api.Task{}
}

func TestOrchestratorCompletesAndAccounts(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "10")
			runTaskService(t, service)
			finished := awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" && !v.AccountingOpen })
			if finished.Budget[0].Spent.Amount != "0.2" || finished.Budget[0].Reserved.Amount != "0" {
				t.Fatalf("ledger %+v", finished.Budget)
			}
			r, e := service.Query(ctx(), taskCaller, api.Query{Method: "task.result", TargetID: task.TaskID, Payload: o.Raw(api.TaskResultInput{})})
			if e != nil {
				t.Fatal(e)
			}
			var result api.TaskResultView
			_ = json.Unmarshal(r.Value, &result)
			if result.Result == nil || result.Result.CompletionBasis != "assessed" || len(result.Result.ConditionResults) != 1 {
				t.Fatal("independent result missing")
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.physicalDecisions != 1 {
				t.Fatalf("physical decisions=%d", f.physicalDecisions)
			}
		})
	}
}
func TestOrchestratorActionAndNoDuplicateEffect(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			f.mode = "act"
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "10")
			runTaskService(t, service)
			finished := awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" && !v.AccountingOpen })
			if finished.Budget[0].Spent.Amount != "0.7" {
				t.Fatal(finished.Budget)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.physicalOperations != 1 || f.physicalDecisions != 2 {
				t.Fatalf("operations=%d decisions=%d", f.physicalOperations, f.physicalDecisions)
			}
		})
	}
}
