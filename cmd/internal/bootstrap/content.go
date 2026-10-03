package bootstrap

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
	Ref        api.ContentRef   `json:"ref"`
	TransferID string           `json:"transfer_id"`
	ReserveID  string           `json:"reserve_id"`
	PutID      string           `json:"put_id"`
	Processed  []api.ContentRef `json:"processed"`
	Disclosed  []api.ContentRef `json:"disclosed"`
	Retention  string           `json:"retention"`
	Deadline   string           `json:"deadline"`
	SubjectID  string           `json:"subject_id"`
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
	var plan publicationPlan
	status, e := a.Store.Within(ctx, scope, []string{"platform", "content", "memory"}, func(tx runtime.Tx) error {
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
		plan = publicationPlan{Ref: ref, TransferID: api.NewID("transfer"), ReserveID: api.NewID("command"), PutID: api.NewID("command"), Processed: processed, Disclosed: disclosed, Retention: api.Time(retain), Deadline: api.Time(now.Add(30 * time.Minute)), SubjectID: auth.SubjectID}
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
	return a.Memory.Upload(ctx, scope, auth, memory.PublicationRequest{ContentRef: plan.Ref, TransferID: plan.TransferID, ReserveCommandID: plan.ReserveID, PutCommandID: plan.PutID, PolicyRef: a.ContentPolicy.PolicyRef, ProcessedSources: plan.Processed, DisclosedSources: plan.Disclosed, RetentionUntil: plan.Retention, TransferDeadline: plan.Deadline}, b)
}

type brainContent struct{ a *App }

func (c brainContent) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p string) ([]byte, error) {
	return c.a.Memory.Read(ctx, s, a, r, p)
}
func (c brainContent) Publish(ctx context.Context, s runtime.Scope, a runtime.Auth, p brain.Publication, b []byte) (api.ContentRef, error) {
	return c.a.Publish(ctx, s, a, p.ContentID, p.MediaType, b, p.ProcessedSources, p.DisclosedSources)
}

type executionContent struct{ a *App }

func (c executionContent) ReadBytes(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p, l string) ([]byte, error) {
	return c.a.Memory.ReadBytes(ctx, s, a, r, p, l)
}
func (c executionContent) Publish(ctx context.Context, s runtime.Scope, a runtime.Auth, p execution.Publication, b []byte) (api.ContentRef, error) {
	return c.a.Publish(ctx, s, a, p.ContentID, p.MediaType, b, p.ProcessedSources, p.DisclosedSources)
}

type governanceContent struct{ a *App }

func (c governanceContent) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p string) ([]byte, error) {
	return c.a.Memory.Read(ctx, s, a, r, p)
}

type interactionContent struct{ a *App }

func (c interactionContent) Read(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p string) ([]byte, error) {
	return c.a.Memory.Read(ctx, s, a, r, p)
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
	return c.a.Memory.Read(ctx, s, a, r, "task.goal")
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
