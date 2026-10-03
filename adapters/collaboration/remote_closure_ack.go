package collaboration

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const remoteClosureAcknowledgements = "collaboration.remote_closure_acknowledgements"

type remoteClosureAcknowledgement struct {
	ClosureRef api.ObjectRef              `json:"closure_ref"`
	Receipt    api.Receipt                `json:"receipt"`
	Parent     RemoteAllocationReadOutput `json:"parent"`
}

// 被拒的原报告回执永远不改。父也可以通过原 State 查询取得同一关闭
// 账单；只有已验签、准确同一 Closure 的 settled 事实才能收束报告责任。
func (r *Remote) acknowledgeReportedClosure(ctx context.Context, scope runtime.Scope, command api.Command, receipt api.Receipt, inc task.IncomingAllocation, ref api.ObjectRef) error {
	if receipt.CommandID != command.CommandID || receipt.Stage != "rejected" || receipt.Error == nil || command.TargetID != inc.AllocationID || inc.TaskRef == nil || inc.Gate != "closed" || inc.ClosureRef == nil || *inc.ClosureRef != ref {
		return api.E("forbidden", "original_closure_report_binding_changed")
	}
	original, err := r.childOriginal(ctx, inc.TaskRef.ObjectID)
	if err != nil {
		return err
	}
	p := original.Packet
	if p.AllocationRef != (api.ObjectRef{TenantID: scope.TenantID, OwnerID: inc.ParentOwner, ObjectID: inc.AllocationID, Revision: 1}) || original.ParentSnapshot == nil {
		return api.E("forbidden", "original_closure_allocation_missing")
	}
	var saved remoteClosureAcknowledgement
	if _, err = r.cfg.Store.Read(ctx, scope, remoteClosureAcknowledgements, command.CommandID, 1, &saved); err == nil {
		if saved.ClosureRef != ref || !api.Equal(saved.Receipt, receipt) {
			return api.E("idempotency_conflict", "original_closure_acknowledgement_changed")
		}
		return validateClosureAcknowledgement(p, original.ParentSnapshot.Allocation, saved.Parent, ref)
	} else if !api.IsCode(err, "not_found") {
		return err
	}
	parent, err := r.readRemoteAllocation(ctx, scope, p.AllocationRef)
	if err != nil {
		return err
	}
	if parent.Allocation.State != "settled" {
		return &task.OriginalCommandRejection{Receipt: receipt}
	}
	if err = validateClosureAcknowledgement(p, original.ParentSnapshot.Allocation, parent, ref); err != nil {
		return err
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	return r.within(ctx, func(tx runtime.Tx) error {
		actual, err := s.ReadIncomingAllocationTx(ctx, tx, r.cfg.Auth, p.AllocationRef)
		if err != nil {
			return err
		}
		if actual.Gate != "closed" || actual.ClosureRef == nil || *actual.ClosureRef != ref || !api.Equal(actual.TaskRef, inc.TaskRef) || actual.UsageRevision != inc.UsageRevision {
			return api.E("snapshot_required", "original_report_closure_advanced")
		}
		var current remoteCommand
		if _, err = tx.Get(ctx, remoteCommands, command.CommandID, &current); err != nil {
			return err
		}
		if !api.Equal(current.Command, command) {
			return api.E("idempotency_conflict", "original_closure_report_changed")
		}
		var known remoteClosureAcknowledgement
		if err = tx.GetVersion(ctx, remoteClosureAcknowledgements, command.CommandID, 1, &known); err == nil {
			if known.ClosureRef != ref || !api.Equal(known.Receipt, receipt) {
				return api.E("idempotency_conflict", "original_closure_acknowledgement_changed")
			}
			return validateClosureAcknowledgement(p, original.ParentSnapshot.Allocation, known.Parent, ref)
		} else if !api.IsCode(err, "not_found") {
			return err
		}
		return tx.Create(ctx, remoteClosureAcknowledgements, command.CommandID, inc.TaskRef.ObjectID, remoteClosureAcknowledgement{ref, receipt, parent})
	})
}

func validateClosureAcknowledgement(p RemoteCreateInput, original task.Allocation, out RemoteAllocationReadOutput, ref api.ObjectRef) error {
	a := out.Allocation
	if out.SourceDatabaseID != p.SourceDatabaseID || a.AllocationID != p.AllocationRef.ObjectID || a.ParentTaskRef != p.Input.ParentTaskRef || a.ReceiverID != p.Input.ReceiverID || a.Deadline != p.Input.Deadline || !api.Equal(a.Limits, p.Input.Budget) || a.CommandRef != original.CommandRef || a.State != "settled" || a.ClosureRef == nil || *a.ClosureRef != ref {
		return api.E("forbidden", "original_closure_acknowledgement_unverified")
	}
	return nil
}
