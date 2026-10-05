// Package scripted 是脚本化推理（规格"辅助切面二"）：实现推理的公共接口，按脚本返回确定的提议。
// 它同样经过准入和出口闸门，不能直接调用执行适配器。
package scripted

import (
	"context"
	"errors"
	"sort"
	"sync"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
)

// Policy 根据快照给出提议。必须只依赖快照，不依赖调用次数：崩溃后重新运行要得到同样的提议。
type Policy func(ctx context.Context, snap *lernav1.ContextSnapshot, models ports.ModelCaller) (*lernav1.Proposal, error)

// Reasoner 是脚本化推理。
type Reasoner struct {
	Policy Policy
	mu     sync.Mutex
	calls  int
}

var _ ports.Reasoner = (*Reasoner)(nil)

// New 返回使用 policy 的脚本化推理；policy 为空时使用 Default。
func New(policy Policy) *Reasoner {
	if policy == nil {
		policy = Default
	}
	return &Reasoner{Policy: policy}
}

// Describe 返回实现版本。
func (r *Reasoner) Describe() ports.ReasonerInfo {
	return ports.ReasonerInfo{Name: "scripted", Version: "m1"}
}

// Propose 按脚本给出提议。
func (r *Reasoner) Propose(ctx context.Context, _ *lernav1.ProposalRequest, snap *lernav1.ContextSnapshot, models ports.ModelCaller) (*lernav1.Proposal, error) {
	r.mu.Lock()
	r.calls++
	r.mu.Unlock()
	return r.Policy(ctx, snap, models)
}

// Calls 返回 Propose 被调用的次数。
func (r *Reasoner) Calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.calls
}

// ErrNothingToDo 表示脚本在当前快照下没有提议。
var ErrNothingToDo = errors.New("scripted: nothing to propose for this snapshot")

// Default 按条件逐条推进：对每条"动作已生效"条件，先提议一次对应的调用；
// 全部条件都有已生效的动作作为证据时，提议完成。
func Default(_ context.Context, snap *lernav1.ContextSnapshot, _ ports.ModelCaller) (*lernav1.Proposal, error) {
	var judgements []*lernav1.RequirementJudgement
	for _, r := range snap.GetRequirements() {
		rule := r.GetRule()
		if rule.GetKind() != lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_OPERATION_APPLIED {
			return nil, ErrNothingToDo
		}
		item := find(snap, rule.GetCapabilityId(), rule.GetParams())
		switch {
		case item == nil:
			return Action(rule.GetCapabilityId(), rule.GetParams()), nil
		case item.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED:
			judgements = append(judgements, &lernav1.RequirementJudgement{
				RequirementId: r.GetRequirementId(), Verdict: lernav1.Verdict_VERDICT_SATISFIED, EvidenceRefs: []string{item.GetOperationId()},
			})
		case item.GetSettled() && item.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_NOT_APPLIED:
			// 确定未生效、不会迟到：可以用新的动作重新尝试。
			return Action(rule.GetCapabilityId(), rule.GetParams()), nil
		default:
			return nil, ErrNothingToDo
		}
	}
	return &lernav1.Proposal{
		Kind: lernav1.ProposalKind_PROPOSAL_KIND_COMPLETION,
		Body: &lernav1.Proposal_Completion{Completion: &lernav1.CompletionBody{Judgements: judgements, Answer: "done"}},
	}, nil
}

// Action 构造一步行动提议。
func Action(capability string, args map[string]string) *lernav1.Proposal {
	return &lernav1.Proposal{
		Kind: lernav1.ProposalKind_PROPOSAL_KIND_ACTION,
		Body: &lernav1.Proposal_Action{Action: &lernav1.ActionBody{Steps: []*lernav1.PlanStep{{
			StepId: "s1", CapabilityId: capability, CapabilityVersion: "1", Arguments: args,
		}}}},
	}
}

// find 返回快照中与能力和参数匹配的最近一项进展；确定未生效的较早动作让位于之后的新动作。
func find(snap *lernav1.ContextSnapshot, capability string, params map[string]string) *lernav1.ProgressItem {
	var match []*lernav1.ProgressItem
	for _, p := range snap.GetProgress() {
		if p.GetCapabilityId() != capability {
			continue
		}
		ok := true
		for k, v := range params {
			if p.GetArguments()[k] != v {
				ok = false
			}
		}
		if ok {
			match = append(match, p)
		}
	}
	if len(match) == 0 {
		return nil
	}
	sort.SliceStable(match, func(i, j int) bool { return rank(match[i]) > rank(match[j]) })
	return match[0]
}

func rank(p *lernav1.ProgressItem) int {
	switch {
	case p.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED:
		return 3
	case !p.GetSettled():
		return 2
	default:
		return 1
	}
}

// ActOnly 只提议条件需要的动作，从不提议完成：用于让任务停在动作之后。
func ActOnly(ctx context.Context, snap *lernav1.ContextSnapshot, models ports.ModelCaller) (*lernav1.Proposal, error) {
	p, err := Default(ctx, snap, models)
	if err != nil || p.GetKind() == lernav1.ProposalKind_PROPOSAL_KIND_COMPLETION {
		return nil, ErrNothingToDo
	}
	return p, nil
}
