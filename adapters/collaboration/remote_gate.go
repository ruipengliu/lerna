package collaboration

import (
	"context"
	"sync"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type remoteChildOrigin struct {
	PacketID string `json:"packet_id"`
}

type parentScopeKey struct{}
type parentScopeCarrier struct {
	mu     sync.Mutex
	values map[string]RemoteAllocationSnapshot
}

// NewParentScopeContext 只为这一次显式入口建立有界内存，不读磁盘或网络。
func NewParentScopeContext(ctx context.Context) context.Context {
	return context.WithValue(ctx, parentScopeKey{}, &parentScopeCarrier{values: map[string]RemoteAllocationSnapshot{}})
}

// CloneParentScopeContext 只深拷显式宿主同一调用树已经取得的控制证明。
// 新Job/网络入口必须New；准确Child/subject/期限仍由纯Task门禁重核。
func CloneParentScopeContext(ctx context.Context) (context.Context, error) {
	values := parentScopeValues(ctx)
	proofs := make([]RemoteAllocationSnapshot, 0, len(values))
	for _, value := range values {
		proofs = append(proofs, value)
	}
	return WithParentScopes(NewParentScopeContext(ctx), proofs)
}
func parentScopeValues(ctx context.Context) map[string]RemoteAllocationSnapshot {
	if carrier, ok := ctx.Value(parentScopeKey{}).(*parentScopeCarrier); ok {
		carrier.mu.Lock()
		defer carrier.mu.Unlock()
		copy := map[string]RemoteAllocationSnapshot{}
		for k, v := range carrier.values {
			copy[k] = v
		}
		return copy
	}
	values, _ := ctx.Value(parentScopeKey{}).(map[string]RemoteAllocationSnapshot)
	return values
}

// WithParentScopes 只携带本次已取得的有限原证明；它不读取网络或创造许可。
func WithParentScopes(ctx context.Context, proofs []RemoteAllocationSnapshot) (context.Context, error) {
	if len(proofs) > 64 {
		return ctx, api.E("overloaded", "remote_scope_carrier_capacity")
	}
	carrier, shared := ctx.Value(parentScopeKey{}).(*parentScopeCarrier)
	var oldValues map[string]RemoteAllocationSnapshot
	if shared {
		carrier.mu.Lock()
		defer carrier.mu.Unlock()
		oldValues = carrier.values
	} else {
		oldValues = parentScopeValues(ctx)
	}
	values := map[string]RemoteAllocationSnapshot{}
	if old := oldValues; old != nil {
		for k, v := range old {
			values[k] = v
		}
	}
	for _, p := range proofs {
		if p.ScopeRevision == 0 || p.Packet.ChildTaskID == "" || p.Proof == "" {
			return ctx, api.E("invalid_request", "remote_scope_carrier_invalid")
		}
		var frozen RemoteAllocationSnapshot
		if err := api.Decode(api.Raw(p), &frozen); err != nil {
			return ctx, err
		}
		key := packetID(p.Packet)
		if old, ok := values[key]; ok && old.ScopeRevision > p.ScopeRevision {
			return ctx, api.E("revision_conflict", "remote_scope_carrier_older_than_known")
		}
		if old, ok := values[key]; ok && old.ScopeRevision == p.ScopeRevision {
			oldDigest, err := remoteScopeDigest(old)
			if err != nil {
				return ctx, err
			}
			newDigest, err := remoteScopeDigest(p)
			if err != nil {
				return ctx, err
			}
			if oldDigest != newDigest {
				return ctx, api.E("idempotency_conflict", "remote_scope_carrier_conflict")
			}
		}
		values[key] = frozen
	}
	if len(values) > 64 {
		return ctx, api.E("overloaded", "remote_scope_carrier_capacity")
	}
	if len(api.Raw(values)) > 1<<20 {
		return ctx, api.E("overloaded", "remote_scope_carrier_bytes")
	}
	if shared {
		carrier.values = values
		return ctx, nil
	}
	return context.WithValue(ctx, parentScopeKey{}, values), nil
}

func (r *Remote) childOriginal(ctx context.Context, taskID string) (remoteReceived, error) {
	var origin remoteChildOrigin
	var saved remoteReceived
	if _, err := r.cfg.Store.Read(ctx, r.cfg.Scope, "collaboration.remote_children", taskID, 1, &origin); err != nil {
		return saved, err
	}
	_, err := r.cfg.Store.Read(ctx, r.cfg.Scope, remoteIncoming, origin.PacketID, 0, &saved)
	return saved, err
}

// PrepareChild 只刷新原父当前证明；不重建目标、不创建子 Task 或追加预算。
// 网络在事务外；准备失败不替换已有 known-deny。
func (r *Remote) PrepareChild(ctx context.Context, taskID string) error {
	_, err := r.PrepareChildContext(ctx, taskID)
	return err
}

func (r *Remote) PrepareChildContext(ctx context.Context, taskID string) (context.Context, error) {
	saved, err := r.childOriginal(ctx, taskID)
	if err != nil {
		return ctx, err
	}
	current, err := r.readParent(ctx, saved.Packet)
	if err != nil {
		return ctx, err
	}
	s, err := r.service()
	if err != nil {
		return ctx, err
	}
	err = r.within(ctx, func(tx runtime.Tx) error {
		if _, err := s.ReadTaskTx(ctx, tx, r.cfg.Auth, taskID); err != nil {
			return err
		}
		return r.observeParentTx(ctx, tx, saved.Packet, current)
	})
	if err != nil {
		return ctx, err
	}
	return WithParentScopes(ctx, []RemoteAllocationSnapshot{current})
}

func (r *Remote) observeParentTx(ctx context.Context, tx runtime.Tx, packet RemoteCreateInput, snapshot RemoteAllocationSnapshot) error {
	peer, ok := r.peers[packet.DelegationRef.OwnerID]
	if !ok {
		return api.E("unsupported", "remote_parent_unpaired")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	if err = r.verifyParent(peer, packet, snapshot, now, true); err != nil {
		return err
	}
	var current remoteReceived
	rev, err := tx.Get(ctx, remoteIncoming, packetID(packet), &current)
	if err != nil {
		return err
	}
	if !api.Equal(current.Packet, packet) {
		return api.E("idempotency_conflict", "original_child_scope_changed")
	}
	if old := current.ParentSnapshot; old != nil {
		if snapshot.ScopeRevision < old.ScopeRevision {
			return api.E("revision_conflict", "parent_scope_older_than_known")
		}
		if snapshot.ScopeRevision == old.ScopeRevision {
			oldDigest, err := remoteScopeDigest(*old)
			if err != nil {
				return err
			}
			newDigest, err := remoteScopeDigest(snapshot)
			if err != nil {
				return err
			}
			if oldDigest != newDigest {
				return api.E("idempotency_conflict", "parent_scope_revision_conflict")
			}
		}
	}
	current.ParentSnapshot = &snapshot
	current.Revision++
	return tx.Put(ctx, remoteIncoming, packetID(packet), rev, current)
}

// CheckTaskCurrentTx 是纯本域门禁。宿主先锁原 Task/预算，再调用此方法。
// 非远端 Task 不取得新增许可；远端缺少当前证明明确停止新使用。
func (r *Remote) CheckTaskCurrentTx(ctx context.Context, tx runtime.Tx, actual api.Task, requireRunning bool) error {
	if err := r.checkScope(tx.Scope()); err != nil {
		return err
	}
	var origin remoteChildOrigin
	if err := tx.GetVersion(ctx, "collaboration.remote_children", actual.TaskID, 1, &origin); err != nil {
		if api.IsCode(err, "not_found") {
			return nil
		}
		return err
	}
	var saved remoteReceived
	if _, err := tx.Get(ctx, remoteIncoming, origin.PacketID, &saved); err != nil {
		return err
	}
	if saved.ChildTaskRef == nil || saved.ChildTaskRef.ObjectID != actual.TaskID || actual.OrchestratorID != tx.Scope().OwnerID || actual.TenantID != tx.Scope().TenantID {
		return api.E("forbidden", "original_child_task_mismatch")
	}
	var control remoteControlGate
	_, err := tx.Get(ctx, remoteControls, origin.PacketID, &control)
	if err != nil && !api.IsCode(err, "not_found") {
		return err
	}
	if control.Cancelled || actual.Status != "active" {
		return api.E("invalid_state", "remote_goal_closed")
	}
	if requireRunning && control.ParentPaused {
		return api.E("invalid_state", "remote_parent_paused")
	}
	if saved.ParentSnapshot == nil {
		return api.E("dependency_unavailable", "remote_parent_scope_required")
	}
	proofs := parentScopeValues(ctx)
	parent, ok := proofs[origin.PacketID]
	if !ok {
		return api.E("dependency_unavailable", "remote_parent_scope_required")
	}
	if parent.ScopeRevision < saved.ParentSnapshot.ScopeRevision {
		return api.E("revision_conflict", "remote_parent_scope_older_than_known")
	}
	if parent.ScopeRevision != saved.ParentSnapshot.ScopeRevision {
		return api.E("dependency_unavailable", "remote_parent_scope_not_observed")
	}
	knownDigest, err := remoteScopeDigest(*saved.ParentSnapshot)
	if err != nil {
		return err
	}
	actualDigest, err := remoteScopeDigest(parent)
	if err != nil {
		return err
	}
	if knownDigest != actualDigest {
		return api.E("idempotency_conflict", "remote_parent_scope_revision_conflict")
	}
	profile, err := r.profile(saved.Packet.ProfileRef)
	if err != nil {
		return err
	}
	if _, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, saved.Packet.SubjectRef, profile, false); err != nil {
		return err
	}
	peer, ok := r.peers[saved.Packet.DelegationRef.OwnerID]
	if !ok {
		return api.E("unsupported", "remote_parent_unpaired")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	if err = r.verifyParent(peer, saved.Packet, parent, now, true); err != nil {
		return err
	}
	if parent.ParentTask.GoalRevision != saved.Packet.Input.ParentGoalRevision || parent.ParentTask.Status != "active" || parent.Allocation.State != "open" && parent.Allocation.State != "preparing" {
		return api.E("invalid_state", "remote_parent_scope_closed")
	}
	if !parent.Allowed && (requireRunning || parent.Reason != "task_not_running") {
		return api.E("forbidden", "remote_parent_scope_denied")
	}
	if requireRunning && parent.ParentTask.Control != "running" {
		return api.E("invalid_state", "remote_parent_paused")
	}
	if control.ControlRevision > parent.ParentTask.ControlRevision {
		return api.E("dependency_unavailable", "remote_parent_scope_older_than_control")
	}
	return nil
}
