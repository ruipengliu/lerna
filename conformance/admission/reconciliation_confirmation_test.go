package admission_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G7、G8、G11、准入-9
func TestClosureConfirmationKeepsOwnerAcrossInvalidApprovalAndReplay(t *testing.T) {
	for _, change := range []string{"control-generation", "expired"} {
		t.Run(change, func(t *testing.T) {
			target := simulator.New("queryable")
			target.SetBehavior("drop-after-apply")
			f := newFixtureWithTarget(t, 200, 200, false, target)
			cap, grant := configureReconciliation(t, f)
			g, e := f.h.Grants.QueryGrant(f.ctx, f.caller, grant)
			if e != nil {
				t.Fatal(e)
			}
			g.Ref = nil
			g.Issuer = nil
			g.Status = ""
			g.SemanticVersion = 0
			g.ConfirmationRequired = true
			r, e := f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("confirmed-query-grant"), Grant: g})
			accepted(t, r, e)
			grant = r.ResultRef
			a, start := prepareStart(t, f)
			r, e = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
			accepted(t, r, e)
			r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, cap, grant))
			accepted(t, r, e)
			if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
				t.Fatal(e)
			}
			p, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || p.State != "PAUSED" || p.PauseReason != "CONFIRMATION_REQUIRED" {
				t.Fatalf("missing approval: %v %v", p, e)
			}
			q, e := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, p.ActiveQueryRef)
			if e != nil || q.AdmissionReceipt != nil || q.AdmissionCommand != nil {
				t.Fatalf("premature P1: %v %v", q, e)
			}
			work := proto.Clone(q.Work).(*v1.ClosureWorkRequest)
			goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
			if e != nil {
				t.Fatal(e)
			}
			makeApproval := func(id string, expires int64) (*v1.RequestClosureConfirmationCommand, *v1.CommandReceipt, *v1.Ref) {
				t.Helper()
				cmd := &v1.RequestClosureConfirmationCommand{Header: header("closure-confirmation-" + id), WorkRef: work.Ref, SessionId: goal.Receipt.SessionRef.Name, ExpiresAtUnixMs: expires}
				request, e := f.h.Tasks.RequestClosureConfirmation(f.ctx, f.caller, cmd)
				accepted(t, request, e)
				pending, e := f.h.Sessions.QueryConfirmation(f.ctx, f.caller, request.ResultRef)
				if e != nil || !proto.Equal(pending.GetOperationAdmission().ProposalRef, work.Ref) || !proto.Equal(pending.GetOperationAdmission().QuerySubject, work.QuerySubject) {
					t.Fatalf("wrong confirmation scope: %v %v", pending, e)
				}
				approved, e := f.h.Sessions.RespondConfirmation(f.ctx, f.caller, &v1.RespondConfirmationCommand{Header: header("approve-closure-" + id), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"})
				accepted(t, approved, e)
				return cmd, request, approved.ResultRef
			}
			expires := int64(0)
			if change == "expired" {
				expires = time.Now().Add(time.Second).UnixMilli()
			}
			_, _, wrong := makeApproval("old", expires)
			if change == "control-generation" {
				task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
				if e != nil {
					t.Fatal(e)
				}
				r, e = f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("pause-changes-confirmed-scope"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "CONTROL", Control: "PAUSE", ExpectedControlGeneration: task.ControlGeneration})
				accepted(t, r, e)
			} else {
				time.Sleep(time.Until(time.UnixMilli(expires)) + 20*time.Millisecond)
			}
			resume := func(id string, confirmation *v1.Ref) {
				t.Helper()
				p, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
				if e != nil {
					t.Fatal(e)
				}
				r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader(id), OperationId: a.OperationId, ExpectedRevision: p.Ref.Revision, Action: "RESUME", ConfirmationRef: confirmation})
				accepted(t, r, e)
				if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
					t.Fatal(e)
				}
			}
			resume("resume-wrong-scope", wrong)
			p, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || p.State != "PAUSED" {
				t.Fatalf("invalid approval ran: %v %v", p, e)
			}
			q, e = f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, p.ActiveQueryRef)
			if e != nil || q.AdmissionReceipt != nil || q.AdmissionCommand != nil || !proto.Equal(q.Work, work) {
				t.Fatalf("invalid approval poisoned original command: %v %v", q, e)
			}
			cmd, requested, correct := makeApproval("current", 0)
			resume("resume-correct-scope", correct)
			p, e = f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
			if e != nil || p.State != "COMPLETED" || p.CheckCount != 1 {
				t.Fatalf("approved owner incomplete: %v %v", p, e)
			}
			q, e = f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, p.QueryRefs[0])
			if e != nil || !proto.Equal(q.Work, work) || q.AdmissionCommand == nil {
				t.Fatalf("owner changed: %v %v", q, e)
			}
			consumed, e := f.h.Sessions.QueryCurrentConfirmation(f.ctx, f.caller, correct.Name)
			if e != nil || consumed.State != "CONSUMED" || !proto.Equal(consumed.GetConsumedAdmissionRef(), q.AdmissionReceipt.ResultRef) {
				t.Fatalf("confirmation not atomically consumed: %v %v", consumed, e)
			}
			data, e := protojson.Marshal(cmd)
			if e != nil {
				t.Fatal(e)
			}
			path := filepath.Join(t.TempDir(), "closure.json")
			if e = os.WriteFile(path, data, 0600); e != nil {
				t.Fatal(e)
			}
			cli := interaction.CLI{Tasks: f.h.Tasks, Ledger: f.h.Ledger, Caller: f.caller, Domain: "d"}
			var out bytes.Buffer
			if e = cli.Run(f.ctx, []string{"closure-confirmation", "--json", path}, &out); e != nil {
				t.Fatal(e)
			}
			cliReceipt := new(v1.CommandReceipt)
			if e = protojson.Unmarshal(out.Bytes(), cliReceipt); e != nil || !proto.Equal(cliReceipt, requested) {
				t.Fatalf("closure CLI replay: %v %v", cliReceipt, e)
			}
			replay, e := f.h.Tasks.RequestClosureConfirmation(f.ctx, f.caller, cmd)
			if e != nil || !proto.Equal(replay, requested) {
				t.Fatalf("confirmation request replay: %v %v", replay, e)
			}
			replay, e = f.h.Tasks.AdmitClosure(f.ctx, &v1.Caller{UserId: "u", IssuerId: "ledger-reconciliation"}, q.AdmissionCommand)
			if e != nil || !proto.Equal(replay, q.AdmissionReceipt) {
				t.Fatalf("P1 replay: %v %v", replay, e)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 2 || len(effects) != 1 {
				t.Fatalf("confirmation duplicate IO: %v %v", requests, effects)
			}
		})
	}
}
