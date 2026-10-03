package development

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

type publicationPlan struct {
	Ref        api.ContentRef    `json:"ref"`
	TransferID string            `json:"transfer_id"`
	ReserveID  string            `json:"reserve_id"`
	PutID      string            `json:"put_id"`
	Processed  []api.ContentRef  `json:"processed"`
	Disclosed  []api.ContentRef  `json:"disclosed"`
	Retention  string            `json:"retention"`
	Deadline   string            `json:"deadline"`
	SubjectID  string            `json:"subject_id"`
	PolicyRef  *api.ComponentRef `json:"policy_ref,omitempty"`
}

func uniqueSources(xs []api.ContentRef) []api.ContentRef {
	out := []api.ContentRef{}
	seen := map[string]bool{}
	for _, r := range xs {
		k := r.OwnerID + "/" + r.ContentID + "/" + r.Hash
		if !seen[k] {
			out = append(out, r)
			seen[k] = true
		}
	}
	return out
}
func (a *App) Publish(ctx context.Context, scope runtime.Scope, auth runtime.Auth, id, media string, b []byte, processed, disclosed []api.ContentRef) (api.ContentRef, error) {
	ref := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: id, Version: 1, Hash: api.Hash(b), MediaType: media, ByteLength: uint64(len(b))}
	processed = uniqueSources(processed)
	disclosed = uniqueSources(disclosed)
	ctx, e := a.prepareForeignSources(ctx, scope, auth, uniqueSources(append(append([]api.ContentRef{}, processed...), disclosed...)), "content.write", "cloud")
	if e != nil {
		return ref, e
	}
	var plan publicationPlan
	status, e := a.Store.Within(ctx, scope, []string{"platform", "content", "memory", "governance"}, func(tx runtime.Tx) error {
		_, e := tx.Get(ctx, "platform.publications", id, &plan)
		if e == nil {
			if !api.Equal(plan.Ref, ref) || !api.Equal(plan.Processed, processed) || !api.Equal(plan.Disclosed, disclosed) || plan.SubjectID != auth.SubjectID {
				return api.E("idempotency_conflict", "publication_intent_changed")
			}
			return nil
		}
		if !api.IsCode(e, "not_found") {
			return e
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		retain := now.Add(24 * time.Hour)
		policyUntil, e := api.ParseTime(a.ContentPolicy.Values.RetainUntil)
		if e != nil {
			return e
		}
		if policyUntil.Before(retain) {
			retain = policyUntil
		}
		for _, source := range uniqueSources(append(append([]api.ContentRef{}, processed...), disclosed...)) {
			value, e := a.Memory.CheckContentTx(ctx, tx, auth, source, "content.write", "cloud", true)
			if e != nil {
				return e
			}
			until, e := api.ParseTime(value.RetentionUntil)
			if e != nil {
				return e
			}
			if until.Before(retain) {
				retain = until
			}
		}
		policy, e := a.publicationPolicyTx(ctx, tx, auth, uniqueSources(append(append([]api.ContentRef{}, processed...), disclosed...)))
		if e != nil {
			return e
		}
		deadline := now.Add(30 * time.Minute)
		if retain.Before(deadline) {
			deadline = retain
		}
		plan = publicationPlan{Ref: ref, TransferID: api.NewID("transfer"), ReserveID: api.NewID("command"), PutID: api.NewID("command"), Processed: processed, Disclosed: disclosed, Retention: api.Time(retain), Deadline: api.Time(deadline), SubjectID: auth.SubjectID, PolicyRef: &policy.PolicyRef}
		return tx.Create(ctx, "platform.publications", id, auth.SubjectID, plan)
	})
	if status == runtime.CommitUnknown {
		return ref, runtime.ErrCommitUnknown
	}
	if e != nil {
		return ref, e
	}
	// 如果 publication 已提交，恢复沿原put查询，过期不重新造上传身份。
	original, e := a.Store.LookupCommand(ctx, scope, plan.PutID)
	if e == nil && original.Receipt.Stage == "applied" {
		if original.Receipt.Error != nil {
			return ref, original.Receipt.Error
		}
		return ref, nil
	}
	if e != nil && !api.IsCode(e, "not_found") {
		return ref, e
	}
	policy := a.ContentPolicy.PolicyRef
	if plan.PolicyRef != nil {
		policy = *plan.PolicyRef
	} else {
		// 旧内联plan只可从已保存的准确reserve恢复policy，不把新装配许可替换进去。
		reserve, err := a.Store.LookupCommand(ctx, scope, plan.ReserveID)
		if api.IsCode(err, "not_found") {
			return ref, api.E("unsupported", "original_publication_policy_unavailable")
		}
		if err != nil {
			return ref, err
		}
		var in memory.ReserveInput
		if err = api.Decode(reserve.Command.Payload, &in); err != nil {
			return ref, err
		}
		if reserve.PrincipalID != plan.SubjectID || reserve.Command.Method != "content.upload_reserve" || reserve.Command.TargetID != plan.Ref.ContentID || reserve.Command.ExpiresAt != plan.Deadline || in.TransferID != plan.TransferID || !api.Equal(in.ContentRef, plan.Ref) || !api.Equal(in.ProcessedSources, plan.Processed) || in.RetentionUntil != plan.Retention || in.TransferDeadline != plan.Deadline {
			return ref, api.E("idempotency_conflict", "original_publication_reserve_changed")
		}
		policy = in.PolicyRef
	}
	return a.Memory.Upload(ctx, scope, auth, memory.PublicationRequest{ContentRef: plan.Ref, TransferID: plan.TransferID, ReserveCommandID: plan.ReserveID, PutCommandID: plan.PutID, PolicyRef: policy, ProcessedSources: plan.Processed, DisclosedSources: plan.Disclosed, RetentionUntil: plan.Retention, TransferDeadline: plan.Deadline}, b)
}

type brainContent struct{ a *App }

func (c brainContent) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p string) ([]byte, error) {
	return c.a.ReadContent(ctx, s, a, r, p)
}
func (c brainContent) Publish(ctx context.Context, s runtime.Scope, a runtime.Auth, p brain.Publication, b []byte) (api.ContentRef, error) {
	return c.a.Publish(ctx, s, a, p.ContentID, p.MediaType, b, p.ProcessedSources, p.DisclosedSources)
}
func (c brainContent) PublishUsageProof(ctx context.Context, s runtime.Scope, a runtime.Auth, p brain.Publication, facts brain.AccountingFacts) (api.ContentRef, error) {
	return c.a.publishModelAccounting(ctx, s, a, p, facts)
}

type executionContent struct{ a *App }

func (c executionContent) ReadBytes(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p, l string) ([]byte, error) {
	return c.a.ReadContentBytes(ctx, s, a, r, p, l)
}
func (c executionContent) Publish(ctx context.Context, s runtime.Scope, a runtime.Auth, p execution.Publication, b []byte) (api.ContentRef, error) {
	if p.Purpose == "execution_usage_proof" {
		if ref, handled, err := c.a.publishInformationAccounting(ctx, s, a, p, b); handled || err != nil {
			return ref, err
		}
	}
	if p.Purpose == "execution_result" && len(c.a.information) > 0 {
		var err error
		p.ProcessedSources, err = c.a.informationOutputSources(ctx, s, a, p.ProcessedSources, b)
		if err != nil {
			return api.ContentRef{}, err
		}
	}
	return c.a.Publish(ctx, s, a, p.ContentID, p.MediaType, b, p.ProcessedSources, p.DisclosedSources)
}

type governanceContent struct{ a *App }

func (c governanceContent) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p string) ([]byte, error) {
	return c.a.ReadContent(ctx, s, a, r, p)
}

type interactionContent struct{ a *App }

func (c interactionContent) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p string) ([]byte, error) {
	return c.a.ReadContent(ctx, s, a, r, p)
}
func (c interactionContent) CheckTx(ctx context.Context, tx runtime.Tx, a runtime.Auth, r api.ContentRef, p string) error {
	if e := currentCredentialTx(ctx, tx, a); e != nil {
		return e
	}
	_, e := c.a.Memory.CheckContentTx(ctx, tx, a, r, p, "cloud", true)
	return e
}

type taskContent struct{ a *App }

func (c taskContent) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef) ([]byte, error) {
	return c.a.ReadContent(ctx, s, a, r, "task.goal")
}
func (c taskContent) Publish(ctx context.Context, s runtime.Scope, id, media string, b []byte) (api.ContentRef, error) {
	sources := []api.ContentRef{}
	var result api.Result
	if api.Decode(b, &result) == nil && api.ValidID(result.TaskID) {
		sources = append(sources, result.ArtifactRefs...)
	}
	var doc api.GoalDocument
	if api.Decode(b, &doc) == nil && doc.FormatVersion == 1 {
		sources = append(sources, doc.InitialGoalRef)
		sources = append(sources, doc.AmendmentRefs...)
	}
	return c.a.Publish(ctx, s, c.a.ServiceAuth, id, media, b, sources, []api.ContentRef{})
}
