package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type contextCapabilityGrant struct {
	Scope           runtime.Scope             `json:"scope"`
	Lookup          task.ContextLookupRequest `json:"lookup"`
	Subject         runtime.Auth              `json:"subject"`
	Check           governance.UseRequest     `json:"check"`
	DescriptionHash string                    `json:"description_hash"`
	DescriptionSize uint64                    `json:"description_size"`
}

// 原描述准入依据在任何出版 I/O 前固定。它只核已声明能力，不申请工具或预留。
func (a *App) prepareContextCapability(ctx context.Context, scope runtime.Scope, original task.ContextLookupRequest, descriptor actionDescriptor) ([]byte, error) {
	check := governance.UseRequest{UseID: stableID("use", "context-description/"+original.LookupID), SubjectRef: a.ServiceAuth.Ref(scope.OwnerID), TargetRef: scope.Ref(original.DecisionID, 1), TargetKind: "decision", IntentHash: descriptor.ConfigHash, GrantRefs: []api.ObjectRef{descriptor.GrantRef}, RequestedUnits: []api.Amount{}, Resources: descriptor.Resources, Actions: descriptor.Actions, Recipient: descriptor.Recipient, Location: descriptor.Location, Purposes: []string{"goal_action"}, StartBefore: original.ExpiresAt}
	raw, err := a.query(ctx, "grant.check", scope.OwnerID, check)
	if err != nil {
		return nil, err
	}
	var receipt governance.UseReceipt
	if err = api.Decode(raw, &receipt); err != nil {
		return nil, err
	}
	if receipt.Decision != "allowed" {
		return nil, api.E("forbidden", "capability_current_grant_unavailable")
	}
	body := api.Raw(actionDeclaration{Capability: descriptor.Capability, BindingRef: descriptor.BindingRef, InstallLockRef: descriptor.InstallLockRef, AllowedResources: descriptor.Resources, AllowedActions: descriptor.Actions})
	frozen := contextCapabilityGrant{scope, original, a.ServiceAuth, check, api.Hash(body), uint64(len(body))}
	status, err := a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		var old contextCapabilityGrant
		if err := tx.GetVersion(ctx, "platform.context_capability_grants", original.LookupID, 1, &old); err == nil {
			if !api.Equal(old, frozen) {
				return api.E("idempotency_conflict", "original_context_capability_changed")
			}
			return nil
		} else if !api.IsCode(err, "not_found") {
			return err
		}
		return tx.Create(ctx, "platform.context_capability_grants", original.LookupID, original.TaskRef.ObjectID, frozen)
	})
	if status == runtime.CommitUnknown {
		return nil, runtime.ErrCommitUnknown
	}
	return body, err
}

func (a *App) checkContextCapabilityTx(ctx context.Context, tx runtime.Tx, material task.ContextMaterial) error {
	if material.Kind != "capability_describe" {
		return nil
	}
	var original contextCapabilityGrant
	if err := tx.GetVersion(ctx, "platform.context_capability_grants", material.LookupRef.ObjectID, 1, &original); err != nil {
		return err
	}
	if !api.Equal(original.Scope, tx.Scope()) || !api.Equal(material.LookupRef, tx.Scope().Ref(original.Lookup.LookupID, 1)) || material.ContentRef.Hash != original.DescriptionHash || material.ContentRef.ByteLength != original.DescriptionSize || material.ContentRef.MediaType != "application/vnd.harness.capability-description+json" || material.QueryRef != nil && !api.Equal(*material.QueryRef, original.Lookup.Lookup.QueryRef) {
		return api.E("forbidden", "original_context_capability_mismatch")
	}
	if err := currentCredentialTx(ctx, tx, original.Subject); err != nil {
		return err
	}
	receipt, err := a.Governance.CheckGrantTx(ctx, tx, original.Subject, original.Check)
	if err != nil {
		return err
	}
	if receipt.Decision != "allowed" {
		return api.E("forbidden", "capability_current_grant_unavailable")
	}
	return nil
}
