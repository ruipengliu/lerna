package collaboration

import (
	"context"
	"sort"
	"sync"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// RemoteAgentValues 是显式配对的上限；正文和模型不能给远端增加权限。
type RemoteAgentValues struct {
	ParentOwnerID    string              `json:"parent_owner_id"`
	ReceiverID       string              `json:"receiver_id"`
	AgentBindingRef  api.ObjectRef       `json:"agent_binding_ref"`
	PolicyRef        api.ComponentRef    `json:"policy_ref"`
	InstallLockRef   api.ComponentRef    `json:"install_lock_ref"`
	SubjectRefs      []api.ObjectRef     `json:"subject_refs"`
	PermissionRefs   []api.ObjectRef     `json:"permission_refs"`
	CapabilityRefs   []api.ComponentRef  `json:"capability_refs"`
	BindingRefs      []api.ObjectRef     `json:"binding_refs"`
	ResourceRefs     []api.ComponentRef  `json:"resource_refs"`
	BudgetLimits     []api.Amount        `json:"budget_limits"`
	MaxDepth         uint64              `json:"max_depth"`
	MaxInputs        uint64              `json:"max_inputs"`
	Location         string              `json:"location"`
	MaterialPurposes []string            `json:"material_purposes"`
	ActionScopes     []RemoteActionScope `json:"action_scopes,omitempty"`
	ModelScope       *RemoteModelScope   `json:"model_scope,omitempty"`
}
type RemoteAgentProfile struct {
	ProfileRef api.ComponentRef  `json:"profile_ref"`
	Values     RemoteAgentValues `json:"values"`
}

const maxRemoteMaterialPurposes = 32

// RemoteAgentProfileSchema 限定显式配对配置；用途仍逐项受原 source policy
// 约束。上限允许完整执行/对账消费者，不自动为旧 profile 增加用途。
func RemoteAgentProfileSchema() api.Schema {
	schema := api.SchemaFor[RemoteAgentProfile]()
	values := schema["properties"].(map[string]any)["values"].(api.Schema)
	properties := values["properties"].(map[string]any)
	for _, name := range []string{"permission_refs", "capability_refs", "binding_refs", "resource_refs"} {
		array := properties[name].(api.Schema)
		array["maxItems"] = 64
		properties[name] = api.Schema{"anyOf": []any{array, api.Schema{"type": "null"}}}
	}
	purposes := api.Array(api.Schema{"type": "string", "minLength": 1, "maxLength": 128}, 0, maxRemoteMaterialPurposes)
	purposes["uniqueItems"] = true
	// 历史无材料用途配置的 nil 仍表示零用途，不改其原 digest/身份。
	properties["material_purposes"] = api.Schema{"anyOf": []any{purposes, api.Schema{"type": "null"}}}
	return schema
}

func NewRemoteAgentProfile(id, version string, v RemoteAgentValues) (RemoteAgentProfile, error) {
	digest, err := api.Digest(v)
	p := RemoteAgentProfile{api.ComponentRef{ComponentID: id, Version: version, Digest: digest}, v}
	if err == nil {
		err = validateRemoteProfile(p)
	}
	if err != nil {
		return RemoteAgentProfile{}, err
	}
	var frozen RemoteAgentProfile
	err = api.Decode(api.Raw(p), &frozen)
	return frozen, err
}
func validateRemoteProfile(p RemoteAgentProfile) error {
	validator, err := api.NewValidator(RemoteAgentProfileSchema())
	if err != nil {
		return err
	}
	if err = validator.Validate(api.Raw(p)); err != nil {
		return api.E("invalid_request", "remote_agent_profile_schema_invalid")
	}
	v := p.Values
	digest, err := api.Digest(v)
	if err != nil {
		return err
	}
	if api.ValidateRecord("ComponentRef", p.ProfileRef) != nil || digest != p.ProfileRef.Digest || !api.ValidID(v.ParentOwnerID) || !api.ValidID(v.ReceiverID) || v.ParentOwnerID == v.ReceiverID || v.AgentBindingRef.OwnerID != v.ParentOwnerID || api.ValidateRecord("ObjectRef", v.AgentBindingRef) != nil || api.ValidateRecord("ComponentRef", v.PolicyRef) != nil || api.ValidateRecord("ComponentRef", v.InstallLockRef) != nil || len(v.SubjectRefs) == 0 || len(v.SubjectRefs) > 64 || len(v.PermissionRefs) > 64 || len(v.BindingRefs) > 64 || len(v.CapabilityRefs) > 64 || len(v.ResourceRefs) > 64 || len(v.BudgetLimits) == 0 || len(v.BudgetLimits) > 16 || v.MaxDepth == 0 || v.MaxDepth > 4 || v.MaxInputs > 32 || v.Location == "" {
		return api.E("invalid_request", "remote_agent_profile_invalid")
	}
	for _, refs := range [][]api.ObjectRef{v.SubjectRefs, v.PermissionRefs, v.BindingRefs} {
		seen := map[string]bool{}
		for _, ref := range refs {
			key, _ := api.Digest(ref)
			if api.ValidateRecord("ObjectRef", ref) != nil || ref.TenantID != v.AgentBindingRef.TenantID || seen[key] {
				return api.E("invalid_request", "remote_agent_scope_invalid")
			}
			seen[key] = true
		}
	}
	for _, ref := range v.SubjectRefs {
		if ref.OwnerID != v.ParentOwnerID {
			return api.E("invalid_request", "remote_subject_owner_mismatch")
		}
	}
	for _, refs := range [][]api.ComponentRef{v.CapabilityRefs, v.ResourceRefs} {
		seen := map[string]bool{}
		for _, ref := range refs {
			key, _ := api.Digest(ref)
			if api.ValidateRecord("ComponentRef", ref) != nil || seen[key] {
				return api.E("invalid_request", "remote_component_invalid")
			}
			seen[key] = true
		}
	}
	if len(v.Location) > 128 || len(v.MaterialPurposes) > maxRemoteMaterialPurposes {
		return api.E("invalid_request", "remote_material_purpose_limit")
	}
	purposes := map[string]bool{}
	for _, purpose := range v.MaterialPurposes {
		if purpose == "" || len(purpose) > 128 || purposes[purpose] {
			return api.E("invalid_request", "remote_material_purpose_invalid")
		}
		purposes[purpose] = true
	}
	if err := validateRemoteActions(p); err != nil {
		return err
	}
	return api.ValidateAmounts(v.BudgetLimits)
}

// Authority 只核当前本库配对和主体；固定 source owner 的消息不携带可自授权角色。
type RemoteAuthority interface {
	CheckPeerTx(context.Context, runtime.Tx, runtime.Auth, string) error
	ResolveSubjectTx(context.Context, runtime.Tx, api.ObjectRef, RemoteAgentProfile, bool) (runtime.Auth, error)
}
type RemotePeer struct {
	Scope  runtime.Scope
	Client *harness.Client
	Keys   *platform.Keyring
}
type RemoteConfig struct {
	Store        runtime.Store
	Scope        runtime.Scope
	Registry     *runtime.Registry
	Memory       *memory.Service
	ProofPolicy  memory.Policy
	Keys         *platform.Keyring
	SigningKeyID string
	Auth         runtime.Auth
	Authority    RemoteAuthority
	ScopeGate    RemoteScopeAuthority
	Profiles     []RemoteAgentProfile
	Peers        []RemotePeer
	Participants []string
	Local        task.CollaborationPort
	// MaterialPrincipal仅由受信宿主显式选择；原SourcePolicy必须分别允许它。
	// nil不为另一个主体自动取得材料；普通远端合同和原user holder保持不变。
	MaterialPrincipal *runtime.Auth
}
type Remote struct {
	cfg      RemoteConfig
	parts    []string
	profiles map[string]RemoteAgentProfile
	peers    map[string]RemotePeer
	mu       sync.RWMutex
	task     *task.Service
}

func NewRemote(c RemoteConfig) (*Remote, error) {
	if c.Store == nil || c.Scope.DatabaseID != c.Store.ID() || !api.ValidID(c.Scope.OwnerID) || !api.ValidID(c.Scope.TenantID) || c.Registry == nil || c.Keys == nil || c.SigningKeyID == "" || c.Authority == nil || c.Auth.TenantID != c.Scope.TenantID || !api.ValidID(c.Auth.SubjectID) || c.Auth.CredentialGeneration == 0 || !c.Auth.HasRole("service") || len(c.Profiles) == 0 || len(c.Profiles) > 64 || len(c.Peers) > 16 {
		return nil, api.E("unsupported", "remote_agent_authority_unconfigured")
	}
	if c.MaterialPrincipal != nil {
		if !api.Equal(*c.MaterialPrincipal, c.Auth) {
			return nil, api.E("forbidden", "remote_material_principal_unpaired")
		}
		frozen := *c.MaterialPrincipal
		frozen.Roles = append([]string{}, frozen.Roles...)
		c.MaterialPrincipal = &frozen
	}
	key, ok := c.Keys.Keys[c.SigningKeyID]
	if !ok || key.Private == nil || key.Issuer != c.Scope.OwnerID || key.TenantID != c.Scope.TenantID {
		return nil, api.E("forbidden", "remote_agent_signer_unpaired")
	}
	out := &Remote{cfg: c, profiles: map[string]RemoteAgentProfile{}, peers: map[string]RemotePeer{}}
	for _, p := range c.Profiles {
		if err := validateRemoteProfile(p); err != nil {
			return nil, err
		}
		if p.Values.AgentBindingRef.TenantID != c.Scope.TenantID || p.Values.ParentOwnerID != c.Scope.OwnerID && p.Values.ReceiverID != c.Scope.OwnerID {
			return nil, api.E("forbidden", "remote_agent_profile_scope_mismatch")
		}
		key, _ := api.Digest(p.ProfileRef)
		if _, exists := out.profiles[key]; exists {
			return nil, api.E("invalid_request", "duplicate_remote_profile")
		}
		var frozen RemoteAgentProfile
		if err := api.Decode(api.Raw(p), &frozen); err != nil {
			return nil, err
		}
		out.profiles[key] = frozen
	}
	for _, p := range c.Peers {
		if p.Scope.TenantID != c.Scope.TenantID || p.Scope.OwnerID == c.Scope.OwnerID || !api.ValidID(p.Scope.OwnerID) || !api.ValidID(p.Scope.DatabaseID) || p.Client == nil || p.Keys == nil || p.Client.Discovery.LogicalServiceID != p.Scope.OwnerID || p.Client.Discovery.IdentityScope != p.Scope.TenantID+"/"+p.Scope.OwnerID+"/"+p.Scope.DatabaseID {
			return nil, api.E("invalid_request", "remote_peer_scope_invalid")
		}
		if _, exists := out.peers[p.Scope.OwnerID]; exists {
			return nil, api.E("invalid_request", "duplicate_remote_peer")
		}
		out.peers[p.Scope.OwnerID] = p
	}
	parts := map[string]bool{"collaboration": true, "task": true, "content": true, "memory": true}
	for _, part := range c.Participants {
		if part == "" {
			return nil, api.E("invalid_request", "remote_participant_required")
		}
		parts[part] = true
	}
	for part := range parts {
		out.parts = append(out.parts, part)
	}
	sort.Strings(out.parts)
	return out, nil
}
func (r *Remote) BindTask(s *task.Service) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s == nil || r.task != nil {
		return api.E("invalid_state", "remote_task_binding_fixed")
	}
	r.task = s
	return nil
}
func (r *Remote) service() (*task.Service, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.task == nil {
		return nil, api.E("unsupported", "remote_task_unbound")
	}
	return r.task, nil
}
func (r *Remote) checkScope(s runtime.Scope) error {
	if s != r.cfg.Scope {
		return api.E("forbidden", "remote_collaboration_scope_mismatch")
	}
	return nil
}
func (r *Remote) within(ctx context.Context, fn func(runtime.Tx) error) error {
	status, err := r.cfg.Store.Within(ctx, r.cfg.Scope, r.parts, fn)
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
func (r *Remote) CheckCollaborationTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, kind, receiver string) error {
	if err := r.checkScope(tx.Scope()); err != nil {
		return err
	}
	if receiver == r.cfg.Scope.OwnerID && r.cfg.Local != nil {
		if local, ok := r.cfg.Local.(task.CollaborationAdmission); ok {
			return local.CheckCollaborationTx(ctx, tx, a, kind, receiver)
		}
	}
	if kind == "session" {
		return r.sessionAdmissionTx(ctx, tx, a, receiver)
	}
	if kind != "delegate" && kind != "transfer" {
		return api.E("unsupported", "remote_reusable_session_unconfigured")
	}
	if _, ok := r.peers[receiver]; !ok {
		return api.E("unsupported", "remote_agent_transport_unconfigured")
	}
	if _, err := r.service(); err != nil {
		return err
	}
	for _, p := range r.profiles {
		if p.Values.ParentOwnerID != r.cfg.Scope.OwnerID || p.Values.ReceiverID != receiver {
			continue
		}
		for _, subject := range p.Values.SubjectRefs {
			if subject == a.Ref(r.cfg.Scope.OwnerID) {
				_, err := r.cfg.Authority.ResolveSubjectTx(ctx, tx, subject, p, false)
				return err
			}
		}
	}
	return api.E("forbidden", "remote_agent_subject_scope_exceeded")
}

func profileKey(ref api.ComponentRef) string { key, _ := api.Digest(ref); return key }
