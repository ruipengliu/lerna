package admission_test

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G5、G11、R3、开始-2
func TestCLIRefusesReservedTakeoverAndReturnWithoutChangingOriginalResponsibility(t *testing.T) {
	for _, control := range []string{"TAKEOVER", "RETURN"} {
		t.Run(control, func(t *testing.T) {
			target := simulator.NewBillingTarget(25)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			a, start := prepareStart(t, f)
			task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil {
				t.Fatal(e)
			}
			goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
			if e != nil {
				t.Fatal(e)
			}
			session, e := f.h.Sessions.QuerySession(f.ctx, f.caller, goal.Receipt.SessionRef.Name)
			if e != nil {
				t.Fatal(e)
			}
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil {
				t.Fatal(e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil {
				t.Fatal(e)
			}
			cli := interaction.CLI{Sessions: f.h.Sessions, Tasks: f.h.Tasks, Durable: f.h.Durable, Content: f.h.Content, Ledger: f.h.Ledger, Egress: f.h.Egress, Caller: f.caller, Domain: "d"}
			id := "unsupported-" + control
			args := []string{"input", "--command", id, "--session", session.SessionId.LocalId, "--task", task.TaskId.LocalId, "--kind", "CONTROL", "--control", control, "--control-generation", strconv.FormatUint(task.ControlGeneration, 10)}
			var original *v1.CommandReceipt
			for range 2 {
				var out bytes.Buffer
				if e = cli.Run(f.ctx, args, &out); e != nil {
					t.Fatal(e)
				}
				r := new(v1.CommandReceipt)
				if e = protojson.Unmarshal(out.Bytes(), r); e != nil {
					t.Fatal(e)
				}
				if r.Decision != v1.Decision_DECISION_REJECTED || r.GetError().GetCode() != "UNSUPPORTED_FEATURE" || r.ResultRef != nil {
					t.Fatalf("unsupported control: %v", r)
				}
				if original == nil {
					original = r
				} else if !proto.Equal(original, r) {
					t.Fatal("replay changed original refusal")
				}
				q, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header(id).Identity)
				if e != nil || !proto.Equal(q.Receipt, original) {
					t.Fatalf("refusal query: %v %v", q, e)
				}
			}
			afterTask, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
			if e != nil || !proto.Equal(task, afterTask) {
				t.Fatalf("task advanced: %v %v", afterTask, e)
			}
			afterPlanning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil || !proto.Equal(planning, afterPlanning) {
				t.Fatalf("planning advanced: %v %v", afterPlanning, e)
			}
			afterSession, e := f.h.Sessions.QuerySession(f.ctx, f.caller, session.SessionId)
			if e != nil || !proto.Equal(session, afterSession) {
				t.Fatalf("session advanced: %v %v", afterSession, e)
			}
			afterOp, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || !proto.Equal(op, afterOp) {
				t.Fatalf("original execution changed: %v %v", afterOp, e)
			}
			afterBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || !proto.Equal(budget, afterBudget) {
				t.Fatalf("budget changed: %v %v", afterBudget, e)
			}
			afterGoal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
			if e != nil || !proto.Equal(goal, afterGoal) {
				t.Fatalf("original receipt changed: %v %v", afterGoal, e)
			}
			assertUnstarted(t, f, a, start)
			calls, effects := target.Target.Snapshot()
			if len(calls) != 0 || len(effects) != 0 || len(target.Bills()) != 0 {
				t.Fatal("unsupported control invoked target")
			}
		})
	}
}

// 规则：G3、G4、G5、G11、R3、开始-1
func TestUnsupportedBackendReferencesRefusePreparationAndRetainOriginalAdmission(t *testing.T) {
	// 这些是请求中的未知适配器标识，不是已实现的设备或 Agent 协议。
	for _, backend := range []string{"device", "subprocess", "gui", "agent"} {
		t.Run(backend, func(t *testing.T) {
			target := simulator.NewBillingTarget(25)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if e != nil {
				t.Fatal(e)
			}
			cap.Ref, cap.ApprovedBy = nil, nil
			cap.AdapterRef.Name.LocalId = backend
			configured, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("unsupported-capability"), Capability: cap})
			accepted(t, configured, e)
			f.capability = configured.ResultRef
			proposal := f.propose(t, nil)
			admit := &v1.AdmitCommand{Header: header("unsupported-admission"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant}
			receipt, e := f.h.Tasks.Admit(f.ctx, f.caller, admit)
			accepted(t, receipt, e)
			a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, receipt.ResultRef)
			if e != nil {
				t.Fatal(e)
			}
			if e = f.h.Tasks.ProcessHandoffs(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			claim, e := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("unsupported-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "worker"})
			accepted(t, claim, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || op.Execution != nil {
				t.Fatalf("preparation already exists: %v %v", op, e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil {
				t.Fatal(e)
			}
			c := &v1.PrepareExecutionCommand{Header: ledgerHeader("unsupported-prepare"), OperationId: a.OperationId, ProcessInstance: "worker", Claim: claim.Jobs[0]}
			var refusal *v1.CommandReceipt
			for range 2 {
				r, e := f.h.Ledger.Prepare(f.ctx, f.caller, c)
				if e != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.GetError().GetCode() != "UNSUPPORTED_CAPABILITY" || r.ResultRef != nil {
					t.Fatalf("unsupported backend: %v %v", r, e)
				}
				if refusal == nil {
					refusal = r
				} else if !proto.Equal(refusal, r) {
					t.Fatal("preparation refusal changed")
				}
				q, e := f.h.Ledger.QueryReceipt(f.ctx, f.caller, c.Header.Identity)
				if e != nil || !proto.Equal(q.Receipt, refusal) {
					t.Fatalf("refusal missing: %v %v", q, e)
				}
			}
			after, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || !proto.Equal(op, after) {
				t.Fatalf("original operation changed: %v %v", after, e)
			}
			job, e := f.h.LedgerWork.QueryJob(f.ctx, f.caller, claim.Jobs[0].Ref.Name)
			if e != nil || !proto.Equal(job, claim.Jobs[0]) {
				t.Fatalf("original claim changed: %v %v", job, e)
			}
			saved, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, a.Ref)
			if e != nil || !proto.Equal(saved, a) {
				t.Fatalf("admission changed: %v %v", saved, e)
			}
			r, e := f.h.Tasks.Admit(f.ctx, f.caller, admit)
			accepted(t, r, e)
			if !proto.Equal(r, receipt) {
				t.Fatal("original admission receipt changed")
			}
			b, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || !proto.Equal(b, budget) {
				t.Fatalf("budget changed: %v %v", b, e)
			}
			calls, effects := target.Target.Snapshot()
			if len(calls) != 0 || len(effects) != 0 || len(target.Bills()) != 0 || f.calls.Load() != 0 {
				t.Fatal("unsupported backend produced an external call/effect/fee")
			}
		})
	}
}
