package collaboration

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type RemoteChildTransferInput struct {
	TransferID string `json:"transfer_id"`
}
type RemoteChildRequestInput struct {
	ParentOwnerID string        `json:"parent_owner_id"`
	CreationKey   string        `json:"creation_key"`
	RequestRef    api.ObjectRef `json:"request_ref"`
}
type remoteChildTransferPlan struct {
	Transfer task.Transfer `json:"transfer"`
	Command  api.Command   `json:"command"`
}

const remoteChildTransfers = "collaboration.remote_child_transfers"

func RemoteChildTransferContracts() []api.MethodContract {
	contracts := []api.MethodContract{api.Contract[RemoteChildTransferInput, RemoteInputAck]("collaboration.child.transfer", "orchestrator", "command", false, true), api.Contract[RemoteChildRequestInput, task.InputRequestView]("collaboration.child.request.get", "orchestrator", "query", false, false)}
	for i := range contracts {
		contracts[i].SchemaDigest, _ = api.Digest([]any{contracts[i].InputSchema, contracts[i].OutputSchema})
	}
	return contracts
}
func (r *Remote) childRequest(ctx context.Context, peer runtime.Auth, q api.Query, in RemoteChildRequestInput) (task.InputRequestView, error) {
	var out task.InputRequestView
	if q.TargetID != in.RequestRef.ObjectID || in.RequestRef.OwnerID != r.cfg.Scope.OwnerID || in.RequestRef.TenantID != r.cfg.Scope.TenantID || api.ValidateRecord("ObjectRef", in.RequestRef) != nil {
		return out, api.E("forbidden", "remote_request_scope_invalid")
	}
	var birth remoteReceived
	if _, err := r.cfg.Store.Read(ctx, r.cfg.Scope, remoteIncoming, remoteID("handoff", r.cfg.Scope.TenantID, in.ParentOwnerID, in.CreationKey), 1, &birth); err != nil {
		return out, err
	}
	profile, err := r.profile(birth.Packet.ProfileRef)
	if err != nil {
		return out, err
	}
	prepared, err := r.PrepareChildContext(ctx, birth.Packet.ChildTaskID)
	if err != nil {
		return out, err
	}
	s, err := r.service()
	if err != nil {
		return out, err
	}
	err = r.within(prepared, func(tx runtime.Tx) error {
		if err := r.cfg.Authority.CheckPeerTx(prepared, tx, peer, in.ParentOwnerID); err != nil {
			return err
		}
		actor, err := r.cfg.Authority.ResolveSubjectTx(prepared, tx, birth.Packet.SubjectRef, profile, false)
		if err != nil {
			return err
		}
		if err = s.CheckTaskCurrentTx(prepared, tx, actor, birth.Packet.ChildTaskID, false); err != nil {
			return err
		}
		out, err = s.RequestViewTx(prepared, tx, actor, in.RequestRef)
		if err != nil {
			return err
		}
		if out.Request.TargetRef.ObjectID != birth.Packet.ChildTaskID || out.Request.GoalRevision == nil || out.Request.State != "pending" {
			return api.E("revision_conflict", "request_version_changed")
		}
		return nil
	})
	return out, err
}
func (r *Remote) planChildTransfer(ctx context.Context, scope runtime.Scope, tr task.Transfer) (remoteChildTransferPlan, error) {
	var saved remoteChildTransferPlan
	_, err := r.cfg.Store.Read(ctx, scope, remoteChildTransfers, tr.TransferID, 1, &saved)
	if err == nil {
		if !api.Equal(saved.Transfer, tr) {
			return saved, api.E("idempotency_conflict", "original_child_transfer_changed")
		}
		return saved, nil
	}
	if !api.IsCode(err, "not_found") {
		return saved, err
	}
	s, err := r.service()
	if err != nil {
		return saved, err
	}
	var d task.Delegation
	err = r.within(ctx, func(tx runtime.Tx) error {
		if err := s.CheckTransferTx(ctx, tx, tr.ActorAuth, tr.TransferID); err != nil {
			return err
		}
		current, err := s.CheckDelegationScopeTx(ctx, tx, tr.ActorAuth, tr.DelegationID)
		d = current.Delegation
		return err
	})
	if err != nil {
		return saved, err
	}
	if d.Internal || d.ChildTaskRef == nil || d.ChildTaskRef.OwnerID == scope.OwnerID {
		return saved, api.E("unsupported", "remote_transfer_not_configured")
	}
	input := RemoteInputSend{TransferID: tr.TransferID, DelegationID: tr.DelegationID, ParentGoalRevision: tr.Input.ParentGoalRevision, ChildGoalRevision: tr.Input.ChildGoalRevision, Kind: tr.Input.Kind, RequestRef: tr.Input.RequestRef}
	switch input.Kind {
	case "steer":
		if tr.Input.ContentRef == nil || input.ChildGoalRevision == 0 {
			return saved, api.E("invalid_request", "original_steer_transfer_incomplete")
		}
		input.ContentRef = *tr.Input.ContentRef
	case "answer_request":
		if tr.Input.AnswerRef == nil || input.RequestRef == nil {
			return saved, api.E("invalid_request", "original_answer_transfer_incomplete")
		}
		peer, ok := r.peers[d.ReceiverID]
		if !ok {
			return saved, api.E("unsupported", "remote_agent_transport_unconfigured")
		}
		raw, err := peer.Client.Query(ctx, api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: d.ReceiverID, QueryID: api.NewID("query"), Method: "collaboration.child.request.get", TargetID: input.RequestRef.ObjectID, Payload: api.Raw(RemoteChildRequestInput{ParentOwnerID: scope.OwnerID, CreationKey: d.CreationKey, RequestRef: *input.RequestRef})})
		if err != nil {
			return saved, err
		}
		var request task.InputRequestView
		if err = api.Decode(raw, &request); err != nil {
			return saved, err
		}
		if request.RequestRef != *input.RequestRef || request.Request.TargetRef.ObjectID != d.ChildTaskRef.ObjectID || request.Request.GoalRevision == nil || request.Request.State != "pending" {
			return saved, api.E("revision_conflict", "request_version_changed")
		}
		input.ChildGoalRevision = *request.Request.GoalRevision
		input.ContentRef = *tr.Input.AnswerRef
	default:
		return saved, api.E("unsupported", "remote_input_kind_unconfigured")
	}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: tr.CommandRef.ObjectID, Method: "collaboration.child.transfer", TargetID: tr.TransferID, ExpiresAt: tr.ExpiresAt, Payload: api.Raw(RemoteChildTransferInput{TransferID: tr.TransferID})}
	saved = remoteChildTransferPlan{Transfer: tr, Command: command}
	err = r.within(ctx, func(tx runtime.Tx) error {
		if err := s.CheckTransferTx(ctx, tx, tr.ActorAuth, tr.TransferID); err != nil {
			return err
		}
		var old remoteChildTransferPlan
		_, err := tx.Get(ctx, remoteChildTransfers, tr.TransferID, &old)
		if err == nil {
			if !api.Equal(old, saved) {
				return api.E("idempotency_conflict", "original_child_transfer_changed")
			}
			saved = old
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		// 准确 request goal 已在外部核定；仅保存资料，首次真正准入仍在原 Dispatcher Tx。
		if err = tx.Create(ctx, "collaboration.remote_child_transfer_input", tr.TransferID, tr.DelegationID, input); err != nil {
			return err
		}
		return tx.Create(ctx, remoteChildTransfers, tr.TransferID, tr.DelegationID, saved)
	})
	return saved, err
}
func (r *Remote) receiveChildTransfer(ctx context.Context, tx runtime.Tx, actor runtime.Auth, c api.Command, in RemoteChildTransferInput) (runtime.Outcome, error) {
	s, err := r.service()
	if err != nil {
		return runtime.Outcome{}, err
	}
	if c.TargetID != in.TransferID {
		return runtime.Outcome{}, api.E("invalid_request", "remote_transfer_target_invalid")
	}
	if err = s.CheckTransferTx(ctx, tx, actor, in.TransferID); err != nil {
		return runtime.Outcome{}, err
	}
	var saved remoteChildTransferPlan
	if _, err = tx.Get(ctx, remoteChildTransfers, in.TransferID, &saved); err != nil {
		return runtime.Outcome{}, err
	}
	if !api.Equal(saved.Command, c) || !api.Equal(saved.Transfer.ActorAuth, actor) {
		return runtime.Outcome{}, api.E("idempotency_conflict", "original_child_transfer_command_changed")
	}
	var input RemoteInputSend
	if _, err = tx.Get(ctx, "collaboration.remote_child_transfer_input", in.TransferID, &input); err != nil {
		return runtime.Outcome{}, err
	}
	outgoing, err := r.planInputTx(ctx, tx, actor, c, input, saved.Transfer.SourceSubmissionRef)
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
	receiverRef := api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: outgoing.Command.LogicalServiceID, ObjectID: outgoing.Command.CommandID, Revision: 1}
	return runtime.Accepted(RemoteInputAck{TransferID: in.TransferID, Phase: "pending", ReceiverCommandRef: &receiverRef}), nil
}
func (r *Remote) transferRemoteChild(ctx context.Context, scope runtime.Scope, tr task.Transfer) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	d, err := s.DelegationRead(ctx, r.cfg.Store, scope, r.cfg.Auth, tr.DelegationID)
	if err != nil {
		return err
	}
	if d.ReceiverID == scope.OwnerID && r.cfg.Local != nil {
		return r.cfg.Local.Transfer(ctx, scope, tr)
	}
	saved, err := r.planChildTransfer(ctx, scope, tr)
	if err != nil {
		return err
	}
	dispatcher := runtime.Dispatcher{Store: r.cfg.Store, OwnerID: scope.OwnerID, Registry: r.cfg.Registry}
	receipt, err := dispatcher.Command(ctx, tr.ActorAuth, api.Raw(saved.Command))
	return knownReceipt(receipt, err)
}
func (r *Remote) registerRemoteChildTransfer() error {
	methods := []runtime.Method{remoteCommandMethod[RemoteChildTransferInput, RemoteInputAck](r, "collaboration.child.transfer", true, r.receiveChildTransfer), remoteQueryMethod[RemoteChildRequestInput, task.InputRequestView](r, "collaboration.child.request.get", r.childRequest)}
	for _, m := range methods {
		if err := r.cfg.Registry.Register(m); err != nil {
			return err
		}
	}
	return nil
}

// IsOriginalChildTransferSubmission 只辨认真正消费了原输入的 receiver 命令。
// 它不授正文许可；Context compiler随后仍重核准确 Content 当前用途。
func (r *Remote) IsOriginalChildTransferSubmission(ctx context.Context, scope runtime.Scope, actual api.Task, source api.SourceEvidence) (bool, error) {
	if err := r.checkScope(scope); err != nil {
		return false, err
	}
	if source.SubmissionRef == nil || source.SubmissionRef.OwnerID == scope.OwnerID || source.SourceKind != "user_input" || source.Locator != "" {
		return false, nil
	}
	birth, err := r.childOriginal(ctx, actual.TaskID)
	if api.IsCode(err, "not_found") {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if source.SubmissionRef.OwnerID != birth.Packet.DelegationRef.OwnerID || source.SubmissionRef.TenantID != scope.TenantID {
		return false, nil
	}
	if _, err = r.validatePacket(birth.Packet); err != nil {
		return false, err
	}
	found := false
	err = r.within(ctx, func(tx runtime.Tx) error {
		s, err := r.service()
		if err != nil {
			return err
		}
		current, err := s.ReadTaskTx(ctx, tx, r.cfg.Auth, actual.TaskID)
		if err != nil {
			return err
		}
		if current.OrchestratorID != scope.OwnerID || current.TenantID != scope.TenantID {
			return api.E("forbidden", "original_child_input_task_changed")
		}
		rows, err := tx.List(ctx, remoteInputReceived, packetID(birth.Packet), "", 129)
		if err != nil {
			return err
		}
		if len(rows) > 128 {
			return api.E("overloaded", "remote_input_capacity")
		}
		for _, row := range rows {
			var input remoteReceivedInput
			if err = row.Decode(&input); err != nil {
				return err
			}
			if input.Packet.SourceSubmissionRef != *source.SubmissionRef || input.Packet.Input.ContentRef != source.ContentRef {
				continue
			}
			if input.Command.TargetID != actual.TaskID || input.Packet.SubjectRef != birth.Packet.SubjectRef || input.Packet.ParentOwnerID != birth.Packet.DelegationRef.OwnerID || input.Packet.CreationKey != birth.Packet.CreationKey || input.Packet.Input.DelegationID != birth.Packet.CreationKey {
				return api.E("forbidden", "original_child_input_source_changed")
			}
			if err = validRemoteInput(input.Packet.Input, scope.OwnerID, scope.TenantID); err != nil {
				return err
			}
			command, err := tx.LoadCommand(ctx, input.Command.CommandID)
			if err != nil {
				return err
			}
			if command.Receipt.Stage != "applied" {
				continue
			}
			if !api.Equal(command.Command, input.Command) || command.PrincipalID != input.Peer.SubjectID {
				return api.E("idempotency_conflict", "original_child_input_receipt_changed")
			}
			found = true
		}
		return nil
	})
	return found, err
}
