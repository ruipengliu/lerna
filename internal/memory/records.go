package memory

import (
	"context"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type CreateInput struct {
	MemoryID     string         `json:"memory_id"`
	Values       MemoryValues   `json:"values"`
	CandidateRef *api.ObjectRef `json:"candidate_ref,omitempty"`
}
type ReplaceInput struct {
	MemoryID string       `json:"memory_id"`
	Values   MemoryValues `json:"values"`
}
type RestrictInput struct {
	MemoryID            string           `json:"memory_id"`
	RestrictedPolicyRef api.ComponentRef `json:"restricted_policy_ref"`
}
type DeleteInput struct {
	MemoryID string `json:"memory_id"`
	Reason   string `json:"reason"`
}
type MemoryOutput struct {
	MemoryRef  api.ObjectRef `json:"memory_ref"`
	State      string        `json:"state"`
	ChangeHead uint64        `json:"change_head"`
}
type ReadMemoryInput struct {
	MemoryID string `json:"memory_id"`
	Revision uint64 `json:"revision,omitempty"`
	Purpose  string `json:"purpose,omitempty"`
}

func validMemoryType(value string) bool {
	return contains([]string{"fact", "preference", "inference", "experience"}, value)
}

func (s *Service) validateValues(ctx context.Context, tx runtime.Tx, auth runtime.Auth, values MemoryValues) error {
	if !validMemoryType(values.Type) || len(values.Sources) > 100 || values.Confidence != nil && (*values.Confidence < 0 || *values.Confidence > 1) {
		return api.E("invalid_request", "invalid_memory_values")
	}
	if _, err := api.ParseTime(values.ObservedAt); err != nil {
		return api.E("invalid_request", "invalid_observed_at")
	}
	var from, to time.Time
	var err error
	if values.ValidFrom != "" {
		if from, err = api.ParseTime(values.ValidFrom); err != nil {
			return api.E("invalid_request", "invalid_valid_from")
		}
	}
	if values.ValidTo != "" {
		if to, err = api.ParseTime(values.ValidTo); err != nil {
			return api.E("invalid_request", "invalid_valid_to")
		}
	}
	if !from.IsZero() && !to.IsZero() && !from.Before(to) {
		return api.E("invalid_request", "invalid_validity_interval")
	}
	p, err := s.allowed(ctx, tx, auth, values.PolicyRef, "memory.save", s.Location, false)
	if err != nil {
		return err
	}
	if values.Type == "experience" {
		var content ContentVersion
		_, err = tx.Get(ctx, "content.versions", contentKey(values.ContentRef), &content)
		if err != nil {
			return err
		}
		if values.ContentRef.MediaType != ExperienceMediaType || content.ExperienceOutcome == "" || content.ExperienceOutcome != "unknown" && content.ExperienceProofRef == nil {
			return api.E("invalid_request", "experience_requires_actual_outcome")
		}
	}
	refs := []api.ContentRef{values.ContentRef, values.ScopeRef}
	seen := map[string]bool{}
	for _, source := range values.Sources {
		if err = api.ValidateRecord("SourceEvidence", source); err != nil {
			return err
		}
		if seen[source.ContentRef.OwnerID+":"+contentKey(source.ContentRef)] {
			return api.E("invalid_request", "duplicate_source")
		}
		seen[source.ContentRef.OwnerID+":"+contentKey(source.ContentRef)] = true
		if source.SubmissionRef != nil {
			if err = runtime.CheckRef(tx.Scope(), *source.SubmissionRef); err != nil {
				return err
			}
		}
		refs = append(refs, source.ContentRef)
	}
	for _, ref := range refs {
		v, err := s.CheckContentTx(ctx, tx, auth, ref, "memory.save", s.Location, false)
		if err != nil {
			return err
		}
		if ref != values.ContentRef && ref != values.ScopeRef {
			if err = s.checkSourceGate(ctx, tx, auth, ref, "memory.save", s.Location, false); err != nil {
				return err
			}
		}
		sp, err := s.policy(ctx, tx, v.PolicyRef)
		if err != nil {
			return err
		}
		if !subset(p.Values.Subjects, sp.Values.Subjects) || !subset(p.Values.Purposes, sp.Values.Purposes) || !subset(p.Values.Locations, sp.Values.Locations) {
			return api.E("forbidden", "source_scope_expansion")
		}
	}
	return nil
}

func (s *Service) memoryAllowed(ctx context.Context, tx runtime.Tx, auth runtime.Auth, record MemoryRecord, purpose string, continuous bool) error {
	return s.memoryAllowedAt(ctx, tx, auth, record, purpose, s.Location, continuous)
}

func (s *Service) memoryAllowedAt(ctx context.Context, tx runtime.Tx, auth runtime.Auth, record MemoryRecord, purpose, location string, continuous bool) error {
	if record.State == "deleted" {
		return api.E("gone", "memory_deleted")
	}
	if record.State != "active" {
		return api.E("forbidden", "memory_not_active")
	}
	if purpose == "" {
		purpose = "memory.read"
	}
	if _, err := s.allowed(ctx, tx, auth, record.Values.PolicyRef, purpose, location, continuous); err != nil {
		return err
	}
	for _, ref := range []api.ContentRef{record.Values.ContentRef, record.Values.ScopeRef} {
		if _, err := s.CheckContentTx(ctx, tx, auth, ref, purpose, location, continuous); err != nil {
			return err
		}
	}
	policy, err := s.policy(ctx, tx, record.Values.PolicyRef)
	if err != nil {
		return err
	}
	for _, ref := range sourceRefs(record.Values.Sources) {
		if _, err = s.checkContent(ctx, tx, auth, ref, purpose, location, continuous, map[string]bool{}, 1, policy.Values.IndependentDerived); err != nil {
			return err
		}
	}
	return nil
}

func sourceRefs(values []api.SourceEvidence) []api.ContentRef {
	refs := make([]api.ContentRef, 0, len(values))
	for _, v := range values {
		refs = append(refs, v.ContentRef)
	}
	return refs
}

func appendChange(ctx context.Context, tx runtime.Tx, head ChangeHead, record MemoryRecord, kind string, visibilityChange bool) (uint64, error) {
	if head.ChangeHead >= api.MaxSafeInteger {
		return 0, api.E("overloaded", "change_head_exhausted")
	}
	head.ChangeHead++
	if visibilityChange {
		head.VisibilityRevision++
	}
	change := MemoryChange{MemoryID: record.MemoryID, Revision: record.Revision, ChangeSeq: head.ChangeHead, Kind: kind}
	if err := tx.Create(ctx, "memory.changes", fmt.Sprintf("%016d", change.ChangeSeq), record.MemoryID, change); err != nil {
		return 0, err
	}
	if err := saveHead(ctx, tx, head); err != nil {
		return 0, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Raise(ctx, "memory.index", tx.Scope().OwnerID, tx.Scope().Ref(record.MemoryID, record.Revision), now); err != nil {
		return 0, err
	}
	return head.ChangeHead, nil
}

func (s *Service) registerMemoryRefs(ctx context.Context, tx runtime.Tx, auth runtime.Auth, record MemoryRecord) error {
	refs := append([]api.ContentRef{record.Values.ContentRef, record.Values.ScopeRef}, sourceRefs(record.Values.Sources)...)
	seen := map[string]bool{}
	for _, ref := range refs {
		key := contentKey(ref)
		if seen[key] {
			continue
		}
		seen[key] = true
		var v ContentVersion
		_, err := tx.Get(ctx, "content.versions", key, &v)
		if err != nil {
			return err
		}
		copyID := semanticID("copy", record.MemoryID+":"+fmt.Sprint(record.Revision)+":"+key)
		_, err = s.RegisterCopyTx(ctx, tx, auth, RegisterCopyInput{CopyID: copyID, ContentRef: ref, HolderRef: tx.Scope().Ref(record.MemoryID, record.Revision), Purpose: "memory.save", Location: s.Location, RetainUntil: v.RetentionUntil, ReferenceIntentRef: tx.Scope().Ref(record.MemoryID, record.Revision)})
		if err != nil {
			return err
		}
		edgeID := semanticID("medge", record.MemoryID+":"+fmt.Sprint(record.Revision)+":"+key)
		var holder CopyHolder
		holderRevision, err := tx.Get(ctx, "content.holders", copyID, &holder)
		if err != nil {
			return err
		}
		holder.Kind = "metadata_reference"
		holder.Revision = holderRevision + 1
		if err = tx.Put(ctx, "content.holders", copyID, holderRevision, holder); err != nil {
			return err
		}
		if err = tx.Create(ctx, "memory.source_edges", edgeID, key, MemorySourceEdge{MemoryRef: tx.Scope().Ref(record.MemoryID, record.Revision), SourceRef: ref, CopyID: copyID}); err != nil {
			return err
		}
		if err = tx.Create(ctx, "memory.holder_edges", edgeID, record.MemoryID, MemorySourceEdge{MemoryRef: tx.Scope().Ref(record.MemoryID, record.Revision), SourceRef: ref, CopyID: copyID}); err != nil {
			return err
		}
	}
	return nil
}

type MemorySourceEdge struct {
	MemoryRef api.ObjectRef  `json:"memory_ref"`
	SourceRef api.ContentRef `json:"source_ref"`
	CopyID    string         `json:"copy_id"`
}

func (s *Service) CreateTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in CreateInput) (MemoryOutput, error) {
	if !api.ValidID(in.MemoryID) {
		return MemoryOutput{}, api.E("invalid_request", "invalid_memory_identity")
	}
	head, err := loadHead(ctx, tx)
	if err != nil {
		return MemoryOutput{}, err
	}
	if err = s.validateValues(ctx, tx, auth, in.Values); err != nil {
		return MemoryOutput{}, err
	}
	var old MemoryRecord
	_, err = tx.Get(ctx, "memory.records", in.MemoryID, &old)
	if err == nil {
		return MemoryOutput{}, api.E("idempotency_conflict", "memory_identity_exists")
	}
	if !api.IsCode(err, "not_found") {
		return MemoryOutput{}, err
	}
	var candidate ExtractionCandidate
	var candidateRevision uint64
	if in.CandidateRef != nil {
		if err = runtime.CheckRef(tx.Scope(), *in.CandidateRef); err != nil {
			return MemoryOutput{}, err
		}
		if in.CandidateRef.OwnerID != tx.Scope().OwnerID {
			return MemoryOutput{}, api.E("dependency_unavailable", "candidate_authority_unavailable")
		}
		candidateRevision, err = tx.Get(ctx, "memory.candidates", in.CandidateRef.ObjectID, &candidate)
		if err != nil {
			return MemoryOutput{}, err
		}
		if candidateRevision != in.CandidateRef.Revision || !api.Equal(candidate.Values, in.Values) {
			return MemoryOutput{}, api.E("revision_conflict", "candidate_changed")
		}
		if candidate.State == "saved" {
			return MemoryOutput{}, api.E("idempotency_conflict", "candidate_already_saved")
		}
		if candidate.State != "pending" {
			return MemoryOutput{}, api.E("invalid_state", "candidate_rejected")
		}
		if _, err = future(ctx, tx, candidate.ExpiresAt); err != nil {
			return MemoryOutput{}, err
		}
		var extraction Extraction
		_, err = tx.Get(ctx, "memory.extractions", candidate.ExtractionID, &extraction)
		if err != nil {
			return MemoryOutput{}, err
		}
		if extraction.PrincipalID != auth.SubjectID {
			return MemoryOutput{}, api.E("forbidden", "candidate_saving_closed")
		}
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return MemoryOutput{}, err
	}
	record := MemoryRecord{MemoryID: in.MemoryID, Revision: 1, Values: in.Values, State: "active", RecordedAt: api.Time(now), CreatorID: auth.SubjectID, CleanupState: "pending"}
	if err = tx.Create(ctx, "memory.records", in.MemoryID, "", record); err != nil {
		return MemoryOutput{}, err
	}
	if in.CandidateRef != nil {
		digest, _ := api.Digest(in.Values)
		if err = tx.Bind(ctx, "memory.records", "candidate:"+in.CandidateRef.ObjectID, in.MemoryID, digest); err != nil {
			return MemoryOutput{}, err
		}
		ref := tx.Scope().Ref(in.MemoryID, 1)
		candidate.State = "saved"
		candidate.MemoryRef = &ref
		candidate.Revision = candidateRevision + 1
		if err = tx.Put(ctx, "memory.candidates", candidate.CandidateID, candidateRevision, candidate); err != nil {
			return MemoryOutput{}, err
		}
	}
	if err = s.registerMemoryRefs(ctx, tx, auth, record); err != nil {
		return MemoryOutput{}, err
	}
	seq, err := appendChange(ctx, tx, head, record, "upsert", false)
	return MemoryOutput{tx.Scope().Ref(in.MemoryID, 1), record.State, seq}, err
}

func managing(auth runtime.Auth, record MemoryRecord) bool {
	return record.CreatorID == auth.SubjectID || auth.HasRole("memory_admin")
}

func (s *Service) replace(ctx context.Context, tx runtime.Tx, auth runtime.Auth, expected *uint64, in ReplaceInput) (MemoryOutput, error) {
	head, err := loadHead(ctx, tx)
	if err != nil {
		return MemoryOutput{}, err
	}
	var record MemoryRecord
	rev, err := tx.Get(ctx, "memory.records", in.MemoryID, &record)
	if err != nil {
		return MemoryOutput{}, err
	}
	if err = checkAuth(tx.Scope(), auth); err != nil {
		return MemoryOutput{}, err
	}
	if !managing(auth, record) {
		return MemoryOutput{}, api.E("forbidden", "memory_management_required")
	}
	if record.State == "deleted" {
		return MemoryOutput{}, api.E("gone", "memory_deleted")
	}
	if err = compareExpected(expected, rev); err != nil {
		return MemoryOutput{}, err
	}
	if err = s.validateValues(ctx, tx, auth, in.Values); err != nil {
		return MemoryOutput{}, err
	}
	oldContent := record.Values.ContentRef
	record.Values = in.Values
	record.Revision = rev + 1
	now, err := tx.Now(ctx)
	if err != nil {
		return MemoryOutput{}, err
	}
	record.RecordedAt = api.Time(now)
	if record.State == "needs_review" {
		record.State = "active"
		record.ReviewReason = ""
	}
	// disabled 不由纠正隐式启用。
	if err = tx.Put(ctx, "memory.records", in.MemoryID, rev, record); err != nil {
		return MemoryOutput{}, err
	}
	if err = sourceGate(ctx, tx, record, oldContent, "corrected", record.Values.PolicyRef); err != nil {
		return MemoryOutput{}, err
	}
	if err = s.registerMemoryRefs(ctx, tx, auth, record); err != nil {
		return MemoryOutput{}, err
	}
	seq, err := appendChange(ctx, tx, head, record, "upsert", true)
	if err != nil {
		return MemoryOutput{}, err
	}
	if _, err = tx.Raise(ctx, "memory.correction_impact", contentKey(oldContent), tx.Scope().Ref(record.MemoryID, record.Revision), now); err != nil {
		return MemoryOutput{}, err
	}
	if _, err = tx.Raise(ctx, "memory.cleanup", in.MemoryID, tx.Scope().Ref(in.MemoryID, record.Revision), now); err != nil {
		return MemoryOutput{}, err
	}
	return MemoryOutput{tx.Scope().Ref(in.MemoryID, record.Revision), record.State, seq}, nil
}

func (s *Service) restrict(ctx context.Context, tx runtime.Tx, auth runtime.Auth, expected *uint64, in RestrictInput) (MemoryOutput, error) {
	head, err := loadHead(ctx, tx)
	if err != nil {
		return MemoryOutput{}, err
	}
	var record MemoryRecord
	rev, err := tx.Get(ctx, "memory.records", in.MemoryID, &record)
	if err != nil {
		return MemoryOutput{}, err
	}
	if err = checkAuth(tx.Scope(), auth); err != nil {
		return MemoryOutput{}, err
	}
	if !managing(auth, record) {
		return MemoryOutput{}, api.E("forbidden", "memory_management_required")
	}
	if record.State == "deleted" {
		return MemoryOutput{}, api.E("gone", "memory_deleted")
	}
	if err = compareExpected(expected, rev); err != nil {
		return MemoryOutput{}, err
	}
	previous, err := s.policy(ctx, tx, record.Values.PolicyRef)
	if err != nil {
		return MemoryOutput{}, err
	}
	next, err := s.policy(ctx, tx, in.RestrictedPolicyRef)
	if err != nil {
		return MemoryOutput{}, err
	}
	if !narrower(next.Values, previous.Values) {
		return MemoryOutput{}, api.E("forbidden", "scope_expansion")
	}
	record.Values.PolicyRef = in.RestrictedPolicyRef
	record.Revision = rev + 1
	if err = tx.Put(ctx, "memory.records", in.MemoryID, rev, record); err != nil {
		return MemoryOutput{}, err
	}
	if err = sourceGate(ctx, tx, record, record.Values.ContentRef, "restricted", in.RestrictedPolicyRef); err != nil {
		return MemoryOutput{}, err
	}
	seq, err := appendChange(ctx, tx, head, record, "restrict", true)
	if err != nil {
		return MemoryOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return MemoryOutput{}, err
	}
	if _, err = tx.Raise(ctx, "memory.restrict_impact", contentKey(record.Values.ContentRef), tx.Scope().Ref(record.MemoryID, record.Revision), now); err != nil {
		return MemoryOutput{}, err
	}
	return MemoryOutput{tx.Scope().Ref(in.MemoryID, record.Revision), record.State, seq}, nil
}

func (s *Service) delete(ctx context.Context, tx runtime.Tx, auth runtime.Auth, expected *uint64, in DeleteInput) (MemoryOutput, error) {
	head, err := loadHead(ctx, tx)
	if err != nil {
		return MemoryOutput{}, err
	}
	var record MemoryRecord
	rev, err := tx.Get(ctx, "memory.records", in.MemoryID, &record)
	if err != nil {
		return MemoryOutput{}, err
	}
	if err = checkAuth(tx.Scope(), auth); err != nil {
		return MemoryOutput{}, err
	}
	if !managing(auth, record) {
		return MemoryOutput{}, api.E("forbidden", "memory_management_required")
	}
	if err = compareExpected(expected, rev); err != nil {
		return MemoryOutput{}, err
	}
	if record.State == "deleted" {
		return MemoryOutput{tx.Scope().Ref(in.MemoryID, rev), "deleted", head.ChangeHead}, nil
	}
	record.State = "deleted"
	record.Revision = rev + 1
	record.CleanupState = "pending"
	if err = tx.Put(ctx, "memory.records", in.MemoryID, rev, record); err != nil {
		return MemoryOutput{}, err
	}
	if err = sourceGate(ctx, tx, record, record.Values.ContentRef, "deleted", record.Values.PolicyRef); err != nil {
		return MemoryOutput{}, err
	}
	seq, err := appendChange(ctx, tx, head, record, "delete", true)
	if err != nil {
		return MemoryOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return MemoryOutput{}, err
	}
	if _, err = tx.Raise(ctx, "memory.cleanup", in.MemoryID, tx.Scope().Ref(in.MemoryID, record.Revision), now); err != nil {
		return MemoryOutput{}, err
	}
	if _, err = tx.Raise(ctx, "memory.source_impact", contentKey(record.Values.ContentRef), tx.Scope().Ref(record.MemoryID, record.Revision), now); err != nil {
		return MemoryOutput{}, err
	}
	return MemoryOutput{tx.Scope().Ref(in.MemoryID, record.Revision), "deleted", seq}, nil
}

func (s *Service) ReadMemory(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in ReadMemoryInput) (MemoryRecord, error) {
	var out MemoryRecord
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		var current MemoryRecord
		_, err := tx.Get(ctx, "memory.records", in.MemoryID, &current)
		if err != nil {
			return err
		}
		if err = s.memoryAllowed(ctx, tx, auth, current, in.Purpose, false); err != nil {
			return err
		}
		if in.Revision != 0 && in.Revision != current.Revision {
			return api.E("gone", "memory_revision_superseded")
		}
		out = current
		return nil
	})
	return out, err
}

func (s *Service) InspectMemory(ctx context.Context, scope runtime.Scope, auth runtime.Auth, id string) (MemoryRecord, error) {
	var out MemoryRecord
	err := s.authWithin(ctx, scope, auth, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		_, err := tx.Get(ctx, "memory.records", id, &out)
		if err != nil {
			return err
		}
		if !managing(auth, out) || !auth.HasRole("memory_admin") {
			return api.E("forbidden", "memory_management_required")
		}
		return nil
	})
	return out, err
}

func (s *Service) registerMemory(registry *runtime.Registry) {
	command(s, registry, "memory.create", "memory", false, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in CreateInput) (MemoryOutput, error) {
		if c.TargetID != in.MemoryID {
			return MemoryOutput{}, api.E("invalid_request", "target_mismatch")
		}
		return s.CreateTx(ctx, tx, auth, in)
	})
	command(s, registry, "memory.replace", "memory", true, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ReplaceInput) (MemoryOutput, error) {
		if c.TargetID != in.MemoryID {
			return MemoryOutput{}, api.E("invalid_request", "target_mismatch")
		}
		return s.replace(ctx, tx, auth, c.ExpectedRevision, in)
	})
	command(s, registry, "memory.restrict", "memory", true, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in RestrictInput) (MemoryOutput, error) {
		if c.TargetID != in.MemoryID {
			return MemoryOutput{}, api.E("invalid_request", "target_mismatch")
		}
		return s.restrict(ctx, tx, auth, c.ExpectedRevision, in)
	})
	command(s, registry, "memory.delete", "memory", true, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in DeleteInput) (MemoryOutput, error) {
		if c.TargetID != in.MemoryID {
			return MemoryOutput{}, api.E("invalid_request", "target_mismatch")
		}
		return s.delete(ctx, tx, auth, c.ExpectedRevision, in)
	})
	query(s, registry, "memory.read", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in ReadMemoryInput) (MemoryRecord, error) {
		if q.TargetID != in.MemoryID {
			return MemoryRecord{}, api.E("invalid_request", "target_mismatch")
		}
		return s.ReadMemory(ctx, scope, auth, in)
	})
	query(s, registry, "memory.inspect", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in ReadMemoryInput) (MemoryRecord, error) {
		if q.TargetID != in.MemoryID {
			return MemoryRecord{}, api.E("invalid_request", "target_mismatch")
		}
		return s.InspectMemory(ctx, scope, auth, in.MemoryID)
	})
	query(s, registry, "memory.cleanup.get", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in ReadMemoryInput) (MemoryRecord, error) {
		if q.TargetID != in.MemoryID {
			return MemoryRecord{}, api.E("invalid_request", "target_mismatch")
		}
		// 同管理视图只恢复当前清理元数据；不会读已撤正文或触发新的清理。
		return s.InspectMemory(ctx, scope, auth, in.MemoryID)
	})
}
