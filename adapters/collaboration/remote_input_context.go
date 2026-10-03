package collaboration

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// PrepareInputContext 只为原已接纳命令的原输入取得当次范围；没有换owner或补授权。
// 宿主在显式 Content/Context端口（Tx外）调用；factory本身不调用它。
func (r *Remote) PrepareInputContext(ctx context.Context, commandID string) (context.Context, error) {
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
	prepared, err := r.PrepareChildContext(ctx, saved.Command.TargetID)
	if err != nil {
		return ctx, err
	}
	handoff, err := r.childOriginal(prepared, saved.Command.TargetID)
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
	var actor runtime.Auth
	var actual api.Task
	err = r.within(prepared, func(tx runtime.Tx) error {
		var err error
		actual, err = s.ReadTaskTx(prepared, tx, r.cfg.Auth, saved.Command.TargetID)
		if err != nil {
			return err
		}
		actor, err = r.cfg.Authority.ResolveSubjectTx(prepared, tx, saved.Packet.SubjectRef, profile, false)
		if err != nil {
			return err
		}
		return s.CheckTaskCurrentTx(prepared, tx, actor, actual.TaskID, false)
	})
	if err != nil {
		return ctx, err
	}
	return r.cfg.Memory.PrepareForeignContext(prepared, r.cfg.Scope, actor, []api.ContentRef{saved.Packet.Input.ContentRef, actual.GoalRef}, "task.goal", profile.Values.Location)
}

var _ task.CurrentTaskGate = (*Remote)(nil)
