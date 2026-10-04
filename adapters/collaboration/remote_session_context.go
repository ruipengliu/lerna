package collaboration

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// RemoteSessionContext 只是准确获准历史的来源；不改变本次 Goal、Grant 或 Delegation 输入。
type RemoteSessionContext struct {
	ChildRef         api.ObjectRef        `json:"child_ref"`
	SessionRef       api.ObjectRef        `json:"session_ref"`
	HistoryCutoff    uint64               `json:"history_cutoff"`
	SourceCommandRef api.ObjectRef        `json:"source_command_ref"`
	Binding          RemoteSessionBinding `json:"binding"`
}

// 这只是当前已验证的普通 delegate 入口，不冒充 StoredCommand 或原回执。
type remoteDelegationCommand struct {
	Command api.Command
	Auth    runtime.Auth
}

func (r *Remote) originalSessionContextTx(ctx context.Context, tx runtime.Tx, d task.Delegation, subject api.ObjectRef, current *remoteDelegationCommand) (*RemoteSessionContext, error) {
	if current != nil {
		c := current.Command
		if c.Method != "collaboration.delegate" || c.LogicalServiceID != tx.Scope().OwnerID || c.TargetID != d.DelegationID || d.CommandRef != tx.Scope().Ref(c.CommandID, 1) || current.Auth.Ref(tx.Scope().OwnerID) != subject {
			return nil, api.E("forbidden", "original_delegate_command_changed")
		}
		var input task.DelegateInput
		if err := api.Decode(c.Payload, &input); err != nil {
			return nil, err
		}
		if !api.Equal(input, d.DelegateInput) {
			return nil, api.E("forbidden", "original_delegate_input_changed")
		}
		return nil, nil
	}
	original, err := tx.LoadCommand(ctx, d.CommandRef.ObjectID)
	if err != nil {
		return nil, err
	}
	if original.Command.Method != "child.send" {
		return nil, nil
	}
	if d.CommandRef != tx.Scope().Ref(original.Command.CommandID, 1) {
		return nil, api.E("forbidden", "original_child_session_command_scope_changed")
	}
	var input task.ChildSendInput
	if err = api.Decode(original.Command.Payload, &input); err != nil {
		return nil, err
	}
	if input.Mode != "new_goal" || input.Delegation == nil || input.Delegation.DelegationID != d.DelegationID || !api.Equal(*input.Delegation, d.DelegateInput) || original.Command.ExpectedRevision == nil || original.Command.TargetID != input.ChildID || original.PrincipalID != subject.ObjectID {
		return nil, api.E("forbidden", "original_child_session_send_changed")
	}
	var handle task.ChildHandle
	if err = tx.GetVersion(ctx, "task.children", input.ChildID, *original.Command.ExpectedRevision, &handle); err != nil {
		return nil, err
	}
	if handle.Revision != *original.Command.ExpectedRevision || handle.State != "open" || handle.ChildSessionRef == nil || handle.ChildSessionRef.OwnerID != d.ReceiverID || handle.SubjectID != subject.ObjectID || handle.SubjectGeneration != subject.Revision {
		return nil, api.E("forbidden", "original_child_session_scope_changed")
	}
	if !api.Equal(handle.ActiveDelegationRef, input.ExpectedActiveDelegationRef) {
		return nil, api.E("forbidden", "original_child_session_cas_changed")
	}
	binding, p, err := r.sessionBinding(handle)
	if err != nil {
		return nil, err
	}
	if p.Values.AgentBindingRef != d.AgentBindingRef || p.Values.ReceiverID != d.ReceiverID {
		return nil, api.E("forbidden", "original_child_session_binding_changed")
	}
	// mapping 在同一 child.send 事务创建，原库版本保留；首次 plan 可同 Tx 读取。
	var mapped api.ObjectRef
	if _, err = tx.Get(ctx, "task.child_mappings", input.ChildID+"/"+d.DelegationID, &mapped); err != nil {
		return nil, err
	}
	if mapped != tx.Scope().Ref(d.DelegationID, 1) {
		return nil, api.E("idempotency_conflict", "original_child_session_mapping_changed")
	}
	return &RemoteSessionContext{ChildRef: tx.Scope().Ref(input.ChildID, handle.Revision), SessionRef: *handle.ChildSessionRef, HistoryCutoff: input.HistoryCutoff, SourceCommandRef: d.CommandRef, Binding: binding}, nil
}
func (r *Remote) ChildSessionContext(ctx context.Context, scope runtime.Scope, actual api.Task) (*RemoteSessionContext, error) {
	if err := r.checkScope(scope); err != nil {
		return nil, err
	}
	birth, err := r.childOriginal(ctx, actual.TaskID)
	if api.IsCode(err, "not_found") {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	meta := birth.Packet.SessionContext
	if meta == nil {
		return nil, nil
	}
	if _, err = r.validatePacket(birth.Packet); err != nil {
		return nil, err
	}
	if meta.SessionRef.OwnerID != scope.OwnerID || meta.SessionRef.TenantID != scope.TenantID || meta.SourceCommandRef != birth.Packet.OriginalCommandRef || meta.ChildRef.OwnerID != birth.Packet.DelegationRef.OwnerID || meta.ChildRef.TenantID != scope.TenantID || api.ValidateRecord("ObjectRef", meta.ChildRef) != nil || api.ValidateRecord("ObjectRef", meta.SessionRef) != nil || meta.Binding.ProfileRef != birth.Packet.ProfileRef || meta.Binding.AgentBindingRef != birth.Packet.Input.AgentBindingRef {
		return nil, api.E("forbidden", "original_child_session_context_changed")
	}
	authority, err := r.sessionAuthority()
	if err != nil {
		return nil, err
	}
	allowed := false
	for _, b := range authority.RemoteSessionBindings() {
		allowed = allowed || api.Equal(b, meta.Binding)
	}
	if !allowed {
		return nil, api.E("forbidden", "original_child_session_context_unconfigured")
	}
	copy := *meta
	return &copy, nil
}

// 原 send 的不可变身份与当前本地主体/AccessScope分开核验；普通历史不会授权本次Use。
func (r *Remote) checkOriginalSessionContextTx(ctx context.Context, tx runtime.Tx, actor runtime.Auth, d task.Delegation, expected *RemoteSessionContext) error {
	return r.checkOriginalSessionCommandTx(ctx, tx, actor, d, expected, nil)
}

func (r *Remote) checkOriginalSessionCommandTx(ctx context.Context, tx runtime.Tx, actor runtime.Auth, d task.Delegation, expected *RemoteSessionContext, current *remoteDelegationCommand) error {
	actual, err := r.originalSessionContextTx(ctx, tx, d, actor.Ref(tx.Scope().OwnerID), current)
	if err != nil {
		return err
	}
	if !api.Equal(actual, expected) {
		return api.E("idempotency_conflict", "original_child_session_context_changed")
	}
	if actual == nil {
		return nil
	}
	if r.cfg.Memory == nil {
		return api.E("unsupported", "remote_session_history_source_gate_unconfigured")
	}
	_, err = r.cfg.Memory.CheckContentTx(ctx, tx, actor, actual.Binding.AccessScopeRef, "child.new_goal", r.profiles[profileKey(actual.Binding.ProfileRef)].Values.Location, false)
	return err
}

// Packet/SDK边界先封闭元数据范围；跨owner动态读取仍由原Session负责方当前门禁裁决。
func (r *Remote) validateOriginalSessionContext(p RemoteCreateInput, profile RemoteAgentProfile) error {
	meta := p.SessionContext
	if meta == nil {
		return nil
	}
	if api.ValidateRecord("ObjectRef", meta.ChildRef) != nil || api.ValidateRecord("ObjectRef", meta.SessionRef) != nil || api.ValidateRecord("ObjectRef", meta.SourceCommandRef) != nil || api.ValidateRecord("ContentRef", meta.Binding.AccessScopeRef) != nil || meta.ChildRef.OwnerID != p.DelegationRef.OwnerID || meta.ChildRef.TenantID != r.cfg.Scope.TenantID || meta.SessionRef.OwnerID != p.Input.ReceiverID || meta.SessionRef.TenantID != r.cfg.Scope.TenantID || meta.SourceCommandRef != p.OriginalCommandRef || meta.Binding.ProfileRef != p.ProfileRef || meta.Binding.AgentBindingRef != p.Input.AgentBindingRef || meta.Binding.InstallLockRef != profile.Values.InstallLockRef || meta.Binding.AccessScopeRef.OwnerID != p.DelegationRef.OwnerID || meta.Binding.AccessScopeRef.TenantID != r.cfg.Scope.TenantID {
		return api.E("forbidden", "original_child_session_context_changed")
	}
	authority, err := r.sessionAuthority()
	if err != nil {
		return err
	}
	for _, binding := range authority.RemoteSessionBindings() {
		if api.Equal(binding, meta.Binding) {
			return nil
		}
	}
	return api.E("forbidden", "original_child_session_context_unconfigured")
}
