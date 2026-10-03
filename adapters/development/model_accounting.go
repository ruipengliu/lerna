package development

import (
	"context"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 最低模型invoice来自原Brain主账，不读取Snapshot/编码/回复Content正文。
// 原Use/Task/命令只作准确身份绑定，不声明普通来源材料或新的数据许可。
func (a *App) publishModelAccounting(ctx context.Context, scope runtime.Scope, auth runtime.Auth, p brain.Publication, facts brain.AccountingFacts) (api.ContentRef, error) {
	if scope != a.Scope || auth.TenantID != scope.TenantID || auth.SubjectID != scope.OwnerID || !auth.HasRole("usage_reporter") || p.MediaType != "application/vnd.harness.usage-proof+json" || len(p.DisclosedSources) != 0 {
		return api.ContentRef{}, api.E("forbidden", "model_accounting_authority_required")
	}
	actual, err := a.Brain.AccountingFacts(ctx, a.Store, scope, auth, facts.Proof.Snapshot.SourceRef)
	if err != nil {
		return api.ContentRef{}, err
	}
	if !api.Equal(actual, facts) || p.ContentID != "content_"+api.Hash([]byte(facts.Proof.Snapshot.SourceRef.ObjectID + "/usage/" + strconv.FormatUint(facts.Proof.Snapshot.UsageRevision, 10)))[7:39] {
		return api.ContentRef{}, api.E("forbidden", "model_original_accounting_changed")
	}
	command, err := a.Store.LookupCommand(ctx, scope, facts.OriginalCommand.ObjectID)
	if err != nil {
		return api.ContentRef{}, err
	}
	var original brain.DecideInput
	if err = api.Decode(command.Command.Payload, &original); err != nil {
		return api.ContentRef{}, err
	}
	if command.PrincipalID != auth.SubjectID || command.Command.Method != "brain.decide" || command.Command.TargetID != facts.Proof.Snapshot.SourceRef.ObjectID || !api.Equal(original.TaskRef, facts.TaskRef) || !api.Equal(original.UseRefs, facts.UseRefs) || !api.Equal(original.ModelProfileRef, facts.ModelProfileRef) || !api.Equal(p.ProcessedSources, []api.ContentRef{original.SnapshotRef}) {
		return api.ContentRef{}, api.E("forbidden", "model_original_accounting_command_changed")
	}
	body := api.Raw(facts.Proof) // 保留既有原proof准确JSON字节和ContentID。
	ref := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: p.ContentID, Version: 1, Hash: api.Hash(body), MediaType: p.MediaType, ByteLength: uint64(len(body))}
	var plan publicationPlan
	var policy memory.Policy
	legacyApplied := false
	status, err := a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		if err := currentCredentialTx(ctx, tx, auth); err != nil {
			return err
		}
		_, err := tx.Get(ctx, "platform.publications", ref.ContentID, &plan)
		if err == nil {
			if !api.Equal(plan.Ref, ref) || plan.SubjectID != auth.SubjectID || plan.PolicyRef == nil {
				return api.E("idempotency_conflict", "model_original_accounting_publication_changed")
			}
			if plan.PolicyRef.Version != "model-accounting/1" {
				// 已实际出版的旧proof只返回原元数据依据；不会重开其普通读取许可。
				// 未结旧上传不能改policy/来源或伪造出版成功。
				legacyApplied = true
				return nil
			}
			return tx.GetVersion(ctx, "platform.model_accounting_policies", plan.PolicyRef.ComponentID, 1, &policy)
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
		policy = memory.Policy{PolicyRef: api.ComponentRef{ComponentID: stableID("component", "model-accounting/"+ref.ContentID), Version: "model-accounting/1"}, Values: memory.PolicyValues{Subjects: []string{scope.OwnerID}, Purposes: []string{"content.write", "brain_usage_proof"}, Locations: []string{"cloud"}, RetainUntil: api.Time(retain), Continuous: true, IndependentDerived: false}, Revision: 1, State: "active"}
		policy.PolicyRef.Digest, err = api.Digest(policy.Values)
		if err != nil {
			return err
		}
		deadline := now.Add(30 * time.Minute)
		if retain.Before(deadline) {
			deadline = retain
		}
		plan = publicationPlan{Ref: ref, TransferID: api.NewID("transfer"), ReserveID: api.NewID("command"), PutID: api.NewID("command"), Processed: []api.ContentRef{}, Disclosed: []api.ContentRef{}, Retention: api.Time(retain), Deadline: api.Time(deadline), SubjectID: auth.SubjectID, PolicyRef: &policy.PolicyRef}
		if err = tx.Create(ctx, "platform.model_accounting_policies", policy.PolicyRef.ComponentID, facts.Proof.Snapshot.SourceRef.ObjectID, policy); err != nil {
			return err
		}
		return tx.Create(ctx, "platform.publications", ref.ContentID, facts.Proof.Snapshot.SourceRef.ObjectID, plan)
	})
	if status == runtime.CommitUnknown {
		return ref, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ref, err
	}
	// 每次恢复先查询原put；已applied不再进入上传检查或刷新原票据期限。
	put, lookupErr := a.Store.LookupCommand(ctx, scope, plan.PutID)
	if lookupErr != nil && !api.IsCode(lookupErr, "not_found") {
		return ref, lookupErr
	}
	if lookupErr == nil && put.Receipt.Stage == "applied" && put.Receipt.Error == nil {
		if put.PrincipalID != auth.SubjectID || put.Command.Method != "content.put" || put.Command.TargetID != ref.ContentID {
			return ref, api.E("idempotency_conflict", "model_original_accounting_put_changed")
		}
		var originalPut memory.PutInput
		if err = api.Decode(put.Command.Payload, &originalPut); err != nil {
			return ref, err
		}
		if !api.Equal(originalPut.ContentRef, ref) || originalPut.TransferID != plan.TransferID || !api.Equal(originalPut.PolicyRef, *plan.PolicyRef) || !api.Equal(originalPut.ProcessedSources, plan.Processed) || !api.Equal(originalPut.DisclosedSources, plan.Disclosed) {
			return ref, api.E("idempotency_conflict", "model_original_accounting_put_changed")
		}
		return ref, nil
	}
	if lookupErr == nil && put.Receipt.Error != nil {
		return ref, put.Receipt.Error
	}
	if legacyApplied {
		return ref, api.E("dependency_unavailable", "original_model_invoice_publication_pending")
	}
	if err = a.Memory.InstallPolicy(ctx, scope, auth, policy); err != nil {
		return ref, err
	}
	return a.Memory.Upload(ctx, scope, auth, memory.PublicationRequest{ContentRef: plan.Ref, TransferID: plan.TransferID, ReserveCommandID: plan.ReserveID, PutCommandID: plan.PutID, PolicyRef: policy.PolicyRef, ProcessedSources: plan.Processed, DisclosedSources: plan.Disclosed, RetentionUntil: plan.Retention, TransferDeadline: plan.Deadline}, body)
}

func (a *App) checkModelAccountingTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ComponentRef) error {
	if ref.Version != "model-accounting/1" {
		return nil
	}
	if auth.SubjectID != tx.Scope().OwnerID || !auth.HasRole("usage_reporter") {
		return api.E("forbidden", "model_accounting_authority_required")
	}
	var original memory.Policy
	if err := tx.GetVersion(ctx, "platform.model_accounting_policies", ref.ComponentID, 1, &original); err != nil {
		return err
	}
	if !api.Equal(original.PolicyRef, ref) {
		return api.E("forbidden", "model_original_accounting_policy_changed")
	}
	return nil
}
