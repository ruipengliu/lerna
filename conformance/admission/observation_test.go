package admission_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G4、G5、R7
func TestPhysicalResponsePersistsRawEvidenceBeforeInterpretation(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := f.h.Content.QueryObservation(f.ctx, f.caller, x.Send.ObservationRef)
	if e != nil || raw == nil || raw.Source != "TRUSTED_IO" || raw.StatusCode != 200 || raw.BodyRef == nil {
		t.Fatalf("raw evidence %v %v", raw, e)
	}
	body, e := f.h.Content.Read(f.ctx, f.caller, raw.BodyRef)
	if e != nil || body == nil {
		t.Fatalf("body %v %v", body, e)
	}
	evidence, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, raw.Ref)
	if e != nil || evidence == nil {
		t.Fatalf("ledger evidence %v %v", evidence, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "UNKNOWN" || len(op.Effect.EvidenceRefs) != 1 || f.calls.Load() != 1 {
		t.Fatalf("200 alone is not proof: %v %v", op, e)
	}
}

// 规则：G4、R7
func TestObservationCreatesIndependentUsageAndTraceReceipts(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	x, e := f.h.Ledger.QueryExecution(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	reports, e := f.h.Ledger.QueryReports(f.ctx, f.caller, x.Send.ObservationRef)
	if e != nil || reports == nil || reports.UsageReceipt == nil || reports.TraceReceipt == nil {
		t.Fatalf("reports %v %v", reports, e)
	}
	usage, e := f.h.Budget.QueryUsage(f.ctx, f.caller, reports.UsageReceipt.ResultRef)
	if e != nil || usage == nil || usage.Settlement != "PENDING" || usage.PhysicalSends != 1 {
		t.Fatalf("usage %v %v", usage, e)
	}
	event, e := f.h.Trace.QueryEvent(f.ctx, f.caller, reports.TraceReceipt.ResultRef)
	if e != nil || event == nil || event.EventType != "PHYSICAL_OBSERVATION" || event.BodyRef == nil {
		t.Fatalf("trace %v %v", event, e)
	}
}

// 规则：G1、G5、R3
func TestReviewedSimulatorEvidenceSettlesAndPreservesUnknownHistory(t *testing.T) {
	target := simulator.New("idempotent")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Lifecycle != "SETTLED" || op.Dispatch != "SEALED" || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" {
		t.Fatalf("effect %v %v", op, e)
	}
	original, e := f.h.Ledger.QueryEffect(f.ctx, f.caller, &v1.Ref{Name: op.EffectRef.Name, Revision: 2, SchemaId: "lerna.v1.Effect"})
	if e != nil || original.Outcome != "UNKNOWN" || original.LateEffect != "MAY_OCCUR" {
		t.Fatalf("rewritten history %v %v", original, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("target %v %v", requests, effects)
	}
	if e = f.h.Ledger.ProcessReports(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	requests, effects = target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("report retry performed IO")
	}
}

// 规则：G1、G5、R3
func TestProtocolEvidenceDistinguishesTerminalAndLateEffects(t *testing.T) {
	for _, tc := range []struct {
		behavior, outcome, late, lifecycle string
		effects                            int
	}{
		{"reject", "NOT_APPLIED", "RULED_OUT", "SETTLED", 0},
		{"applied-not-terminal", "APPLIED", "MAY_OCCUR", "ACTIVE", 1},
		{"empty-success", "UNKNOWN", "MAY_OCCUR", "ACTIVE", 1},
		{"drop-after-apply", "UNKNOWN", "MAY_OCCUR", "ACTIVE", 1},
		{"malformed", "UNKNOWN", "MAY_OCCUR", "ACTIVE", 1},
		{"wrong-key", "UNKNOWN", "MAY_OCCUR", "ACTIVE", 1},
		{"redirect", "UNKNOWN", "MAY_OCCUR", "ACTIVE", 1},
		{"contradictory", "UNKNOWN", "MAY_OCCUR", "ACTIVE", 1},
	} {
		t.Run(tc.behavior, func(t *testing.T) {
			target := simulator.New("idempotent")
			target.SetBehavior(tc.behavior)
			f := newFixtureWithTarget(t, 100, 80, false, target)
			a, c := prepareStart(t, f)
			r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
			accepted(t, r, e)
			op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
			if e != nil || op.Effect.Outcome != tc.outcome || op.Effect.LateEffect != tc.late || op.Lifecycle != tc.lifecycle {
				t.Fatalf("effect %v %v", op, e)
			}
			if op.Effect.EvidenceConflict != (tc.behavior == "contradictory") {
				t.Fatalf("conflict flag %v", op.Effect)
			}
			requests, effects := target.Snapshot()
			if len(requests) != 1 || len(effects) != tc.effects {
				t.Fatalf("target %v %v", requests, effects)
			}
		})
	}
}

// 规则：G4、G5、G7
func TestGrantExitClosureRejectsUnauthenticatedCallerBeforeMutation(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	a, c := prepareStart(t, f)
	h := ledgerHeader("close")
	h.Identity.IssuerId = "host"
	_, e := f.h.Egress.CloseForGrantRevocation(f.ctx, f.caller, &v1.CloseGrantExitCommand{Header: h, CredentialRef: c.CredentialRef, Binding: c.Binding})
	if e == nil {
		t.Fatal("untrusted closure accepted")
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Dispatch != "OPEN" || f.calls.Load() != 0 {
		t.Fatalf("closure mutated operation %v %v", op, e)
	}
}

// 规则：G1、G3、G11、R7
func TestLateRawEvidenceSurvivesExpiredProgressLease(t *testing.T) {
	oracle := simulator.New("idempotent")
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(700 * time.Millisecond)
		oracle.ServeHTTP(w, r)
	})
	f := newFixtureWithTarget(t, 100, 80, false, slow)
	a, c := prepareStartWithLease(t, f, 500)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	if time.Now().UnixMilli() <= c.Claim.LeaseUntilUnixMs {
		t.Fatal("lease did not expire")
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "APPLIED" || op.Lifecycle != "SETTLED" {
		t.Fatalf("lost late evidence %v %v", op, e)
	}
	r, e = f.h.Ledger.Prepare(f.ctx, f.caller, &v1.PrepareExecutionCommand{Header: ledgerHeader("stale-progress"), OperationId: a.OperationId, ProcessInstance: c.Claim.ProcessInstance, Claim: c.Claim})
	if e != nil || r.GetError().GetCode() != "STALE_CLAIM" {
		t.Fatalf("stale advancement %v %v", r, e)
	}
	requests, effects := oracle.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("late evidence caused new send")
	}
}

// 规则：G4、G5、G7
func TestSeparateHarnessClosureWaitsForOriginalPhysicalUse(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	oracle := simulator.New("idempotent")
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { close(entered); <-release; oracle.ServeHTTP(w, r) })
	f := newFixtureWithTarget(t, 100, 80, false, slow)
	_, c := prepareStart(t, f)
	done := make(chan error, 1)
	go func() { _, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c); done <- e }()
	<-entered
	second, e := assembly.Open(f.path, "u", "d")
	if e != nil {
		close(release)
		t.Fatal(e)
	}
	defer second.Close()
	closed := make(chan error, 1)
	go func() {
		h := ledgerHeader("closure")
		h.Identity.IssuerId = "grants-revocation"
		_, e := second.Egress.CloseForGrantRevocation(f.ctx, &v1.Caller{UserId: "u", IssuerId: "grants-revocation"}, &v1.CloseGrantExitCommand{Header: h, CredentialRef: c.CredentialRef, Binding: c.Binding})
		closed <- e
	}()
	select {
	case e := <-closed:
		close(release)
		<-done
		t.Fatalf("other instance passed the critical section before original use exited: %v", e)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if e = <-done; e != nil {
		t.Fatal(e)
	}
	// 08 会接入真实撤销源；这里的拒绝仍必须排在已经进入的实际使用之后。
	if e = <-closed; e == nil {
		t.Fatal("missing source authority accepted")
	}
	requests, effects := oracle.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatalf("target %v %v", requests, effects)
	}
}

// 规则：G3、G9、R7
func TestEvidenceQueriesRequireExactImmutableReferences(t *testing.T) {
	f := newFixtureWithTarget(t, 100, 80, false, simulator.New("idempotent"))
	a, c := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, c)
	accepted(t, r, e)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
	if e != nil {
		t.Fatal(e)
	}
	reports, e := f.h.Ledger.QueryReports(f.ctx, f.caller, raw.Ref)
	if e != nil {
		t.Fatal(e)
	}
	forged := func(r *v1.Ref) *v1.Ref {
		r = proto.Clone(r).(*v1.Ref)
		digest := "forged"
		r.Digest = &digest
		return r
	}
	tests := map[string]func() error{
		"effect": func() error { _, e := f.h.Ledger.QueryEffect(f.ctx, f.caller, forged(op.EffectRef)); return e },
		"attempt": func() error {
			_, e := f.h.Ledger.QueryAttempt(f.ctx, f.caller, forged(op.Execution.Attempt.Ref))
			return e
		},
		"send":      func() error { _, e := f.h.Ledger.QuerySend(f.ctx, f.caller, forged(op.Execution.Send.Ref)); return e },
		"operation": func() error { _, e := f.h.Ledger.QueryOperationVersion(f.ctx, f.caller, forged(op.Ref)); return e },
		"raw":       func() error { _, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, forged(raw.Ref)); return e },
		"reports":   func() error { _, e := f.h.Ledger.QueryReports(f.ctx, f.caller, forged(raw.Ref)); return e },
		"content":   func() error { _, e := f.h.Content.Read(f.ctx, f.caller, forged(raw.BodyRef)); return e },
		"usage": func() error {
			_, e := f.h.Budget.QueryUsage(f.ctx, f.caller, forged(reports.UsageReceipt.ResultRef))
			return e
		},
		"trace": func() error {
			_, e := f.h.Trace.QueryEvent(f.ctx, f.caller, forged(reports.TraceReceipt.ResultRef))
			return e
		},
	}
	for name, check := range tests {
		t.Run(name, func(t *testing.T) {
			if check() == nil {
				t.Fatal("forged reference accepted")
			}
		})
	}
}
