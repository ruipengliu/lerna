//go:build fault

package admission_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G10、G11、R7
func TestManualProgressLostObservationReceiptKeepsOriginalResponsibility(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	physical := performWithoutObservation(t, f, a, start)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress-io"}
	header := header("observe:" + physical.Observation.Ref.Name.LocalId)
	header.Identity.IssuerId, header.Identity.TargetDomainId = actor.IssuerId, "d/content"
	r, e := f.h.Content.RegisterObservation(f.ctx, actor, &v1.RegisterObservationCommand{Header: header, Observation: physical.Observation, Body: physical.Body})
	accepted(t, r, e)
	fault, e := sqlite.WithFault(f.ctx, "ledger.observation", sqlite.LoseReceipt)
	if e != nil {
		t.Fatal(e)
	}
	cli := interaction.CLI{Progress: f.h.Recovery, Caller: &v1.Caller{UserId: "u", IssuerId: "local-cli"}, Domain: "d"}
	if e = cli.Run(fault, []string{"recover"}, new(bytes.Buffer)); e == nil {
		t.Fatal("manual progress hid the original lost receipt")
	}
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "UNKNOWN" || original.Effect.LateEffect != "MAY_OCCUR" {
		t.Fatalf("later manual phase ran after original error: %v %v", original, e)
	}
	id := &v1.CommandIdentity{UserId: "u", IssuerId: "content-observation", TargetDomainId: "d/ledger", CommandId: "observe:" + physical.Observation.Ref.Name.LocalId}
	q, e := f.h.LedgerWork.QueryReceipt(f.ctx, &v1.Caller{UserId: "u", IssuerId: id.IssuerId}, id)
	if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || q.Receipt.Decision != v1.Decision_DECISION_ACCEPTED || !proto.Equal(q.Receipt.ResultRef, physical.Observation.Ref) {
		t.Fatalf("manual failure lost committed original receipt: %v %v", q, e)
	}
	if e = cli.Run(f.ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" || !proto.Equal(op.Execution.Attempt.Ref.Name, original.Execution.Attempt.Ref.Name) {
		t.Fatalf("manual replay replaced original effect responsibility: %v %v", op, e)
	}
	again, e := f.h.LedgerWork.QueryReceipt(f.ctx, &v1.Caller{UserId: "u", IssuerId: id.IssuerId}, id)
	if e != nil || !proto.Equal(again.Receipt, q.Receipt) {
		t.Fatal("manual replay changed original observation receipt", e)
	}
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Execution.Send.Ref)
	if e != nil || source.Status != "SETTLED" || source.Amount == nil || *source.Amount != 25 {
		t.Fatalf("manual replay lost original billing source: %v %v", source, e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatal("manual recovery repeated original physical send")
	}
}
