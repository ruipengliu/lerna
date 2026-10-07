package hosting

import (
	"context"
	"errors"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

type StartupCompatibility interface {
	CheckStartupCompatibility(context.Context) error
}
type SessionRecovery interface {
	RecoverPending(context.Context, *v1.Caller) error
}
type HandoffRecovery interface {
	RecoverHandoffs(context.Context, *v1.Caller) error
}
type RegistrationProgress interface {
	ProcessRegistrations(context.Context, *v1.Caller) error
}
type CancellationRecovery interface {
	RecoverCancellations(context.Context, *v1.Caller) error
}
type CompletionRecovery interface {
	RecoverCompletions(context.Context, *v1.Caller) error
}
type TaskClosureRecovery interface {
	RecoverTaskClosures(context.Context, *v1.Caller) error
}
type ReasonerDriverRecovery interface {
	RecoverReasonerDrivers(context.Context, *v1.Caller) error
}

// StartupDependencies 固定启动用户与全部必需端口，不继承手动模式的可选配置。
type StartupDependencies struct {
	User                string
	TasksCompatibility  StartupCompatibility
	LedgerCompatibility StartupCompatibility
	Sessions            SessionRecovery
	Handoffs            HandoffRecovery
	Registrations       RegistrationProgress
	Observations        ObservationProgress
	Reports             ReportProgress
	Revocations         RevocationProgress
	Cancellations       CancellationRecovery
	Completions         CompletionRecovery
	Reconciliations     ReconciliationProgress
	TaskClosures        TaskClosureRecovery
	TaskClosings        TaskClosingProgress
	BudgetClosures      BudgetClosureProgress
	ExecutionFollowups  ExecutionFollowupProgress
	SettlementFollowups SettlementFollowupProgress
	Drivers             ReasonerDriverRecovery
	Trace               TraceProgress
}

// New 构造两个模式均已完成配置的宿主；没有宿主到核心的反向循环。
func New(manual ManualDependencies, startup StartupDependencies) (*Service, error) {
	s := &Service{manual: manual, startup: &startup}
	if err := s.ValidateDependencies(); err != nil {
		return nil, err
	}
	return s, nil
}

// ValidateStartupDependencies 供生产完成门禁拒绝 Manual-only 实例及启动缺项。
func (s *Service) ValidateStartupDependencies() error {
	if err := durable.RequireDependencies("hosting", durable.Dependency{Name: "startup", Value: s.startup}); err != nil {
		return err
	}
	d := s.startup
	if d.User == "" {
		return errors.New("missing required dependency: hosting.startup.user")
	}
	return durable.RequireDependencies("hosting.startup",
		durable.Dependency{Name: "tasksCompatibility", Value: d.TasksCompatibility},
		durable.Dependency{Name: "ledgerCompatibility", Value: d.LedgerCompatibility},
		durable.Dependency{Name: "sessions", Value: d.Sessions},
		durable.Dependency{Name: "handoffs", Value: d.Handoffs},
		durable.Dependency{Name: "registrations", Value: d.Registrations},
		durable.Dependency{Name: "observations", Value: d.Observations},
		durable.Dependency{Name: "reports", Value: d.Reports},
		durable.Dependency{Name: "revocations", Value: d.Revocations},
		durable.Dependency{Name: "cancellations", Value: d.Cancellations},
		durable.Dependency{Name: "completions", Value: d.Completions},
		durable.Dependency{Name: "reconciliations", Value: d.Reconciliations},
		durable.Dependency{Name: "taskClosures", Value: d.TaskClosures},
		durable.Dependency{Name: "taskClosings", Value: d.TaskClosings},
		durable.Dependency{Name: "budgetClosures", Value: d.BudgetClosures},
		durable.Dependency{Name: "executionFollowups", Value: d.ExecutionFollowups},
		durable.Dependency{Name: "settlementFollowups", Value: d.SettlementFollowups},
		durable.Dependency{Name: "drivers", Value: d.Drivers},
		durable.Dependency{Name: "trace", Value: d.Trace},
	)
}

// Startup 只推进完整配置的原启动责任；期限和自然等待沿用调用方及原负责方。
func (s *Service) Startup(ctx context.Context) error {
	if err := s.ValidateStartupDependencies(); err != nil {
		return err
	}
	d := s.startup
	// 固定受信宿主身份仅驱动已保存的责任，不替换原命令身份。
	if err := d.TasksCompatibility.CheckStartupCompatibility(ctx); err != nil {
		return err
	}
	if err := d.LedgerCompatibility.CheckStartupCompatibility(ctx); err != nil {
		return err
	}
	if err := d.Sessions.RecoverPending(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Handoffs.RecoverHandoffs(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Registrations.ProcessRegistrations(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Observations.ProcessObservations(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Reports.ProcessReports(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Reports.ProcessInterpretations(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Revocations.ProcessRevocations(ctx); err != nil {
		return err
	}
	if err := d.Cancellations.RecoverCancellations(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Completions.RecoverCompletions(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Reconciliations.ProcessOperationProgress(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Reconciliations.RecoverReconciliations(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Reconciliations.ProcessOperationProgress(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.TaskClosures.RecoverTaskClosures(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.TaskClosings.ProcessTaskClosings(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.BudgetClosures.ProcessClosures(ctx); err != nil {
		return err
	}
	if err := d.ExecutionFollowups.ProcessExecutionFollowups(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.SettlementFollowups.ProcessSettlementFollowups(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	if err := d.Drivers.RecoverReasonerDrivers(ctx, &v1.Caller{UserId: d.User, IssuerId: "host"}); err != nil {
		return err
	}
	if err := d.Trace.Recover(ctx, &v1.Caller{UserId: d.User, IssuerId: "host-recovery"}); err != nil {
		return err
	}
	return nil
}
