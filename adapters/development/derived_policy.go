package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func intersectValues(base, limit []string) []string {
	result := []string{}
	for _, value := range base {
		if containsString(limit, value) {
			result = append(result, value)
		}
	}
	return result
}

// 准确源policy的交集只收紧本方当前上限；原File同策略仍保留原PolicyRef。
func (a *App) publicationPolicyTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, sources []api.ContentRef) (memory.Policy, error) {
	policy := a.ContentPolicy
	for _, ref := range sources {
		source, err := a.Memory.SourcePolicySnapshotTx(ctx, tx, auth, ref, "content.write", "cloud")
		if err != nil {
			return memory.Policy{}, err
		}
		values := source.Policy.Values
		policy.Values.Subjects = intersectValues(policy.Values.Subjects, values.Subjects)
		policy.Values.Purposes = intersectValues(policy.Values.Purposes, values.Purposes)
		policy.Values.Locations = intersectValues(policy.Values.Locations, values.Locations)
		until, err := api.ParseTime(policy.Values.RetainUntil)
		if err != nil {
			return memory.Policy{}, err
		}
		limit, err := api.ParseTime(values.RetainUntil)
		if err != nil {
			return memory.Policy{}, err
		}
		if limit.Before(until) {
			policy.Values.RetainUntil = values.RetainUntil
		}
		policy.Values.Continuous = policy.Values.Continuous || values.Continuous
		policy.Values.IndependentDerived = policy.Values.IndependentDerived && values.IndependentDerived
	}
	if api.Equal(policy.Values, a.ContentPolicy.Values) {
		return a.ContentPolicy, nil
	}
	if len(policy.Values.Subjects) == 0 || len(policy.Values.Purposes) == 0 || len(policy.Values.Locations) == 0 {
		return memory.Policy{}, api.E("forbidden", "derived_source_scope_empty")
	}
	digest, err := api.Digest(policy.Values)
	if err != nil {
		return memory.Policy{}, err
	}
	policy.PolicyRef = api.ComponentRef{ComponentID: stableID("policy", "derived/"+digest), Version: "1.0.0", Digest: digest}
	// 受信管理方仅安装上述已核交集，不采用调用者自报策略或提升其管理角色。
	if err = a.Memory.InstallPolicyTx(ctx, tx, a.ServiceAuth, policy); err != nil {
		return memory.Policy{}, err
	}
	return policy, nil
}
