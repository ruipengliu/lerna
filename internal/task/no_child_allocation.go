package task

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// RejectedIncomingClosure 只投影原永久拒绝与已关闭额度，不构造子 Task。
type RejectedIncomingClosure struct {
	Incoming   IncomingAllocation
	Closure    api.AllocationClosure
	ClosureRef api.ObjectRef
	CommandRef api.ObjectRef
	Receipt    api.Receipt
}

// SealRejectedIncomingTx 沿原 Runtime 命令、关闭墓碑和原预算单位核验未创建。
// 不使用当前开始权，不读取目标正文；未知命令或可能已有 Task 均不能归零。
func (s *Service) SealRejectedIncomingTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef, commandID string) (RejectedIncomingClosure, error) {
	var out RejectedIncomingClosure
	original, err := tx.LoadCommand(ctx, commandID)
	if err != nil {
		return out, err
	}
	if original.Tombstone || original.Command.Method != "collaboration.create" || original.Command.LogicalServiceID != tx.Scope().OwnerID || original.Command.CommandID != commandID || original.PrincipalID != auth.SubjectID || auth.SubjectID != ref.OwnerID || auth.TenantID != tx.Scope().TenantID || ref.TenantID != tx.Scope().TenantID || ref.Revision != 1 || original.Receipt.CommandID != commandID || original.Receipt.RequestDigest != original.Digest || original.Receipt.Stage != "rejected" || original.Receipt.AcceptedAt == "" || original.Receipt.Error == nil || original.Receipt.Error.Code != "invalid_state" || original.Receipt.Error.Reason != "allocation_closed" {
		return out, api.E("forbidden", "original_no_child_rejection_required")
	}
	commandDigest, err := api.Digest(original.Command)
	if err != nil || commandDigest != original.Digest {
		return out, api.E("forbidden", "original_no_child_command_digest_changed")
	}
	// 原公开命令已经按负责方封闭合同接纳。这里只读取领域需要的元数据，
	// 不复制 Goal、权限包或正文，也不把当前调用参数当作原预算事实。
	var packet struct {
		CreationKey     string        `json:"creation_key"`
		CreateCommandID string        `json:"create_command_id"`
		ChildTaskID     string        `json:"child_task_id"`
		DelegationRef   api.ObjectRef `json:"delegation_ref"`
		AllocationRef   api.ObjectRef `json:"allocation_ref"`
		Input           DelegateInput `json:"input"`
	}
	if err = json.Unmarshal(original.Command.Payload, &packet); err != nil {
		return out, err
	}
	if api.ValidateRecord("ObjectRef", ref) != nil || api.ValidateRecord("ObjectRef", packet.Input.ParentTaskRef) != nil || packet.AllocationRef != ref || packet.CreateCommandID != commandID || packet.CreationKey != original.Command.TargetID || packet.Input.DelegationID != packet.CreationKey || packet.DelegationRef != (api.ObjectRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID, ObjectID: packet.CreationKey, Revision: 1}) || packet.Input.ReceiverID != tx.Scope().OwnerID || packet.Input.Deadline != original.Command.ExpiresAt || packet.Input.ParentTaskRef.TenantID != ref.TenantID || packet.Input.ParentTaskRef.OwnerID != ref.OwnerID || !api.ValidID(packet.ChildTaskID) || len(packet.Input.Budget) == 0 {
		return out, api.E("forbidden", "original_no_child_allocation_changed")
	}
	if err = api.ValidateAmounts(packet.Input.Budget); err != nil {
		return out, err
	}
	var absent taskState
	if err = tx.GetVersion(ctx, tasks, packet.ChildTaskID, 1, &absent); err == nil {
		return out, api.E("invalid_state", "original_child_already_created")
	} else if !confirmedNotFound(err) {
		return out, err
	}
	id := incomingID(ref)
	a, err := incomingForTaskTx(ctx, tx, id)
	if err != nil {
		return out, err
	}
	if a.Gate != "closed" || a.TaskRef != nil || a.AllocationID != ref.ObjectID || a.ParentOwner != ref.OwnerID || a.ReceiverID != tx.Scope().OwnerID || a.ParentTaskRef != packet.Input.ParentTaskRef || a.UsageRevision != 1 || a.ClosedAt == "" {
		return out, api.E("forbidden", "original_no_child_gate_unverified")
	}
	closed, err := api.ParseTime(a.ClosedAt)
	if err != nil {
		return out, err
	}
	decided, err := api.ParseTime(original.Receipt.DecidedAt)
	if err != nil {
		return out, err
	}
	accepted, err := api.ParseTime(original.Receipt.AcceptedAt)
	if err != nil {
		return out, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return out, err
	}
	if decided.Before(accepted) || now.Before(decided) || now.Before(closed) {
		return out, api.E("forbidden", "original_no_child_rejection_time_changed")
	}
	zero := make([]api.Amount, 0, len(packet.Input.Budget))
	for _, amount := range packet.Input.Budget {
		zero = append(zero, api.Amount{Unit: amount.Unit, Value: "0"})
	}
	// 旧永久关闭墓碑没有获准金额；准确单位现在来自同一已拒原命令。
	// 非空存量必须完全等同；不能覆写未知或非零费用。
	if len(a.Limits) != 0 && !api.Equal(a.Limits, packet.Input.Budget) || len(a.Cumulative) != 0 && !api.Equal(a.Cumulative, zero) {
		return out, api.E("forbidden", "original_no_child_usage_unknown")
	}
	closure := api.AllocationClosure{AllocationID: a.AllocationID, ParentOwnerID: a.ParentOwner, ReceiverID: a.ReceiverID, SpendingClosed: true, ClosedAt: a.ClosedAt, UsageRevision: a.UsageRevision, FinalUsage: zero}
	if a.ClosureRef == nil {
		semantic := fmt.Sprintf("%s/%020d", id, a.UsageRevision)
		binding, lookupErr := tx.LookupKey(ctx, closures, semantic)
		var sealedRef api.ObjectRef
		if lookupErr == nil {
			var saved api.AllocationClosure
			if err = tx.GetVersion(ctx, closures, binding.ObjectID, 1, &saved); err != nil {
				return out, err
			}
			closure.ProofRef = saved.ProofRef
			if !api.Equal(saved, closure) {
				return out, api.E("idempotency_conflict", "original_no_child_closure_changed")
			}
			sealedRef = tx.Scope().Ref(binding.ObjectID, 1)
		} else if !confirmedNotFound(lookupErr) {
			return out, lookupErr
		} else {
			sealer, ok := s.ports.ClosureProof.(AllocationProofPort)
			if !ok {
				return out, api.E("dependency_unavailable", "allocation_closure_proof_unavailable")
			}
			closure.ProofRef, err = sealer.SealAllocationClosureTx(ctx, tx, closure)
			if err != nil {
				return out, err
			}
			if err = s.checkSourceProof(tx.Scope(), closure.ProofRef); err != nil {
				return out, err
			}
			if err = api.ValidateRecord("AllocationClosure", closure); err != nil {
				return out, err
			}
			closureID := s.config.Identity.NewID("closure")
			if err = tx.Create(ctx, closures, closureID, a.AllocationID, closure); err != nil {
				return out, err
			}
			digest, err := api.Digest(closure)
			if err != nil {
				return out, err
			}
			if err = tx.Bind(ctx, closures, semantic, closureID, digest); err != nil {
				return out, err
			}
			sealedRef = tx.Scope().Ref(closureID, 1)
		}
		if err = s.checkSourceProof(tx.Scope(), closure.ProofRef); err != nil {
			return out, err
		}
		if err = api.ValidateRecord("AllocationClosure", closure); err != nil {
			return out, err
		}
		if a.Revision >= 9007199254740991 {
			return out, api.E("overloaded", "incoming_revision_limit")
		}
		a.ClosureRef = &sealedRef
		a.Limits, a.Cumulative, a.ClosurePending = packet.Input.Budget, zero, false
		a.Revision++
		if err = tx.Put(ctx, incoming, id, a.Revision-1, a); err != nil {
			return out, err
		}
		if err = queueJob(ctx, tx, JobAllocation, "correction/"+id, tx.Scope().Ref(a.AllocationID, a.UsageRevision)); err != nil {
			return out, err
		}
	} else {
		var saved api.AllocationClosure
		if err = tx.GetVersion(ctx, closures, a.ClosureRef.ObjectID, a.ClosureRef.Revision, &saved); err != nil {
			return out, err
		}
		closure.ProofRef = saved.ProofRef
		if !api.Equal(saved, closure) || a.ClosureRef.OwnerID != tx.Scope().OwnerID || a.ClosureRef.TenantID != tx.Scope().TenantID || a.ClosureRef.Revision != 1 {
			return out, api.E("forbidden", "original_no_child_closure_changed")
		}
		if err = s.checkSourceProof(tx.Scope(), saved.ProofRef); err != nil {
			return out, err
		}
		if err = api.ValidateRecord("AllocationClosure", saved); err != nil {
			return out, err
		}
	}
	out = RejectedIncomingClosure{Incoming: a, Closure: closure, ClosureRef: *a.ClosureRef, CommandRef: tx.Scope().Ref(commandID, 1), Receipt: original.Receipt}
	return out, nil
}
