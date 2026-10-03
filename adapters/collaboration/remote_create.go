package collaboration

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func (r *Remote) createOriginal(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, saved remoteReceived) error {
	p := saved.Packet
	if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	parent, err := r.readParent(ctx, p)
	if err != nil {
		return err
	}
	if !parent.Allowed {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Waiting(time.Now().Add(time.Second)), nil)
	}
	if r.cfg.Memory == nil {
		return api.E("unsupported", "foreign_goal_memory_unconfigured")
	}
	profile, err := r.profile(p.ProfileRef)
	if err != nil {
		return err
	}
	var actor runtime.Auth
	err = r.within(ctx, func(tx runtime.Tx) error {
		if err := r.cfg.Authority.CheckPeerTx(ctx, tx, saved.PeerAuth, p.DelegationRef.OwnerID); err != nil {
			return err
		}
		var err error
		actor, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, p.SubjectRef, profile, false)
		return err
	})
	if err != nil {
		return err
	}
	uses := []memory.ForeignUse{}
	for _, ref := range p.ForeignReferences {
		if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
			return err
		}
		use, err := r.cfg.Memory.PrepareForeignUse(ctx, scope, actor, ref)
		if err != nil {
			return err
		}
		uses = append(uses, use)
	}
	if r.cfg.MaterialPrincipal != nil {
		// 服务发布与原user读取是两个holder。每项仍先原意图落库，再让源
		// 以准确新holder/用途独立裁决；不继承user证明或刷新原保存期限。
		if len(p.ForeignReferences)*2+1 > 100 {
			return api.E("overloaded", "remote_material_holder_limit")
		}
		for _, original := range p.ForeignReferences {
			if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
				return err
			}
			ref := remoteMaterialReference(original, *r.cfg.MaterialPrincipal, scope)
			use, err := r.cfg.Memory.PrepareForeignUse(ctx, scope, *r.cfg.MaterialPrincipal, ref)
			if err != nil {
				return err
			}
			uses = append(uses, use)
		}
	}
	// 证明本身保留原 source owner；准确签封字节也由源端原副本合同读取。
	proofReference := remoteForeignReference(p, parent.ProofRef, "task.goal", profile.Values.Location, p.Input.Deadline)
	use, err := r.cfg.Memory.PrepareForeignUse(ctx, scope, actor, proofReference)
	if err != nil {
		return err
	}
	uses = append(uses, use)
	prepared, err := memory.WithForeignUses(ctx, uses)
	if err != nil {
		return err
	}
	proofBytes, err := r.cfg.Memory.ReadBytes(prepared, scope, actor, parent.ProofRef, "task.goal", profile.Values.Location)
	if err != nil {
		return err
	}
	if string(proofBytes) != parent.Proof {
		return api.E("forbidden", "parent_proof_bytes_changed")
	}
	goal, err := r.cfg.Memory.ReadBytes(prepared, scope, actor, p.Input.GoalRef, "task.goal", profile.Values.Location)
	if err != nil {
		return err
	}
	if api.Hash(goal) != p.Input.GoalRef.Hash || uint64(len(goal)) != p.Input.GoalRef.ByteLength {
		return api.E("invalid_request", "delegated_goal_bytes_mismatch")
	}
	peer := r.peers[p.DelegationRef.OwnerID]
	s, err := r.service()
	if err != nil {
		return err
	}
	return runtime.Finish(prepared, store, scope, r.parts, work, runtime.Done(), func(tx runtime.Tx) error {
		return task.WithJobIntents(prepared, tx, func(tx runtime.Tx) error {
			original, err := tx.LoadCommand(prepared, p.CreateCommandID)
			if err != nil {
				return err
			}
			if original.Receipt.Stage != "accepted" {
				return api.E("invalid_state", "remote_create_already_decided")
			}
			now, err := tx.Now(prepared)
			if err != nil {
				return err
			}
			if err = r.verifyParent(peer, p, parent, now, true); err != nil {
				return err
			}
			if !parent.Allowed {
				return api.E("forbidden", "parent_scope_closed")
			}
			if _, err = s.ReceiveAllocationTx(prepared, tx, saved.PeerAuth, p.AllocationRef, parent.Allocation); err != nil {
				return err
			}
			var control remoteControlGate
			controlRev, err := tx.Get(prepared, remoteControls, packetID(p), &control)
			if err != nil && !api.IsCode(err, "not_found") {
				return err
			}
			if control.Cancelled {
				return api.E("invalid_state", "remote_goal_closed")
			}
			if control.ParentPaused {
				if parent.ParentTask.Control != "running" || parent.ParentTask.ControlRevision <= control.ControlRevision {
					return api.E("dependency_unavailable", "remote_parent_paused")
				}
				control.ParentPaused = false
				if err = tx.Put(prepared, remoteControls, packetID(p), controlRev, control); err != nil {
					return err
				}
			}
			if err = r.cfg.Authority.CheckPeerTx(prepared, tx, saved.PeerAuth, p.DelegationRef.OwnerID); err != nil {
				return err
			}
			currentActor, err := r.cfg.Authority.ResolveSubjectTx(prepared, tx, p.SubjectRef, profile, false)
			if err != nil {
				return err
			}
			if currentActor.SubjectID != actor.SubjectID || currentActor.CredentialGeneration != actor.CredentialGeneration {
				return api.E("forbidden", "original_remote_subject_changed")
			}
			for _, ref := range append([]api.ContentRef{p.Input.GoalRef}, p.Input.InputRefs...) {
				if _, err = r.cfg.Memory.CheckContentTx(prepared, tx, currentActor, ref, "task.goal", profile.Values.Location, false); err != nil {
					return err
				}
			}
			window := api.ControlSnapshot{OrchestratorID: p.DelegationRef.OwnerID, TaskID: parent.ParentTask.TaskID, GoalRevision: parent.ParentTask.GoalRevision, ControlRevision: parent.ParentTask.ControlRevision, Status: parent.ParentTask.Status, Control: parent.ParentTask.Control, IssuedAt: parent.IssuedAt, StartBefore: parent.StartBefore, ProofRef: parent.ProofRef, WindowID: p.CreationKey}
			delegationContext := api.DelegationContext{DelegationID: p.CreationKey, ParentTaskRef: p.Input.ParentTaskRef, ParentGoalRevision: p.Input.ParentGoalRevision, AncestorTaskRefs: p.AncestorTaskRefs, AgentBindingRef: p.Input.AgentBindingRef, AllocationRef: p.AllocationRef, PermissionRefs: p.Input.PermissionRefs, ParentControlSnapshot: window, ParentProofRef: parent.ProofRef}
			delegationContext.ContextDigest, err = api.Digest(delegationContext)
			if err != nil {
				return err
			}
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: p.CreateCommandID, Method: "task.submit", TargetID: p.ChildTaskID, ExpiresAt: p.Input.Deadline}
			in := task.SubmitInput{OrchestratorID: scope.OwnerID, GoalRef: p.Input.GoalRef, PolicyRef: p.Input.PolicyRef, Deadline: p.Input.Deadline, Budget: p.Input.Budget, DelegationContext: &delegationContext, SourceSubmissionRef: &p.OriginalCommandRef}
			out, err := s.SubmitTx(prepared, tx, currentActor, command, in)
			if err != nil {
				return err
			}
			var current remoteReceived
			rev, err := tx.Get(prepared, remoteIncoming, packetID(p), &current)
			if err != nil {
				return err
			}
			if !api.Equal(current.Packet, p) || current.Phase != "preparing" {
				return api.E("idempotency_conflict", "original_child_mapping_changed")
			}
			current.Phase = "applied"
			current.ChildTaskRef = &out.TaskRef
			current.ParentSnapshot = &parent
			current.Revision++
			if err = tx.Put(prepared, remoteIncoming, packetID(p), rev, current); err != nil {
				return err
			}
			if err = tx.Create(prepared, "collaboration.remote_children", p.ChildTaskID, packetID(p), struct {
				PacketID string `json:"packet_id"`
			}{packetID(p)}); err != nil {
				return err
			}
			return runtime.Decide(prepared, tx, p.CreateCommandID, RemoteCreateOutput{p.CreationKey, "applied", &out.TaskRef}, nil)
		})
	})
}

func remoteMaterialReference(original memory.ForeignReference, actor runtime.Auth, scope runtime.Scope) memory.ForeignReference {
	holder := actor.Ref(scope.OwnerID)
	digest, _ := api.Digest(holder)
	ref := original
	ref.CopyID = remoteID("copy", original.CopyID, digest)
	ref.RegisterCommandID = remoteID("command", ref.CopyID, "register")
	ref.ReleaseCommandID = remoteID("command", ref.CopyID, "release")
	ref.ReferenceIntentRef = scope.Ref(remoteID("intent", ref.CopyID), 1)
	ref.HolderRef = holder
	return ref
}
