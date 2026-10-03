package development

import (
	"context"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func remoteStringsSubset(actual, permitted []string) bool {
	for _, value := range actual {
		if !containsString(permitted, value) {
			return false
		}
	}
	return true
}

// 名称只是Grant投影；递归委派仍须保留父准入的准确资源版本。
func remoteResourcesSubset(actual, permitted collaboration.RemoteActionScope) bool {
	if len(actual.ResourceRefs) != len(actual.Resources) || len(permitted.ResourceRefs) != len(permitted.Resources) {
		return false
	}
	for index, name := range actual.Resources {
		found := false
		for originalIndex, originalName := range permitted.Resources {
			if name == originalName && actual.ResourceRefs[index] == permitted.ResourceRefs[originalIndex] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (a *App) remoteParentAdmission(ctx context.Context, scope runtime.Scope, taskID string) (*collaboration.RemoteParentAdmission, error) {
	if a.RemoteAgent == nil {
		return nil, nil
	}
	var admission *collaboration.RemoteParentAdmission
	status, err := a.Store.Within(ctx, scope, []string{"task", "collaboration", "content", "memory", "governance", "platform"}, func(tx runtime.Tx) error {
		actual, err := a.Task.ReadTaskTreeTx(ctx, tx, a.ServiceAuth, taskID)
		if err != nil {
			return err
		}
		admission, err = a.RemoteAgent.ChildAdmissionTx(ctx, tx, actual)
		return err
	})
	if status == runtime.CommitUnknown {
		return nil, runtime.ErrCommitUnknown
	}
	return admission, err
}

func remoteActionScope(admission collaboration.RemoteParentAdmission, descriptor actionDescriptor) bool {
	if !remoteComponentContains(admission.CapabilityRefs, descriptor.Capability.Ref) {
		return false
	}
	for _, scope := range admission.ActionScopes {
		if scope.CapabilityRef == descriptor.Capability.Ref && scope.BindingRef == descriptor.BindingRef && scope.Recipient == descriptor.Recipient && scope.Location == descriptor.Location && remoteStringsSubset(descriptor.Resources, scope.Resources) && remoteStringsSubset(descriptor.Actions, scope.Actions) {
			return true
		}
	}
	return false
}

// Context只能声明原父准入的准确动作；过滤保留原cap/binding配对及顺序。
func (a *App) restrictRemoteAgentActions(ctx context.Context, scope runtime.Scope, t api.Task, original actionSnapshot) (actionSnapshot, *collaboration.RemoteParentAdmission, error) {
	admission, err := a.remoteParentAdmission(ctx, scope, t.TaskID)
	if err != nil || admission == nil {
		return original, admission, err
	}
	entries := []actionDescriptor{}
	for _, descriptor := range original.Entries {
		if remoteActionScope(*admission, descriptor) {
			entries = append(entries, descriptor)
		}
	}
	original.Entries = entries
	return original, admission, nil
}

func (a *App) checkRemoteEncoding(admission *collaboration.RemoteParentAdmission, snapshot api.Snapshot, encoding brain.Encoding, cost []api.Amount) error {
	if admission == nil {
		return nil
	}
	limits := admission.Controls
	if uint64(len(encoding.Body)) > limits.MaxInputBytes || snapshot.ReservedOutputTokens > limits.MaxOutputTokens || !knowledgeCostsBounded(cost, limits.MaxCallCostBound) {
		return api.E("forbidden", "remote_parent_model_control_exceeded")
	}
	if a.Model != nil {
		m := admission.ModelScope
		if m == nil || admission.UseRef == nil || snapshot.ModelProfileRef != m.ModelProfileRef || encoding.Receiver != m.Receiver || encoding.Location != m.Location {
			return api.E("forbidden", "remote_parent_model_scope_required")
		}
	} else if encoding.Receiver != "builtin-rule-engine" || encoding.Location != "cloud" {
		return api.E("forbidden", "remote_parent_model_exit_changed")
	}
	return nil
}

func (a *App) checkRemoteModelTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in brain.DecideInput, encoding *brain.Encoding) error {
	if a.RemoteAgent == nil {
		return nil
	}
	snapshot, err := a.Task.DecisionSnapshotTx(ctx, tx, auth, in.DecisionID)
	if err != nil {
		return err
	}
	actual, err := a.Task.ReadTaskTx(ctx, tx, auth, snapshot.TaskRef.ObjectID)
	if err != nil {
		return err
	}
	admission, err := a.RemoteAgent.ChildAdmissionTx(ctx, tx, actual)
	if err != nil || admission == nil {
		return err
	}
	if !knowledgeCostsBounded(in.Limits, admission.Controls.MaxCallCostBound) {
		return api.E("forbidden", "remote_parent_model_cost_exceeded")
	}
	deadline, err := api.ParseTime(in.Deadline)
	if err != nil {
		return err
	}
	until, err := api.ParseTime(actual.Deadline)
	if err != nil {
		return err
	}
	if deadline.After(until) {
		return api.E("forbidden", "remote_parent_model_window_expanded")
	}
	if encoding != nil {
		return a.checkRemoteEncoding(admission, snapshot, *encoding, in.Limits)
	}
	if a.Model != nil && (admission.ModelScope == nil || admission.UseRef == nil || in.ModelProfileRef != admission.ModelScope.ModelProfileRef) {
		return api.E("forbidden", "remote_parent_model_scope_required")
	}
	return nil
}

func (a *App) remoteProposalLimits(ctx context.Context, scope runtime.Scope, intent api.DecisionDispatchIntent, proposal brain.Proposal) (*collaboration.RemoteParentAdmission, error) {
	admission, err := a.remoteParentAdmission(ctx, scope, intent.TaskRef.ObjectID)
	if err != nil || admission == nil {
		return admission, err
	}
	if uint64(len(proposal.Actions)) > admission.Controls.MaxActionsPerDecision {
		return admission, api.E("forbidden", "remote_parent_action_count_exceeded")
	}
	for _, action := range proposal.Actions {
		if !remoteComponentContains(admission.CapabilityRefs, action.CapabilityRef) {
			return admission, api.E("forbidden", "remote_parent_capability_exceeded")
		}
		allowed := false
		for _, scope := range admission.ActionScopes {
			allowed = allowed || scope.CapabilityRef == action.CapabilityRef && scope.BindingRef == action.BindingRef
		}
		if !allowed || admission.UseRef == nil {
			return admission, api.E("forbidden", "remote_parent_action_scope_required")
		}
	}
	return admission, nil
}

func (a *App) checkRemoteActionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, intent task.OperationIntent, admitted actionAdmission) error {
	if a.RemoteAgent == nil {
		return nil
	}
	actual, err := a.Task.ReadTaskTx(ctx, tx, auth, intent.TaskRef.ObjectID)
	if err != nil {
		return err
	}
	parent, err := a.RemoteAgent.ChildAdmissionTx(ctx, tx, actual)
	if err != nil || parent == nil {
		return err
	}
	if parent.UseRef == nil || !remoteActionScope(*parent, admitted.Descriptor) || !knowledgeCostsBounded(intent.CostBound, parent.Controls.MaxCallCostBound) {
		return api.E("forbidden", "remote_parent_action_scope_required")
	}
	for _, scope := range parent.ActionScopes {
		if scope.CapabilityRef == intent.CapabilityRef && scope.BindingRef == intent.BindingRef {
			if !remoteStringsSubset(admitted.Resources, scope.Resources) || !remoteStringsSubset(admitted.Actions, scope.Actions) {
				return api.E("forbidden", "remote_parent_action_resources_exceeded")
			}
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			return remoteActionWithinDeadline(now, intent.Deadline, parent.Controls, actual.Deadline)
		}
	}
	return api.E("forbidden", "remote_parent_action_scope_required")
}
