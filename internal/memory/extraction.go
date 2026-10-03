package memory

import (
	"context"
	"fmt"
	"strings"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// SavingAuthorization 在同库核验有限预授权；普通候选保存仍由本人原命令决定。
type SavingAuthorization interface {
	AuthorizeSaving(context.Context, runtime.Tx, runtime.Auth, api.ObjectRef, []api.ContentRef, ExtractionLimits) error
}

type ExtractInput struct {
	ExtractionID   string           `json:"extraction_id"`
	InputRefs      []api.ContentRef `json:"input_refs"`
	ExtractorRef   api.ComponentRef `json:"extractor_ref"`
	Limits         ExtractionLimits `json:"limits"`
	Deadline       string           `json:"deadline"`
	SavingMode     string           `json:"saving_mode"`
	SavingGrantRef *api.ObjectRef   `json:"saving_grant_ref,omitempty"`
}
type ExtractionOutput struct {
	ExtractionRef api.ObjectRef `json:"extraction_ref"`
	State         string        `json:"state"`
}
type CancelExtractionInput struct {
	ExtractionID string `json:"extraction_id"`
	Reason       string `json:"reason"`
}
type ReadExtractionInput struct {
	ExtractionID string `json:"extraction_id"`
}
type ReadCandidateInput struct {
	CandidateID string `json:"candidate_id"`
}
type CandidateReplaceInput struct {
	CandidateID string       `json:"candidate_id"`
	Values      MemoryValues `json:"values"`
	ExpiresAt   string       `json:"expires_at"`
}
type CandidateRejectInput struct {
	CandidateID string `json:"candidate_id"`
	Reason      string `json:"reason"`
}
type CandidateOutput struct {
	CandidateRef api.ObjectRef `json:"candidate_ref"`
	State        string        `json:"state"`
}

type ListCandidatesInput struct {
	ExtractionID string `json:"extraction_id"`
	Limit        uint64 `json:"limit,omitempty"`
	Cursor       string `json:"cursor,omitempty"`
}

func (s *Service) ListCandidates(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in ListCandidatesInput) (api.Page[ExtractionCandidate], error) {
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit > 20 {
		return api.Page[ExtractionCandidate]{}, api.E("invalid_request", "invalid_page_limit")
	}
	var out api.Page[ExtractionCandidate]
	err := s.within(ctx, scope, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		var extraction Extraction
		_, err := tx.Get(ctx, "memory.extractions", in.ExtractionID, &extraction)
		if err != nil {
			return err
		}
		if extraction.PrincipalID != auth.SubjectID {
			return api.E("forbidden", "extraction_principal_mismatch")
		}
		token, err := s.visibility(ctx, tx, auth)
		if err != nil {
			return err
		}
		digest, err := api.Digest([]any{token, in.ExtractionID, extraction.Revision, auth.SubjectID})
		if err != nil {
			return err
		}
		last := ""
		if in.Cursor != "" {
			parts := strings.Split(in.Cursor, ":")
			if len(parts) != 2 || parts[0] != strings.TrimPrefix(digest, "sha256:") {
				return api.E("snapshot_required", "candidate_scope_changed")
			}
			last = parts[1]
		}
		rows, err := tx.List(ctx, "memory.candidates", in.ExtractionID, last, 200)
		if err != nil {
			return err
		}
		out = api.Page[ExtractionCandidate]{Items: []ExtractionCandidate{}, CollectionRevision: extraction.Revision, Gaps: []string{}, Partial: len(rows) == 200}
		visited := 0
		for _, row := range rows {
			visited++
			last = row.ID
			var candidate ExtractionCandidate
			if err = row.Decode(&candidate); err != nil {
				return err
			}
			if err = s.validateValues(ctx, tx, auth, candidate.Values); err != nil {
				if api.IsCode(err, "forbidden") || api.IsCode(err, "gone") {
					continue
				}
				if isUnavailable(err) {
					out.Partial = true
					out.Gaps = unique(append(out.Gaps, "source_unavailable"))
					continue
				}
				return err
			}
			out.Items = append(out.Items, candidate)
			if len(out.Items) == int(in.Limit) {
				break
			}
		}
		out.Exhausted = visited == len(rows) && len(rows) < 200
		if !out.Exhausted {
			out.NextCursor = strings.TrimPrefix(digest, "sha256:") + ":" + last
		}
		return nil
	})
	return out, err
}

type ExtractionStatement struct {
	Values    MemoryValues `json:"values"`
	ExpiresAt string       `json:"expires_at"`
}
type ExtractionDocument struct {
	Statements    []ExtractionStatement `json:"statements"`
	CheckpointRef *api.ContentRef       `json:"checkpoint_ref,omitempty"`
}

func RuleExtractor() api.ComponentRef {
	return api.ComponentRef{ComponentID: "extractor_00000000000000000000000000000001", Version: "explicit-values1", Digest: api.Hash([]byte("bounded-authorized-memory-values-no-model-no-implicit-save/v1"))}
}

func (s *Service) extract(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ExtractInput) (ExtractionOutput, error) {
	if c.TargetID != in.ExtractionID || !api.ValidID(in.ExtractionID) || len(in.InputRefs) == 0 || in.Limits.MaxCandidates < 1 || in.Limits.MaxCandidates > 100 || in.Limits.MaxInputBytes < 1 || in.Limits.MaxInputBytes > MaxContentBytes || in.Limits.MaxTokens > api.MaxSafeInteger {
		return ExtractionOutput{}, api.E("invalid_request", "invalid_extraction_limits")
	}
	if err := validateSources(tx.Scope(), in.InputRefs); err != nil {
		return ExtractionOutput{}, err
	}
	if !api.Equal(in.ExtractorRef, RuleExtractor()) {
		return ExtractionOutput{}, api.E("unsupported", "extractor_not_installed")
	}
	if err := api.ValidateAmounts([]api.Amount{in.Limits.MaxCost}); err != nil {
		return ExtractionOutput{}, api.E("invalid_request", "invalid_extraction_cost")
	}
	if _, err := future(ctx, tx, in.Deadline); err != nil {
		return ExtractionOutput{}, err
	}
	bytes := uint64(0)
	for _, ref := range in.InputRefs {
		bytes += ref.ByteLength
		if bytes > in.Limits.MaxInputBytes {
			return ExtractionOutput{}, api.E("invalid_request", "input_byte_budget_exceeded")
		}
		if _, err := s.CheckContentTx(ctx, tx, auth, ref, "memory.extract", s.Location, false); err != nil {
			return ExtractionOutput{}, err
		}
	}
	if in.SavingMode != "review_only" && in.SavingMode != "preapproved" {
		return ExtractionOutput{}, api.E("invalid_request", "invalid_saving_mode")
	}
	if in.SavingMode == "preapproved" {
		if in.SavingGrantRef == nil || s.SavingAuthorization == nil {
			return ExtractionOutput{}, api.E("unsupported", "saving_grant_not_configured")
		}
		if err := runtime.CheckRef(tx.Scope(), *in.SavingGrantRef); err != nil {
			return ExtractionOutput{}, err
		}
		if err := s.SavingAuthorization.AuthorizeSaving(ctx, tx, auth, *in.SavingGrantRef, in.InputRefs, in.Limits); err != nil {
			return ExtractionOutput{}, err
		}
	}
	x := Extraction{ExtractionID: in.ExtractionID, Revision: 1, InputRefs: in.InputRefs, ExtractorRef: in.ExtractorRef, Limits: in.Limits, Deadline: in.Deadline, SavingMode: in.SavingMode, SavingGrantRef: in.SavingGrantRef, State: "active", PrincipalID: auth.SubjectID, PrincipalGeneration: auth.CredentialGeneration}
	if err := tx.Create(ctx, "memory.extractions", in.ExtractionID, auth.SubjectID, x); err != nil {
		return ExtractionOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ExtractionOutput{}, err
	}
	if _, err = tx.Raise(ctx, "memory.extract", in.ExtractionID, tx.Scope().Ref(in.ExtractionID, 1), now); err != nil {
		return ExtractionOutput{}, err
	}
	return ExtractionOutput{tx.Scope().Ref(in.ExtractionID, 1), "active"}, nil
}

func (s *Service) extractionJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var extraction Extraction
	_, err := store.Read(ctx, scope, "memory.extractions", work.Job.SourceRef.ObjectID, 0, &extraction)
	if err != nil {
		return err
	}
	if extraction.State != "active" {
		return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), nil)
	}
	if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: extraction.PrincipalID, CredentialGeneration: extraction.PrincipalGeneration}
	statements := []ExtractionStatement{}
	var checkpoint *api.ContentRef
	for _, ref := range extraction.InputRefs {
		body, err := s.Read(ctx, scope, auth, ref, "memory.extract")
		if err != nil {
			return err
		}
		validator, err := api.NewValidator(api.SchemaFor[ExtractionDocument]())
		if err != nil {
			return err
		}
		if err = validator.Validate(body); err != nil {
			return s.failExtraction(ctx, store, scope, work, extraction, err)
		}
		var doc ExtractionDocument
		if err = api.Decode(body, &doc); err != nil {
			return s.failExtraction(ctx, store, scope, work, extraction, err)
		}
		statements = append(statements, doc.Statements...)
		if uint64(len(statements)) > extraction.Limits.MaxCandidates {
			return s.failExtraction(ctx, store, scope, work, extraction, api.E("invalid_request", "candidate_budget_exceeded"))
		}
		if doc.CheckpointRef != nil {
			checkpoint = doc.CheckpointRef
		}
	}
	return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := loadHead(ctx, tx); err != nil {
			return err
		}
		var current Extraction
		rev, err := tx.Get(ctx, "memory.extractions", extraction.ExtractionID, &current)
		if err != nil {
			return err
		}
		if current.State != "active" {
			return nil
		}
		if _, err = future(ctx, tx, current.Deadline); err != nil {
			current.State = "cancelled"
			current.Revision = rev + 1
			return tx.Put(ctx, "memory.extractions", current.ExtractionID, rev, current)
		}
		if current.SavingMode == "preapproved" {
			if current.SavingGrantRef == nil || s.SavingAuthorization == nil {
				return api.E("forbidden", "saving_grant_unavailable")
			}
			if err = s.SavingAuthorization.AuthorizeSaving(ctx, tx, auth, *current.SavingGrantRef, current.InputRefs, current.Limits); err != nil {
				return err
			}
		}
		for i, statement := range statements {
			if err = s.validateValues(ctx, tx, auth, statement.Values); err != nil {
				return err
			}
			if _, err = future(ctx, tx, statement.ExpiresAt); err != nil {
				return err
			}
			id := semanticID("candidate", current.ExtractionID+":"+fmt.Sprint(i))
			candidate := ExtractionCandidate{CandidateID: id, Revision: 1, ExtractionID: current.ExtractionID, Values: statement.Values, ExpiresAt: statement.ExpiresAt, State: "pending"}
			var existing ExtractionCandidate
			_, err = tx.Get(ctx, "memory.candidates", id, &existing)
			if api.IsCode(err, "not_found") {
				if err = tx.Create(ctx, "memory.candidates", id, current.ExtractionID, candidate); err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if !api.Equal(existing.Values, statement.Values) {
				return api.E("idempotency_conflict", "candidate_input_changed")
			}
			if current.SavingMode == "preapproved" && existing.State != "saved" {
				ref := scope.Ref(id, 1)
				memoryID := semanticID("memory", current.ExtractionID+":"+fmt.Sprint(i))
				if _, err = s.CreateTx(ctx, tx, auth, CreateInput{MemoryID: memoryID, Values: statement.Values, CandidateRef: &ref}); err != nil {
					return err
				}
			}
		}
		if checkpoint != nil {
			if _, err = s.CheckContentTx(ctx, tx, auth, *checkpoint, "memory.extract", s.Location, false); err != nil {
				return err
			}
		}
		current.State = "closed"
		current.CandidateCount = uint64(len(statements))
		current.CheckpointRef = checkpoint
		current.Revision = rev + 1
		return tx.Put(ctx, "memory.extractions", current.ExtractionID, rev, current)
	})
}

func (s *Service) failExtraction(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, extraction Extraction, cause error) error {
	return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		var current Extraction
		rev, err := tx.Get(ctx, "memory.extractions", extraction.ExtractionID, &current)
		if err != nil {
			return err
		}
		current.State = "cancelled"
		current.FailureReason = cause.Error()
		current.Revision = rev + 1
		return tx.Put(ctx, "memory.extractions", current.ExtractionID, rev, current)
	})
}

func (s *Service) registerExtraction(registry *runtime.Registry) {
	query(s, registry, "memory.candidate.list", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in ListCandidatesInput) (api.Page[ExtractionCandidate], error) {
		if q.TargetID != in.ExtractionID {
			return api.Page[ExtractionCandidate]{}, api.E("invalid_request", "target_mismatch")
		}
		return s.ListCandidates(ctx, scope, auth, in)
	})
	command(s, registry, "memory.extract", "memory", false, s.extract)
	command(s, registry, "memory.extract.cancel", "memory", true, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in CancelExtractionInput) (ExtractionOutput, error) {
		var x Extraction
		rev, err := tx.Get(ctx, "memory.extractions", in.ExtractionID, &x)
		if err != nil {
			return ExtractionOutput{}, err
		}
		if c.TargetID != in.ExtractionID || x.PrincipalID != auth.SubjectID {
			return ExtractionOutput{}, api.E("forbidden", "extraction_principal_mismatch")
		}
		if err = compareExpected(c.ExpectedRevision, rev); err != nil {
			return ExtractionOutput{}, err
		}
		if x.State == "active" {
			x.State = "cancelled"
			x.Revision = rev + 1
			if err = tx.Put(ctx, "memory.extractions", in.ExtractionID, rev, x); err != nil {
				return ExtractionOutput{}, err
			}
		}
		return ExtractionOutput{tx.Scope().Ref(in.ExtractionID, x.Revision), x.State}, nil
	})
	query(s, registry, "memory.extract.read", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in ReadExtractionInput) (Extraction, error) {
		var out Extraction
		err := s.within(ctx, scope, func(tx runtime.Tx) error {
			_, err := tx.Get(ctx, "memory.extractions", in.ExtractionID, &out)
			if err != nil {
				return err
			}
			if q.TargetID != in.ExtractionID || out.PrincipalID != auth.SubjectID {
				return api.E("forbidden", "extraction_principal_mismatch")
			}
			return nil
		})
		return out, err
	})
	query(s, registry, "memory.candidate.read", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in ReadCandidateInput) (ExtractionCandidate, error) {
		var out ExtractionCandidate
		err := s.within(ctx, scope, func(tx runtime.Tx) error {
			_, err := tx.Get(ctx, "memory.candidates", in.CandidateID, &out)
			if err != nil {
				return err
			}
			var x Extraction
			_, err = tx.Get(ctx, "memory.extractions", out.ExtractionID, &x)
			if err != nil {
				return err
			}
			if q.TargetID != in.CandidateID || x.PrincipalID != auth.SubjectID {
				return api.E("forbidden", "candidate_principal_mismatch")
			}
			if out.State == "pending" {
				return s.validateValues(ctx, tx, auth, out.Values)
			}
			return nil
		})
		return out, err
	})
	command(s, registry, "memory.candidate.replace", "memory", true, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in CandidateReplaceInput) (CandidateOutput, error) {
		var candidate ExtractionCandidate
		rev, err := tx.Get(ctx, "memory.candidates", in.CandidateID, &candidate)
		if err != nil {
			return CandidateOutput{}, err
		}
		var x Extraction
		_, err = tx.Get(ctx, "memory.extractions", candidate.ExtractionID, &x)
		if err != nil {
			return CandidateOutput{}, err
		}
		if c.TargetID != in.CandidateID || x.PrincipalID != auth.SubjectID {
			return CandidateOutput{}, api.E("forbidden", "candidate_principal_mismatch")
		}
		if err = compareExpected(c.ExpectedRevision, rev); err != nil {
			return CandidateOutput{}, err
		}
		if candidate.State != "pending" {
			return CandidateOutput{}, api.E("invalid_state", "candidate_already_decided")
		}
		if err = s.validateValues(ctx, tx, auth, in.Values); err != nil {
			return CandidateOutput{}, err
		}
		if _, err = future(ctx, tx, in.ExpiresAt); err != nil {
			return CandidateOutput{}, err
		}
		candidate.Values = in.Values
		candidate.ExpiresAt = in.ExpiresAt
		candidate.Revision = rev + 1
		if err = tx.Put(ctx, "memory.candidates", in.CandidateID, rev, candidate); err != nil {
			return CandidateOutput{}, err
		}
		return CandidateOutput{tx.Scope().Ref(in.CandidateID, candidate.Revision), candidate.State}, nil
	})
	command(s, registry, "memory.candidate.reject", "memory", true, func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in CandidateRejectInput) (CandidateOutput, error) {
		var candidate ExtractionCandidate
		rev, err := tx.Get(ctx, "memory.candidates", in.CandidateID, &candidate)
		if err != nil {
			return CandidateOutput{}, err
		}
		var x Extraction
		_, err = tx.Get(ctx, "memory.extractions", candidate.ExtractionID, &x)
		if err != nil {
			return CandidateOutput{}, err
		}
		if c.TargetID != in.CandidateID || x.PrincipalID != auth.SubjectID {
			return CandidateOutput{}, api.E("forbidden", "candidate_principal_mismatch")
		}
		if err = compareExpected(c.ExpectedRevision, rev); err != nil {
			return CandidateOutput{}, err
		}
		if candidate.State != "pending" {
			return CandidateOutput{}, api.E("invalid_state", "candidate_already_decided")
		}
		candidate.State = "rejected"
		candidate.Revision = rev + 1
		if err = tx.Put(ctx, "memory.candidates", in.CandidateID, rev, candidate); err != nil {
			return CandidateOutput{}, err
		}
		return CandidateOutput{tx.Scope().Ref(in.CandidateID, candidate.Revision), "rejected"}, nil
	})
}
