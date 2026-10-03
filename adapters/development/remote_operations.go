package development

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func (e executionBridge) remoteIntent(ctx context.Context, s runtime.Scope, ref api.ObjectRef) (task.OperationIntent, error) {
	if s != e.a.Scope || runtime.CheckRef(s, ref) != nil {
		return task.OperationIntent{}, api.E("forbidden", "original_remote_scope_changed")
	}
	i, err := e.a.Task.ReadOperationIntent(ctx, e.a.Store, s, e.a.ServiceAuth, ref.ObjectID)
	if err != nil {
		return i, err
	}
	if i.ExecutorID != ref.OwnerID || i.TaskRef.OwnerID != s.OwnerID {
		return i, api.E("forbidden", "original_remote_operation_mismatch")
	}
	_, err = e.a.remoteExecutors.binding(i.BindingRef, i.CapabilityRef, i.InstallLockRef)
	return i, err
}

func (e executionBridge) readRemote(ctx context.Context, s runtime.Scope, ref api.ObjectRef) (api.Operation, error) {
	i, err := e.remoteIntent(ctx, s, ref)
	if err != nil {
		return api.Operation{}, err
	}
	route, err := e.a.remoteExecutors.client(ctx, i.ExecutorID)
	if err != nil {
		return api.Operation{}, err
	}
	if err = e.remoteAdmissionReady(ctx, s, i, route); err != nil {
		return api.Operation{}, err
	}
	view, err := route.Client.Get(ctx, i.OperationID)
	if err != nil {
		return api.Operation{}, err
	}
	op := view.Operation
	if op.OwnerID != i.ExecutorID || op.OperationID != i.OperationID || !api.Equal(op.TaskRef, i.TaskRef) {
		return op, api.E("forbidden", "original_remote_fact_mismatch")
	}
	if err = api.ValidateRecord("Operation", op); err != nil {
		return op, err
	}
	if op.ResultRef != nil {
		if err = e.a.rememberDeviceResult(ctx, s, i, *op.ResultRef); err != nil {
			return op, err
		}
	}
	return op, nil
}

func (e executionBridge) remoteUsage(ctx context.Context, s runtime.Scope, ref api.ObjectRef) (api.UsageSnapshot, error) {
	i, err := e.remoteIntent(ctx, s, ref)
	if err != nil {
		return api.UsageSnapshot{}, err
	}
	route, err := e.a.remoteExecutors.client(ctx, i.ExecutorID)
	if err != nil {
		return api.UsageSnapshot{}, err
	}
	if err = e.remoteAdmissionReady(ctx, s, i, route); err != nil {
		return api.UsageSnapshot{}, fmt.Errorf("read original remote admission for accounting: %w", err)
	}
	u, err := route.Client.Usage(ctx, i.OperationID)
	if err != nil {
		return u, err
	}
	if u.SourceRef.TenantID != s.TenantID || u.SourceRef.OwnerID != i.ExecutorID || u.SourceRef.ObjectID != i.OperationID || u.SourceRef.Revision != u.UsageRevision {
		return u, api.E("forbidden", "original_remote_usage_mismatch")
	}
	unsigned := u
	unsigned.UsageDigest = ""
	digest, err := api.Digest(unsigned)
	if err != nil || digest != u.UsageDigest {
		return u, api.E("forbidden", "original_remote_usage_digest_mismatch")
	}
	return u, nil
}

// 费用/效果恢复可能先于原设备安装。仅准确原准入的确认缺失或未就绪
// 返回待恢复，不把其它鉴权、数据库、网络错误归为零费用或新身份。
func (e executionBridge) remoteAdmissionReady(ctx context.Context, s runtime.Scope, i task.OperationIntent, route *remoteExecutor) error {
	var original executor.AdmissionBundle
	if _, err := e.a.Store.Read(ctx, s, "platform.remote_bundles", i.OperationID, 0, &original); err != nil {
		if api.IsCode(err, "not_found") {
			pending := api.E("dependency_unavailable", "original_remote_admission_pending")
			pending.Cause = err
			return pending
		}
		return err
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: i.ExecutorID, QueryID: api.NewID("query"), Method: "executor.admission.get", TargetID: i.OperationID, Payload: api.Raw(executor.AdmissionID{BundleID: original.BundleID})}
	raw, err := route.Client.SDK.Query(ctx, q)
	if err != nil {
		if api.IsCode(err, "not_found") {
			pending := api.E("dependency_unavailable", "original_remote_admission_pending")
			pending.Cause = err
			return pending
		}
		return err
	}
	var view executor.AdmissionView
	if err = api.Decode(raw, &view); err != nil {
		return err
	}
	if !api.Equal(view.Bundle, original) || original.DeviceDatabaseID != route.Config.DatabaseID || original.EndpointID != route.Config.OwnerID {
		return api.E("forbidden", "original_remote_admission_changed")
	}
	// 当前撤回或正文缓存缺失只能阻断新执行/正文；已安装的准确原准入仍允许
	// 最小状态和账务恢复。Client.Prepare 与设备开始 barrier 继续核 Denied/Complete。
	return nil
}

// 同一签名 lease report 的原证明保存 operation 累计快照。分别两次查询会
// 正常撞上账单进度；这里只沿这份准确依据各归并原 Grant 与 Task 的账务。
func (e executionBridge) remoteBillingUsage(ctx context.Context, s runtime.Scope, ref api.ObjectRef) (api.UsageSnapshot, error) {
	i, err := e.remoteIntent(ctx, s, ref)
	if err != nil {
		return api.UsageSnapshot{}, fmt.Errorf("query original device lease usage: %w", err)
	}
	if len(i.UseIntentRefs) != 1 {
		return api.UsageSnapshot{}, api.E("forbidden", "original_remote_lease_missing")
	}
	route, err := e.a.remoteExecutors.client(ctx, i.ExecutorID)
	if err != nil {
		return api.UsageSnapshot{}, err
	}
	if err = e.remoteAdmissionReady(ctx, s, i, route); err != nil {
		return api.UsageSnapshot{}, err
	}
	report, err := route.Client.LeaseUsage(ctx, i.UseIntentRefs[0].ObjectID)
	if err != nil {
		if api.IsCode(err, "not_found") {
			pending := api.E("dependency_unavailable", "original_remote_usage_pending")
			pending.Cause = err
			return api.UsageSnapshot{}, pending
		}
		return api.UsageSnapshot{}, err
	}
	if err = executor.VerifyLeaseReport(route.Keys, report); err != nil {
		return api.UsageSnapshot{}, err
	}
	l := report.Report.Usage
	if report.SourceDatabaseID != route.Config.DatabaseID || !api.Equal(report.Report.LeaseRef, i.UseIntentRefs[0]) || l.SourceRef.TenantID != s.TenantID || l.SourceRef.OwnerID != i.ExecutorID || l.SourceRef.ObjectID != i.UseIntentRefs[0].ObjectID || l.SourceRef.Revision != l.UsageRevision || len(l.ProofRefs) != 1 {
		return api.UsageSnapshot{}, api.E("forbidden", "original_remote_lease_source_changed")
	}
	bytes, permission, err := route.Client.ReadBytes(ctx, l.ProofRefs[0])
	if err != nil {
		return api.UsageSnapshot{}, fmt.Errorf("read original device lease accounting proof: %w", err)
	}
	if !containsString(permission.Purposes, "execution_usage_proof") {
		return api.UsageSnapshot{}, api.E("forbidden", "original_accounting_purpose_mismatch")
	}
	var basis executor.LeaseUsageProof
	if err = api.Decode(bytes, &basis); err != nil {
		return api.UsageSnapshot{}, fmt.Errorf("decode original device lease accounting proof: %w", err)
	}
	var allocation remoteAllocation
	if _, err = e.a.Store.Read(ctx, s, "platform.remote_allocations", i.OperationID, 1, &allocation); err != nil {
		return api.UsageSnapshot{}, err
	}
	u := basis.OperationUsage
	unsigned := u
	unsigned.UsageDigest = ""
	digest, err := api.Digest(unsigned)
	if err != nil || digest != u.UsageDigest || u.SourceRef.TenantID != s.TenantID || u.SourceRef.OwnerID != i.ExecutorID || u.SourceRef.ObjectID != i.OperationID || u.SourceRef.Revision != u.UsageRevision || !api.Equal(basis.LeaseRef, report.Report.LeaseRef) || basis.EndpointID != i.ExecutorID || basis.InstanceID != route.Config.InstanceID || basis.AllocationDigest != allocation.Lease.AllocationDigest || basis.LocalLease.LeaseID != allocation.Lease.LeaseID || basis.LocalLease.Revision != l.UsageRevision || !api.Equal(basis.LocalLease.Scope, allocation.Lease.Scope) || !api.Equal(basis.LocalLease.Limits, allocation.Lease.Limits) || !api.Equal(basis.LocalLease.Cumulative, l.Cumulative) || !api.Equal(u.Cumulative, l.Cumulative) || basis.LocalLease.SpendingClosed != l.SpendingClosed || basis.LocalLease.UsageFinal != l.UsageFinal || u.SpendingClosed != l.SpendingClosed || u.UsageFinal != l.UsageFinal {
		return api.UsageSnapshot{}, api.E("forbidden", "original_remote_lease_basis_changed")
	}
	// 原租约只经ApplyLeaseReportTx归并；不再把同一operation账单交ApplySettlementTx。
	status, err := e.a.Store.Within(ctx, s, []string{"governance", "platform"}, func(tx runtime.Tx) error {
		if err := currentCredentialTx(ctx, tx, e.a.ServiceAuth); err != nil {
			return err
		}
		_, err := e.a.Governance.ApplyLeaseReportTx(ctx, tx, report.Report)
		return err
	})
	if status == runtime.CommitUnknown {
		return u, runtime.ErrCommitUnknown
	}
	if err != nil {
		return u, fmt.Errorf("apply original signed device lease report: %w", err)
	}
	return u, nil
}

func (e executionBridge) controlRemote(ctx context.Context, s runtime.Scope, owner string, command api.Command, c api.ControlSnapshot) error {
	if s != e.a.Scope || c.OrchestratorID != s.OwnerID {
		return api.E("forbidden", "original_remote_control_scope_mismatch")
	}
	delivery, err := e.a.remoteControlDelivery(ctx, s, c)
	if err != nil {
		return err
	}
	route, err := e.a.remoteExecutors.client(ctx, owner)
	if err != nil {
		return err
	}
	receipt, err := route.Client.Control(ctx, command, delivery)
	if err != nil {
		return err
	}
	if receipt.Error != nil {
		return receipt.Error
	}
	return nil
}
