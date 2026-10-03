package collaboration

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const (
	remoteSessionsSent     = "collaboration.remote_sessions_sent"
	remoteSessionsReceived = "collaboration.remote_sessions_received"
	JobRemoteSessionCreate = "collaboration.remote_session_create"
)

// RemoteSessionPacket 封闭认证 ingress，原 Session 命令的 ID、内容与期限不变。
// 外层方法只证明配对传输；真正 Session 仍由 Interaction 同事务唯一创建。
type RemoteSessionPacket struct {
	ParentOwnerID    string                  `json:"parent_owner_id"`
	ChildID          string                  `json:"child_id"`
	SubjectRef       api.ObjectRef           `json:"subject_ref"`
	Binding          RemoteSessionBinding    `json:"binding"`
	Command          api.Command             `json:"command"`
	ForeignReference memory.ForeignReference `json:"foreign_reference"`
}
type RemoteSessionOutput struct {
	CommandRef api.ObjectRef  `json:"command_ref"`
	Phase      string         `json:"phase"`
	SessionRef *api.ObjectRef `json:"session_ref,omitempty"`
}
type RemoteSessionReadInput struct {
	ParentOwnerID string `json:"parent_owner_id"`
	CommandID     string `json:"command_id"`
}
type remoteSessionIntent struct {
	Packet     RemoteSessionPacket `json:"packet"`
	Command    api.Command         `json:"command"`
	Phase      string              `json:"phase"`
	Peer       runtime.Auth        `json:"peer"`
	SessionRef *api.ObjectRef      `json:"session_ref,omitempty"`
}

func RemoteSessionContracts() []api.MethodContract {
	contracts := []api.MethodContract{api.Contract[RemoteSessionPacket, RemoteSessionOutput]("collaboration.session.create", "orchestrator", "command", false, true), api.Contract[RemoteSessionReadInput, RemoteSessionOutput]("collaboration.session.get", "orchestrator", "query", false, false)}
	for i := range contracts {
		contracts[i].SchemaDigest, _ = api.Digest([]any{contracts[i].InputSchema, contracts[i].OutputSchema})
	}
	return contracts
}
func (r *Remote) sessionAuthority() (RemoteSessionAuthority, error) {
	a, ok := r.cfg.Authority.(RemoteSessionAuthority)
	if !ok || len(a.RemoteSessionBindings()) == 0 || len(a.RemoteSessionBindings()) > 64 {
		return nil, api.E("unsupported", "remote_reusable_session_unconfigured")
	}
	return a, nil
}
func (r *Remote) sessionBinding(h task.ChildHandle) (RemoteSessionBinding, RemoteAgentProfile, error) {
	a, err := r.sessionAuthority()
	if err != nil {
		return RemoteSessionBinding{}, RemoteAgentProfile{}, err
	}
	for _, b := range a.RemoteSessionBindings() {
		p, err := r.profile(b.ProfileRef)
		if err != nil {
			return b, p, err
		}
		if p.Values.ParentOwnerID == r.cfg.Scope.OwnerID && p.Values.ReceiverID == h.SessionOwnerID && b.AgentBindingRef == h.AgentBindingRef && b.SessionConfigRef == h.SessionConfigRef && b.InstallLockRef == h.InstallLockRef && b.AccessScopeRef == h.AccessScopeRef {
			return b, p, nil
		}
	}
	return RemoteSessionBinding{}, RemoteAgentProfile{}, api.E("forbidden", "remote_session_binding_scope_exceeded")
}
func (r *Remote) sessionAdmissionTx(ctx context.Context, tx runtime.Tx, actor runtime.Auth, receiver string) error {
	a, err := r.sessionAuthority()
	if err != nil {
		return err
	}
	if _, ok := r.peers[receiver]; !ok {
		return api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	for _, b := range a.RemoteSessionBindings() {
		p, err := r.profile(b.ProfileRef)
		if err != nil {
			return err
		}
		if p.Values.ParentOwnerID == tx.Scope().OwnerID && p.Values.ReceiverID == receiver && subsetRefs([]api.ObjectRef{actor.Ref(tx.Scope().OwnerID)}, p.Values.SubjectRefs) {
			_, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, actor.Ref(tx.Scope().OwnerID), p, false)
			return err
		}
	}
	return api.E("forbidden", "remote_session_subject_unpaired")
}
func sessionForeignReference(p RemoteSessionPacket, receiver, location string) memory.ForeignReference {
	id := remoteID("copy", p.ParentOwnerID, p.Command.CommandID, "child.create")
	return memory.ForeignReference{ContentRef: p.Binding.AccessScopeRef, CopyID: id, RegisterCommandID: remoteID("command", id, "register"), ReleaseCommandID: remoteID("command", id, "release"), ReferenceIntentRef: api.ObjectRef{TenantID: p.SubjectRef.TenantID, OwnerID: receiver, ObjectID: remoteID("intent", id), Revision: 1}, HolderRef: api.ObjectRef{TenantID: p.SubjectRef.TenantID, OwnerID: receiver, ObjectID: p.SubjectRef.ObjectID, Revision: p.SubjectRef.Revision}, Purpose: "child.create", Location: location, RetainUntil: p.Command.ExpiresAt}
}
func (r *Remote) createRemoteSession(ctx context.Context, scope runtime.Scope, h task.ChildHandle) (api.ObjectRef, error) {
	if err := r.checkScope(scope); err != nil {
		return api.ObjectRef{}, err
	}
	peer, ok := r.peers[h.SessionOwnerID]
	if !ok {
		return api.ObjectRef{}, api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	var saved remoteSessionIntent
	_, err := r.cfg.Store.Read(ctx, scope, remoteSessionsSent, h.SessionCommandRef.ObjectID, 0, &saved)
	if api.IsCode(err, "not_found") {
		b, p, e := r.sessionBinding(h)
		if e != nil {
			return api.ObjectRef{}, e
		}
		if h.SessionCommandRef.OwnerID != h.SessionOwnerID || h.SessionCommandRef.TenantID != scope.TenantID || h.SessionCommandRef.Revision != 1 {
			return api.ObjectRef{}, api.E("forbidden", "remote_session_command_scope_changed")
		}
		actor := runtime.Auth{TenantID: scope.TenantID, SubjectID: h.SubjectID, CredentialGeneration: h.SubjectGeneration, Roles: append([]string{}, h.SubjectRoles...)}
		err = r.within(ctx, func(tx runtime.Tx) error {
			var actual task.ChildHandle
			if _, e := tx.Get(ctx, "task.children", h.ChildID, &actual); e != nil {
				return e
			}
			if !api.Equal(actual.ChildCreateInput, h.ChildCreateInput) || actual.SessionCommandRef != h.SessionCommandRef || actual.State != "preparing" && actual.State != "closed" || actual.SubjectID != actor.SubjectID || actual.SubjectGeneration != actor.CredentialGeneration {
				return api.E("invalid_state", "original_child_preparation_closed")
			}
			if _, e := r.cfg.Authority.ResolveSubjectTx(ctx, tx, actor.Ref(scope.OwnerID), p, false); e != nil {
				return e
			}
			if _, e := r.cfg.Memory.CheckContentTx(ctx, tx, actor, h.AccessScopeRef, "child.create", p.Values.Location, false); e != nil {
				return e
			}
			original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: h.SessionOwnerID, CommandID: h.SessionCommandRef.ObjectID, Method: "session.create", TargetID: h.SessionOwnerID, ExpiresAt: h.PrepareDeadline, Payload: api.Raw(interaction.CreateSessionInput{SessionID: remoteID("session", scope.OwnerID, h.SessionCommandRef.ObjectID), DefaultBranchID: remoteID("branch", scope.OwnerID, h.SessionCommandRef.ObjectID), ConfigRef: h.SessionConfigRef})}
			packet := RemoteSessionPacket{ParentOwnerID: scope.OwnerID, ChildID: h.ChildID, SubjectRef: actor.Ref(scope.OwnerID), Binding: b, Command: original}
			packet.ForeignReference = sessionForeignReference(packet, h.SessionOwnerID, p.Values.Location)
			wire := original
			wire.Method = "collaboration.session.create"
			wire.Payload = api.Raw(packet)
			saved = remoteSessionIntent{Packet: packet, Command: wire, Phase: "pending"}
			return tx.Create(ctx, remoteSessionsSent, original.CommandID, h.ChildID, saved)
		})
	}
	if err != nil {
		return api.ObjectRef{}, err
	}
	if saved.Packet.Command.CommandID != h.SessionCommandRef.ObjectID || saved.Packet.ChildID != h.ChildID || saved.Packet.Command.ExpiresAt != h.PrepareDeadline || saved.Packet.Binding.AccessScopeRef != h.AccessScopeRef {
		return api.ObjectRef{}, api.E("idempotency_conflict", "original_remote_session_changed")
	}
	receipt, err := sendOriginal(ctx, peer, saved.Command, func() error {
		return r.within(ctx, func(tx runtime.Tx) error {
			var actual task.ChildHandle
			if _, e := tx.Get(ctx, "task.children", h.ChildID, &actual); e != nil {
				return e
			}
			// close 只封新 send，原已接纳 Session 创建仍按原身份恢复。
			// Core 合并迟到结果时保留 closed，不重新打开 handle。
			if actual.State != "preparing" && actual.State != "closed" || actual.SessionCommandRef != h.SessionCommandRef {
				return api.E("invalid_state", "original_child_preparation_closed")
			}
			p, e := r.profile(saved.Packet.Binding.ProfileRef)
			if e != nil {
				return e
			}
			actor, e := r.cfg.Authority.ResolveSubjectTx(ctx, tx, saved.Packet.SubjectRef, p, false)
			if e != nil {
				return e
			}
			_, e = r.cfg.Memory.CheckContentTx(ctx, tx, actor, h.AccessScopeRef, "child.create", p.Values.Location, false)
			return e
		})
	})
	if err = knownReceipt(receipt, err); err != nil {
		return api.ObjectRef{}, err
	}
	var out RemoteSessionOutput
	if err = api.Decode(receipt.Output, &out); err != nil {
		return api.ObjectRef{}, err
	}
	if out.SessionRef == nil || out.CommandRef != h.SessionCommandRef || out.SessionRef.TenantID != scope.TenantID || out.SessionRef.OwnerID != h.SessionOwnerID || out.SessionRef.ObjectID != remoteID("session", scope.OwnerID, h.SessionCommandRef.ObjectID) || out.SessionRef.Revision != 1 {
		return api.ObjectRef{}, api.E("forbidden", "original_remote_session_receipt_changed")
	}
	return *out.SessionRef, nil
}
func (r *Remote) validateSessionPacket(p RemoteSessionPacket) (RemoteAgentProfile, error) {
	a, err := r.sessionAuthority()
	if err != nil {
		return RemoteAgentProfile{}, err
	}
	profile, err := r.profile(p.Binding.ProfileRef)
	if err != nil {
		return profile, err
	}
	matched := false
	for _, b := range a.RemoteSessionBindings() {
		matched = matched || api.Equal(b, p.Binding)
	}
	c := p.Command
	if !matched || p.ParentOwnerID != profile.Values.ParentOwnerID || profile.Values.ReceiverID != r.cfg.Scope.OwnerID || p.Binding.AgentBindingRef != profile.Values.AgentBindingRef || p.Binding.InstallLockRef != profile.Values.InstallLockRef || !subsetRefs([]api.ObjectRef{p.SubjectRef}, profile.Values.SubjectRefs) || !api.ValidID(p.ChildID) || c.Method != "session.create" || c.Protocol != api.Protocol || c.Profile != api.Profile || c.LogicalServiceID != r.cfg.Scope.OwnerID || c.TargetID != r.cfg.Scope.OwnerID || c.ExpectedRevision != nil || !api.ValidID(c.CommandID) {
		return profile, api.E("forbidden", "remote_session_binding_scope_exceeded")
	}
	var in interaction.CreateSessionInput
	if err = api.Decode(c.Payload, &in); err != nil {
		return profile, err
	}
	if in.ConfigRef != p.Binding.SessionConfigRef || in.SessionID != remoteID("session", p.ParentOwnerID, c.CommandID) || in.DefaultBranchID != remoteID("branch", p.ParentOwnerID, c.CommandID) || !api.Equal(p.ForeignReference, sessionForeignReference(p, r.cfg.Scope.OwnerID, profile.Values.Location)) {
		return profile, api.E("forbidden", "original_remote_session_changed")
	}
	return profile, nil
}
func (r *Remote) receiveSession(ctx context.Context, tx runtime.Tx, peer runtime.Auth, c api.Command, p RemoteSessionPacket) (runtime.Outcome, error) {
	profile, err := r.validateSessionPacket(p)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if c.CommandID != p.Command.CommandID || c.TargetID != p.Command.TargetID || c.ExpiresAt != p.Command.ExpiresAt {
		return runtime.Outcome{}, api.E("idempotency_conflict", "original_remote_session_command_changed")
	}
	if err = r.cfg.Authority.CheckPeerTx(ctx, tx, peer, p.ParentOwnerID); err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, p.SubjectRef, profile, false); err != nil {
		return runtime.Outcome{}, err
	}
	var saved remoteSessionIntent
	_, err = tx.Get(ctx, remoteSessionsReceived, c.CommandID, &saved)
	if err == nil {
		return runtime.Outcome{}, api.E("idempotency_conflict", "original_remote_session_exists")
	}
	if !api.IsCode(err, "not_found") {
		return runtime.Outcome{}, err
	}
	saved = remoteSessionIntent{Packet: p, Command: c, Peer: peer, Phase: "pending"}
	if err = tx.Create(ctx, remoteSessionsReceived, c.CommandID, p.ParentOwnerID, saved); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, JobRemoteSessionCreate, "session/"+c.CommandID, tx.Scope().Ref(c.CommandID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(RemoteSessionOutput{CommandRef: tx.Scope().Ref(c.CommandID, 1), Phase: "pending"}), nil
}
func (r *Remote) sessionRead(ctx context.Context, peer runtime.Auth, q api.Query, in RemoteSessionReadInput) (RemoteSessionOutput, error) {
	var out RemoteSessionOutput
	if q.TargetID != in.CommandID || !api.ValidID(in.CommandID) {
		return out, api.E("invalid_request", "session_query_target_mismatch")
	}
	err := r.within(ctx, func(tx runtime.Tx) error {
		if err := r.cfg.Authority.CheckPeerTx(ctx, tx, peer, in.ParentOwnerID); err != nil {
			return err
		}
		var saved remoteSessionIntent
		if _, err := tx.Get(ctx, remoteSessionsReceived, in.CommandID, &saved); err != nil {
			return err
		}
		if saved.Packet.ParentOwnerID != in.ParentOwnerID {
			return api.E("forbidden", "original_remote_session_owner_changed")
		}
		out = RemoteSessionOutput{CommandRef: tx.Scope().Ref(in.CommandID, 1), Phase: saved.Phase, SessionRef: saved.SessionRef}
		return nil
	})
	return out, err
}
func (r *Remote) sessionCreateJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	var saved remoteSessionIntent
	if _, err := store.Read(ctx, scope, remoteSessionsReceived, work.Job.SourceRef.ObjectID, 0, &saved); err != nil {
		return err
	}
	if saved.Phase != "pending" {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), nil)
	}
	profile, err := r.validateSessionPacket(saved.Packet)
	if err != nil {
		return err
	}
	var actor runtime.Auth
	err = r.within(ctx, func(tx runtime.Tx) error {
		if err := r.cfg.Authority.CheckPeerTx(ctx, tx, saved.Peer, saved.Packet.ParentOwnerID); err != nil {
			return err
		}
		var err error
		actor, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, saved.Packet.SubjectRef, profile, false)
		return err
	})
	if err != nil {
		return err
	}
	use, err := r.cfg.Memory.PrepareForeignUse(ctx, scope, actor, saved.Packet.ForeignReference)
	if err != nil {
		return err
	}
	prepared, err := memory.WithForeignUses(ctx, []memory.ForeignUse{use})
	if err != nil {
		return err
	}
	authority, err := r.sessionAuthority()
	if err != nil {
		return err
	}
	parts := append(append([]string{}, r.parts...), "interaction")
	return runtime.Finish(prepared, store, scope, parts, work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(prepared, saved.Command.CommandID); err != nil {
			return err
		}
		if err := r.cfg.Authority.CheckPeerTx(prepared, tx, saved.Peer, saved.Packet.ParentOwnerID); err != nil {
			return err
		}
		actor, err := r.cfg.Authority.ResolveSubjectTx(prepared, tx, saved.Packet.SubjectRef, profile, false)
		if err != nil {
			return err
		}
		if _, err = r.cfg.Memory.CheckContentTx(prepared, tx, actor, saved.Packet.Binding.AccessScopeRef, "child.create", profile.Values.Location, false); err != nil {
			return err
		}
		var current remoteSessionIntent
		rev, err := tx.Get(prepared, remoteSessionsReceived, saved.Command.CommandID, &current)
		if err != nil {
			return err
		}
		if !api.Equal(current.Packet, saved.Packet) || current.Phase != "pending" {
			return api.E("idempotency_conflict", "original_remote_session_changed")
		}
		var in interaction.CreateSessionInput
		if err = api.Decode(saved.Packet.Command.Payload, &in); err != nil {
			return err
		}
		result, err := authority.CreateRemoteSessionTx(prepared, tx, actor, saved.Packet.Command, in)
		if err != nil {
			return err
		}
		current.Phase = "applied"
		current.SessionRef = &result.SessionRef
		if err = tx.Put(prepared, remoteSessionsReceived, saved.Command.CommandID, rev, current); err != nil {
			return err
		}
		return runtime.Decide(prepared, tx, saved.Command.CommandID, RemoteSessionOutput{CommandRef: scope.Ref(saved.Command.CommandID, 1), Phase: "applied", SessionRef: &result.SessionRef}, nil)
	})
}
func (r *Remote) registerRemoteSessions() error {
	if _, err := r.sessionAuthority(); err != nil {
		return nil
	}
	methods := []runtime.Method{remoteCommandMethod[RemoteSessionPacket, RemoteSessionOutput](r, "collaboration.session.create", true, r.receiveSession), remoteQueryMethod[RemoteSessionReadInput, RemoteSessionOutput](r, "collaboration.session.get", r.sessionRead)}
	for _, m := range methods {
		if err := r.cfg.Registry.Register(m); err != nil {
			return err
		}
	}
	if err := r.registerRemoteChildTransfer(); err != nil {
		return err
	}
	return r.cfg.Registry.RegisterJob(JobRemoteSessionCreate, r.sessionCreateJob)
}
