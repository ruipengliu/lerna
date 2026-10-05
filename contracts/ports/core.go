// Package ports 定义可替换部分与核心之间的公共接口（核心契约 1：可替换部分的接口）。
//
// 这些接口属于公共契约，进入 SDK；事务上下文、存储访问等内部接口不在这里。
package ports

import (
	"context"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 公共命令类型。命令正文的结构见 contracts/proto/lerna/v1/commands.proto。
const (
	CommandCreateSession = "sessions.create_session"
	CommandSubmitInput   = "sessions.submit_input"
	CommandIssueGrant    = "grants.issue_grant"
	CommandRevokeGrant   = "grants.revoke_grant"
	CommandRequestGrant  = "grants.request_grant"
	CommandSetBudget     = "budget.set_budget"
)

// 核心模块之间的跨域命令（R7 交接）。它们是跨进程命令，属于公共契约；
// 只能由核心的固定服务命名空间发出，不承载扩展发起的请求。
const (
	// CommandAdmit 是任务编排对提议中一步的准入（裁决域内部命令）。
	CommandAdmit = "tasks.admit"
	// CommandAcceptIntent 把已准入的动作意图交给执行管理（持久点 6）。
	CommandAcceptIntent = "ledger.accept_intent"
	// CommandOperationUpdate 是执行管理向任务编排的变化通知。
	CommandOperationUpdate = "tasks.operation_update"
	// CommandStartSend 是出口 P4 的开始门禁（裁决域）。
	CommandStartSend = "tasks.start_send"
	// CommandReportUsage 是出口 P7 的用量回报（预算）。
	CommandReportUsage = "budget.report_usage"
	// CommandSealDispatch 是封闭尚未开始的派发（执行管理）。
	CommandSealDispatch = "ledger.seal_dispatch"
)

// ContractVersion 是 M1 运行的唯一契约版本（核心契约 9：单版本运行）。
const ContractVersion = "lerna.v1"

// Core 是交互适配器看到的核心：只通过公共契约命令和查询交互。
type Core interface {
	// Submit 提交一条命令。成功返回原决定回执；超时或断线时返回"结果未知"，
	// 调用方必须用原命令身份查询，不得换新标识重试。
	Submit(ctx context.Context, env *lernav1.CommandEnvelope) (*lernav1.Receipt, error)
	// QueryCommand 用原命令身份查询回执，返回核心契约 3.1 的五种结果之一。
	QueryCommand(ctx context.Context, id *lernav1.CommandIdentity) (*lernav1.ReceiptQueryResponse, error)
	// Session 返回会话的全量快照。
	Session(ctx context.Context, userID, sessionID string) (*lernav1.SessionView, error)
	// Task 返回任务的全量快照和当前阶段。
	Task(ctx context.Context, userID, taskID string) (*lernav1.TaskView, error)
	// Grant 返回授权及撤销进度（被接纳 ≠ 完全生效）。
	Grant(ctx context.Context, userID, grantID string) (*lernav1.GrantView, error)
	// Budget 返回用户和任务的预算、未知或未结清的费用。
	Budget(ctx context.Context, userID string) (*lernav1.BudgetView, error)
}
