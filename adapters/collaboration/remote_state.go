package collaboration

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const JobRemoteProof = "collaboration.remote_proof"

type RemoteState struct {
	ParentOwnerID        string                   `json:"parent_owner_id"`
	CreationKey          string                   `json:"creation_key"`
	PacketDigest         string                   `json:"packet_digest"`
	Fact                 task.DelegationFact      `json:"fact"`
	Task                 *api.Task                `json:"task,omitempty"`
	ResultRef            *api.ObjectRef           `json:"result_ref,omitempty"`
	AllocationClosure    *api.AllocationClosure   `json:"allocation_closure,omitempty"`
	AllocationClosureRef *api.ObjectRef           `json:"allocation_closure_ref,omitempty"`
	DelegationClosure    *task.DelegationClosure  `json:"delegation_closure,omitempty"`
	DelegationClosureRef *api.ObjectRef           `json:"delegation_closure_ref,omitempty"`
	TaskClosure          *task.ClosureView        `json:"task_closure,omitempty"`
	RejectedCreation     *RemoteRejectedCreation  `json:"rejected_creation,omitempty"`
	Incoming             *task.IncomingAllocation `json:"incoming,omitempty"`
	SourceDatabaseID     string                   `json:"source_database_id"`
	IssuedAt             string                   `json:"issued_at"`
	StartBefore          string                   `json:"start_before"`
	Proof                string                   `json:"proof"`
}

func remoteStateDigest(p RemoteState) (string, error) { p.Proof = ""; return api.Digest(p) }
func remoteStateClaims(scope runtime.Scope, p RemoteState, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: scope.TenantID, Issuer: scope.OwnerID, Audience: p.ParentOwnerID, Purpose: "agent_state", ObjectRef: scope.Ref(p.CreationKey, p.Fact.Revision), Digest: digest, WindowID: p.CreationKey, IssuedAt: p.IssuedAt, StartBefore: p.StartBefore}
}
func (r *Remote) state(ctx context.Context, peer runtime.Auth, q api.Query, in RemoteKeyInput) (RemoteState, error) {
	var out RemoteState
	if q.TargetID != in.CreationKey || !api.ValidID(in.ParentOwnerID) || !api.ValidID(in.CreationKey) {
		return out, api.E("invalid_request", "remote_key_invalid")
	}
	id := remoteID("handoff", r.cfg.Scope.TenantID, in.ParentOwnerID, in.CreationKey)
	var saved remoteReceived
	if _, err := r.cfg.Store.Read(ctx, r.cfg.Scope, remoteIncoming, id, 0, &saved); err != nil {
		return out, err
	}
	s, err := r.service()
	if err != nil {
		return out, err
	}
	var child *api.Task
	var closure task.ClosureView
	var inc task.IncomingAllocation
	if saved.ChildTaskRef != nil {
		t, err := s.Read(ctx, r.cfg.Store, r.cfg.Scope, r.cfg.Auth, saved.ChildTaskRef.ObjectID)
		if err != nil {
			return out, err
		}
		child = &t
		closure, err = s.Closure(ctx, r.cfg.Store, r.cfg.Scope, r.cfg.Auth, *saved.ChildTaskRef)
		if err != nil {
			return out, err
		}
		if closure.TaskRef.Revision != t.Revision {
			return out, api.E("snapshot_required", "child_state_changed")
		}
	}
	inc, err = s.IncomingRead(ctx, r.cfg.Store, r.cfg.Scope, saved.PeerAuth, saved.Packet.AllocationRef)
	if err != nil && !api.IsCode(err, "not_found") {
		return out, err
	}
	fact := task.DelegationFact{DelegationID: in.CreationKey, ChildTaskRef: saved.ChildTaskRef, Gaps: []string{}, TransfersClosed: true}
	if child == nil {
		if saved.Phase == "rejected" && inc.Gate == "closed" {
			fact.GoalWorkClosed = true
			fact.EffectsClosed = true
		} else {
			fact.Gaps = []string{"child_creation_pending"}
		}
	} else {
		fact.GoalWorkClosed = closure.GoalWorkClosed
		fact.EffectsClosed = closure.EffectsClosed
	}
	out = RemoteState{ParentOwnerID: in.ParentOwnerID, CreationKey: in.CreationKey, Fact: fact, Task: child, SourceDatabaseID: r.cfg.Scope.DatabaseID}
	if inc.AllocationID != "" {
		out.Incoming = &inc
	}
	out.PacketDigest, err = api.Digest(saved.Packet)
	if err != nil {
		return out, err
	}
	if child != nil && child.ResultRef != nil {
		out.ResultRef = child.ResultRef
	}
	if inc.ClosureRef != nil {
		c, err := s.AllocationClosureRead(ctx, r.cfg.Store, r.cfg.Scope, r.cfg.Auth, *inc.ClosureRef)
		if err != nil {
			return out, err
		}
		out.AllocationClosure = &c
		out.AllocationClosureRef = inc.ClosureRef
		usage := api.UsageSnapshot{SourceRef: r.cfg.Scope.Ref(c.AllocationID, c.UsageRevision), UsageRevision: c.UsageRevision, Cumulative: c.FinalUsage, SpendingClosed: c.SpendingClosed, UsageFinal: true, ProofRefs: []api.ContentRef{c.ProofRef}}
		usage.UsageDigest, err = task.UsageDigest(usage)
		if err != nil {
			return out, err
		}
		out.Fact.Usage = &usage
		out.Fact.UsageFinal = true
	}
	err = r.within(ctx, func(tx runtime.Tx) error {
		return task.WithJobIntents(ctx, tx, func(tx runtime.Tx) error {
			var rejected *task.RejectedIncomingClosure
			if child != nil {
				actual, err := s.ReadTaskTx(ctx, tx, r.cfg.Auth, child.TaskID)
				if err != nil {
					return err
				}
				if actual.Revision != child.Revision {
					return api.E("snapshot_required", "child_state_changed")
				}
			}
			if child == nil && saved.Phase == "rejected" && inc.Gate == "closed" {
				actual, err := s.SealRejectedIncomingTx(ctx, tx, saved.PeerAuth, saved.Packet.AllocationRef, saved.Packet.CreateCommandID)
				if err != nil {
					return err
				}
				rejected = &actual
				out.Incoming = &actual.Incoming
				out.AllocationClosure = &actual.Closure
				out.AllocationClosureRef = &actual.ClosureRef
				usage := api.UsageSnapshot{SourceRef: r.cfg.Scope.Ref(actual.Closure.AllocationID, actual.Closure.UsageRevision), UsageRevision: actual.Closure.UsageRevision, Cumulative: actual.Closure.FinalUsage, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{actual.Closure.ProofRef}}
				usage.UsageDigest, err = task.UsageDigest(usage)
				if err != nil {
					return err
				}
				out.Fact.Usage, out.Fact.UsageFinal = &usage, true
			} else if out.Incoming != nil {
				actual, err := s.ReadIncomingAllocationTx(ctx, tx, r.cfg.Auth, saved.Packet.AllocationRef)
				if err != nil {
					return err
				}
				if !api.Equal(actual, *out.Incoming) {
					return api.E("snapshot_required", "original_incoming_usage_changed")
				}
			}
			if err := r.cfg.Authority.CheckPeerTx(ctx, tx, peer, in.ParentOwnerID); err != nil {
				return err
			}
			var current remoteReceived
			rev, err := tx.Get(ctx, remoteIncoming, id, &current)
			if err != nil {
				return err
			}
			if current.Revision != saved.Revision {
				return api.E("snapshot_required", "original_child_mapping_changed")
			}
			if rejected != nil && (current.Phase != "rejected" || current.ChildTaskRef != nil || !api.Equal(current.Packet, saved.Packet) || !api.Equal(current.PeerAuth, saved.PeerAuth)) {
				return api.E("snapshot_required", "original_no_child_mapping_changed")
			}
			// 回答/恢复控制在实际原命令决定且本方责任收束前不能被称作 closed。
			// 所有登记沿同一 Task→domain 锁序，完整原集合最多各128条。
			for _, ns := range []string{remoteInputReceived, remotePendingControls} {
				rows, err := tx.List(ctx, ns, id, "", 129)
				if err != nil {
					return err
				}
				if len(rows) > 128 {
					return api.E("overloaded", "remote_transfer_responsibility_capacity")
				}
				for _, row := range rows {
					var transfer struct {
						Phase string `json:"phase"`
					}
					if err := row.Decode(&transfer); err != nil {
						return err
					}
					if transfer.Phase != "consumed" && transfer.Phase != "rejected" && transfer.Phase != "applied" {
						out.Fact.TransfersClosed = false
					}
				}
			}
			if !out.Fact.TransfersClosed {
				out.Fact.GoalWorkClosed = false
				out.Fact.Gaps = append(out.Fact.Gaps, "input_or_control_pending")
			}
			if rejected != nil {
				if err := r.sealRejectedDelegationTx(ctx, tx, id, saved.Packet, *rejected, &out); err != nil {
					return err
				}
			} else {
				if err := r.sealDelegationClosureTx(ctx, tx, id, saved.Packet, closure, &out); err != nil {
					return err
				}
			}
			semantic, err := api.Digest(out)
			if err != nil {
				return err
			}
			if current.FactRevision == 0 {
				current.FactRevision = 1
			}
			if current.FactDigest != semantic {
				current.FactDigest = semantic
				current.FactRevision++
				current.Revision++
				if err = tx.Put(ctx, remoteIncoming, id, rev, current); err != nil {
					return err
				}
			}
			out.Fact.Revision = current.FactRevision
			if err := r.validateStateUsage(saved.Packet, out); err != nil {
				return err
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			out.IssuedAt = api.Time(now)
			out.StartBefore = api.Time(now.Add(30 * time.Second))
			digest, err := remoteStateDigest(out)
			if err != nil {
				return err
			}
			out.Proof, err = r.cfg.Keys.Sign(r.cfg.SigningKeyID, remoteStateClaims(r.cfg.Scope, out, digest))
			return err
		})
	})
	return out, err
}
func (r *Remote) State(ctx context.Context, scope runtime.Scope, d task.Delegation) (RemoteState, error) {
	var out RemoteState
	if err := r.checkScope(scope); err != nil {
		return out, err
	}
	peer, ok := r.peers[d.ReceiverID]
	if !ok {
		return out, api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	var original remoteSent
	if _, err := r.cfg.Store.Read(ctx, scope, remoteOutgoing, remoteID("handoff", scope.TenantID, scope.OwnerID, d.CreationKey), 0, &original); err != nil {
		return out, err
	}
	raw, err := peer.Client.Query(ctx, api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: peer.Scope.OwnerID, QueryID: api.NewID("query"), Method: "collaboration.state", TargetID: d.CreationKey, Payload: api.Raw(RemoteKeyInput{scope.OwnerID, d.CreationKey})})
	if err != nil {
		return out, err
	}
	if err = api.Decode(raw, &out); err != nil {
		return out, err
	}
	digest, err := remoteStateDigest(out)
	if err != nil {
		return out, err
	}
	if _, err = peer.Keys.Verify(out.Proof, remoteStateClaims(peer.Scope, out, digest), time.Now()); err != nil {
		return out, err
	}
	packetDigest, err := api.Digest(original.Packet)
	if err != nil {
		return out, err
	}
	if out.ParentOwnerID != scope.OwnerID || out.CreationKey != d.CreationKey || out.SourceDatabaseID != peer.Scope.DatabaseID || out.PacketDigest != packetDigest || out.Fact.DelegationID != d.DelegationID || out.Fact.ChildTaskRef != nil && (out.Fact.ChildTaskRef.OwnerID != d.ReceiverID || out.Fact.ChildTaskRef.ObjectID != original.Packet.ChildTaskID) {
		return out, api.E("forbidden", "original_child_state_scope_mismatch")
	}
	issued, err := api.ParseTime(out.IssuedAt)
	if err != nil {
		return out, err
	}
	until, err := api.ParseTime(out.StartBefore)
	if err != nil || !issued.Before(until) || until.Sub(issued) > 30*time.Second {
		return out, api.E("forbidden", "remote_child_state_window_invalid")
	}
	if err = r.validateStateUsage(original.Packet, out); err != nil {
		return out, err
	}
	return out, nil
}
func (r *Remote) Read(ctx context.Context, scope runtime.Scope, d task.Delegation) (task.DelegationFact, error) {
	if d.ReceiverID == scope.OwnerID && r.cfg.Local != nil {
		return r.cfg.Local.Read(ctx, scope, d)
	}
	out, err := r.State(ctx, scope, d)
	if err != nil {
		return out.Fact, err
	}
	if err = r.observeStateUsage(ctx, d, out); err != nil {
		return out.Fact, err
	}
	return out.Fact, nil
}

// sealPublication 只保存准确签封字节及出版责任；实际ObjectStore在Job事务外。
func (r *Remote) sealPublication(ctx context.Context, tx runtime.Tx, parent string, claims platform.ProofClaims) (api.ContentRef, error) {
	if r.cfg.Memory == nil || r.cfg.ProofPolicy.PolicyRef.ComponentID == "" {
		return api.ContentRef{}, api.E("unsupported", "agent_proof_publication_unconfigured")
	}
	compact, err := r.cfg.Keys.Sign(r.cfg.SigningKeyID, claims)
	if err != nil {
		return api.ContentRef{}, err
	}
	body := []byte(compact)
	ref := api.ContentRef{TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, ContentID: remoteID("content", claims.Purpose, claims.Digest, api.Hash(body)), Version: 1, Hash: api.Hash(body), ByteLength: uint64(len(body)), MediaType: "application/jose"}
	now, err := tx.Now(ctx)
	if err != nil {
		return ref, err
	}
	retain, err := api.ParseTime(r.cfg.ProofPolicy.Values.RetainUntil)
	if err != nil {
		return ref, err
	}
	deadline := now.Add(time.Minute)
	if retain.Before(deadline) {
		deadline = retain
	}
	p := remoteProofPublication{Request: memory.PublicationRequest{ContentRef: ref, TransferID: remoteID("transfer", ref.ContentID), ReserveCommandID: remoteID("command", ref.ContentID, "reserve"), PutCommandID: remoteID("command", ref.ContentID, "put"), PolicyRef: r.cfg.ProofPolicy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(retain), TransferDeadline: api.Time(deadline)}, Bytes: body}
	if err = tx.Create(ctx, remoteProofs, ref.ContentID, parent, p); err != nil {
		return ref, err
	}
	if planner, ok := tx.(task.JobIntentPlanner); ok {
		err = planner.AddJobIntent(ctx, JobRemoteProof, "proof/"+ref.ContentID, tx.Scope().Ref(ref.ContentID, 1), now)
	} else {
		_, err = tx.Raise(ctx, JobRemoteProof, "proof/"+ref.ContentID, tx.Scope().Ref(ref.ContentID, 1), now)
	}
	return ref, err
}
func (r *Remote) SealClosureTx(ctx context.Context, tx runtime.Tx, view task.ClosureView) (api.ContentRef, error) {
	view.ProofRef = api.ContentRef{}
	digest, err := api.Digest(view)
	if err != nil {
		return api.ContentRef{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return api.ContentRef{}, err
	}
	return r.sealPublication(ctx, tx, view.TaskRef.ObjectID, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: tx.Scope().OwnerID, Purpose: "agent_state", ObjectRef: view.TaskRef, Digest: digest, WindowID: view.TaskRef.ObjectID, IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(30 * time.Second))})
}
func (r *Remote) SealAllocationClosureTx(ctx context.Context, tx runtime.Tx, c api.AllocationClosure) (api.ContentRef, error) {
	c.ProofRef = api.ContentRef{}
	digest, err := api.Digest(c)
	if err != nil {
		return api.ContentRef{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return api.ContentRef{}, err
	}
	return r.sealPublication(ctx, tx, c.AllocationID, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: c.ParentOwnerID, Purpose: "agent_state", ObjectRef: tx.Scope().Ref(c.AllocationID, c.UsageRevision), Digest: digest, WindowID: c.AllocationID, IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(30 * time.Second))})
}
func (r *Remote) proofJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	var p remoteProofPublication
	if _, err := store.Read(ctx, scope, remoteProofs, work.Job.SourceRef.ObjectID, 0, &p); err != nil {
		return err
	}
	if _, err := r.cfg.Memory.Upload(ctx, scope, r.cfg.Auth, p.Request, p.Bytes); err != nil {
		return err
	}
	return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), nil)
}
