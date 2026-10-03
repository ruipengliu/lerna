package development

import (
	"context"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 信息源最低账务依据只来自原执行账本；它不包含正文、标题、命中或引用 quote。
// 原数据许可撤回不改写已知费用，也不使费用核对获得普通正文读取资格。
func (a *App) publishInformationAccounting(ctx context.Context, scope runtime.Scope, auth runtime.Auth, p execution.Publication, body []byte) (api.ContentRef, bool, error) {
	var proof execution.UsageProof
	if err := api.Decode(body, &proof); err != nil {
		return api.ContentRef{}, false, err
	}
	var admission actionAdmission
	_, err := a.Store.Read(ctx, scope, "platform.action_admissions", proof.OperationRef.ObjectID, 1, &admission)
	if api.IsCode(err, "not_found") {
		return api.ContentRef{}, false, nil
	}
	if err != nil {
		return api.ContentRef{}, false, err
	}
	if admission.Descriptor.Kind != providers.InformationSearch && admission.Descriptor.Kind != providers.InformationBody {
		return api.ContentRef{}, false, nil
	}
	if scope != a.Scope || auth.SubjectID != scope.OwnerID || auth.TenantID != scope.TenantID || !auth.HasRole("usage_reporter") || runtime.CheckRef(scope, proof.OperationRef) != nil {
		return api.ContentRef{}, true, api.E("forbidden", "information_accounting_authority_required")
	}
	var encoded encodedIntent
	if _, err = a.Store.Read(ctx, scope, "platform.execution_intents", proof.OperationRef.ObjectID, 0, &encoded); err != nil {
		return api.ContentRef{}, true, err
	}
	raw, err := a.queryAs(ctx, auth, "execution.get", proof.OperationRef.ObjectID, execution.OperationIDInput{OperationID: proof.OperationRef.ObjectID})
	if err != nil {
		return api.ContentRef{}, true, err
	}
	var actual execution.OperationView
	if err = api.Decode(raw, &actual); err != nil {
		return api.ContentRef{}, true, err
	}
	if proof.IntentHash != encoded.Hash || encoded.AdmissionHash == "" || !api.Equal(encoded.Domain.CapabilityRef, admission.Prepared.CapabilityRef) || !api.Equal(encoded.Domain.BindingRef, admission.Prepared.BindingRef) || proof.UsageRevision != actual.Operation.Revision || proof.OperationRef.Revision != actual.Operation.Revision || proof.SpendingClosed != actual.NewAttemptsClosed || proof.UsageFinal != actual.Operation.UsageFinal || !actual.Attempts.Exhausted || actual.Attempts.Partial || len(actual.Attempts.Gaps) != 0 || !api.Equal(proof.Attempts, actual.Attempts.Items) {
		return api.ContentRef{}, true, api.E("revision_conflict", "information_original_accounting_changed")
	}
	units := map[string]string{}
	for _, bound := range encoded.Domain.CostBound {
		if _, exists := units[bound.Unit]; exists {
			return api.ContentRef{}, true, api.E("invalid_state", "information_frozen_cost_units_invalid")
		}
		units[bound.Unit] = "0"
	}
	for _, amount := range actual.Operation.Usage {
		if _, exists := units[amount.Unit]; !exists {
			return api.ContentRef{}, true, api.E("accounting_unknown", "information_usage_unit_not_reserved")
		}
		units[amount.Unit] = amount.Value
	}
	keys := make([]string, 0, len(units))
	for unit := range units {
		keys = append(keys, unit)
	}
	sort.Strings(keys)
	cumulative := []api.Amount{}
	for _, unit := range keys {
		cumulative = append(cumulative, api.Amount{Unit: unit, Value: units[unit]})
	}
	var started, minimum, maximum uint64
	for _, attempt := range actual.Attempts.Items {
		if attempt.StartedAt != "" {
			started++
			if attempt.Effect != "not_started" {
				maximum++
				if attempt.Effect == "applied" || attempt.ResultRef != nil {
					minimum++
				}
			}
		}
	}
	if len(cumulative) == 0 || !api.Equal(proof.Cumulative, cumulative) || proof.SendStartedCount != started || proof.PhysicalCountMin != minimum || proof.PhysicalCountMax != maximum {
		return api.ContentRef{}, true, api.E("forbidden", "information_accounting_basis_mismatch")
	}
	ref := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: p.ContentID, Version: 1, Hash: api.Hash(body), MediaType: p.MediaType, ByteLength: uint64(len(body))}
	var plan publicationPlan
	var policy memory.Policy
	status, err := a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		if err := currentCredentialTx(ctx, tx, auth); err != nil {
			return err
		}
		_, err := tx.Get(ctx, "platform.publications", ref.ContentID, &plan)
		if err == nil {
			if !api.Equal(plan.Ref, ref) || plan.SubjectID != auth.SubjectID || plan.PolicyRef == nil || plan.PolicyRef.Version != "information-accounting/1" {
				return api.E("idempotency_conflict", "information_original_accounting_publication_changed")
			}
			return tx.GetVersion(ctx, "platform.information_accounting_policies", plan.PolicyRef.ComponentID, 1, &policy)
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		retain := now.Add(24 * time.Hour)
		end, err := api.ParseTime(a.Config.PolicyExpiresAt)
		if err != nil {
			return err
		}
		if end.Before(retain) {
			retain = end
		}
		policy = memory.Policy{PolicyRef: api.ComponentRef{ComponentID: stableID("component", "information-accounting/"+ref.ContentID), Version: "information-accounting/1"}, Values: memory.PolicyValues{Subjects: []string{scope.OwnerID}, Purposes: []string{"content.write", "execution_usage_proof"}, Locations: []string{"cloud"}, RetainUntil: api.Time(retain), Continuous: true, IndependentDerived: false}, Revision: 1, State: "active"}
		policy.PolicyRef.Digest, err = api.Digest(policy.Values)
		if err != nil {
			return err
		}
		deadline := now.Add(30 * time.Minute)
		if retain.Before(deadline) {
			deadline = retain
		}
		plan = publicationPlan{Ref: ref, TransferID: api.NewID("transfer"), ReserveID: api.NewID("command"), PutID: api.NewID("command"), Processed: []api.ContentRef{}, Disclosed: []api.ContentRef{}, Retention: api.Time(retain), Deadline: api.Time(deadline), SubjectID: auth.SubjectID, PolicyRef: &policy.PolicyRef}
		if err = tx.Create(ctx, "platform.information_accounting_policies", policy.PolicyRef.ComponentID, proof.OperationRef.ObjectID, policy); err != nil {
			return err
		}
		return tx.Create(ctx, "platform.publications", ref.ContentID, proof.OperationRef.ObjectID, plan)
	})
	if status == runtime.CommitUnknown {
		return ref, true, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ref, true, err
	}
	if err = a.Memory.InstallPolicy(ctx, scope, auth, policy); err != nil {
		return ref, true, err
	}
	ref, err = a.Memory.Upload(ctx, scope, auth, memory.PublicationRequest{ContentRef: plan.Ref, TransferID: plan.TransferID, ReserveCommandID: plan.ReserveID, PutCommandID: plan.PutID, PolicyRef: policy.PolicyRef, ProcessedSources: plan.Processed, DisclosedSources: plan.Disclosed, RetentionUntil: plan.Retention, TransferDeadline: plan.Deadline}, body)
	return ref, true, err
}

func (a *App) checkInformationAccountingTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ComponentRef) error {
	if ref.Version != "information-accounting/1" {
		return nil
	}
	if auth.SubjectID != tx.Scope().OwnerID || !auth.HasRole("usage_reporter") {
		return api.E("forbidden", "information_accounting_authority_required")
	}
	var original memory.Policy
	if err := tx.GetVersion(ctx, "platform.information_accounting_policies", ref.ComponentID, 1, &original); err != nil {
		return err
	}
	if !api.Equal(original.PolicyRef, ref) {
		return api.E("forbidden", "information_original_accounting_policy_changed")
	}
	return nil
}
