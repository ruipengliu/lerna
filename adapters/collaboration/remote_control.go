package collaboration

import (
	"context"
	"errors"
	"os"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const (
	JobRemoteControl      = "collaboration.remote_control"
	remoteControls        = "collaboration.remote_controls"
	remotePendingControls = "collaboration.remote_pending_controls"
	remoteCommands        = "collaboration.remote_commands"
)

type RemoteControlInput struct {
	ParentOwnerID         string `json:"parent_owner_id"`
	CreationKey           string `json:"creation_key"`
	ParentGoalRevision    uint64 `json:"parent_goal_revision"`
	ParentControlRevision uint64 `json:"parent_control_revision"`
	Kind                  string `json:"kind"`
	Reason                string `json:"reason"`
}
type RemoteControlOutput struct {
	CreationKey string `json:"creation_key"`
	Kind        string `json:"kind"`
	Phase       string `json:"phase"`
}
type remoteControlGate struct {
	ParentOwnerID   string `json:"parent_owner_id"`
	CreationKey     string `json:"creation_key"`
	GoalRevision    uint64 `json:"goal_revision"`
	ControlRevision uint64 `json:"control_revision"`
	ParentPaused    bool   `json:"parent_paused"`
	Cancelled       bool   `json:"cancelled"`
}
type remotePendingControl struct {
	Command api.Command        `json:"command"`
	Input   RemoteControlInput `json:"input"`
	Peer    runtime.Auth       `json:"peer"`
	Phase   string             `json:"phase"`
}
type remoteCommand struct {
	Command api.Command `json:"command"`
}

// sendOriginal 在原发送记录存在时先核原回执；只在原 not_found 下重传原命令。
func sendOriginal(ctx context.Context, peer RemotePeer, command api.Command, before func() error) (api.Receipt, error) {
	_, err := peer.Client.Journal.Read(ctx, command.CommandID)
	if err == nil {
		original, lookupErr := peer.Client.Receipt(ctx, command.CommandID)
		if lookupErr == nil {
			return original, nil
		}
		if !api.IsCode(lookupErr, "not_found") {
			return api.Receipt{}, lookupErr
		}
	} else if !errors.Is(err, os.ErrNotExist) && !api.IsCode(err, "not_found") {
		return api.Receipt{}, err
	}
	if before != nil {
		if err = before(); err != nil {
			return api.Receipt{}, err
		}
	}
	return peer.Client.Send(ctx, command)
}
func knownReceipt(receipt api.Receipt, err error) error {
	if err != nil {
		return err
	}
	if receipt.Stage == "rejected" {
		return &task.OriginalCommandRejection{Receipt: receipt}
	}
	if receipt.Stage != "applied" {
		return api.E("dependency_unavailable", "remote_command_pending")
	}
	return nil
}

func (r *Remote) Control(ctx context.Context, scope runtime.Scope, d task.Delegation, kind string) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if d.ReceiverID == scope.OwnerID && r.cfg.Local != nil {
		return r.cfg.Local.Control(ctx, scope, d, kind)
	}
	if kind != "pause" && kind != "resume" && kind != "cancel" {
		return api.E("unsupported", "remote_control_kind_unsupported")
	}
	peer, ok := r.peers[d.ReceiverID]
	if !ok {
		return api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	var command api.Command
	err = r.within(ctx, func(tx runtime.Tx) error {
		parent, err := s.ReadTaskTx(ctx, tx, r.cfg.Auth, d.ParentTaskRef.ObjectID)
		if err != nil {
			return err
		}
		if d.ParentTaskRef.OwnerID != scope.OwnerID || d.ParentTaskRef.TenantID != scope.TenantID {
			return api.E("forbidden", "remote_control_parent_mismatch")
		}
		var original remoteSent
		if _, err = tx.Get(ctx, remoteOutgoing, remoteID("handoff", scope.TenantID, scope.OwnerID, d.CreationKey), &original); err != nil {
			return err
		}
		if !api.Equal(original.Packet.Input, d.DelegateInput) {
			return api.E("idempotency_conflict", "original_control_mapping_changed")
		}
		input := RemoteControlInput{ParentOwnerID: scope.OwnerID, CreationKey: d.CreationKey, ParentGoalRevision: d.ParentGoalRevision, ParentControlRevision: parent.ControlRevision, Kind: kind, Reason: "original parent control"}
		id := remoteID("command", packetID(original.Packet), kind, strconv.FormatUint(parent.ControlRevision, 10))
		var saved remoteCommand
		_, err = tx.Get(ctx, remoteCommands, id, &saved)
		if err == nil {
			command = saved.Command
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		rows, err := tx.List(ctx, remoteCommands, packetID(original.Packet), "", 129)
		if err != nil {
			return err
		}
		if len(rows) >= 128 {
			return api.E("overloaded", "remote_control_capacity")
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		command = api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: peer.Scope.OwnerID, CommandID: id, Method: "collaboration.control", TargetID: d.CreationKey, ExpiresAt: api.Time(now.Add(10 * time.Minute)), Payload: api.Raw(input)}
		return tx.Create(ctx, remoteCommands, id, packetID(original.Packet), remoteCommand{command})
	})
	if err != nil {
		return err
	}
	receipt, err := sendOriginal(ctx, peer, command, nil)
	return knownReceipt(receipt, err)
}
func (r *Remote) CloseAllocation(ctx context.Context, scope runtime.Scope, allocation task.Allocation) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if allocation.ReceiverID == scope.OwnerID && r.cfg.Local != nil {
		return r.cfg.Local.CloseAllocation(ctx, scope, allocation)
	}
	peer, ok := r.peers[allocation.ReceiverID]
	if !ok {
		return api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	original, err := s.AllocationRead(ctx, r.cfg.Store, scope, r.cfg.Auth, allocation.AllocationID)
	if err != nil {
		return err
	}
	if !api.Equal(original.ParentTaskRef, allocation.ParentTaskRef) || original.ReceiverID != allocation.ReceiverID || !api.Equal(original.Limits, allocation.Limits) {
		return api.E("idempotency_conflict", "original_close_allocation_changed")
	}
	var command api.Command
	err = r.within(ctx, func(tx runtime.Tx) error {
		if _, err := s.ReadTaskTx(ctx, tx, r.cfg.Auth, original.ParentTaskRef.ObjectID); err != nil {
			return err
		}
		id := remoteID("command", scope.TenantID, scope.OwnerID, allocation.AllocationID, "close")
		var saved remoteCommand
		_, err := tx.Get(ctx, remoteCommands, id, &saved)
		if err == nil {
			command = saved.Command
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		command = api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: peer.Scope.OwnerID, CommandID: id, Method: "collaboration.allocation.close", TargetID: allocation.AllocationID, ExpiresAt: api.Time(now.Add(10 * time.Minute)), Payload: api.Raw(task.AllocationCloseInput{AllocationRef: scope.Ref(allocation.AllocationID, 1), ParentTaskRef: allocation.ParentTaskRef, Reason: "original parent spending close"})}
		return tx.Create(ctx, remoteCommands, id, allocation.AllocationID, remoteCommand{command})
	})
	if err != nil {
		return err
	}
	receipt, err := sendOriginal(ctx, peer, command, nil)
	return knownReceipt(receipt, err)
}

func (r *Remote) receiveControl(ctx context.Context, tx runtime.Tx, peer runtime.Auth, c api.Command, in RemoteControlInput) (runtime.Outcome, error) {
	if err := r.checkScope(tx.Scope()); err != nil {
		return runtime.Outcome{}, err
	}
	if c.TargetID != in.CreationKey || !api.ValidID(in.ParentOwnerID) || !api.ValidID(in.CreationKey) || in.ParentGoalRevision == 0 || in.ParentControlRevision == 0 || in.Reason == "" || in.Kind != "pause" && in.Kind != "resume" && in.Kind != "cancel" {
		return runtime.Outcome{}, api.E("invalid_request", "remote_control_invalid")
	}
	id := remoteID("handoff", tx.Scope().TenantID, in.ParentOwnerID, in.CreationKey)
	var birth remoteReceived
	var child *api.Task
	err := tx.GetVersion(ctx, remoteIncoming, id, 1, &birth)
	if err == nil {
		s, err := r.service()
		if err != nil {
			return runtime.Outcome{}, err
		}
		current, err := s.ReadTaskTreeTx(ctx, tx, r.cfg.Auth, birth.Packet.ChildTaskID)
		if err == nil {
			child = &current
		} else if !api.IsCode(err, "not_found") {
			return runtime.Outcome{}, err
		}
		if birth.Packet.Input.ParentGoalRevision != in.ParentGoalRevision {
			return runtime.Outcome{}, api.E("revision_conflict", "remote_control_goal_changed")
		}
	} else if !api.IsCode(err, "not_found") {
		return runtime.Outcome{}, err
	}
	if err = r.cfg.Authority.CheckPeerTx(ctx, tx, peer, in.ParentOwnerID); err != nil {
		return runtime.Outcome{}, err
	}
	if birth.Packet.CreationKey == "" && in.Kind == "resume" {
		return runtime.Outcome{}, api.E("not_found", "original_remote_creation_missing")
	}
	if birth.Packet.CreationKey != "" {
		profile, err := r.profile(birth.Packet.ProfileRef)
		if err != nil {
			return runtime.Outcome{}, err
		}
		if _, err = r.cfg.Authority.ResolveSubjectTx(ctx, tx, birth.Packet.SubjectRef, profile, true); err != nil {
			return runtime.Outcome{}, err
		}
	}
	var gate remoteControlGate
	rev, err := tx.Get(ctx, remoteControls, id, &gate)
	if err != nil && !api.IsCode(err, "not_found") {
		return runtime.Outcome{}, err
	}
	if gate.Cancelled && in.Kind != "cancel" {
		return runtime.Outcome{}, api.E("invalid_state", "remote_goal_closed")
	}
	if gate.ControlRevision > in.ParentControlRevision {
		return runtime.Outcome{}, api.E("revision_conflict", "remote_control_older_than_known")
	}
	if gate.GoalRevision != 0 && gate.GoalRevision != in.ParentGoalRevision {
		return runtime.Outcome{}, api.E("revision_conflict", "remote_control_goal_changed")
	}
	gate.ParentOwnerID, gate.CreationKey, gate.GoalRevision, gate.ControlRevision = in.ParentOwnerID, in.CreationKey, in.ParentGoalRevision, in.ParentControlRevision
	gate.ParentPaused = in.Kind == "pause" || in.Kind == "resume"
	if in.Kind == "cancel" {
		gate.Cancelled = true
		if child != nil && child.Status == "active" {
			s, _ := r.service()
			internal := c
			internal.Method, internal.TargetID, internal.ExpectedRevision = "task.cancel", child.TaskID, &child.Revision
			if _, err = s.ControlTx(ctx, tx, r.cfg.Auth, internal, task.ControlInput{TaskID: child.TaskID, Reason: in.Reason}); err != nil {
				return runtime.Outcome{}, err
			}
		}
	}
	if rev == 0 {
		err = tx.Create(ctx, remoteControls, id, in.ParentOwnerID, gate)
	} else {
		err = tx.Put(ctx, remoteControls, id, rev, gate)
	}
	if err != nil {
		return runtime.Outcome{}, err
	}
	out := RemoteControlOutput{in.CreationKey, in.Kind, "applied"}
	if in.Kind != "resume" {
		return runtime.Applied(out), nil
	}
	if err = tx.Create(ctx, remotePendingControls, c.CommandID, id, remotePendingControl{c, in, peer, "pending"}); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, JobRemoteControl, "control/"+c.CommandID, tx.Scope().Ref(c.CommandID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	out.Phase = "pending"
	return runtime.Accepted(out), nil
}

func (r *Remote) controlJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if store.ID() != r.cfg.Store.ID() {
		return api.E("forbidden", "remote_job_database_mismatch")
	}
	if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	var pending remotePendingControl
	if _, err := store.Read(ctx, scope, remotePendingControls, work.Job.SourceRef.ObjectID, 0, &pending); err != nil {
		return err
	}
	if pending.Phase != "pending" {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), nil)
	}
	id := remoteID("handoff", scope.TenantID, pending.Input.ParentOwnerID, pending.Input.CreationKey)
	var received remoteReceived
	if _, err := store.Read(ctx, scope, remoteIncoming, id, 0, &received); err != nil {
		return err
	}
	if received.ChildTaskRef == nil {
		return api.E("dependency_unavailable", "remote_child_creation_pending")
	}
	prepared, err := r.PrepareChildContext(ctx, received.ChildTaskRef.ObjectID)
	if err != nil {
		return err
	}
	ctx = prepared
	s, err := r.service()
	if err != nil {
		return err
	}
	return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, pending.Command.CommandID); err != nil {
			return err
		}
		actual, err := s.ReadTaskTreeTx(ctx, tx, r.cfg.Auth, received.ChildTaskRef.ObjectID)
		if err != nil {
			return err
		}
		if err = r.cfg.Authority.CheckPeerTx(ctx, tx, pending.Peer, pending.Input.ParentOwnerID); err != nil {
			return err
		}
		var current remoteReceived
		if _, err = tx.Get(ctx, remoteIncoming, id, &current); err != nil {
			return err
		}
		var gate remoteControlGate
		gateRev, err := tx.Get(ctx, remoteControls, id, &gate)
		if err != nil {
			return err
		}
		var decisionErr *api.Error
		if gate.Cancelled || actual.Status != "active" || gate.ControlRevision != pending.Input.ParentControlRevision {
			decisionErr = api.E("invalid_state", "remote_control_superseded")
		}
		if decisionErr == nil {
			if current.ParentSnapshot == nil || !current.ParentSnapshot.Allowed || current.ParentSnapshot.ParentTask.ControlRevision < pending.Input.ParentControlRevision {
				decisionErr = api.E("forbidden", "remote_parent_resume_denied")
			}
		}
		var latest remotePendingControl
		rev, err := tx.Get(ctx, remotePendingControls, pending.Command.CommandID, &latest)
		if err != nil {
			return err
		}
		if latest.Phase != "pending" || !api.Equal(latest.Input, pending.Input) {
			return api.E("idempotency_conflict", "original_remote_control_changed")
		}
		if decisionErr != nil {
			latest.Phase = "rejected"
		} else {
			gate.ParentPaused = false
			if err = tx.Put(ctx, remoteControls, id, gateRev, gate); err != nil {
				return err
			}
			latest.Phase = "applied"
		}
		if err = tx.Put(ctx, remotePendingControls, pending.Command.CommandID, rev, latest); err != nil {
			return err
		}
		if err = runtime.Decide(ctx, tx, pending.Command.CommandID, RemoteControlOutput{pending.Input.CreationKey, pending.Input.Kind, latest.Phase}, decisionErr); err != nil {
			return err
		}
		if decisionErr == nil {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			_, err = tx.Raise(ctx, task.JobAdvance, "advance/"+actual.TaskID, tx.Scope().Ref(actual.TaskID, actual.Revision), now)
			return err
		}
		return nil
	})
}
