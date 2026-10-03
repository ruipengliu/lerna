package collaboration

import (
	"context"
	"regexp"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 父准入上限由受信宿主从原Decision/当前Grant取得，不由子模型生成。
type RemoteControlLimits struct {
	MaxInputBytes             uint64       `json:"max_input_bytes"`
	MaxOutputTokens           uint64       `json:"max_output_tokens"`
	MaxActionsPerDecision     uint64       `json:"max_actions_per_decision"`
	MaxDelegationsPerDecision uint64       `json:"max_delegations_per_decision"`
	MaxDepth                  uint64       `json:"max_depth"`
	MaxActionDurationSeconds  uint64       `json:"max_action_duration_seconds"`
	MaxCallCostBound          []api.Amount `json:"max_call_cost_bound"`
}

type RemoteActionScope struct {
	CapabilityRef api.ComponentRef `json:"capability_ref"`
	BindingRef    api.ObjectRef    `json:"binding_ref"`
	Resources     []string         `json:"resources"`
	// ResourceRefs与Resources逐项对应；Grant的namespace名称独立于组件ID格式。
	ResourceRefs []api.ComponentRef `json:"resource_refs"`
	Actions      []string           `json:"actions"`
	Recipient    string             `json:"recipient"`
	Location     string             `json:"location"`
}

// ModelScope 只限定受信装配已开放的出口；不能从子提案添加模型或接收方。
type RemoteModelScope struct {
	ModelProfileRef api.ComponentRef `json:"model_profile_ref"`
	Receiver        string           `json:"receiver"`
	Location        string           `json:"location"`
}

type RemoteKnowledgeReference struct {
	SelectionID     string         `json:"selection_id"`
	SelectionDigest string         `json:"selection_digest"`
	PacketRef       api.ContentRef `json:"packet_ref"`
}

type RemoteParentAdmission struct {
	TaskRef         api.ObjectRef             `json:"task_ref"`
	DecisionID      string                    `json:"decision_id"`
	SnapshotID      string                    `json:"snapshot_id"`
	SnapshotRef     api.ContentRef            `json:"snapshot_ref"`
	ModelProfileRef api.ComponentRef          `json:"model_profile_ref"`
	InstallLockRef  api.ComponentRef          `json:"install_lock_ref"`
	CapabilityRefs  []api.ComponentRef        `json:"capability_refs"`
	ActionScopes    []RemoteActionScope       `json:"action_scopes"`
	ModelScope      *RemoteModelScope         `json:"model_scope,omitempty"`
	Controls        RemoteControlLimits       `json:"controls"`
	ValidUntil      string                    `json:"valid_until"`
	Knowledge       *RemoteKnowledgeReference `json:"knowledge,omitempty"`
	UseRef          *api.ObjectRef            `json:"use_ref,omitempty"`
	UseIntentHash   string                    `json:"use_intent_hash,omitempty"`
	UseDigest       string                    `json:"use_digest,omitempty"`
}

// 只用于已显式同库装配的父事实；纯Tx，不获取Content正文或发RPC。
type RemoteScopeAuthority interface {
	FreezeScopeTx(context.Context, runtime.Tx, runtime.Auth, task.DelegationScope, RemoteAgentProfile) (RemoteParentAdmission, error)
	CheckScopeTx(context.Context, runtime.Tx, runtime.Auth, task.DelegationScope, RemoteAgentProfile, RemoteParentAdmission) error
	ObserveUsageTx(context.Context, runtime.Tx, RemoteCreateInput, api.UsageSnapshot) error
}

func componentSubset(refs, limits []api.ComponentRef) bool {
	for _, ref := range refs {
		found := false
		for _, limit := range limits {
			found = found || ref == limit
		}
		if !found {
			return false
		}
	}
	return true
}

func validateRemoteActions(profile RemoteAgentProfile) error {
	v := profile.Values
	if len(v.ActionScopes) > 16 || (len(v.ActionScopes) > 0 || v.ModelScope != nil) && len(v.PermissionRefs) == 0 {
		return api.E("forbidden", "remote_action_permission_required")
	}
	if m := v.ModelScope; m != nil && (api.ValidateRecord("ComponentRef", m.ModelProfileRef) != nil || m.Receiver == "" || len(m.Receiver) > 128 || m.Location != v.Location) {
		return api.E("forbidden", "remote_model_scope_invalid")
	}
	seen := map[string]bool{}
	resourceNames := map[api.ComponentRef]string{}
	resourceVersions := map[string]api.ComponentRef{}
	for _, scope := range v.ActionScopes {
		key, err := api.Digest([]any{scope.CapabilityRef, scope.BindingRef})
		if err != nil {
			return err
		}
		if !componentSubset([]api.ComponentRef{scope.CapabilityRef}, v.CapabilityRefs) || !subsetRefs([]api.ObjectRef{scope.BindingRef}, v.BindingRefs) || scope.BindingRef.OwnerID != v.ReceiverID || scope.Recipient != v.ReceiverID || scope.Location != v.Location || len(scope.Resources) == 0 || len(scope.Resources) > 32 || len(scope.ResourceRefs) != len(scope.Resources) || len(scope.Actions) == 0 || len(scope.Actions) > 8 || seen[key] {
			return api.E("forbidden", "remote_action_scope_invalid")
		}
		seen[key] = true
		for _, values := range [][]string{scope.Resources, scope.Actions} {
			unique := map[string]bool{}
			for _, value := range values {
				if value == "" || len(value) > 128 || unique[value] {
					return api.E("invalid_request", "remote_action_scope_invalid")
				}
				unique[value] = true
			}
		}
		for index, resource := range scope.Resources {
			ref := scope.ResourceRefs[index]
			if api.ValidateRecord("ComponentRef", ref) != nil || !componentSubset([]api.ComponentRef{ref}, v.ResourceRefs) {
				return api.E("forbidden", "remote_action_resource_undeclared")
			}
			if old, exists := resourceNames[ref]; exists && old != resource {
				return api.E("forbidden", "remote_action_resource_binding_changed")
			}
			if old, exists := resourceVersions[resource]; exists && old != ref {
				return api.E("forbidden", "remote_action_resource_binding_changed")
			}
			resourceNames[ref], resourceVersions[resource] = resource, ref
		}
	}
	return nil
}

func validateRemoteAdmission(packet RemoteCreateInput, profile RemoteAgentProfile) error {
	p := packet.ParentAdmission
	if p == nil {
		return nil
	}
	if api.ValidateRecord("ObjectRef", p.TaskRef) != nil || p.TaskRef.TenantID != packet.SubjectRef.TenantID || p.TaskRef.OwnerID != packet.DelegationRef.OwnerID || p.TaskRef.ObjectID != packet.Input.ParentTaskRef.ObjectID || !api.ValidID(p.DecisionID) || !api.ValidID(p.SnapshotID) || api.ValidateRecord("ContentRef", p.SnapshotRef) != nil || p.SnapshotRef.TenantID != packet.SubjectRef.TenantID || p.SnapshotRef.OwnerID != packet.DelegationRef.OwnerID || api.ValidateRecord("ComponentRef", p.ModelProfileRef) != nil || api.ValidateRecord("ComponentRef", p.InstallLockRef) != nil || !componentSubset(p.CapabilityRefs, profile.Values.CapabilityRefs) || len(p.CapabilityRefs) > 64 || len(p.ActionScopes) > 16 {
		return api.E("forbidden", "remote_parent_admission_invalid")
	}
	limits := p.Controls
	if limits.MaxInputBytes == 0 || limits.MaxInputBytes > 2<<20 || limits.MaxOutputTokens == 0 || limits.MaxOutputTokens > 65536 || limits.MaxActionsPerDecision > 16 || limits.MaxDelegationsPerDecision > 4 || limits.MaxDepth > profile.Values.MaxDepth || limits.MaxActionDurationSeconds == 0 || limits.MaxActionDurationSeconds > 3600 || len(limits.MaxCallCostBound) > 8 {
		return api.E("forbidden", "remote_parent_controls_invalid")
	}
	if err := api.ValidateAmounts(limits.MaxCallCostBound); err != nil {
		return err
	}
	if err := remoteBudget(limits.MaxCallCostBound, packet.Input.Budget); err != nil {
		return err
	}
	deadline, err := api.ParseTime(packet.Input.Deadline)
	if err != nil {
		return err
	}
	until, err := api.ParseTime(p.ValidUntil)
	if err != nil {
		return err
	}
	if until.After(deadline) {
		return api.E("forbidden", "remote_parent_window_expanded")
	}
	for _, scope := range p.ActionScopes {
		if !componentSubset([]api.ComponentRef{scope.CapabilityRef}, p.CapabilityRefs) {
			return api.E("forbidden", "remote_parent_capability_expanded")
		}
		found := false
		for _, original := range profile.Values.ActionScopes {
			found = found || api.Equal(scope, original)
		}
		if !found {
			return api.E("forbidden", "remote_parent_action_expanded")
		}
	}
	if p.Knowledge != nil {
		if !api.ValidID(p.Knowledge.SelectionID) || !remoteAdmissionDigest.MatchString(p.Knowledge.SelectionDigest) || api.ValidateRecord("ContentRef", p.Knowledge.PacketRef) != nil || p.Knowledge.PacketRef.OwnerID != p.TaskRef.OwnerID || p.Knowledge.PacketRef.TenantID != p.TaskRef.TenantID {
			return api.E("forbidden", "remote_parent_knowledge_invalid")
		}
	}
	if p.ModelScope != nil && (profile.Values.ModelScope == nil || !api.Equal(p.ModelScope, profile.Values.ModelScope)) {
		return api.E("forbidden", "remote_parent_model_expanded")
	}
	if p.UseRef != nil {
		if api.ValidateRecord("ObjectRef", *p.UseRef) != nil || p.UseRef.OwnerID != packet.DelegationRef.OwnerID || p.UseRef.TenantID != packet.SubjectRef.TenantID || p.UseRef.Revision != 1 || !remoteAdmissionDigest.MatchString(p.UseIntentHash) || !remoteAdmissionDigest.MatchString(p.UseDigest) {
			return api.E("forbidden", "remote_parent_use_invalid")
		}
	} else if p.UseIntentHash != "" || p.UseDigest != "" || len(p.ActionScopes) > 0 || p.ModelScope != nil {
		return api.E("forbidden", "remote_parent_use_required")
	}
	return nil
}

var remoteAdmissionDigest = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func (r *Remote) CountSnapshotDelegationsTx(ctx context.Context, tx runtime.Tx, parentTaskID, decisionID string) (uint64, error) {
	if err := r.checkScope(tx.Scope()); err != nil {
		return 0, err
	}
	rows, err := tx.List(ctx, remoteOutgoing, parentTaskID, "", 513)
	if err != nil {
		return 0, err
	}
	if len(rows) > 512 {
		return 0, api.E("overloaded", "remote_delegation_scan_limit")
	}
	var count uint64
	for _, row := range rows {
		var original remoteSent
		if err := row.Decode(&original); err != nil {
			return 0, err
		}
		if original.Packet.ParentAdmission != nil && original.Packet.ParentAdmission.DecisionID == decisionID {
			count++
		}
	}
	return count, nil
}

// ChildAdmissionTx返回本次已完整强核的原父上限；本机任务不合成远程许可。
func (r *Remote) ChildAdmissionTx(ctx context.Context, tx runtime.Tx, actual api.Task) (*RemoteParentAdmission, error) {
	if err := r.checkScope(tx.Scope()); err != nil {
		return nil, err
	}
	var origin remoteChildOrigin
	if err := tx.GetVersion(ctx, "collaboration.remote_children", actual.TaskID, 1, &origin); err != nil {
		if api.IsCode(err, "not_found") {
			return nil, nil
		}
		return nil, err
	}
	if err := r.CheckTaskCurrentTx(ctx, tx, actual, true); err != nil {
		return nil, err
	}
	var saved remoteReceived
	if _, err := tx.Get(ctx, remoteIncoming, origin.PacketID, &saved); err != nil {
		return nil, err
	}
	if saved.Packet.ParentAdmission == nil {
		return nil, api.E("unsupported", "remote_parent_admission_required")
	}
	var frozen RemoteParentAdmission
	if err := api.Decode(api.Raw(saved.Packet.ParentAdmission), &frozen); err != nil {
		return nil, err
	}
	return &frozen, nil
}
