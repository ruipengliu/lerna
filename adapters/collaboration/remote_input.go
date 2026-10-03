package collaboration

import (
	"context"
	"errors"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const (
	JobRemoteInputSend    = "collaboration.remote_input_send"
	JobRemoteInputReceive = "collaboration.remote_input_receive"
	remoteInputSent       = "collaboration.remote_input_sent"
	remoteInputReceived   = "collaboration.remote_input_received"
	remoteInputHeads      = "collaboration.remote_input_heads"
	remoteInputCommands   = "collaboration.remote_input_commands"
)

// RemoteInputSend 固定原子任务目标版本；回答只能消费原请求。
type RemoteInputSend struct {
	TransferID         string         `json:"transfer_id"`
	DelegationID       string         `json:"delegation_id"`
	ParentGoalRevision uint64         `json:"parent_goal_revision"`
	ChildGoalRevision  uint64         `json:"child_goal_revision"`
	Kind               string         `json:"kind"`
	ContentRef         api.ContentRef `json:"content_ref"`
	RequestRef         *api.ObjectRef `json:"request_ref,omitempty"`
}
type RemoteInputPacket struct {
	ParentOwnerID       string                    `json:"parent_owner_id"`
	CreationKey         string                    `json:"creation_key"`
	Input               RemoteInputSend           `json:"input"`
	SubjectRef          api.ObjectRef             `json:"subject_ref"`
	SourceSubmissionRef api.ObjectRef             `json:"source_submission_ref"`
	ExpiresAt           string                    `json:"expires_at"`
	ForeignReferences   []memory.ForeignReference `json:"foreign_references"`
}
type RemoteInputAck struct {
	TransferID         string         `json:"transfer_id"`
	Phase              string         `json:"phase"`
	ReceiverCommandRef *api.ObjectRef `json:"receiver_command_ref,omitempty"`
}
type remoteSentInput struct {
	Command           api.Command       `json:"command"`
	Packet            RemoteInputPacket `json:"packet"`
	OriginalCommandID string            `json:"original_command_id"`
	ParentTaskID      string            `json:"parent_task_id"`
	Actor             runtime.Auth      `json:"actor"`
	Phase             string            `json:"phase"`
}
type remoteReceivedInput struct {
	Command api.Command       `json:"command"`
	Packet  RemoteInputPacket `json:"packet"`
	Peer    runtime.Auth      `json:"peer"`
	Phase   string            `json:"phase"`
}
type remoteInputHead struct {
	Count uint64 `json:"count"`
}
type remoteInputOrigin struct {
	TransferID string `json:"transfer_id"`
}

func remoteInputContract() api.MethodContract {
	c := api.Contract[RemoteInputPacket, RemoteInputAck]("collaboration.input", "orchestrator", "command", false, true)
	// 原领域 Job 使用同一命令决定原回答/steer；没有替代命令或伪消费回执。
	c.OutputSchema = api.Schema{"oneOf": []any{api.SchemaFor[RemoteInputAck](), api.SchemaFor[task.InputOutput](), api.SchemaFor[task.TaskOutput]()}}
	c.SchemaDigest, _ = api.Digest([]any{c.InputSchema, c.OutputSchema})
	return c
}

func validRemoteInput(in RemoteInputSend, owner, tenant string) error {
	if !api.ValidID(in.TransferID) || !api.ValidID(in.DelegationID) || in.ParentGoalRevision == 0 || in.ChildGoalRevision == 0 || api.ValidateRecord("ContentRef", in.ContentRef) != nil || in.ContentRef.TenantID != tenant || in.ContentRef.ByteLength > memory.MaxContentBytes {
		return api.E("invalid_request", "remote_input_invalid")
	}
	switch in.Kind {
	case "steer":
		if in.RequestRef != nil {
			return api.E("invalid_request", "steer_has_request")
		}
	case "answer_request":
		if in.RequestRef == nil || api.ValidateRecord("ObjectRef", *in.RequestRef) != nil || in.RequestRef.OwnerID != owner || in.RequestRef.TenantID != tenant {
			return api.E("invalid_request", "remote_request_scope_invalid")
		}
	default:
		return api.E("unsupported", "remote_input_kind_unconfigured")
	}
	return nil
}

func (r *Remote) planInputTx(ctx context.Context, tx runtime.Tx, actor runtime.Auth, original api.Command, in RemoteInputSend, source api.ObjectRef) (remoteSentInput, error) {
	var out remoteSentInput
	s, err := r.service()
	if err != nil {
		return out, err
	}
	current, err := s.CheckDelegationScopeTx(ctx, tx, actor, in.DelegationID)
	if err != nil {
		return out, err
	}
	d := current.Delegation
	if err = validRemoteInput(in, d.ReceiverID, tx.Scope().TenantID); err != nil {
		return out, err
	}
	if d.ParentGoalRevision != in.ParentGoalRevision || d.ChildTaskRef == nil {
		return out, api.E("revision_conflict", "remote_input_parent_or_child_changed")
	}
	if source.OwnerID != tx.Scope().OwnerID || source.TenantID != tx.Scope().TenantID || api.ValidateRecord("ObjectRef", source) != nil {
		return out, api.E("forbidden", "remote_input_source_invalid")
	}
	var handoff remoteSent
	if _, err = tx.Get(ctx, remoteOutgoing, remoteID("handoff", tx.Scope().TenantID, tx.Scope().OwnerID, d.CreationKey), &handoff); err != nil {
		return out, err
	}
	profile, err := r.profile(handoff.Packet.ProfileRef)
	if err != nil {
		return out, err
	}
	if !subsetRefs([]api.ObjectRef{actor.Ref(tx.Scope().OwnerID)}, profile.Values.SubjectRefs) {
		return out, api.E("forbidden", "remote_input_subject_unpaired")
	}
	if _, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, actor.Ref(tx.Scope().OwnerID), profile, false); err != nil {
		return out, err
	}
	version, err := r.cfg.Memory.CheckContentTx(ctx, tx, actor, in.ContentRef, "task.goal", profile.Values.Location, false)
	if err != nil {
		return out, err
	}
	packet := RemoteInputPacket{ParentOwnerID: tx.Scope().OwnerID, CreationKey: d.CreationKey, Input: in, SubjectRef: actor.Ref(tx.Scope().OwnerID), SourceSubmissionRef: source, ExpiresAt: original.ExpiresAt, ForeignReferences: []memory.ForeignReference{}}
	retain, err := api.ParseTime(version.RetentionUntil)
	if err != nil {
		return out, err
	}
	deadline, err := api.ParseTime(d.Deadline)
	if err != nil {
		return out, err
	}
	if deadline.Before(retain) {
		retain = deadline
	}
	for _, purpose := range profile.Values.MaterialPurposes {
		if _, err = r.cfg.Memory.CheckContentTx(ctx, tx, actor, in.ContentRef, purpose, profile.Values.Location, false); err != nil {
			return out, err
		}
		packet.ForeignReferences = append(packet.ForeignReferences, remoteForeignReference(handoff.Packet, in.ContentRef, purpose, profile.Values.Location, api.Time(retain)))
	}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: d.ReceiverID, CommandID: remoteID("command", original.CommandID, "input"), Method: "collaboration.input", TargetID: handoff.Packet.ChildTaskID, ExpiresAt: original.ExpiresAt, Payload: api.Raw(packet)}
	out = remoteSentInput{Command: command, Packet: packet, OriginalCommandID: original.CommandID, ParentTaskID: d.ParentTaskRef.ObjectID, Actor: actor, Phase: "pending"}
	var old remoteSentInput
	_, err = tx.Get(ctx, remoteInputSent, in.TransferID, &old)
	if err == nil {
		if !api.Equal(old.Packet, packet) || old.OriginalCommandID != original.CommandID || !api.Equal(old.Actor, actor) {
			return out, api.E("idempotency_conflict", "original_remote_input_changed")
		}
		return old, nil
	}
	if !api.IsCode(err, "not_found") {
		return out, err
	}
	if err = incrementInputTx(ctx, tx, "send/"+packetID(handoff.Packet)); err != nil {
		return out, err
	}
	err = tx.Create(ctx, remoteInputSent, in.TransferID, d.ParentTaskRef.ObjectID, out)
	return out, err
}
func incrementInputTx(ctx context.Context, tx runtime.Tx, key string) error {
	var h remoteInputHead
	rev, err := tx.Get(ctx, remoteInputHeads, key, &h)
	if err != nil && !api.IsCode(err, "not_found") {
		return err
	}
	if h.Count >= 128 {
		return api.E("overloaded", "remote_input_capacity")
	}
	h.Count++
	if rev == 0 {
		return tx.Create(ctx, remoteInputHeads, key, key, h)
	}
	return tx.Put(ctx, remoteInputHeads, key, rev, h)
}
func (r *Remote) sendInput(ctx context.Context, tx runtime.Tx, actor runtime.Auth, c api.Command, in RemoteInputSend) (runtime.Outcome, error) {
	if c.TargetID != in.TransferID {
		return runtime.Outcome{}, api.E("invalid_request", "remote_transfer_target_invalid")
	}
	saved, err := r.planInputTx(ctx, tx, actor, c, in, tx.Scope().Ref(c.CommandID, 1))
	if err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, JobRemoteInputSend, "input/"+in.TransferID, tx.Scope().Ref(in.TransferID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	ref := api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: saved.Command.LogicalServiceID, ObjectID: saved.Command.CommandID, Revision: 1}
	return runtime.Accepted(RemoteInputAck{TransferID: in.TransferID, Phase: "pending", ReceiverCommandRef: &ref}), nil
}
func (r *Remote) receiveInput(ctx context.Context, tx runtime.Tx, peer runtime.Auth, c api.Command, p RemoteInputPacket) (runtime.Outcome, error) {
	id := remoteID("handoff", tx.Scope().TenantID, p.ParentOwnerID, p.CreationKey)
	var birth remoteReceived
	if err := tx.GetVersion(ctx, remoteIncoming, id, 1, &birth); err != nil {
		return runtime.Outcome{}, err
	}
	s, err := r.service()
	if err != nil {
		return runtime.Outcome{}, err
	}
	actual, err := s.ReadTaskTreeTx(ctx, tx, r.cfg.Auth, birth.Packet.ChildTaskID)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err = validRemoteInput(p.Input, tx.Scope().OwnerID, tx.Scope().TenantID); err != nil {
		return runtime.Outcome{}, err
	}
	if c.TargetID != actual.TaskID || c.ExpiresAt != p.ExpiresAt || p.Input.DelegationID != p.CreationKey || p.Input.ParentGoalRevision != birth.Packet.Input.ParentGoalRevision || p.SubjectRef != birth.Packet.SubjectRef || p.SourceSubmissionRef.OwnerID != p.ParentOwnerID || p.SourceSubmissionRef.TenantID != tx.Scope().TenantID || api.ValidateRecord("ObjectRef", p.SourceSubmissionRef) != nil {
		return runtime.Outcome{}, api.E("forbidden", "remote_input_scope_changed")
	}
	if err = r.cfg.Authority.CheckPeerTx(ctx, tx, peer, p.ParentOwnerID); err != nil {
		return runtime.Outcome{}, err
	}
	profile, err := r.profile(birth.Packet.ProfileRef)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, p.SubjectRef, profile, false); err != nil {
		return runtime.Outcome{}, err
	}
	if actual.Status != "active" || actual.GoalRevision != p.Input.ChildGoalRevision {
		return runtime.Outcome{}, api.E("revision_conflict", "remote_input_child_goal_changed")
	}
	var control remoteControlGate
	_, err = tx.Get(ctx, remoteControls, id, &control)
	if err != nil && !api.IsCode(err, "not_found") {
		return runtime.Outcome{}, err
	}
	if control.Cancelled {
		return runtime.Outcome{}, api.E("invalid_state", "remote_goal_closed")
	}
	// 仅接纳传输责任；新使用必须由 Job 在线取得当前父范围。
	if len(p.ForeignReferences) != len(profile.Values.MaterialPurposes) {
		return runtime.Outcome{}, api.E("forbidden", "remote_input_copy_scope_invalid")
	}
	seen := map[string]bool{}
	for _, f := range p.ForeignReferences {
		expected := remoteForeignReference(birth.Packet, p.Input.ContentRef, f.Purpose, profile.Values.Location, f.RetainUntil)
		if !api.Equal(f, expected) || seen[f.Purpose] {
			return runtime.Outcome{}, api.E("forbidden", "remote_input_copy_scope_invalid")
		}
		allowed := false
		for _, purpose := range profile.Values.MaterialPurposes {
			if f.Purpose == purpose {
				allowed = true
			}
		}
		if !allowed {
			return runtime.Outcome{}, api.E("forbidden", "remote_input_copy_purpose_invalid")
		}
		seen[f.Purpose] = true
	}
	var old remoteReceivedInput
	_, err = tx.Get(ctx, remoteInputReceived, p.Input.TransferID, &old)
	if err == nil {
		if !api.Equal(old.Packet, p) || old.Command.CommandID != c.CommandID || !api.Equal(old.Peer, peer) {
			return runtime.Outcome{}, api.E("idempotency_conflict", "original_remote_input_changed")
		}
		ref := tx.Scope().Ref(c.CommandID, 1)
		return runtime.Accepted(RemoteInputAck{TransferID: p.Input.TransferID, Phase: old.Phase, ReceiverCommandRef: &ref}), nil
	}
	if !api.IsCode(err, "not_found") {
		return runtime.Outcome{}, err
	}
	if err = incrementInputTx(ctx, tx, "receive/"+id); err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Create(ctx, remoteInputReceived, p.Input.TransferID, id, remoteReceivedInput{Command: c, Packet: p, Peer: peer, Phase: "pending"}); err != nil {
		return runtime.Outcome{}, err
	}
	if err = tx.Create(ctx, remoteInputCommands, c.CommandID, id, remoteInputOrigin{p.Input.TransferID}); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, JobRemoteInputReceive, "input/"+p.Input.TransferID, tx.Scope().Ref(p.Input.TransferID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	ref := tx.Scope().Ref(c.CommandID, 1)
	return runtime.Accepted(RemoteInputAck{TransferID: p.Input.TransferID, Phase: "pending", ReceiverCommandRef: &ref}), nil
}

func (r *Remote) inputSendJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	var saved remoteSentInput
	if _, err := store.Read(ctx, scope, remoteInputSent, work.Job.SourceRef.ObjectID, 0, &saved); err != nil {
		return err
	}
	if saved.Phase != "pending" {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), nil)
	}
	peer, ok := r.peers[saved.Command.LogicalServiceID]
	if !ok {
		return api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	receipt, err := sendOriginal(ctx, peer, saved.Command, func() error {
		if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
			return err
		}
		return r.within(ctx, func(tx runtime.Tx) error {
			_, _, err := s.CheckDelegationTx(ctx, tx, saved.Actor, saved.Packet.Input.DelegationID)
			return err
		})
	})
	if err != nil {
		return err
	}
	if receipt.Stage == "accepted" {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Waiting(time.Now().Add(time.Second)), nil)
	}
	return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, saved.OriginalCommandID); err != nil {
			return err
		}
		if _, err := s.ReadTaskTx(ctx, tx, r.cfg.Auth, saved.ParentTaskID); err != nil {
			return err
		}
		var current remoteSentInput
		rev, err := tx.Get(ctx, remoteInputSent, saved.Packet.Input.TransferID, &current)
		if err != nil {
			return err
		}
		if !api.Equal(current.Packet, saved.Packet) {
			return api.E("idempotency_conflict", "original_remote_input_changed")
		}
		current.Phase = "consumed"
		if receipt.Stage == "rejected" {
			current.Phase = "rejected"
		}
		if err = tx.Put(ctx, remoteInputSent, saved.Packet.Input.TransferID, rev, current); err != nil {
			return err
		}
		if receipt.Stage == "rejected" {
			return runtime.Decide(ctx, tx, saved.OriginalCommandID, nil, receipt.Error)
		}
		ref := api.ObjectRef{TenantID: scope.TenantID, OwnerID: saved.Command.LogicalServiceID, ObjectID: saved.Command.CommandID, Revision: 1}
		return runtime.Decide(ctx, tx, saved.OriginalCommandID, RemoteInputAck{TransferID: saved.Packet.Input.TransferID, Phase: current.Phase, ReceiverCommandRef: &ref}, nil)
	})
}
func (r *Remote) inputReceiveJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	var saved remoteReceivedInput
	if _, err := store.Read(ctx, scope, remoteInputReceived, work.Job.SourceRef.ObjectID, 0, &saved); err != nil {
		return err
	}
	if saved.Phase == "consumed" || saved.Phase == "rejected" {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), nil)
	}
	var original runtime.StoredCommand
	err := r.within(ctx, func(tx runtime.Tx) error {
		var err error
		original, err = tx.LoadCommand(ctx, saved.Command.CommandID)
		return err
	})
	if err != nil {
		return err
	}
	if original.Receipt.Stage != "accepted" {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), func(tx runtime.Tx) error {
			if _, err := tx.LoadCommand(ctx, saved.Command.CommandID); err != nil {
				return err
			}
			var current remoteReceivedInput
			rev, err := tx.Get(ctx, remoteInputReceived, saved.Packet.Input.TransferID, &current)
			if err != nil {
				return err
			}
			current.Phase = "consumed"
			if original.Receipt.Stage == "rejected" {
				current.Phase = "rejected"
			}
			return tx.Put(ctx, remoteInputReceived, saved.Packet.Input.TransferID, rev, current)
		})
	}
	if saved.Phase == "forwarded" {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Waiting(time.Now().Add(time.Second)), nil)
	}
	if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	prepared, err := r.PrepareChildContext(ctx, saved.Command.TargetID)
	if err != nil {
		return err
	}
	var handoff remoteReceived
	if _, err = store.Read(prepared, scope, remoteIncoming, remoteID("handoff", scope.TenantID, saved.Packet.ParentOwnerID, saved.Packet.CreationKey), 0, &handoff); err != nil {
		return err
	}
	profile, err := r.profile(handoff.Packet.ProfileRef)
	if err != nil {
		return err
	}
	var actor runtime.Auth
	err = r.within(prepared, func(tx runtime.Tx) error {
		var err error
		actor, err = r.cfg.Authority.ResolveSubjectTx(prepared, tx, saved.Packet.SubjectRef, profile, false)
		return err
	})
	if err != nil {
		return err
	}
	uses := []memory.ForeignUse{}
	for _, ref := range saved.Packet.ForeignReferences {
		if err = store.CheckClaim(prepared, scope, work.Claim); err != nil {
			return err
		}
		use, err := r.cfg.Memory.PrepareForeignUse(prepared, scope, actor, ref)
		if err != nil {
			return err
		}
		uses = append(uses, use)
	}
	prepared, err = memory.WithForeignUses(prepared, uses)
	if err != nil {
		return err
	}
	// 慢介质/多个来源之后只重取原父范围，不刷新输入命令或Data retention。
	prepared, err = r.PrepareChildContext(prepared, saved.Command.TargetID)
	if err != nil {
		return err
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	err = runtime.Finish(prepared, store, scope, r.parts, work, runtime.Waiting(time.Now().Add(time.Second)), func(tx runtime.Tx) error {
		return task.WithJobIntents(prepared, tx, func(tx runtime.Tx) error {
			if _, err := tx.LoadCommand(prepared, saved.Command.CommandID); err != nil {
				return err
			}
			actual, err := s.ReadTaskTreeTx(prepared, tx, actor, saved.Command.TargetID)
			if err != nil {
				return err
			}
			if err = s.CheckTaskCurrentTx(prepared, tx, actor, actual.TaskID, true); err != nil {
				return err
			}
			if err = r.cfg.Authority.CheckPeerTx(prepared, tx, saved.Peer, saved.Packet.ParentOwnerID); err != nil {
				return err
			}
			if err = r.CheckTaskCurrentTx(prepared, tx, actual, true); err != nil {
				return err
			}
			if actual.GoalRevision != saved.Packet.Input.ChildGoalRevision {
				return api.E("revision_conflict", "remote_input_child_goal_changed")
			}
			if _, err = r.cfg.Memory.CheckContentTx(prepared, tx, actor, saved.Packet.Input.ContentRef, "task.goal", profile.Values.Location, false); err != nil {
				return err
			}
			var current remoteReceivedInput
			rev, err := tx.Get(prepared, remoteInputReceived, saved.Packet.Input.TransferID, &current)
			if err != nil {
				return err
			}
			if current.Phase != "pending" || !api.Equal(current.Packet, saved.Packet) {
				return api.E("revision_conflict", "original_remote_input_changed")
			}
			if saved.Packet.Input.Kind == "steer" {
				_, err = s.SteerTx(prepared, tx, actor, saved.Command, task.SteerInput{TaskID: actual.TaskID, BaseGoalRevision: actual.GoalRevision, AmendmentRef: saved.Packet.Input.ContentRef, SourceSubmissionRef: saved.Packet.SourceSubmissionRef, PrepareDeadline: saved.Packet.ExpiresAt})
			} else {
				_, err = s.PrepareInputTx(prepared, tx, actor, saved.Command, task.InputAnswer{TaskID: actual.TaskID, RequestRef: *saved.Packet.Input.RequestRef, GoalRevision: actual.GoalRevision, AnswerRef: saved.Packet.Input.ContentRef})
			}
			if err != nil {
				return err
			}
			current.Phase = "forwarded"
			return tx.Put(prepared, remoteInputReceived, saved.Packet.Input.TransferID, rev, current)
		})
	})
	if err == nil || errors.Is(err, runtime.ErrCommitUnknown) {
		return err
	}
	var business *api.Error
	if !errors.As(err, &business) || business.Code == "dependency_unavailable" || business.Code == "overloaded" {
		return err
	}
	return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, saved.Command.CommandID); err != nil {
			return err
		}
		if _, err := s.ReadTaskTx(ctx, tx, r.cfg.Auth, saved.Command.TargetID); err != nil {
			return err
		}
		var current remoteReceivedInput
		rev, err := tx.Get(ctx, remoteInputReceived, saved.Packet.Input.TransferID, &current)
		if err != nil {
			return err
		}
		current.Phase = "rejected"
		if err = tx.Put(ctx, remoteInputReceived, saved.Packet.Input.TransferID, rev, current); err != nil {
			return err
		}
		return runtime.Decide(ctx, tx, saved.Command.CommandID, nil, business)
	})
}
