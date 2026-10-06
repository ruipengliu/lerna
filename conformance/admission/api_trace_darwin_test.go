//go:build darwin && cgo

package admission_test

import (
	"bytes"
	"net/http"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G4、G5、G10、G11、R7
func TestAPIWaitTracePreservesOriginalSourceThroughIndependentHandoffs(t *testing.T) {
	const secret = "synthetic-api-source-secret-9ae6bd43"
	f := newFixtureWithTarget(t, 100, 80, false, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Lerna-Rate-Category", "RATE")
		w.Header().Set("Retry-After", "3600")
		w.Header().Set("X-Request-ID", "provider-opaque-source-canary-746dde72")
		w.WriteHeader(429)
		_, _ = w.Write([]byte("provider-body-source-canary-746dde72"))
	}))
	d := configureAPI(t, f)
	keychain := withAPIKeychain(t, f, d, []byte(secret))
	a, start := prepareStart(t, f)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress"}
	r, err := f.h.Egress.Invoke(f.ctx, actor, start)
	accepted(t, r, err)
	op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if err != nil {
		t.Fatal(err)
	}
	sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	var original *v1.TraceSourceRecord
	for _, source := range sources {
		event := source.Command.Event
		if event.EventType == "API_WAIT_RECORDED" {
			if original != nil {
				t.Fatal("duplicate wait source")
			}
			original = source
		}
	}
	if original == nil {
		t.Fatal("durable API wait has no original source")
	}
	event := original.Command.Event
	if event.OriginCommand == nil || event.OriginCommand.UserId != "u" || event.OriginCommand.IssuerId != "ledger-interpretation" || event.OriginCommand.TargetDomainId != "d/ledger" || event.OriginCommand.CommandId != "interpret:"+op.ApiWait.ObservationRef.Name.LocalId {
		t.Fatal("wait source omitted original interpretation command")
	}
	if original.Receipt != nil || !proto.Equal(event.SourceRecordRef, op.Execution.Send.Ref) || !proto.Equal(event.TaskId, a.TaskId) || !proto.Equal(event.OperationId, a.OperationId) || !proto.Equal(event.AttemptId, op.Execution.Attempt.Ref.Name) || !proto.Equal(event.SendRef, op.ApiWait.SendRef) || !proto.Equal(event.ObservationRef, op.ApiWait.ObservationRef) || event.ReasonCode != "API_429_RATE" {
		t.Fatal("wait source changed original identity")
	}
	stored, err := f.h.Ledger.QuerySend(f.ctx, f.caller, event.SourceRecordRef)
	if err != nil || !proto.Equal(stored.ApiWait, op.ApiWait) || stored.ApiWait.ReadyAtUnixMs != stored.ApiWait.ObservedAtUnixMs+3600000 {
		t.Fatal("source does not dereference original durable wait", err)
	}
	view, err := f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: a.OperationId})
	if err != nil {
		t.Fatal(err)
	}
	for _, indexed := range view.Events {
		if proto.Equal(indexed.Ref, event.Ref) {
			t.Fatal("uncollected source was indexed")
		}
	}
	if err = f.h.Trace.Collect(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	acceptedEvent, err := f.h.Trace.QueryEvent(f.ctx, f.caller, event.Ref)
	if err != nil || !proto.Equal(acceptedEvent, event) {
		t.Fatal("receiver acceptance changed original source", err)
	}
	sources, err = f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	acknowledged := false
	for _, source := range sources {
		if proto.Equal(source.Command.Event.Ref, event.Ref) {
			receipt, queryErr := f.h.Trace.QueryReceipt(f.ctx, &v1.Caller{UserId: "u", IssuerId: source.Command.Header.Identity.IssuerId}, source.Command.Header.Identity)
			if queryErr != nil || source.Receipt == nil || !proto.Equal(receipt.GetReceipt(), source.Receipt) || !proto.Equal(source.Command, original.Command) {
				t.Fatal("source acknowledgement lost original receiver receipt", queryErr)
			}
			acknowledged = true
		}
	}
	if !acknowledged {
		t.Fatal("original source disappeared")
	}
	if err = f.h.Trace.Index(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	view, err = f.h.Trace.Query(f.ctx, f.caller, &v1.TraceQuery{OperationId: a.OperationId})
	if err != nil || !view.Complete {
		t.Fatal("indexed source coverage incomplete", err)
	}
	indexed := false
	for _, item := range view.Events {
		if proto.Equal(item.Ref, event.Ref) {
			indexed = proto.Equal(item, event)
		}
	}
	if !indexed {
		t.Fatal("original wait source missing from index")
	}
	assertAPITraceExcludes(t, f, secret, keychain.Path(), d.Binding.Origin, "provider-opaque-source-canary-746dde72", "provider-body-source-canary-746dde72")
	r, err = f.h.Egress.Invoke(f.ctx, actor, start)
	accepted(t, r, err)
	if err = f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	replayed, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil || len(replayed) != len(sources) || f.calls.Load() != 1 {
		t.Fatal("replay duplicated source or target request", err)
	}
	budget, err := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if err != nil || budget.Reserved != 30 {
		t.Fatal("trace handoff changed original fee", err)
	}
}

func assertAPIExecutionTrace(t *testing.T, f *fixture, op *v1.Operation, wantWaits int) {
	t.Helper()
	if err := f.h.Trace.Recover(f.ctx, f.caller); err != nil {
		t.Fatal(err)
	}
	sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	waits, observations, interpretations, bills := 0, 0, 0, map[string]bool{}
	for _, source := range sources {
		event := source.Command.Event
		if !proto.Equal(event.OperationId, op.Ref.Name) {
			continue
		}
		acceptedEvent, err := f.h.Trace.QueryEvent(f.ctx, f.caller, event.Ref)
		if err != nil || source.Receipt == nil || !proto.Equal(event, acceptedEvent) {
			t.Fatal("API source handoff changed original fact", err)
		}
		switch event.EventType {
		case "API_WAIT_RECORDED":
			waits++
			send, err := f.h.Ledger.QuerySend(f.ctx, f.caller, event.SourceRecordRef)
			if err != nil || send == nil || send.ApiWait == nil || !proto.Equal(event.AttemptId, send.AttemptId) || !proto.Equal(event.ObservationRef, send.ApiWait.ObservationRef) || !proto.Equal(event.SendRef, send.ApiWait.SendRef) || event.ReasonCode != "API_429_"+send.ApiWait.Category {
				t.Fatal("wait source lost original send version", err)
			}
			raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, event.ObservationRef)
			if err != nil || raw == nil || raw.StatusCode != 429 || !proto.Equal(raw.OperationId, op.Ref.Name) || !proto.Equal(raw.SendRef, event.SendRef) || raw.ExternalKey != op.Execution.Attempt.ExternalKey || send.ApiWait.ObservedAtUnixMs != raw.FinishedAtUnixMs {
				t.Fatal("wait source rebound original observation", err)
			}
		case "PHYSICAL_OBSERVATION", "EFFECT_INTERPRETED":
			raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, event.ObservationRef)
			if err != nil || raw == nil || !proto.Equal(raw.OperationId, event.OperationId) || !proto.Equal(raw.AttemptId, event.AttemptId) || !proto.Equal(raw.SendRef, event.SendRef) || raw.ExternalKey != op.Execution.Attempt.ExternalKey {
				t.Fatal("physical source lost original attempt/send", err)
			}
			if event.EventType == "PHYSICAL_OBSERVATION" {
				observations++
				if !proto.Equal(event.SourceRecordRef, raw.Ref) || !proto.Equal(event.BodyRef, raw.BodyRef) {
					t.Fatal("physical source lost governed body identity")
				}
			} else {
				interpretations++
				finding, err := f.h.Ledger.QueryInterpretation(f.ctx, f.caller, event.SourceRecordRef)
				if err != nil || finding == nil || !proto.Equal(finding.ObservationRef, raw.Ref) || finding.Outcome != event.EffectOutcome || finding.LateEffect != event.LateEffect {
					t.Fatal("interpretation source changed original proof", err)
				}
			}
		case "BILLING_UNKNOWN", "BILLING_SETTLED":
			bill, err := f.h.Budget.QueryBillingSourceVersion(f.ctx, f.caller, event.SourceRecordRef)
			if err != nil || bill == nil || !proto.Equal(bill.OperationId, event.OperationId) || !proto.Equal(bill.TaskId, event.TaskId) || !proto.Equal(bill.SendRef, event.SendRef) {
				t.Fatal("fee trace lost original source/version", err)
			}
			bills[event.SendRef.Name.LocalId] = true
		}
	}
	if waits != wantWaits || observations != len(op.Effect.EvidenceRefs) || interpretations != observations || len(bills) != 1+len(op.Execution.PreviousSends) {
		t.Fatalf("API source omissions waits=%d raw=%d proof=%d bills=%d", waits, observations, interpretations, len(bills))
	}
}

func assertAPIQueryTrace(t *testing.T, f *fixture, plan *v1.Reconciliation) {
	t.Helper()
	assertReconciliationTrace(t, f, plan, "APPLIED", true)
	for _, ref := range plan.QueryRefs {
		relation, err := f.h.Ledger.QueryReconciliationQuery(f.ctx, f.caller, ref)
		if err != nil {
			t.Fatal(err)
		}
		query, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, relation.QueryOperationRef.Name)
		if err != nil || query == nil || proto.Equal(query.Ref.Name, plan.OperationId) || !proto.Equal(query.QuerySubject.OperationId, plan.OperationId) || !proto.Equal(query.QuerySubject.AttemptId, plan.OriginalAttemptRef.Name) {
			t.Fatal("query trace collapsed original and read operation", err)
		}
		assertAPIExecutionTrace(t, f, query, 0)
		raw, err := f.h.Ledger.QueryObservation(f.ctx, f.caller, relation.ObservationRef)
		if err != nil || raw == nil || !proto.Equal(raw.OperationId, query.Ref.Name) || !proto.Equal(raw.AttemptId, query.Execution.Attempt.Ref.Name) || raw.ExternalKey != query.Execution.Attempt.ExternalKey || !proto.Equal(raw.QuerySubject, query.QuerySubject) || !proto.Equal(query.ClosureWorkRef, relation.Work.Ref) {
			t.Fatal("query provenance changed original read observation", err)
		}
	}
}

func assertAPITraceExcludes(t *testing.T, f *fixture, markers ...string) {
	t.Helper()
	sources, err := f.h.Trace.QuerySources(f.ctx, f.caller)
	if err != nil {
		t.Fatal(err)
	}
	for _, source := range sources {
		encoded, err := proto.Marshal(source)
		if err != nil {
			t.Fatal(err)
		}
		for _, marker := range markers {
			if bytes.Contains(encoded, []byte(marker)) {
				t.Fatal("private API value entered trace source")
			}
		}
		if source.Receipt != nil {
			event, err := f.h.Trace.QueryEvent(f.ctx, f.caller, source.Command.Event.Ref)
			if err != nil || !proto.Equal(event, source.Command.Event) {
				t.Fatal("accepted API source differs", err)
			}
		}
	}
}
