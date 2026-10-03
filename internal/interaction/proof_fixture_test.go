package interaction_test

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 此桥接与生产边界相同：Tx 内实际 ES256 seal 与出版意图，提交后上传准确字节。
type signedProofBridge struct {
	keys   *platform.Keyring
	m      *memory.Service
	auth   runtime.Auth
	policy memory.Policy
}
type sealedInteractionProof struct {
	Ref     api.ContentRef       `json:"ref"`
	Compact string               `json:"compact"`
	Claims  platform.ProofClaims `json:"claims"`
}

type taskPublicationBridge struct {
	m      *memory.Service
	auth   runtime.Auth
	policy memory.Policy
}

func (b taskPublicationBridge) Read(ctx context.Context, scope runtime.Scope, a runtime.Auth, ref api.ContentRef) ([]byte, error) {
	return b.m.Read(ctx, scope, a, ref, "task.goal")
}
func (b taskPublicationBridge) Publish(ctx context.Context, scope runtime.Scope, id, media string, body []byte) (api.ContentRef, error) {
	derived := func(prefix, suffix string) string { return prefix + "_" + api.Hash([]byte(id + suffix))[7:39] }
	ref := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: derived("content", "/bytes"), Version: 1, Hash: api.Hash(body), ByteLength: uint64(len(body)), MediaType: media}
	return b.m.Upload(ctx, scope, b.auth, memory.PublicationRequest{ContentRef: ref, TransferID: id, ReserveCommandID: derived("command", "/reserve"), PutCommandID: derived("command", "/put"), PolicyRef: b.policy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(20 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(10 * time.Minute))}, body)
}

func (p *signedProofBridge) seal(ctx context.Context, tx runtime.Tx, claims platform.ProofClaims) (api.ContentRef, error) {
	compact, e := p.keys.Sign("development-es256", claims)
	if e != nil {
		return api.ContentRef{}, e
	}
	ref := api.ContentRef{TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(compact)), MediaType: "application/jose", ByteLength: uint64(len(compact))}
	e = tx.Create(ctx, "interaction.test_proofs", ref.ContentID, claims.ObjectRef.ObjectID, sealedInteractionProof{ref, compact, claims})
	return ref, e
}
func (p *signedProofBridge) SealClosureTx(ctx context.Context, tx runtime.Tx, v task.ClosureView) (api.ContentRef, error) {
	digest, e := api.Digest(v)
	if e != nil {
		return api.ContentRef{}, e
	}
	issued, e := api.ParseTime(v.IssuedAt)
	if e != nil {
		return api.ContentRef{}, e
	}
	return p.seal(ctx, tx, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: tx.Scope().OwnerID, Purpose: "task_closure", ObjectRef: v.TaskRef, Digest: digest, IssuedAt: v.IssuedAt, StartBefore: api.Time(issued.Add(time.Hour))})
}
func (p *signedProofBridge) SealControl(ctx context.Context, tx runtime.Tx, v api.ControlSnapshot) (api.ContentRef, error) {
	digest, e := api.Digest(v)
	if e != nil {
		return api.ContentRef{}, e
	}
	return p.seal(ctx, tx, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: tx.Scope().OwnerID, Purpose: "control", ObjectRef: tx.Scope().Ref(v.TaskID, v.GoalRevision), Digest: digest, ControlRevision: v.ControlRevision, WindowID: v.WindowID, IssuedAt: v.IssuedAt, StartBefore: v.StartBefore})
}
func (p *signedProofBridge) publish(ctx context.Context, scope runtime.Scope, store runtime.Store, ref api.ContentRef) error {
	var sealed sealedInteractionProof
	if _, e := store.Read(ctx, scope, "interaction.test_proofs", ref.ContentID, 1, &sealed); e != nil {
		return e
	}
	if _, e := p.keys.Verify(sealed.Compact, sealed.Claims, time.Now()); e != nil {
		return e
	}
	id := func(prefix, suffix string) string {
		return prefix + "_" + api.Hash([]byte(ref.ContentID + suffix))[7:39]
	}
	_, e := p.m.Upload(ctx, scope, p.auth, memory.PublicationRequest{ContentRef: ref, TransferID: id("upload", "/proof"), ReserveCommandID: id("command", "/reserve"), PutCommandID: id("command", "/put"), PolicyRef: p.policy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(20 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(10 * time.Minute))}, []byte(sealed.Compact))
	return e
}
