package content

import (
	"context"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

// ConsumePolicyCleanup qualifies one bounded page of original policy
// responsibilities. Body erasure remains the responsibility of Lifecycle.Step.
func (l *Lifecycle) ConsumePolicyCleanup(ctx context.Context, subject *v.SubjectBinding, changeKey, cursor string) (PropagationObservation, error) {
	// ReadChange takes a row lock. Release this selection transaction before
	// acquiring any version/policy locks in the qualification transaction.
	candidates, err := l.manager.ObserveChange(ctx, subject, changeKey, cursor, l.config.PageSize)
	if err != nil {
		return PropagationObservation{}, err
	}
	if candidates.Change.Key != changeKey || candidates.Change.Key != changeKeyForCleanup(candidates.Change) {
		return PropagationObservation{}, runtime.ErrScope
	}
	err = l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		type qualification struct {
			expected CleanupResponsibility
			bound    time.Time
		}
		qualified := []qualification{}
		// All version/policy acquisition precedes every responsibility row lock.
		for _, original := range candidates.Responsibilities {
			if original.ChangeKey != changeKey || original.Ref.Owner != l.config.Owner {
				return runtime.ErrScope
			}
			if original.BodyCleanup != "pending" || original.Reason != "" || original.Residual != "holder_unconfirmed" {
				continue
			}
			change := candidates.Change
			if change.Reason != "" && change.Reason != "original_deadline_expired" || change.AdmissionTarget != nil || change.Previous == nil || !change.Previous.Save || change.Policy.Save || change.Previous.Ref != change.Policy.Ref || change.Previous.Purpose != change.Policy.Purpose || !sameSavingSubject(change.Previous.Subject, change.Policy.Subject) {
				continue
			}
			if _, known := propagationPhase(change); !known {
				continue
			}
			// The candidate is identity/history only; current saving authority
			// comes from the original version's complete saving basis below.
			if !sameSavingSubject(original.Subject, l.config.TrustedSubject) {
				return refusal("forbidden")
			}
			now, err := l.store.Now(ctx, tx)
			if err != nil {
				return err
			}
			policy, err := l.store.CheckPolicy(ctx, tx, original.Subject, original.Ref, original.Purpose, []string{"save"}, now)
			if err != nil {
				return err
			}
			record, err := l.store.LockVersion(ctx, tx, original.Ref)
			if err != nil {
				return err
			}
			if err = l.manager.current(ctx, tx); err != nil {
				return err
			}
			if record == nil || record.Ref != original.Ref || record.ValidateIdentity() != nil || !sameSavingSubject(record.Subject, l.config.TrustedSubject) {
				return refusal("forbidden")
			}
			if !sameSavingSubject(original.Subject, record.Subject) || original.Purpose != record.Purpose || !sameSavingSubject(change.Policy.Subject, record.Subject) || change.Policy.Purpose != record.Purpose {
				continue
			}
			if record.Publication != "published" || record.BodySeal != nil || record.BodyGone {
				continue
			}
			now, err = l.store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(cutoff(record.CurrentRetainUntil)) {
				continue
			}
			if policy == nil || policy.Ref != record.Ref {
				continue
			}
			service := Service{config: Config{Owner: l.config.Owner, Store: l.store}}
			sources, err := service.registeredClosure(ctx, tx, record.Ref, record.Sources, record.Subject, record.Purpose, []string{"save"})
			if err != nil {
				if _, denied := qualificationCode(err); denied {
					continue
				}
				return err
			}
			found := record.Ref == change.Policy.Ref
			for _, ref := range sources.refs {
				found = found || ref == change.Policy.Ref
			}
			if !found {
				continue
			}
			bound := earlier(earlier(policy.ValidUntil, policy.RetainUntil), cutoff(record.CurrentRetainUntil))
			if len(sources.refs) > 0 {
				bound = earlier(bound, earlier(sources.validBefore, sources.retainBefore))
			}
			if err = l.manager.current(ctx, tx); err != nil {
				return err
			}
			now, err = l.store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(bound) {
				continue
			}
			qualified = append(qualified, qualification{original, bound})
		}
		for _, current := range qualified {
			if err := l.store.QualifyPolicyCleanupNotRequired(ctx, tx, current.expected); err != nil {
				return err
			}
		}
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		for _, current := range qualified {
			if !now.Before(current.bound) {
				return refusal("expired")
			}
		}
		return nil
	})
	if err != nil {
		return PropagationObservation{}, err
	}
	return l.manager.ObserveChange(ctx, subject, changeKey, cursor, l.config.PageSize)
}

func changeKeyForCleanup(change PolicyChange) string { return changeKey(change.Policy) }
