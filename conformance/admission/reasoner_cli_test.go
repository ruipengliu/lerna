package admission_test

import (
	"bytes"
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

// 规则：G1、G3、G4、G5、G8、G10、G11、R6
func TestProductionCLIAssemblesDefaultReasonerAndReplaysOriginalRequest(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lerna")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/lerna")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	provider := simulator.NewModelProvider()
	provider.Output = `{"kind":"QUESTION","question":{"question":"Which destination?","changesBasis":true},"gaps":["destination missing"]}`
	f := modelFixtureTarget(t, provider)
	run := modelRunCommand(t, f, 60000)
	file := writeReasonerCLIJSON(t, "run.json", run)
	if e := f.h.Close(); e != nil {
		t.Fatal(e)
	}
	invoke := func(issuer, action, path string) ([]byte, error) {
		t.Helper()
		return exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "--issuer", issuer, action, "--json", path).CombinedOutput()
	}
	if out, e := invoke("local-cli", "run-reasoner", file); e == nil || !bytes.Contains(out, []byte("PERMISSION_DENIED")) || provider.Calls() != 0 {
		t.Fatalf("ordinary caller elevated: %v %s calls=%d", e, out, provider.Calls())
	}
	out, e := invoke("host", "run-reasoner", file)
	outcome := new(v1.ProposalOutcome)
	if e != nil || protojson.Unmarshal(out, outcome) != nil || outcome.Proposal.GetKind() != "QUESTION" || outcome.Proposal.GetBodyContentRef() == nil || outcome.UsageRef == nil {
		t.Fatalf("production reasoner: %v %s", e, out)
	}
	if bytes.Contains(out, []byte("Which destination?")) {
		t.Fatal("structural outcome leaked body")
	}
	again, e := invoke("host", "run-reasoner", file)
	replay := new(v1.ProposalOutcome)
	if e != nil || protojson.Unmarshal(again, replay) != nil || !proto.Equal(outcome, replay) || provider.Calls() != 1 || len(provider.Bills()) != 1 {
		t.Fatalf("new process replay: %v %s calls=%d bills=%d", e, again, provider.Calls(), len(provider.Bills()))
	}
	ref := writeReasonerCLIJSON(t, "proposal.json", outcome.ProposalRef)
	body, e := invoke("local-cli", "read-proposal", ref)
	proposal := new(v1.Proposal)
	if e != nil || protojson.Unmarshal(body, proposal) != nil || proposal.GetQuestion().GetQuestion() != "Which destination?" {
		t.Fatalf("governed display: %v %s", e, body)
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
	if e != nil || len(request.ModelOperationRefs) != 1 || len(request.OutcomeRefs) != 1 || !proto.Equal(request.OutcomeReceipt.ResultRef, outcome.Ref) {
		t.Fatalf("original request: %v %v", request, e)
	}
	call, e := f.h.Tasks.QueryModelCall(f.ctx, f.caller, run.Preparation.RequestRef, 0)
	if e != nil || call.Result == nil || !proto.Equal(call.Result.UsageRef, outcome.UsageRef) {
		t.Fatalf("original call: %v %v", call, e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, call.Result.OperationId)
	if e != nil || op.Execution == nil || op.Execution.Send.SendSeq != 1 {
		t.Fatalf("original physical send: %v %v", op, e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || budget.Settled != 7 || budget.Reserved != 0 {
		t.Fatalf("actual cost: %v %v", budget, e)
	}
}

func writeReasonerCLIJSON(t *testing.T, name string, value proto.Message) string {
	t.Helper()
	body, e := protojson.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	path := filepath.Join(t.TempDir(), name)
	if e = os.WriteFile(path, body, 0600); e != nil {
		t.Fatal(e)
	}
	return path
}

// 规则：G3、G4、G5、G10、G12、准入-3、准入-7
func TestProductionCLIReasonerPreservesOwnerAuthorityChecks(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lerna")
	build := exec.Command("go", "build", "-o", binary, "../../cmd/lerna")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build: %v %s", e, out)
	}
	for _, mode := range []string{"request", "claim", "grant", "settings", "command-identity"} {
		t.Run(mode, func(t *testing.T) {
			provider := simulator.NewModelProvider()
			f := modelFixtureTarget(t, provider)
			run := modelRunCommand(t, f, 60000)
			original := proto.Clone(run.Preparation.RequestRef).(*v1.Ref)
			switch mode {
			case "request":
				run.Preparation.RequestRef.Name.LocalId = "missing-original-request"
			case "claim":
				run.Preparation.Claim.ClaimEpoch++
			case "grant":
				run.GrantRef.Name.LocalId = "missing-original-grant"
			case "settings":
				run.Preparation.Settings.Provider = "unconfigured-provider"
			case "command-identity":
				run.Preparation.Header.Identity.IssuerId = "local-cli"
			}
			file := writeReasonerCLIJSON(t, mode+".json", run)
			if e := f.h.Close(); e != nil {
				t.Fatal(e)
			}
			out, invocationError := exec.Command(binary, "--db", f.path, "--user", "u", "--domain", "d", "--issuer", "host", "run-reasoner", "--json", file).CombinedOutput()
			expected := map[string]string{"request": "NOT_FOUND", "claim": "STALE_CLAIM", "grant": "GRANT_INVALID", "settings": "PREPARATION_UNRECOVERABLE", "command-identity": "PERMISSION_DENIED"}[mode]
			// 模型配置拒绝可以成为原请求的结构错误回报；它也不能形成提议或实际调用。
			if invocationError == nil {
				result := new(v1.ProposalOutcome)
				if e := protojson.Unmarshal(out, result); e != nil || result.ErrorCode != expected || result.Proposal != nil || !proto.Equal(result.RequestRef, original) {
					t.Fatalf("invalid binding accepted: %s %v", out, e)
				}
			} else if !bytes.Contains(out, []byte(expected)) {
				t.Fatalf("unexpected owner refusal: want %s, got %v %s", expected, invocationError, out)
			}
			if provider.Calls() != 0 || len(provider.Bills()) != 0 {
				t.Fatalf("invalid binding reached target: calls=%d bills=%d", provider.Calls(), len(provider.Bills()))
			}
			var e error
			f.h, e = assembly.Open(f.path, "u", "d")
			if e != nil {
				t.Fatal(e)
			}
			request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, original)
			if e != nil || len(request.ModelOperationRefs) != 0 {
				t.Fatalf("invalid binding admitted operation: %v %v", request, e)
			}
			planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
			if e != nil || len(planning.AdmissionRefs) != 0 || planning.Proposal != nil {
				t.Fatalf("invalid binding changed planning: %v %v", planning, e)
			}
			budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
			if e != nil || budget.Reserved != 0 || budget.Settled != 0 {
				t.Fatalf("invalid binding consumed budget: %v %v", budget, e)
			}
		})
	}
}
