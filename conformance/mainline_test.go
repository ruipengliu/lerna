package conformance_test

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/adapters/interaction"
	"github.com/ruipengliu/lerna/cmd/host"
	"github.com/ruipengliu/lerna/conformance/harness"
	"github.com/ruipengliu/lerna/conformance/scripted"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
)

func resultBlob(t *testing.T, h *harness.Harness, res *lernav1.SubmitInputResult) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(taskOf(t, h, res).GetResult())
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 主线打通：目标 → 提议 → 准入 → 执行 → 效果 → 完成。持久记录和模拟 API 收到 1 次调用。
//
// 规则：G2、G4、G5、完成-4、完成-5、完成-6
func TestMainlineFromGoalToResult(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	tv := taskOf(t, h, res)
	if tv.GetPhase() != "SUCCEEDED" || tv.GetResult().GetOutcome() != lernav1.TaskOutcome_TASK_OUTCOME_SUCCEEDED {
		t.Fatalf("task must succeed: phase %s, waiting %v", tv.GetPhase(), tv.GetTask().GetWaitingOn())
	}
	if h.API.Received() != 1 {
		t.Fatalf("target received %d calls, want 1", h.API.Received())
	}
	r := tv.GetResult()
	if len(r.GetRequirementEvaluations()) != 1 {
		t.Fatalf("result must list each requirement it verified: %v", r)
	}
	ev := r.GetRequirementEvaluations()[0]
	if ev.GetVerdict() != lernav1.Verdict_VERDICT_SATISFIED || len(ev.GetEvidenceRefs()) != 1 ||
		ev.GetEvidenceRefs()[0] != tv.GetOperations()[0].GetOperationId() ||
		ev.GetSource() != lernav1.RequirementSource_REQUIREMENT_SOURCE_TEMPLATE || ev.GetRule() == nil {
		t.Fatalf("result must name the rule, source, verdict and evidence: %v", ev)
	}
	if len(tv.GetRounds()) != 1 || tv.GetRounds()[0].GetStatus() != lernav1.VerificationStatus_VERIFICATION_STATUS_PASSED {
		t.Fatalf("one passed verification round expected: %v", tv.GetRounds())
	}
	if n := h.Count(host.DomainAdjudication, `SELECT COUNT(*) FROM results`); n != 1 {
		t.Fatalf("results = %d", n)
	}
	before := resultBlob(t, h, res)
	h.Clock.Advance(3600e9)
	h.MustRun()
	if !bytes.Equal(before, resultBlob(t, h, res)) {
		t.Fatal("a fixed Result must never change")
	}
}

// liarThenDefault 先在没有任何动作时宣称完成（伪造证据），被拒绝后按缺口正常推进。
func liarThenDefault(ctx context.Context, snap *lernav1.ContextSnapshot, m ports.ModelCaller) (*lernav1.Proposal, error) {
	if len(snap.GetProgress()) == 0 && len(snap.GetGaps()) == 0 {
		return &lernav1.Proposal{
			Kind: lernav1.ProposalKind_PROPOSAL_KIND_COMPLETION,
			Body: &lernav1.Proposal_Completion{Completion: &lernav1.CompletionBody{Judgements: []*lernav1.RequirementJudgement{{
				RequirementId: "r1", Verdict: lernav1.Verdict_VERDICT_SATISFIED, EvidenceRefs: []string{"made-up"},
			}}}},
		}, nil
	}
	return scripted.Default(ctx, snap, m)
}

// 条件缺证据时不能标记成功：完成核验被拒绝，缺口交给推理，按缺口重新规划后才成功。
//
// 规则：G2、G6、完成-5
func TestCompletionClaimWithoutEvidenceIsRejected(t *testing.T) {
	h := harness.New(t)
	h.Reasoner.Policy = liarThenDefault
	h.GrantStanding("mockapi.put")
	res := h.SubmitGoal(harness.NewID(), "把 k1 设为 v1", apiPutDraft("k1", "v1"))
	h.MustRun()
	tv := taskOf(t, h, res)
	if len(tv.GetRounds()) != 2 || tv.GetRounds()[0].GetStatus() != lernav1.VerificationStatus_VERIFICATION_STATUS_REJECTED ||
		len(tv.GetRounds()[0].GetGaps()) == 0 {
		t.Fatalf("the unsupported claim must be rejected with gaps: %v", tv.GetRounds())
	}
	if tv.GetPhase() != "SUCCEEDED" || h.API.Received() != 1 {
		t.Fatalf("after replanning the task succeeds with one call: phase %s, calls %d", tv.GetPhase(), h.API.Received())
	}
}

// 命令行能查看 Result：结果、核验了哪些条件和证据。
//
// 规则：G2
func TestCLIShowsResult(t *testing.T) {
	h := harness.New(t)
	h.GrantStanding("mockapi.put")
	var out bytes.Buffer
	cli := &interaction.CLI{Core: h.Client(), User: harness.User, Issuer: harness.Issuer, Domain: host.DomainAdjudication, Out: &out,
		Settle: func(context.Context) error { return h.Run() }}
	err := cli.Run(context.Background(), []string{"goal", "--template", "api_put", "--param", "capability=mockapi.put",
		"--param", "key=k1", "--param", "value=v1", "--budget", "100000", "把 k1 设为 v1"})
	if err != nil {
		t.Fatalf("cli: %v\n%s", err, out.String())
	}
	for _, want := range []string{"已成功", "结果：成功", "核验 r1：满足"} {
		if !strings.Contains(out.String(), want) {
			t.Fatalf("CLI output lacks %q:\n%s", want, out.String())
		}
	}
}
