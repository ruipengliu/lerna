package collaboration

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// ChildSessionHistoryAuth为这次原历史读取解析当前domain actor；compiler/service
// 不提供Session主体身份。当前父证明在事务外准备，事务内只核原身份和当前门禁。
func (r *Remote) ChildSessionHistoryAuth(ctx context.Context, scope runtime.Scope, actual api.Task) (context.Context, runtime.Auth, error) {
	if err := r.checkScope(scope); err != nil {
		return ctx, runtime.Auth{}, err
	}
	birth, err := r.childOriginal(ctx, actual.TaskID)
	if err != nil {
		return ctx, runtime.Auth{}, err
	}
	if birth.Packet.SessionContext == nil {
		return ctx, runtime.Auth{}, api.E("invalid_state", "original_child_session_context_required")
	}
	profile, err := r.profile(birth.Packet.ProfileRef)
	if err != nil {
		return ctx, runtime.Auth{}, err
	}
	if _, err = r.validatePacket(birth.Packet); err != nil {
		return ctx, runtime.Auth{}, err
	}
	// 沿原creation_key取得当前sourceScope；不换原send/cutoff、Goal、holder或期限。
	prepared, err := r.PrepareChildContext(ctx, actual.TaskID)
	if err != nil {
		return ctx, runtime.Auth{}, err
	}
	s, err := r.service()
	if err != nil {
		return ctx, runtime.Auth{}, err
	}
	var actor runtime.Auth
	err = r.within(prepared, func(tx runtime.Tx) error {
		current, err := s.ReadTaskTreeTx(prepared, tx, r.cfg.Auth, actual.TaskID)
		if err != nil {
			return err
		}
		if current.Revision != actual.Revision || current.GoalRevision != actual.GoalRevision || current.GoalRef != actual.GoalRef || current.ControlRevision != actual.ControlRevision {
			return api.E("revision_conflict", "original_child_history_task_changed")
		}
		var origin remoteChildOrigin
		if err = tx.GetVersion(prepared, "collaboration.remote_children", actual.TaskID, 1, &origin); err != nil {
			return err
		}
		var saved remoteReceived
		if _, err = tx.Get(prepared, remoteIncoming, origin.PacketID, &saved); err != nil {
			return err
		}
		if !api.Equal(saved.Packet, birth.Packet) || saved.ChildTaskRef == nil || saved.ChildTaskRef.ObjectID != actual.TaskID || saved.ChildTaskRef.OwnerID != scope.OwnerID {
			return api.E("idempotency_conflict", "original_child_history_actor_mapping_changed")
		}
		actor, err = r.cfg.Authority.ResolveSubjectTx(prepared, tx, saved.Packet.SubjectRef, profile, false)
		if err != nil {
			return err
		}
		if err = s.CheckTaskCurrentTx(prepared, tx, actor, current.TaskID, true); err != nil {
			return err
		}
		return r.CheckTaskCurrentTx(prepared, tx, current, true)
	})
	if err != nil {
		return ctx, runtime.Auth{}, err
	}
	return prepared, actor, nil
}
