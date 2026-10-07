package assembly

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// start 沿同一生产入口核验并恢复原责任，调用方持有启动期限。
func (h *Harness) start(ctx context.Context) error {
	if err := h.complete(); err != nil {
		return err
	}
	// 固定受信宿主身份仅驱动已保存的责任，不替换原命令身份。
	if err := h.Tasks.CheckStartupCompatibility(ctx); err != nil {
		return err
	}
	if err := h.Ledger.CheckStartupCompatibility(ctx); err != nil {
		return err
	}
	if err := h.Sessions.RecoverPending(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Tasks.RecoverHandoffs(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Content.ProcessRegistrations(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Content.ProcessObservations(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Ledger.ProcessReports(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Ledger.ProcessInterpretations(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Grants.ProcessRevocations(ctx); err != nil {
		return err
	}
	if err := h.Tasks.RecoverCancellations(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Tasks.RecoverCompletions(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Ledger.ProcessOperationProgress(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Ledger.RecoverReconciliations(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Ledger.ProcessOperationProgress(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Tasks.RecoverTaskClosures(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Tasks.ProcessTaskClosings(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Budget.ProcessClosures(ctx); err != nil {
		return err
	}
	if err := h.Ledger.ProcessExecutionFollowups(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Budget.ProcessSettlementFollowups(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := h.Tasks.RecoverReasonerDrivers(ctx, &v1.Caller{UserId: h.user, IssuerId: "host"}); err != nil {
		return err
	}
	if err := h.Trace.Recover(ctx, &v1.Caller{UserId: h.user, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	return nil
}
