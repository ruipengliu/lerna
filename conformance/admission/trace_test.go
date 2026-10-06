package admission_test

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G6、G10、R7
func TestTraceAssociatesAuthoritativeAdmissionExecutionAndGrant(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	admission, call := prepareStart(t, f)
	receipt, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, call)
	accepted(t, receipt, err)
	if err = f.h.Ledger.ProcessInterpretations(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || !view.Complete {
		t.Fatalf("incomplete %v %v", view, err)
	}
	kinds := map[string]bool{}
	for _, e := range view.Events {
		kinds[e.EventType] = true
		if e.SourceRecordRef == nil {
			t.Fatalf("missing authority: %v", e)
		}
	}
	for _, kind := range []string{"TASK_CHANGED", "REQUIREMENTS_ACCEPTED", "PROPOSAL_RECEIVED", "ADMISSION_ACCEPTED", "GRANT_CHANGED", "GRANT_USED", "OPERATION_CHANGED", "INPUT_RECORDED", "HANDOFF_CHANGED", "START_ACCEPTED", "EFFECT_INTERPRETED"} {
		if !kinds[kind] {
			t.Errorf("missing %s", kind)
		}
	}
	byOperation, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: admission.OperationId})
	if err != nil {
		t.Fatal(err)
	}
	linked := false
	for _, e := range byOperation.Events {
		if e.EventType == "ADMISSION_ACCEPTED" && proto.Equal(e.SourceRecordRef, admission.Ref) {
			linked = true
		}
	}
	if !linked {
		t.Fatal("operation query lost original admission")
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("trace changed external calls %d %d", len(requests), len(effects))
	}
}

// 规则：G3、G6、G7、R7、准入-1
func TestTraceRejectedAdmissionKeepsReasonAndOriginalDecision(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	proposal := f.propose(t, nil)
	c := &v1.AdmitCommand{Header: header("trace-first-admit"), TaskId: f.task.Name, ProposalRef: proposal, GrantRef: f.grant}
	r, err := f.h.Tasks.Admit(f.ctx, f.caller, c)
	accepted(t, r, err)
	c.Header = header("trace-rejected-admit")
	rejected, err := f.h.Tasks.Admit(f.ctx, f.caller, c)
	if err != nil || rejected.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("expected rejection %v %v", rejected, err)
	}
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range view.Events {
		if e.EventType == "DECISION_REJECTED" && proto.Equal(e.SourceRecordRef, rejected.DecisionRef) && e.ReasonCode == "STALE_PROPOSAL" {
			return
		}
	}
	t.Fatal("rejection reason and receipt authority missing")
}

// 规则：G1、G3、G6、G11、G12、R7
func TestTraceOutOfOrderKeepsGapsAndRejectsForgedCoreProducer(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	var original *v1.AcceptTraceCommand
	for _, source := range sources {
		if source.Command.Event.Producer == "tasks" && source.Command.Event.SourceSeq == 2 {
			original = source.Command
		}
	}
	if original == nil {
		t.Fatal("missing second original source")
	}
	actor := &v1.Caller{UserId: "u", IssuerId: original.Header.Identity.IssuerId}
	first, err := f.h.Trace.Accept(f.ctx, actor, original)
	accepted(t, first, err)
	again, err := f.h.Trace.Accept(f.ctx, actor, original)
	accepted(t, again, err)
	if !proto.Equal(first, again) {
		t.Fatal("duplicate changed original receipt")
	}
	if err = f.h.Trace.Index(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, p := range view.Sources {
		if p.SourceStreamId == "d/tasks" {
			found = true
			if p.AcceptedContiguous != 0 || len(p.Gaps) == 0 || p.Gaps[0].First != 1 || p.PendingReceipts == 0 {
				t.Fatalf("gap or source pending hidden: %v", p)
			}
		}
	}
	if !found || view.Complete {
		t.Fatal("out of order claimed completeness")
	}
	conflicting := proto.Clone(original).(*v1.AcceptTraceCommand)
	conflicting.Event.EventType = "RESULT_FIXED"
	if _, err = f.h.Trace.Accept(f.ctx, actor, conflicting); err == nil || err.Error() != "IDEMPOTENCY_CONFLICT" {
		t.Fatalf("conflicting payload accepted: %v", err)
	}
	forged := proto.Clone(original).(*v1.AcceptTraceCommand)
	forged.Header.Identity.CommandId = "forged-source"
	rejected, err := f.h.Trace.Accept(f.ctx, actor, forged)
	if err != nil || rejected.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("forged core source accepted %v %v", rejected, err)
	}
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	complete, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || !complete.Complete || complete.PendingReceipts != 0 {
		t.Fatalf("gap not repaired %v %v", complete, err)
	}
	unknown, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name, Cutoff: []*v1.TraceCutoff{{SourceStreamId: "unknown", SourceSeq: 1}}})
	if err != nil || unknown.Complete {
		t.Fatalf("unknown source claimed completeness: %v %v", unknown, err)
	}
}

// 规则：G1、G3、G10、G11、R7
func TestTraceCollectionFailurePreservesUnknownCancellationAndFees(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	target.DropReceipt(true)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, c := prepareStart(t, f)
	r, err := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, err)
	goal, err := f.h.Durable.QueryReceipt(f.ctx, f.caller, header("goal").Identity)
	if err != nil {
		t.Fatal(err)
	}
	task, err := f.h.Tasks.QueryTask(f.ctx, f.caller, a.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	r, err = f.h.Sessions.SubmitInput(f.ctx, f.caller, &v1.SubmitInputCommand{Header: header("trace-cancel"), SessionId: goal.Receipt.SessionRef.Name, TaskId: a.TaskId, InputKind: "CONTROL", Control: "CANCEL", ExpectedControlGeneration: task.ControlGeneration})
	accepted(t, r, err)
	op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || op.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("unknown expected %v %v", op, err)
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if err != nil || budget.Reserved != 30 {
		t.Fatalf("fee hold missing %v %v", budget, err)
	}
	unavailable, cancel := context.WithCancel(f.ctx)
	cancel()
	if err = f.h.Trace.Recover(unavailable, f.caller); err == nil {
		t.Fatal("collection failure hidden")
	}
	pending, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || pending.Complete || pending.Backlog == 0 {
		t.Fatalf("missing collection not exposed %v %v", pending, err)
	}
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil {
		t.Fatal(err)
	}
	cancelled, unknown := false, false
	for _, e := range view.Events {
		cancelled = cancelled || e.EventType == "TASK_CANCEL_REQUESTED"
		unknown = unknown || e.EffectOutcome == "UNKNOWN" && e.LateEffect == "MAY_OCCUR"
	}
	if !cancelled || !unknown {
		t.Fatal("diagnostics lost cancellation or unknown effect")
	}
	afterOp, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil || !proto.Equal(op, afterOp) {
		t.Fatal("trace changed effect")
	}
	afterBudget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if err != nil || !proto.Equal(budget, afterBudget) || len(target.Bills()) != 1 {
		t.Fatal("trace changed bill or sent twice")
	}
}

// 规则：G8、G12、R7
func TestTraceWhitelistExcludesBodiesTargetsProviderMetadataAndTransportErrors(t *testing.T) {
	const marker = "SYNTHETIC_TRACE_SECRET_823"
	for _, transportFailure := range []bool{false, true} {
		t.Run(fmt.Sprint(transportFailure), func(t *testing.T) {
			target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if transportFailure {
					conn, _, err := w.(http.Hijacker).Hijack()
					if err != nil {
						return
					}
					defer conn.Close()
					fmt.Fprint(conn, marker+"\r\n\r\n")
					return
				}
				w.Header().Set("X-Request-ID", marker)
				fmt.Fprint(w, marker)
			})
			f := newFixtureWithTarget(t, 100, 80, false, target)
			cap, err := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
			if err != nil {
				t.Fatal(err)
			}
			cap.Ref = nil
			cap.ApprovedBy = nil
			cap.Resource += "/" + marker
			r, err := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("trace-secret-capability"), Capability: cap})
			accepted(t, r, err)
			f.capability = r.ResultRef
			grant, err := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
			if err != nil {
				t.Fatal(err)
			}
			grant.Ref = nil
			grant.Issuer = nil
			grant.Status = ""
			grant.Permissions[0].Resource = cap.Resource
			r, err = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("trace-secret-grant"), Grant: grant})
			accepted(t, r, err)
			f.grant = r.ResultRef
			a, c := prepareStart(t, f)
			r, err = f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, err)
			execution, err := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, execution.Send.ObservationRef)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(raw.Target, marker) || transportFailure && !strings.Contains(raw.TransportError, marker) || !transportFailure && raw.ProviderRequestId != marker {
				t.Fatal("fixture did not carry marker in authoritative evidence")
			}
			if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
				t.Fatal(err)
			}
			sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
			if err != nil {
				t.Fatal(err)
			}
			for _, source := range sources {
				b, err := protojson.Marshal(source)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(b, []byte(marker)) {
					t.Fatal("source queue retained prohibited field")
				}
			}
			view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
			if err != nil {
				t.Fatal(err)
			}
			for _, e := range view.Events {
				saved, err := f.h.Trace.QueryEvent(f.ctx, f.caller, e.Ref)
				if err != nil {
					t.Fatal(err)
				}
				b, err := protojson.Marshal(saved)
				if err != nil {
					t.Fatal(err)
				}
				if bytes.Contains(b, []byte(marker)) {
					t.Fatal("trace storage retained prohibited field")
				}
			}
			var output bytes.Buffer
			cli := interaction.CLI{Trace: f.h.Trace, Caller: f.caller, Domain: "d"}
			if err = cli.Run(f.ctx, []string{"trace-operation", a.OperationId.LocalId}, &output); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(output.String(), marker) || f.calls.Load() != 1 {
				t.Fatal("diagnostic output leaked metadata or replayed request")
			}
		})
	}
}

// 规则：G1、G3、G12、R7
func TestTraceCompletenessIsBoundToSourceCutoffVector(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	if err := f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	q := &v1.TraceQuery{TaskId: f.task.Name}
	first, err := f.h.Trace.Query(f.ctx, f.caller, q)
	if err != nil || !first.Complete {
		t.Fatalf("first scope %v %v", first, err)
	}
	for _, p := range first.Sources {
		q.Cutoff = append(q.Cutoff, &v1.TraceCutoff{SourceStreamId: p.SourceStreamId, SourceSeq: p.Cutoff})
	}
	f.propose(t, nil)
	held, err := f.h.Trace.Query(f.ctx, f.caller, q)
	if err != nil || !held.Complete {
		t.Fatalf("fixed cutoff changed %v %v", held, err)
	}
	if len(held.Events) != len(first.Events) {
		t.Fatal("future event entered fixed scope")
	}
	latest, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{TaskId: f.task.Name})
	if err != nil || latest.Complete || latest.Backlog == 0 {
		t.Fatal("old completeness claimed future progress")
	}
	q.Cutoff = []*v1.TraceCutoff{{SourceStreamId: "d/tasks", SourceSeq: 1000000}}
	if _, err = f.h.Trace.Query(f.ctx, f.caller, q); err == nil {
		t.Fatal("unreported future source cutoff accepted")
	}
}
