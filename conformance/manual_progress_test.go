package conformance_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/infra/hosting"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G4、G11、R6、R7
func TestManualProgressUsesOriginalCallerAndKeepsOptionalAbsence(t *testing.T) {
	ctx := context.Background()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "manual.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	caller := &v1.Caller{UserId: "alice", IssuerId: "ordinary-cli"}
	c := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "ordinary-cli", TargetDomainId: "local", CommandId: "manual-original"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "preserve original caller"}
	submitted, e := h.Sessions.SubmitGoal(ctx, caller, c)
	if e != nil || submitted.Phase != v1.ReceiptPhase_RECEIPT_PHASE_SUBMITTED {
		t.Fatalf("original pending command: %v %v", submitted, e)
	}
	host, e := hosting.NewManual(hosting.ManualDependencies{Sessions: h.Sessions})
	if e != nil {
		t.Fatal(e)
	}
	cli := interaction.CLI{Progress: host, Caller: caller, Domain: "local"}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if e = cli.Run(cancelled, []string{"recover"}, new(bytes.Buffer)); e == nil || e.Error() != "DEPENDENCY_UNAVAILABLE" {
		t.Fatalf("manual progress replaced caller context/error: %v", e)
	}
	q, e := h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if e != nil || !proto.Equal(q.Receipt, submitted) {
		t.Fatal("cancelled progress lost original responsibility", e)
	}
	if e = cli.Run(ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	q, e = h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED || !proto.Equal(q.Receipt.Identity, c.Identity) || q.Receipt.TaskRef == nil {
		t.Fatalf("manual progress changed original decision: %v %v", q, e)
	}
	input, e := h.Sessions.QuerySession(ctx, caller, q.Receipt.SessionRef.Name)
	if e != nil || len(input.Inputs) != 1 || input.LastCommittedSeq != 1 || !proto.Equal(input.Inputs[0].CommandIdentity, c.Identity) {
		t.Fatalf("manual progress changed original input: %v %v", input, e)
	}
	fixed := proto.Clone(q.Receipt).(*v1.CommandReceipt)
	if e = cli.Run(ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	q, e = h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if e != nil || !proto.Equal(q.Receipt, fixed) {
		t.Fatal("manual replay changed original receipt", e)
	}

	c = proto.Clone(c).(*v1.SubmitGoalCommand)
	c.Identity.CommandId = "manual-leased-original"
	submitted, e = h.Sessions.SubmitGoal(ctx, caller, c)
	if e != nil {
		t.Fatal(e)
	}
	claim, e := h.Durable.ExecuteJob(ctx, caller, &v1.JobCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "ordinary-cli", TargetDomainId: "local", CommandId: "manual-old-worker"}, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"DECIDE_GOAL"}, Limit: 1, LeaseMs: 60000, ProcessInstance: "original-worker"})
	if e != nil || len(claim.Jobs) != 1 {
		t.Fatalf("original leased responsibility: %v %v", claim, e)
	}
	if e = cli.Run(ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	job, e := h.Durable.QueryJob(ctx, caller, claim.Jobs[0].Ref.Name)
	if e != nil || !proto.Equal(job, claim.Jobs[0]) {
		t.Fatal("manual progress took an unexpired original lease", e)
	}
	q, e = h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if e != nil || !proto.Equal(q.Receipt, submitted) {
		t.Fatal("manual progress replaced the leased original request", e)
	}
}

// 规则：G3、R6
func TestManualProgressRejectsMissingRequiredConfiguration(t *testing.T) {
	for _, required := range []hosting.SessionProgress{nil, (*sessions.Service)(nil)} {
		host, e := hosting.NewManual(hosting.ManualDependencies{Sessions: required})
		if host != nil || e == nil || !strings.Contains(e.Error(), "sessions") {
			t.Fatalf("incomplete hosting configuration: %v %v", host, e)
		}
	}
}
