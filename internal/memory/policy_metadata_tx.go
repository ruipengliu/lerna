package memory

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// RegisteredPolicyTx 供受信管理装配读取准确原政策，不授权 Content、不安装政策或初始化 head。
// 缺失只接受 repository 的原缺失标记；混合故障、认证错误及不可用政策均保留拒绝。
func (s *Service) RegisteredPolicyTx(ctx context.Context, tx runtime.Tx, scope runtime.Scope, auth runtime.Auth, ref api.ComponentRef) (Policy, bool, error) {
	if s.Store == nil || scope.DatabaseID != s.Store.ID() || tx.Scope() != scope || !api.ValidID(scope.TenantID) || !api.ValidID(scope.OwnerID) {
		return Policy{}, false, api.E("forbidden", "policy_scope_mismatch")
	}
	if err := checkAuth(scope, auth); err != nil {
		return Policy{}, false, err
	}
	if !auth.HasRole("content_admin") && !auth.HasRole("memory_admin") {
		return Policy{}, false, api.E("forbidden", "policy_management_required")
	}
	if s.Authorization == nil {
		return Policy{}, false, api.E("dependency_unavailable", "policy_authority_not_configured")
	}
	if err := s.currentAuth(ctx, tx, auth); err != nil {
		return Policy{}, false, err
	}
	if err := api.ValidateRecord("ComponentRef", ref); err != nil {
		return Policy{}, false, err
	}
	var policy Policy
	_, err := tx.Get(ctx, "content.policies", policyKey(ref), &policy)
	if err == runtime.ErrNotFound {
		return Policy{}, false, nil
	}
	if err != nil {
		return Policy{}, false, err
	}
	if policy.PolicyRef != ref || policy.State != "active" {
		return Policy{}, true, api.E("forbidden", "policy_unavailable")
	}
	if err = policyValid(policy); err != nil {
		return Policy{}, true, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return Policy{}, true, err
	}
	expires, err := api.ParseTime(policy.Values.RetainUntil)
	if err != nil {
		return Policy{}, true, err
	}
	if !now.Before(expires) {
		return Policy{}, true, api.E("gone", "retention_expired")
	}
	return policy, true, nil
}
