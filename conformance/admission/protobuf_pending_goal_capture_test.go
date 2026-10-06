package admission_test

import (
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G1、G3、G11
func TestCapturePendingGoalPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	capturePendingGoal(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.PendingGoal", "lerna.v1.Job"})
	roundTripClosingSamples(t, samples)
}
func capturePendingGoal(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	f := newFixture(t, 100, 80, false)
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, "pending-goal-"+name, m, e)
	}
	goal := &v1.SubmitGoalCommand{Identity: header("capture-pending-goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "synthetic pending goal body", Credential: "synthetic-pending-credential", TraceId: "synthetic-trace"}
	submitted, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, goal)
	add("submitted", submitted, e)
	if submitted.Phase != v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED || submitted.JobRef == nil {
		t.Fatal("goal was not durably submitted")
	}
	ready, e := f.h.Durable.QueryJob(f.ctx, f.caller, submitted.JobRef.Name)
	add("ready-job", ready, e)
	add("payload", ready.Goal, nil)
	if ready.State != "READY" || ready.Goal.Command.Goal != "" || ready.Goal.Command.Credential != "" || ready.Goal.Command.TraceId != "" || !proto.Equal(ready.Goal.Command.Identity, goal.Identity) || !proto.Equal(ready.Goal.ContentRef, submitted.InputRef) || !proto.Equal(ready.SpecificationRef, submitted.InputRef) {
		t.Fatal("pending payload lost original identity or copied governed text")
	}
	body, e := f.h.Content.Read(f.ctx, f.caller, ready.Goal.ContentRef)
	add("body", body, e)
	if string(command.ContentBytes(body)) != goal.Goal {
		t.Fatal("pending body mismatch")
	}
	claim := &v1.JobCommand{Identity: header("capture-pending-claim").Identity, ContractVersion: 1, Action: "CLAIM", Module: "sessions", AllowedTypes: []string{"DECIDE_GOAL"}, JobRef: ready.Ref, Limit: 1, LeaseMs: 60000, ProcessInstance: "capture-pending-worker"}
	receipt, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, claim)
	accepted(t, receipt, e)
	add("claim-command", claim, nil)
	add("claim-receipt", receipt, nil)
	if len(receipt.Jobs) != 1 || !proto.Equal(receipt.Jobs[0].Goal, ready.Goal) || receipt.Jobs[0].ClaimEpoch == 0 {
		t.Fatal("actual claim lacks original goal")
	}
	add("claimed-job", receipt.Jobs[0], nil)
	if e = f.h.Sessions.ProcessClaim(f.ctx, receipt.Jobs[0]); e != nil {
		t.Fatal(e)
	}
	decided, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, goal.Identity)
	add("decided", decided, e)
	accepted(t, decided.Receipt, nil)
	completed, e := f.h.Durable.QueryJob(f.ctx, f.caller, ready.Ref.Name)
	add("completed-job", completed, e)
	if completed.State != "COMPLETED" || !proto.Equal(completed.Goal, ready.Goal) || decided.Receipt.TaskRef == nil {
		t.Fatal("goal decision lost original work")
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	replay, e := f.h.Durable.ExecuteJob(f.ctx, f.caller, claim)
	accepted(t, replay, e)
	add("claim-replay", replay, nil)
	again, e := f.h.Sessions.SubmitGoal(f.ctx, f.caller, goal)
	accepted(t, again, e)
	if !proto.Equal(replay, receipt) || !proto.Equal(again, decided.Receipt) || f.calls.Load() != 0 {
		t.Fatal("recovery changed original claim/decision or sent")
	}
	t.Log("actual submitted goal -> original public claim -> accepted decision; restart preserves original claim and decision, target requests 0")
}
