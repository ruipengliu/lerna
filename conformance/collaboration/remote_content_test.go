package collaboration_test

import (
	"context"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 有限测试装配使用真实Content端口，只有受信GoalDocument投影可出版。
// 授权前提是pairedAgents显式安装的主体、用途和保存政策。
type agentContent struct {
	f      *agentFixture
	retain string
}
type agentGoalPublication struct {
	Request memory.PublicationRequest
	Body    []byte
}

func fixtureID(kind string, parts ...string) string {
	return kind + "_" + strings.TrimPrefix(api.Hash([]byte(strings.Join(parts, "/"))), "sha256:")[:32]
}
func (b agentContent) Read(ctx context.Context, scope runtime.Scope, a runtime.Auth, ref api.ContentRef) ([]byte, error) {
	if scope != b.f.scope {
		return nil, api.E("forbidden", "fixture_content_scope")
	}
	var err error
	ctx, err = b.prepare(ctx)
	if err != nil {
		return nil, err
	}
	body, err := b.f.memory.ReadBytes(ctx, scope, a, ref, "task.goal", "cloud")
	if err == nil && b.f.afterRead != nil {
		b.f.afterRead(ref)
	}
	return body, err
}
func (b agentContent) Publish(ctx context.Context, scope runtime.Scope, id, media string, body []byte) (api.ContentRef, error) {
	if scope != b.f.scope || media != "application/json" {
		return api.ContentRef{}, api.E("forbidden", "fixture_goal_publication_scope")
	}
	var err error
	ctx, err = b.prepare(ctx)
	if err != nil {
		return api.ContentRef{}, err
	}
	var doc api.GoalDocument
	if err := api.Decode(body, &doc); err != nil {
		return api.ContentRef{}, err
	}
	if api.ValidateRecord("GoalDocument", doc) != nil {
		return api.ContentRef{}, api.E("invalid_request", "fixture_goal_document_invalid")
	}
	var plan agentGoalPublication
	status, err := b.f.store.Within(ctx, scope, []string{"collaboration"}, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, "collaboration.fixture_publications", id, &plan)
		if err == nil {
			if string(plan.Body) != string(body) {
				return api.E("idempotency_conflict", "original_goal_bytes_changed")
			}
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		ref := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: fixtureID("content", id), Version: 1, MediaType: media, Hash: api.Hash(body), ByteLength: uint64(len(body))}
		plan = agentGoalPublication{Body: body, Request: memory.PublicationRequest{ContentRef: ref, TransferID: fixtureID("transfer", id), ReserveCommandID: fixtureID("command", id, "reserve"), PutCommandID: fixtureID("command", id, "put"), PolicyRef: b.f.goalPolicy.PolicyRef, ProcessedSources: append([]api.ContentRef{doc.InitialGoalRef}, doc.AmendmentRefs...), DisclosedSources: []api.ContentRef{}, RetentionUntil: b.retain, TransferDeadline: api.Time(now.Add(time.Minute))}}
		return tx.Create(ctx, "collaboration.fixture_publications", id, b.f.auth.SubjectID, plan)
	})
	if status == runtime.CommitUnknown {
		return api.ContentRef{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return api.ContentRef{}, err
	}
	uses := []memory.ForeignUse{}
	for _, ref := range plan.Request.ProcessedSources {
		if ref.OwnerID == scope.OwnerID {
			continue
		}
		for _, purpose := range []string{"content.write", "task.goal"} {
			copyID := fixtureID("copy", id, ref.OwnerID, ref.ContentID, purpose)
			foreign := memory.ForeignReference{ContentRef: ref, CopyID: copyID, RegisterCommandID: fixtureID("command", copyID, "register"), ReleaseCommandID: fixtureID("command", copyID, "release"), ReferenceIntentRef: scope.Ref(fixtureID("intent", copyID), 1), HolderRef: b.f.auth.Ref(scope.OwnerID), Purpose: purpose, Location: "cloud", RetainUntil: plan.Request.RetentionUntil}
			use, err := b.f.memory.PrepareForeignUse(ctx, scope, b.f.auth, foreign)
			if err != nil {
				return api.ContentRef{}, err
			}
			uses = append(uses, use)
		}
	}
	prepared, err := memory.WithForeignUses(ctx, uses)
	if err != nil {
		return api.ContentRef{}, err
	}
	ref, err := b.f.memory.Upload(prepared, scope, b.f.auth, plan.Request, plan.Body)
	if err == nil && b.f.afterPublish != nil {
		b.f.afterPublish(ref)
	}
	return ref, err
}
