package admission_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G4、G5、G8、G9、G10、G11、R7
func TestProductionDriverCompletesManagedFileFromActualEvidence(t *testing.T) {
	runProductionFileDriver(t, false)
}

// 规则：G1、G3、G4、G5、G10、G11
func TestProductionDriverKeepsMissingFileRootUnknownAcrossRestart(t *testing.T) {
	runProductionFileDriver(t, true)
}

func runProductionFileDriver(t *testing.T, missingRoot bool) {
	binary := filepath.Join(t.TempDir(), "lerna")
	if out, e := exec.Command("go", "build", "-o", binary, "../../cmd/lerna").CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	root, e := filepath.EvalSymlinks(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if e = os.Chmod(root, 0700); e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"objects", "commits", "locks"} {
		if e = os.Mkdir(filepath.Join(root, name), 0700); e != nil {
			t.Fatal(e)
		}
	}
	f := newFixtureWithTargetAndFiles(t, 100, 80, false, nil, map[string]string{"documents": root})
	modelCapability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	modelGrant := f.grant
	fileParameters(t, f, "", []byte("create a record"))
	configureFile(t, f, "CREATE", "managed://documents/report")
	parameters := f.parameters
	capability, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	capability.Ref = nil
	capability.ApprovedBy = nil
	capability.ParameterSchemaJson = []byte(`{"type":"object","properties":{"expected_version":{"type":"string"},"data":{"type":"string"}},"required":["expected_version","data"],"additionalProperties":false}`)
	configured, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, &v1.ConfigureCapabilityCommand{Header: header("driver-action-cap"), Capability: capability})
	accepted(t, configured, e)
	f.capability = configured.ResultRef
	capability, e = f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	targetCapability, targetGrant := f.capability, f.grant
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
	defer endpoint.Close()
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
	configuration := writeReasonerCLIJSON(t, "driver.json", &v1.ConfigureReasonerDriverCommand{Header: header("driver-file-config"), TaskId: f.task.Name, Policy: policy})
	advance := writeReasonerCLIJSON(t, "advance.json", &v1.AdvanceReasonerTaskRequest{TaskId: f.task.Name, Limit: 16})
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	rootOptions := []string{"--file-root", "documents=" + root}
	if missingRoot {
		rootOptions = nil
	}
	invoke := func() *v1.ReasonerDriver {
		t.Helper()
		args := append([]string{"--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host"}, rootOptions...)
		args = append(args, "advance-task", "--json", advance)
		out, e := exec.Command(binary, args...).CombinedOutput()
		result := new(v1.ReasonerDriver)
		if e != nil || protojson.Unmarshal(out, result) != nil {
			t.Fatalf("advance: %v %s", e, out)
		}
		return result
	}
	out, e := exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host", "--file-root", "documents="+root, "configure-reasoner", "--json", configuration).CombinedOutput()
	receipt := new(v1.CommandReceipt)
	if e != nil || protojson.Unmarshal(out, receipt) != nil || receipt.Error != nil {
		t.Fatalf("configure: %v %s", e, out)
	}
	driver := invoke()
	if missingRoot {
		if driver.State != "WAITING" || driver.WaitingReason != "OPERATION_PENDING" || !proto.Equal(driver.RequestRef, snapshot.RequestRef) || driver.AdmissionRef == nil {
			t.Fatalf("missing root: %v", driver)
		}
		if provider.Calls() != 1 || len(provider.Bills()) != 1 {
			t.Fatalf("missing root model calls=%d bills=%d", provider.Calls(), len(provider.Bills()))
		}
		// 修复宿主映射不构成原发送未生效的证据；启动恢复不得重新写文件。
		rootOptions = []string{"--file-root", "documents=" + root}
		again := invoke()
		if !proto.Equal(driver, again) || provider.Calls() != 1 {
			t.Fatalf("repair resent or resampled: %v", again)
		}
		for _, directory := range []string{"objects", "commits", "locks"} {
			entries, err := os.ReadDir(filepath.Join(root, directory))
			if err != nil || len(entries) != 0 {
				t.Fatalf("unproven retry changed target %s: %v %v", directory, entries, err)
			}
		}
		f.h, e = assembly.OpenWithFiles(f.path, "u", "d", map[string]string{"documents": root})
		if e != nil {
			t.Fatal(e)
		}
		a, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, driver.AdmissionRef)
		if err != nil {
			t.Fatal(err)
		}
		op, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
		if err != nil || op.GetEffect().GetOutcome() != "UNKNOWN" || op.Execution.Send.Phase != "DISPATCH_POSSIBLE" {
			t.Fatalf("original uncertain send: %v %v", op, err)
		}
		planning, err := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
		if err != nil || planning.Snapshot.PlanningGeneration != snapshot.PlanningGeneration {
			t.Fatalf("advanced without evidence: %v %v", planning, err)
		}
		result, err := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
		if err != nil || result != nil {
			t.Fatalf("unproven completion: %v %v", result, err)
		}
		return
	}
	if driver.State != "COMPLETED" {
		t.Fatalf("production task did not complete: %v", driver)
	}
	if proto.Equal(snapshot.RequestRef, driver.RequestRef) {
		t.Fatal("actual progress did not create next request")
	}
	if provider.Calls() != 2 || len(provider.Bills()) != 2 {
		t.Fatalf("actual model counts: calls=%d bills=%d", provider.Calls(), len(provider.Bills()))
	}
	pointerBytes, e := os.ReadFile(filepath.Join(root, "commits", "report"))
	if e != nil {
		t.Fatal(e)
	}
	var pointer struct {
		ObjectName string `json:"object_name"`
		Digest     string `json:"digest"`
		Version    string `json:"version"`
	}
	if e = json.Unmarshal(pointerBytes, &pointer); e != nil {
		t.Fatal(e)
	}
	body, e := os.ReadFile(filepath.Join(root, "objects", pointer.ObjectName))
	if e != nil || string(body) != "create a record" || pointer.Version == "" || pointer.Digest == "" {
		t.Fatalf("published object: %q %v %v", body, pointer, e)
	}
	objects, e := os.ReadDir(filepath.Join(root, "objects"))
	if e != nil || len(objects) != 1 {
		t.Fatalf("object inventory: %v %v", objects, e)
	}
	again := invoke()
	if !proto.Equal(driver, again) || provider.Calls() != 2 {
		t.Fatalf("restart changed result or resampled: %v", again)
	}
	f.h, e = assembly.OpenWithFiles(f.path, "u", "d", map[string]string{"documents": root})
	if e != nil {
		t.Fatal(e)
	}
	result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name)
	if e != nil || result == nil || result.Outcome != "SUCCEEDED" || len(result.OperationRefs) != 3 {
		t.Fatalf("core result: %v %v", result, e)
	}
	first, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, snapshot.RequestRef)
	if e != nil || len(first.OutcomeRefs) != 1 || len(first.ModelOperationRefs) != 1 {
		t.Fatalf("first responsibility: %v %v", first, e)
	}
	next, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, driver.RequestRef)
	if e != nil || len(next.OutcomeRefs) != 1 || len(next.ModelOperationRefs) != 1 {
		t.Fatalf("next responsibility: %v %v", next, e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil || planning.Snapshot.PlanningGeneration != snapshot.PlanningGeneration+1 {
		t.Fatalf("nonunique continuation: %v %v", planning, e)
	}
	for _, ref := range []*v1.Ref{snapshot.RequestRef, driver.RequestRef} {
		call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, ref, 1)
		if e != nil || call != nil {
			t.Fatalf("extra position: %v %v", call, e)
		}
	}
	after, e := os.ReadFile(filepath.Join(root, "commits", "report"))
	if e != nil || !bytes.Equal(pointerBytes, after) {
		t.Fatalf("restart changed committed pointer: %v", e)
	}
	objects, e = os.ReadDir(filepath.Join(root, "objects"))
	if e != nil || len(objects) != 1 {
		t.Fatalf("restart added object: %v %v", objects, e)
	}
	if len(planning.AdmissionRefs) != 3 {
		t.Fatalf("two model admissions and one file admission: %d", len(planning.AdmissionRefs))
	}
	var admission *v1.Admission
	for _, ref := range planning.AdmissionRefs {
		candidate, err := f.h.Tasks.QueryAdmission(f.ctx, f.caller, ref)
		if err != nil {
			t.Fatal(err)
		}
		if proto.Equal(candidate.CapabilityRef, f.capability) {
			modelOperation, err := f.h.Ledger.QueryOperation(f.ctx, f.caller, candidate.OperationId)
			if err != nil {
				t.Fatal(err)
			}
			modelSource, err := f.h.Budget.QueryBillingSource(f.ctx, f.caller, modelOperation.Execution.Send.Ref)
			if err != nil || modelSource.Status != "SETTLED" || modelSource.Amount == nil || *modelSource.Amount != 7 {
				t.Fatalf("model source: %v %v", modelSource, err)
			}
		}
		if proto.Equal(candidate.CapabilityRef, targetCapability) {
			if admission != nil {
				t.Fatal("duplicate file admission")
			}
			admission = candidate
		}
	}
	if admission == nil || !proto.Equal(admission.ParametersRef, parameters) {
		t.Fatalf("original parameters: %v", admission)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, admission.OperationId)
	if e != nil || op.GetEffect().GetOutcome() != "APPLIED" || op.Lifecycle != "SETTLED" {
		t.Fatalf("file effect: %v %v", op, e)
	}
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, op.Execution.Send.Ref)
	if e != nil || source.Status != "SETTLED" || source.Amount == nil || *source.Amount != 0 {
		t.Fatalf("file source: %v %v", source, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Settled != 14 || budget.Reserved != 0 {
		t.Fatalf("separate model charges: %v %v", budget, e)
	}

}
