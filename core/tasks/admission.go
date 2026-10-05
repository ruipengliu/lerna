package tasks

import (
	"context"
	"crypto/sha256"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/grants"
)

// 工作类别（核心契约 2.6）：说明这次准入为什么可以跳过部分条件，与处理目的是两回事。
const (
	WorkTarget  = "TARGET"
	WorkPrepare = "PREPARE"
	WorkClosure = "CLOSURE"
)

// admissionFacts 是准入门禁在裁决事务中读到的当前事实。
type admissionFacts struct {
	task           *lernav1.Task
	requirements   *lernav1.RequirementSet
	proposal       *lernav1.Proposal
	proposalStatus string
	operations     []*lernav1.OperationView
	frozen         bool
	workClass      string
}

// checkAdmission 检查准入-1 至准入-5（核心契约 2.6）。准入-6（记忆依赖）在 M1 没有记忆策略，
// 不产生依赖；准入-7 至准入-10 由授权、预算、会话和内容各自在同一事务中检查。
func checkAdmission(f admissionFacts) error {
	t, p := f.task, f.proposal
	// 准入-1：提议属于当前请求、未被消费，规划代次是当前的。
	if p == nil {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "proposal not found")
	}
	if f.proposalStatus != proposalReceived {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION, "proposal %s is already %s", p.GetProposalId(), f.proposalStatus)
	}
	if p.GetPlanningGeneration() != t.GetPlanningGeneration() {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION,
			"proposal planning generation %d, current %d", p.GetPlanningGeneration(), t.GetPlanningGeneration())
	}
	// 准入-4：本任务允许推进：OPEN、ACTIVE、没有核验冻结。
	if t.GetLifecycle() != lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION, "task is closed")
	}
	if t.GetControl() != lernav1.TaskControl_TASK_CONTROL_ACTIVE {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION, "task control is %s", t.GetControl())
	}
	if f.frozen && f.workClass == WorkTarget {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION, "a completion verification round holds the freeze")
	}
	// 准入-2：条件集版本、输入版本、控制代次都是当前的。
	if p.GetRequirementsVersion() != t.GetRequirementsVersion() {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_REQUIREMENT,
			"proposal requirements v%d, current v%d", p.GetRequirementsVersion(), t.GetRequirementsVersion())
	}
	if p.GetInputVersion() != t.GetInputVersion() {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_INPUT, "proposal input v%d, current v%d", p.GetInputVersion(), t.GetInputVersion())
	}
	if p.GetControlGeneration() != t.GetControlGeneration() {
		return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION,
			"proposal control generation %d, current %d", p.GetControlGeneration(), t.GetControlGeneration())
	}
	// 准入-3：条件集已接纳，且已处理到当前输入版本（准备类准入豁免）。
	if f.workClass == WorkTarget {
		rs := f.requirements
		if rs.GetStatus() != lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_REQUIREMENT, "requirement set v%d is not accepted", rs.GetVersion())
		}
		if rs.GetBoundInputVersion() != t.GetInputVersion() {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_STALE_INPUT,
				"requirement set bound to input v%d, current input v%d", rs.GetBoundInputVersion(), t.GetInputVersion())
		}
	}
	// 准入-5：没有阻断性的未知效果（收尾类准入豁免）。
	if f.workClass == WorkTarget {
		for _, op := range f.operations {
			if blocking(op) {
				return errs.New(lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE,
					"operation %s is not settled (effect %s); new target actions wait for reconciliation", op.GetOperationId(), op.GetEffect())
			}
		}
	}
	return nil
}

// blocking 判断一个目标动作是否阻断新的目标准入：效果未知，或者尚未收尾、也没有确定已生效。
// 还没收到执行管理的回报不等于没有执行，所以未回报的动作同样阻断。
func blocking(op *lernav1.OperationView) bool {
	if op.GetOrigin() == "" || isCoreWork(op.GetOrigin()) {
		return false
	}
	if op.GetSettled() {
		return op.GetEffect() == lernav1.EffectOutcome_EFFECT_OUTCOME_UNKNOWN && op.GetLateEffect() != lernav1.LateEffect_LATE_EFFECT_RULED_OUT
	}
	return op.GetEffect() != lernav1.EffectOutcome_EFFECT_OUTCOME_APPLIED
}

func isCoreWork(origin string) bool {
	return len(origin) >= 5 && origin[:5] == "core:"
}

// sendQuota 是准入时分配的实际发送额度：目标对同一尝试标识幂等时，核心事先为一次安全重发分配额度。
func sendQuota(d *lernav1.CapabilityDeclaration) int32 {
	if d.GetIdempotency().GetSupported() && d.GetIdempotency().GetKeyValiditySeconds() > 0 {
		return 2
	}
	return 1
}

// queryQuota 是预先准入的有上限核对计划（执行管理 4.3）：核对查询本身也经过准入、预算和出口。
func queryQuota(d *lernav1.CapabilityDeclaration) int32 {
	if d.GetQuery().GetSupported() {
		return 4
	}
	return 0
}

// RequestDigest 是动作参数的规范化摘要：能力和按键排序的参数。
func RequestDigest(capability string, args map[string]string) []byte {
	h := sha256.New()
	h.Write([]byte(capability))
	for _, k := range sortedKeys(args) {
		h.Write([]byte{0})
		h.Write([]byte(k))
		h.Write([]byte{0})
		h.Write([]byte(args[k]))
	}
	return h.Sum(nil)
}

// handleAdmit 是持久点 5：在一个可串行化的裁决事务中检查准入门禁，保存动作意图、
// 授权使用、预算预留、确认消费、回执和交接。任何一项不通过都不留下部分修改。
func (m *Module) handleAdmit(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
	cmd := in.Payload.(*lernav1.AdmitProposalCommand)
	user := in.UserID()
	t, err := loadTask(tx, user, cmd.GetTaskId())
	if err != nil {
		return durable.Outcome{}, err
	}
	if t == nil {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "task %s not found", cmd.GetTaskId())
	}
	p, status, _, err := loadProposal(tx, user, cmd.GetProposalId())
	if err != nil {
		return durable.Outcome{}, err
	}
	f := admissionFacts{task: t, proposal: p, proposalStatus: status, workClass: WorkTarget}
	if f.requirements, err = loadRequirementSet(tx, user, t.GetTaskId(), t.GetRequirementsVersion()); err != nil {
		return durable.Outcome{}, err
	}
	if f.operations, err = loadOperationViews(tx, user, t.GetTaskId()); err != nil {
		return durable.Outcome{}, err
	}
	if f.frozen, err = verificationFrozen(tx, t); err != nil {
		return durable.Outcome{}, err
	}
	if err := checkAdmission(f); err != nil {
		return durable.Outcome{}, err
	}
	var step *lernav1.PlanStep
	for _, s := range p.GetAction().GetSteps() {
		if s.GetStepId() == cmd.GetStepId() {
			step = s
		}
	}
	if step == nil {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "step %s not in proposal", cmd.GetStepId())
	}
	origin := "proposal:" + p.GetProposalId() + "/" + step.GetStepId()
	return m.admitOperation(tx, t, admitSpec{
		origin:     origin,
		workClass:  WorkTarget,
		capability: step.GetCapabilityId(),
		args:       step.GetArguments(),
		proposalID: p.GetProposalId(),
		stepID:     step.GetStepId(),
	})
}

// admitSpec 描述一个待准入的动作。
type admitSpec struct {
	origin     string
	workClass  string
	capability string
	args       map[string]string
	proposalID string
	stepID     string
	// 模型调用：提议请求和调用位置。
	modelRequestID string
	modelPosition  int32
}

// admitOperation 执行准入-7 至准入-10，并保存准入记录、动作视图和向执行管理的交接（R7 第一步）。
func (m *Module) admitOperation(tx *durable.Tx, t *lernav1.Task, s admitSpec) (durable.Outcome, error) {
	user := t.GetUserId()
	if m.Catalog == nil {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_UNSUPPORTED_FEATURE, "no capability catalog")
	}
	capb, ok := m.Catalog.Lookup(s.capability)
	if !ok {
		return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "unknown capability %q", s.capability)
	}
	for _, a := range capb.Decl.GetRequiredArguments() {
		if _, ok := s.args[a]; !ok {
			return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "capability %s needs argument %q", s.capability, a)
		}
	}
	opID := ids.New()
	admissionID := ids.New()
	digest := RequestDigest(s.capability, s.args)
	// 准入-7：授权链有效，覆盖动作、操作权利和处理目的，还有剩余次数；原子建立使用记录。
	grantID, useID, confirmationRef, err := m.occupyGrant(tx, t, s, opID, admissionID, digest)
	if err != nil {
		return durable.Outcome{}, err
	}
	// 准入-8：预算预留；费用上界未知的调用不准入。
	sends, queries := sendQuota(capb.Decl), queryQuota(capb.Decl)
	basis, err := budget.Reserve(tx, budget.ReserveRequest{
		User: user, TaskID: t.GetTaskId(), AdmissionID: admissionID, OperationID: opID,
		Billing: capb.Decl.GetBilling(), SendQuota: sends, QueryQuota: queries,
	})
	if err != nil {
		return durable.Outcome{}, err
	}
	credID, err := grants.IssueCredential(tx, user, opID, grantID, useID, capb.EndpointID, digest)
	if err != nil {
		return durable.Outcome{}, err
	}
	rec := &lernav1.AdmissionRecord{
		AdmissionId:         admissionID,
		UserId:              user,
		TaskId:              t.GetTaskId(),
		Origin:              s.origin,
		RequirementsVersion: t.GetRequirementsVersion(),
		InputVersion:        t.GetInputVersion(),
		ControlGeneration:   t.GetControlGeneration(),
		OperationId:         opID,
		LedgerDomainId:      capb.LedgerDomainID,
		ExecutorEndpointId:  capb.EndpointID,
		GrantRefs:           []string{grantID},
		BudgetRef:           basis.GetReservationId(),
		Ceiling:             basis.GetCeiling(),
		Reserved:            basis.GetReserved(),
		ConfirmationRef:     confirmationRef,
		Parameters:          s.args,
		CapabilityId:        s.capability,
		CapabilityVersion:   capb.Decl.GetCapabilityVersion(),
		SendQuota:           sends,
		QueryQuota:          queries,
		ModelRequestId:      s.modelRequestID,
		ModelCallPosition:   s.modelPosition,
		WorkClass:           s.workClass,
	}
	blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(rec)
	if err != nil {
		return durable.Outcome{}, err
	}
	if _, err := tx.Exec(`INSERT INTO admissions (user_id, admission_id, task_id, operation_id, origin, record, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, user, admissionID, t.GetTaskId(), opID, s.origin, blob, tx.NowMs()); err != nil {
		return durable.Outcome{}, err
	}
	if err := saveOperationView(tx, user, t.GetTaskId(), &lernav1.OperationView{
		OperationId:               opID,
		CapabilityId:              s.capability,
		Origin:                    s.origin,
		Arguments:                 s.args,
		AdmittedControlGeneration: t.GetControlGeneration(),
	}); err != nil {
		return durable.Outcome{}, err
	}
	if s.proposalID != "" {
		if err := setProposalStatus(tx, user, s.proposalID, proposalAdmitted); err != nil {
			return durable.Outcome{}, err
		}
	}
	intent := &lernav1.OperationIntent{
		UserId:             user,
		OperationId:        opID,
		TaskId:             t.GetTaskId(),
		AdmissionRef:       admissionID,
		LedgerDomainId:     capb.LedgerDomainID,
		ExecutorEndpointId: capb.EndpointID,
		AdapterRef:         capb.AdapterID,
		Capability:         capb.Decl,
		Parameters:         s.args,
		RequestDigest:      digest,
		SendQuota:          sends,
		QueryQuota:         queries,
		ControlGeneration:  t.GetControlGeneration(),
		Origin:             s.origin,
		GrantRefs:          []string{grantID},
		BudgetBasis:        basis,
		CredentialRef:      credID,
	}
	// 持久点 6a：本方已有意图和交接记录；执行管理的持久接纳由投递工作完成（R7）。
	if err := tx.EnqueueHandoff(durable.Handoff{
		ID:        "intent:" + opID,
		User:      user,
		Target:    capb.LedgerDomainID,
		Kind:      ports.CommandAcceptIntent,
		Payload:   intent,
		IntentRef: opID,
	}); err != nil {
		return durable.Outcome{}, err
	}
	return durable.Outcome{Result: &lernav1.AdmitProposalResult{OperationId: opID, AdmissionId: admissionID}}, nil
}

// onIntentReceipt 是 R7 第三步：保存执行管理的接纳回执。
func (m *Module) onIntentReceipt(_ context.Context, tx *durable.Tx, h *durable.HandoffRecord, r *lernav1.Receipt) error {
	v, taskID, err := loadOperationView(tx, h.User, h.IntentRef)
	if err != nil || v == nil {
		return err
	}
	if r.GetDecision() == lernav1.Decision_DECISION_ACCEPTED {
		v.LedgerAccepted = true
	}
	return saveOperationView(tx, h.User, taskID, v)
}
