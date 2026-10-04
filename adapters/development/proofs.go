package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

const proofJob = "platform.publish_proof"

func raiseProofJob(ctx context.Context, tx runtime.Tx, id string, at time.Time) error {
	if plan, ok := tx.(task.JobIntentPlanner); ok {
		return plan.AddJobIntent(ctx, proofJob, "proof/"+id, tx.Scope().Ref(id, 1), at)
	}
	_, err := tx.Raise(ctx, proofJob, "proof/"+id, tx.Scope().Ref(id, 1), at)
	return err
}

type sealedProof struct {
	Ref                api.ContentRef           `json:"ref"`
	Compact            string                   `json:"compact"`
	Control            *api.ControlSnapshot     `json:"control,omitempty"`
	Closure            *task.ClosureView        `json:"closure,omitempty"`
	Allocation         *api.AllocationClosure   `json:"allocation,omitempty"`
	Published          bool                     `json:"published"`
	Revision           uint64                   `json:"revision"`
	PublicationFailure *proofPublicationFailure `json:"publication_failure,omitempty"`
}
type controlProof struct{ a *App }

func (p controlProof) SealControl(ctx context.Context, tx runtime.Tx, c api.ControlSnapshot) (api.ContentRef, error) {
	c.ProofRef = api.ContentRef{}
	digest, e := api.Digest(c)
	if e != nil {
		return api.ContentRef{}, e
	}
	claims := platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: c.OrchestratorID, Audience: tx.Scope().OwnerID, Purpose: "control", ObjectRef: tx.Scope().Ref(c.TaskID, 1), Digest: digest, ControlRevision: c.ControlRevision, WindowID: c.WindowID, IssuedAt: c.IssuedAt, StartBefore: c.StartBefore}
	compact, e := p.a.Keys.Sign("development-es256", claims)
	if e != nil {
		return api.ContentRef{}, e
	}
	id := stableID("content", "control-proof/"+c.WindowID)
	ref := api.ContentRef{TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, ContentID: id, Version: 1, Hash: api.Hash([]byte(compact)), MediaType: "application/jose", ByteLength: uint64(len(compact))}
	proof := sealedProof{Ref: ref, Compact: compact, Control: &c, Revision: 1}
	if e = tx.Create(ctx, "platform.proofs", id, c.TaskID, proof); e != nil {
		return ref, e
	}
	now, e := tx.Now(ctx)
	if e == nil {
		e = raiseProofJob(ctx, tx, id, now)
	}
	return ref, e
}

type closureProof struct{ a *App }

func (p closureProof) SealAllocationClosureTx(ctx context.Context, tx runtime.Tx, c api.AllocationClosure) (api.ContentRef, error) {
	c.ProofRef = api.ContentRef{}
	digest, err := api.Digest(c)
	if err != nil {
		return api.ContentRef{}, err
	}
	id := stableID("content", "allocation-closure/"+digest)
	var old sealedProof
	if _, err = tx.Get(ctx, "platform.proofs", id, &old); err == nil {
		if old.Allocation == nil || !api.Equal(*old.Allocation, c) {
			return api.ContentRef{}, api.E("idempotency_conflict", "allocation_closure_changed")
		}
		if old.PublicationFailure != nil {
			return api.ContentRef{}, api.E("invalid_state", "proof_publication_failed")
		}
		return old.Ref, nil
	} else if !api.IsCode(err, "not_found") {
		return api.ContentRef{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return api.ContentRef{}, err
	}
	compact, err := p.a.Keys.Sign("development-es256", platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: c.ParentOwnerID, Purpose: "allocation_closure", ObjectRef: tx.Scope().Ref(c.AllocationID, c.UsageRevision), Digest: digest, WindowID: id, IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(time.Hour))})
	if err != nil {
		return api.ContentRef{}, err
	}
	ref := api.ContentRef{TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, ContentID: id, Version: 1, Hash: api.Hash([]byte(compact)), MediaType: "application/jose", ByteLength: uint64(len(compact))}
	err = tx.Create(ctx, "platform.proofs", id, c.AllocationID, sealedProof{Ref: ref, Compact: compact, Allocation: &c, Revision: 1})
	if err == nil {
		err = raiseProofJob(ctx, tx, id, now)
	}
	return ref, err
}

func (p closureProof) SealClosureTx(ctx context.Context, tx runtime.Tx, c task.ClosureView) (api.ContentRef, error) {
	c.ProofRef = api.ContentRef{}
	digest, e := api.Digest(c)
	if e != nil {
		return api.ContentRef{}, e
	}
	id := stableID("content", "closure-proof/"+digest)
	var old sealedProof
	if _, e = tx.Get(ctx, "platform.proofs", id, &old); e == nil {
		if old.PublicationFailure != nil {
			return api.ContentRef{}, api.E("invalid_state", "proof_publication_failed")
		}
		return old.Ref, nil
	} else if !api.IsCode(e, "not_found") {
		return api.ContentRef{}, e
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return api.ContentRef{}, e
	}
	compact, e := p.a.Keys.Sign("development-es256", platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: tx.Scope().OwnerID, Purpose: "closure", ObjectRef: c.TaskRef, Digest: digest, WindowID: id, IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(time.Hour))})
	if e != nil {
		return api.ContentRef{}, e
	}
	ref := api.ContentRef{TenantID: tx.Scope().TenantID, OwnerID: tx.Scope().OwnerID, ContentID: id, Version: 1, Hash: api.Hash([]byte(compact)), MediaType: "application/jose", ByteLength: uint64(len(compact))}
	e = tx.Create(ctx, "platform.proofs", id, c.TaskRef.ObjectID, sealedProof{Ref: ref, Compact: compact, Closure: &c, Revision: 1})
	if e == nil {
		e = raiseProofJob(ctx, tx, id, now)
	}
	return ref, e
}

// 重读已封存的原来源证明，不更新签发时间或出版责任。
// VerifySource 仅核历史来源；当前 State/控制启动仍核各自的有限窗口。
func (p closureProof) CheckClosureProofTx(ctx context.Context, tx runtime.Tx, c task.ClosureView) error {
	var saved sealedProof
	if _, err := tx.Get(ctx, "platform.proofs", c.ProofRef.ContentID, &saved); err != nil {
		return err
	}
	unsigned := c
	unsigned.ProofRef = api.ContentRef{}
	if saved.Closure == nil || !api.Equal(*saved.Closure, unsigned) || saved.Ref != c.ProofRef || api.Hash([]byte(saved.Compact)) != c.ProofRef.Hash || uint64(len(saved.Compact)) != c.ProofRef.ByteLength {
		return api.E("idempotency_conflict", "original_closed_proof_changed")
	}
	if saved.PublicationFailure != nil {
		return api.E("invalid_state", "proof_publication_failed")
	}
	digest, err := api.Digest(unsigned)
	if err != nil {
		return err
	}
	_, err = p.a.Keys.VerifySource(saved.Compact, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: tx.Scope().OwnerID, Purpose: "closure", ObjectRef: c.TaskRef, Digest: digest, WindowID: c.ProofRef.ContentID})
	return err
}

func (a *App) verifyControlTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.ControlSnapshot) error {
	if !auth.HasRole("service") || auth.SubjectID != c.OrchestratorID {
		return api.E("forbidden", "orchestrator_identity_required")
	}
	if e := currentCredentialTx(ctx, tx, auth); e != nil {
		return e
	}
	var saved sealedProof
	if _, e := tx.Get(ctx, "platform.proofs", c.ProofRef.ContentID, &saved); e != nil {
		return api.E("forbidden", "original_control_proof_missing")
	}
	if saved.Control == nil || !api.Equal(saved.Ref, c.ProofRef) || api.Hash([]byte(saved.Compact)) != c.ProofRef.Hash {
		return api.E("forbidden", "control_proof_binding_mismatch")
	}
	if saved.PublicationFailure != nil {
		return api.E("forbidden", "control_proof_publication_failed")
	}
	unsigned := c
	unsigned.ProofRef = api.ContentRef{}
	if !api.Equal(*saved.Control, unsigned) {
		return api.E("forbidden", "control_proof_binding_mismatch")
	}
	digest, e := api.Digest(unsigned)
	if e != nil {
		return e
	}
	_, e = a.Keys.VerifySource(saved.Compact, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: c.OrchestratorID, Audience: tx.Scope().OwnerID, Purpose: "control", ObjectRef: tx.Scope().Ref(c.TaskID, 1), Digest: digest, ControlRevision: c.ControlRevision, WindowID: c.WindowID, IssuedAt: c.IssuedAt, StartBefore: c.StartBefore})
	return e
}
func (a *App) publishProof(ctx context.Context, store runtime.Store, s runtime.Scope, w runtime.Work) error {
	var p sealedProof
	if _, e := store.Read(ctx, s, "platform.proofs", w.Job.SourceRef.ObjectID, 0, &p); e != nil {
		return e
	}
	if p.PublicationFailure != nil {
		return finishFailedProofReplay(ctx, store, s, w, p)
	}
	if !p.Published {
		ref, e := a.Publish(ctx, s, a.ServiceAuth, p.Ref.ContentID, p.Ref.MediaType, []byte(p.Compact), []api.ContentRef{}, []api.ContentRef{})
		if e != nil {
			return a.finishRejectedProof(ctx, store, s, w, p, e)
		}
		if !api.Equal(ref, p.Ref) {
			return api.E("idempotency_conflict", "proof_publication_changed")
		}
	}
	return runtime.Finish(ctx, store, s, []string{"platform"}, w, runtime.Done(), func(tx runtime.Tx) error {
		var current sealedProof
		rev, e := tx.Get(ctx, "platform.proofs", p.Ref.ContentID, &current)
		if e != nil {
			return e
		}
		if current.Published {
			return nil
		}
		current.Published = true
		current.Revision++
		return tx.Put(ctx, "platform.proofs", p.Ref.ContentID, rev, current)
	})
}
