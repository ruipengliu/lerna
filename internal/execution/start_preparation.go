package execution

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
)

// StartGatePreparer 为原 prepared Attempt 取得本次有限当前证明。
// 它只在 Tx 外准备门禁上下文，不改变原输入、授权使用、窗口或预算，
// 也不执行目标动作。恢复已有 PreparedAuthority 时仍须重新准备当前门禁；
// 已发送、未知效果和终态责任不走此正向入口。
type StartGatePreparer interface {
	PrepareStartGate(context.Context, rt.Scope, StartRequest) (context.Context, error)
}

func (s *Service) prepareStartGate(ctx context.Context, st rt.Store, sc rt.Scope, w rt.Work, op operationRecord, attempt Attempt) (context.Context, error) {
	preparer, configured := s.cfg.Authority.(StartGatePreparer)
	if !configured {
		return ctx, nil
	}
	if err := st.CheckClaim(ctx, sc, w.Claim); err != nil {
		return nil, err
	}
	prepared, err := preparer.PrepareStartGate(ctx, sc, StartRequest{ControlWindow: attempt.ControlWindow, Invoke: op.Invoke, Intent: *op.Intent, AttemptID: attempt.AttemptID, Auth: op.Principal})
	if err != nil {
		return nil, err
	}
	if prepared == nil {
		return nil, api.E("dependency_unavailable", "current_start_context_unavailable")
	}
	if err = st.CheckClaim(prepared, sc, w.Claim); err != nil {
		return nil, err
	}
	return prepared, nil
}
