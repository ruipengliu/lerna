package tasks

import (
	"context"
	"errors"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ids"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/sessions"
)

// modelCaller 返回推理使用的模型调用请求接口。
func (m *Module) modelCaller(string, *requestRow) ports.ModelCaller { return noModels{} }

type noModels struct{}

func (noModels) Call(context.Context, int32, *lernav1.ModelCallDescription) (*lernav1.ModelCallResult, error) {
	return nil, errors.New("model calls are not available")
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

// onCoreWorkUpdate 处理核心安排的工作（模型调用、核对查询）的进展。
func (m *Module) onCoreWorkUpdate(*durable.Tx, *lernav1.Task, *lernav1.OperationView, *lernav1.OperationUpdate) error {
	return nil
}

// afterClose 在任务关闭的事务中关闭任务预算：不再接受新消耗，但仍接受已发生的费用。
func (m *Module) afterClose(tx *durable.Tx, t *lernav1.Task) error {
	return budget.CloseTaskBudget(tx, t.GetUserId(), t.GetTaskId())
}
