package tasks

import (
	"context"
	"errors"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/sessions"
)

// occupyGrant 是准入-7（以及需要时的准入-9）：占用一份覆盖动作的当前授权。
func (m *Module) occupyGrant(tx *durable.Tx, t *lernav1.Task, s admitSpec, opID, admissionID string, digest []byte) (grantID, useID, confirmationRef string, err error) {
	grantID, useID, err = grants.OccupyForAdmission(tx, grants.UseRequest{
		User: t.GetUserId(), TaskID: t.GetTaskId(), OperationID: opID, AdmissionID: admissionID,
		Resource: s.capability, Action: grants.ActionInvoke, Params: s.args, RequestDigest: digest,
	})
	return grantID, useID, "", err
}

// modelCaller 返回推理使用的模型调用请求接口。
func (m *Module) modelCaller(string, *requestRow) ports.ModelCaller { return noModels{} }

type noModels struct{}

func (noModels) Call(context.Context, int32, *lernav1.ModelCallDescription) (*lernav1.ModelCallResult, error) {
	return nil, errors.New("model calls are not available")
}

// adjudicateCompletion 处理完成提议。
func (m *Module) adjudicateCompletion(ctx context.Context, c *durable.Claim, p *lernav1.Proposal) error {
	return m.Domain.Advance(ctx, c, "tasks:completion_pending", func(*durable.Tx) (durable.Transition, error) {
		return durable.WaitWake("completion gate not available"), nil
	})
}

// adjudicateAsk 处理向用户提问的提议：会话持久建立输入请求，任务等待用户。
func (m *Module) adjudicateAsk(ctx context.Context, c *durable.Claim, p *lernav1.Proposal) error {
	return m.Domain.Advance(ctx, c, "tasks:ask_user", func(tx *durable.Tx) (durable.Transition, error) {
		t, err := loadTask(tx, c.User, p.GetTaskId())
		if err != nil {
			return durable.Transition{}, err
		}
		reqID := ids.New()
		if err := sessions.OpenRequest(tx, &lernav1.InputRequest{
			UserId: c.User, RequestId: reqID, SessionId: t.GetSessionId(), TaskId: t.GetTaskId(),
			Question: p.GetAskUser().GetQuestion(), ChangesBasis: p.GetAskUser().GetChangesBasis(),
		}); err != nil {
			return durable.Transition{}, err
		}
		if err := setProposalStatus(tx, c.User, p.GetProposalId(), proposalHandled); err != nil {
			return durable.Transition{}, err
		}
		return durable.Done(), setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_USER, Ref: reqID,
			Gap: "等待用户回答：" + p.GetAskUser().GetQuestion()})
	})
}

// awaitConfirmation 在需要用户确认时登记等待。
func (m *Module) awaitConfirmation(tx *durable.Tx, t *lernav1.Task, p *lernav1.Proposal, _ *lernav1.PlanStep, rej *lernav1.Error) (durable.Transition, error) {
	return durable.WaitWake("waiting for confirmation"), setWaiting(tx, t, &lernav1.WaitingOn{Kind: lernav1.WaitingKind_WAITING_KIND_USER,
		Ref: p.GetProposalId(), Gap: rej.GetDiagnostic()})
}

// verificationFrozen 报告是否有完成核验轮次持有目标推进冻结。
func verificationFrozen(*durable.Tx, *lernav1.Task) (bool, error) { return false, nil }
