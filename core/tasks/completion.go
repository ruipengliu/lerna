package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
)

// JobVerify 是完成核验第二阶段的工作。
const JobVerify = "tasks.verify"

func verifyPurpose(taskID string, round int64) string {
	return "verify:" + taskID + ":" + strconv.FormatInt(round, 10)
}

func sealHandoffID(taskID string, round int64) string {
	return "seal:" + taskID + ":" + strconv.FormatInt(round, 10)
}

// verificationFrozen 报告是否有完成核验轮次持有目标推进冻结。
func verificationFrozen(_ *durable.Tx, t *lernav1.Task) (bool, error) {
	return t.GetFrozenRound() != 0, nil
}

func loadRound(tx *durable.Tx, user, taskID string, n int64) (*lernav1.VerificationRound, error) {
	var blob []byte
	err := tx.QueryRow(`SELECT record FROM verification_rounds WHERE user_id = ? AND task_id = ? AND round_no = ?`, user, taskID, n).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := &lernav1.VerificationRound{}
	return r, proto.Unmarshal(blob, r)
}

func saveRound(tx *durable.Tx, user string, r *lernav1.VerificationRound) error {
	blob, err := proto.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO verification_rounds (user_id, task_id, round_no, status, record) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, task_id, round_no) DO UPDATE SET status = excluded.status, record = excluded.record`,
		user, r.GetTaskId(), r.GetRoundNo(), int32(r.GetStatus()), blob)
	return err
}

func loadRounds(tx *durable.Tx, user, taskID string) ([]*lernav1.VerificationRound, error) {
	rows, err := tx.Query(`SELECT record FROM verification_rounds WHERE user_id = ? AND task_id = ? ORDER BY round_no`, user, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*lernav1.VerificationRound
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, err
		}
		r := &lernav1.VerificationRound{}
		if err := proto.Unmarshal(blob, r); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func loadResult(tx *durable.Tx, user, taskID string) (*lernav1.Result, error) {
	var blob []byte
	err := tx.QueryRow(`SELECT record FROM results WHERE user_id = ? AND task_id = ?`, user, taskID).Scan(&blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r := &lernav1.Result{}
	return r, proto.Unmarshal(blob, r)
}

func saveTaskCore(tx *durable.Tx, t *lernav1.Task) error {
	waiting, err := marshalWaiting(t.GetWaitingOn())
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE tasks SET requirements_version = ?, input_version = ?, lifecycle = ?, control = ?, progress = ?,
		waiting_on = ?, planning_generation = ?, control_generation = ?, result_ref = ?, frozen_round = ?, revision = revision + 1
		WHERE user_id = ? AND task_id = ?`,
		t.GetRequirementsVersion(), t.GetInputVersion(), int32(t.GetLifecycle()), int32(t.GetControl()), int32(t.GetProgress()),
		waiting, t.GetPlanningGeneration(), t.GetControlGeneration(), t.GetResultRef(), t.GetFrozenRound(), t.GetUserId(), t.GetTaskId())
	return err
}

// canBeginVerification 是开始核验时的检查：完成-1（完成提议绑定当前的条件集版本、输入版本和
// 控制代次，条件集已接纳）和完成-2（M1 没有子任务）；任务必须可以推进，且没有进行中的核验轮次。
func canBeginVerification(p *lernav1.Proposal, t *lernav1.Task, set *lernav1.RequirementSet) bool {
	return p.GetPlanningGeneration() == t.GetPlanningGeneration() &&
		p.GetRequirementsVersion() == t.GetRequirementsVersion() && p.GetInputVersion() == t.GetInputVersion() &&
		p.GetControlGeneration() == t.GetControlGeneration() &&
		set.GetStatus() == lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED && set.GetBoundInputVersion() == t.GetInputVersion() &&
		t.GetLifecycle() == lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN && t.GetControl() == lernav1.TaskControl_TASK_CONTROL_ACTIVE &&
		t.GetFrozenRound() == 0
}

// adjudicateCompletion 是完成门禁的第一阶段（任务编排 4.7）：检查完成提议绑定当前版本（完成-1），
// 开启核验轮次——同一事务中冻结新的目标准入、递增控制代次、固定完整的动作清单——并向执行管理
// 提交对清单中准确动作集合的封闭请求。
func (m *Module) adjudicateCompletion(ctx context.Context, c *durable.Claim, p *lernav1.Proposal) error {
	return m.Domain.Advance(ctx, c, "tasks:begin_verification", func(tx *durable.Tx) (durable.Transition, error) {
		t, err := loadTask(tx, c.User, p.GetTaskId())
		if err != nil {
			return durable.Transition{}, err
		}
		set, err := loadRequirementSet(tx, c.User, t.GetTaskId(), t.GetRequirementsVersion())
		if err != nil {
			return durable.Transition{}, err
		}
		if !canBeginVerification(p, t, set) {
			if err := setProposalStatus(tx, c.User, p.GetProposalId(), proposalStale); err != nil {
				return durable.Transition{}, err
			}
			return durable.Done(), m.maybeRequestProposal(tx, t)
		}
		rounds, err := loadRounds(tx, c.User, t.GetTaskId())
		if err != nil {
			return durable.Transition{}, err
		}
		n := int64(len(rounds) + 1)
		ops, err := loadOperationViews(tx, c.User, t.GetTaskId())
		if err != nil {
			return durable.Transition{}, err
		}
		// 递增控制代次：清单中尚未到 P4 的动作从此无法开始，不依赖封闭请求何时送达。
		t.ControlGeneration++
		t.FrozenRound = n
		round := &lernav1.VerificationRound{
			TaskId:              t.GetTaskId(),
			RoundNo:             n,
			Status:              lernav1.VerificationStatus_VERIFICATION_STATUS_VERIFYING,
			RequirementsVersion: t.GetRequirementsVersion(),
			InputVersion:        t.GetInputVersion(),
			ControlGeneration:   t.GetControlGeneration(),
			ProposalId:          p.GetProposalId(),
		}
		var toSeal []string
		for _, op := range ops {
			round.OperationIds = append(round.OperationIds, op.GetOperationId())
			if !op.GetSettled() {
				toSeal = append(toSeal, op.GetOperationId())
			}
		}
		if err := saveRound(tx, c.User, round); err != nil {
			return durable.Transition{}, err
		}
		if err := saveTaskCore(tx, t); err != nil {
			return durable.Transition{}, err
		}
		if err := setProposalStatus(tx, c.User, p.GetProposalId(), proposalHandled); err != nil {
			return durable.Transition{}, err
		}
		if len(toSeal) > 0 {
			// 封闭范围是本轮清单中的准确动作集合，包括尚未收到接纳回执的意图。
			if err := tx.EnqueueHandoff(durable.Handoff{
				ID:     sealHandoffID(t.GetTaskId(), n),
				User:   c.User,
				Target: m.ledgerDomain(),
				Kind:   ports.CommandSealDispatch,
				Payload: &lernav1.SealDispatchCommand{
					UserId: c.User, TaskId: t.GetTaskId(), OperationIds: toSeal,
					Reason: fmt.Sprintf("completion verification round %d", n), ControlGeneration: t.GetControlGeneration(),
				},
				IntentRef: verifyPurpose(t.GetTaskId(), n),
			}); err != nil {
				return durable.Transition{}, err
			}
		}
		if _, err := tx.EnqueueJob(durable.JobSpec{Kind: JobVerify, User: c.User, Subject: t.GetTaskId(),
			PurposeKey: verifyPurpose(t.GetTaskId(), n), Spec: []byte(strconv.FormatInt(n, 10))}); err != nil {
			return durable.Transition{}, err
		}
		return durable.Done(), nil
	})
}

func (m *Module) ledgerDomain() string {
	if m.Catalog != nil {
		for _, d := range m.Catalog.All() {
			if c, ok := m.Catalog.Lookup(d.GetCapabilityId()); ok {
				return c.LedgerDomainID
			}
		}
	}
	return "ledger"
}

// onSealReceipt 保存执行管理的封闭回执，唤醒核验工作。
func (m *Module) onSealReceipt(_ context.Context, tx *durable.Tx, h *durable.HandoffRecord, _ *lernav1.Receipt) error {
	return tx.WakeJob(h.User, h.IntentRef)
}

// completionVerdict 是核验第二阶段的结论。
type completionVerdict int

const (
	verdictPass completionVerdict = iota + 1
	// verdictWait：目标可能已达成，但还需要执行管理的事实（缺证据），本轮保持冻结。
	verdictWait
	// verdictReject：目标未达成，或缺口无法自动补齐。
	verdictReject
	// verdictSuperseded：输入、条件或控制已经变更，本轮失去裁决资格。
	verdictSuperseded
)

type completionFacts struct {
	task         *lernav1.Task
	requirements *lernav1.RequirementSet
	round        *lernav1.VerificationRound
	operations   []*lernav1.OperationView
	judgements   []*lernav1.RequirementJudgement
}

type completionResult struct {
	verdict       completionVerdict
	gaps          []string
	evaluations   []*lernav1.RequirementEvaluation
	uncertainties []*lernav1.Uncertainty
}

// evaluateCompletion 检查完成-3 至完成-7（核心契约 2.6）。
func evaluateCompletion(f completionFacts) completionResult {
	t, r := f.task, f.round
	// 完成-7：关闭事务中读到的版本仍等于本轮核验记录的值。
	if t.GetRequirementsVersion() != r.GetRequirementsVersion() || t.GetInputVersion() != r.GetInputVersion() ||
		t.GetControlGeneration() != r.GetControlGeneration() {
		return completionResult{verdict: verdictSuperseded, gaps: []string{"核验期间条件、输入或控制已变更"}}
	}
	// 完成-4：本轮必须仍在核验中。
	if r.GetStatus() != lernav1.VerificationStatus_VERIFICATION_STATUS_VERIFYING {
		return completionResult{verdict: verdictSuperseded, gaps: []string{"本轮核验已结束"}}
	}
	res := completionResult{verdict: verdictPass}
	// 完成-3：条件集为 ACCEPTED，且已处理到当前输入版本。
	rs := f.requirements
	if rs.GetStatus() != lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED || rs.GetBoundInputVersion() != t.GetInputVersion() {
		return completionResult{verdict: verdictReject, gaps: []string{"条件集尚未接纳，或有会改变依据的输入尚未处理"}}
	}
	// 完成-6：全部已准入动作都已 SETTLED，清单完整。
	byID := map[string]*lernav1.OperationView{}
	listed := map[string]bool{}
	for _, id := range r.GetOperationIds() {
		listed[id] = true
	}
	waiting := false
	for _, op := range f.operations {
		byID[op.GetOperationId()] = op
		if !listed[op.GetOperationId()] {
			res.gaps = append(res.gaps, "动作 "+op.GetOperationId()+" 不在本轮清单中")
			res.verdict = verdictReject
			continue
		}
		if !op.GetSettled() {
			if op.GetReconcileState() == lernav1.ReconcileState_RECONCILE_STATE_PAUSED {
				res.gaps = append(res.gaps, "动作 "+op.GetOperationId()+" 结果未知，自动核对已暂停："+op.GetPauseReason())
				res.verdict = verdictReject
				continue
			}
			waiting = true
			continue
		}
		if op.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN {
			// G2 的唯一例外：确定不会再迟到生效的未知效果可以遗留，但必须在 Result 中披露。
			res.uncertainties = append(res.uncertainties, &lernav1.Uncertainty{
				OperationId: op.GetOperationId(), Effect: op.GetEffect(), LateEffect: op.GetLateEffect(),
				Note: "历史效果未知，执行管理已证明不会再迟到生效",
			})
		}
	}
	for id := range listed {
		if byID[id] == nil {
			res.gaps = append(res.gaps, "清单中的动作 "+id+" 没有记录")
			res.verdict = verdictReject
		}
	}
	// 完成-5：当前全部必要条件都有合格证据。
	judged := map[string]*lernav1.RequirementJudgement{}
	for _, j := range f.judgements {
		judged[j.GetRequirementId()] = j
	}
	for _, req := range rs.GetRequirements() {
		ev := &lernav1.RequirementEvaluation{
			RequirementId: req.GetRequirementId(), Description: req.GetDescription(), Rule: req.GetRule(), Source: req.GetSource(),
			Verdict: lernav1.Verdict_VERDICT_UNKNOWN,
		}
		res.evaluations = append(res.evaluations, ev)
		j := judged[req.GetRequirementId()]
		for _, ref := range j.GetEvidenceRefs() {
			if op := byID[ref]; op != nil && satisfies(req.GetRule(), op) {
				ev.Verdict = lernav1.Verdict_VERDICT_SATISFIED
				ev.EvidenceRefs = append(ev.EvidenceRefs, ref)
			}
		}
		if ev.GetVerdict() != lernav1.Verdict_VERDICT_SATISFIED && req.GetNecessary() {
			if j.GetVerdict() == lernav1.Verdict_VERDICT_UNSATISFIED {
				ev.Verdict = lernav1.Verdict_VERDICT_UNSATISFIED
			}
			res.gaps = append(res.gaps, "条件 "+req.GetRequirementId()+"（"+req.GetDescription()+"）没有合格证据")
			if res.verdict == verdictPass {
				res.verdict = verdictReject
			}
		}
	}
	if waiting && res.verdict == verdictPass {
		res.verdict = verdictWait
	}
	if waiting && res.verdict == verdictReject {
		// 还有动作在收尾：等待事实，不在未收尾时宣布结论。
		res.verdict = verdictWait
	}
	return res
}

// satisfies 按固定版本的核验规则判断一条证据是否合格。"模型认为满足了"不得自动成为证据。
func satisfies(rule *lernav1.VerificationRule, op *lernav1.OperationView) bool {
	if !op.GetSettled() {
		return false
	}
	switch rule.GetKind() {
	case lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_OPERATION_APPLIED:
		if op.GetCapabilityId() != rule.GetCapabilityId() || op.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED {
			return false
		}
		for k, v := range rule.GetParams() {
			if op.GetArguments()[k] != v {
				return false
			}
		}
		return true
	case lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_FILE_CONTENT:
		return op.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED &&
			op.GetObserved()["path"] == rule.GetParams()["path"] && op.GetObserved()["sha256"] == rule.GetExpectedDigest()
	}
	return false
}

// verify 是完成门禁的第二阶段：逐项核验并裁决。通过就在同一事务写入核验记录和 Result 并以
// SUCCEEDED 关闭；缺证据就保持冻结等待事实；目标未达成就记录缺口、释放冻结、按缺口重新规划。
func (m *Module) verify(ctx context.Context, c *durable.Claim) error {
	n, _ := strconv.ParseInt(string(c.Spec), 10, 64)
	return m.Domain.Advance(ctx, c, "tasks:verify", func(tx *durable.Tx) (durable.Transition, error) {
		t, err := loadTask(tx, c.User, c.Subject)
		if err != nil {
			return durable.Transition{}, err
		}
		r, err := loadRound(tx, c.User, t.GetTaskId(), n)
		if err != nil {
			return durable.Transition{}, err
		}
		if r == nil || r.GetStatus() != lernav1.VerificationStatus_VERIFICATION_STATUS_VERIFYING || t.GetFrozenRound() != n {
			return durable.Done(), nil
		}
		if h, err := tx.LoadHandoff(sealHandoffID(t.GetTaskId(), n)); err != nil {
			return durable.Transition{}, err
		} else if h != nil && h.State == durable.HandoffPending {
			// 封闭回执尚未保存：先等执行管理接纳封闭请求。
			return durable.WaitWake("waiting for the seal receipt"), nil
		}
		set, err := loadRequirementSet(tx, c.User, t.GetTaskId(), t.GetRequirementsVersion())
		if err != nil {
			return durable.Transition{}, err
		}
		ops, err := loadOperationViews(tx, c.User, t.GetTaskId())
		if err != nil {
			return durable.Transition{}, err
		}
		p, _, _, err := loadProposal(tx, c.User, r.GetProposalId())
		if err != nil {
			return durable.Transition{}, err
		}
		res := evaluateCompletion(completionFacts{task: t, requirements: set, round: r, operations: ops, judgements: p.GetCompletion().GetJudgements()})
		switch res.verdict {
		case verdictPass:
			r.Status = lernav1.VerificationStatus_VERIFICATION_STATUS_PASSED
			if err := saveRound(tx, c.User, r); err != nil {
				return durable.Transition{}, err
			}
			t.FrozenRound = 0
			return durable.Done(), m.closeTask(tx, t, lernav1.TaskOutcome_TASK_OUTCOME_SUCCEEDED, "completion gate passed",
				res.evaluations, res.uncertainties, p.GetCompletion().GetAnswer(), n)
		case verdictWait:
			if err := setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_RECONCILIATION,
				Ref: verifyPurpose(t.GetTaskId(), n), Gap: "完成核验等待动作收尾"}); err != nil {
				return durable.Transition{}, err
			}
			return durable.WaitWake("waiting for operations to settle"), nil
		case verdictSuperseded:
			return durable.Done(), m.supersedeRound(tx, t, "completion facts changed during verification")
		default:
			return durable.Done(), m.rejectRound(tx, t, r, res.gaps)
		}
	})
}

// rejectRound 在一个事务中保存逐项缺口、把本轮记为 REJECTED、总是释放本轮冻结，
// 再按当前状态登记后续工作：可以推进就重新请求提议，有阻断性未知就等待核对。
func (m *Module) rejectRound(tx *durable.Tx, t *lernav1.Task, r *lernav1.VerificationRound, gaps []string) error {
	r.Status = lernav1.VerificationStatus_VERIFICATION_STATUS_REJECTED
	r.Gaps = gaps
	if err := saveRound(tx, t.GetUserId(), r); err != nil {
		return err
	}
	t.FrozenRound = 0
	if err := clearWaiting(tx, t, lernav1.WaitingKind_WAITING_KIND_RECONCILIATION); err != nil {
		return err
	}
	if err := saveTaskCore(tx, t); err != nil {
		return err
	}
	return m.maybeRequestProposal(tx, t)
}

// supersedeRound 在引起变更的同一事务中把进行中的核验轮次标为 SUPERSEDED 并释放冻结。
// 它不代表条件不满足；按当前控制登记后续工作由调用方决定。
func (m *Module) supersedeRound(tx *durable.Tx, t *lernav1.Task, reason string) error {
	if t.GetFrozenRound() == 0 {
		return nil
	}
	r, err := loadRound(tx, t.GetUserId(), t.GetTaskId(), t.GetFrozenRound())
	if err != nil {
		return err
	}
	if r != nil && r.GetStatus() == lernav1.VerificationStatus_VERIFICATION_STATUS_VERIFYING {
		r.Status = lernav1.VerificationStatus_VERIFICATION_STATUS_SUPERSEDED
		r.Reason = reason
		if err := saveRound(tx, t.GetUserId(), r); err != nil {
			return err
		}
	}
	if err := tx.SealJob(t.GetUserId(), verifyPurpose(t.GetTaskId(), t.GetFrozenRound())); err != nil {
		return err
	}
	t.FrozenRound = 0
	return saveTaskCore(tx, t)
}

// closeTask 以 outcome 关闭任务：Result 与任务终态在同一事务写入，之后不再修改。
// 关闭结束的是目标的生命周期，不抹掉遗留的责任：执行管理的核对和预算的结算继续。
func (m *Module) closeTask(tx *durable.Tx, t *lernav1.Task, outcome lernav1.TaskOutcome, reason string,
	evals []*lernav1.RequirementEvaluation, uncertainties []*lernav1.Uncertainty, answer string, round int64) error {
	ops, err := loadOperationViews(tx, t.GetUserId(), t.GetTaskId())
	if err != nil {
		return err
	}
	res := &lernav1.Result{
		UserId:                 t.GetUserId(),
		TaskId:                 t.GetTaskId(),
		Outcome:                outcome,
		CloseReason:            reason,
		RequirementsVersion:    t.GetRequirementsVersion(),
		RequirementEvaluations: evals,
		Uncertainties:          uncertainties,
		Answer:                 answer,
		ClosedAt:               timestamppb.New(tx.Now()),
		VerificationRound:      round,
		UsageSnapshotRef:       "budget/task/" + t.GetTaskId() + "@" + strconv.FormatInt(tx.NowMs(), 10),
	}
	ledger := m.ledgerDomain()
	for _, op := range ops {
		res.OperationSnapshotRefs = append(res.OperationSnapshotRefs, op.GetOperationId()+"@"+strconv.FormatInt(op.GetLedgerRevision(), 10))
		if !op.GetSettled() || op.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN {
			if outcome != lernav1.TaskOutcome_TASK_OUTCOME_SUCCEEDED {
				res.Uncertainties = append(res.Uncertainties, &lernav1.Uncertainty{
					OperationId: op.GetOperationId(), LedgerDomainId: ledger, Effect: op.GetEffect(), LateEffect: op.GetLateEffect(),
					Note: "执行管理继续核对；预算继续结算",
				})
			}
			res.PendingResponsibilityRefs = append(res.PendingResponsibilityRefs, ledger+"/"+op.GetOperationId())
		}
	}
	for _, u := range res.Uncertainties {
		if u.GetLedgerDomainId() == "" {
			u.LedgerDomainId = ledger
		}
	}
	sort.SliceStable(res.Uncertainties, func(i, j int) bool {
		return res.Uncertainties[i].GetOperationId() < res.Uncertainties[j].GetOperationId()
	})
	blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(res)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO results (user_id, task_id, outcome, record, closed_at) VALUES (?, ?, ?, ?, ?)`,
		t.GetUserId(), t.GetTaskId(), int32(outcome), blob, tx.NowMs()); err != nil {
		return err
	}
	t.Lifecycle = lernav1.TaskLifecycle_TASK_LIFECYCLE_CLOSED
	t.ResultRef = "result/" + t.GetTaskId()
	t.FrozenRound = 0
	t.WaitingOn = nil
	t.Progress = lernav1.TaskProgress_TASK_PROGRESS_RUNNING
	if err := saveTaskCore(tx, t); err != nil {
		return err
	}
	return m.afterClose(tx, t)
}
