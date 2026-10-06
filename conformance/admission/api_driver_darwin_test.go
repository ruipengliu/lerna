//go:build darwin && cgo

package admission_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/keys"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type productionAPIDriver struct {
	f                                                          *fixture
	provider                                                   *simulator.ModelProvider
	keychain                                                   *keys.SyntheticKeychain
	binary, advance                                            string
	snapshot                                                   *v1.ContextSnapshot
	targetCapability, targetGrant, queryCapability, queryGrant *v1.Ref
	descriptor                                                 *v1.ApiDescriptor
}

func newProductionAPIDriver(t *testing.T, target http.Handler, secret string) *productionAPIDriver {
	t.Helper()
	binary := filepath.Join(t.TempDir(), "lerna")
	if out, e := exec.Command("go", "build", "-o", binary, "../../cmd/lerna").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	f := newFixtureWithTarget(t, 200, 200, false, target)
	modelCapability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	modelGrant := f.grant
	d, queryCapability, queryGrant := configureAPIQueryable(t, f, true)
	keychain := withAPIKeychain(t, f, d, []byte(secret))
	if e = keychain.Remove(d.Binding); e != nil {
		t.Fatal(e)
	}
	if e = keychain.PutForExecutable(d.Binding, []byte(secret), binary); e != nil {
		t.Fatal(e)
	}
	capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	capability.Ref = nil
	capability.ApprovedBy = nil
	capability.ParameterSchemaJson = []byte(`{"type":"object","properties":{"value":{"type":"string"},"quantity":{"type":"integer"}},"required":["value"],"additionalProperties":false}`)
	configured, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("driver-api-action-cap"), Capability: capability})
	accepted(t, configured, e)
	f.capability = configured.ResultRef
	capability, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	targetCapability, targetGrant, parameters := f.capability, f.grant, f.parameters
	scopeRequirement(t, f)
	action := &v1.Proposal{Kind: "ACTION", Step: &v1.ActionStep{StepId: "create", CapabilityRef: targetCapability, SchemaDigest: capability.SchemaDigest, ParametersRef: parameters}, BasisRefs: []*v1.Ref{parameters}}
	provider := simulator.NewModelProvider()
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, e := io.ReadAll(r.Body)
		if e != nil {
			t.Error(e)
			http.Error(w, "read", 500)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		var view struct {
			Facts struct {
				Facts []json.RawMessage `json:"facts"`
			} `json:"facts"`
		}
		if e = json.Unmarshal(body, &view); e != nil || len(view.Facts.Facts) == 0 {
			t.Errorf("model received no actual snapshot: %v", e)
			http.Error(w, "snapshot", 500)
			return
		}
		snapshot := new(v1.ContextSnapshot)
		if e = protojson.Unmarshal(view.Facts.Facts[0], snapshot); e != nil {
			t.Error(e)
			http.Error(w, "snapshot", 500)
			return
		}
		output := action
		for _, fact := range snapshot.ProgressFacts {
			if proto.Equal(fact.CapabilityRef, targetCapability) && fact.EffectOutcome == "APPLIED" && !fact.EvidenceConflict && !fact.ExecutionReportPending {
				output = &v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "SATISFIED", OperationId: fact.OperationRef.Name, EvidenceRefs: fact.EvidenceRefs}}, BasisRefs: fact.EvidenceRefs}
				break
			}
		}
		encoded, e := protojson.Marshal(output)
		if e != nil {
			t.Error(e)
			http.Error(w, "output", 500)
			return
		}
		provider.Output = string(encoded)
		provider.ServeHTTP(w, r)
	}))
	t.Cleanup(endpoint.Close)
	capability = modelCapability
	capability.Ref = nil
	capability.ApprovedBy = nil
	capability.Action = "MODEL_INFER"
	capability.Resource = endpoint.URL
	capability.AdapterRef.Name.LocalId = "model-reference-v1"
	capability.ParameterSchemaJson = nil
	capability.SchemaDigest = ""
	configured, e = f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("driver-model-cap"), Capability: capability})
	accepted(t, configured, e)
	f.capability = configured.ResultRef
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, modelGrant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref = nil
	grant.Issuer = nil
	grant.Status = ""
	grant.UsePoolId = "driver-model"
	grant.Permissions[0].Action = "MODEL_INFER"
	grant.Permissions[0].Resource = endpoint.URL
	configured, e = f.h.Grants.Configure(f.ctx, f.caller, &v1.ConfigureGrantCommand{Header: header("driver-model-grant"), Grant: grant})
	accepted(t, configured, e)
	f.grant = configured.ResultRef
	snapshot, e := f.h.Tasks.RequestProposal(f.ctx, f.caller, &v1.RequestProposalCommand{Header: header("driver-action-request"), TaskId: f.task.Name})
	if e != nil {
		t.Fatal(e)
	}
	policy := driverPolicy(f)
	policy.Actions = []*v1.ReasonerActionAuthority{{CapabilityRef: targetCapability, GrantRef: targetGrant}}
	configuration := writeReasonerCLIJSON(t, "driver.json", &v1.ConfigureReasonerDriverCommand{Header: header("driver-api-config"), TaskId: f.task.Name, Policy: policy})
	advance := writeReasonerCLIJSON(t, "advance.json", &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 16})

	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	out, e := exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host", "--api-keychain", keychain.Path(), "configure-reasoner", "--json", configuration).CombinedOutput()
	receipt := new(v1.CommandReceipt)
	if e != nil || protojson.Unmarshal(out, receipt) != nil || receipt.Error != nil {
		t.Fatalf("configure: %v %s", e, out)
	}
	return &productionAPIDriver{f: f, provider: provider, keychain: keychain, binary: binary, advance: advance, snapshot: snapshot, targetCapability: targetCapability, targetGrant: targetGrant, queryCapability: queryCapability, queryGrant: queryGrant, descriptor: d}
}

func (x *productionAPIDriver) invoke(t *testing.T) *v1.ReasonerDriver {
	t.Helper()
	out, e := exec.Command(x.binary, "--db", x.f.path, "--user", "u", "--domain", "d", "--issuer", "host", "--api-keychain", x.keychain.Path(), "advance-task", "--json", x.advance).CombinedOutput()
	driver := new(v1.ReasonerDriver)
	if e != nil || protojson.Unmarshal(out, driver) != nil {
		t.Fatalf("advance: %v %s", e, out)
	}
	return driver
}
func (x *productionAPIDriver) reopen(t *testing.T) {
	t.Helper()
	var e error
	x.f.h, e = assembly.OpenWithOptions(x.f.path, "u", "d", assembly.Options{APIKeychainPath: x.keychain.Path()})
	if e != nil {
		t.Fatal(e)
	}
}

// 规则：G1、G2、G3、G4、G5、G8、G9、G10、G11、R7
func TestProductionAPIDriverCompletesFromActualNativeEvidence(t *testing.T) {
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
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || len(result.OperationRefs) != 3 {
		t.Fatalf("result: %v %v", result, e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || len(planning.AdmissionRefs) != 3 || planning.Snapshot.PlanningGeneration != x.snapshot.PlanningGeneration+1 {
		t.Fatalf("planning: %v %v", planning, e)
	}
	for _, ref := range []*v1.Ref{x.snapshot.RequestRef, driver.RequestRef} {
		request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, ref)
		if e != nil || len(request.ModelOperationRefs) != 1 || len(request.OutcomeRefs) != 1 {
			t.Fatalf("request: %v %v", request, e)
		}
		assertDriverReceivedSnapshot(t, &rejectionDriver{f: f, provider: x.provider}, request)
		call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, ref, 0)
		if e != nil || call == nil {
			t.Fatalf("missing original model position: %v %v", call, e)
		}
		extra, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, ref, 1)
		if e != nil || extra != nil {
			t.Fatalf("extra model position: %v %v", extra, e)
		}
	}
	for _, ref := range planning.AdmissionRefs {
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
	assertAPITraceExcludes(t, f, secret, x.keychain.Path(), x.descriptor.Binding.Origin)
	if x.provider.Calls() != 2 || f.calls.Load() != 1 {
		t.Fatal("restart resent")
	}
	assertTarget()
	mu.Lock()
	defer mu.Unlock()
	t.Logf("actual MODEL=%d POST=%d GET=0 effects=%d model_bills=%d target_bills=%d bills=%d settled=%d reserved=%d result=%s", x.provider.Calls(), f.calls.Load(), len(records), len(x.provider.Bills()), len(bills), len(x.provider.Bills())+len(bills), budget.Settled, budget.Reserved, result.Outcome)
}
