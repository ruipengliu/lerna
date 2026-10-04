package collaboration

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// PrepareInputPublicationContext 为原 steer/clarify_goal 的文档发布取得独立 service 许可。
// user 的读取证明不能授权发布者；只使用宿主已固定的 MaterialPrincipal。
// 调用方先 PrepareInputContext；本方法不创造父范围或刷新原引用保存期限。
func (r *Remote) PrepareInputPublicationContext(ctx context.Context, commandID string) (context.Context, error) {
	var origin remoteInputOrigin
	if _, err := r.cfg.Store.Read(ctx, r.cfg.Scope, remoteInputCommands, commandID, 1, &origin); err != nil {
		return ctx, err
	}
	var saved remoteReceivedInput
	if _, err := r.cfg.Store.Read(ctx, r.cfg.Scope, remoteInputReceived, origin.TransferID, 0, &saved); err != nil {
		return ctx, err
	}
	if saved.Command.CommandID != commandID || saved.Phase != "forwarded" {
		return ctx, api.E("invalid_state", "original_remote_input_not_forwarded")
	}
	if saved.Packet.Input.Kind != "steer" && saved.Packet.Input.Kind != "answer_request" {
		return ctx, nil
	}
	if r.cfg.MaterialPrincipal == nil || r.cfg.Memory == nil || !api.Equal(*r.cfg.MaterialPrincipal, r.cfg.Auth) {
		return ctx, api.E("unsupported", "remote_input_material_principal_unconfigured")
	}
	handoff, err := r.childOriginal(ctx, saved.Command.TargetID)
	if err != nil {
		return ctx, err
	}
	profile, err := r.profile(handoff.Packet.ProfileRef)
	if err != nil {
		return ctx, err
	}
	s, err := r.service()
	if err != nil {
		return ctx, err
	}
	var actual api.Task
	var actor runtime.Auth
	var reference memory.ForeignReference
	needsPublication := saved.Packet.Input.Kind == "steer"
	err = r.within(ctx, func(tx runtime.Tx) error {
		original, err := tx.LoadCommand(ctx, commandID)
		if err != nil {
			return err
		}
		if !api.Equal(original.Command, saved.Command) || original.Receipt.Stage != "accepted" || !api.Equal(saved.Command.Payload, api.Raw(saved.Packet)) {
			return api.E("idempotency_conflict", "original_remote_input_publication_changed")
		}
		actual, err = s.ReadTaskTreeTx(ctx, tx, r.cfg.Auth, saved.Command.TargetID)
		if err != nil {
			return err
		}
		actor, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, saved.Packet.SubjectRef, profile, false)
		if err != nil {
			return err
		}
		var current remoteReceivedInput
		if _, err = tx.Get(ctx, remoteInputReceived, origin.TransferID, &current); err != nil {
			return err
		}
		if original.PrincipalID != saved.Peer.SubjectID || current.Phase != "forwarded" || !api.Equal(current.Command, saved.Command) || !api.Equal(current.Packet, saved.Packet) || !api.Equal(current.Peer, saved.Peer) {
			return api.E("idempotency_conflict", "original_remote_input_publication_changed")
		}
		if err = r.cfg.Authority.CheckPeerTx(ctx, tx, saved.Peer, saved.Packet.ParentOwnerID); err != nil {
			return err
		}
		if saved.Packet.SubjectRef != handoff.Packet.SubjectRef || saved.Packet.CreationKey != handoff.Packet.CreationKey || saved.Packet.ParentOwnerID != handoff.Packet.DelegationRef.OwnerID || actual.GoalRevision != saved.Packet.Input.ChildGoalRevision || saved.Packet.SourceSubmissionRef.OwnerID != saved.Packet.ParentOwnerID {
			return api.E("forbidden", "original_remote_input_publication_scope_changed")
		}
		if err = s.CheckTaskCurrentTx(ctx, tx, actor, actual.TaskID, false); err != nil {
			return err
		}
		if err = r.CheckTaskCurrentTx(ctx, tx, actual, false); err != nil {
			return err
		}
		if saved.Packet.Input.Kind == "answer_request" {
			if saved.Packet.Input.RequestRef == nil {
				return api.E("invalid_request", "original_answer_request_missing")
			}
			view, err := s.RequestViewTx(ctx, tx, actor, *saved.Packet.Input.RequestRef)
			if err != nil {
				return err
			}
			if view.RequestRef != *saved.Packet.Input.RequestRef || view.Request.TargetRef.ObjectID != actual.TaskID || view.Request.GoalRevision == nil || *view.Request.GoalRevision != actual.GoalRevision || view.Request.State != "pending" {
				return api.E("revision_conflict", "original_answer_request_changed")
			}
			needsPublication = view.Request.Purpose == "clarify_goal"
			if !needsPublication {
				return nil
			}
		}
		allowed := false
		for _, purpose := range profile.Values.MaterialPurposes {
			allowed = allowed || purpose == "content.write"
		}
		if !allowed {
			return api.E("forbidden", "original_remote_input_publication_purpose_unconfigured")
		}
		count := 0
		for _, original := range saved.Packet.ForeignReferences {
			if original.Purpose != "content.write" {
				continue
			}
			expected := remoteForeignReference(handoff.Packet, saved.Packet.Input.ContentRef, original.Purpose, profile.Values.Location, original.RetainUntil)
			until, timeErr := api.ParseTime(original.RetainUntil)
			deadline, deadlineErr := api.ParseTime(handoff.Packet.Input.Deadline)
			if !api.Equal(original, expected) || original.HolderRef != actor.Ref(tx.Scope().OwnerID) || timeErr != nil || deadlineErr != nil || until.After(deadline) {
				return api.E("forbidden", "original_remote_input_publication_reference_changed")
			}
			reference = remoteMaterialReference(original, *r.cfg.MaterialPrincipal, tx.Scope())
			count++
		}
		if count != 1 {
			return api.E("forbidden", "original_remote_input_publication_reference_unconfigured")
		}
		return nil
	})
	if err != nil || !needsPublication {
		return ctx, err
	}
	use, err := r.cfg.Memory.PrepareForeignUse(ctx, r.cfg.Scope, *r.cfg.MaterialPrincipal, reference)
	if err != nil {
		return ctx, err
	}
	prepared, err := memory.WithForeignUses(ctx, []memory.ForeignUse{use})
	if err != nil {
		return ctx, err
	}
	prepared, err = r.cfg.Memory.PrepareForeignContext(prepared, r.cfg.Scope, *r.cfg.MaterialPrincipal, []api.ContentRef{saved.Packet.Input.ContentRef, actual.GoalRef}, "content.write", profile.Values.Location)
	if err != nil {
		return ctx, err
	}
	if saved.Packet.Input.Kind == "answer_request" {
		// 最终消费调用的是原用户的 task.input；发布者 content.write 证明
		// 不能代替它。此处只沿原已登记 holder 取得本次 Current，不增用途。
		prepared, err = r.cfg.Memory.PrepareForeignContext(prepared, r.cfg.Scope, actor, []api.ContentRef{saved.Packet.Input.ContentRef}, "task.input", profile.Values.Location)
		if err != nil {
			return ctx, err
		}
	}
	// 多个源 Current 或慢介质之后刷新同一个原父证明，不借 disk 旧许可。
	return r.PrepareChildContext(prepared, saved.Command.TargetID)
}
