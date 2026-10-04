// Package content owns exact byte-version publication and its current policy.
package content

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"github.com/ruipengliu/lerna/runtime"
)

var ErrUnavailable = errors.New("content publication unavailable")
var ErrObjectIntegrity = errors.New("object bytes violate exact declaration")

type Config struct {
	Owner                  v.OwnerRef
	Store                  Repository
	Objects                Objects
	Limits                 Limits
	PublishBudget          time.Duration
	MaxPublicationAttempts int
	Worker                 string
}
type Service struct{ config Config }

func New(config Config) (*Service, error) {
	if _, err := v.Encode(config.Owner); err != nil {
		return nil, err
	}
	if config.Store == nil || config.Objects == nil || config.Limits.MaxPreparingVersions < 1 || config.Limits.MaxPreparingVersions > 4096 || config.Limits.MaxStagingBytes < 1 || config.Limits.MaxStagingBytes > 1<<30 || config.Limits.Lease <= 0 || config.Limits.Lease > 5*time.Minute || config.Limits.WorkTimeout <= 0 || config.Limits.WorkTimeout > config.Limits.Lease || config.PublishBudget <= 0 || config.PublishBudget > 24*time.Hour || config.MaxPublicationAttempts < 1 || config.MaxPublicationAttempts > 16 || config.Worker == "" || len(config.Worker) > 128 {
		return nil, errors.New("explicit finite Content storage, limits, publication policy and worker required")
	}
	return &Service{config: config}, nil
}
func owner(o v.OwnerRef) contract.OwnerRef {
	return contract.OwnerRef{TenantID: contract.ID(o.TenantID), OwnerID: contract.ID(o.OwnerID)}
}
func finite(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	_, ok := ctx.Deadline()
	return ok && ctx.Err() == nil
}
func trustedSubject(subject *v.SubjectBinding, o v.OwnerRef) (v.SubjectBinding, bool) {
	if subject == nil || subject.TenantID != o.TenantID {
		return v.SubjectBinding{}, false
	}
	data, err := v.Encode(*subject)
	if err != nil {
		return v.SubjectBinding{}, false
	}
	frozen, err := v.Decode[v.SubjectBinding](data)
	return frozen, err == nil
}
func refusal(code v.ErrorCode) error { return &v.ContractError{PublicError: v.PublicError{Code: code}} }
func rejected(ref v.CommandRef, code v.ErrorCode) v.CommandReceipt {
	next := "resolve_rejection"
	return v.NewCommandReceiptRejected(v.CommandReceiptRejected{CommandRef: ref, Reason: code, NextAction: &next})
}
func cutoff(text v.Time) time.Time {
	t, _ := time.Parse("2006-01-02T15:04:05.000000Z", string(text))
	return t
}
func wireTime(t time.Time) v.Time {
	return v.Time(t.UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
}
func earlier(a, b time.Time) time.Time {
	if b.Before(a) {
		return b
	}
	return a
}
func accepted(ref v.CommandRef, record Record) v.CommandReceipt {
	next := "query_original"
	return v.NewCommandReceiptAccepted(v.CommandReceiptAccepted{CommandRef: ref, ObjectRef: v.ContentTarget{TenantID: record.Ref.Owner.TenantID, OwnerID: record.Ref.Owner.OwnerID, Kind: "content", ID: record.Ref.ContentID}, ContentRef: record.Ref, RetainUntil: record.EffectiveRetainUntil, NextAction: &next})
}
func (s *Service) Put(ctx context.Context, raw []byte, subject *v.SubjectBinding) (v.TransportOutcome, error) {
	request, err := v.DecodePut(raw)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	principal, ok := trustedSubject(subject, s.config.Owner)
	if !ok || request.Payload.ContentRef.Owner != s.config.Owner {
		return v.TransportOutcome{}, refusal("forbidden")
	}
	if !finite(ctx) {
		return v.TransportOutcome{}, ErrUnavailable
	}
	subjectJSON, _ := v.Encode(principal)
	digest, err := v.CommandDigest(raw, subjectJSON)
	if err != nil {
		return v.TransportOutcome{}, err
	}
	ref := v.CommandRef{Owner: s.config.Owner, CommandID: request.CommandID}
	var fixed v.CommandReceipt
	err = s.config.Store.Within(ctx, owner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		allowed, err := s.config.Store.CheckCommandReader(ctx, tx, principal, now)
		if err != nil {
			return err
		}
		if !allowed {
			return refusal("forbidden")
		}
		original, err := s.config.Store.LockCommand(ctx, tx, ref)
		if err != nil {
			return err
		}
		if original != nil {
			old, _ := v.Encode(original.Subject)
			if string(old) != string(subjectJSON) {
				return refusal("forbidden")
			}
			if original.Digest != digest {
				return refusal("idempotency_conflict")
			}
			fixed = original.Receipt
			return nil
		}
		reject := func(reason v.ErrorCode) error {
			fixed = rejected(ref, reason)
			return s.config.Store.SaveCommand(ctx, tx, ref, CommandRecord{Digest: digest, Subject: principal, Ref: request.Payload.ContentRef, Purpose: string(request.Payload.Purpose), Receipt: fixed})
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(cutoff(request.AcceptBefore)) {
			return reject("expired")
		}
		policy, err := s.config.Store.CheckPolicy(ctx, tx, principal, request.Payload.ContentRef, string(request.Payload.Purpose), "save", now)
		if err != nil {
			return err
		}
		if policy == nil {
			return reject("forbidden")
		}
		bytes, err := v.DecodeBytes(request.Payload.BytesBase64)
		if err != nil {
			return err
		}
		length, _ := strconv.ParseInt(string(request.Payload.ContentRef.ByteLength), 10, 64)
		if length > v.MaxContentBytes {
			return reject("input_over_limit")
		}
		sum := sha256.Sum256(bytes)
		if length != int64(len(bytes)) || request.Payload.ContentRef.Hash != "sha256:"+hex.EncodeToString(sum[:]) {
			return reject("integrity")
		}
		tuple, err := TupleDigest(request.Payload)
		if err != nil {
			return err
		}
		record, err := s.config.Store.LockVersion(ctx, tx, request.Payload.ContentRef)
		if err != nil {
			return err
		}
		policyBefore := policy.ValidUntil
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(cutoff(request.AcceptBefore)) {
			return reject("expired")
		}
		if !now.Before(policyBefore) {
			return reject("forbidden")
		}
		effective := earlier(cutoff(request.Payload.RetainUntil), policy.RetainUntil)
		if !effective.After(now) {
			return reject("expired")
		}
		if record != nil {
			if record.TupleDigest != tuple {
				return reject("version_conflict")
			}
			fixed = accepted(ref, *record)
			return s.config.Store.SaveCommand(ctx, tx, ref, CommandRecord{Digest: digest, Subject: principal, Ref: record.Ref, Purpose: record.Purpose, Receipt: fixed})
		}
		if policy.Ref != request.Payload.ContentRef {
			return reject("forbidden")
		}
		// Direct sources are real published inputs in this owner's bounded scope.
		// The later source-policy ticket adds full inherited closure and revocation.
		for _, source := range request.Payload.Sources {
			if source.Owner.TenantID != s.config.Owner.TenantID {
				return reject("forbidden")
			}
			if source.Owner != s.config.Owner {
				return reject("unsupported")
			}
			for _, action := range []string{"read", "process", "save"} {
				sourcePolicy, err := s.config.Store.CheckPolicy(ctx, tx, principal, source, string(request.Payload.Purpose), action, now)
				if err != nil {
					return err
				}
				if sourcePolicy == nil {
					return reject("forbidden")
				}
				policyBefore = earlier(policyBefore, sourcePolicy.ValidUntil)
				effective = earlier(effective, sourcePolicy.RetainUntil)
			}
			sourceRecord, err := s.config.Store.LockVersion(ctx, tx, source)
			if err != nil {
				return err
			}
			if sourceRecord == nil || sourceRecord.Ref != source || sourceRecord.Publication != "published" {
				return reject("source_unavailable")
			}
			effective = earlier(effective, cutoff(sourceRecord.CurrentRetainUntil))
		}
		if !effective.After(now) {
			return reject("expired")
		}
		available, err := s.config.Store.CheckCapacity(ctx, tx, s.config.Limits, int64(len(bytes)))
		if err != nil {
			return err
		}
		if !available {
			return reject("input_over_limit")
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(cutoff(request.AcceptBefore)) || !now.Before(effective) {
			return reject("expired")
		}
		if !now.Before(policyBefore) {
			return reject("forbidden")
		}
		id, key, err := VersionIdentity(request.Payload.ContentRef)
		if err != nil {
			return err
		}
		deadline := earlier(effective, now.Add(s.config.PublishBudget))
		record = &Record{Ref: request.Payload.ContentRef, Sources: append([]v.ContentRef{}, request.Payload.Sources...), Purpose: string(request.Payload.Purpose), Subject: principal, TupleDigest: tuple, ObjectID: id, ObjectKey: key, RequestedRetainUntil: request.Payload.RetainUntil, EffectiveRetainUntil: wireTime(effective), CurrentRetainUntil: wireTime(effective), AdmittedAt: wireTime(now), PublishDeadline: wireTime(deadline), Publication: "preparing", Revision: 1, StagingHolder: true, Bytes: bytes, MaxPublicationAttempts: s.config.MaxPublicationAttempts}
		if bytes == nil {
			record.Bytes = []byte{}
		}
		if err = s.config.Store.SaveVersion(ctx, tx, *record); err != nil {
			return err
		}
		if _, err = s.config.Store.Trigger(ctx, tx, contract.ObjectRef{TenantID: contract.ID(s.config.Owner.TenantID), OwnerID: contract.ID(s.config.Owner.OwnerID), Kind: "content", ID: contract.ID(id)}, "publish", 1, now); err != nil {
			return err
		}
		fixed = accepted(ref, *record)
		return s.config.Store.SaveCommand(ctx, tx, ref, CommandRecord{Digest: digest, Subject: principal, Ref: record.Ref, Purpose: record.Purpose, Receipt: fixed})
	})
	if errors.Is(err, runtime.ErrCommitUnknown) {
		return v.NewTransportOutcomeCommitUnknown(v.TransportOutcomeCommitUnknown{CommandRef: ref, NextAction: "query_or_retransmit_original"}), nil
	}
	if err != nil {
		return v.TransportOutcome{}, err
	}
	return v.NewTransportOutcomeReceived(v.TransportOutcomeReceived{Receipt: fixed}), nil
}
func (s *Service) Get(ctx context.Context, raw []byte, subject *v.SubjectBinding) (v.ContentGetResponse, error) {
	request, err := v.DecodeGet(raw)
	if err != nil {
		return v.ContentGetResponse{}, err
	}
	ref := request.Payload.ContentRef
	denied := func(code v.ErrorCode) v.ContentGetResponse {
		return v.NewContentGetResponseRejected(v.ContentGetResponseRejected{Reason: code})
	}
	principal, ok := trustedSubject(subject, s.config.Owner)
	if !ok || ref.Owner != s.config.Owner {
		return denied("forbidden"), nil
	}
	if !finite(ctx) {
		return v.NewContentGetResponseUnavailable(v.ContentGetResponseUnavailable{ContentRef: ref, Reason: "dependency_unavailable"}), nil
	}
	var result v.ContentGetResponse
	var record *Record
	var readBefore time.Time
	err = s.config.Store.Within(ctx, owner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(cutoff(request.AcceptBefore)) {
			result = denied("expired")
			return nil
		}
		readBefore = time.Time{}
		for _, action := range []string{"read", "disclose"} {
			policy, err := s.config.Store.CheckPolicy(ctx, tx, principal, ref, string(request.Payload.Purpose), action, now)
			if err != nil {
				return err
			}
			if policy == nil {
				result = denied("forbidden")
				return nil
			}
			bound := earlier(policy.ValidUntil, policy.RetainUntil)
			if readBefore.IsZero() {
				readBefore = bound
			} else {
				readBefore = earlier(readBefore, bound)
			}
		}
		record, err = s.config.Store.LockVersion(ctx, tx, ref)
		if err != nil {
			return err
		}
		if record == nil {
			result = v.NewContentGetResponseNotFound(v.ContentGetResponseNotFound{ContentRef: ref})
			return nil
		}
		if record.Ref != ref {
			result = denied("integrity")
			record = nil
			return nil
		}
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		readBefore = earlier(readBefore, cutoff(record.CurrentRetainUntil))
		if !now.Before(readBefore) {
			result = denied("expired")
			record = nil
			return nil
		}
		switch record.Publication {
		case "preparing":
			result = v.NewContentGetResponsePreparing(v.ContentGetResponsePreparing{ContentRef: ref})
		case "failed":
			result = v.NewContentGetResponseFailed(v.ContentGetResponseFailed{ContentRef: ref, Reason: string(record.Failure)})
		case "published":
		default:
			return ErrUnavailable
		}
		return nil
	})
	if err != nil {
		return v.NewContentGetResponseUnavailable(v.ContentGetResponseUnavailable{ContentRef: ref, Reason: "dependency_unavailable"}), nil
	}
	if record == nil || record.Publication != "published" {
		return result, nil
	}
	length, _ := strconv.ParseInt(string(ref.ByteLength), 10, 64)
	bounded, cancel := context.WithDeadline(ctx, readBefore)
	defer cancel()
	bytes, err := s.config.Objects.Read(bounded, record.ObjectKey, ref.Hash, length)
	if err == nil {
		err = bounded.Err()
	}
	if err != nil {
		reason := "dependency_unavailable"
		if errors.Is(err, ErrObjectIntegrity) {
			reason = "integrity"
		}
		return v.NewContentGetResponseUnavailable(v.ContentGetResponseUnavailable{ContentRef: ref, Reason: reason}), nil
	}
	if request.Payload.Range != nil {
		offset, _ := strconv.ParseInt(string(request.Payload.Range.Offset), 10, 64)
		size, _ := strconv.ParseInt(string(request.Payload.Range.Length), 10, 64)
		if offset > length || size > length-offset {
			return denied("range_invalid"), nil
		}
		bytes = bytes[offset : offset+size]
	}
	result = v.NewContentGetResponsePublished(v.ContentGetResponsePublished{ContentRef: ref, BytesBase64: base64.StdEncoding.EncodeToString(bytes), Range: request.Payload.Range})
	if _, err = v.EncodeContentResponse(result, request.Payload); err != nil {
		return v.ContentGetResponse{}, err
	}
	return result, nil
}
func (s *Service) GetCommand(ctx context.Context, raw []byte, subject *v.SubjectBinding) (v.CommandGetResponse, error) {
	request, err := v.DecodeCommand(raw)
	if err != nil {
		return v.CommandGetResponse{}, err
	}
	ref := request.Payload.CommandRef
	denied := func(code v.ErrorCode) v.CommandGetResponse {
		return v.NewCommandGetResponseRejected(v.CommandGetResponseRejected{Reason: code})
	}
	principal, ok := trustedSubject(subject, s.config.Owner)
	if !ok || ref.Owner != s.config.Owner {
		return denied("forbidden"), nil
	}
	unavailable := v.NewCommandGetResponseUnavailable(v.CommandGetResponseUnavailable{CommandRef: ref, Reason: "dependency_unavailable"})
	if !finite(ctx) {
		return unavailable, nil
	}
	var result v.CommandGetResponse
	err = s.config.Store.Within(ctx, owner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if !now.Before(cutoff(request.AcceptBefore)) {
			result = denied("expired")
			return nil
		}
		allowed, err := s.config.Store.CheckCommandReader(ctx, tx, principal, now)
		if err != nil {
			return err
		}
		if !allowed {
			result = denied("forbidden")
			return nil
		}
		record, err := s.config.Store.ReadCommandRecord(ctx, tx, ref)
		if err != nil {
			return err
		}
		if record == nil {
			result = v.NewCommandGetResponseNotFound(v.CommandGetResponseNotFound{CommandRef: ref})
			return nil
		}
		a, _ := v.Encode(record.Subject)
		b, _ := v.Encode(principal)
		if string(a) != string(b) {
			result = denied("forbidden")
			return nil
		}
		progress := v.NewCommandProgressNone(v.CommandProgressNone{})
		if _, accepted := record.Receipt.AsAccepted(); accepted {
			progress = v.NewCommandProgressUnavailable(v.CommandProgressUnavailable{Reason: "dependency_unavailable"})
			version, err := s.config.Store.LockVersion(ctx, tx, record.Ref)
			if err != nil {
				return err
			}
			if version != nil && version.Ref == record.Ref {
				progress = v.NewCommandProgressContent(v.CommandProgressContent{ContentRef: version.Ref, Publication: version.Publication})
			}
		}
		result = v.NewCommandGetResponseFound(v.CommandGetResponseFound{CommandRef: ref, Receipt: record.Receipt, Progress: progress})
		return nil
	})
	if err != nil {
		return unavailable, nil
	}
	if _, err = v.EncodeCommandResponse(result, ref); err != nil {
		return unavailable, nil
	}
	return result, nil
}

// Step resumes one original durable publication, with object I/O outside every
// database transaction. The caller owns the finite synchronous invocation.
func (s *Service) Step(ctx context.Context) (bool, error) {
	if !finite(ctx) {
		return false, ErrUnavailable
	}
	var selected *Record
	var claim *runtime.Claim
	err := s.config.Store.Within(ctx, owner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		jobs, err := s.config.Store.Scan(ctx, tx, now, 64)
		if err != nil {
			return err
		}
		for _, job := range jobs {
			record, err := s.config.Store.LockObject(ctx, tx, string(job.Object.ID))
			if err != nil {
				return err
			}
			if record == nil || record.ValidateIdentity() != nil || record.ObjectID != string(job.Object.ID) || job.Object.Kind != "content" || job.Phase != "publish" {
				return runtime.ErrScope
			}
			current, ioBefore, failure, err := s.publicationPolicy(ctx, tx, *record, now)
			if err != nil {
				return err
			}
			now, err = s.config.Store.Now(ctx, tx)
			if err != nil {
				return err
			}
			if record.Attempts >= record.MaxPublicationAttempts && failure == "" {
				failure = "dependency_unavailable"
			}
			until := earlier(now.Add(s.config.Limits.Lease), ioBefore)
			if failure != "" {
				until = now.Add(s.config.Limits.Lease)
			}
			claim, err = s.config.Store.Claim(ctx, tx, job, s.config.Worker, now, until)
			if err != nil {
				return err
			}
			if claim == nil {
				continue
			}
			record.CurrentRetainUntil = wireTime(current)
			record.IODeadline = wireTime(ioBefore)
			if failure != "" {
				record.Publication = "failed"
				record.Failure = failure
				record.CleanupPending = true
			} else {
				record.Attempts++
			}
			record.AttemptKey = fmt.Sprintf("%s.%d.tmp", record.ObjectKey, claim.Epoch)
			record.Revision++
			if err = s.config.Store.SaveVersion(ctx, tx, *record); err != nil {
				return err
			}
			selected = record
			break
		}
		return nil
	})
	if err != nil || selected == nil {
		return false, err
	}
	ioDeadline := earlier(earlier(cutoff(selected.IODeadline), claim.LeaseUntil), time.Now().Add(s.config.Limits.WorkTimeout))
	bounded, cancel := context.WithDeadline(ctx, ioDeadline)
	var ioErr error
	length, _ := strconv.ParseInt(string(selected.Ref.ByteLength), 10, 64)
	if selected.Publication == "preparing" {
		ioErr = bounded.Err()
		if ioErr == nil {
			ioErr = s.config.Objects.Put(bounded, selected.ObjectKey, selected.AttemptKey, selected.Ref.Hash, length, selected.Bytes)
		}
	}
	cancel()
	err = s.config.Store.Within(ctx, owner(s.config.Owner), func(ctx context.Context, tx runtime.Tx) error {
		record, err := s.config.Store.LockVersion(ctx, tx, selected.Ref)
		if err != nil {
			return err
		}
		if record == nil || record.ObjectID != string(claim.Object.ID) || record.ValidateIdentity() != nil {
			return runtime.ErrScope
		}
		now, err := s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = s.config.Store.ValidateClaim(ctx, tx, *claim, now); err != nil {
			return err
		}
		if record.Publication != "preparing" {
			return s.config.Store.Complete(ctx, tx, *claim, now)
		}
		current, ioBefore, failure, err := s.publicationPolicy(ctx, tx, *record, now)
		if err != nil {
			return err
		}
		record.CurrentRetainUntil = wireTime(current)
		record.IODeadline = wireTime(ioBefore)
		now, err = s.config.Store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = s.config.Store.ValidateClaim(ctx, tx, *claim, now); err != nil {
			return err
		}
		if failure != "" {
		} else if errors.Is(ioErr, ErrObjectIntegrity) {
			failure = "integrity"
		} else if ioErr != nil && record.Attempts >= record.MaxPublicationAttempts {
			failure = "dependency_unavailable"
		}
		if failure == "" && ioErr != nil {
			// Policy tightening observed after I/O remains durable across retries.
			record.Revision++
			if err = s.config.Store.SaveVersion(ctx, tx, *record); err != nil {
				return err
			}
			due := earlier(now.Add(100*time.Millisecond), cutoff(record.PublishDeadline))
			return s.config.Store.DeferClaim(ctx, tx, *claim, now, due)
		}
		record.Revision++
		if failure != "" {
			record.Publication = "failed"
			record.Failure = failure
			record.CleanupPending = true
			record.ObjectHolder = ioErr == nil
		} else {
			record.Publication = "published"
			record.ObjectHolder = true
			record.StagingHolder = false
			record.Bytes = nil
		}
		if err = s.config.Store.SaveVersion(ctx, tx, *record); err != nil {
			return err
		}
		return s.config.Store.Complete(ctx, tx, *claim, now)
	})
	return true, err
}

func (s *Service) publicationPolicy(ctx context.Context, tx runtime.Tx, record Record, now time.Time) (time.Time, time.Time, v.ErrorCode, error) {
	current := cutoff(record.CurrentRetainUntil)
	ioBefore := earlier(current, cutoff(record.PublishDeadline))
	policy, err := s.config.Store.CheckPolicy(ctx, tx, record.Subject, record.Ref, record.Purpose, "save", now)
	if err != nil {
		return current, ioBefore, "", err
	}
	if policy == nil || policy.Ref != record.Ref {
		return current, ioBefore, "forbidden", nil
	}
	current = earlier(current, policy.RetainUntil)
	ioBefore = earlier(ioBefore, policy.ValidUntil)
	for _, source := range record.Sources {
		for _, action := range []string{"read", "process", "save"} {
			p, err := s.config.Store.CheckPolicy(ctx, tx, record.Subject, source, record.Purpose, action, now)
			if err != nil {
				return current, ioBefore, "", err
			}
			if p == nil || p.Ref != source {
				return current, ioBefore, "forbidden", nil
			}
			current = earlier(current, p.RetainUntil)
			ioBefore = earlier(ioBefore, p.ValidUntil)
		}
		sourceRecord, err := s.config.Store.LockVersion(ctx, tx, source)
		if err != nil {
			return current, ioBefore, "", err
		}
		if sourceRecord == nil || sourceRecord.Ref != source || sourceRecord.Publication != "published" {
			return current, ioBefore, "source_unavailable", nil
		}
		current = earlier(current, cutoff(sourceRecord.CurrentRetainUntil))
	}
	now, err = s.config.Store.Now(ctx, tx)
	if err != nil {
		return current, ioBefore, "", err
	}
	ioBefore = earlier(ioBefore, current)
	if !now.Before(current) || !now.Before(cutoff(record.PublishDeadline)) || !now.Before(ioBefore) {
		return current, ioBefore, "expired", nil
	}
	return current, ioBefore, "", nil
}
