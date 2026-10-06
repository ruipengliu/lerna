package admission_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G2、G3、G4、G5、G11、R3、V4
func TestLocalMetricGuidanceExplainsUnknownPausedAndTraceGapsWithoutBusinessAdvance(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	capability, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, reconciliationCommand(f, a, capability, grant))
	accepted(t, r, e)
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	r, e = f.h.Ledger.ControlReconciliation(f.ctx, f.caller, &v1.ControlReconciliationCommand{Header: ledgerHeader("guidance-pause"), OperationId: a.OperationId, ExpectedRevision: plan.Ref.Revision, Action: "PAUSE"})
	accepted(t, r, e)
	beforePlan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	beforeOperation, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil {
		t.Fatal(e)
	}
	beforeBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil {
		t.Fatal(e)
	}
	beforeSources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	metrics, e := f.h.QueryMetrics(f.ctx, f.caller)
	if e != nil {
		t.Fatal(e)
	}
	body, e := protojson.Marshal(metrics)
	if e != nil {
		t.Fatal(e)
	}
	guidance := readMetricGuidance(t, body)
	for _, required := range []string{"UNKNOWN", "operation OPERATION_ID", "reconciliation OPERATION_ID", "PAUSED", "trace-task TASK_ID", "DISABLED", "completion-duration endpoints"} {
		if !strings.Contains(strings.Join(guidance, "\n"), required) {
			t.Errorf("actual risk/blind spot lacks local investigation guidance %q: %v", required, guidance)
		}
	}
	for _, text := range guidance {
		if strings.Contains(text, a.OperationId.LocalId) || strings.Contains(text, f.task.Name.LocalId) {
			t.Fatal("guidance interpolated an original object identifier")
		}
	}
	var out bytes.Buffer
	if e = (interaction.CLI{Metrics: f.h, Caller: f.caller, Domain: "d"}).Run(f.ctx, []string{"metrics"}, &out); e != nil {
		t.Fatal(e)
	}
	cliGuidance := readMetricGuidance(t, out.Bytes())
	if strings.Join(cliGuidance, "\n") != strings.Join(guidance, "\n") {
		t.Fatal("CLI changed the public metric investigation guidance")
	}
	afterPlan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(beforePlan, afterPlan) {
		t.Fatalf("guidance changed original paused responsibility: %v %v", afterPlan, e)
	}
	afterOperation, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(beforeOperation, afterOperation) {
		t.Fatalf("guidance changed original unknown effect: %v %v", afterOperation, e)
	}
	afterBudget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, nil)
	if e != nil || !proto.Equal(beforeBudget, afterBudget) {
		t.Fatalf("guidance changed budget: %v %v", afterBudget, e)
	}
	afterSources, e := f.h.Trace.QuerySources(f.ctx, f.caller)
	if e != nil || len(beforeSources) != len(afterSources) {
		t.Fatalf("guidance changed source population: %v", e)
	}
	for i := range beforeSources {
		if !proto.Equal(beforeSources[i], afterSources[i]) {
			t.Fatal("guidance advanced source acknowledgement")
		}
	}
	receipt, e := f.h.Ledger.QueryReceipt(f.ctx, f.caller, r.Identity)
	requests, effects := target.Snapshot()
	if e != nil || !proto.Equal(receipt.Receipt, r) || len(requests) != 1 || requests[0].Method != "POST" || len(effects) != 1 || f.calls.Load() != 1 {
		t.Fatalf("guidance changed receipt or physical target: %v %v requests=%v effects=%v", receipt, e, requests, effects)
	}
}

func readMetricGuidance(t *testing.T, body []byte) []string {
	t.Helper()
	var rendered struct {
		InvestigationGuidance []string `json:"investigationGuidance"`
	}
	if e := json.Unmarshal(body, &rendered); e != nil {
		t.Fatal(e)
	}
	if len(rendered.InvestigationGuidance) == 0 {
		t.Fatal("public local metrics expose risk counts but no investigation guidance")
	}
	return rendered.InvestigationGuidance
}

// 规则：G1、G2、G3、G5、G7、G11、R3、V4
func TestLocalMetricGuidanceKeepsNoSamplesAndUnavailableOwnersAsBlindSpots(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	metrics, e := f.h.QueryMetrics(f.ctx, f.caller)
	if e != nil || metrics.Admission.Source.Availability != "NO_SAMPLES" || metrics.Admission.DurationSumNs != nil {
		t.Fatalf("empty actual process: %v %v", metrics, e)
	}
	body, e := protojson.Marshal(metrics)
	if e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(strings.Join(readMetricGuidance(t, body), "\n"), "NO_SAMPLES") {
		t.Error("guidance failed to explain the missing original process latency")
	}
	foreign, e := f.h.QueryMetrics(f.ctx, &v1.Caller{UserId: "another-user", IssuerId: f.caller.IssuerId})
	if e == nil || foreign != nil {
		t.Fatal("guidance exposed another user's metric snapshot")
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	partial, e := f.h.QueryMetrics(f.ctx, f.caller)
	if e != nil || partial.Ledger != nil || partial.Trace != nil || len(partial.UnavailableSources) != 2 || partial.Admission.Source.Availability != "NO_SAMPLES" || partial.Admission.DurationSumNs != nil {
		t.Fatalf("actual unavailable owners became health values: %v %v", partial, e)
	}
	body, e = protojson.Marshal(partial)
	if e != nil {
		t.Fatal(e)
	}
	guidance := readMetricGuidance(t, body)
	for _, required := range []string{"NO_SAMPLES", "unavailable", "unavailableSources", "not healthy zeros"} {
		if !strings.Contains(strings.Join(guidance, "\n"), required) {
			t.Errorf("actual blind spot lacks investigation guidance %q: %v", required, guidance)
		}
	}
	if len(guidance) > 16 {
		t.Fatal("fixed local guidance became an unbounded label set")
	}
	var out bytes.Buffer
	if e = (interaction.CLI{Metrics: f.h, Caller: f.caller, Domain: "d"}).Run(f.ctx, []string{"metrics"}, &out); e != nil {
		t.Fatal(e)
	}
	if strings.Join(readMetricGuidance(t, out.Bytes()), "\n") != strings.Join(guidance, "\n") {
		t.Fatal("CLI lost owner-unavailable guidance")
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil || f.calls.Load() != 0 {
		t.Fatalf("guidance/reopen caused business work: %v calls=%d", e, f.calls.Load())
	}
}
