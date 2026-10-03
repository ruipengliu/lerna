package development

import (
	"context"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 最低 WASI invoice 保留原源和授权引用作为账务身份，不包含代码、
// 参数、命名空间或模型正文，也不把原来源引用解释为新的 DataUse。
type wasiAccountingBasis struct {
	Proof             execution.UsageProof `json:"proof"`
	CommandRef        api.ObjectRef        `json:"command_ref"`
	TaskRef           api.ObjectRef        `json:"task_ref"`
	BudgetRef         api.ObjectRef        `json:"budget_ref"`
	IntentRef         api.ContentRef       `json:"intent_ref"`
	AdmissionHash     string               `json:"admission_hash"`
	OriginalSources   []api.ContentRef     `json:"original_sources"`
	EnvironmentRef    api.ObjectRef        `json:"environment_ref"`
	EnvironmentConfig api.ComponentRef     `json:"environment_config"`
	InstallLock       api.ComponentRef     `json:"install_lock"`
	UseRef            api.ObjectRef        `json:"use_ref"`
	UseDigest         string               `json:"use_digest"`
	GrantRefs         []api.ObjectRef      `json:"grant_refs"`
	NativeReceipts    []wasi.Receipt       `json:"native_receipts"`
}

func (a *App) publishWASIAccounting(ctx context.Context, scope runtime.Scope, auth runtime.Auth, p execution.Publication, body []byte) (api.ContentRef, bool, error) {
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
	if !api.Equal(admission.Descriptor.Capability.Ref, execution.WASIRunCellCapability().Ref) {
		return api.ContentRef{}, false, nil
	}
	ref := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: p.ContentID, Version: 1, Hash: api.Hash(body), MediaType: p.MediaType, ByteLength: uint64(len(body))}
	if scope != a.Scope || auth.SubjectID != scope.OwnerID || auth.TenantID != scope.TenantID || !auth.HasRole("usage_reporter") || p.MediaType != "application/json" || p.Location != "cloud" || len(p.DisclosedSources) != 0 || runtime.CheckRef(scope, proof.OperationRef) != nil || proof.OperationRef.OwnerID != scope.OwnerID || proof.UsageRevision != proof.OperationRef.Revision || p.ContentID != stableID("content", proof.OperationRef.ObjectID+":usage:"+strconv.FormatUint(proof.UsageRevision, 10)) {
		return ref, true, api.E("forbidden", "wasi_accounting_authority_required")
	}
	basis, err := a.originalWASIAccounting(ctx, scope, auth, admission, p, proof)
	if err != nil {
		return ref, true, err
	}
	var plan publicationPlan
	var policy memory.Policy
	legacy := false
	status, err := a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		if err := currentCredentialTx(ctx, tx, auth); err != nil {
			return err
		}
		_, err := tx.Get(ctx, "platform.publications", ref.ContentID, &plan)
		if err == nil {
			if !api.Equal(plan.Ref, ref) || plan.SubjectID != auth.SubjectID || plan.PolicyRef == nil {
				return api.E("idempotency_conflict", "original_wasi_invoice_publication_changed")
			}
			if plan.PolicyRef.Version != "wasi-accounting/1" {
				// 已 applied 的旧普通 proof 不改 policy/来源/期限；只沿原 put 返回。
				if !api.Equal(plan.Processed, uniqueSources(p.ProcessedSources)) || !api.Equal(plan.Disclosed, []api.ContentRef{}) {
					return api.E("idempotency_conflict", "original_wasi_invoice_sources_changed")
				}
				legacy = true
				return nil
			}
			var original wasiAccountingBasis
			if err = tx.GetVersion(ctx, "platform.wasi_accounting_basis", ref.ContentID, 1, &original); err != nil {
				return err
			}
			if !api.Equal(original, basis) {
				return api.E("idempotency_conflict", "original_wasi_invoice_basis_changed")
			}
			return tx.GetVersion(ctx, "platform.wasi_accounting_policies", plan.PolicyRef.ComponentID, 1, &policy)
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
		policy = memory.Policy{PolicyRef: api.ComponentRef{ComponentID: stableID("component", "wasi-accounting/"+ref.ContentID), Version: "wasi-accounting/1"}, Values: memory.PolicyValues{Subjects: []string{scope.OwnerID}, Purposes: []string{"content.write", "execution_usage_proof"}, Locations: []string{"cloud"}, RetainUntil: api.Time(retain), Continuous: true, IndependentDerived: false}, Revision: 1, State: "active"}
		policy.PolicyRef.Digest, err = api.Digest(policy.Values)
		if err != nil {
			return err
		}
		deadline := now.Add(30 * time.Minute)
		if retain.Before(deadline) {
			deadline = retain
		}
		plan = publicationPlan{Ref: ref, TransferID: api.NewID("transfer"), ReserveID: api.NewID("command"), PutID: api.NewID("command"), Processed: []api.ContentRef{}, Disclosed: []api.ContentRef{}, Retention: api.Time(retain), Deadline: api.Time(deadline), SubjectID: auth.SubjectID, PolicyRef: &policy.PolicyRef}
		if err = tx.Create(ctx, "platform.wasi_accounting_basis", ref.ContentID, proof.OperationRef.ObjectID, basis); err != nil {
			return err
		}
		if err = tx.Create(ctx, "platform.wasi_accounting_policies", policy.PolicyRef.ComponentID, proof.OperationRef.ObjectID, policy); err != nil {
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
	put, lookupErr := a.Store.LookupCommand(ctx, scope, plan.PutID)
	if lookupErr != nil && !api.IsCode(lookupErr, "not_found") {
		return ref, true, lookupErr
	}
	if lookupErr == nil && put.Receipt.Stage == "applied" && put.Receipt.Error == nil {
		var original memory.PutInput
		if put.PrincipalID != auth.SubjectID || put.Command.Method != "content.put" || put.Command.TargetID != ref.ContentID || api.Decode(put.Command.Payload, &original) != nil || !api.Equal(original.ContentRef, ref) || original.TransferID != plan.TransferID || !api.Equal(original.PolicyRef, *plan.PolicyRef) || !api.Equal(original.ProcessedSources, plan.Processed) || !api.Equal(original.DisclosedSources, plan.Disclosed) {
			return ref, true, api.E("idempotency_conflict", "original_wasi_invoice_put_changed")
		}
		return ref, true, nil
	}
	if lookupErr == nil && put.Receipt.Error != nil {
		return ref, true, put.Receipt.Error
	}
	if legacy {
		return ref, true, api.E("dependency_unavailable", "original_wasi_invoice_publication_pending")
	}
	if err = a.Memory.InstallPolicy(ctx, scope, auth, policy); err != nil {
		return ref, true, err
	}
	ref, err = a.Memory.Upload(ctx, scope, auth, memory.PublicationRequest{ContentRef: plan.Ref, TransferID: plan.TransferID, ReserveCommandID: plan.ReserveID, PutCommandID: plan.PutID, PolicyRef: policy.PolicyRef, ProcessedSources: plan.Processed, DisclosedSources: plan.Disclosed, RetentionUntil: plan.Retention, TransferDeadline: plan.Deadline}, body)
	return ref, true, err
}

func (a *App) originalWASIAccounting(ctx context.Context, scope runtime.Scope, auth runtime.Auth, admission actionAdmission, p execution.Publication, proof execution.UsageProof) (wasiAccountingBasis, error) {
	var encoded encodedIntent
	if _, err := a.Store.Read(ctx, scope, "platform.execution_intents", proof.OperationRef.ObjectID, 0, &encoded); err != nil {
		return wasiAccountingBasis{}, err
	}
	if admission.Scope != scope || admission.Prepared.OperationID != proof.OperationRef.ObjectID || encoded.Command == nil || encoded.AdmissionHash == "" || proof.IntentHash != encoded.Hash || encoded.Domain.ExecutorID != scope.OwnerID || !api.Equal(encoded.Domain.CapabilityRef, admission.Prepared.CapabilityRef) || !api.Equal(encoded.Domain.BindingRef, admission.Prepared.BindingRef) || !api.Equal(encoded.Domain.InstallLockRef, admission.Prepared.InstallLockRef) || !api.Equal(encoded.Domain.CostBound, admission.Prepared.CostBound) || admission.Descriptor.PreparedCell == nil {
		return wasiAccountingBasis{}, api.E("forbidden", "original_wasi_admission_changed")
	}
	command, err := a.Store.LookupCommand(ctx, scope, encoded.Command.CommandID)
	if err != nil {
		return wasiAccountingBasis{}, err
	}
	var invoke execution.InvokeInput
	if err = api.Decode(command.Command.Payload, &invoke); err != nil {
		return wasiAccountingBasis{}, err
	}
	if command.PrincipalID != auth.SubjectID || !api.Equal(command.Command, *encoded.Command) || command.Command.Method != "execution.invoke" || command.Command.TargetID != proof.OperationRef.ObjectID || command.Receipt.Error != nil || command.Receipt.Stage != "applied" || invoke.OperationID != proof.OperationRef.ObjectID || !api.Equal(invoke.TaskRef, encoded.Domain.TaskRef) || invoke.GoalRevision != encoded.Domain.GoalRevision || invoke.ControlRevision != encoded.Domain.ControlRevision || invoke.IntentHash != encoded.Hash || !api.Equal(invoke.IntentRef, encoded.Ref) || !api.Equal(invoke.CapabilityRef, encoded.Domain.CapabilityRef) || !api.Equal(invoke.BindingRef, encoded.Domain.BindingRef) || !api.Equal(invoke.UseRefs, admission.Prepared.UseIntentRefs) || len(invoke.UseRefs) != 1 {
		return wasiAccountingBasis{}, api.E("forbidden", "original_wasi_command_changed")
	}
	raw, err := a.queryAs(ctx, auth, "execution.get", invoke.OperationID, execution.OperationIDInput{OperationID: invoke.OperationID})
	if err != nil {
		return wasiAccountingBasis{}, err
	}
	var actual execution.OperationView
	if err = api.Decode(raw, &actual); err != nil {
		return wasiAccountingBasis{}, err
	}
	if actual.Operation.OperationID != invoke.OperationID || actual.Operation.OwnerID != scope.OwnerID || !api.Equal(actual.Operation.TaskRef, invoke.TaskRef) || proof.UsageRevision != actual.Operation.Revision || proof.SpendingClosed != actual.NewAttemptsClosed || proof.UsageFinal != actual.Operation.UsageFinal || !actual.Attempts.Exhausted || actual.Attempts.Partial || len(actual.Attempts.Gaps) != 0 || len(actual.Attempts.Items) > 1 || !api.Equal(proof.Attempts, actual.Attempts.Items) || !api.Equal(p.ProcessedSources, append([]api.ContentRef{encoded.Ref}, actual.Operation.EvidenceRefs...)) {
		return wasiAccountingBasis{}, api.E("revision_conflict", "original_wasi_accounting_changed")
	}
	if len(encoded.Domain.CostBound) != 1 || encoded.Domain.CostBound[0].Unit != "cpu_seconds" || api.ValidateAmounts(encoded.Domain.CostBound) != nil {
		return wasiAccountingBasis{}, api.E("forbidden", "original_wasi_cpu_bound_changed")
	}
	raw, err = a.queryAs(ctx, auth, "grant.use.get", invoke.UseRefs[0].ObjectID, governance.IDInput{ID: invoke.UseRefs[0].ObjectID})
	if err != nil {
		return wasiAccountingBasis{}, err
	}
	var use governance.UseReceipt
	if err = api.Decode(raw, &use); err != nil {
		return wasiAccountingBasis{}, err
	}
	if use.UseID != invoke.UseRefs[0].ObjectID || use.TargetKind != "operation" || !api.Equal(use.TargetRef, scope.Ref(invoke.OperationID, 1)) || use.SubjectRef.ObjectID != auth.SubjectID || use.Decision != "allowed" || use.IntentHash != encoded.AdmissionHash || !api.Equal(use.CostBound, encoded.Domain.CostBound) || use.Recipient != scope.OwnerID || use.Location != "cloud" || !api.Equal(use.GrantRefs, []api.ObjectRef{admission.Descriptor.GrantRef}) {
		return wasiAccountingBasis{}, api.E("forbidden", "original_wasi_use_changed")
	}
	cell := admission.Descriptor.PreparedCell.Arguments
	if runtime.CheckRef(scope, cell.EnvironmentRef) != nil || cell.EnvironmentRef.OwnerID != scope.OwnerID {
		return wasiAccountingBasis{}, api.E("forbidden", "original_wasi_environment_scope_changed")
	}
	raw, err = a.queryAs(ctx, auth, "environment.get", cell.EnvironmentRef.ObjectID, execution.EnvironmentIDInput{EnvironmentID: cell.EnvironmentRef.ObjectID})
	if err != nil {
		return wasiAccountingBasis{}, err
	}
	var environment execution.Environment
	if err = api.Decode(raw, &environment); err != nil {
		return wasiAccountingBasis{}, err
	}
	if environment.EnvironmentID != cell.EnvironmentRef.ObjectID || environment.Principal.SubjectID != auth.SubjectID || environment.Principal.TenantID != scope.TenantID || !api.Equal(environment.InstallLockRef, encoded.Domain.InstallLockRef) || environment.RuntimeKind != "restricted_wasi_preview1" {
		return wasiAccountingBasis{}, api.E("forbidden", "original_wasi_environment_changed")
	}
	basis := wasiAccountingBasis{Proof: proof, CommandRef: scope.Ref(command.Command.CommandID, 1), TaskRef: invoke.TaskRef, BudgetRef: invoke.ReservationRef, IntentRef: encoded.Ref, AdmissionHash: encoded.AdmissionHash, OriginalSources: p.ProcessedSources, EnvironmentRef: cell.EnvironmentRef, EnvironmentConfig: environment.ConfigRef, InstallLock: environment.InstallLockRef, UseRef: invoke.UseRefs[0], UseDigest: use.RequestDigest, GrantRefs: use.GrantRefs, NativeReceipts: []wasi.Receipt{}}
	sum := "0"
	var started, minimum, maximum uint64
	for _, attempt := range actual.Attempts.Items {
		if attempt.OperationID != invoke.OperationID {
			return basis, api.E("forbidden", "original_wasi_attempt_changed")
		}
		if attempt.StartedAt == "" {
			if len(attempt.Usage) != 0 && !api.Equal(attempt.Usage, []api.Amount{{Unit: "cpu_seconds", Value: "0"}}) {
				return basis, api.E("forbidden", "unstarted_wasi_usage_not_zero")
			}
			continue
		}
		started++
		if attempt.Effect != "not_started" {
			maximum++
			if attempt.Effect == "applied" || attempt.ResultRef != nil {
				minimum++
			}
		}
		receipt, err := wasi.ReadOriginalReceipt(ctx, filepath.Join(a.Config.DataRoot, "wasi"), scope, invoke.OperationID, attempt.AttemptID, environment.ConfigRef, environment.InstallLockRef)
		if err != nil {
			return basis, err
		}
		bytes := api.Raw(receipt)
		nativeRef := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: "content_" + strings.TrimPrefix(api.Hash(bytes), "sha256:")[:32], Version: 1, Hash: api.Hash(bytes), MediaType: "application/json", ByteLength: uint64(len(bytes))}
		bound := false
		for _, evidence := range attempt.EvidenceRefs {
			bound = bound || api.Equal(evidence, nativeRef)
		}
		if !bound && !attempt.UsageFinal {
			return basis, api.E("accounting_unknown", "original_wasi_native_meter_not_recorded")
		}
		if !bound || !receipt.ActuallyExited || !api.Equal(receipt.Usage, attempt.Usage) || receipt.UsageFinal != attempt.UsageFinal || receipt.ActuallyExited != attempt.ActuallyStopped || len(receipt.Usage) != 1 || receipt.Usage[0].Unit != "cpu_seconds" {
			return basis, api.E("forbidden", "original_wasi_native_meter_changed")
		}
		sum, err = api.AddDecimal(sum, receipt.Usage[0].Value)
		if err != nil {
			return basis, err
		}
		basis.NativeReceipts = append(basis.NativeReceipts, receipt)
	}
	if len(actual.Operation.Usage) != 0 && !api.Equal(actual.Operation.Usage, []api.Amount{{Unit: "cpu_seconds", Value: sum}}) || !api.Equal(proof.Cumulative, []api.Amount{{Unit: "cpu_seconds", Value: sum}}) || proof.SendStartedCount != started || proof.PhysicalCountMin != minimum || proof.PhysicalCountMax != maximum {
		return basis, api.E("forbidden", "original_wasi_invoice_amount_or_count_changed")
	}
	return basis, nil
}

func (a *App) checkWASIAccountingTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ComponentRef) error {
	if ref.Version != "wasi-accounting/1" {
		return nil
	}
	if auth.SubjectID != tx.Scope().OwnerID || !auth.HasRole("usage_reporter") {
		return api.E("forbidden", "wasi_accounting_authority_required")
	}
	var original memory.Policy
	if err := tx.GetVersion(ctx, "platform.wasi_accounting_policies", ref.ComponentID, 1, &original); err != nil {
		return err
	}
	if !api.Equal(original.PolicyRef, ref) {
		return api.E("forbidden", "original_wasi_accounting_policy_changed")
	}
	return nil
}
