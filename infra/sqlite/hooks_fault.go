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
	return []string{"content.stage", "durable.submit", "durable.decide", "durable.jobs", "tasks.planning", "grants.configure", "grants.credential", "budget.configure", "tasks.admit", "ledger.accept", "tasks.handoff_receipt"}
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
