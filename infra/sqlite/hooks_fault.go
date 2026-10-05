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
	return []string{"budget.limit", "budget.import", "budget.release", "content.derivation_takeover", "content.derivation", "content.derivation_input", "content.derivation_seal", "content.derivation_commit", "content.register", "body.accept", "content.publish", "tasks.completion_receipt", "ledger.completion_seal", "tasks.verification", "tasks.completion", "tasks.confirmation", "grants.confirmation", "grants.revoke", "grants.revocation_receipt", "sessions.confirmation", "sessions.input", "tasks.input", "content.stage", "ledger.grant_closure", "ledger.interpret", "budget.usage", "trace.accept", "ledger.usage_ack", "ledger.trace_ack", "content.observation", "ledger.observation", "content.observation_ack", "durable.submit", "durable.decide", "durable.jobs", "tasks.planning", "tasks.model_prepare", "tasks.model_seal", "tasks.model_admit", "tasks.model_result", "tasks.proposal_outcome", "tasks.proposal_stop", "grants.configure", "grants.credential", "budget.configure", "tasks.admit", "tasks.start", "ledger.accept", "ledger.prepare", "ledger.resend", "ledger.dispatch", "tasks.handoff_receipt", "ledger.reconcile_confirmation", "tasks.closure_confirmation", "ledger.reconcile_pause", "ledger.progress_ack", "tasks.operation_progress", "ledger.reconcile_control", "ledger.reconcile_request", "ledger.reconcile_prepare", "ledger.reconcile_admission_ack", "tasks.closure_admit"}

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
	point string
	mode  FaultMode
	fired atomic.Bool
}

// WithFault 返回一次性、限于本次调用链的故障计划，不使用全局配置。
func WithFault(ctx context.Context, point string, mode FaultMode) (context.Context, error) {
	if !registered(point) {
		return nil, fmt.Errorf("unregistered persistence point: %s", point)
	}
	if mode != CrashBeforeCommit && mode != CrashAfterCommit && mode != LoseReceipt {
		return nil, fmt.Errorf("unknown fault mode: %s", mode)
	}
	return context.WithValue(ctx, faultKey{}, &faultPlan{point: point, mode: mode}), nil
}
func persistenceBoundary(ctx context.Context, point string, committed bool) error {
	if !registered(point) {
		return fmt.Errorf("unregistered persistence point: %s", point)
	}
	p, ok := ctx.Value(faultKey{}).(*faultPlan)
	if !ok || p.point != point {
		return nil
	}
	if p.mode == LoseReceipt && committed && p.fired.CompareAndSwap(false, true) {
		return storageError(fmt.Errorf("injected lost receipt"), true)
	}
	if (p.mode == CrashBeforeCommit && !committed || p.mode == CrashAfterCommit && committed) && p.fired.CompareAndSwap(false, true) {
		os.Exit(CrashExitCode)
	}
	return nil
}
