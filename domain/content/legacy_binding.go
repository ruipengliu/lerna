package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// LegacyPrimaryQualification is fixed by the trusted host after its original
// scope supervisor confirms every old writer closed and exited. Public
// requests cannot supply or amend these facts. The consumer supports only the
// finite published versions and complete actual attempts in this handoff.
type LegacyPrimaryQualification struct {
	ID             string                 `json:"id"`
	Namespace      string                 `json:"namespace"`
	Owner          v.OwnerRef             `json:"owner"`
	WriterSHA      string                 `json:"writer_sha"`
	Binding        string                 `json:"binding"`
	EvidenceDigest string                 `json:"evidence_digest"`
	ValidUntil     time.Time              `json:"valid_until"`
	Versions       []LegacyPrimaryVersion `json:"versions"`
}

type LegacyPrimaryVersion struct {
	Ref         v.ContentRef     `json:"content_ref"`
	Subject     v.SubjectBinding `json:"subject"`
	Purpose     string           `json:"purpose"`
	ObjectKey   string           `json:"object_key"`
	AttemptKeys []string         `json:"attempt_keys"`
}

type LegacyPrimaryRequest struct {
	Ref     v.ContentRef
	Purpose string
}

type LegacyPrimaryObservation struct {
	Ref             v.ContentRef `json:"content_ref"`
	Binding         string       `json:"binding"`
	QualificationID string       `json:"qualification_id"`
	EvidenceDigest  string       `json:"evidence_digest"`
	Publication     string       `json:"publication"`
}

func cloneLegacyPrimary(input *LegacyPrimaryQualification) *LegacyPrimaryQualification {
	if input == nil {
		return nil
	}
	result := *input
	result.Versions = make([]LegacyPrimaryVersion, len(input.Versions))
	for i, version := range input.Versions {
		result.Versions[i] = version
		result.Versions[i].Subject.DelegationChain = append([]v.DelegatedSubject{}, version.Subject.DelegationChain...)
		result.Versions[i].AttemptKeys = append([]string{}, version.AttemptKeys...)
	}
	return &result
}

var legacyEvidenceDigest = regexp.MustCompile(`^sha256:[a-f0-9]{64}$`)

// BindLegacyPrimary identifies original responsibility; it does not authorize
// normal body actions, revive caps, publish or erase. Two short transactions
// surround an actual bounded read of the host's fixed original medium.
func (l *Lifecycle) BindLegacyPrimary(ctx context.Context, subject *v.SubjectBinding, request LegacyPrimaryRequest) (LegacyPrimaryObservation, error) {
	var result LegacyPrimaryObservation
	if err := l.manager.authorize(ctx, subject); err != nil {
		return result, err
	}
	q := l.config.LegacyPrimary
	if q == nil {
		return result, ErrUnavailable
	}
	if q.ID == "" || len(q.ID) > 128 || q.Namespace == "" || q.Owner != l.config.Owner || q.Binding == "" || q.WriterSHA != "1a7d910238eb74cddc712d92b0ba4014a72ff507" || !legacyEvidenceDigest.MatchString(q.EvidenceDigest) || q.ValidUntil.IsZero() || len(q.Versions) == 0 || len(q.Versions) > 64 {
		return result, runtime.ErrScope
	}
	if request.Ref.Owner != q.Owner || request.Purpose == "" {
		return result, refusal("forbidden")
	}
	if _, err := v.Encode(request.Ref); err != nil {
		return result, err
	}
	var version *LegacyPrimaryVersion
	for i := range q.Versions {
		candidate := &q.Versions[i]
		if candidate.Ref == request.Ref {
			if version != nil {
				return result, runtime.ErrScope
			}
			version = candidate
		}
	}
	if version == nil || version.Purpose != request.Purpose || !sameSavingSubject(version.Subject, l.config.TrustedSubject) {
		return result, refusal("forbidden")
	}
	if l.config.Objects.Binding() != q.Binding {
		return result, ErrHolderBinding
	}
	var expected Record
	qualify := func(ctx context.Context, tx runtime.Tx, record *Record) (bool, error) {
		if err := l.manager.current(ctx, tx); err != nil {
			return false, err
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return false, err
		}
		if !now.Before(q.ValidUntil) || q.ValidUntil.After(l.config.TrustedUntil) || q.ValidUntil.After(now.Add(l.config.WorkBudget)) {
			return false, refusal("expired")
		}
		if record == nil || record.Ref != request.Ref || record.ObjectKey != version.ObjectKey || record.Purpose != version.Purpose || !sameSavingSubject(record.Subject, version.Subject) {
			return false, refusal("forbidden")
		}
		if err = record.ValidateIdentity(); err != nil {
			return false, err
		}
		if record.PrimaryHolderBinding != "" {
			if record.PrimaryHolderBinding == q.Binding && record.LegacyPrimaryQualificationID == q.ID && record.LegacyPrimaryEvidenceDigest == q.EvidenceDigest {
				return true, nil
			}
			return false, ErrHolderBinding
		}
		if record.LegacyPrimaryQualificationID != "" || record.LegacyPrimaryEvidenceDigest != "" || record.BodySeal != nil || record.BodyGone || record.Publication != "published" || record.Bytes != nil || record.StagingHolder || !record.ObjectHolder || len(version.AttemptKeys) == 0 || len(version.AttemptKeys) > 64 || len(version.AttemptKeys) != record.Attempts {
			return false, ErrUnavailable
		}
		seen := map[string]bool{}
		for _, attempt := range version.AttemptKeys {
			if seen[attempt] || !strings.HasPrefix(attempt, record.ObjectKey+".") || !strings.HasSuffix(attempt, ".tmp") {
				return false, runtime.ErrScope
			}
			epoch := strings.TrimSuffix(strings.TrimPrefix(attempt, record.ObjectKey+"."), ".tmp")
			n, err := strconv.ParseInt(epoch, 10, 64)
			if err != nil || n < 1 || n > int64(record.Attempts) || strconv.FormatInt(n, 10) != epoch {
				return false, runtime.ErrScope
			}
			seen[attempt] = true
		}
		return false, nil
	}
	observe := func(record Record) {
		result = LegacyPrimaryObservation{Ref: record.Ref, Binding: record.PrimaryHolderBinding, QualificationID: record.LegacyPrimaryQualificationID, EvidenceDigest: record.LegacyPrimaryEvidenceDigest, Publication: record.Publication}
	}
	bound := false
	err := l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		record, err := l.store.LockVersion(ctx, tx, request.Ref)
		if err != nil {
			return err
		}
		bound, err = qualify(ctx, tx, record)
		if err != nil {
			return err
		}
		if bound {
			checked, err := l.store.BindLegacyPrimary(ctx, tx, *record, *q)
			if err != nil {
				return err
			}
			if err = l.manager.current(ctx, tx); err != nil {
				return err
			}
			now, err := l.store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(q.ValidUntil) {
				return refusal("expired")
			}
			observe(checked)
			return nil
		}
		expected = *record
		return nil
	})
	if err != nil || bound {
		return result, err
	}
	length, err := strconv.ParseInt(string(expected.Ref.ByteLength), 10, 64)
	if err != nil {
		return result, err
	}
	bounded, cancel := context.WithDeadline(ctx, earlier(q.ValidUntil, l.config.TrustedUntil))
	body, readErr := l.config.Objects.Read(bounded, expected.ObjectKey, expected.Ref.Hash, length)
	readErr = errors.Join(readErr, bounded.Err())
	cancel()
	if readErr != nil {
		return result, readErr
	}
	digest := sha256.Sum256(body)
	if int64(len(body)) != length || expected.Ref.Hash != "sha256:"+hex.EncodeToString(digest[:]) {
		return result, ErrObjectIntegrity
	}
	if l.config.Objects.Binding() != q.Binding {
		return result, ErrHolderBinding
	}
	err = l.store.Within(ctx, owner(l.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		if err := l.manager.current(ctx, tx); err != nil {
			return err
		}
		record, err := l.store.LockVersion(ctx, tx, request.Ref)
		if err != nil {
			return err
		}
		already, err := qualify(ctx, tx, record)
		if err != nil {
			return err
		}
		if already {
			checked, err := l.store.BindLegacyPrimary(ctx, tx, *record, *q)
			if err != nil {
				return err
			}
			if err = l.manager.current(ctx, tx); err != nil {
				return err
			}
			now, err := l.store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if !now.Before(q.ValidUntil) {
				return refusal("expired")
			}
			observe(checked)
			return nil
		}
		if !reflect.DeepEqual(*record, expected) {
			return runtime.ErrClaim
		}
		next, err := l.store.BindLegacyPrimary(ctx, tx, expected, *q)
		if err != nil {
			return err
		}
		for _, attempt := range version.AttemptKeys {
			effect := next
			effect.AttemptKey = attempt
			if err = l.store.SavePublicationAttempt(ctx, tx, effect); err != nil {
				return err
			}
		}
		if err = l.manager.current(ctx, tx); err != nil {
			return err
		}
		now, err := l.store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(q.ValidUntil) {
			return refusal("expired")
		}
		observe(next)
		return nil
	})
	return result, err
}
