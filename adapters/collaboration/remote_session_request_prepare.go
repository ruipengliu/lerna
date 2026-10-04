package collaboration

import (
	"context"
	"errors"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const JobRemoteChildRequestPrepare = "collaboration.remote_child_request_prepare"

// 请求只读也必须归属于已持久接纳的原父命令。业务拒绝由本方决定
// 原命令，不能把 receiver 回执改名，也不能把网络未知当成请求失效。
func childRequestBusinessError(err error) *api.Error {
	if errors.Is(err, runtime.ErrCommitUnknown) {
		return nil
	}
	var e *api.Error
	if !errors.As(err, &e) {
		return nil
	}
	switch e.Code {
	case "revision_conflict", "forbidden", "expired", "invalid_state", "not_found", "gone":
		return e
	}
	return nil
}

func (r *Remote) childRequestPrepareJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	if err := r.checkScope(scope); err != nil {
		return err
	}
	if err := store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	var saved remoteChildTransferPlan
	if _, err := store.Read(ctx, scope, remoteChildTransfers, work.Job.SourceRef.ObjectID, 1, &saved); err != nil {
		return err
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	var input RemoteInputSend
	var d task.Delegation
	terminal := false
	err = r.within(ctx, func(tx runtime.Tx) error {
		original, err := tx.LoadCommand(ctx, saved.Command.CommandID)
		if err != nil {
			return err
		}
		if !api.Equal(original.Command, saved.Command) || original.PrincipalID != saved.Transfer.ActorAuth.SubjectID {
			return api.E("idempotency_conflict", "original_child_transfer_command_changed")
		}
		if original.Receipt.Stage != "accepted" {
			terminal = true
			return nil
		}
		if err = s.CheckTransferTx(ctx, tx, saved.Transfer.ActorAuth, saved.Transfer.TransferID); err != nil {
			return err
		}
		current, err := s.CheckDelegationScopeTx(ctx, tx, saved.Transfer.ActorAuth, saved.Transfer.DelegationID)
		if err != nil {
			return err
		}
		d = current.Delegation
		if _, err = tx.Get(ctx, "collaboration.remote_child_transfer_input", saved.Transfer.TransferID, &input); err != nil {
			return err
		}
		if saved.Transfer.Input.AnswerRef == nil {
			return api.E("idempotency_conflict", "original_child_request_changed")
		}
		expected := RemoteInputSend{TransferID: saved.Transfer.TransferID, DelegationID: saved.Transfer.DelegationID, ParentGoalRevision: saved.Transfer.Input.ParentGoalRevision, ChildGoalRevision: saved.Transfer.Input.ChildGoalRevision, Kind: saved.Transfer.Input.Kind, ContentRef: *saved.Transfer.Input.AnswerRef, RequestRef: saved.Transfer.Input.RequestRef}
		if expected.Kind != "answer_request" || expected.RequestRef == nil || !api.Equal(input, expected) {
			return api.E("idempotency_conflict", "original_child_request_changed")
		}

		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		deadline, err := api.ParseTime(saved.Command.ExpiresAt)
		if err != nil {
			return err
		}
		if !now.Before(deadline) {
			return api.E("expired", "original_child_transfer_expired")
		}
		return nil
	})
	if terminal {
		return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), nil)
	}
	if err != nil {
		// 本地记录缺失没有对端请求不存在的证明。
		if api.IsCode(err, "not_found") || api.IsCode(err, "gone") {
			return err
		}
		return r.finishChildRequestRejection(ctx, store, scope, work, saved, err)
	}
	peer, ok := r.peers[d.ReceiverID]
	if !ok {
		return api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	raw, err := peer.Client.Query(ctx, api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: d.ReceiverID, QueryID: api.NewID("query"), Method: "collaboration.child.request.get", TargetID: input.RequestRef.ObjectID, Payload: api.Raw(RemoteChildRequestInput{ParentOwnerID: scope.OwnerID, CreationKey: d.CreationKey, RequestRef: *input.RequestRef})})
	if err != nil {
		return r.finishChildRequestRejection(ctx, store, scope, work, saved, err)
	}
	var request task.InputRequestView
	if err = api.Decode(raw, &request); err != nil {
		return err
	}
	if request.RequestRef != *input.RequestRef || d.ChildTaskRef == nil || request.Request.TargetRef.OwnerID != d.ReceiverID || request.Request.TargetRef.ObjectID != d.ChildTaskRef.ObjectID || request.Request.GoalRevision == nil || request.Request.State != "pending" {
		return r.finishChildRequestRejection(ctx, store, scope, work, saved, api.E("revision_conflict", "request_version_changed"))
	}
	if input.ChildGoalRevision != 0 && input.ChildGoalRevision != *request.Request.GoalRevision {
		return r.finishChildRequestRejection(ctx, store, scope, work, saved, api.E("revision_conflict", "request_goal_changed"))
	}
	input.ChildGoalRevision = *request.Request.GoalRevision
	err = runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), func(tx runtime.Tx) error {
		original, err := tx.LoadCommand(ctx, saved.Command.CommandID)
		if err != nil {
			return err
		}
		if !api.Equal(original.Command, saved.Command) || original.PrincipalID != saved.Transfer.ActorAuth.SubjectID {
			return api.E("idempotency_conflict", "original_child_transfer_command_changed")
		}
		if original.Receipt.Stage != "accepted" {
			return nil
		}
		if _, err = s.ReadTaskTreeTx(ctx, tx, r.cfg.Auth, d.ParentTaskRef.ObjectID); err != nil {
			return err
		}
		// 失败回滚全部待发送资料；只提交真正原命令的业务拒绝。
		err = tx.Savepoint(ctx, func(inner runtime.Tx) error {
			if err := s.CheckTransferTx(ctx, inner, saved.Transfer.ActorAuth, saved.Transfer.TransferID); err != nil {
				return err
			}
			if _, err := r.planInputTx(ctx, inner, saved.Transfer.ActorAuth, saved.Command, input, saved.Transfer.SourceSubmissionRef); err != nil {
				return err
			}
			now, err := inner.Now(ctx)
			if err != nil {
				return err
			}
			_, err = inner.Raise(ctx, JobRemoteInputSend, "input/"+input.TransferID, scope.Ref(input.TransferID, 1), now)
			return err
		})
		if err == nil {
			return nil
		}
		if business := childRequestBusinessError(err); business != nil && business.Code != "not_found" && business.Code != "gone" {
			return runtime.Decide(ctx, tx, saved.Command.CommandID, nil, business)
		}
		return err
	})
	return err
}

func (r *Remote) finishChildRequestRejection(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, saved remoteChildTransferPlan, cause error) error {
	business := childRequestBusinessError(cause)
	if business == nil {
		return cause
	}
	s, err := r.service()
	if err != nil {
		return err
	}
	return runtime.Finish(ctx, store, scope, r.parts, work, runtime.Done(), func(tx runtime.Tx) error {
		original, err := tx.LoadCommand(ctx, saved.Command.CommandID)
		if err != nil {
			return err
		}
		if !api.Equal(original.Command, saved.Command) || original.PrincipalID != saved.Transfer.ActorAuth.SubjectID {
			return api.E("idempotency_conflict", "original_child_transfer_command_changed")
		}
		if original.Receipt.Stage != "accepted" {
			return nil
		}
		var birth task.Delegation
		if err = tx.GetVersion(ctx, "task.delegations", saved.Transfer.DelegationID, 1, &birth); err != nil {
			return err
		}
		if _, err = s.ReadTaskTreeTx(ctx, tx, r.cfg.Auth, birth.ParentTaskRef.ObjectID); err != nil {
			return err
		}
		var current remoteChildTransferPlan
		if _, err = tx.Get(ctx, remoteChildTransfers, saved.Transfer.TransferID, &current); err != nil {
			return err
		}
		if !api.Equal(current, saved) {
			return api.E("idempotency_conflict", "original_child_transfer_changed")
		}
		return runtime.Decide(ctx, tx, saved.Command.CommandID, nil, business)
	})
}
