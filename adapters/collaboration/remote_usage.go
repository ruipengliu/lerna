package collaboration

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const remoteDelegatedUsages = "collaboration.remote_delegated_usages"
const remoteDelegatedUsageHeads = "collaboration.remote_delegated_usage_heads"

// 本方费用投影保留已经在接收时完整验签的原state，不替换任何foreign引用。
type remoteDelegatedUsage struct {
	PacketDigest  string               `json:"packet_digest"`
	Usage         api.UsageSnapshot    `json:"usage"`
	OriginalUsage *api.UsageSnapshot   `json:"original_usage,omitempty"`
	State         *RemoteState         `json:"state,omitempty"`
	Closure       *RemoteClosureReport `json:"closure,omitempty"`
}

// 本方投影代次只为原delegation结算去重；child计费代次与State事实代次
// 仍在完整原签名中保留，不把两个独立序列当成一个源版本。
type remoteDelegatedUsageHead struct {
	PacketDigest  string            `json:"packet_digest"`
	OriginalUsage api.UsageSnapshot `json:"original_usage"`
	Usage         api.UsageSnapshot `json:"usage"`
}

func (r *Remote) validateStateUsage(packet RemoteCreateInput, out RemoteState) error {
	if out.Fact.Revision == 0 {
		return api.E("forbidden", "remote_child_state_revision_invalid")
	}
	if out.Task != nil {
		if out.Task.TenantID != packet.SubjectRef.TenantID || out.Task.OrchestratorID != packet.Input.ReceiverID || out.Task.TaskID != packet.ChildTaskID || out.Fact.ChildTaskRef == nil || out.Task.ResultRef != nil && (out.ResultRef == nil || *out.Task.ResultRef != *out.ResultRef) {
			return api.E("forbidden", "remote_child_task_truth_changed")
		}
	}
	if inc := out.Incoming; inc != nil {
		if inc.AllocationID != packet.AllocationRef.ObjectID || inc.ParentOwner != packet.DelegationRef.OwnerID || inc.ReceiverID != packet.Input.ReceiverID || inc.ParentTaskRef != packet.Input.ParentTaskRef || inc.UsageRevision == 0 || !api.Equal(inc.TaskRef, out.Fact.ChildTaskRef) {
			return api.E("forbidden", "remote_original_incoming_changed")
		}
		if err := api.ValidateAmounts(inc.Cumulative); err != nil {
			return err
		}
		if inc.TaskRef != nil && !api.Equal(inc.Limits, packet.Input.Budget) {
			return api.E("forbidden", "remote_original_allocation_limits_changed")
		}
	}
	if out.AllocationClosure != nil {
		c := out.AllocationClosure
		if out.Incoming == nil || out.Incoming.Gate != "closed" || out.Incoming.ClosureRef == nil || out.AllocationClosureRef == nil || *out.Incoming.ClosureRef != *out.AllocationClosureRef || out.AllocationClosureRef.TenantID != packet.SubjectRef.TenantID || out.AllocationClosureRef.OwnerID != packet.Input.ReceiverID || api.ValidateRecord("AllocationClosure", *c) != nil || c.AllocationID != packet.AllocationRef.ObjectID || c.ParentOwnerID != packet.DelegationRef.OwnerID || c.ReceiverID != packet.Input.ReceiverID || !c.SpendingClosed || c.UsageRevision != out.Incoming.UsageRevision || !api.Equal(c.FinalUsage, out.Incoming.Cumulative) || c.ProofRef.OwnerID != packet.Input.ReceiverID || c.ProofRef.TenantID != packet.SubjectRef.TenantID {
			return api.E("forbidden", "remote_original_closure_changed")
		}
		u := out.Fact.Usage
		if u == nil || !out.Fact.UsageFinal || !u.SpendingClosed || !u.UsageFinal || u.SourceRef != (api.ObjectRef{TenantID: packet.SubjectRef.TenantID, OwnerID: packet.Input.ReceiverID, ObjectID: c.AllocationID, Revision: c.UsageRevision}) || u.UsageRevision != c.UsageRevision || !api.Equal(u.Cumulative, c.FinalUsage) || !api.Equal(u.ProofRefs, []api.ContentRef{c.ProofRef}) {
			return api.E("forbidden", "remote_original_closed_usage_changed")
		}
		digest, err := task.UsageDigest(*u)
		if err != nil {
			return err
		}
		if digest != u.UsageDigest {
			return api.E("forbidden", "remote_original_usage_digest_changed")
		}
	} else if out.AllocationClosureRef != nil || out.Fact.Usage != nil || out.Fact.UsageFinal {
		return api.E("forbidden", "remote_unverified_final_usage")
	}
	return nil
}

func (r *Remote) observeStateUsage(ctx context.Context, d task.Delegation, out RemoteState) error {
	if out.Incoming == nil || out.Incoming.TaskRef == nil {
		// 尚无Child时不从空费用拼出最终零账单；永久关闭由原Closure协议收束。
		return nil
	}
	var saved remoteSent
	if _, err := r.cfg.Store.Read(ctx, r.cfg.Scope, remoteOutgoing, remoteID("handoff", r.cfg.Scope.TenantID, r.cfg.Scope.OwnerID, d.CreationKey), 0, &saved); err != nil {
		return err
	}
	usage := api.UsageSnapshot{SourceRef: api.ObjectRef{TenantID: r.cfg.Scope.TenantID, OwnerID: d.ReceiverID, ObjectID: d.AllocationRef.ObjectID, Revision: out.Incoming.UsageRevision}, UsageRevision: out.Incoming.UsageRevision, Cumulative: append([]api.Amount{}, out.Incoming.Cumulative...), SpendingClosed: false, UsageFinal: false, ProofRefs: []api.ContentRef{}}
	if out.Fact.Usage != nil {
		usage.SpendingClosed, usage.UsageFinal = out.Fact.Usage.SpendingClosed, out.Fact.Usage.UsageFinal
		usage.ProofRefs = append([]api.ContentRef{}, out.Fact.Usage.ProofRefs...)
	}
	var err error
	usage.UsageDigest, err = task.UsageDigest(usage)
	if err != nil {
		return err
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	return r.within(ctx, func(tx runtime.Tx) error {
		// 费用是原接收方的事实；父取消或新开始权收回不能把它抹掉。
		actual, err := s.ReadAllocationTx(ctx, tx, r.cfg.Auth, d.AllocationRef.ObjectID)
		if err != nil {
			return err
		}
		if actual.ReceiverID != saved.Packet.Input.ReceiverID || actual.ParentTaskRef.ObjectID != saved.Packet.Input.ParentTaskRef.ObjectID || actual.AllocationID != saved.Packet.AllocationRef.ObjectID {
			return api.E("forbidden", "remote_original_usage_allocation_changed")
		}
		return r.projectDelegationUsageTx(ctx, tx, saved.Packet, usage, &out, nil)
	})
}

func (r *Remote) observeClosureUsageTx(ctx context.Context, tx runtime.Tx, allocation task.Allocation, closure RemoteClosureReport) error {
	rows, err := tx.List(ctx, remoteOutgoing, allocation.ParentTaskRef.ObjectID, "", 129)
	if err != nil {
		return err
	}
	if len(rows) > 128 {
		return api.E("overloaded", "remote_delegation_lifetime_limit")
	}
	var original *RemoteCreateInput
	for _, row := range rows {
		var saved remoteSent
		if err = row.Decode(&saved); err != nil {
			return err
		}
		p := saved.Packet
		if p.AllocationRef.ObjectID != allocation.AllocationID {
			continue
		}
		if original != nil || p.AllocationRef != tx.Scope().Ref(allocation.AllocationID, 1) || p.Input.ParentTaskRef != allocation.ParentTaskRef || p.Input.ReceiverID != allocation.ReceiverID || !api.Equal(p.Input.Budget, allocation.Limits) || p.SourceDatabaseID != tx.Scope().DatabaseID || p.DelegationRef.OwnerID != tx.Scope().OwnerID || p.DelegationRef.TenantID != tx.Scope().TenantID || p.DelegationRef.ObjectID != p.Input.DelegationID || p.DelegationRef.Revision != 1 || row.ID != remoteID("handoff", tx.Scope().TenantID, tx.Scope().OwnerID, p.CreationKey) {
			return api.E("forbidden", "remote_original_closure_mapping_changed")
		}
		original = &p
	}
	if original == nil {
		return api.E("forbidden", "remote_original_delegation_missing")
	}
	c := closure.Closure
	usage := api.UsageSnapshot{SourceRef: api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: allocation.ReceiverID, ObjectID: allocation.AllocationID, Revision: c.UsageRevision}, UsageRevision: c.UsageRevision, Cumulative: append([]api.Amount{}, c.FinalUsage...), SpendingClosed: c.SpendingClosed, UsageFinal: true, ProofRefs: []api.ContentRef{c.ProofRef}}
	usage.UsageDigest, err = task.UsageDigest(usage)
	if err != nil {
		return err
	}
	return r.projectDelegationUsageTx(ctx, tx, *original, usage, nil, &closure)
}

func remoteUsageMonotone(previous, current api.UsageSnapshot) bool {
	if previous.SpendingClosed && !current.SpendingClosed || previous.UsageFinal && !current.UsageFinal {
		return false
	}
	for _, before := range previous.Cumulative {
		after := "0"
		for _, candidate := range current.Cumulative {
			if candidate.Unit == before.Unit {
				after = candidate.Value
				break
			}
		}
		cmp, err := api.CompareDecimal(after, before.Value)
		if err != nil || cmp < 0 {
			return false
		}
	}
	return true
}

// 调用方已经核验完整当前签名及Task根/预算。本方头、准确来源投影与
// 原Use结算同一短Tx提交；这里不读取远端正文，不产生RPC或新授权。
func (r *Remote) projectDelegationUsageTx(ctx context.Context, tx runtime.Tx, packet RemoteCreateInput, original api.UsageSnapshot, state *RemoteState, closure *RemoteClosureReport) error {
	if original.SourceRef != (api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: packet.Input.ReceiverID, ObjectID: packet.AllocationRef.ObjectID, Revision: original.UsageRevision}) || original.UsageRevision == 0 || (state == nil) == (closure == nil) {
		return api.E("forbidden", "remote_original_usage_source_changed")
	}
	digest, err := task.UsageDigest(original)
	if err != nil {
		return err
	}
	if digest != original.UsageDigest {
		return api.E("forbidden", "remote_original_usage_digest_changed")
	}
	packetDigest, err := api.Digest(packet)
	if err != nil {
		return err
	}
	id := packet.DelegationRef.ObjectID
	var head remoteDelegatedUsageHead
	rev, err := tx.Get(ctx, remoteDelegatedUsageHeads, id, &head)
	if err != nil && !api.IsCode(err, "not_found") {
		return err
	}
	exists := err == nil
	if exists {
		validator, schemaErr := api.NewValidator(api.SchemaFor[remoteDelegatedUsageHead]())
		if schemaErr != nil {
			return schemaErr
		}
		if err = validator.Validate(api.Raw(head)); err != nil {
			return err
		}
		for _, saved := range []api.UsageSnapshot{head.Usage, head.OriginalUsage} {
			digest, digestErr := task.UsageDigest(saved)
			if digestErr != nil {
				return digestErr
			}
			if saved.UsageRevision == 0 || saved.SourceRef.Revision != saved.UsageRevision || digest != saved.UsageDigest {
				return api.E("idempotency_conflict", "remote_original_usage_head_digest_changed")
			}
		}
		if head.PacketDigest != packetDigest || head.Usage.SourceRef != tx.Scope().Ref(id, head.Usage.UsageRevision) || head.OriginalUsage.SourceRef.TenantID != original.SourceRef.TenantID || head.OriginalUsage.SourceRef.OwnerID != original.SourceRef.OwnerID || head.OriginalUsage.SourceRef.ObjectID != original.SourceRef.ObjectID {
			return api.E("idempotency_conflict", "remote_original_usage_head_changed")
		}
		if original.UsageRevision < head.OriginalUsage.UsageRevision || original.UsageDigest == head.OriginalUsage.UsageDigest {
			if r.cfg.ScopeGate != nil {
				return r.cfg.ScopeGate.ObserveUsageTx(ctx, tx, packet, head.Usage)
			}
			return nil
		}
		if !remoteUsageMonotone(head.OriginalUsage, original) || original.UsageRevision == head.OriginalUsage.UsageRevision && (!api.Equal(original.Cumulative, head.OriginalUsage.Cumulative) || head.OriginalUsage.UsageFinal) {
			return api.E("idempotency_conflict", "remote_original_usage_revision_changed")
		}
	} else {
		// 兼容已保存的旧State投影，不改其字节、SourceRef或既有结算。
		after, scanned := "", 0
		for {
			limit := min(1000, 1025-scanned)
			rows, listErr := tx.List(ctx, remoteDelegatedUsages, id, after, limit)
			if listErr != nil {
				return listErr
			}
			scanned += len(rows)
			if scanned > 1024 {
				return api.E("overloaded", "remote_legacy_usage_projection_limit")
			}
			for _, row := range rows {
				var old remoteDelegatedUsage
				if err = row.Decode(&old); err != nil {
					return err
				}
				if err = api.ValidateRecord("UsageSnapshot", old.Usage); err != nil {
					return err
				}
				digest, digestErr := task.UsageDigest(old.Usage)
				if digestErr != nil {
					return digestErr
				}
				if old.PacketDigest != packetDigest || old.Usage.SourceRef != tx.Scope().Ref(id, old.Usage.UsageRevision) || row.ID != remoteID("usage", id, fmt.Sprint(old.Usage.UsageRevision)) || digest != old.Usage.UsageDigest {
					return api.E("idempotency_conflict", "remote_original_usage_projection_changed")
				}
				if old.Usage.UsageRevision > head.Usage.UsageRevision {
					head.Usage = old.Usage
				}
			}
			if len(rows) < limit {
				break
			}
			after = rows[len(rows)-1].ID
		}
		if !remoteUsageMonotone(head.Usage, original) {
			return api.E("idempotency_conflict", "remote_original_usage_regressed")
		}
	}
	if head.Usage.UsageRevision == ^uint64(0) {
		return api.E("overloaded", "remote_usage_revision_limit")
	}
	usage := original
	usage.UsageRevision = head.Usage.UsageRevision + 1
	usage.SourceRef = tx.Scope().Ref(id, usage.UsageRevision)
	usage.UsageDigest, err = task.UsageDigest(usage)
	if err != nil {
		return err
	}
	head = remoteDelegatedUsageHead{PacketDigest: packetDigest, OriginalUsage: original, Usage: usage}
	if exists {
		err = tx.Put(ctx, remoteDelegatedUsageHeads, id, rev, head)
	} else {
		err = tx.Create(ctx, remoteDelegatedUsageHeads, id, id, head)
	}
	if err != nil {
		return err
	}
	evidence := remoteDelegatedUsage{PacketDigest: packetDigest, Usage: usage, OriginalUsage: &original, State: state, Closure: closure}
	if err = tx.Create(ctx, remoteDelegatedUsages, remoteID("usage", id, fmt.Sprint(usage.UsageRevision)), id, evidence); err != nil {
		return err
	}
	if r.cfg.ScopeGate != nil {
		return r.cfg.ScopeGate.ObserveUsageTx(ctx, tx, packet, usage)
	}
	return nil
}

// VerifyDelegationUsage只接受此前真正验签并提交的本方费用投影。
// 它是费用恢复端口，既不发RPC，也不授予新行动或改写原远端账单。
func (r *Remote) VerifyDelegationUsage(ctx context.Context, scope runtime.Scope, target api.ObjectRef, usage api.UsageSnapshot) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if target.TenantID != scope.TenantID || target.OwnerID != scope.OwnerID || target.ObjectID != usage.SourceRef.ObjectID || usage.SourceRef.TenantID != scope.TenantID || usage.SourceRef.OwnerID != scope.OwnerID || usage.SourceRef.Revision != usage.UsageRevision || usage.UsageRevision == 0 {
		return api.E("forbidden", "remote_original_usage_target_changed")
	}
	var saved remoteDelegatedUsage
	_, err := r.cfg.Store.Read(ctx, scope, remoteDelegatedUsages, remoteID("usage", target.ObjectID, fmt.Sprint(usage.UsageRevision)), 1, &saved)
	if err != nil {
		return err
	}
	if !api.Equal(saved.Usage, usage) {
		return api.E("forbidden", "remote_original_usage_projection_changed")
	}
	return nil
}
