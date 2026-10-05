package tasks

import (
	"crypto/sha256"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/types/known/timestamppb"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/sessions"
)

// admissionFingerprint 是动作确认绑定的事项摘要：事项类型、任务、能力、参数摘要、条件集版本和输入版本。
// 参数、条件或主体有任何改变，摘要就不同，旧确认随之失效。
func admissionFingerprint(t *lernav1.Task, capability string, args map[string]string) []byte {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00%s\x00%s\x00%x\x00%d\x00%d", lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_OPERATION_ADMISSION,
		t.GetTaskId(), capability, RequestDigest(capability, args), t.GetRequirementsVersion(), t.GetInputVersion())
	return h.Sum(nil)
}

// describeAction 生成确认展示给用户的动作描述：用户确认的就是实际要执行的。
func describeAction(t *lernav1.Task, capability string, args map[string]string) string {
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, k+"="+strconv.Quote(args[k]))
	}
	return fmt.Sprintf("在任务 %s 中调用 %s（%s），依据条件集 v%d、输入版本 %d",
		t.GetTaskId(), capability, strings.Join(parts, ", "), t.GetRequirementsVersion(), t.GetInputVersion())
}

// occupyGrant 是准入-7 和准入-9：占用一份覆盖动作的当前授权；没有持续授权覆盖时，
// 在同一事务中消费用户对这个具体动作的确认，签发与动作原子绑定的单次授权并占用它。
func (m *Module) occupyGrant(tx *durable.Tx, t *lernav1.Task, s admitSpec, opID, admissionID string, digest []byte) (grantID, useID, confirmationRef string, err error) {
	req := grants.UseRequest{
		User: t.GetUserId(), TaskID: t.GetTaskId(), OperationID: opID, AdmissionID: admissionID,
		Resource: s.capability, Action: grants.ActionInvoke, Params: s.args, RequestDigest: digest,
	}
	grantID, useID, err = grants.OccupyForAdmission(tx, req)
	if err == nil || !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_PERMISSION_DENIED) || s.proposalID == "" {
		return grantID, useID, "", err
	}
	fp := admissionFingerprint(t, s.capability, s.args)
	conf, err := sessions.ConsumeForAdmission(tx, t.GetUserId(), s.proposalID, s.stepID, fp, admissionID)
	if err != nil {
		return "", "", "", err
	}
	if conf == nil {
		return "", "", "", errs.New(lernav1.ErrorCode_ERROR_CODE_CONFIRMATION_INVALID, "需要用户确认：%s", describeAction(t, s.capability, s.args))
	}
	// 单次授权：精确绑定这个动作的能力、参数和任务，次数上限为 1。
	g := &lernav1.Grant{
		UserId:     t.GetUserId(),
		GrantId:    ids.New(),
		SubjectRef: "task-runner/" + t.GetUserId(),
		Clauses: []*lernav1.GrantClause{{
			Resource:           s.capability,
			Actions:            []string{grants.ActionInvoke, grants.ActionQuery},
			UseRights:          []lernav1.UseRight{lernav1.UseRight_USE_RIGHT_ACT, lernav1.UseRight_USE_RIGHT_READ},
			ProcessingPurposes: []lernav1.ProcessingPurpose{lernav1.ProcessingPurpose_PROCESSING_PURPOSE_CURRENT_TASK},
			ParamEquals:        s.args,
			TaskId:             t.GetTaskId(),
		}},
		SemanticVersion: grants.SemanticVersion,
		ValidFrom:       timestamppb.New(tx.Now()),
		ValidUntil:      timestamppb.New(tx.Now().Add(30 * 24 * time.Hour)),
		UseMode:         lernav1.UseMode_USE_MODE_SINGLE,
		MaxAdmissions:   1,
		Status:          lernav1.GrantStatus_GRANT_STATUS_ACTIVE,
		StateVersion:    1,
		IssuerRef:       "confirmation/" + conf.GetConfirmationId(),
	}
	g.UsePoolId = g.GetGrantId()
	if err := grants.Insert(tx, g); err != nil {
		return "", "", "", err
	}
	req.GrantID = g.GetGrantId()
	grantID, useID, err = grants.OccupyForAdmission(tx, req)
	return grantID, useID, conf.GetConfirmationId(), err
}

// awaitConfirmation 在准入因缺少确认被拒绝后，由核心生成动作描述并发布确认事项，任务等待用户。
func (m *Module) awaitConfirmation(tx *durable.Tx, t *lernav1.Task, p *lernav1.Proposal, step *lernav1.PlanStep, rej *lernav1.Error) (durable.Transition, error) {
	existing, err := sessions.PendingForProposal(tx, t.GetUserId(), p.GetProposalId(), step.GetStepId())
	if err != nil {
		return durable.Transition{}, err
	}
	confID := ""
	if existing != nil && existing.GetStatus() == lernav1.ConfirmationStatus_CONFIRMATION_STATUS_PENDING {
		confID = existing.GetConfirmationId()
	} else if existing == nil {
		confID = ids.New()
		if err := sessions.CreateConfirmation(tx, &lernav1.Confirmation{
			UserId:              t.GetUserId(),
			ConfirmationId:      confID,
			SessionId:           t.GetSessionId(),
			SubjectKind:         lernav1.ConfirmationSubjectKind_CONFIRMATION_SUBJECT_KIND_OPERATION_ADMISSION,
			TaskId:              t.GetTaskId(),
			Description:         describeAction(t, step.GetCapabilityId(), step.GetArguments()),
			RequirementsVersion: t.GetRequirementsVersion(),
			InputVersion:        t.GetInputVersion(),
			IntentFingerprint:   admissionFingerprint(t, step.GetCapabilityId(), step.GetArguments()),
			ProposalId:          p.GetProposalId(),
			StepId:              step.GetStepId(),
		}); err != nil {
			return durable.Transition{}, err
		}
	} else {
		// 已批准但这次准入仍未消费：说明绑定不再一致（例如版本已变），不复用旧确认。
		return durable.WaitWake("confirmation no longer matches"), setWaiting(tx, t, &lernav1.WaitingOn{
			Kind: lernav1.WaitingKind_WAITING_KIND_USER, Ref: existing.GetConfirmationId(), Gap: rej.GetDiagnostic()})
	}
	return durable.WaitWake("waiting for confirmation"), setWaiting(tx, t, &lernav1.WaitingOn{
		Kind: lernav1.WaitingKind_WAITING_KIND_USER, Ref: confID, Gap: "等待用户确认：" + describeAction(t, step.GetCapabilityId(), step.GetArguments())})
}

// ConfirmationResponded 实现 sessions.TaskPort：批准后唤醒原提议的裁决（用新的准入命令标识）；
// 拒绝后这个提议不再执行，任务等待用户给出新的方向。
func (m *Module) ConfirmationResponded(tx *durable.Tx, c *lernav1.Confirmation) error {
	t, err := loadTask(tx, c.GetUserId(), c.GetTaskId())
	if err != nil || t == nil {
		return err
	}
	if c.GetStatus() == lernav1.ConfirmationStatus_CONFIRMATION_STATUS_APPROVED {
		if err := clearWaiting(tx, t, lernav1.WaitingKind_WAITING_KIND_USER); err != nil {
			return err
		}
		return tx.WakeJob(c.GetUserId(), "adjudicate:"+c.GetProposalId())
	}
	if err := setProposalStatus(tx, c.GetUserId(), c.GetProposalId(), proposalRejected); err != nil {
		return err
	}
	if err := tx.SealJob(c.GetUserId(), "adjudicate:"+c.GetProposalId()); err != nil {
		return err
	}
	if err := clearWaiting(tx, t, lernav1.WaitingKind_WAITING_KIND_USER); err != nil {
		return err
	}
	return setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_USER, Ref: c.GetConfirmationId(),
		Gap: "用户拒绝了动作：" + c.GetDescription()})
}
