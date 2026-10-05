package tasks

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
)

// 提议请求与提议的工作类型。
const (
	JobPropose    = "tasks.propose"
	JobAdjudicate = "tasks.adjudicate"
)

// M1 的推理限制（推理接口 9）：每个提议请求最多一个调用位置，实际发送上限 1，计划长度 1。
var m1Limits = &lernav1.ReasonerLimits{CallPositions: 1, ActualSends: 1, PlanLength: 1}

// 提议请求与提议的状态。
const (
	requestPending    = "PENDING"
	requestReported   = "REPORTED"
	requestSuperseded = "SUPERSEDED"
	requestFailed     = "FAILED"

	proposalReceived = "RECEIVED"
	proposalAdmitted = "ADMITTED"
	proposalRejected = "REJECTED"
	proposalStale    = "STALE"
	proposalHandled  = "HANDLED"
)

// Capability 是能力目录中的一项：声明和固定的执行归属。
type Capability struct {
	Decl           *lernav1.CapabilityDeclaration
	AdapterID      string
	LedgerDomainID string
	EndpointID     string
}

// Catalog 是核心固定的能力目录（受审查的参考适配器）。
type Catalog interface {
	Lookup(capabilityID string) (Capability, bool)
	All() []*lernav1.CapabilityDeclaration
	Version() string
}

// requestProposal 登记新一轮提议请求（推理接口持久点 1）：递增规划代次，固定快照，登记待办。
// 同一时刻最多一个可裁决的请求；旧请求被替代，迟到的输出不会重新变成可执行的提议。
func (m *Module) requestProposal(tx *durable.Tx, t *lernav1.Task) error {
	if _, err := tx.Exec(`UPDATE proposal_requests SET status = ? WHERE user_id = ? AND task_id = ? AND status = ?`,
		requestSuperseded, t.GetUserId(), t.GetTaskId(), requestPending); err != nil {
		return err
	}
	// 新一轮提议开始：此前推理失败留下的等待不再适用。
	if err := clearWaiting(tx, t, lernav1.WaitingKind_WAITING_KIND_EXTERNAL); err != nil {
		return err
	}
	t.PlanningGeneration++
	if _, err := tx.Exec(`UPDATE tasks SET planning_generation = ?, revision = revision + 1 WHERE user_id = ? AND task_id = ?`,
		t.GetPlanningGeneration(), t.GetUserId(), t.GetTaskId()); err != nil {
		return err
	}
	snap, err := m.buildSnapshot(tx, t)
	if err != nil {
		return err
	}
	blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(snap)
	if err != nil {
		return err
	}
	reqID := ids.New()
	if _, err := tx.Exec(`INSERT INTO proposal_requests (user_id, request_id, task_id, planning_generation, snapshot, status, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, t.GetUserId(), reqID, t.GetTaskId(), t.GetPlanningGeneration(), blob, requestPending, tx.NowMs()); err != nil {
		return err
	}
	_, err = tx.EnqueueJob(durable.JobSpec{Kind: JobPropose, User: t.GetUserId(), Subject: reqID, PurposeKey: "propose:" + reqID})
	return err
}

// buildSnapshot 在裁决事务中固定上下文快照：目标、条件版本、输入版本、控制版本、进展和能力目录。
// 快照不携带行动权限。
func (m *Module) buildSnapshot(tx *durable.Tx, t *lernav1.Task) (*lernav1.ContextSnapshot, error) {
	set, err := loadRequirementSet(tx, t.GetUserId(), t.GetTaskId(), t.GetRequirementsVersion())
	if err != nil {
		return nil, err
	}
	var goalInput string
	if err := tx.QueryRow(`SELECT goal_input_id FROM tasks WHERE user_id = ? AND task_id = ?`, t.GetUserId(), t.GetTaskId()).Scan(&goalInput); err != nil {
		return nil, err
	}
	text, err := sessions.InputText(tx, t.GetUserId(), goalInput)
	if err != nil {
		return nil, err
	}
	ops, err := loadOperationViews(tx, t.GetUserId(), t.GetTaskId())
	if err != nil {
		return nil, err
	}
	snap := &lernav1.ContextSnapshot{
		SnapshotId:          ids.New(),
		UserId:              t.GetUserId(),
		TaskId:              t.GetTaskId(),
		GoalRef:             t.GetGoalRef(),
		GoalText:            text,
		RequirementsVersion: t.GetRequirementsVersion(),
		Requirements:        set.GetRequirements(),
		InputVersion:        t.GetInputVersion(),
		ControlGeneration:   t.GetControlGeneration(),
		PlanningGeneration:  t.GetPlanningGeneration(),
		AllowedPurposes:     []string{"TARGET"},
		Limits:              m1Limits,
	}
	if set.GetStatus() != lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED || set.GetBoundInputVersion() != t.GetInputVersion() {
		// 有未处理的、会改变依据的输入时，只允许解释、澄清和收尾。
		snap.AllowedPurposes = []string{"PREPARE", "CLARIFY", "CLOSURE"}
	}
	for _, op := range ops {
		snap.Progress = append(snap.Progress, &lernav1.ProgressItem{
			OperationId:  op.GetOperationId(),
			CapabilityId: op.GetCapabilityId(),
			Arguments:    op.GetArguments(),
			Effect:       op.GetEffect(),
			LateEffect:   op.GetLateEffect(),
			Settled:      op.GetSettled(),
			Origin:       op.GetOrigin(),
		})
	}
	if m.Catalog != nil {
		snap.CapabilityCatalogVersion = m.Catalog.Version()
		snap.Capabilities = m.Catalog.All()
	}
	if snap.Answers, err = sessions.TaskAnswers(tx, t.GetUserId(), t.GetTaskId()); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT input_id FROM task_inputs WHERE user_id = ? AND task_id = ? AND input_version > ? AND changes_basis = 1
		ORDER BY task_input_seq`, t.GetUserId(), t.GetTaskId(), set.GetBoundInputVersion())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return nil, err
		}
		snap.UnprocessedInputs = append(snap.UnprocessedInputs, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	rounds, err := loadRounds(tx, t.GetUserId(), t.GetTaskId())
	if err != nil {
		return nil, err
	}
	if n := len(rounds); n > 0 && rounds[n-1].GetStatus() == lernav1.VerificationStatus_VERIFICATION_STATUS_REJECTED {
		// 完成核验被拒绝：把具体缺口交给推理，按缺口重新规划。
		snap.Gaps = rounds[n-1].GetGaps()
	}
	return snap, nil
}

func loadOperationViews(tx *durable.Tx, user, taskID string) ([]*lernav1.OperationView, error) {
	rows, err := tx.Query(`SELECT view FROM task_operations WHERE user_id = ? AND task_id = ? ORDER BY rowid`, user, taskID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var out []*lernav1.OperationView
	for rows.Next() {
		var blob []byte
		if err := rows.Scan(&blob); err != nil {
			return nil, err
		}
		v := &lernav1.OperationView{}
		if err := proto.Unmarshal(blob, v); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func saveOperationView(tx *durable.Tx, user, taskID string, v *lernav1.OperationView) error {
	blob, err := proto.Marshal(v)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO task_operations (user_id, task_id, operation_id, ledger_revision, view) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (user_id, operation_id) DO UPDATE SET ledger_revision = excluded.ledger_revision, view = excluded.view`,
		user, taskID, v.GetOperationId(), v.GetLedgerRevision(), blob)
	return err
}

func loadOperationView(tx *durable.Tx, user, opID string) (*lernav1.OperationView, string, error) {
	var blob []byte
	var taskID string
	err := tx.QueryRow(`SELECT view, task_id FROM task_operations WHERE user_id = ? AND operation_id = ?`, user, opID).Scan(&blob, &taskID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	v := &lernav1.OperationView{}
	return v, taskID, proto.Unmarshal(blob, v)
}

type requestRow struct {
	id         string
	taskID     string
	generation int64
	status     string
	snapshot   *lernav1.ContextSnapshot
	digest     []byte
}

func loadRequest(tx *durable.Tx, user, reqID string) (*requestRow, error) {
	r := &requestRow{id: reqID}
	var blob []byte
	err := tx.QueryRow(`SELECT task_id, planning_generation, status, snapshot FROM proposal_requests WHERE user_id = ? AND request_id = ?`,
		user, reqID).Scan(&r.taskID, &r.generation, &r.status, &blob)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.snapshot = &lernav1.ContextSnapshot{}
	if err := proto.Unmarshal(blob, r.snapshot); err != nil {
		return nil, err
	}
	sum := sha256.Sum256(blob)
	r.digest = sum[:]
	return r, nil
}

// propose 是推理工作：读取固定快照，在事务之外调用推理，然后在新事务中持久接收提议
// （持久点 4）。推理的输出只是提议，不产生任何执行（G6、R1）。
func (m *Module) propose(ctx context.Context, c *durable.Claim) error {
	var req *requestRow
	current := false
	if err := m.Domain.Read(ctx, func(tx *durable.Tx) error {
		var err error
		if req, err = loadRequest(tx, c.User, c.Subject); err != nil || req == nil {
			return err
		}
		t, err := loadTask(tx, c.User, req.taskID)
		if err != nil || t == nil {
			return err
		}
		current = req.status == requestPending && t.GetPlanningGeneration() == req.generation &&
			t.GetLifecycle() == lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN
		return nil
	}); err != nil {
		return err
	}
	if req == nil || !current {
		// 被替代的代次不得推进任务。
		return m.Domain.Advance(ctx, c, "tasks:propose_superseded", func(*durable.Tx) (durable.Transition, error) {
			return durable.Done(), nil
		})
	}
	if m.Reasoner == nil {
		return m.Domain.Advance(ctx, c, "tasks:propose_no_reasoner", func(*durable.Tx) (durable.Transition, error) {
			return durable.WaitWake("no reasoner configured"), nil
		})
	}
	pr := &lernav1.ProposalRequest{
		RequestId:          req.id,
		TaskId:             req.taskID,
		SnapshotId:         req.snapshot.GetSnapshotId(),
		SnapshotDigest:     req.digest,
		PlanningGeneration: req.generation,
		Limits:             m1Limits,
	}
	caller := m.modelCaller(c.User, req)
	p, perr := m.Reasoner.Propose(ctx, pr, proto.Clone(req.snapshot).(*lernav1.ContextSnapshot), caller)
	if errors.Is(perr, ports.ErrAwaitingModelCall) {
		// 模型调用已准入或待准入；结果就绪后唤醒，同一请求按调用位置拿回原结果。
		return m.Domain.Advance(ctx, c, "tasks:propose_await_model", func(*durable.Tx) (durable.Transition, error) {
			return durable.WaitWake("awaiting model call"), nil
		})
	}
	return m.Domain.Advance(ctx, c, "tasks:receive_proposal", func(tx *durable.Tx) (durable.Transition, error) {
		cur, err := loadRequest(tx, c.User, req.id)
		if err != nil {
			return durable.Transition{}, err
		}
		t, err := loadTask(tx, c.User, req.taskID)
		if err != nil {
			return durable.Transition{}, err
		}
		if cur.status != requestPending || t.GetPlanningGeneration() != req.generation {
			// 旧规划代次的提议拒绝。
			return durable.Done(), nil
		}
		if perr != nil {
			return durable.Done(), m.recordReasonerFailure(tx, t, req.id, perr)
		}
		return durable.Done(), m.receiveProposal(tx, t, req, p)
	})
}

func (m *Module) recordReasonerFailure(tx *durable.Tx, t *lernav1.Task, reqID string, perr error) error {
	if _, err := tx.Exec(`UPDATE proposal_requests SET status = ? WHERE user_id = ? AND request_id = ?`,
		requestFailed, t.GetUserId(), reqID); err != nil {
		return err
	}
	gap := "推理未给出提议：" + perr.Error()
	if errors.Is(perr, ports.ErrPreparationUnrecoverable) {
		gap = "准备不可恢复：" + perr.Error()
	}
	return setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_EXTERNAL, Ref: reqID, Gap: gap})
}

// receiveProposal 保存提议及其依据（持久点 4），然后登记裁决。提议绑定快照中的条件版本、
// 输入版本和控制版本；推理不得自行选择版本或负责方。
func (m *Module) receiveProposal(tx *durable.Tx, t *lernav1.Task, req *requestRow, p *lernav1.Proposal) error {
	if p == nil {
		return m.recordReasonerFailure(tx, t, req.id, errors.New("empty proposal"))
	}
	p = proto.Clone(p).(*lernav1.Proposal)
	p.ProposalId = req.id + "/p"
	p.RequestId = req.id
	p.TaskId = req.taskID
	p.ContextSnapshotRef = req.snapshot.GetSnapshotId()
	p.PlanningGeneration = req.generation
	p.RequirementsVersion = req.snapshot.GetRequirementsVersion()
	p.InputVersion = req.snapshot.GetInputVersion()
	p.ControlGeneration = req.snapshot.GetControlGeneration()
	status := proposalReceived
	verr := validateProposal(p)
	if verr != nil {
		status = proposalRejected
	}
	blob, err := proto.MarshalOptions{Deterministic: true}.Marshal(p)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO proposals (user_id, proposal_id, request_id, task_id, planning_generation, kind, record,
		status, adjudications, created_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?)`,
		t.GetUserId(), p.GetProposalId(), req.id, req.taskID, req.generation, int32(p.GetKind()), blob, status, tx.NowMs()); err != nil {
		return err
	}
	if _, err := tx.Exec(`UPDATE proposal_requests SET status = ? WHERE user_id = ? AND request_id = ?`,
		requestReported, t.GetUserId(), req.id); err != nil {
		return err
	}
	if verr != nil {
		return setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_EXTERNAL, Ref: p.GetProposalId(),
			Gap: "推理给出的提议不合法：" + verr.Error()})
	}
	_, err = tx.EnqueueJob(durable.JobSpec{Kind: JobAdjudicate, User: t.GetUserId(), Subject: p.GetProposalId(),
		PurposeKey: "adjudicate:" + p.GetProposalId()})
	return err
}

// validateProposal 检查提议的形状：一次提议只有一个主类型；有限计划长度上限为 1，参数只允许具体值。
func validateProposal(p *lernav1.Proposal) error {
	switch p.GetKind() {
	case lernav1.ProposalKind_PROPOSAL_KIND_ACTION:
		steps := p.GetAction().GetSteps()
		if len(steps) == 0 || int32(len(steps)) > m1Limits.GetPlanLength() {
			return fmt.Errorf("plan must have 1..%d steps, got %d", m1Limits.GetPlanLength(), len(steps))
		}
		for _, s := range steps {
			if s.GetStepId() == "" || s.GetCapabilityId() == "" {
				return errors.New("each step needs step_id and capability_id")
			}
			if len(s.GetDependsOn()) > 0 {
				return errors.New("M1 plans cannot depend on earlier steps")
			}
		}
	case lernav1.ProposalKind_PROPOSAL_KIND_COMPLETION:
		if len(p.GetCompletion().GetJudgements()) == 0 {
			return errors.New("a completion proposal must judge each requirement")
		}
	case lernav1.ProposalKind_PROPOSAL_KIND_ASK_USER:
		if p.GetAskUser().GetQuestion() == "" {
			return errors.New("question is empty")
		}
	case lernav1.ProposalKind_PROPOSAL_KIND_MODIFY_REQUIREMENTS:
		if len(p.GetModifyRequirements().GetRequirements()) == 0 {
			return errors.New("modification carries no requirements")
		}
	default:
		return errors.New("proposal kind is missing")
	}
	return nil
}

// setWaiting 记下一项等待：多项等待分别保存，解除一项不清空其他项。
func setWaiting(tx *durable.Tx, t *lernav1.Task, w *lernav1.WaitingOn) error {
	for _, x := range t.GetWaitingOn() {
		if x.GetKind() == w.GetKind() && x.GetRef() == w.GetRef() {
			return nil
		}
	}
	t.WaitingOn = append(t.WaitingOn, w)
	t.Progress = lernav1.TaskProgress_TASK_PROGRESS_WAITING
	return saveProgress(tx, t)
}

// clearWaiting 解除指定种类的等待。
func clearWaiting(tx *durable.Tx, t *lernav1.Task, kinds ...lernav1.WaitingKind) error {
	var keep []*lernav1.WaitingOn
	for _, x := range t.GetWaitingOn() {
		drop := false
		for _, k := range kinds {
			if x.GetKind() == k {
				drop = true
			}
		}
		if !drop {
			keep = append(keep, x)
		}
	}
	t.WaitingOn = keep
	if len(keep) == 0 {
		t.Progress = lernav1.TaskProgress_TASK_PROGRESS_RUNNING
	}
	return saveProgress(tx, t)
}

func saveProgress(tx *durable.Tx, t *lernav1.Task) error {
	waiting, err := marshalWaiting(t.GetWaitingOn())
	if err != nil {
		return err
	}
	_, err = tx.Exec(`UPDATE tasks SET progress = ?, waiting_on = ?, revision = revision + 1 WHERE user_id = ? AND task_id = ?`,
		int32(t.GetProgress()), waiting, t.GetUserId(), t.GetTaskId())
	return err
}

func loadProposal(tx *durable.Tx, user, proposalID string) (*lernav1.Proposal, string, int, error) {
	var blob []byte
	var status string
	var n int
	err := tx.QueryRow(`SELECT record, status, adjudications FROM proposals WHERE user_id = ? AND proposal_id = ?`, user, proposalID).
		Scan(&blob, &status, &n)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, "", 0, nil
	}
	if err != nil {
		return nil, "", 0, err
	}
	p := &lernav1.Proposal{}
	return p, status, n, proto.Unmarshal(blob, p)
}

func setProposalStatus(tx *durable.Tx, user, proposalID, status string) error {
	_, err := tx.Exec(`UPDATE proposals SET status = ? WHERE user_id = ? AND proposal_id = ?`, status, user, proposalID)
	return err
}

// adjudicate 裁决一项提议。行动提议走准入命令（准入门禁在裁决域的一个事务里检查）；
// 拒绝作为命令决定持久保存，之后按原因登记后续工作。
func (m *Module) adjudicate(ctx context.Context, c *durable.Claim) error {
	var p *lernav1.Proposal
	var status string
	var attempts int
	if err := m.Domain.Read(ctx, func(tx *durable.Tx) error {
		var err error
		p, status, attempts, err = loadProposal(tx, c.User, c.Subject)
		return err
	}); err != nil {
		return err
	}
	if p == nil || status != proposalReceived {
		return m.Domain.Advance(ctx, c, "tasks:adjudicate_done", func(*durable.Tx) (durable.Transition, error) { return durable.Done(), nil })
	}
	switch p.GetKind() {
	case lernav1.ProposalKind_PROPOSAL_KIND_ACTION:
		return m.adjudicateAction(ctx, c, p, attempts+1)
	case lernav1.ProposalKind_PROPOSAL_KIND_COMPLETION:
		return m.adjudicateCompletion(ctx, c, p)
	case lernav1.ProposalKind_PROPOSAL_KIND_ASK_USER:
		return m.adjudicateAsk(ctx, c, p)
	default:
		return m.Domain.Advance(ctx, c, "tasks:adjudicate_unsupported", func(tx *durable.Tx) (durable.Transition, error) {
			t, err := loadTask(tx, c.User, p.GetTaskId())
			if err != nil {
				return durable.Transition{}, err
			}
			if err := setProposalStatus(tx, c.User, p.GetProposalId(), proposalRejected); err != nil {
				return durable.Transition{}, err
			}
			return durable.Done(), setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_USER,
				Ref: p.GetProposalId(), Gap: "推理提议修改条件：M1 只接受任务模板和显式条件列表，请用户给出新的条件"})
		})
	}
}

// coreIssuer 是任务编排自己发出命令时使用的固定服务命名空间。
const coreIssuer = "core.tasks"

func (m *Module) adjudicateAction(ctx context.Context, c *durable.Claim, p *lernav1.Proposal, attempt int) error {
	step := p.GetAction().GetSteps()[0]
	cmd := &lernav1.AdmitProposalCommand{TaskId: p.GetTaskId(), ProposalId: p.GetProposalId(), StepId: step.GetStepId(), Attempt: int32(attempt)}
	env, err := durable.NewEnvelope(&lernav1.CommandIdentity{
		UserId:         c.User,
		IssuerId:       coreIssuer,
		TargetDomainId: m.Domain.ID(),
		CommandId:      "admit:" + p.GetProposalId() + ":" + strconv.Itoa(attempt),
	}, ports.CommandAdmit, cmd)
	if err != nil {
		return err
	}
	rec, err := m.Domain.Execute(ctx, env)
	if err != nil && !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE) {
		// 结果未知或暂时故障：下次用原命令标识重入，拿回原决定。
		return err
	}
	return m.Domain.Advance(ctx, c, "tasks:adjudicated", func(tx *durable.Tx) (durable.Transition, error) {
		t, terr := loadTask(tx, c.User, p.GetTaskId())
		if terr != nil {
			return durable.Transition{}, terr
		}
		if err != nil {
			// 准入-5：有阻断性的未知效果，进入等待核对；有新事实时唤醒重新裁决。
			if werr := setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_RECONCILIATION,
				Ref: p.GetProposalId(), Gap: err.Error()}); werr != nil {
				return durable.Transition{}, werr
			}
			return durable.WaitWake("blocked by unknown effects"), nil
		}
		if _, err := tx.Exec(`UPDATE proposals SET adjudications = ? WHERE user_id = ? AND proposal_id = ?`,
			attempt, c.User, p.GetProposalId()); err != nil {
			return durable.Transition{}, err
		}
		if rec.GetDecision() == lernav1.Decision_DECISION_ACCEPTED {
			return durable.Done(), clearWaiting(tx, t, lernav1.WaitingKind_WAITING_KIND_GRANT, lernav1.WaitingKind_WAITING_KIND_BUDGET,
				lernav1.WaitingKind_WAITING_KIND_USER, lernav1.WaitingKind_WAITING_KIND_RECONCILIATION)
		}
		return m.afterRejection(tx, c, t, p, step, rec.GetRejection())
	})
}

// afterRejection 按准入拒绝的原因登记后续工作：版本过期就重新请求提议；缺授权、预算或确认就等待。
func (m *Module) afterRejection(tx *durable.Tx, c *durable.Claim, t *lernav1.Task, p *lernav1.Proposal, step *lernav1.PlanStep, rej *lernav1.Error) (durable.Transition, error) {
	switch rej.GetCode() {
	case lernav1.ErrorCode_ERROR_CODE_STALE_GENERATION, lernav1.ErrorCode_ERROR_CODE_STALE_INPUT, lernav1.ErrorCode_ERROR_CODE_STALE_REQUIREMENT:
		if err := setProposalStatus(tx, c.User, p.GetProposalId(), proposalStale); err != nil {
			return durable.Transition{}, err
		}
		if err := sessions.SupersedeForProposal(tx, c.User, p.GetProposalId()); err != nil {
			return durable.Transition{}, err
		}
		return durable.Done(), m.maybeRequestProposal(tx, t)
	case lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED, lernav1.ErrorCode_ERROR_CODE_GRANT_REVOKED:
		return durable.WaitWake("waiting for a grant"), setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_GRANT,
			Ref: p.GetProposalId(), Gap: rej.GetDiagnostic()})
	case lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED:
		return durable.WaitWake("waiting for budget"), setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_BUDGET,
			Ref: p.GetProposalId(), Gap: rej.GetDiagnostic()})
	case lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID:
		return m.awaitConfirmation(tx, t, p, step, rej)
	default:
		if err := setProposalStatus(tx, c.User, p.GetProposalId(), proposalRejected); err != nil {
			return durable.Transition{}, err
		}
		return durable.Done(), setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_EXTERNAL,
			Ref: p.GetProposalId(), Gap: "准入被拒绝：" + rej.GetDiagnostic()})
	}
}

// maybeRequestProposal 在任务可以推进、没有可裁决的提议、没有阻断性未知效果时登记新一轮提议请求。
func (m *Module) maybeRequestProposal(tx *durable.Tx, t *lernav1.Task) error {
	if t.GetLifecycle() != lernav1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.GetControl() != lernav1.TaskControl_TASK_CONTROL_ACTIVE {
		return nil
	}
	set, err := loadRequirementSet(tx, t.GetUserId(), t.GetTaskId(), t.GetRequirementsVersion())
	if err != nil {
		return err
	}
	if set.GetStatus() != lernav1.RequirementSetStatus_REQUIREMENT_SET_STATUS_ACCEPTED {
		return nil
	}
	if frozen, err := verificationFrozen(tx, t); err != nil || frozen {
		return err
	}
	var pending int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM proposal_requests WHERE user_id = ? AND task_id = ? AND status = ?`,
		t.GetUserId(), t.GetTaskId(), requestPending).Scan(&pending); err != nil {
		return err
	}
	if err := tx.QueryRow(`SELECT COUNT(*) + ? FROM proposals WHERE user_id = ? AND task_id = ? AND status = ?`,
		pending, t.GetUserId(), t.GetTaskId(), proposalReceived).Scan(&pending); err != nil {
		return err
	}
	if pending > 0 {
		return nil
	}
	ops, err := loadOperationViews(tx, t.GetUserId(), t.GetTaskId())
	if err != nil {
		return err
	}
	for _, op := range ops {
		if blocking(op) {
			return setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_RECONCILIATION,
				Ref: op.GetOperationId(), Gap: "动作 " + op.GetOperationId() + " 尚未收尾，等待执行管理的事实"})
		}
	}
	if err := clearWaiting(tx, t, lernav1.WaitingKind_WAITING_KIND_RECONCILIATION); err != nil {
		return err
	}
	return m.requestProposal(tx, t)
}

func sortedKeys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
