package collaboration

import (
	"context"
	"errors"
	"os"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func (r *Remote) outgoingProfile(d task.Delegation) (RemoteAgentProfile, error) {
	var selected RemoteAgentProfile
	for _, p := range r.profiles {
		if p.Values.ParentOwnerID != r.cfg.Scope.OwnerID || p.Values.ReceiverID != d.ReceiverID || p.Values.AgentBindingRef != d.AgentBindingRef || !api.Equal(p.Values.PolicyRef, d.PolicyRef) {
			continue
		}
		if selected.ProfileRef.ComponentID != "" {
			return selected, api.E("invalid_request", "remote_agent_profile_ambiguous")
		}
		selected = p
	}
	if selected.ProfileRef.ComponentID == "" {
		return selected, api.E("unsupported", "remote_agent_binding_unconfigured")
	}
	return selected, nil
}
func (r *Remote) planCreate(ctx context.Context, d task.Delegation, a task.Allocation) (remoteSent, error) {
	var out remoteSent
	err := r.within(ctx, func(tx runtime.Tx) error {
		var err error
		out, err = r.planCreateTx(ctx, tx, d, a)
		return err
	})
	return out, err
}
func (r *Remote) planCreateTx(ctx context.Context, tx runtime.Tx, d task.Delegation, a task.Allocation) (remoteSent, error) {
	var out remoteSent
	profile, err := r.outgoingProfile(d)
	if err != nil {
		return out, err
	}
	if r.cfg.Memory == nil {
		return out, api.E("unsupported", "remote_material_authority_unconfigured")
	}
	s, err := r.service()
	if err != nil {
		return out, err
	}
	id := remoteID("handoff", r.cfg.Scope.TenantID, r.cfg.Scope.OwnerID, d.CreationKey)
	err = func() error {
		current, err := s.CheckDelegationScopeTx(ctx, tx, r.cfg.Auth, d.DelegationID)
		if err != nil {
			return err
		}
		if !api.Equal(current.Delegation.DelegateInput, d.DelegateInput) || current.Delegation.CreationKey != d.CreationKey || current.Delegation.AllocationRef != d.AllocationRef || current.Allocation.AllocationID != a.AllocationID || !api.Equal(current.Allocation.Limits, a.Limits) {
			return api.E("idempotency_conflict", "original_delegation_changed")
		}
		actor, err := r.cfg.Authority.ResolveSubjectTx(ctx, tx, current.SubjectRef, profile, false)
		if err != nil {
			return err
		}
		_, err = tx.Get(ctx, remoteOutgoing, id, &out)
		if err == nil {
			if !api.Equal(out.Packet.Input, d.DelegateInput) || out.Packet.AllocationRef != d.AllocationRef || out.Packet.SubjectRef != current.SubjectRef {
				return api.E("idempotency_conflict", "original_remote_create_changed")
			}
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		packet := RemoteCreateInput{CreationKey: d.CreationKey, CreateCommandID: remoteID("command", id, "create"), ChildTaskID: remoteID("task", id, "child"), ProfileRef: profile.ProfileRef, DelegationRef: r.cfg.Scope.Ref(d.DelegationID, 1), AllocationRef: d.AllocationRef, SourceDatabaseID: r.cfg.Scope.DatabaseID, SubjectRef: current.SubjectRef, Input: d.DelegateInput, AncestorTaskRefs: append([]api.ObjectRef{}, d.AncestorTaskRefs...), ForeignReferences: []memory.ForeignReference{}}
		packet.OriginalCommandRef, packet.ParentSources = d.CommandRef, current.ParentSources
		pending := append([]api.ContentRef{d.GoalRef}, d.InputRefs...)
		seen := map[api.ContentRef]bool{}
		for len(pending) > 0 {
			ref := pending[0]
			pending = pending[1:]
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if len(seen) > 100 || len(pending) > 100 {
				return api.E("overloaded", "remote_material_source_limit")
			}
			if ref.OwnerID == d.ReceiverID {
				continue
			}
			for _, purpose := range profile.Values.MaterialPurposes {
				v, err := r.cfg.Memory.CheckContentTx(ctx, tx, actor, ref, purpose, profile.Values.Location, false)
				if err != nil {
					return err
				}
				pending = append(pending, v.ProcessedSources...)
				pending = append(pending, v.DisclosedSources...)
				retain, err := api.ParseTime(v.RetentionUntil)
				if err != nil {
					return err
				}
				deadline, err := api.ParseTime(d.Deadline)
				if err != nil {
					return err
				}
				if deadline.Before(retain) {
					retain = deadline
				}
				packet.ForeignReferences = append(packet.ForeignReferences, remoteForeignReference(packet, ref, purpose, profile.Values.Location, api.Time(retain)))
				if len(packet.ForeignReferences) > 100 || r.cfg.MaterialPrincipal != nil && len(packet.ForeignReferences)*2+1 > 100 {
					return api.E("overloaded", "remote_material_holder_limit")
				}
			}
		}
		if r.cfg.ScopeGate != nil {
			admission, err := r.cfg.ScopeGate.FreezeScopeTx(ctx, tx, actor, current, profile)
			if err != nil {
				return err
			}
			packet.ParentAdmission = &admission
		}
		if _, err = r.validatePacket(packet); err != nil {
			return err
		}
		out = remoteSent{Packet: packet, Command: api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: d.ReceiverID, CommandID: packet.CreateCommandID, Method: "collaboration.create", TargetID: d.CreationKey, ExpiresAt: d.Deadline, Payload: api.Raw(packet)}}
		return tx.Create(ctx, remoteOutgoing, id, d.ParentTaskRef.ObjectID, out)
	}()
	return out, err
}
func remoteForeignReference(p RemoteCreateInput, ref api.ContentRef, purpose, location, retain string) memory.ForeignReference {
	digest, _ := api.Digest(ref)
	id := remoteID("copy", p.DelegationRef.TenantID, p.DelegationRef.OwnerID, p.CreationKey, digest, purpose, location)
	return memory.ForeignReference{ContentRef: ref, CopyID: id, RegisterCommandID: remoteID("command", id, "register"), ReleaseCommandID: remoteID("command", id, "release"), ReferenceIntentRef: api.ObjectRef{TenantID: p.SubjectRef.TenantID, OwnerID: p.Input.ReceiverID, ObjectID: remoteID("intent", id), Revision: 1}, HolderRef: api.ObjectRef{TenantID: p.SubjectRef.TenantID, OwnerID: p.Input.ReceiverID, ObjectID: p.SubjectRef.ObjectID, Revision: p.SubjectRef.Revision}, Purpose: purpose, Location: location, RetainUntil: retain}
}
func (r *Remote) Create(ctx context.Context, scope runtime.Scope, d task.Delegation, a task.Allocation) (task.DelegationFact, error) {
	if err := r.checkScope(scope); err != nil {
		return task.DelegationFact{}, err
	}
	if d.ReceiverID == scope.OwnerID && r.cfg.Local != nil {
		return r.cfg.Local.Create(ctx, scope, d, a)
	}
	peer, ok := r.peers[d.ReceiverID]
	if !ok {
		return task.DelegationFact{}, api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	var saved remoteSent
	_, err := r.cfg.Store.Read(ctx, scope, remoteOutgoing, remoteID("handoff", scope.TenantID, scope.OwnerID, d.CreationKey), 0, &saved)
	if api.IsCode(err, "not_found") {
		saved, err = r.planCreate(ctx, d, a)
	}
	if err != nil {
		return task.DelegationFact{}, err
	}
	if !api.Equal(saved.Packet.Input, d.DelegateInput) || saved.Packet.AllocationRef != d.AllocationRef {
		return task.DelegationFact{}, api.E("idempotency_conflict", "original_remote_mapping_changed")
	}
	// 查询原回执早于新发送门禁；撤权不能删除原已接纳责任。
	entry, err := peer.Client.Journal.Read(ctx, saved.Command.CommandID)
	if err == nil {
		original, lookupErr := peer.Client.Receipt(ctx, saved.Command.CommandID)
		if lookupErr == nil {
			if original.Stage == "rejected" {
				return task.DelegationFact{}, &task.OriginalCommandRejection{Receipt: original}
			}
			return r.Read(ctx, scope, d)
		}
		if !api.IsCode(lookupErr, "not_found") {
			return task.DelegationFact{}, lookupErr
		}
	}
	if err == nil && entry.Receipt != nil {
		if entry.Receipt.Stage == "rejected" {
			return task.DelegationFact{}, &task.OriginalCommandRejection{Receipt: *entry.Receipt}
		}
		return r.Read(ctx, scope, d)
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) && !api.IsCode(err, "not_found") {
		return task.DelegationFact{}, err
	}
	s, err := r.service()
	if err != nil {
		return task.DelegationFact{}, err
	}
	if err = r.within(ctx, func(tx runtime.Tx) error {
		_, _, err := s.CheckDelegationTx(ctx, tx, r.cfg.Auth, d.DelegationID)
		return err
	}); err != nil {
		return task.DelegationFact{}, err
	}
	receipt, err := peer.Client.Send(ctx, saved.Command)
	if err != nil {
		return task.DelegationFact{}, err
	}
	if receipt.Stage == "rejected" {
		return task.DelegationFact{}, &task.OriginalCommandRejection{Receipt: receipt}
	}
	return r.Read(ctx, scope, d)
}
func (r *Remote) CreateSession(ctx context.Context, scope runtime.Scope, h task.ChildHandle) (api.ObjectRef, error) {
	if h.SessionOwnerID == scope.OwnerID && r.cfg.Local != nil {
		return r.cfg.Local.CreateSession(ctx, scope, h)
	}
	return r.createRemoteSession(ctx, scope, h)
}
func (r *Remote) ReadAllocation(ctx context.Context, scope runtime.Scope, ref api.ObjectRef) (task.Allocation, error) {
	if err := r.checkScope(scope); err != nil {
		return task.Allocation{}, err
	}
	if ref.OwnerID == scope.OwnerID {
		s, err := r.service()
		if err != nil {
			return task.Allocation{}, err
		}
		return s.AllocationRead(ctx, r.cfg.Store, scope, r.cfg.Auth, ref.ObjectID)
	}
	out, err := r.readRemoteAllocation(ctx, scope, ref)
	return out.Allocation, err
}

// 保留完整原签名，可供费用报告恢复持久核验接收事实；没有新的开始权。
func (r *Remote) readRemoteAllocation(ctx context.Context, scope runtime.Scope, ref api.ObjectRef) (RemoteAllocationReadOutput, error) {
	var out RemoteAllocationReadOutput
	if err := r.checkScope(scope); err != nil {
		return out, err
	}
	peer, ok := r.peers[ref.OwnerID]
	if !ok {
		return out, api.E("unsupported", "allocation_source_unconfigured")
	}
	raw, err := peer.Client.Query(ctx, api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: ref.OwnerID, QueryID: api.NewID("query"), Method: "collaboration.allocation.get", TargetID: ref.ObjectID, Payload: api.Raw(RemoteAllocationReadInput{ref})})
	if err != nil {
		return out, err
	}
	if err = api.Decode(raw, &out); err != nil {
		return out, err
	}
	digest, err := allocationReadDigest(out)
	if err != nil {
		return out, err
	}
	if out.SourceDatabaseID != peer.Scope.DatabaseID || out.Allocation.AllocationID != ref.ObjectID || out.Allocation.Revision < ref.Revision || out.Allocation.ParentTaskRef.OwnerID != ref.OwnerID || out.Allocation.ParentTaskRef.TenantID != scope.TenantID || out.Allocation.ReceiverID != scope.OwnerID {
		return out, api.E("forbidden", "original_allocation_scope_changed")
	}
	if _, err = peer.Keys.Verify(out.Proof, allocationReadClaims(peer.Scope, out, digest), time.Now()); err != nil {
		return out, err
	}
	issued, err := api.ParseTime(out.IssuedAt)
	if err != nil {
		return out, err
	}
	until, err := api.ParseTime(out.StartBefore)
	if err != nil {
		return out, err
	}
	if !issued.Before(until) || until.Sub(issued) > 30*time.Second {
		return out, api.E("forbidden", "allocation_window_exceeded")
	}
	return out, nil
}
func (r *Remote) Transfer(ctx context.Context, scope runtime.Scope, tr task.Transfer) error {
	return r.transferRemoteChild(ctx, scope, tr)
}

var _ task.CollaborationPort = (*Remote)(nil)
