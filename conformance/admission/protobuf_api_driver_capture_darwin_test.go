//go:build darwin && cgo

package admission_test

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G1、G3、G4、G5、G8、G10、G11
func TestCaptureDefaultModelAPIDriverPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureDefaultModelAPIDriver(t, &samples)
	requireCapturedTypes(t, samples, []string{"lerna.v1.ReasonerDriver", "lerna.v1.ModelCall", "lerna.v1.ProposalRequest", "lerna.v1.ProposalOutcome", "lerna.v1.Proposal", "lerna.v1.ApiDescriptor", "lerna.v1.Result"})
	roundTripClosingSamples(t, samples)
}
func captureDefaultModelAPIDriver(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, "default-model-api-"+name, m, e)
	}
	secret := "synthetic-production-api-driver-secret"
	var mu sync.Mutex
	records := map[string][]byte{}
	bills := map[string][]byte{}
	assertTarget := func() {
		t.Helper()
		mu.Lock()
		defer mu.Unlock()
		if len(records) != 1 || len(bills) != 1 {
			t.Fatalf("actual target effects/bills: %d/%d", len(records), len(bills))
		}
	}

	target := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer "+secret {
			t.Error("invalid native API request")
		}
		body, e := io.ReadAll(r.Body)
		if e != nil || string(body) != `{"quantity":1,"value":"hello"}` {
			t.Errorf("canonical body: %q %v", body, e)
		}
		bill := apiQueryBoundaryBill(r, 7)
		billBody, e := json.Marshal(map[string]any{"billing": bill})
		if e != nil {
			t.Error(e)
			return
		}
		mu.Lock()
		records[r.Header.Get("Idempotency-Key")] = append([]byte(nil), body...)
		bills[r.Header.Get("Lerna-Send-Id")] = billBody
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "account": r.Header.Get("Lerna-Account"), "origin": "http://" + r.Host, "applied": true, "terminal": true, "billing": bill})
	})
	x := newProductionAPIDriver(t, target, secret)
	f := x.f
	driver := x.invoke(t)
	if driver.State != "COMPLETED" || proto.Equal(driver.RequestRef, x.snapshot.RequestRef) {
		t.Fatalf("driver: %v", driver)
	}
	if x.provider.Calls() != 2 || len(x.provider.Bills()) != 2 || f.calls.Load() != 1 {
		t.Fatalf("actual MODEL/API calls: %d/%d", x.provider.Calls(), f.calls.Load())
	}
	assertTarget()
	again := x.invoke(t)
	if !proto.Equal(driver, again) {
		t.Fatal("restart changed completed driver")
	}
	assertTarget()
	x.reopen(t)
	assertTarget()
	add("driver", driver, nil)
	add("api-descriptor", x.descriptor, nil)
	add("api-binding", x.descriptor.Binding, nil)
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || len(result.OperationRefs) != 3 {
		t.Fatalf("result: %v %v", result, e)
	}
	add("result", result, e)
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || len(planning.AdmissionRefs) != 3 || planning.Snapshot.PlanningGeneration != x.snapshot.PlanningGeneration+1 {
		t.Fatalf("planning: %v %v", planning, e)
	}
	add("planning", planning, e)
	for i, ref := range []*v1.Ref{x.snapshot.RequestRef, driver.RequestRef} {
		request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, ref)
		if e != nil || len(request.ModelOperationRefs) != 1 || len(request.OutcomeRefs) != 1 {
			t.Fatalf("request: %v %v", request, e)
		}
		prefix := fmt.Sprintf("request-%d-", i)
		add(prefix+"request", request, e)
		snapshot, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, request.SnapshotRef)
		add(prefix+"snapshot", snapshot, e)
		outcome, e := f.h.Tasks.QueryProposalOutcome(f.ctx, f.caller, request.OutcomeRefs[0])
		add(prefix+"outcome", outcome, e)
		full, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, outcome.ProposalRef)
		add(prefix+"proposal-full", full, e)
		audit, e := f.h.Tasks.QueryProposal(f.ctx, f.caller, outcome.ProposalRef)
		add(prefix+"proposal-audit", audit, e)
		body, e := f.h.Content.Read(f.ctx, f.caller, full.BodyContentRef)
		add(prefix+"proposal-body", body, e)
		if (i == 0 && full.Kind != "ACTION") || (i == 1 && full.Kind != "COMPLETE") {
			t.Fatal("actual model proposal branch drift")
		}
		assertDriverReceivedSnapshot(t, &rejectionDriver{f: f, provider: x.provider}, request)
		call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, ref, 0)
		if e != nil || call == nil {
			t.Fatalf("missing original model position: %v %v", call, e)
		}
		add(prefix+"model-call", call, e)
		extra, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, ref, 1)
		if e != nil || extra != nil {
			t.Fatalf("extra model position: %v %v", extra, e)
		}
	}
	for i, ref := range planning.AdmissionRefs {
		a, e := f.h.Tasks.QueryAdmission(f.ctx, f.caller, ref)
		if e != nil {
			t.Fatal(e)
		}
		op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
		if e != nil || op.Lifecycle != "SETTLED" || op.Effect.Outcome != "APPLIED" {
			t.Fatalf("operation: %v %v", op, e)
		}
		source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
		if e != nil || source.Status != "SETTLED" || source.Amount == nil || *source.Amount != 7 {
			t.Fatalf("source: %v %v", source, e)
		}
		prefix := fmt.Sprintf("operation-%d-", i)
		add(prefix+"admission", a, nil)
		add(prefix+"operation", op, nil)
		add(prefix+"send", op.Execution.Send, nil)
		add(prefix+"call-descriptor", op.Execution.CallDescriptor, nil)
		add(prefix+"billing-source", source, nil)
		raw, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, op.Execution.Send.ObservationRef)
		add(prefix+"raw-observation", raw, e)
		if proto.Equal(a.CapabilityRef, x.targetCapability) {
			assertAPIExecutionTrace(t, f, op, 0)
			mu.Lock()
			record := append([]byte(nil), records[op.Execution.Attempt.ExternalKey]...)
			billBody := append([]byte(nil), bills[op.Execution.Send.Ref.Name.LocalId]...)
			mu.Unlock()
			if string(record) != `{"quantity":1,"value":"hello"}` || len(billBody) == 0 {
				t.Fatal("target record or bill not bound to original API attempt/send")
			}
		}
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	if e != nil || budget.Settled != 21 || budget.Reserved != 0 {
		t.Fatalf("budget: %v %v", budget, e)
	}
	add("budget", budget, e)
	assertAPITraceExcludes(t, f, secret, x.keychain.Path(), x.descriptor.Binding.Origin)
	if x.provider.Calls() != 2 || f.calls.Load() != 1 {
		t.Fatal("restart resent")
	}
	assertTarget()
	mu.Lock()
	defer mu.Unlock()
	t.Logf("actual MODEL=%d POST=%d GET=0 effects=%d model_bills=%d target_bills=%d bills=%d settled=%d reserved=%d result=%s", x.provider.Calls(), f.calls.Load(), len(records), len(x.provider.Bills()), len(bills), len(x.provider.Bills())+len(bills), budget.Settled, budget.Reserved, result.Outcome)
}
