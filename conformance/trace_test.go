package conformance_test

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G3、G8、G11、R7
func TestTraceTaskSourceSurvivesRestartAndRetainsOriginalAcceptance(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "trace.db")
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	caller := &v1.Caller{UserId: "alice", IssuerId: "cli"}
	c := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "trace-goal"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "SECRET-BODY-MARKER"}
	if _, err = h.Sessions.SubmitGoal(ctx, caller, c); err != nil {
		t.Fatal(err)
	}
	if err = h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	receipt, err := h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if err != nil {
		t.Fatal(err)
	}
	query := &v1.TraceQuery{TaskId: receipt.Receipt.TaskRef.Name}
	before, err := h.Trace.Query(ctx, caller, query)
	if err != nil {
		t.Fatal(err)
	}
	if before.Complete || before.Backlog == 0 {
		t.Fatalf("pending source hidden: %v", before)
	}
	if err = h.Close(); err != nil {
		t.Fatal(err)
	}
	h, err = assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	after, err := h.Trace.Query(ctx, caller, query)
	if err != nil {
		t.Fatal(err)
	}
	if !after.Complete || after.Backlog != 0 || len(after.Events) == 0 {
		t.Fatalf("missing original task source: %v", after)
	}
	if err = h.Trace.Recover(ctx, caller); err != nil {
		t.Fatal(err)
	}
	again, err := h.Trace.Query(ctx, caller, query)
	if err != nil || !proto.Equal(again, after) {
		t.Fatalf("recovery changed source: %v %v", again, err)
	}
	var output bytes.Buffer
	cli := interaction.CLI{Trace: h.Trace, Caller: caller, Domain: "local"}
	if err = cli.Run(ctx, []string{"trace-task", query.TaskId.LocalId}, &output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "SECRET-BODY-MARKER") || !strings.Contains(output.String(), "TASK_CHANGED") {
		t.Fatalf("unsafe diagnostic output: %s", output.String())
	}

}

// 规则：G1、G3、G8、G12、R7
func TestTraceIndexLagAndForgedSourceCannotClaimCompleteness(t *testing.T) {
	ctx := context.Background()
	h, err := assembly.Open(filepath.Join(t.TempDir(), "lag.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	caller := &v1.Caller{UserId: "alice", IssuerId: "cli"}
	c := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "cli", TargetDomainId: "local", CommandId: "lag"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "private"}
	if _, err = h.Sessions.SubmitGoal(ctx, caller, c); err != nil {
		t.Fatal(err)
	}
	if err = h.Sessions.ProcessPending(ctx, caller); err != nil {
		t.Fatal(err)
	}
	receipt, err := h.Durable.QueryReceipt(ctx, caller, c.Identity)
	if err != nil {
		t.Fatal(err)
	}
	q := &v1.TraceQuery{TaskId: receipt.Receipt.TaskRef.Name}
	if err = h.Trace.Collect(ctx, caller); err != nil {
		t.Fatal(err)
	}
	lag, err := h.Trace.Query(ctx, caller, q)
	if err != nil {
		t.Fatal(err)
	}
	if lag.Complete || lag.Backlog != 0 || lag.IndexBacklog == 0 {
		t.Fatalf("index lag hidden: %v", lag)
	}
	if err = h.Trace.Index(ctx, caller); err != nil {
		t.Fatal(err)
	}
	good, err := h.Trace.Query(ctx, caller, q)
	if err != nil || !good.Complete {
		t.Fatalf("index did not catch up: %v %v", good, err)
	}
	q.ProcessingPurpose = "EVALUATION"
	if _, err = h.Trace.Query(ctx, caller, q); err == nil {
		t.Fatal("diagnostic authorization expanded")
	}
	q.ProcessingPurpose = ""
	if _, err = h.Trace.Query(ctx, &v1.Caller{UserId: "other", IssuerId: "cli"}, q); err == nil {
		t.Fatal("foreign user read")
	}
}
