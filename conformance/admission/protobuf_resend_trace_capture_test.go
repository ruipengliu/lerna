package admission_test

import (
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func captureResendHistory(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	target := simulator.New("idempotent")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	configureResendLimit(t, f, 2)
	a, first := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, err := f.h.Egress.Invoke(f.ctx, actor, first)
	accepted(t, r, err)
	before, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "resend-original-unknown", before, err)
	if before.Effect.Outcome != "UNKNOWN" {
		t.Fatal("lost response not unknown")
	}
	target.SetBehavior("")
	claim, err := f.h.LedgerWork.ExecuteJob(f.ctx, f.caller, &v1.JobCommand{Identity: ledgerHeader("capture-resend-claim").Identity, ContractVersion: 1, Action: "CLAIM", AllowedTypes: []string{"EXECUTE_OPERATION"}, Limit: 1, LeaseMs: 30000, ProcessInstance: "capture-resend-worker"})
	accepted(t, claim, err)
	if len(claim.Jobs) != 1 {
		t.Fatal(claim)
	}
	request := &v1.PrepareResendCommand{Header: ledgerHeader("capture-resend"), OperationId: a.OperationId, PreviousSendRef: before.Execution.Send.Ref, Claim: claim.Jobs[0]}
	decision, err := f.h.Ledger.PrepareResend(f.ctx, f.caller, request)
	accepted(t, decision, err)
	captureObject(t, samples, "resend-command", request, nil)
	x, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "resend-prepared-execution", x, err)
	if x.Send.SendSeq != 2 || !proto.Equal(x.Attempt.Ref.Name, before.Execution.Attempt.Ref.Name) || !proto.Equal(x.CallDescriptor, before.Execution.CallDescriptor) {
		t.Fatal("resend changed attempt or descriptor")
	}
	second := resendStart(t, f, a, first, x, claim.Jobs[0])
	r, err = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, err)
	captureObject(t, samples, "resend-start-command", second, nil)
	replay, err := f.h.Ledger.PrepareResend(f.ctx, f.caller, request)
	if err != nil || !proto.Equal(replay, decision) {
		t.Fatalf("resend replay changed decision %v %v", replay, err)
	}
	r, err = f.h.Egress.Invoke(f.ctx, actor, second)
	accepted(t, r, err)
	current, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	captureObject(t, samples, "resend-settled-operation", current, err)
	if current.Effect.Outcome != "APPLIED" || current.Lifecycle != "SETTLED" || len(current.Execution.PreviousSends) != 1 {
		t.Fatal(current)
	}
	old, err := f.h.Ledger.QueryOperationVersion(f.ctx, f.caller, before.Ref)
	captureObject(t, samples, "resend-historical-operation", old, err)
	if !proto.Equal(old, before) {
		t.Fatal("immutable original operation changed")
	}
	oldSend, err := f.h.Ledger.QuerySend(f.ctx, f.caller, before.Execution.Send.Ref)
	captureObject(t, samples, "resend-original-send", oldSend, err)
	if !proto.Equal(oldSend, before.Execution.Send) {
		t.Fatal("immutable original send changed")
	}
	projection, err := f.h.Ledger.QuerySendExecution(f.ctx, f.caller, a.OperationId, oldSend.Ref)
	captureObject(t, samples, "resend-original-send-projection", projection, err)
	if !proto.Equal(projection.Send.Ref.Name, oldSend.Ref.Name) || projection.Send.SendSeq != 1 {
		t.Fatal("original send query returned second send")
	}
	attempt, err := f.h.Ledger.QueryAttempt(f.ctx, f.caller, before.Execution.Attempt.Ref)
	captureObject(t, samples, "resend-original-attempt", attempt, err)
	effect, err := f.h.Ledger.QueryEffect(f.ctx, f.caller, before.Effect.Ref)
	captureObject(t, samples, "resend-historical-effect", effect, err)
	if effect.Outcome != "UNKNOWN" {
		t.Fatal("old unknown replaced by current applied")
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	captureObject(t, samples, "resend-budget-two-holds", budget, err)
	if budget.Reserved != 60 {
		t.Fatal("missing independent unknown fee holds")
	}
	for i, send := range []*v1.PhysicalSend{oldSend, current.Execution.Send} {
		source, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, send.Ref)
		name := "resend-first-billing-source"
		if i == 1 {
			name = "resend-second-billing-source"
		}
		captureObject(t, samples, name, source, err)
		if source.Amount != nil {
			t.Fatal("unknown fee represented as zero")
		}
	}
	assertResendTrace(t, f, request, decision)
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: a.OperationId})
	captureObject(t, samples, "resend-trace-operation-view", view, err)
	sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, source := range sources {
		event := source.Command.Event
		if seen[event.Producer] {
			continue
		}
		if source.Receipt == nil {
			t.Fatal("recovered source lacks acknowledgement")
		}
		actual, err := f.h.Trace.QueryEvent(f.ctx, f.caller, event.Ref)
		if err != nil || !proto.Equal(actual, event) {
			t.Fatal("source differs from accepted event", err)
		}
		captureObject(t, samples, "trace-producer-"+event.Producer, source, nil)
		seen[event.Producer] = true
	}
	for _, producer := range []string{"tasks", "sessions", "grants", "budget", "ledger", "content"} {
		if !seen[producer] {
			t.Fatalf("missing actual owner producer %s", producer)
		}
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 || requests[0].ExternalKey != requests[1].ExternalKey || requests[0].Send == requests[1].Send {
		t.Fatalf("resend effects/identity: %v %v", requests, effects)
	}
}

func captureTraceHandoff(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	f := newFixture(t, 100, 80, false)
	sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	var original *v1.TraceSourceRecord
	for _, source := range sources {
		if source.Command.Event.Producer == "tasks" && source.Command.Event.SourceSeq == 2 {
			original = source
			break
		}
	}
	if original == nil || original.Receipt != nil {
		t.Fatal("actual pending source missing")
	}
	captureObject(t, samples, "trace-source-pending", original, nil)
	command := original.Command
	actor := &v1.Caller{UserId: "u", IssuerId: command.Header.Identity.IssuerId}
	first, err := f.h.Trace.Accept(f.ctx, actor, command)
	accepted(t, first, err)
	captureObject(t, samples, "trace-accept-original-command", command, nil)
	replay, err := f.h.Trace.Accept(f.ctx, actor, command)
	if err != nil || !proto.Equal(replay, first) {
		t.Fatal("trace replay changed original receipt", err)
	}
	if err = f.h.Trace.Index(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	query := &v1.TraceQuery{TaskId: f.task.Name, ProcessingPurpose: "DIAGNOSTIC"}
	gap, err := f.h.Trace.Query(f.ctx, f.caller, query)
	captureObject(t, samples, "trace-view-gap", gap, err)
	found := false
	for _, progress := range gap.Sources {
		if progress.SourceStreamId == "d/tasks" && progress.AcceptedContiguous == 0 && len(progress.Gaps) != 0 && progress.Gaps[0].First == 1 && progress.PendingReceipts != 0 {
			found = true
		}
	}
	if !found || gap.Complete {
		t.Fatal("out-of-order source hidden by completeness")
	}
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	complete, err := f.h.Trace.Query(f.ctx, f.caller, query)
	captureObject(t, samples, "trace-view-complete", complete, err)
	if !complete.Complete || complete.PendingReceipts != 0 || complete.Backlog != 0 || complete.IndexBacklog != 0 {
		t.Fatal("source handoff/index not caught up")
	}
	for _, progress := range complete.Sources {
		query.Cutoff = append(query.Cutoff, &v1.TraceCutoff{SourceStreamId: progress.SourceStreamId, SourceSeq: progress.Cutoff})
	}
	fixed, err := f.h.Trace.Query(f.ctx, f.caller, query)
	captureObject(t, samples, "trace-fixed-cutoff-view", fixed, err)
	captureObject(t, samples, "trace-cutoff-query", query, nil)
	if !fixed.Complete {
		t.Fatal("saved cutoff incomplete")
	}
	sources, err = f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	found = false
	for _, source := range sources {
		if proto.Equal(source.Command.Header.Identity, command.Header.Identity) {
			if !proto.Equal(source.Receipt, first) || !proto.Equal(source.Command, command) {
				t.Fatal("source ACK changed original event/receipt")
			}
			captureObject(t, samples, "trace-source-acknowledged", source, nil)
			found = true
		}
	}
	if !found {
		t.Fatal("source disappeared after acknowledgement")
	}
	data, err := protojson.Marshal(complete)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "create a record") {
		t.Fatal("trace copied goal body")
	}
}
