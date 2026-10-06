package admission_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type reasonerSourceExpectation struct {
	kind         string
	source, body *v1.Ref
	related      []*v1.Ref
}

// 规则：G3、G8、G9、G10、G11、R7
func TestReasonerTracePreservesGovernedProposalAndQuestionSources(t *testing.T) {
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"TRACE-PRIVATE-16","changesBasis":true},"gaps":["TRACE-PRIVATE-GAP-16"]}`
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 60000)
	outcome, e := f.h.RunDefaultReasoner(f.ctx, f.caller, run)
	if e != nil {
		t.Fatal(e)
	}
	goal, e := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if e != nil {
		t.Fatal(e)
	}
	publish := &v1.PublishProposalQuestionCommand{Header: header("trace-reasoner-question"), ProposalRef: outcome.ProposalRef, SessionId: goal.Receipt.SessionRef.Name}
	receipt, e := f.h.Tasks.PublishProposalQuestion(f.ctx, f.caller, publish)
	accepted(t, receipt, e)
	question, e := f.h.Sessions.QueryQuestion(f.ctx, f.caller, receipt.ResultRef)
	if e != nil {
		t.Fatal(e)
	}
	expected := []reasonerSourceExpectation{
		{"PROPOSAL_RECEIVED", outcome.ProposalRef, outcome.Proposal.BodyContentRef, []*v1.Ref{run.Preparation.RequestRef, outcome.Proposal.ContextSnapshotRef}},
		{"PROPOSAL_OUTCOME_ACCEPTED", outcome.Ref, outcome.Proposal.BodyContentRef, []*v1.Ref{outcome.ProposalRef, outcome.OutputRef, outcome.UsageRef}},
		{"CONTENT_PUBLISHED", outcome.Proposal.BodyContentRef, outcome.Proposal.BodyContentRef, []*v1.Ref{outcome.OutputRef}},
		{"CONTENT_PUBLISHED", question.ContentRef, question.ContentRef, []*v1.Ref{outcome.Proposal.BodyContentRef}},
		{"QUESTION_PENDING", question.Ref, question.ContentRef, []*v1.Ref{question.ContentRef}},
	}
	assertReasonerSourceRoundTrip(t, f, expected, "TRACE-PRIVATE-16", "TRACE-PRIVATE-GAP-16")
	before, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	replay, e := f.h.RunDefaultReasoner(f.ctx, f.caller, run)
	if e != nil || !proto.Equal(outcome, replay) {
		t.Fatalf("replay %v %v", replay, e)
	}
	repeated, e := f.h.Tasks.PublishProposalQuestion(f.ctx, f.caller, publish)
	accepted(t, repeated, e)
	if !proto.Equal(receipt, repeated) {
		t.Fatal("question replay changed original receipt")
	}
	if _, e = f.h.Tasks.ReadProposal(f.ctx, f.caller, outcome.ProposalRef); e != nil {
		t.Fatal(e)
	}
	after, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil || len(before) != len(after) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatalf("replay/read invented facts or resampled: %v calls=%d", e, provider.Calls())
	}
	task, e := f.h.Tasks.QueryTask(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	answered, e := f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("trace-reasoner-answer"), SessionId: question.SessionId, TaskId: f.task.Name, InputKind: "ANSWER", ContentRef: f.parameters, ExpectedInputVersion: task.InputVersion, ExpectedRequirementsVersion: task.RequirementsVersion, RequestRef: question.Ref})
	accepted(t, answered, e)
	question, e = f.h.Sessions.QueryQuestion(f.ctx, f.caller, question.Ref)
	if e != nil {
		t.Fatal(e)
	}
	assertReasonerSourceRoundTrip(t, f, []reasonerSourceExpectation{{"QUESTION_ANSWERED", question.Ref, question.ContentRef, []*v1.Ref{question.ResponseInputRef, question.ContentRef}}}, "TRACE-PRIVATE-16")
}

func assertReasonerSourceRoundTrip(t *testing.T, f *fixture, expected []reasonerSourceExpectation, forbidden ...string) {
	t.Helper()
	sources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	originals := make([]*v1.TraceSourceRecord, 0, len(expected))
	for _, want := range expected {
		var found *v1.TraceSourceRecord
		for _, source := range sources {
			event := source.Command.Event
			if event.EventType == want.kind && proto.Equal(event.SourceRecordRef, want.source) {
				if found != nil {
					t.Fatalf("duplicate source for %s %v", want.kind, want.source)
				}
				found = source
			}
		}
		if found == nil {
			t.Fatalf("missing original source %s %v", want.kind, want.source)
		}
		if !proto.Equal(found.Command.Event.BodyRef, want.body) {
			t.Fatalf("wrong governed body reference: %s %v", want.kind, found.Command.Event)
		}
		for _, ref := range want.related {
			requireModelTraceRef(t, found.Command.Event, ref)
		}
		originals = append(originals, proto.Clone(found).(*v1.TraceSourceRecord))
	}
	if e = f.h.Trace.Collect(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	acknowledged, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	for _, original := range originals {
		found := false
		for _, current := range acknowledged {
			if !proto.Equal(current.Command.Header.Identity, original.Command.Header.Identity) {
				continue
			}
			found = true
			actor := &v1.Caller{UserId: f.caller.UserId, IssuerId: original.Command.Header.Identity.IssuerId}
			receipt, e := f.h.Trace.QueryReceipt(f.ctx, actor, original.Command.Header.Identity)
			if e != nil || !proto.Equal(current.Command, original.Command) || current.Receipt == nil || !proto.Equal(current.Receipt, receipt.GetReceipt()) {
				t.Fatalf("source ACK lost original %v %v", current, e)
			}
			event, e := f.h.Trace.QueryEvent(f.ctx, f.caller, original.Command.Event.Ref)
			if e != nil || !proto.Equal(event, original.Command.Event) {
				t.Fatalf("receiver changed original source: %v %v", event, e)
			}
		}
		if !found {
			t.Fatal("source disappeared after collection")
		}
	}
	if e = f.h.Trace.Index(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	view, e := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if e != nil || !view.Complete || view.PendingReceipts != 0 {
		t.Fatalf("source coverage %v %v", view, e)
	}
	for _, original := range originals {
		found := false
		for _, event := range view.Events {
			found = found || proto.Equal(event, original.Command.Event)
		}
		if !found {
			t.Fatalf("indexed view lost original event: %v", original.Command.Event)
		}
	}
	encoded, e := protojson.Marshal(view)
	if e != nil {
		t.Fatal(e)
	}
	var cliOutput bytes.Buffer
	cli := interaction.CLI{Trace: f.h.Trace, Caller: f.caller, Domain: "d"}
	if e = cli.Run(f.ctx, []string{"trace-task", f.task.Name.LocalId}, &cliOutput); e != nil {
		t.Fatal(e)
	}
	for _, secret := range forbidden {
		if bytes.Contains(encoded, []byte(secret)) || bytes.Contains(cliOutput.Bytes(), []byte(secret)) {
			t.Fatal("trace copied governed body")
		}
		for _, source := range sources {
			b, e := protojson.Marshal(source)
			if e != nil || bytes.Contains(b, []byte(secret)) {
				t.Fatalf("source copied governed body %v", e)
			}
		}
	}
}
