package development

import (
	"context"
	"errors"
	"os"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 失败是原 publication 的独立终态；保留原身份、回执和准确引用，
// Published 始终为 false，不能被消费者解释为已出版的可读证明。
type proofPublicationFailure struct {
	Plan           publicationPlan `json:"plan"`
	ReserveReceipt api.Receipt     `json:"reserve_receipt"`
	RecordedAt     string          `json:"recorded_at"`
}

func checkProofPublicationWork(scope runtime.Scope, work runtime.Work, proof sealedProof) error {
	if err := api.ValidateRecord("ContentRef", proof.Ref); err != nil {
		return err
	}
	kinds := 0
	for _, present := range []bool{proof.Control != nil, proof.Closure != nil, proof.Allocation != nil} {
		if present {
			kinds++
		}
	}
	if work.Job.Kind != proofJob || work.Job.ResponsibilityKey != "proof/"+proof.Ref.ContentID || work.Job.SourceRef != scope.Ref(proof.Ref.ContentID, 1) || proof.Ref.TenantID != scope.TenantID || proof.Ref.OwnerID != scope.OwnerID || proof.Ref.Version != 1 || proof.Ref.MediaType != "application/jose" || proof.Ref.Hash != api.Hash([]byte(proof.Compact)) || proof.Ref.ByteLength != uint64(len(proof.Compact)) || proof.Revision == 0 || kinds != 1 {
		return api.E("idempotency_conflict", "original_proof_publication_changed")
	}
	return nil
}

func (a *App) finishRejectedProof(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, proof sealedProof, publicationErr error) error {
	var refusal *api.Error
	if !errors.As(publicationErr, &refusal) || refusal.Code != "expired" || refusal.Reason != "command_expired" {
		return publicationErr
	}
	if err := checkProofPublicationWork(scope, work, proof); err != nil {
		return err
	}
	var plan publicationPlan
	if _, err := store.Read(ctx, scope, "platform.publications", proof.Ref.ContentID, 1, &plan); err != nil {
		return err
	}
	if plan.Ref != proof.Ref || plan.SubjectID != a.ServiceAuth.SubjectID || len(plan.Processed) != 0 || len(plan.Disclosed) != 0 {
		return api.E("idempotency_conflict", "original_proof_publication_changed")
	}
	policy := plan.PolicyRef
	if policy == nil {
		original, err := store.LookupCommand(ctx, scope, plan.ReserveID)
		if err != nil {
			return err
		}
		var in memory.ReserveInput
		if err = api.Decode(original.Command.Payload, &in); err != nil {
			return err
		}
		policy = &in.PolicyRef
	}
	// 原 rejected reserve 与无票据尚不能单独证明介质不存在。
	// Locate 位于短事务外；任何已有对象、损坏或未知读取都保留原责任。
	_, err := a.Memory.Objects.Locate(ctx, proof.Ref)
	var missing *api.Error
	if err == nil {
		return api.E("effect_unknown", "original_proof_object_exists_without_publication")
	}
	if !errors.Is(err, os.ErrNotExist) && (!errors.As(err, &missing) || missing.Code != "gone" || missing.Reason != "content_bytes_missing") {
		return errors.Join(err, publicationErr)
	}
	request := memory.PublicationRequest{ContentRef: plan.Ref, TransferID: plan.TransferID, ReserveCommandID: plan.ReserveID, PutCommandID: plan.PutID, PolicyRef: *policy, ProcessedSources: plan.Processed, DisclosedSources: plan.Disclosed, RetentionUntil: plan.Retention, TransferDeadline: plan.Deadline}
	return runtime.Finish(ctx, store, scope, []string{"platform", "content", "memory", "governance"}, work, runtime.Done(), func(tx runtime.Tx) error {
		receipt, known, err := a.Memory.CheckUploadRejectionTx(ctx, tx, a.ServiceAuth, request)
		if err != nil {
			return err
		}
		if !known {
			return publicationErr
		}
		var current sealedProof
		revision, err := tx.Get(ctx, "platform.proofs", proof.Ref.ContentID, &current)
		if err != nil {
			return err
		}
		var currentPlan publicationPlan
		if _, err = tx.Get(ctx, "platform.publications", proof.Ref.ContentID, &currentPlan); err != nil {
			return err
		}
		var original sealedProof
		if err = tx.GetVersion(ctx, "platform.proofs", proof.Ref.ContentID, 1, &original); err != nil {
			return err
		}
		if current.Published || current.Ref != original.Ref || current.Compact != original.Compact || !api.Equal(current.Control, original.Control) || !api.Equal(current.Closure, original.Closure) || !api.Equal(current.Allocation, original.Allocation) || original.Ref != proof.Ref || original.Compact != proof.Compact || !api.Equal(currentPlan, plan) {
			return api.E("idempotency_conflict", "original_proof_publication_changed")
		}
		if current.PublicationFailure != nil {
			if !api.Equal(current.PublicationFailure.Plan, plan) || !api.Equal(current.PublicationFailure.ReserveReceipt, receipt) {
				return api.E("idempotency_conflict", "original_proof_publication_changed")
			}
			return nil
		}
		if revision >= api.MaxSafeInteger || current.Revision != revision {
			return api.E("overloaded", "proof_revision_exhausted")
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		current.PublicationFailure = &proofPublicationFailure{Plan: plan, ReserveReceipt: receipt, RecordedAt: api.Time(now)}
		current.Revision++
		return tx.Put(ctx, "platform.proofs", proof.Ref.ContentID, revision, current)
	})
}

func finishFailedProofReplay(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, proof sealedProof) error {
	if err := checkProofPublicationWork(scope, work, proof); err != nil {
		return err
	}
	return runtime.Finish(ctx, store, scope, []string{"platform"}, work, runtime.Done(), func(tx runtime.Tx) error {
		var current sealedProof
		if _, err := tx.Get(ctx, "platform.proofs", proof.Ref.ContentID, &current); err != nil {
			return err
		}
		if current.Published || current.Ref != proof.Ref || current.Compact != proof.Compact || current.PublicationFailure == nil || !api.Equal(current.PublicationFailure, proof.PublicationFailure) {
			return api.E("idempotency_conflict", "original_proof_publication_changed")
		}
		return nil
	})
}
