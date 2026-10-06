package admission_test

import (
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G4、G10、G11、R3、V4、准入-7、准入-8
func TestLedgerMetricsKeepLiteralHundredOriginalUnknownsAndNinetyCompletedPlans(t *testing.T) {
	// 人口来自同一数据库的公开生产入口；保留原对象只为独立核验，不构造指标事实。
	const population, completed = 100, 90
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	base := newFixtureWithTarget(t, 10000, 80, false, target)
	queryCapability, initialQueryGrant := configureReconciliation(t, base)
	writeCapability, e := base.h.Tasks.QueryCapability(base.ctx, base.caller, base.capability)
	if e != nil || writeCapability == nil {
		t.Fatalf("configured write capability: %v %v", writeCapability, e)
	}
	t.Logf("actual storage/build=%+v; original adapter=%v; query capability=%v", base.h.StorageSettings(), writeCapability.AdapterRef, queryCapability)
	type member struct {
		fixture    *fixture
		queryGrant *v1.Ref
		admission  *v1.Admission
		original   *v1.Operation
	}
	members := make([]member, 0, population)
	for index := range population {
		f, queryGrant := base, initialQueryGrant
		if index > 0 {
			f = newMetricPopulationTask(t, base, index, writeCapability)
			queryGrant = f.grant
		}
		a, start := prepareMetricPopulationStart(t, f)
		r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
		accepted(t, r, e)
		original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
		if e != nil || original == nil || original.QuerySubject != nil || original.Effect.GetOutcome() != "UNKNOWN" || original.Effect.GetLateEffect() != "MAY_OCCUR" || original.Execution.GetAttempt().GetFirstPossibleSendAtUnixMs() <= 0 {
			t.Fatalf("member %d original UNKNOWN: %v %v", index, original, e)
		}
		members = append(members, member{fixture: f, queryGrant: queryGrant, admission: a, original: original})
	}
	initial, e := base.h.Ledger.QueryMetrics(base.ctx, base.caller)
	if e != nil || initial == nil || initial.Unknown.GetTotal() != population || initial.UnknownBusinessOperations != population || initial.UnknownQueryOperations != 0 || initial.Reconciliation.GetTotal() != 0 || initial.Reconciliation.GetUnknownUncovered() != population || initial.Reconciliation.GetUnknownCovered() != 0 {
		t.Fatalf("all original UNKNOWNs before plans: %v %v", initial, e)
	}
	assertMetricPopulationUnknownSource(t, initial, population)
	requests, effects := target.Snapshot()
	if len(requests) != population || len(effects) != population || base.calls.Load() != population {
		t.Fatalf("original independent target population: calls=%d requests=%d effects=%d", base.calls.Load(), len(requests), len(effects))
	}
	originalByKey := make(map[string]*v1.Operation, population)
	for _, m := range members {
		key := m.original.Execution.Attempt.ExternalKey
		if key == "" || originalByKey[key] != nil {
			t.Fatalf("distinct original external keys: %q", key)
		}
		originalByKey[key] = m.original
	}
	assertMetricPopulationTarget(t, requests, effects, originalByKey, nil)

	// 十项由实际暂停命令保持未完成，不能从完成样本分母里消失。
	target.SetBehavior("")
	for index, m := range members {
		c := reconciliationCommand(m.fixture, m.admission, queryCapability, m.queryGrant)
		c.Header = ledgerHeader(fmt.Sprintf("metric-population-reconcile-%03d", index))
		r, e := base.h.Ledger.RequestReconciliation(base.ctx, base.caller, c)
		accepted(t, r, e)
		if index >= completed {
			plan, e := base.h.Ledger.QueryReconciliation(base.ctx, base.caller, m.admission.OperationId)
			if e != nil || plan == nil {
				t.Fatalf("original pause plan %d: %v %v", index, plan, e)
			}
			r, e = base.h.Ledger.ControlReconciliation(base.ctx, base.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader(fmt.Sprintf("metric-population-pause-%03d", index)), OperationId: m.admission.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "PAUSE"})
			accepted(t, r, e)
		}
	}
	before, e := base.h.Ledger.QueryMetrics(base.ctx, base.caller)
	if e != nil || before == nil || before.Reconciliation.GetTotal() != population || before.Reconciliation.GetReady() != completed || before.Reconciliation.GetActive() != completed || before.Reconciliation.GetPaused() != population-completed || before.Reconciliation.GetUnfinished() != population || before.Reconciliation.GetCompleted() != 0 || before.Reconciliation.GetUnknownCovered() != population || before.Reconciliation.GetUnknownUncovered() != 0 || before.Unknown.GetTotal() != population {
		t.Fatalf("all plans before actual queries: %v %v", before, e)
	}
	assertMetricPopulationUnknownSource(t, before, population)
	if base.calls.Load() != population {
		t.Fatal("requesting or pausing reconciliation called target")
	}
	if e = base.h.Ledger.ProcessReconciliations(base.ctx, base.caller); e != nil {
		t.Fatal(e)
	}
	queryByKey := make(map[string]*v1.Operation, completed)
	var actualCompleted, actualUnfinished, actualUnknown uint64
	for index, m := range members {
		plan, e := base.h.Ledger.QueryReconciliation(base.ctx, base.caller, m.admission.OperationId)
		if e != nil || plan == nil || !proto.Equal(plan.OriginalAttemptRef, m.original.Execution.Attempt.Ref) {
			t.Fatalf("member %d original plan: %v %v", index, plan, e)
		}
		op, e := base.h.Ledger.QueryOperation(base.ctx, base.caller, m.admission.OperationId)
		if e != nil || op == nil || !proto.Equal(op.Execution, m.original.Execution) {
			t.Fatalf("member %d original execution changed: %v %v", index, op, e)
		}
		if index >= completed {
			if plan.State != "PAUSED" || plan.PauseReason != "USER_PAUSED" || plan.CheckCount != 0 || len(plan.QueryRefs) != 0 || plan.ActiveQueryRef != nil || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
				t.Fatalf("member %d unfinished responsibility: plan=%v operation=%v", index, plan, op)
			}
			actualUnfinished++
			actualUnknown++
			continue
		}
		if plan.State != "COMPLETED" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
			t.Fatalf("member %d actual completion: plan=%v operation=%v", index, plan, op)
		}
		actualCompleted++
		relation, e := base.h.Ledger.QueryReconciliationQuery(base.ctx, base.caller, plan.QueryRefs[0])
		if e != nil || relation == nil || relation.AdmissionReceipt.GetDecision() != v1.Decision_DECISION_ACCEPTED || relation.ObservationRef == nil || relation.InterpretationRef == nil || !proto.Equal(relation.Work.GetQuerySubject().GetOperationId(), op.Ref.Name) || !proto.Equal(relation.Work.GetQuerySubject().GetAttemptId(), m.original.Execution.Attempt.Ref.Name) || relation.Work.GetQuerySubject().GetExternalKey() != m.original.Execution.Attempt.ExternalKey {
			t.Fatalf("member %d actual query relation: %v %v", index, relation, e)
		}
		qa, e := base.h.Tasks.QueryAdmission(base.ctx, base.caller, relation.AdmissionReceipt.ResultRef)
		if e != nil || qa == nil || qa.WorkCategory != "CLOSURE" || !proto.Equal(qa.Origin, relation.Work.Ref) || proto.Equal(qa.OperationId, op.Ref.Name) {
			t.Fatalf("member %d independent query admission: %v %v", index, qa, e)
		}
		query, e := base.h.Ledger.QueryOperation(base.ctx, base.caller, qa.OperationId)
		if e != nil || query == nil || query.Lifecycle != "SETTLED" || query.Execution.GetCallDescriptor().GetMethod() != "GET" || !proto.Equal(query.QuerySubject, relation.Work.QuerySubject) || query.Execution.GetAttempt().GetExternalKey() == m.original.Execution.Attempt.ExternalKey {
			t.Fatalf("member %d independent GET: %v %v", index, query, e)
		}
		key := query.Execution.Attempt.ExternalKey
		if key == "" || queryByKey[key] != nil || originalByKey[key] != nil {
			t.Fatalf("member %d reused physical query identity: %q", index, key)
		}
		queryByKey[key] = query
	}
	final, e := base.h.Ledger.QueryMetrics(base.ctx, base.caller)
	if e != nil || final == nil || actualCompleted != completed || actualUnfinished != population-completed || actualUnknown != population-completed || final.Reconciliation.GetTotal() != population || final.Reconciliation.GetCompleted() != actualCompleted || final.Reconciliation.GetUnfinished() != actualUnfinished || final.Reconciliation.GetPaused() != actualUnfinished || final.Reconciliation.GetActive() != 0 || final.Reconciliation.GetReady() != 0 || final.Reconciliation.GetInProgress() != 0 || final.Reconciliation.GetWaiting() != 0 || final.Reconciliation.GetOther() != 0 || final.Unknown.GetTotal() != actualUnknown || final.UnknownBusinessOperations != actualUnknown || final.UnknownQueryOperations != 0 || final.Reconciliation.GetUnknownCovered() != actualUnknown || final.Reconciliation.GetUnknownUncovered() != 0 || final.Source.GetSourceRevision() <= before.Source.GetSourceRevision() {
		t.Fatalf("literal complete population denominator: %v %v owner completed=%d unfinished=%d unknown=%d", final, e, actualCompleted, actualUnfinished, actualUnknown)
	}
	assertMetricPopulationUnknownSource(t, final, population-completed)
	again, e := base.h.Ledger.QueryMetrics(base.ctx, base.caller)
	if e != nil || again == nil || again.Source.GetSourceRevision() != final.Source.GetSourceRevision() {
		t.Fatalf("metric query advanced owner: %v %v", again, e)
	}
	trace, e := base.h.Trace.QueryMetrics(base.ctx, base.caller)
	if e != nil || trace == nil || trace.Diagnostics == nil || trace.CoverageComplete || trace.PendingAcknowledgements == 0 || trace.ReceiverBacklog == 0 || trace.Diagnostics.GetSource().GetAvailability() != "DISABLED" || trace.Diagnostics.Dropped != nil || trace.Diagnostics.StalenessMs != nil {
		t.Fatalf("uncollected diagnostic coverage or disabled measurement became healthy zero: %v %v", trace, e)
	}
	requests, effects = target.Snapshot()
	if len(requests) != population+completed || len(effects) != population || base.calls.Load() != population+completed || len(queryByKey) != completed {
		t.Fatalf("actual independent final target: calls=%d requests=%d effects=%d query identities=%d", base.calls.Load(), len(requests), len(effects), len(queryByKey))
	}
	assertMetricPopulationTarget(t, requests, effects, originalByKey, queryByKey)
	// 进展比例有完整分母，但没有原始时长端点，不据此宣称一秒完成或达到 SLO。
	t.Logf("one database; original UNKNOWN population=%d; actual completed=%d; unfinished=%d; remaining UNKNOWN=%d; target POST=%d GET=%d effects=%d; owner=%s definition=%s revision=%d captured_at=%d; duration=%s; trace coverage=%t diagnostics=%s; no completion-duration/SLO measurement", population, actualCompleted, actualUnfinished, actualUnknown, population, completed, len(effects), final.Source.OwnerDomainId, final.Source.DefinitionVersion, final.Source.GetSourceRevision(), final.Source.CapturedAtUnixMs, final.Reconciliation.DurationAvailability, trace.CoverageComplete, trace.Diagnostics.Source.Availability)
}

func newMetricPopulationTask(t *testing.T, base *fixture, index int, capability *v1.Capability) *fixture {
	t.Helper()
	f := &fixture{path: base.path, suffix: fmt.Sprintf("-metric-population-%03d", index), h: base.h, ctx: base.ctx, caller: base.caller, capability: base.capability}
	r, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, &v1.SubmitGoalCommand{Identity: header("goal" + f.suffix).Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "create a record"})
	if e != nil || r == nil || r.Phase != v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED || r.JobRef == nil {
		t.Fatalf("member %d original submitted goal: %v %v", index, r, e)
	}
	if e = f.h.Sessions.ProcessPending(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, r.Identity)
	if e != nil || q == nil || q.Receipt.GetDecision() != v1.Decision_DECISION_ACCEPTED || q.Receipt.GetTaskRef() == nil || q.Receipt.GetInputRef() == nil {
		t.Fatalf("member %d actual goal receipt: %v %v", index, q, e)
	}
	f.task, f.parameters = q.Receipt.TaskRef, q.Receipt.InputRef
	r, e = f.h.Tasks.AcceptRequirements(f.ctx, f.caller, &v1.AcceptRequirementsCommand{Header: header("requirements" + f.suffix), TaskRef: f.task, InputVersion: 1, Conditions: []*v1.Requirement{{ConditionId: "created", DescriptionRef: f.parameters, Necessary: true, VerificationRule: "TARGET_RECORD", RuleVersion: 1}}, Source: "TRUSTED_TEMPLATE"})
	accepted(t, r, e)
	r, e = f.h.Budget.Configure(f.ctx, f.caller, &v1.ConfigureBudgetCommand{Header: header("task-budget" + f.suffix), TaskId: f.task.Name, Unit: "USD_MICRO", Limit: 80})
	accepted(t, r, e)
	r, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("grant" + f.suffix), Grant: &v1.Grant{Subject: f.task.Name, Permissions: []*v1.PermissionClause{{Action: "CREATE", Resource: capability.Resource, UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: capability.ExecutorEndpointId}, {Action: "QUERY", Resource: capability.Resource, UseRight: "READ", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: capability.ExecutorEndpointId}, {Action: "QUERY", Resource: capability.Resource, UseRight: "SAVE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: capability.ExecutorEndpointId}}, ValidFromUnixMs: time.Now().Add(-time.Minute).UnixMilli(), ValidUntilUnixMs: time.Now().Add(time.Hour).UnixMilli(), UseMode: "CONTINUOUS"}})
	accepted(t, r, e)
	f.grant = r.ResultRef
	return f
}

func prepareMetricPopulationStart(t *testing.T, f *fixture) (*v1.Admission, *v1.StartExecutionCommand) {
	t.Helper()
	p := f.propose(t, nil)
	r, e := f.h.Tasks.Admit(f.ctx, f.caller, &v1.AdmitCommand{Header: header("admit" + f.suffix), TaskId: f.task.Name, ProposalRef: p, GrantRef: f.grant})
	accepted(t, r, e)
	a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, r.ResultRef)
	if e != nil || a == nil {
		t.Fatalf("actual population admission: %v %v", a, e)
	}
	if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	jobs, e := f.h.Ledger.QueryJobs(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	var pending *v1.Job
	for _, job := range jobs {
		if job.JobType == "EXECUTE_OPERATION" {
			if pending != nil {
				t.Fatal("multiple original execution jobs")
			}
			pending = job
		}
	}
	if pending == nil {
		t.Fatal("original execution job missing")
	}
	// 精确领取当前动作，长人口构造不能顺便领取先前未解决的原动作。
	process := "metric-population-worker" + f.suffix
	r, e = f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("claim" + f.suffix).Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, JobRef: pending.Ref, Limit: 1, LeaseMs: 30000, ProcessInstance: process})
	accepted(t, r, e)
	if len(r.Jobs) != 1 || !proto.Equal(r.Jobs[0].SpecificationRef.Name, a.OperationId) {
		t.Fatalf("exact original execution claim: %v", r)
	}
	claim := r.Jobs[0]
	r, e = f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: ledgerHeader("prepare" + f.suffix), OperationId: a.OperationId, ProcessInstance: process, Claim: claim})
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil || x == nil {
		t.Fatalf("original population execution: %v %v", x, e)
	}
	b := &v1.ExitCredentialBinding{UserId: "u", TaskId: a.TaskId, SubjectId: a.TaskId, OperationId: a.OperationId, AttemptId: x.Attempt.Ref.Name, SendSeq: x.Send.SendSeq, AdmissionRef: a.Ref, GrantUseRef: a.GrantUseRef, CallerIssuerId: "egress", Audience: "egress", ExecutorEndpointId: a.ExecutorEndpointId, ExecutorInstance: x.Send.ProcessInstance, DescriptorDigest: x.CallDescriptor.Digest, UseRight: a.CapabilitySnapshot.UseRight, ProcessingPurpose: a.CapabilitySnapshot.ProcessingPurpose, RequirementsVersion: a.RequirementsVersion, InputVersion: a.InputVersion, ControlGeneration: a.ControlGeneration, BudgetReservationRef: a.BudgetBasis.ReservationRef}
	g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil || g == nil {
		t.Fatalf("original population grant: %v %v", g, e)
	}
	r, e = f.h.Grants.IssueCredential(f.ctx, f.caller, &v1.IssueExitCredentialCommand{Header: header("credential" + f.suffix), AdmissionRef: a.Ref, Binding: b, ExpiresAtUnixMs: min(time.Now().Add(time.Minute).UnixMilli(), g.ValidUntilUnixMs)})
	accepted(t, r, e)
	h := header("start:" + x.Send.Ref.Name.LocalId)
	h.Identity.IssuerId = "egress"
	return a, &v1.StartExecutionCommand{Header: h, AdmissionRef: a.Ref, CredentialRef: r.ResultRef, Binding: b, Claim: claim, CallDescriptor: x.CallDescriptor}
}

func assertMetricPopulationUnknownSource(t *testing.T, metrics *v1.LedgerMetrics, expected uint64) {
	t.Helper()
	u, r, source := metrics.Unknown, metrics.Reconciliation, metrics.Source
	if source.GetAvailability() != "AVAILABLE" || source.GetOwnerDomainId() != "d/ledger" || source.GetDefinitionVersion() != "lerna.m1.ledger.v1" || source.GetCapturedAtUnixMs() <= 0 || source.SourceRevision == nil || source.GetScope() != "ALL_OPERATIONS_INCLUDING_CLOSED_TASKS" || u.GetTotal() != expected || u.GetQueryable() != expected || u.GetUnqueryable() != 0 || u.GetQueryabilityMissing() != 0 || u.GetLateMayOccur() != expected || u.GetLateRuledOut() != 0 || u.GetLateUnknown() != 0 || u.GetAgeSamples() != expected || u.GetAgeMissing() != 0 || u.GetAgeClockInvalid() != 0 || u.OldestAgeMs == nil || u.GetAgeBasis() != "FIRST_POSSIBLE_SEND_APPROXIMATE_WALL" || r.GetDurationAvailability() != "MISSING_ORIGINAL_UNKNOWN_AND_COMPLETION_ENDPOINTS" || metrics.UninterpretableOperations != 0 {
		t.Fatalf("population source/missing/age scope: %v", metrics)
	}
	var ageSamples uint64
	for _, bucket := range u.AgeBuckets {
		ageSamples += bucket.Count
	}
	if ageSamples != expected {
		t.Fatalf("age population=%d expected=%d", ageSamples, expected)
	}
}

func assertMetricPopulationTarget(t *testing.T, requests []simulator.Request, effects []simulator.Effect, originals, queries map[string]*v1.Operation) {
	t.Helper()
	seen := make(map[string]bool, len(requests))
	for _, request := range requests {
		operations := originals
		if request.Method == http.MethodGet {
			operations = queries
		} else if request.Method != http.MethodPost {
			t.Fatalf("unexpected physical method: %s", request.Method)
		}
		op := operations[request.ExternalKey]
		if op == nil || seen[request.ExternalKey] || request.User != "u" || request.Operation != op.Ref.Name.LocalId || request.Attempt != op.Execution.Attempt.Ref.Name.LocalId || request.Send != strconv.FormatUint(uint64(op.Execution.Send.SendSeq), 10) {
			t.Fatalf("independent target identity or extra send: %+v", request)
		}
		seen[request.ExternalKey] = true
	}
	if len(seen) != len(originals)+len(queries) {
		t.Fatalf("independent physical identity coverage=%d original=%d query=%d", len(seen), len(originals), len(queries))
	}
	seenEffects := make(map[string]bool, len(effects))
	for _, effect := range effects {
		if originals[effect.ExternalKey] == nil || seenEffects[effect.ExternalKey] {
			t.Fatalf("extra, repeated or query target effect: %+v", effect)
		}
		seenEffects[effect.ExternalKey] = true
	}
	if len(seenEffects) != len(originals) {
		t.Fatalf("independent original effects=%d originals=%d", len(seenEffects), len(originals))
	}
}
