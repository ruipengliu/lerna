package development

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type remoteScopeAuthority struct{ a *App }

func remoteComponentContains(refs []api.ComponentRef, ref api.ComponentRef) bool {
	for _, candidate := range refs {
		if candidate == ref {
			return true
		}
	}
	return false
}

func remoteControls(in governance.KnowledgeControls) collaboration.RemoteControlLimits {
	return collaboration.RemoteControlLimits{MaxInputBytes: in.MaxInputBytes, MaxOutputTokens: in.MaxOutputTokens, MaxActionsPerDecision: in.MaxActionsPerDecision, MaxDelegationsPerDecision: in.MaxDelegationsPerDecision, MaxDepth: in.MaxDepth, MaxActionDurationSeconds: in.MaxActionDurationSeconds, MaxCallCostBound: append([]api.Amount{}, in.MaxCallCostBound...)}
}

func narrowRemoteControls(base, limit collaboration.RemoteControlLimits) collaboration.RemoteControlLimits {
	base.MaxInputBytes = smallestKnowledgeBound(base.MaxInputBytes, limit.MaxInputBytes)
	base.MaxOutputTokens = smallestKnowledgeBound(base.MaxOutputTokens, limit.MaxOutputTokens)
	base.MaxActionsPerDecision = smallestKnowledgeBound(base.MaxActionsPerDecision, limit.MaxActionsPerDecision)
	base.MaxDelegationsPerDecision = smallestKnowledgeBound(base.MaxDelegationsPerDecision, limit.MaxDelegationsPerDecision)
	base.MaxDepth = smallestKnowledgeBound(base.MaxDepth, limit.MaxDepth)
	base.MaxActionDurationSeconds = smallestKnowledgeBound(base.MaxActionDurationSeconds, limit.MaxActionDurationSeconds)
	base.MaxCallCostBound = narrowKnowledgeAmountBounds(base.MaxCallCostBound, limit.MaxCallCostBound)
	return base
}

func (g remoteScopeAuthority) originalAdmissionTx(ctx context.Context, tx runtime.Tx, actor runtime.Auth, current task.DelegationScope, profile collaboration.RemoteAgentProfile, view task.TaskSnapshotView) (collaboration.RemoteParentAdmission, error) {
	var out collaboration.RemoteParentAdmission
	if actor.Ref(tx.Scope().OwnerID) != current.SubjectRef || current.ParentTask.OrchestratorID != tx.Scope().OwnerID || current.ParentTask.PolicyRef != g.a.TaskPolicy.PolicyRef {
		return out, api.E("forbidden", "remote_original_parent_authority_changed")
	}
	if err := currentCredentialTx(ctx, tx, actor); err != nil {
		return out, err
	}
	snapshot := view.Snapshot
	if snapshot.TaskRef.ObjectID != current.ParentTask.TaskID || snapshot.TaskRef.OwnerID != tx.Scope().OwnerID || snapshot.TaskRef.TenantID != tx.Scope().TenantID || snapshot.GoalRef != current.ParentTask.GoalRef || snapshot.GoalRevision != current.ParentTask.GoalRevision || snapshot.ControlRevision != current.ParentTask.ControlRevision || snapshot.PolicyRef != current.ParentTask.PolicyRef || view.Intent.TaskRef != snapshot.TaskRef || view.Intent.SnapshotRevision != snapshot.Revision {
		return out, api.E("forbidden", "remote_original_parent_snapshot_changed")
	}
	out = collaboration.RemoteParentAdmission{TaskRef: snapshot.TaskRef, DecisionID: view.Intent.DecisionID, SnapshotID: snapshot.SnapshotID, SnapshotRef: view.Intent.SnapshotRef, ModelProfileRef: snapshot.ModelProfileRef, InstallLockRef: snapshot.InstallLockRef, CapabilityRefs: []api.ComponentRef{}, ActionScopes: []collaboration.RemoteActionScope{}, ModelScope: profile.Values.ModelScope, ValidUntil: current.Delegation.Deadline, Controls: collaboration.RemoteControlLimits{MaxInputBytes: g.a.Profile.MaxInputBytes, MaxOutputTokens: smallestKnowledgeBound(snapshot.ReservedOutputTokens, g.a.Profile.MaxOutputTokens), MaxActionsPerDecision: 16, MaxDelegationsPerDecision: smallestKnowledgeBound(g.a.TaskPolicy.MaxDelegations, 4), MaxDepth: smallestKnowledgeBound(g.a.TaskPolicy.MaxDepth, profile.Values.MaxDepth), MaxActionDurationSeconds: smallestKnowledgeBound(g.a.TaskPolicy.MaxDurationSeconds, 3600), MaxCallCostBound: narrowKnowledgeAmountBounds(current.Delegation.Budget, g.a.TaskPolicy.BudgetLimits)}}
	for _, cap := range snapshot.CapabilityRefs {
		if remoteComponentContains(profile.Values.CapabilityRefs, cap) {
			out.CapabilityRefs = append(out.CapabilityRefs, cap)
		}
	}
	commit, found, err := g.a.Governance.FindDecisionSelectionTx(ctx, tx, g.a.ServiceAuth, view.Intent.DecisionID)
	if err != nil {
		return out, err
	}
	if found {
		if commit.SnapshotRef != view.Intent.SnapshotRef || commit.Selection.Request.TaskRef != snapshot.TaskRef || commit.Selection.Request.SnapshotID != snapshot.SnapshotID || commit.Selection.Request.BrainRef != snapshot.ModelProfileRef || commit.Selection.InstallLockRef != snapshot.InstallLockRef {
			return out, api.E("forbidden", "remote_original_parent_knowledge_changed")
		}
		digest, err := api.Digest(commit.Selection)
		if err != nil {
			return out, err
		}
		out.Knowledge = &collaboration.RemoteKnowledgeReference{SelectionID: commit.Selection.ID, SelectionDigest: digest, PacketRef: commit.PacketRef}
		out.Controls = narrowRemoteControls(out.Controls, remoteControls(commit.Selection.EffectiveControls))
		caps := []api.ComponentRef{}
		for _, cap := range out.CapabilityRefs {
			if remoteComponentContains(commit.Selection.EffectiveCapabilityRefs, cap) {
				caps = append(caps, cap)
			}
		}
		out.CapabilityRefs = caps
	}
	upstream, err := g.a.RemoteAgent.ChildAdmissionTx(ctx, tx, current.ParentTask)
	if err != nil {
		return out, err
	}
	if upstream != nil {
		out.Controls = narrowRemoteControls(out.Controls, upstream.Controls)
		caps := []api.ComponentRef{}
		for _, cap := range out.CapabilityRefs {
			if remoteComponentContains(upstream.CapabilityRefs, cap) {
				caps = append(caps, cap)
			}
		}
		out.CapabilityRefs = caps
		if out.ModelScope != nil && (upstream.ModelScope == nil || !api.Equal(out.ModelScope, upstream.ModelScope)) {
			return out, api.E("forbidden", "remote_parent_model_scope_exceeded")
		}
	}
	for _, scope := range profile.Values.ActionScopes {
		allowed := upstream == nil
		if upstream != nil {
			for _, original := range upstream.ActionScopes {
				allowed = allowed || scope.CapabilityRef == original.CapabilityRef && remoteResourcesSubset(scope, original) && remoteStringsSubset(scope.Actions, original.Actions) && scope.Location == original.Location
			}
		}
		if allowed && remoteComponentContains(out.CapabilityRefs, scope.CapabilityRef) {
			out.ActionScopes = append(out.ActionScopes, scope)
		}
	}
	if len(current.Delegation.PermissionRefs) == 0 {
		for i := range out.Controls.MaxCallCostBound {
			out.Controls.MaxCallCostBound[i].Value = "0"
		}
	}
	return out, nil
}

func remoteScopeUseRequest(scope runtime.Scope, subject api.ObjectRef, current task.DelegationScope, location string, admission collaboration.RemoteParentAdmission) (governance.UseRequest, error) {
	admission.UseRef, admission.UseIntentHash, admission.UseDigest = nil, "", ""
	hash, err := api.Digest(struct {
		Input      task.DelegateInput                  `json:"input"`
		Allocation api.ObjectRef                       `json:"allocation_ref"`
		Admission  collaboration.RemoteParentAdmission `json:"admission"`
	}{current.Delegation.DelegateInput, current.Delegation.AllocationRef, admission})
	if err != nil {
		return governance.UseRequest{}, err
	}
	resources, actions := []string{}, []string{}
	for _, item := range admission.ActionScopes {
		for _, resource := range item.Resources {
			if !containsString(resources, resource) {
				resources = append(resources, resource)
			}
		}
		for _, action := range item.Actions {
			if !containsString(actions, action) {
				actions = append(actions, action)
			}
		}
	}
	if admission.ModelScope != nil {
		if !containsString(resources, "model-input") {
			resources = append(resources, "model-input")
		}
		if !containsString(actions, "model.request") {
			actions = append(actions, "model.request")
		}
	}
	return governance.UseRequest{UseID: stableID("use", "remote-agent/"+current.Delegation.CreationKey), SubjectRef: subject, TargetRef: scope.Ref(current.Delegation.DelegationID, 1), TargetKind: "delegation", IntentHash: hash, GrantRefs: append([]api.ObjectRef{}, current.Delegation.PermissionRefs...), RequestedUnits: append([]api.Amount{}, current.Delegation.Budget...), Resources: resources, Actions: actions, Recipient: current.Delegation.ReceiverID, Location: location, Purposes: []string{"delegate"}, StartBefore: current.Delegation.Deadline}, nil
}

func (g remoteScopeAuthority) FreezeScopeTx(ctx context.Context, tx runtime.Tx, actor runtime.Auth, current task.DelegationScope, profile collaboration.RemoteAgentProfile) (collaboration.RemoteParentAdmission, error) {
	view, found, err := g.a.Task.LatestTaskSnapshotTx(ctx, tx, g.a.ServiceAuth, current.ParentTask.TaskID)
	if err != nil {
		return collaboration.RemoteParentAdmission{}, err
	}
	if !found {
		return collaboration.RemoteParentAdmission{}, api.E("dependency_unavailable", "original_parent_snapshot_required")
	}
	out, err := g.originalAdmissionTx(ctx, tx, actor, current, profile, view)
	if err != nil {
		return out, err
	}
	count, err := g.a.RemoteAgent.CountSnapshotDelegationsTx(ctx, tx, current.ParentTask.TaskID, out.DecisionID)
	if err != nil {
		return out, err
	}
	if count >= out.Controls.MaxDelegationsPerDecision || uint64(len(current.Delegation.AncestorTaskRefs)) >= out.Controls.MaxDepth {
		return out, api.E("forbidden", "remote_parent_delegation_control_exceeded")
	}
	if len(out.ActionScopes) == 0 && out.ModelScope == nil {
		return out, nil
	}
	request, err := remoteScopeUseRequest(tx.Scope(), actor.Ref(tx.Scope().OwnerID), current, profile.Values.Location, out)
	if err != nil {
		return out, err
	}
	use, err := g.a.Governance.UseTx(ctx, tx, actor, request)
	if err != nil {
		return out, err
	}
	if use.Decision != "allowed" {
		return out, api.E("forbidden", "remote_parent_permission_denied")
	}
	out.UseRef = new(tx.Scope().Ref(use.UseID, 1))
	out.UseIntentHash, out.UseDigest = use.IntentHash, use.RequestDigest
	return out, nil
}

func (g remoteScopeAuthority) CheckScopeTx(ctx context.Context, tx runtime.Tx, actor runtime.Auth, current task.DelegationScope, profile collaboration.RemoteAgentProfile, original collaboration.RemoteParentAdmission) error {
	snapshot, err := g.a.Task.DecisionSnapshotTx(ctx, tx, g.a.ServiceAuth, original.DecisionID)
	if err != nil {
		return err
	}
	view := task.TaskSnapshotView{Snapshot: snapshot, Intent: api.DecisionDispatchIntent{DecisionID: original.DecisionID, TaskRef: snapshot.TaskRef, SnapshotRef: original.SnapshotRef, SnapshotRevision: snapshot.Revision}}
	expected, err := g.originalAdmissionTx(ctx, tx, actor, current, profile, view)
	if err != nil {
		return err
	}
	compare := original
	compare.UseRef, compare.UseIntentHash, compare.UseDigest = nil, "", ""
	if !api.Equal(expected, compare) {
		return api.E("forbidden", "original_parent_admission_changed")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	until, err := api.ParseTime(original.ValidUntil)
	if err != nil {
		return err
	}
	if !now.Before(until) {
		return api.E("expired", "remote_parent_permission_expired")
	}
	if original.UseRef == nil {
		if len(original.ActionScopes) > 0 || original.ModelScope != nil {
			return api.E("forbidden", "remote_parent_permission_required")
		}
		return nil
	}
	use, err := g.a.Governance.CheckUseHeadsTx(ctx, tx, actor, *original.UseRef, tx.Scope().Ref(current.Delegation.DelegationID, 1), original.UseIntentHash)
	if err != nil {
		return err
	}
	if use.TargetKind != "delegation" || use.RequestDigest != original.UseDigest || !api.Equal(use.CostBound, current.Delegation.Budget) || use.Recipient != profile.Values.ReceiverID || use.Location != profile.Values.Location {
		return api.E("forbidden", "remote_original_use_changed")
	}
	// 原Use的开始窗已在首次Delegate事务UseTx消费。后续调用只沿本次
	// 当前父事实签发有限scope receipt，不能重消费once或延长原Task/Allocation期限。
	return nil
}

func (g remoteScopeAuthority) ObserveUsageTx(ctx context.Context, tx runtime.Tx, packet collaboration.RemoteCreateInput, usage api.UsageSnapshot) error {
	if packet.ParentAdmission == nil || packet.ParentAdmission.UseRef == nil {
		return nil
	}
	allocation, err := g.a.Task.ReadAllocationTx(ctx, tx, g.a.ServiceAuth, packet.AllocationRef.ObjectID)
	if err != nil {
		return err
	}
	if usage.SourceRef.TenantID != tx.Scope().TenantID || usage.SourceRef.OwnerID != tx.Scope().OwnerID || usage.SourceRef.ObjectID != packet.DelegationRef.ObjectID || usage.SourceRef.Revision != usage.UsageRevision || packet.SubjectRef.TenantID != tx.Scope().TenantID || packet.SubjectRef.OwnerID != tx.Scope().OwnerID || packet.SubjectRef.Revision == 0 || packet.AllocationRef != tx.Scope().Ref(allocation.AllocationID, 1) || allocation.ParentTaskRef != packet.Input.ParentTaskRef || allocation.ReceiverID != packet.Input.ReceiverID || !api.Equal(allocation.Limits, packet.Input.Budget) {
		return api.E("forbidden", "original_delegation_usage_changed")
	}
	// 只重建原请求摘要作绑定检查；不再次检查开始窗或当前Grant活性，
	// 撤回新开始权不能抹掉已发生的原费用，更不会恢复已消费的once。
	admission := *packet.ParentAdmission
	location := ""
	if admission.ModelScope != nil {
		location = admission.ModelScope.Location
	}
	for _, action := range admission.ActionScopes {
		if location == "" {
			location = action.Location
		}
		if action.Location != location || action.Recipient != packet.Input.ReceiverID {
			return api.E("forbidden", "original_delegation_use_location_changed")
		}
	}
	current := task.DelegationScope{Delegation: task.Delegation{DelegateInput: packet.Input, CreationKey: packet.CreationKey, AllocationRef: packet.AllocationRef}, Allocation: allocation, SubjectRef: packet.SubjectRef}
	request, err := remoteScopeUseRequest(tx.Scope(), packet.SubjectRef, current, location, admission)
	if err != nil {
		return err
	}
	requestDigest, err := api.Digest(request)
	if err != nil {
		return err
	}
	if admission.UseRef == nil || *admission.UseRef != tx.Scope().Ref(request.UseID, 1) || admission.UseIntentHash != request.IntentHash || admission.UseDigest != requestDigest || location == "" {
		return api.E("forbidden", "original_delegation_use_changed")
	}
	_, err = g.a.Governance.ApplySettlementTx(ctx, tx, governance.SettleRequest{UseID: admission.UseRef.ObjectID, Usage: usage})
	return err
}

func remoteActionWithinDeadline(now time.Time, deadline string, controls collaboration.RemoteControlLimits, until string) error {
	end, err := api.ParseTime(deadline)
	if err != nil {
		return err
	}
	bound, err := api.ParseTime(until)
	if err != nil {
		return err
	}
	if !now.Before(end) || end.After(bound) || end.Sub(now) > time.Duration(controls.MaxActionDurationSeconds)*time.Second {
		return api.E("forbidden", "remote_parent_action_duration_exceeded")
	}
	return nil
}
