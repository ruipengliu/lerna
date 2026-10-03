package development

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

type decisionCancellation struct {
	Intent  api.DecisionDispatchIntent `json:"intent"`
	Command api.Command                `json:"command"`
}

// 负控制有自己的原命令，不借用已终态 Task 的 deadline，也不刷新重放期限。
func (b brainBridge) CancelDecision(ctx context.Context, scope runtime.Scope, intent api.DecisionDispatchIntent) error {
	if intent.TaskRef.TenantID != scope.TenantID || intent.TaskRef.OwnerID != scope.OwnerID || intent.BrainOwnerID != scope.OwnerID {
		return api.E("unsupported", "foreign_brain_cancellation_not_configured")
	}
	var original decisionCancellation
	status, err := b.a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, "platform.brain_cancellations", intent.DecisionID, &original)
		if err == nil {
			if !api.Equal(original.Intent, intent) {
				return api.E("idempotency_conflict", "original_decision_cancel_changed")
			}
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: intent.BrainOwnerID, CommandID: stableID("command", "brain-cancel/"+intent.DecisionID), TargetID: intent.DecisionID, Method: "brain.cancel", ExpiresAt: api.Time(now.Add(time.Minute)), Payload: api.Raw(brain.CancelInput{DecisionID: intent.DecisionID, TaskRef: intent.TaskRef})}
		original = decisionCancellation{Intent: intent, Command: command}
		return tx.Create(ctx, "platform.brain_cancellations", intent.DecisionID, intent.TaskRef.ObjectID, original)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if err != nil {
		return err
	}
	receipt, err := b.a.Dispatcher.Command(ctx, b.a.ServiceAuth, api.Raw(original.Command))
	if err != nil {
		return err
	}
	if receipt.Error != nil {
		return receipt.Error
	}
	if receipt.Stage != "applied" {
		return api.E("dependency_unavailable", "original_brain_cancel_not_decided")
	}
	return nil
}
