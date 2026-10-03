package task

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 只规划不需要 api.Job 返回值的 Task 作业意图。真正 Raise 在本轮领域事实之后。
// 不覆盖 Tx.Raise、不制造 Job/WorkRevision；BillingTx 仍直接取得真实作业。
type taskJobIntent struct {
	kind, key string
	source    api.ObjectRef
	at        time.Time
}
type taskJobsTx struct {
	runtime.Tx
	intents []taskJobIntent
}

const maxTaskJobIntents = 999*257*2 + 32

// JobIntentPlanner 供同库签封端口把必需出版责任排在 Task 领域事实之后。
// 它只接纳显式参与的纯事务内意图；不返回虚构的 Job 或 WorkRevision。
type JobIntentPlanner interface {
	AddJobIntent(context.Context, string, string, api.ObjectRef, time.Time) error
}

func (tx *taskJobsTx) AddJobIntent(_ context.Context, kind, key string, source api.ObjectRef, at time.Time) error {
	if len(tx.intents) >= maxTaskJobIntents {
		return api.E("overloaded", "task_job_intent_capacity")
	}
	tx.intents = append(tx.intents, taskJobIntent{kind, key, source, at})
	return nil
}

func (tx *taskJobsTx) Peek(ctx context.Context, ns, id string, out any) (uint64, error) {
	reader, ok := tx.Tx.(runtime.TxSnapshotReader)
	if !ok {
		return 0, api.E("unsupported", "route_snapshot_unconfigured")
	}
	return reader.Peek(ctx, ns, id, out)
}

func (tx *taskJobsTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	var nested *taskJobsTx
	err := tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		nested = &taskJobsTx{Tx: inner}
		if err := fn(nested); err != nil {
			return err
		}
		if len(tx.intents)+len(nested.intents) > maxTaskJobIntents {
			return api.E("overloaded", "task_job_intent_capacity")
		}
		return nil
	})
	if err == nil {
		tx.intents = append(tx.intents, nested.intents...)
	}
	return err
}

func queueJob(ctx context.Context, tx runtime.Tx, kind, key string, source api.ObjectRef) error {
	plan, ok := tx.(*taskJobsTx)
	if !ok {
		_, err := raise(ctx, tx, kind, key, source)
		return err
	}
	// 上界覆盖配置允许的 257 个当前 Task、各 999 个关系/接收者及固定工作。
	if len(plan.intents) >= maxTaskJobIntents {
		return api.E("overloaded", "task_job_intent_capacity")
	}
	at, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	return plan.AddJobIntent(ctx, kind, key, source, at)
}

func withTaskJobs(ctx context.Context, tx runtime.Tx, fn func(runtime.Tx) error) error {
	_, err := withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (struct{}, error) { return struct{}{}, fn(tx) })
	return err
}

func withTaskJobsValue[T any](ctx context.Context, tx runtime.Tx, fn func(runtime.Tx) (T, error)) (T, error) {
	if _, ok := tx.(*taskJobsTx); ok {
		return fn(tx)
	}
	plan := &taskJobsTx{Tx: tx}
	out, err := fn(plan)
	if err != nil {
		return out, err
	}
	for _, intent := range plan.intents {
		if _, err = tx.Raise(ctx, intent.kind, intent.key, intent.source, intent.at); err != nil {
			return out, err
		}
	}
	return out, nil
}

func (s *Service) ControlTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ControlInput) (TaskOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (TaskOutput, error) { return s.controlTx(ctx, tx, auth, c, in) })
}

func (s *Service) ReviseTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ReviseInput) (TaskOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (TaskOutput, error) { return s.reviseTx(ctx, tx, auth, c, in) })
}

func (s *Service) SteerTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SteerInput) (TaskOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (TaskOutput, error) { return s.steerTx(ctx, tx, auth, c, in) })
}

func (s *Service) ConsumeInputTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in InputAnswer, completeGoal *api.ContentRef) (InputOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (InputOutput, error) { return s.consumeInputTx(ctx, tx, auth, c, in, completeGoal) })
}

func (s *Service) SubmitTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SubmitInput) (TaskOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (TaskOutput, error) { return s.submitTx(ctx, tx, auth, c, in) })
}

func (s *Service) CompleteTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in CompleteInput) (api.Result, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (api.Result, error) { return s.completeTx(ctx, tx, auth, in) })
}

func (s *Service) AcceptTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AcceptInput) (AcceptOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (AcceptOutput, error) { return s.acceptTx(ctx, tx, auth, c, in) })
}

func (s *Service) AllocateTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AllocateInput) (AllocationOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (AllocationOutput, error) { return s.allocateTx(ctx, tx, auth, c, in) })
}

func (s *Service) ReceiveAllocationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef, a Allocation) (IncomingAllocation, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (IncomingAllocation, error) { return s.receiveAllocationTx(ctx, tx, auth, ref, a) })
}

func (s *Service) CloseAllocationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AllocationCloseInput) (AllocationOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (AllocationOutput, error) { return s.closeAllocationTx(ctx, tx, auth, c, in) })
}

func (s *Service) SettleTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SettleInput) (AllocationOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (AllocationOutput, error) { return s.settleTx(ctx, tx, auth, c, in) })
}

func (s *Service) ReconcileClosureTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, allocationID string, closure api.AllocationClosure, sourceRefs ...api.ObjectRef) error {
	return withTaskJobs(ctx, tx, func(tx runtime.Tx) error {
		return s.reconcileClosureTx(ctx, tx, auth, allocationID, closure, sourceRefs...)
	})
}

func (s *Service) DelegateTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in DelegateInput) (DelegateOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (DelegateOutput, error) { return s.delegateTx(ctx, tx, auth, c, in) })
}

func (s *Service) MergeDelegationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, f DelegationFact) error {
	return withTaskJobs(ctx, tx, func(tx runtime.Tx) error { return s.mergeDelegationTx(ctx, tx, auth, f) })
}

func (s *Service) ChildCreateTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ChildCreateInput) (ChildOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (ChildOutput, error) { return s.childCreateTx(ctx, tx, auth, c, in) })
}

func (s *Service) ChildSendTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ChildSendInput) (ChildOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (ChildOutput, error) { return s.childSendTx(ctx, tx, auth, c, in) })
}

func (s *Service) ChildCloseTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ChildCloseInput) (ChildCloseOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (ChildCloseOutput, error) { return s.childCloseTx(ctx, tx, auth, c, in) })
}

func (s *Service) PrepareDecisionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in PreparedDecision) (api.DecisionDispatchIntent, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (api.DecisionDispatchIntent, error) { return s.prepareDecisionTx(ctx, tx, auth, in) })
}

func (s *Service) ConsumeProposalTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, p Proposal, report *ValidationReport) (Consumption, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (Consumption, error) { return s.consumeProposalTx(ctx, tx, auth, p, report) })
}

func (s *Service) MergeOperationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, operation api.Operation) error {
	return withTaskJobs(ctx, tx, func(tx runtime.Tx) error { return s.mergeOperationTx(ctx, tx, auth, operation) })
}

func (s *Service) PrepareInputTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in InputAnswer) (InputOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (InputOutput, error) { return s.prepareInputTx(ctx, tx, auth, c, in) })
}

func (s *Service) AdjustBudgetTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in BudgetInput) (TaskOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (TaskOutput, error) { return s.adjustBudgetTx(ctx, tx, auth, c, in) })
}

func (s *Service) ReconcileUsageTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, kind string, u api.UsageSnapshot) (Reservation, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (Reservation, error) { return s.reconcileUsageTx(ctx, tx, auth, kind, u) })
}

func (s *Service) AdjustmentTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AdjustmentInput) (AdjustmentOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (AdjustmentOutput, error) { return s.adjustmentTx(ctx, tx, auth, c, in) })
}

func (s *Service) AdoptRequirementsTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, taskID, sourceKind string, source api.ObjectRef, delta api.RequirementDelta, report ValidationReport) (api.RequirementAdoption, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (api.RequirementAdoption, error) {
		return s.adoptRequirementsTx(ctx, tx, auth, taskID, sourceKind, source, delta, report)
	})
}

func (s *Service) StoreCoverageTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.GoalCoverage) (api.ObjectRef, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (api.ObjectRef, error) { return s.storeCoverageTx(ctx, tx, auth, c) })
}

func (s *Service) AttachTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AttachInput) (AttachOutput, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (AttachOutput, error) { return s.attachTx(ctx, tx, auth, c, in) })
}

func (s *Service) RecordCheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.ConditionResult) (api.ObjectRef, error) {
	return withTaskJobsValue(ctx, tx, func(tx runtime.Tx) (api.ObjectRef, error) { return s.recordCheckTx(ctx, tx, auth, c) })
}
