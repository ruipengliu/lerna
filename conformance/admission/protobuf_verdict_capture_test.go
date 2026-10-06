package admission_test

import (
	"net/http/httptest"
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"github.com/ruipengliu/lerna/conformance/simulator"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：H1、G1、G2、G3、G4、G5、G9、完成-5
func TestCaptureDefaultNegativeVerdictPublicObjectsRoundTrip(t *testing.T) {
	var samples []protobuf.Sample
	captureDefaultNegativeVerdict(t, &samples)
	requireCapturedFields(t, samples, []string{"lerna.v1.ConditionVerdict.condition_id", "lerna.v1.ConditionVerdict.conclusion", "lerna.v1.ConditionVerdict.evidence_refs", "lerna.v1.ConditionVerdict.operation_id", "lerna.v1.ConditionVerdict.gaps", "lerna.v1.Proposal.result_draft", "lerna.v1.ContextSnapshot.progress_watermarks"})
	roundTripClosingSamples(t, samples)
}

func captureDefaultNegativeVerdict(t *testing.T, samples *[]protobuf.Sample) {
	t.Helper()
	add := func(name string, m proto.Message, e error) {
		t.Helper()
		captureObject(t, samples, "negative-verdict-"+name, m, e)
	}
	target := simulator.NewBillingTarget(25)
	target.Target.SetBehavior("reject")
	f := newFixtureWithTarget(t, 100, 80, false, target)
	scopeRequirement(t, f)
	a, start := prepareStart(t, f)
	add("original-admission", a, nil)
	receipt, e := f.h.Egress.Invoke(f.ctx, &v1.Caller{UserId: "u", IssuerId: "egress"}, start)
	accepted(t, receipt, e)
	op, e := f.h.Ledger.QueryOperation(f.ctx, f.caller, a.OperationId)
	add("actual-negative-operation", op, e)
	if op.Lifecycle != "SETTLED" || op.Effect.Outcome != "NOT_APPLIED" || op.Effect.LateEffect != "RULED_OUT" || len(op.Effect.EvidenceRefs) == 0 {
		t.Fatal("actual reliable negative target evidence absent")
	}
	provider := simulator.NewModelProvider()
	captureModelAuthorityForTarget(t, samples, "negative-verdict", f, provider)
	output, e := protojson.Marshal(&v1.Proposal{Kind: "COMPLETE", Verdicts: []*v1.ConditionVerdict{{ConditionId: "created", Conclusion: "UNSATISFIED", OperationId: a.OperationId, EvidenceRefs: op.Effect.EvidenceRefs, Gaps: []string{"original target reliably rejected the required creation"}}}, BasisRefs: op.Effect.EvidenceRefs, Gaps: []string{"required record absent"}, ResultDraft: "The original creation was rejected; completion is not justified."})
	if e != nil {
		t.Fatal(e)
	}
	provider.Output = string(output)
	run := modelRunCommand(t, f, 30000)
	full := captureGovernedReasonerRun(t, samples, "negative-verdict-model", f, run)
	add("payload", full.Verdicts[0], nil)
	add("candidate", full.CompletionEvidence[0], nil)
	request, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, run.Preparation.RequestRef)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, request.SnapshotRef)
	add("original-snapshot", snapshot, e)
	watermark := false
	for _, ref := range snapshot.ProgressWatermarks {
		watermark = watermark || proto.Equal(ref, op.Ref)
	}
	if !watermark || !proto.Equal(full.Verdicts[0].OperationId, a.OperationId) || full.Verdicts[0].Conclusion != "UNSATISFIED" {
		t.Fatal("verdict or progress watermark invented original facts")
	}
	begin := &v1.BeginCompletionCommand{Header: header("capture-default-negative-completion"), TaskId: f.task.Name, ProposalRef: full.Ref}
	receipt, e = f.h.Tasks.BeginCompletion(f.ctx, f.caller, begin)
	accepted(t, receipt, e)
	add("begin-command", begin, nil)
	if e = f.h.Tasks.ProcessCompletions(f.ctx, f.caller); e != nil {
		t.Fatal(e)
	}
	planning, e := f.h.Tasks.QueryPlanning(f.ctx, f.caller, f.task.Name)
	if e != nil {
		t.Fatal(e)
	}
	v, e := f.h.Tasks.QueryVerification(f.ctx, f.caller, planning.VerificationRef)
	add("rejected-verification", v, e)
	if v.Status != "REJECTED" || v.ContinuationRequestRef == nil || v.Conditions[0].Conclusion != "UNSATISFIED" || !proto.Equal(v.ContinuationRequestRef, planning.Snapshot.RequestRef) {
		t.Fatal("actual negative COMPLETE lost original rejection/continuation")
	}
	next, e := f.h.Tasks.QueryProposalRequest(f.ctx, f.caller, v.ContinuationRequestRef)
	add("exact-continuation", next, e)
	nextSnapshot, e := f.h.Tasks.QuerySnapshot(f.ctx, f.caller, next.SnapshotRef)
	add("exact-continuation-snapshot", nextSnapshot, e)
	job, e := f.h.Durable.QueryJob(f.ctx, f.caller, next.JobRef.Name)
	add("exact-continuation-job", job, e)
	if job.State != "READY" || !proto.Equal(job.SpecificationRef, next.Ref) {
		t.Fatal("continuation replaced original owner job")
	}
	if result, e := f.h.Tasks.QueryResult(f.ctx, f.caller, f.task.Name); e != nil || result != nil {
		t.Fatal("negative draft invented Result", e)
	}
	// 真实正文先由 Content.Read 返回；其公共读取依赖失联时只保留原投影，不重建正文。
	f.h.Tasks.WithModelContent(unavailableProposalBody{ModelContent: f.h.Content, ref: full.BodyContentRef})
	defer f.h.Tasks.WithModelContent(f.h.Content)
	if blocked, e := f.h.Tasks.ReadProposal(f.ctx, f.caller, full.Ref); blocked != nil || e == nil || e.Error() != "CONTENT_UNUSABLE" {
		t.Fatal("capture bypassed unavailable original body", e)
	}
	projection, e := f.h.Tasks.QueryProposal(f.ctx, f.caller, full.Ref)
	add("unavailable-body-projection", projection, e)
	if projection.ResultDraft != "" || len(projection.Gaps) != 0 || len(projection.Verdicts[0].Gaps) != 0 {
		t.Fatal("unavailable body text escaped through metadata")
	}
	requests, effects := target.Target.Snapshot()
	budget, e := f.h.Budget.QueryBudget(f.ctx, f.caller, f.task.Name)
	add("budget", budget, e)
	if len(requests) != 1 || len(effects) != 0 || len(target.Bills()) != 1 || provider.Calls() != 1 || len(provider.Bills()) != 1 || budget.Settled != 32 || budget.Reserved != 0 {
		t.Fatal("negative producer hid original physical calls or fees")
	}
	t.Logf("actual negative COMPLETE: target requests1/effects0/bills1; MODEL calls1/bills1; original UNSAT→REJECTED→exact continuation; unavailable body refused; %d public objects", len(*samples))
}

func captureModelAuthorityForTarget(t *testing.T, samples *[]protobuf.Sample, prefix string, f *fixture, provider *simulator.ModelProvider) {
	t.Helper()
	endpoint := httptest.NewServer(provider)
	t.Cleanup(endpoint.Close)
	cap, e := f.h.Tasks.QueryCapability(f.ctx, f.caller, f.capability)
	if e != nil {
		t.Fatal(e)
	}
	cap.Ref, cap.ApprovedBy = nil, nil
	cap.Action, cap.Resource = "MODEL_INFER", endpoint.URL
	cap.AdapterRef.Name.LocalId = "model-reference-v1"
	cap.ParameterSchemaJson, cap.SchemaDigest = nil, ""
	configure := &v1.ConfigureCapabilityCommand{Header: header(prefix + "-model-cap"), Capability: cap}
	receipt, e := f.h.Tasks.ConfigureCapability(f.ctx, f.caller, configure)
	accepted(t, receipt, e)
	captureObject(t, samples, prefix+"-model-cap-command", configure, nil)
	f.capability = receipt.ResultRef
	grant, e := f.h.Grants.QueryGrant(f.ctx, f.caller, f.grant)
	if e != nil {
		t.Fatal(e)
	}
	grant.Ref, grant.Issuer, grant.Status = nil, nil, ""
	grant.ConfirmationRequired = false
	grant.UsePoolId = prefix + "-model-pool"
	grant.Permissions[0].Action, grant.Permissions[0].Resource = "MODEL_INFER", endpoint.URL
	configureGrant := &v1.ConfigureGrantCommand{Header: header(prefix + "-model-grant"), Grant: grant}
	receipt, e = f.h.Grants.Configure(f.ctx, f.caller, configureGrant)
	accepted(t, receipt, e)
	captureObject(t, samples, prefix+"-model-grant-command", configureGrant, nil)
	f.grant = receipt.ResultRef
}
