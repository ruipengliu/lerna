//go:build fault

package sqlite

import (
	"context"
	"fmt"
	"os"
	"sync/atomic"
)

// FaultMode 仅在故障构建中存在；生产宿主没有配置入口。
type FaultMode string

const (
	CrashBeforeCommit FaultMode = "crash-before-commit"
	CrashAfterCommit  FaultMode = "crash-after-commit"
	LoseReceipt       FaultMode = "lose-receipt"
	// CrashExitCode 区分命中故障点与子进程测试本身失败。
	CrashExitCode = 86
)

// FaultPoints 是持久化点的统一登记表；新增事务必须在此登记。
func FaultPoints() []string {
	return []string{"ledger.cancellation_seal", "tasks.cancellation_receipt", "content.file_root", "tasks.file_use", "content.file_resources", "trace.index", "trace.source_ack", "budget.limit", "budget.import", "budget.release", "content.derivation_takeover", "content.derivation", "content.derivation_input", "content.derivation_seal", "content.derivation_commit", "content.register", "body.accept", "content.publish", "tasks.completion_receipt", "ledger.completion_seal", "tasks.verification", "tasks.completion", "tasks.closing", "tasks.close_final", "tasks.task_closure_receipt", "ledger.task_closure_seal", "ledger.followup_completion", "budget.followup_completion", "tasks.confirmation", "grants.confirmation", "grants.revoke", "grants.revocation_receipt", "sessions.confirmation", "sessions.input", "tasks.input", "content.stage", "ledger.grant_closure", "ledger.interpret", "budget.usage", "trace.accept", "ledger.usage_ack", "ledger.trace_ack", "content.observation", "ledger.observation", "content.observation_ack", "durable.submit", "durable.decide", "durable.jobs", "tasks.planning", "tasks.model_prepare", "tasks.model_seal", "tasks.model_admit", "tasks.model_result", "tasks.proposal_outcome", "tasks.proposal_stop", "grants.configure", "grants.credential", "budget.configure", "tasks.admit", "tasks.start", "ledger.accept", "ledger.prepare", "ledger.resend", "ledger.dispatch", "tasks.handoff_receipt", "ledger.reconcile_confirmation", "tasks.closure_confirmation", "ledger.reconcile_pause", "ledger.progress_ack", "tasks.operation_progress", "ledger.reconcile_control", "ledger.reconcile_request", "ledger.reconcile_prepare", "ledger.reconcile_admission_ack", "tasks.closure_admit"}

}
func registered(point string) bool {
	for _, p := range FaultPoints() {
		if p == point {
			return true
		}
	}
	return false
}

type faultKey struct{}
type faultPlan struct {
	point      string
	mode       FaultMode
	fired      atomic.Bool
	occurrence uint64
	seen       atomic.Uint64
}

// WithFault 返回一次性、限于本次调用链的故障计划，不使用全局配置。
func WithFault(ctx context.Context, point string, mode FaultMode) (context.Context, error) {
	return WithFaultOnOccurrence(ctx, point, mode, 1)
}

// WithFaultOnOccurrence 只计算与模式匹配的提交阶段，选择第 occurrence 次命中。
func WithFaultOnOccurrence(ctx context.Context, point string, mode FaultMode, occurrence uint64) (context.Context, error) {
	if occurrence == 0 {
		return nil, fmt.Errorf("fault occurrence must be positive")
	}
	if !registered(point) {
		return nil, fmt.Errorf("unregistered persistence point: %s", point)
	}
	if mode != CrashBeforeCommit && mode != CrashAfterCommit && mode != LoseReceipt {
		return nil, fmt.Errorf("unknown fault mode: %s", mode)
	}
	return context.WithValue(ctx, faultKey{}, &faultPlan{point: point, mode: mode, occurrence: occurrence}), nil
}

// FaultTriggered 报告本调用链的故障是否实际触发。
func FaultTriggered(ctx context.Context) bool {
	p, ok := ctx.Value(faultKey{}).(*faultPlan)
	return ok && p.fired.Load()
}

func persistenceBoundary(ctx context.Context, point string, committed bool) error {
	if !registered(point) {
		return fmt.Errorf("unregistered persistence point: %s", point)
	}
	p, ok := ctx.Value(faultKey{}).(*faultPlan)
	if !ok || p.point != point {
		return nil
	}
	matches := p.mode == CrashBeforeCommit && !committed || (p.mode == CrashAfterCommit || p.mode == LoseReceipt) && committed
	if !matches || p.seen.Add(1) != p.occurrence || !p.fired.CompareAndSwap(false, true) {
		return nil
	}
	if p.mode == LoseReceipt {
		return storageError(fmt.Errorf("injected lost receipt"), true)
	}
	if p.mode == CrashBeforeCommit || p.mode == CrashAfterCommit {
		os.Exit(CrashExitCode)
	}
	return nil
}
