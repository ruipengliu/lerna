// Package egress 实现出口闸门（docs/architecture/core/egress/README.md）：动作访问外部资源的唯一受控通道。
//
// 出口闸门不持有独立事实：开始门禁（P4）由裁决域裁决，"可能已发出"（P5）和观察（P6）由执行管理
// 保存，用量交预算（P7）。它负责的是顺序：P4 开始回执 → P5 持久成立 → 才执行外部 I/O，
// 并且禁用隐式的重试、回退和并发试探——一次放行只调用适配器一次。
package egress

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/durable/fault"
)

// Gate 是出口闸门。
type Gate struct {
	// Router 把开始门禁命令送到裁决域（可信基础设施 I/O）。
	Router *durable.Router
	// Executor 按适配器标识返回受审查的参考执行适配器。
	Executor func(adapterID string) (ports.Executor, bool)
	// Timeout 是一次实际 I/O 的等待上限；超时只说明调用方不知道结果，回报为无法确定。
	Timeout time.Duration
}

// Send 描述一次物理发送。
type Send struct {
	// Issuer 是执行管理负责方：开始门禁核验它与准入一致。
	Issuer             string
	AdjudicationDomain string
	AdapterID          string
	Start              *lernav1.StartSendCommand
	Execute            *lernav1.ExecuteRequest
	Query              *lernav1.QueryRequest
}

// Hooks 是执行管理在出口路径上的持久化点。
type Hooks struct {
	// DispatchPossible 是 P5：执行管理检查派发未封闭，持久写下"可能已发出"。
	// 返回错误时出口闸门不执行任何外部 I/O。
	DispatchPossible func(ctx context.Context, startReceipt *lernav1.Receipt) error
	// Observe 是 P6：执行管理保存原始观察和回报。ioErr 非空表示没有拿到回报（效果未知）。
	Observe func(ctx context.Context, report *lernav1.ExecutionReport, ioErr error) error
}

// ErrStartRejected 表示开始门禁拒绝了这次发送：从未开放出口。
type ErrStartRejected struct {
	Rejection *lernav1.Error
}

func (e *ErrStartRejected) Error() string {
	return "egress: start gate rejected: " + errs.FromProto(e.Rejection).Error()
}

// StartID 返回一次发送的开始门禁命令身份：同一发送身份只占用一次发送额度，回执丢失时查询原发送。
func StartID(user, adjudication, issuer, attemptID string, seq int32, purpose lernav1.SendPurpose) *lernav1.CommandIdentity {
	kind := "x"
	if purpose == lernav1.SendPurpose_SEND_PURPOSE_QUERY {
		kind = "q"
	}
	return &lernav1.CommandIdentity{
		UserId:         user,
		IssuerId:       issuer,
		TargetDomainId: adjudication,
		CommandId:      "start:" + attemptID + ":" + kind + strconv.Itoa(int(seq)),
	}
}

// StartGate 执行开始门禁（P4）。回执丢失时先查询原发送，再用原标识、原内容重入。
func (g *Gate) StartGate(ctx context.Context, s Send) (*lernav1.Receipt, error) {
	id := StartID(s.Start.GetUserId(), s.AdjudicationDomain, s.Issuer, s.Start.GetAttemptId(), s.Start.GetSendSeq(), s.Start.GetPurpose())
	q, err := g.Router.Query(ctx, id)
	if err == nil && q.GetOutcome() == lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED {
		return q.GetReceipt(), nil
	}
	env, err := durable.NewEnvelope(id, ports.CommandStartSend, s.Start)
	if err != nil {
		return nil, err
	}
	rec, err := g.Router.Deliver(ctx, env)
	if err != nil && errs.IsIndeterminate(err) {
		q, qerr := g.Router.Query(ctx, id)
		if qerr == nil && q.GetOutcome() == lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_DECIDED {
			return q.GetReceipt(), nil
		}
	}
	return rec, err
}

// Call 走完一次受控出口：P4 开始门禁 → P5 持久写下"可能已发出" → 一次实际 I/O → P6 观察。
// 没有 P5 持久化成功就不得执行外部 I/O；出口闸门和适配器内部都不重发。
func (g *Gate) Call(ctx context.Context, s Send, hooks Hooks) error {
	rec, err := g.StartGate(ctx, s)
	if err != nil {
		return err
	}
	if rec.GetDecision() != lernav1.Decision_DECISION_ACCEPTED {
		return &ErrStartRejected{Rejection: rec.GetRejection()}
	}
	if err := hooks.DispatchPossible(ctx, rec); err != nil {
		return err
	}
	return g.Perform(ctx, s, hooks)
}

// Perform 执行实际 I/O 并保存观察。只能在 P5 持久成立之后调用。
func (g *Gate) Perform(ctx context.Context, s Send, hooks Hooks) error {
	exec, ok := g.Executor(s.AdapterID)
	if !ok {
		return hooks.Observe(ctx, nil, errs.New(lernav1.ErrorCode_ERROR_CODE_DEPENDENCY_UNAVAILABLE, "no executor %s", s.AdapterID))
	}
	point := "egress:" + s.Start.GetCapabilityId() + ":" + s.Start.GetPurpose().String()
	if err := fault.Hit(point + ":before_io"); err != nil {
		return hooks.Observe(ctx, nil, errs.New(lernav1.ErrorCode_ERROR_CODE_TRANSPORT_LOST, "injected loss before I/O"))
	}
	timeout := g.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ioCtx, cancel := context.WithTimeout(ctx, timeout)
	var rep *lernav1.ExecutionReport
	var ioErr error
	if s.Query != nil {
		rep, ioErr = exec.Query(ioCtx, s.Query)
	} else {
		rep, ioErr = exec.Execute(ioCtx, s.Execute)
	}
	cancel()
	if ioErr != nil && errors.Is(ioErr, context.DeadlineExceeded) {
		// context 取消只是停止信号，不证明外部动作已经结束。
		ioErr = errs.New(lernav1.ErrorCode_ERROR_CODE_DEADLINE_EXCEEDED, "egress I/O timed out: %v", ioErr)
	}
	if err := fault.Hit(point + ":after_io"); err != nil {
		return hooks.Observe(ctx, nil, errs.New(lernav1.ErrorCode_ERROR_CODE_TRANSPORT_LOST, "injected loss of the response"))
	}
	return hooks.Observe(ctx, rep, ioErr)
}
