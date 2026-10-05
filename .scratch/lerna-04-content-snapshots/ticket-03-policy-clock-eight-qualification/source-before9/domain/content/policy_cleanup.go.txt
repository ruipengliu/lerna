package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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
	expectedKey, err := changeKeyForCleanup(candidates.Change, l.config.Owner)
	if err != nil {
		return PropagationObservation{}, err
	}
	if candidates.Change.Key != changeKey || candidates.Change.Key != expectedKey {
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
		type sealing struct {
			expected CleanupResponsibility
			record   *Record
			request  SealRequest
			cap      time.Time
			due      time.Time
		}
		qualified := []qualification{}
		seals := []sealing{}
		// All version/policy acquisition precedes every responsibility row lock.
		for _, original := range candidates.Responsibilities {
			if original.ChangeKey != changeKey || original.Ref.Owner != l.config.Owner {
				return runtime.ErrScope
			}
			change := candidates.Change
			targeted := change.AdmissionTarget != nil
			if targeted && original.Ref != *change.AdmissionTarget {
				return runtime.ErrScope
			}
			capCandidate := original.Reason == "accepted_retention_expired"
			if original.BodyCleanup != "pending" || original.Residual != "holder_unconfirmed" || original.Reason != "" && !capCandidate {
				continue
			}
			if change.Reason != "" && change.Reason != "original_deadline_expired" || targeted && !capCandidate {
				continue
			}
			if !capCandidate && (change.Previous == nil || !change.Previous.Save || change.Policy.Save || change.Previous.Ref != change.Policy.Ref || change.Previous.Purpose != change.Policy.Purpose || !sameSavingSubject(change.Previous.Subject, change.Policy.Subject)) {
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
			actualPolicy, err := l.store.CurrentSavingPolicy(ctx, tx, original.Subject, original.Ref, original.Purpose)
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
			var expiredCap time.Time
			if capCandidate {
				expiredCap, err = cleanupAcceptedCap(record.CurrentRetainUntil)
				if err != nil {
					return err
				}
				if now.Before(expiredCap) {
					continue
				}
			} else if !now.Before(cutoff(record.CurrentRetainUntil)) {
				continue
			}
			service := Service{config: Config{Owner: l.config.Owner, Store: l.store}}
			// Structural membership is qualified even when save itself is
			// withdrawn. Missing/incorrect source facts never authorize deletion.
			structure, err := service.registeredClosure(ctx, tx, record.Ref, record.Sources, record.Subject, record.Purpose, nil)
			if err != nil {
				if _, denied := qualificationCode(err); denied {
					continue
				}
				return err
			}
			found := record.Ref == change.Policy.Ref
			for _, ref := range structure.refs {
				found = found || ref == change.Policy.Ref
			}
			if !found {
				continue
			}
			knownFalse := actualPolicy != nil && !actualPolicy.Save
			known := actualPolicy != nil
			for _, source := range structure.records {
				current, err := l.store.CurrentSavingPolicy(ctx, tx, record.Subject, source.Ref, record.Purpose)
				if err != nil {
					return err
				}
				known = known && current != nil
				knownFalse = knownFalse || current != nil && !current.Save
			}
			if capCandidate || known && knownFalse {
				if err = l.manager.current(ctx, tx); err != nil {
					return err
				}
				now, err = l.store.Now(ctx, tx)
				if err != nil {
					return err
				}
				if !now.Before(original.Deadline) || original.Deadline.After(now.Add(l.config.WorkBudget)) || original.Deadline.After(l.config.TrustedUntil) || !expiredCap.IsZero() && now.Before(expiredCap) {
					continue
				}
				var due time.Time
				if targeted {
					if now.Before(change.ExpiryDue) || original.Deadline.After(change.ExpiryDeadline) {
						continue
					}
					due = change.ExpiryDue
				}
				seals = append(seals, sealing{expected: original, record: record, request: SealRequest{Ref: record.Ref, Purpose: record.Purpose, SealID: policySealID(changeKey, record.Ref), Deadline: original.Deadline}, cap: expiredCap, due: due})
				continue
			}
			if policy == nil || policy.Ref != record.Ref {
				continue
			}
			sources, err := service.registeredClosure(ctx, tx, record.Ref, record.Sources, record.Subject, record.Purpose, []string{"save"})
			if err != nil {
				if _, denied := qualificationCode(err); denied {
					continue
				}
				return err
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
		// Shared sealing may lock holder/job facts, but never acquires another
		// version or policy after this page's complete qualification phase.
		for _, current := range seals {
			if _, err := l.sealLocked(ctx, tx, current.record, current.request, changeKey); err != nil {
				return err
			}
		}
		for _, current := range qualified {
			if err := l.store.QualifyPolicyCleanupNotRequired(ctx, tx, current.expected); err != nil {
				return err
			}
		}
		for _, current := range seals {
			if err := l.store.BindPolicyCleanupSeal(ctx, tx, current.expected, *current.record.BodySeal); err != nil {
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
		for _, current := range seals {
			if !now.Before(current.request.Deadline) || !current.cap.IsZero() && now.Before(current.cap) || !current.due.IsZero() && now.Before(current.due) {
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

func changeKeyForCleanup(change PolicyChange, own v.OwnerRef) (string, error) {
	if change.AdmissionTarget == nil {
		return changeKey(change.Policy), nil
	}
	if _, err := v.Encode(*change.AdmissionTarget); err != nil {
		return "", runtime.ErrScope
	}
	if _, err := v.Encode(change.Policy.Ref); err != nil {
		return "", runtime.ErrScope
	}
	if _, ok := trustedSubject(&change.Policy.Subject, own); !ok || change.AdmissionTarget.Owner != own || change.Policy.Ref.Owner != own || change.Policy.Purpose == "" || change.Policy.Revision <= 0 {
		return "", runtime.ErrScope
	}
	phase, known := propagationPhase(change)
	budget := change.ExpiryDeadline.Sub(change.ExpiryDue)
	if !known || phase != "natural_expiry" || change.Previous != nil || change.ExpiryDue.IsZero() || !change.Due.Equal(change.ExpiryDue) || !change.Deadline.Equal(change.ExpiryDeadline) || budget <= 0 || budget > 24*time.Hour {
		return "", runtime.ErrScope
	}
	return admissionKey(*change.AdmissionTarget, change.Policy, change.ExpiryDue), nil
}

// Destructive cleanup cannot treat cutoff's parse-error zero as an expired cap.
func cleanupAcceptedCap(text v.Time) (time.Time, error) {
	cap, err := time.Parse("2006-01-02T15:04:05.000000Z", string(text))
	if err != nil || cap.IsZero() || cap.Year() < 1 || cap.Format("2006-01-02T15:04:05.000000Z") != string(text) {
		return time.Time{}, runtime.ErrScope
	}
	return cap, nil
}

func policySealID(key string, ref v.ContentRef) string {
	raw, _ := json.Marshal(struct {
		ChangeKey string
		Ref       v.ContentRef
	}{key, ref})
	sum := sha256.Sum256(raw)
	return "policy-seal-" + hex.EncodeToString(sum[:])
}
