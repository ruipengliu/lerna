package admission_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G11、准入-7、准入-8、开始-1、开始-5
func TestQueryableLostWriteReceiptRecoversThroughSeparateAdmittedRead(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	queryCap, queryGrant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, err)
	before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || before.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("lost write: %v %v", before, err)
	}
	target.SetBehavior("")
	c := &v1.RequestReconciliationCommand{Header: ledgerHeader("reconcile"), OperationId: a.OperationId, QueryCapabilityRef: queryCap, ParametersRef: f.parameters, GrantRef: queryGrant, PolicyVersion: 1, Limits: &v1.ReconciliationLimits{MaxChecks: 3, DeadlineUnixMs: time.Now().Add(time.Hour).UnixMilli(), MaxFee: 30, InitialDelayMs: 20, MaxDelayMs: 1000}}
	r, err = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, c)
	accepted(t, r, err)
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	plan, err := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if err != nil || plan == nil || plan.State != "COMPLETED" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 {
		t.Fatalf("plan: %v %v", plan, err)
	}
	relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
	if err != nil || relation.AdmissionReceipt == nil || relation.ObservationRef == nil || relation.InterpretationRef == nil {
		t.Fatalf("relation: %v %v", relation, err)
	}
	qa, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, relation.AdmissionReceipt.ResultRef)
	if err != nil || qa.WorkCategory != "CLOSURE" || proto.Equal(qa.OperationId, a.OperationId) || !proto.Equal(qa.Origin, relation.Work.Ref) {
		t.Fatalf("query admission: %v %v", qa, err)
	}
	query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, qa.OperationId)
	if err != nil || query.Lifecycle != "SETTLED" || query.Execution.CallDescriptor.Method != "GET" || query.Execution.Attempt.ExternalKey == before.Execution.Attempt.ExternalKey {
		t.Fatalf("query execution: %v %v", query, err)
	}
	original, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || original.Lifecycle != "SETTLED" || original.Effect.Outcome != "APPLIED" || original.Effect.LateEffect != "RULED_OUT" || !proto.Equal(original.Execution, before.Execution) {
		t.Fatalf("original: %v %v", original, err)
	}
	raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, relation.ObservationRef)
	if err != nil || !proto.Equal(raw.OperationId, qa.OperationId) || proto.Equal(raw.AttemptId, before.Execution.Attempt.Ref.Name) {
		t.Fatalf("relabeled query observation: %v %v", raw, err)
	}
	planning, err := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
	if err != nil || len(planning.AdmissionRefs) != 2 {
		t.Fatalf("incomplete action list: %v %v", planning, err)
	}
	reports, err := f.h.Ledger.QueryReports(f.ctx, f.caller, relation.ObservationRef)
	if err != nil || reports.UsageReceipt == nil {
		t.Fatalf("query billing missing: %v %v", reports, err)
	}
	if err = f.h.Close(); err != nil {
		t.Fatal(err)
	}
	f.h, err = assembly.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	again, err := f.h.Ledger.RequestReconciliation(f.ctx, f.caller, c)
	accepted(t, again, err)
	if !proto.Equal(again, r) {
		t.Fatalf("request replay changed: %v %v", again, r)
	}
	if err = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 {
		t.Fatalf("target receives=%v effects=%v", requests, effects)
	}
}

func configureReconciliation(t *testing.T, f *fixture) (*v1.Ref, *v1.Ref) {
	t.Helper()
	original, err := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if err != nil {
		t.Fatal(err)
	}
	original.Ref = nil
	original.ApprovedBy = nil
	original.AdapterRef.Name.LocalId = "simulator-queryable"
	r, err := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("queryable-write"), Capability: original})
	accepted(t, r, err)
	f.capability = r.ResultRef
	read := proto.Clone(original).(*v1.Capability)
	read.Action = "QUERY"
	read.UseRight = "READ"
	fee := int64(5)
	read.FeeCeiling = &fee
	r, err = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("read-capability"), Capability: read})
	accepted(t, r, err)
	capRef := r.ResultRef
	r, err = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("read-grant"), Grant: &v1.Grant{Subject: f.task.Name, Permissions: []*v1.PermissionClause{{Action: "QUERY", Resource: read.Resource, UseRight: "READ", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: read.ExecutorEndpointId}, {Action: "QUERY", Resource: read.Resource, UseRight: "SAVE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: read.ExecutorEndpointId}}, ValidFromUnixMs: time.Now().Add(-time.Minute).UnixMilli(), ValidUntilUnixMs: time.Now().Add(time.Hour).UnixMilli(), UseMode: "CONTINUOUS", UsePoolId: "read-root"}})
	accepted(t, r, err)
	return capRef, r.ResultRef
}

// 规则：G1、G3、G11、V4
func TestWeakQueryPreservesDelayedWriteAndDurableSchedule(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("accept-and-delay")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	requests, effects := target.Snapshot()
	if e != nil || original.Effect.Outcome != "UNKNOWN" || len(requests) != 1 || len(effects) != 0 {
		t.Fatalf("accepted is not applied: %v %v %v %v", original, e, requests, effects)
	}
	target.SetQueryRetryAfter(1000)
	c := reconciliationCommand(f, a, cap, grant)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, c)
	accepted(t, r, e)
	duplicate := proto.Clone(c).(*v1.RequestReconciliationCommand)
	duplicate.Header = ledgerHeader("duplicate-wakeup")
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, duplicate)
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "WAITING" || plan.CheckCount != 1 || plan.NextReconcileAtUnixMs < time.Now().Add(700*time.Millisecond).UnixMilli() {
		t.Fatalf("schedule: %v %v", plan, e)
	}
	original, e = f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("weak absence: %v %v", original, e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	restored, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || restored.NextReconcileAtUnixMs != plan.NextReconcileAtUnixMs || restored.CheckCount != 1 {
		t.Fatalf("schedule resampled: %v %v", restored, e)
	}
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	requests, effects = target.Snapshot()
	if len(requests) != 2 || len(effects) != 0 {
		t.Fatalf("early resend: %v %v", requests, effects)
	}
	target.ReleasePending()
	time.Sleep(time.Until(time.UnixMilli(plan.NextReconcileAtUnixMs)) + 20*time.Millisecond)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "COMPLETED" || plan.CheckCount != 2 {
		t.Fatalf("late proof: %v %v", plan, e)
	}
	requests, effects = target.Snapshot()
	if len(requests) != 3 || requests[1].Method != "GET" || requests[2].Method != "GET" || len(effects) != 1 {
		t.Fatalf("unsafe duplicate write: %v %v", requests, effects)
	}
}
func reconciliationCommand(f *fixture, a *v1.Admission, cap, grant *v1.Ref) *v1.RequestReconciliationCommand {
	return &v1.RequestReconciliationCommand{Header: ledgerHeader("reconcile"), OperationId: a.OperationId, QueryCapabilityRef: cap, ParametersRef: f.parameters, GrantRef: grant, PolicyVersion: 1, Limits: &v1.ReconciliationLimits{MaxChecks: 3, DeadlineUnixMs: time.Now().Add(time.Hour).UnixMilli(), MaxFee: 30, InitialDelayMs: 20, MaxDelayMs: 1000}}
}

// 规则：G1、G2、G11
func TestNegativeQueryRequiresProofCoveringTheOriginalLateRequest(t *testing.T) {
	for _, mode := range []string{"weak", "absent-terminal-weak", "strong-negative"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("accept-and-delay")
			target.SetQueryBehavior(mode)
			f := newFixtureWithTarget(t, 200, 200, false, target)
			cap, grant := configureReconciliation(t, f)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "strong-negative" {
				if original.Effect.Outcome != "NOT_APPLIED" || original.Effect.LateEffect != "RULED_OUT" || original.Lifecycle != "SETTLED" {
					t.Fatalf("strong proof: %v", original)
				}
			} else if original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" || original.Lifecycle == "SETTLED" {
				t.Fatalf("weak negative promoted: %v", original)
			}
			target.ReleasePending()
			requests, effects := target.Snapshot()
			expected := 1
			if mode == "strong-negative" {
				expected = 0
			}
			if len(requests) != 2 || requests[1].Method != "GET" || len(effects) != expected {
				t.Fatalf("negative proof target: %v %v", requests, effects)
			}
		})
	}
}

// 规则：G1、G2、G3、G10、G11
func TestUnprovedQueryResultPausesItsOwnResponsibility(t *testing.T) {
	for _, mode := range []string{"drop", "malformed", "wrong-subject", "no-read-terminal"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("drop-after-apply")
			target.SetQueryBehavior(mode)
			f := newFixtureWithTarget(t, 200, 200, false, target)
			cap, grant := configureReconciliation(t, f)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || plan.State != "PAUSED" || plan.PauseReason != "QUERY_RESULT_UNKNOWN" || plan.CheckCount != 1 {
				t.Fatalf("query result pause: %v %v", plan, e)
			}
			relation, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, plan.QueryRefs[0])
			if e != nil {
				t.Fatal(e)
			}
			query, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
			if e != nil || query.Effect.Outcome != "UNKNOWN" || query.Effect.LateEffect != "MAY_OCCUR" || query.Lifecycle == "SETTLED" {
				t.Fatalf("unproved read: %v %v", query, e)
			}
			aquery, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, query.AdmissionRef)
			if e != nil {
				t.Fatal(e)
			}
			reservation, e := f.h.Budget.QueryReservation(f.ctx, f.caller, aquery.BudgetBasis.ReservationRef)
			if e != nil || reservation.Ceiling != 5 {
				t.Fatalf("query cost disappeared: %v %v", reservation, e)
			}
			if e = f.h.Close(); e != nil {
				t.Fatal(e)
			}
			f.h, e = assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			target.SetQueryBehavior("")
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 2 || len(effects) != 1 {
				t.Fatalf("recursive or implicit retry: %v %v", requests, effects)
			}
		})
	}
}

// 规则：G1、G3、G8、G10、G11
func TestReconciliationLimitsPauseDurablyAndResumeOriginalResponsibility(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("accept-and-delay")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	c := reconciliationCommand(f, a, cap, grant)
	c.Limits.MaxChecks = 1
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, c)
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "PAUSED" || plan.PauseReason != "CHECK_LIMIT" || plan.CheckCount != 1 {
		t.Fatalf("limit: %v %v", plan, e)
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 0 {
		t.Fatalf("paused query ran: %v %v", requests, effects)
	}
	target.ReleasePending()
	limits := proto.Clone(c.Limits).(*v1.ReconciliationLimits)
	limits.MaxChecks = 2
	resume := &v1.ControlReconciliationCommand{Header: ledgerHeader("resume"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "RESUME", Limits: limits}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, resume)
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	plan, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "COMPLETED" || plan.CheckCount != 2 {
		t.Fatalf("resumed: %v %v", plan, e)
	}
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(original.Execution.Attempt.Ref.Name, start.Binding.AttemptId) || original.ExecutorEndpointId != a.ExecutorEndpointId {
		t.Fatalf("moved original: %v %v", original, e)
	}
	again, e := f.h.Ledger.ControlReconciliation(f.ctx, f.caller, resume)
	if e != nil || !proto.Equal(again, r) {
		t.Fatalf("resume replay: %v %v", again, e)
	}
	requests, effects = target.Snapshot()
	if len(requests) != 3 || len(effects) != 1 {
		t.Fatalf("resume duplicate: %v %v", requests, effects)
	}
}

// 规则：G1、G4、G8、G10、准入-7、准入-8、准入-10
func TestReconciliationRefusesMissingReadSaveAndQueryBudget(t *testing.T) {
	cases := []struct{ mode, reason string }{{"write-only", "GRANT_SCOPE_MISMATCH"}, {"read-only", "GRANT_SCOPE_MISMATCH"}, {"budget", "BUDGET_EXCEEDED"}, {"unknown-fee", "FEE_CEILING_UNKNOWN"}, {"no-send-budget", "COST_CEILING_UNKNOWN"}, {"plan-fee", "FEE_LIMIT"}, {"deadline", "TIME_LIMIT"}}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("drop-after-apply")
			limit := int64(200)
			if tc.mode == "budget" {
				limit = 30
			}
			f := newFixtureWithTarget(t, 200, limit, false, target)
			cap, grant := configureReconciliation(t, f)
			validGrant := grant
			if tc.mode == "write-only" {
				grant = f.grant
			}
			if tc.mode == "read-only" {
				g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, grant)
				if e != nil {
					t.Fatal(e)
				}
				g.Ref = nil
				g.Issuer = nil
				g.Status = ""
				g.SemanticVersion = 0
				g.Permissions = g.Permissions[:1]
				r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("read-without-save"), Grant: g})
				accepted(t, r, e)
				grant = r.ResultRef
			}
			if tc.mode == "unknown-fee" || tc.mode == "no-send-budget" {
				c, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, cap)
				if e != nil {
					t.Fatal(e)
				}
				c.Ref = nil
				c.ApprovedBy = nil
				if tc.mode == "unknown-fee" {
					c.FeeCeiling = nil
				} else {
					c.MaxSends = 0
				}
				r, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("limited-query"), Capability: c})
				accepted(t, r, e)
				cap = r.ResultRef
			}
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			c := reconciliationCommand(f, a, cap, grant)
			if tc.mode == "plan-fee" {
				c.Limits.MaxFee = 4
			}
			if tc.mode == "deadline" {
				c.Limits.DeadlineUnixMs = time.Now().Add(100 * time.Millisecond).UnixMilli()
			}
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, c)
			accepted(t, r, e)
			if tc.mode == "deadline" {
				time.Sleep(120 * time.Millisecond)
			}
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || p.State != "PAUSED" || p.PauseReason != tc.reason {
				t.Fatalf("gate %s: %v %v", tc.mode, p, e)
			}
			planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, a.TaskId)
			if e != nil || len(planning.AdmissionRefs) != 1 {
				t.Fatalf("partial admission: %v %v", planning, e)
			}
			reservations, e := f.h.Budget.QueryReservations(f.ctx, f.caller, a.TaskId)
			if e != nil || len(reservations) != 1 {
				t.Fatalf("partial reservation: %v %v", reservations, e)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != 1 {
				t.Fatalf("gate leaked GET: %v %v", requests, effects)
			}
			if tc.mode == "write-only" || tc.mode == "read-only" {
				target.SetBehavior("")
				r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("restore-read-save"), OperationId: a.OperationId, ExpectedRevision: p.Ref.Revision, Action: "RESUME", GrantRef: validGrant})
				accepted(t, r, e)
				if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
					t.Fatal(e)
				}
				p, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
				if e != nil || p.State != "COMPLETED" {
					t.Fatalf("restored authorization: %v %v", p, e)
				}
				requests, effects = target.Snapshot()
				if len(requests) != 2 || len(effects) != 1 {
					t.Fatalf("restored query: %v %v", requests, effects)
				}
			}
		})
	}
}

// 规则：G1、G3、G11、R7
func TestReconciliationProgressWakesTaskThroughDurableOwnerNotice(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, e)
	notices, e := f.h.Ledger.QueryOperationProgress(f.ctx, f.caller, a.OperationId)
	if e != nil || len(notices) != 1 || notices[0].RecipientReceipt != nil {
		t.Fatalf("source intent: %v %v", notices, e)
	}
	if e = f.h.Ledger.ProcessOperationProgress(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	waiting, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil || waiting.Progress != v1.TaskProgress_TASK_PROGRESS_WAITING || len(waiting.WaitingOn) != 1 {
		t.Fatalf("waiting task: %v %v", waiting, e)
	}
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Ledger.ProcessOperationProgress(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	awake, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil || awake.Progress != v1.TaskProgress_TASK_PROGRESS_RUNNING || len(awake.WaitingOn) != 0 {
		t.Fatalf("resolved task stuck: %v %v", awake, e)
	}
	notices, e = f.h.Ledger.QueryOperationProgress(f.ctx, f.caller, a.OperationId)
	if e != nil || len(notices) < 2 {
		t.Fatalf("missing effect notice: %v %v", notices, e)
	}
	for _, n := range notices {
		if n.RecipientReceipt == nil {
			t.Fatalf("unacknowledged: %v", n)
		}
		actual, e := f.h.Tasks.QueryOperationProgress(f.ctx, f.caller, n.Notice.Ref)
		if e != nil || !proto.Equal(actual, n.Notice) {
			t.Fatalf("receiver missing: %v %v", actual, e)
		}
	}
	// 已保存的旧事件重放不能把当前任务重新置为等待。
	old := notices[0]
	source := &v1.Caller{UserId: "u", IssuerId: "ledger-progress"}
	replay, e := f.h.Tasks.AcceptOperationProgress(f.ctx, source, old.Command)
	if e != nil || !proto.Equal(replay, old.RecipientReceipt) {
		t.Fatalf("notice replay: %v %v", replay, e)
	}
	after, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil || !proto.Equal(after, awake) {
		t.Fatalf("stale event changed task: %v %v", after, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("notification resent: %v %v", requests, effects)
	}
}

// 规则：G1、G3、G11、V4、R7
func TestRestartRunsPersistedReconciliationAndDeliversWakeup(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, e)
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || p.State != "COMPLETED" || p.CheckCount != 1 {
		t.Fatalf("restart lost responsibility: %v %v", p, e)
	}
	notices, e := f.h.Ledger.QueryOperationProgress(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	for _, n := range notices {
		if n.RecipientReceipt == nil {
			t.Fatalf("restart lost notice: %v", n)
		}
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if e != nil || task.Progress != v1.TaskProgress_TASK_PROGRESS_RUNNING {
		t.Fatalf("restart left wait: %v %v", task, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatalf("restart IO: %v %v", requests, effects)
	}
}

// 规则：G1、G3、G11、V4
func TestManualReconciliationPauseSurvivesInFlightWeakObservation(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("accept-and-delay")
	entered, release := make(chan struct{}, 1), make(chan struct{})
	target.SetQueryGate(entered, release)
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, e)
	done := make(chan error, 1)
	go func() { done <- f.h.Ledger.ProcessReconciliations(f.ctx, f.caller) }()
	<-entered
	p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("pause-in-flight"), OperationId: a.OperationId, ExpectedRevision: p.Ref.Revision, Action: "PAUSE"})
	accepted(t, r, e)
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	p, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || p.State != "PAUSED" || p.PauseReason != "USER_PAUSED" || p.ActiveQueryRef != nil {
		t.Fatalf("manual pause overwritten: %v %v", p, e)
	}
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 0 {
		t.Fatalf("paused query repeated: %v %v", requests, effects)
	}
}

// 规则：G1、G3、G4、G11、R7
func TestReconciliationOwnerCannotBeBypassedByGenericJobOrForgedClosure(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, e)
	p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("claim-reconciliation").Identity, ContractVersion: 1, Action: "CLAIM", Module: "ledger", JobRef: p.JobRef, AllowedTypes: []string{"RECONCILE_OPERATION"}, ProcessInstance: "test-owner", Limit: 1, LeaseMs: 30000})
	accepted(t, r, e)
	claim := r.Jobs[0]
	for _, action := range []string{"PROGRESS", "CONTROL"} {
		next := "COMPLETED"
		if action == "CONTROL" {
			next = "CLOSED"
		}
		r, e = f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("bypass-" + action).Identity, ContractVersion: 1, Action: action, Module: "ledger", JobRef: claim.Ref, ProcessInstance: claim.ProcessInstance, ClaimEpoch: claim.ClaimEpoch, NextState: next})
		if e != nil || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" {
			t.Fatalf("generic command abandoned responsibility: %v %v", r, e)
		}
	}
	forged := &v1.Ref{Name: &v1.GlobalName{UserId: "u", AuthorityDomainId: "d/ledger", ObjectKind: "closure-work", LocalId: "forged"}, Revision: 1, SchemaId: "lerna.v1.ClosureWorkRequest"}
	source := &v1.Caller{UserId: "u", IssuerId: "ledger-reconciliation"}
	fh := header("forged-closure")
	fh.Identity.IssuerId = source.IssuerId
	r, e = f.h.Tasks.AdmitClosure(f.ctx, source, &v1.AdmitClosureCommand{Header: fh, WorkRef: forged})
	if e != nil || r.GetError().GetCode() != "INVALID_CLOSURE_ORIGIN" {
		t.Fatalf("self-tag accepted: %v %v", r, e)
	}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("pause-owner"), OperationId: a.OperationId, ExpectedRevision: p.Ref.Revision, Action: "PAUSE"})
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliationClaim(f.ctx, f.caller, claim); e == nil {
		t.Fatal("stale owner advanced after pause")
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("stale or forged IO: %v %v", requests, effects)
	}
}

// 规则：G1、G3、G11、V4
func TestLateOriginalResponseCompletesPausedReconciliationWithoutAQuery(t *testing.T) {
	target := simulator.New("queryable")
	entered, release := make(chan struct{}, 1), make(chan struct{})
	target.SetWriteResponseGate(entered, release)
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	done := make(chan error, 1)
	go func() {
		_, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
		done <- e
	}()
	<-entered
	r, e := f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
	accepted(t, r, e)
	p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("pause-before-late-fact"), OperationId: a.OperationId, ExpectedRevision: p.Ref.Revision, Action: "PAUSE"})
	accepted(t, r, e)
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	p, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || p.State != "COMPLETED" || p.CheckCount != 0 {
		t.Fatalf("late original fact lost: %v %v", p, e)
	}
	if e = f.h.Ledger.ProcessOperationProgress(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("unnecessary query: %v %v", requests, effects)
	}
}

// 规则：G1、G3、G11、V4
func TestQueryCapabilityLossKeepsOriginalEffectUnknown(t *testing.T) {
	for _, mode := range []string{"retention-expired", "temporarily-unavailable"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("drop-after-apply")
			target.SetQueryBehavior(mode)
			target.SetQueryRetryAfter(1000)
			f := newFixtureWithTarget(t, 200, 200, false, target)
			cap, grant := configureReconciliation(t, f)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "retention-expired" {
				if p.State != "PAUSED" || p.PauseReason != "QUERY_RETENTION_EXPIRED" {
					t.Fatalf("retention: %v", p)
				}
			} else if p.State != "WAITING" || p.NextReconcileAtUnixMs < time.Now().Add(500*time.Millisecond).UnixMilli() {
				t.Fatalf("availability backoff: %v", p)
			}
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || op.Effect.Outcome != "UNKNOWN" || op.Effect.LateEffect != "MAY_OCCUR" {
				t.Fatalf("capability loss became proof: %v %v", op, e)
			}
			q, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, p.QueryRefs[0])
			if e != nil {
				t.Fatal(e)
			}
			read, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, q.QueryOperationRef.Name)
			if e != nil || read.Lifecycle != "SETTLED" {
				t.Fatalf("own read not terminal: %v %v", read, e)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 2 || len(effects) != 1 {
				t.Fatalf("capability loss retried write: %v %v", requests, effects)
			}
		})
	}
}

// 规则：G3、G4、G11、准入-2、开始-2
func TestClosureQueriesRemainEligibleDuringTaskControlsAndUnboundInput(t *testing.T) {
	for _, mode := range []string{"PAUSE", "CANCEL", "MODIFY"} {
		t.Run(mode, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("drop-after-apply")
			f := newFixtureWithTarget(t, 200, 200, false, target)
			cap, grant := configureReconciliation(t, f)
			a, start := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
			if e != nil {
				t.Fatal(e)
			}
			input := &v1.SubmitInputCommand{Header: header("control-before-query"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "CONTROL", Control: mode, ExpectedControlGeneration: task.ControlGeneration}
			if mode == "MODIFY" {
				input.InputKind = "MODIFY"
				input.ExpectedControlGeneration = 0
				input.Control = ""
			}
			if mode == "MODIFY" {
				input.ContentRef = task.GoalRef
				input.ExpectedInputVersion = task.InputVersion
				input.ExpectedRequirementsVersion = task.RequirementsVersion
			}
			r, e = f.h.Sessions.SubmitInput(f.ctx, f.caller, input)
			accepted(t, r, e)
			before, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
			if e != nil {
				t.Fatal(e)
			}
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			if e = f.h.Ledger.ProcessOperationProgress(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || p.State != "COMPLETED" {
				t.Fatalf("closure blocked by target-only gate: %v %v", p, e)
			}
			after, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
			if e != nil || after.Control != before.Control || after.ControlGeneration != before.ControlGeneration || after.InputVersion != before.InputVersion || after.BoundInputVersion != before.BoundInputVersion || after.Progress == v1.TaskProgress_TASK_PROGRESS_RUNNING {
				t.Fatalf("closure resumed target work: %v %v", after, e)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 2 || len(effects) != 1 {
				t.Fatalf("closure IO: %v %v", requests, effects)
			}
		})
	}
}
