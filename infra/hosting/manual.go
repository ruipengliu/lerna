// Package hosting 以固定阶段协调原负责方恢复，不写业务事实或增加执行权限。
package hosting

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

type SessionProgress interface {
	ProcessPending(context.Context, *v1.Caller) error
}
type ObservationProgress interface {
	ProcessObservations(context.Context, *v1.Caller) error
}
type ReportProgress interface {
	ProcessReports(context.Context, *v1.Caller) error
	ProcessInterpretations(context.Context, *v1.Caller) error
}
type RevocationProgress interface {
	ProcessRevocations(context.Context) error
}
type CompletionProgress interface {
	ProcessCompletions(context.Context, *v1.Caller) error
}
type CancellationProgress interface {
	ProcessCancellations(context.Context, *v1.Caller) error
}
type ReconciliationProgress interface {
	RecoverReconciliations(context.Context, *v1.Caller) error
	ProcessOperationProgress(context.Context, *v1.Caller) error
}
type TaskClosingProgress interface {
	ProcessTaskClosings(context.Context, *v1.Caller) error
}
type BudgetClosureProgress interface {
	ProcessClosures(context.Context) error
}
type ExecutionFollowupProgress interface {
	ProcessExecutionFollowups(context.Context, *v1.Caller) error
}
type SettlementFollowupProgress interface {
	ProcessSettlementFollowups(context.Context, *v1.Caller) error
}
type TraceProgress interface {
	Recover(context.Context, *v1.Caller) error
}

// ManualDependencies 中 Sessions 必需，其余 nil 明确表示该手动能力缺席。
type ManualDependencies struct {
	Sessions            SessionProgress
	Observations        ObservationProgress
	Reports             ReportProgress
	Revocations         RevocationProgress
	Completions         CompletionProgress
	Cancellations       CancellationProgress
	Reconciliations     ReconciliationProgress
	TaskClosings        TaskClosingProgress
	BudgetClosures      BudgetClosureProgress
	ExecutionFollowups  ExecutionFollowupProgress
	SettlementFollowups SettlementFollowupProgress
	Trace               TraceProgress
}

type Service struct {
	manual  ManualDependencies
	startup *StartupDependencies
}

func NewManual(dependencies ManualDependencies) (*Service, error) {
	s := &Service{manual: dependencies}
	if e := s.ValidateDependencies(); e != nil {
		return nil, e
	}
	return s, nil
}

// ValidateDependencies 保留可选能力缺席，拒绝已经声明却为 typed nil 的端口。
func (s *Service) ValidateDependencies() error {
	d := s.manual
	required := []durable.Dependency{{Name: "sessions", Value: d.Sessions}}
	for _, dependency := range []durable.Dependency{
		{Name: "observations", Value: d.Observations},
		{Name: "reports", Value: d.Reports},
		{Name: "revocations", Value: d.Revocations},
		{Name: "completions", Value: d.Completions},
		{Name: "cancellations", Value: d.Cancellations},
		{Name: "reconciliations", Value: d.Reconciliations},
		{Name: "taskClosings", Value: d.TaskClosings},
		{Name: "budgetClosures", Value: d.BudgetClosures},
		{Name: "executionFollowups", Value: d.ExecutionFollowups},
		{Name: "settlementFollowups", Value: d.SettlementFollowups},
		{Name: "trace", Value: d.Trace},
	} {
		if dependency.Value != nil {
			required = append(required, dependency)
		}
	}
	if err := durable.RequireDependencies("hosting", required...); err != nil {
		return err
	}
	if s.startup != nil {
		return s.ValidateStartupDependencies()
	}
	return nil
}

// ManualProgress 原样传递 context/caller，只执行原 CLI 支持的固定阶段。
func (s *Service) ManualProgress(ctx context.Context, caller *v1.Caller) error {
	d := s.manual
	if e := d.Sessions.ProcessPending(ctx, caller); e != nil {
		return e
	}
	if d.Observations != nil {
		if e := d.Observations.ProcessObservations(ctx, caller); e != nil {
			return e
		}
	}
	if d.Reports != nil {
		if e := d.Reports.ProcessReports(ctx, caller); e != nil {
			return e
		}
		if e := d.Reports.ProcessInterpretations(ctx, caller); e != nil {
			return e
		}
	}
	if d.Revocations != nil {
		if e := d.Revocations.ProcessRevocations(ctx); e != nil {
			return e
		}
	}
	if d.Completions != nil {
		if e := d.Completions.ProcessCompletions(ctx, caller); e != nil {
			return e
		}
	}
	if d.Cancellations != nil {
		if e := d.Cancellations.ProcessCancellations(ctx, caller); e != nil {
			return e
		}
	}
	if d.Reconciliations != nil {
		if e := d.Reconciliations.RecoverReconciliations(ctx, caller); e != nil {
			return e
		}
		if e := d.Reconciliations.ProcessOperationProgress(ctx, caller); e != nil {
			return e
		}
	}
	if d.TaskClosings != nil {
		if e := d.TaskClosings.ProcessTaskClosings(ctx, caller); e != nil {
			return e
		}
	}
	if d.BudgetClosures != nil {
		if e := d.BudgetClosures.ProcessClosures(ctx); e != nil {
			return e
		}
	}
	if d.ExecutionFollowups != nil {
		if e := d.ExecutionFollowups.ProcessExecutionFollowups(ctx, caller); e != nil {
			return e
		}
	}
	if d.SettlementFollowups != nil {
		if e := d.SettlementFollowups.ProcessSettlementFollowups(ctx, caller); e != nil {
			return e
		}
	}
	if d.Trace != nil {
		return d.Trace.Recover(ctx, caller)
	}
	return nil
}
