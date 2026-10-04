package collaboration

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const remoteDelegationClosures = "collaboration.remote_delegation_closures"

type remoteDelegationClosure struct {
	Closure          task.DelegationClosure  `json:"closure"`
	TaskClosure      *task.ClosureView       `json:"task_closure,omitempty"`
	RejectedCreation *RemoteRejectedCreation `json:"rejected_creation,omitempty"`
}

// 原 child、Incoming 及交接集合已经在本次 Task→domain 事务强核。
// 此记录保存三层收束的准确组合，不把 AllocationClosureRef 当作委派 Closure。
func (r *Remote) sealDelegationClosureTx(ctx context.Context, tx runtime.Tx, handoffID string, packet RemoteCreateInput, child task.ClosureView, out *RemoteState) error {
	if !out.Fact.GoalWorkClosed || !out.Fact.EffectsClosed || !out.Fact.TransfersClosed || !out.Fact.UsageFinal {
		return nil
	}
	if err := validateStateUsageBindings(packet, *out); err != nil {
		return err
	}
	if out.AllocationClosure == nil || out.AllocationClosureRef == nil || out.Incoming == nil || out.Incoming.Gate != "closed" {
		return api.E("forbidden", "remote_final_allocation_closure_missing")
	}
	c := out.AllocationClosure
	if err := validateClosedTaskSnapshot(packet, *out, child); err != nil {
		return err
	}
	digest, err := api.Digest(*out.AllocationClosureRef)
	if err != nil {
		return err
	}
	id := remoteID("closure", packet.DelegationRef.OwnerID, packet.CreationKey, digest)
	var actual remoteDelegationClosure
	err = tx.GetVersion(ctx, remoteDelegationClosures, id, 1, &actual)
	if api.IsCode(err, "not_found") {
		now, clockErr := tx.Now(ctx)
		if clockErr != nil {
			return clockErr
		}
		actual = remoteDelegationClosure{Closure: task.DelegationClosure{DelegationID: packet.CreationKey, Revision: 1, GoalWorkClosed: true, EffectsClosed: true, AllocationClosureRef: *out.AllocationClosureRef, TransfersClosed: true, ProofRefs: []api.ContentRef{child.ProofRef, c.ProofRef}, ClosedAt: api.Time(now)}, TaskClosure: &child}
		if err = tx.Create(ctx, remoteDelegationClosures, id, handoffID, actual); err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		// 每次 Closure 查询可以签发新证明；固定原组合不重封。当前
		// Task 与完整关系摘要必须仍是原闭合快照，不能复用别的 goal。
		if actual.TaskClosure == nil || actual.RejectedCreation != nil || actual.TaskClosure.TaskRef != child.TaskRef || actual.TaskClosure.SnapshotDigest != child.SnapshotDigest {
			return api.E("snapshot_required", "original_closed_task_snapshot_changed")
		}
		if err = validateClosedTaskSnapshot(packet, *out, *actual.TaskClosure); err != nil {
			return err
		}
	}
	ref := tx.Scope().Ref(id, 1)
	out.TaskClosure = actual.TaskClosure
	out.DelegationClosure = &actual.Closure
	out.DelegationClosureRef = &ref
	out.Fact.ClosureRef = &ref
	return validateDelegationClosure(packet, *out)
}

func validateClosedTaskSnapshot(packet RemoteCreateInput, out RemoteState, closure task.ClosureView) error {
	if out.Task == nil || out.Fact.ChildTaskRef == nil || closure.TaskRef != (api.ObjectRef{TenantID: packet.SubjectRef.TenantID, OwnerID: packet.Input.ReceiverID, ObjectID: packet.ChildTaskID, Revision: out.Task.Revision}) || !closure.GoalWorkClosed || !closure.EffectsClosed || closure.AccountingOpen || closure.SnapshotDigest == "" || len(closure.EvidenceRefs) == 0 || len(closure.EvidenceRefs) > 4096 || api.ValidateRecord("ContentRef", closure.ProofRef) != nil || closure.ProofRef.TenantID != packet.SubjectRef.TenantID || closure.ProofRef.OwnerID != packet.Input.ReceiverID {
		return api.E("forbidden", "remote_closed_task_snapshot_unverified")
	}
	if _, err := api.ParseTime(closure.IssuedAt); err != nil {
		return err
	}
	if err := api.ValidateRecord("ObjectRef", closure.TaskRef); err != nil {
		return err
	}
	for _, ref := range closure.EvidenceRefs {
		if ref.TenantID != packet.SubjectRef.TenantID || api.ValidateRecord("ObjectRef", ref) != nil {
			return api.E("forbidden", "remote_closed_task_evidence_changed")
		}
	}
	return nil
}

func validateDelegationClosure(packet RemoteCreateInput, out RemoteState) error {
	if out.DelegationClosure == nil {
		if out.DelegationClosureRef != nil || out.Fact.ClosureRef != nil || out.TaskClosure != nil || out.RejectedCreation != nil {
			return api.E("forbidden", "remote_delegation_closure_unverified")
		}
		return nil
	}
	c := out.DelegationClosure
	ref := out.DelegationClosureRef
	if ref == nil || api.ValidateRecord("ObjectRef", *ref) != nil || ref.TenantID != packet.SubjectRef.TenantID || ref.OwnerID != packet.Input.ReceiverID || ref.Revision != 1 || out.Fact.ClosureRef == nil || *out.Fact.ClosureRef != *ref || out.AllocationClosure == nil || out.AllocationClosureRef == nil || (out.TaskClosure == nil) == (out.RejectedCreation == nil) || !out.Fact.GoalWorkClosed || !out.Fact.EffectsClosed || !out.Fact.TransfersClosed || !out.Fact.UsageFinal || c.DelegationID != packet.CreationKey || c.Revision != 1 || !c.GoalWorkClosed || !c.EffectsClosed || !c.TransfersClosed || c.AllocationClosureRef != *out.AllocationClosureRef {
		return api.E("forbidden", "remote_original_delegation_closure_changed")
	}
	var checked string
	var goalProof api.ContentRef
	if out.TaskClosure != nil {
		if err := validateClosedTaskSnapshot(packet, out, *out.TaskClosure); err != nil {
			return err
		}
		checked, goalProof = out.TaskClosure.IssuedAt, out.TaskClosure.ProofRef
	} else {
		if err := validateRejectedCreation(packet, out, *out.RejectedCreation); err != nil {
			return err
		}
		checked, goalProof = out.RejectedCreation.CheckedAt, out.RejectedCreation.ProofRef
	}
	if !api.Equal(c.ProofRefs, []api.ContentRef{goalProof, out.AllocationClosure.ProofRef}) {
		return api.E("forbidden", "remote_original_delegation_proofs_changed")
	}
	closed, err := api.ParseTime(c.ClosedAt)
	if err != nil {
		return err
	}
	budgetClosed, err := api.ParseTime(out.AllocationClosure.ClosedAt)
	if err != nil {
		return err
	}
	taskChecked, err := api.ParseTime(checked)
	if err != nil {
		return err
	}
	if closed.Before(budgetClosed) || closed.Before(taskChecked) {
		return api.E("forbidden", "remote_original_closure_time_changed")
	}
	validator, err := api.NewValidator(api.SchemaFor[task.DelegationClosure]())
	if err != nil {
		return err
	}
	if err = validator.Validate(api.Raw(*c)); err != nil {
		return err
	}
	digest, err := api.Digest(c.AllocationClosureRef)
	if err != nil {
		return err
	}
	if ref.ObjectID != remoteID("closure", packet.DelegationRef.OwnerID, packet.CreationKey, digest) {
		return api.E("forbidden", "remote_original_delegation_closure_reference_changed")
	}
	return nil
}
