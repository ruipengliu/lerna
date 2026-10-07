package admission_test

import (
	"bytes"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/infra/hosting"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

func newManualProgress(t *testing.T, dependencies hosting.ManualDependencies) *hosting.Service {
	t.Helper()
	host, e := hosting.NewManual(dependencies)
	if e != nil {
		t.Fatal(e)
	}
	return host
}

// 规则：G1、G3、G4、G10、G11、R7
func TestManualProgressDeliversOriginalObservationReportsAndInterpretation(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	requests, effects := target.Target.Snapshot()
	if len(requests) != 0 || len(effects) != 0 {
		t.Fatal("fixture Open made a target request")
	}
	a, start := prepareStart(t, f)
	result := performWithoutObservation(t, f, a, start)
	actor := &v1.Caller{UserId: "u", IssuerId: "egress-io"}
	header := header("observe:" + result.Observation.Ref.Name.LocalId)
	header.Identity.IssuerId, header.Identity.TargetDomainId = actor.IssuerId, "d/content"
	r, e := f.h.Content.RegisterObservation(f.ctx, actor, &v1.RegisterObservationCommand{Header: header, Observation: result.Observation, Body: result.Body})
	accepted(t, r, e)
	original, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || original.Effect.Outcome != "UNKNOWN" {
		t.Fatalf("original report must still be pending: %v %v", original, e)
	}
	caller := &v1.Caller{UserId: "u", IssuerId: "ordinary-cli"}
	cli := interaction.CLI{Progress: f.h.Recovery, Caller: caller, Domain: "d"}
	if e = cli.Run(f.ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	if e != nil || op.Effect.Outcome != "APPLIED" || op.Effect.LateEffect != "RULED_OUT" || op.Lifecycle != "SETTLED" || !proto.Equal(op.Execution.Attempt.Ref.Name, original.Execution.Attempt.Ref.Name) {
		t.Fatalf("manual progress lost original interpretation: %v %v", op, e)
	}
	observation, e := f.h.Ledger.QueryObservation(f.ctx, f.caller, result.Observation.Ref)
	if e != nil || !proto.Equal(observation.OperationId, a.OperationId) || !proto.Equal(observation.SendRef.Name, original.Execution.Send.Ref.Name) {
		t.Fatalf("manual progress relabelled original observation: %v %v", observation, e)
	}
	reports, e := f.h.Ledger.QueryReports(f.ctx, f.caller, result.Observation.Ref)
	if e != nil || reports.UsageReceipt == nil {
		t.Fatalf("manual progress lost original reports: %v %v", reports, e)
	}
	source, e := f.h.Budget.QueryBillingSource(f.ctx, f.caller, original.Execution.Send.Ref)
	if e != nil || source.Status != "SETTLED" || source.Amount == nil || *source.Amount != 25 {
		t.Fatalf("manual progress lost original billing: %v %v", source, e)
	}
	if e = cli.Run(f.ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	again, e := f.h.Ledger.QueryReports(f.ctx, f.caller, result.Observation.Ref)
	if e != nil || !proto.Equal(again, reports) {
		t.Fatal("manual replay changed report receipts", e)
	}
	requests, effects = target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatal("manual progress repeated original business I/O")
	}
	t.Log("Open: 0 target requests; original P5: 1 request/effect/bill; ManualProgress + replay as ordinary-cli: 0 additional target requests")
}

// 规则：G1、G3、G4、G11、R6、R7
func TestProductionCLIRecoverSeparatesStartupFromManualProgress(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "lerna")
	if out, e := exec.Command("go", "build", "-o", binary, "../../cmd/lerna").CombinedOutput(); e != nil {
		t.Fatalf("build production CLI: %v %s", e, out)
	}
	target := simulator.New("queryable")
	target.SetBehavior("drop-after-apply")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	c := reconciliationCommand(f, a, cap, grant)
	c.Header.Identity.IssuerId = "local-cli"
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, &v1.Caller{UserId: "u", IssuerId: "local-cli"}, c)
	accepted(t, r, e)
	requests, effects := target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 {
		t.Fatal("original business send count")
	}
	if e = f.h.Close(); e != nil {
		t.Fatal(e)
	}
	invoke := func(args ...string) ([]byte, error) {
		return exec.Command(binary, append([]string{"--db", f.path, "--user", "u", "--domain", "d", "--issuer", "local-cli"}, args...)...).CombinedOutput()
	}
	// 只读命令也先 Open；这一步取得的查询不得归因于尚未调用的 ManualProgress。
	out, e := invoke("operation", a.OperationId.LocalId)
	op := new(v1.Operation)
	if e != nil || protojson.Unmarshal(out, op) != nil || op.Effect.Outcome != "APPLIED" {
		t.Fatalf("Open startup did not finish original query: %v %s", e, out)
	}
	requests, effects = target.Snapshot()
	if len(requests) != 2 || requests[0].Method != "POST" || requests[1].Method != "GET" || len(effects) != 1 {
		t.Fatal("Open changed original send or query count")
	}
	if out, e = invoke("recover"); e != nil {
		t.Fatalf("production manual progress: %v %s", e, out)
	}
	requests, effects = target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 {
		t.Fatal("manual progress repeated completed startup query")
	}
	f.h, e = assembly.Open(f.path, "u", "d")
	if e != nil {
		t.Fatal(e)
	}
	replayed, e := f.h.Ledger.RequestReconciliation(f.ctx, &v1.Caller{UserId: "u", IssuerId: "local-cli"}, c)
	if e != nil || !proto.Equal(replayed, r) {
		t.Fatal("Open/manual replaced original reconciliation receipt", e)
	}
	plan, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || plan.State != "COMPLETED" || plan.CheckCount != 1 || len(plan.QueryRefs) != 1 || !proto.Equal(plan.RequestedBy, c.Header.Identity) {
		t.Fatalf("Open/manual replaced original responsibility: %v %v", plan, e)
	}
	t.Log("initial Open: 0 requests; original P5: POST +1; compiled read-only CLI Open as host-recovery: GET +1; compiled recover Open then ManualProgress as local-cli: +0; original request identity remains local-cli")
}

// 规则：G1、G3、G11、R7
func TestManualProgressKeepsOriginalFutureReconciliationDueTime(t *testing.T) {
	target := simulator.New("queryable")
	target.SetBehavior("accept-and-delay")
	f := newFixtureWithTarget(t, 200, 200, false, target)
	cap, grant := configureReconciliation(t, f)
	a, start := prepareStart(t, f)
	r, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, r, e)
	target.SetQueryRetryAfter(10000)
	c := reconciliationCommand(f, a, cap, grant)
	c.Limits.InitialDelayMs, c.Limits.MaxDelayMs = 10000, 10000
	r, e = f.h.Ledger.RequestReconciliation(f.ctx, f.caller, c)
	accepted(t, r, e)
	if e = f.h.Ledger.ProcessReconciliations(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	before, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || before.State != "WAITING" || before.NextReconcileAtUnixMs < time.Now().Add(8*time.Second).UnixMilli() {
		t.Fatalf("original future responsibility: %v %v", before, e)
	}
	cli := interaction.CLI{Progress: f.h.Recovery, Caller: &v1.Caller{UserId: "u", IssuerId: "ordinary-cli"}, Domain: "d"}
	if e = cli.Run(f.ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	after, e := f.h.Ledger.QueryReconciliation(f.ctx, f.caller, a.OperationId)
	if e != nil || !proto.Equal(before, after) {
		t.Fatalf("manual progress accelerated future work: %v %v", after, e)
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 0 {
		t.Fatal("manual progress sent before original due time")
	}
}

// 规则：G1、G2、G3、G10、G11、R7、完成-6
func TestManualProgressCompletesOriginalClosingFollowupsWithoutChangingResult(t *testing.T) {
	target := simulator.NewBillingTarget(25)
	f := newFixtureWithTarget(t, 100, 80, false, target)
	a, start := prepareStart(t, f)
	late := performWithoutObservation(t, f, a, start)
	r, e := beginNonSuccessClose(t, f, "manual-close-original", "FAILED", "USER_STOPPED")
	accepted(t, r, e)
	cli := interaction.CLI{Progress: f.h.Recovery, Caller: &v1.Caller{UserId: "u", IssuerId: "local-cli"}, Domain: "d"}
	if e = cli.Run(f.ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	view, e := f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	if e != nil || view.Result == nil || view.Result.Outcome != "FAILED" || len(view.ExecutionFollowups) != 1 || len(view.SettlementFollowups) != 1 || len(view.FollowupJobs) != 2 {
		t.Fatalf("manual progress lost original unresolved closure: %v %v", view, e)
	}
	for _, job := range view.FollowupJobs {
		if job.State != "WAITING" || job.WaitingReason == "" {
			t.Fatal("manual progress fabricated completed responsibility")
		}
	}
	fixed, e := proto.Marshal(view.Result)
	if e != nil {
		t.Fatal(e)
	}
	actor := &v1.Caller{UserId: "u", IssuerId: "egress-io"}
	header := header("observe:" + late.Observation.Ref.Name.LocalId)
	header.Identity.IssuerId, header.Identity.TargetDomainId = actor.IssuerId, "d/content"
	r, e = f.h.Content.RegisterObservation(f.ctx, actor, &v1.RegisterObservationCommand{Header: header, Observation: late.Observation, Body: late.Body})
	accepted(t, r, e)
	if e = cli.Run(f.ctx, []string{"recover"}, new(bytes.Buffer)); e != nil {
		t.Fatal(e)
	}
	view, e = f.h.Tasks.QueryTaskClosingView(f.ctx, f.caller, a.TaskId)
	if e != nil || len(view.FollowupJobs) != 2 || view.Operations[0].Effect.Outcome != "APPLIED" {
		t.Fatalf("manual progress lost original late facts: %v %v", view, e)
	}
	for _, job := range view.FollowupJobs {
		if job.State != "COMPLETED" {
			t.Fatal("manual progress stranded original followup")
		}
	}
	after, e := proto.Marshal(view.Result)
	if e != nil || !bytes.Equal(after, fixed) {
		t.Fatal("manual progress rewrote fixed Result", e)
	}
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, a.TaskId)
	if e != nil || budget.Settled != 25 || budget.Reserved != 0 {
		t.Fatalf("manual progress lost original late fee: %v %v", budget, e)
	}
	requests, effects := target.Target.Snapshot()
	if len(requests) != 1 || len(effects) != 1 || len(target.Bills()) != 1 {
		t.Fatal("manual followup progress repeated original I/O")
	}
}
