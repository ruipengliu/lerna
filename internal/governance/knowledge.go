package governance

import (
	"context"
	"errors"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const KnowledgeValidationJob = "governance.knowledge_validate"

const (
	maxSkillBodyBytes  = 64 << 10
	maxSkillUsageBytes = 16 << 10
)

type knowledgeValidation struct {
	ID       string       `json:"id"`
	Revision uint64       `json:"revision"`
	RecordID string       `json:"record_id"`
	Kind     string       `json:"kind"`
	Auth     runtime.Auth `json:"auth"`
}

func (s *Service) registerKnowledge(r *runtime.Registry) error {
	if err := registerCommand[SkillDefinition, StateOutput](s, r, "skill.register", false, true, s.registerSkill); err != nil {
		return err
	}
	for _, register := range []func() error{
		func() error { return registerQuery[SkillReference, SkillRecord](r, "skill.get", s.readSkill) },
		func() error { return registerQuery[SkillReference, LoadedSkill](r, "skill.load", s.loadSkill) },
		func() error {
			return registerCommand[KnowledgeChange, StateOutput](s, r, "skill.withdraw", true, false, s.withdrawSkill)
		},
		func() error {
			return registerCommand[KnowledgeChange, StateOutput](s, r, "skill.reopen", true, true, s.reopenSkill)
		},
		func() error {
			return registerCommand[AgentConfigDefinition, StateOutput](s, r, "agent_config.register", false, true, s.registerAgentConfig)
		},
		func() error {
			return registerQuery[AgentConfigReference, AgentConfigRecord](r, "agent_config.get", s.readAgentConfig)
		},
		func() error {
			return registerCommand[KnowledgeChange, StateOutput](s, r, "agent_config.withdraw", true, false, s.withdrawAgentConfig)
		},
		func() error {
			return registerCommand[KnowledgeChange, StateOutput](s, r, "agent_config.reopen", true, true, s.reopenAgentConfig)
		},
		func() error {
			return registerQuery[KnowledgeRequest, KnowledgeBundle](r, "knowledge.load", func(ctx context.Context, _ runtime.Store, scope runtime.Scope, auth runtime.Auth, _ api.Query, in KnowledgeRequest) (KnowledgeBundle, error) {
				return s.LoadKnowledge(ctx, scope, auth, in)
			})
		},
		func() error {
			return registerQuery[KnowledgeSelectionReference, KnowledgeCommit](r, "knowledge.selection.get", s.readKnowledgeSelection)
		},
	} {
		if err := register(); err != nil {
			return err
		}
	}
	return nil
}
func (s *Service) loadSkill(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query, in SkillReference) (LoadedSkill, error) {
	record, err := s.readSkill(ctx, store, scope, auth, q, in)
	if err != nil {
		return LoadedSkill{}, err
	}
	if record.State != "active" || record.Usage == nil {
		return LoadedSkill{}, api.E("invalid_state", "skill_not_active")
	}
	body, err := s.knowledgeBytes(ctx, scope, auth, record.Definition.BodyRef, "skill.load", maxSkillBodyBytes)
	if err != nil {
		return LoadedSkill{}, err
	}
	current, err := s.readSkill(ctx, store, scope, auth, q, in)
	if err != nil {
		return LoadedSkill{}, err
	}
	if current.State != "active" || current.Revision != record.Revision {
		return LoadedSkill{}, api.E("revision_conflict", "skill_changed_during_load")
	}
	out := LoadedSkill{Definition: record.Definition, Body: string(body), Usage: *record.Usage}
	if len(api.Raw(out)) > 128<<10 {
		return LoadedSkill{}, api.E("invalid_request", "knowledge_packet_limit")
	}
	return out, nil
}
func knowledgeChange(scope runtime.Scope, auth runtime.Auth, c api.Command, in KnowledgeChange) error {
	if auth.TenantID != scope.TenantID || !api.ValidID(auth.SubjectID) || auth.CredentialGeneration == 0 {
		return api.E("forbidden", "knowledge_principal_invalid")
	}
	if err := ownerRef(scope, in.Ref); err != nil {
		return err
	}
	if c.TargetID != in.Ref.ObjectID || in.Reason == "" || len(in.Reason) > 1024 {
		return api.E("invalid_request", "knowledge_change_invalid")
	}
	return nil
}
func knowledgeAuthor(auth runtime.Auth, author api.ObjectRef) error {
	if auth.TenantID != author.TenantID || (auth.SubjectID != author.ObjectID && !auth.HasRole("maintainer")) {
		return api.E("forbidden", "knowledge_author_required")
	}
	return nil
}
func (s *Service) withdrawSkill(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in KnowledgeChange) (runtime.Outcome, error) {
	if err := knowledgeChange(tx.Scope(), auth, c, in); err != nil {
		return runtime.Outcome{}, err
	}
	var current SkillRecord
	rev, err := tx.Get(ctx, ns("skills"), in.Ref.ObjectID, &current)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err := knowledgeAuthor(auth, current.AuthorRef); err != nil {
		return runtime.Outcome{}, err
	}
	if err := requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if in.Ref.Revision != rev {
		return runtime.Outcome{}, api.E("revision_conflict", "knowledge_ref_changed")
	}
	if current.State == "validating" {
		return runtime.Outcome{}, api.E("invalid_state", "skill_validation_in_progress")
	}
	current.State, current.FailureReason, current.Revision = "withdrawn", in.Reason, rev+1
	if err := tx.Put(ctx, ns("skills"), current.ID, rev, current); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Applied(StateOutput{Ref: tx.Scope().Ref(current.ID, current.Revision), State: current.State}), nil
}
func (s *Service) reopenSkill(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in KnowledgeChange) (runtime.Outcome, error) {
	if err := knowledgeChange(tx.Scope(), auth, c, in); err != nil {
		return runtime.Outcome{}, err
	}
	if s.Ports.Content == nil {
		return runtime.Outcome{}, api.E("unsupported", "knowledge_content_unavailable")
	}
	var birth SkillRecord
	if err := tx.GetVersion(ctx, ns("skills"), in.Ref.ObjectID, 1, &birth); err != nil {
		return runtime.Outcome{}, err
	}
	if err := s.checkKnowledgeTx(ctx, tx, auth, skillContents(birth.Definition), "skill.reopen"); err != nil {
		return runtime.Outcome{}, err
	}
	var current SkillRecord
	rev, err := tx.Get(ctx, ns("skills"), in.Ref.ObjectID, &current)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if err := knowledgeAuthor(auth, current.AuthorRef); err != nil {
		return runtime.Outcome{}, err
	}
	if err := requireCAS(c, rev); err != nil {
		return runtime.Outcome{}, err
	}
	if in.Ref.Revision != rev {
		return runtime.Outcome{}, api.E("revision_conflict", "knowledge_ref_changed")
	}
	if current.State != "withdrawn" && current.State != "invalid" {
		return runtime.Outcome{}, api.E("invalid_state", "skill_not_closed")
	}
	current.State, current.Revision, current.Usage, current.FailureReason, current.ValidationCommandID = "validating", rev+1, nil, "", c.CommandID
	if err := tx.Put(ctx, ns("skills"), current.ID, rev, current); err != nil {
		return runtime.Outcome{}, err
	}
	if err := tx.Create(ctx, ns("knowledge_validations"), c.CommandID, current.ID, knowledgeValidation{ID: c.CommandID, Revision: 1, RecordID: current.ID, Kind: "skill", Auth: auth}); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err := tx.Raise(ctx, KnowledgeValidationJob, c.CommandID, tx.Scope().Ref(current.ID, current.Revision), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(StateOutput{Ref: tx.Scope().Ref(current.ID, current.Revision), State: current.State}), nil
}
func validateSkill(scope runtime.Scope, in SkillDefinition) error {
	if err := api.ValidateRecord("ComponentRef", in.SkillRef); err != nil {
		return err
	}
	sealed, err := SealSkill(in)
	if err != nil || !api.Equal(sealed, in) {
		return api.E("invalid_request", "skill_manifest_digest_mismatch")
	}
	if len(in.SourceRefs) > 8 || in.BodyRef.ByteLength == 0 || in.BodyRef.ByteLength > maxSkillBodyBytes || in.UsageContractRef.ByteLength == 0 || in.UsageContractRef.ByteLength > maxSkillUsageBytes {
		return api.E("invalid_request", "skill_content_bounds_invalid")
	}
	seen := map[string]bool{}
	for _, ref := range skillContents(in) {
		if err := api.ValidateRecord("ContentRef", ref); err != nil {
			return err
		}
		if ref.TenantID != scope.TenantID {
			return api.E("forbidden", "knowledge_content_scope_mismatch")
		}
		key := digestID("content", ref)
		if seen[key] {
			return api.E("invalid_request", "duplicate_knowledge_content")
		}
		seen[key] = true
	}
	return nil
}
func skillContents(in SkillDefinition) []api.ContentRef {
	return append([]api.ContentRef{in.BodyRef, in.UsageContractRef}, in.SourceRefs...)
}
func (s *Service) checkKnowledgeTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ContentRef, purpose string) error {
	if s.Ports.KnowledgeGate == nil {
		return api.E("unsupported", "current_knowledge_gate_unavailable")
	}
	if auth.TenantID != tx.Scope().TenantID || !api.ValidID(auth.SubjectID) || auth.CredentialGeneration == 0 {
		return api.E("forbidden", "knowledge_principal_invalid")
	}
	return s.Ports.KnowledgeGate.CheckTx(ctx, tx, auth, refs, purpose)
}
func (s *Service) registerSkill(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SkillDefinition) (runtime.Outcome, error) {
	if s.Ports.Content == nil {
		return runtime.Outcome{}, api.E("unsupported", "knowledge_content_unavailable")
	}
	if err := validateSkill(tx.Scope(), in); err != nil {
		return runtime.Outcome{}, err
	}
	if c.TargetID != in.SkillRef.ComponentID {
		return runtime.Outcome{}, api.E("invalid_request", "skill_target_mismatch")
	}
	if err := s.checkKnowledgeTx(ctx, tx, auth, skillContents(in), "skill.register"); err != nil {
		return runtime.Outcome{}, err
	}
	id := componentKey(in.SkillRef)
	record := SkillRecord{ID: id, Revision: 1, Definition: in, AuthorRef: auth.Ref(tx.Scope().OwnerID), InstallLockRef: SkillInstallLock(in), State: "validating", ValidationCommandID: c.CommandID}
	if err := tx.Create(ctx, ns("skills"), id, auth.SubjectID, record); err != nil {
		return runtime.Outcome{}, err
	}
	// 准确同版本身份只可绑定一次，改变摘要必须使用另一个版本。
	if err := tx.Bind(ctx, ns("skills"), in.SkillRef.ComponentID+"/"+in.SkillRef.Version, id, in.SkillRef.Digest); err != nil {
		return runtime.Outcome{}, err
	}
	validation := knowledgeValidation{ID: c.CommandID, Revision: 1, RecordID: id, Kind: "skill", Auth: auth}
	if err := tx.Create(ctx, ns("knowledge_validations"), c.CommandID, id, validation); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err := tx.Raise(ctx, KnowledgeValidationJob, c.CommandID, tx.Scope().Ref(id, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(StateOutput{Ref: tx.Scope().Ref(id, 1), State: record.State}), nil
}
func (s *Service) readSkill(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, _ api.Query, in SkillReference) (SkillRecord, error) {
	var birth, out SkillRecord
	id := componentKey(in.SkillRef)
	if _, err := store.Read(ctx, scope, ns("skills"), id, 1, &birth); err != nil {
		return out, err
	}
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		if err := s.checkKnowledgeTx(ctx, tx, auth, skillContents(birth.Definition), "skill.read"); err != nil {
			return err
		}
		_, err := tx.Get(ctx, ns("skills"), id, &out)
		return err
	})
	if status == runtime.CommitUnknown {
		return SkillRecord{}, runtime.ErrCommitUnknown
	}
	return out, err
}
func knowledgeRejection(err error) *api.Error {
	var e *api.Error
	if !errors.As(err, &e) || e.Cause != nil {
		return nil
	}
	switch e.Code {
	case "invalid_request", "forbidden", "invalid_state", "not_found", "unsupported":
		return e
	}
	return nil
}
func (s *Service) knowledgeBytes(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref api.ContentRef, purpose string, limit uint64) ([]byte, error) {
	if s.Ports.Content == nil {
		return nil, api.E("unsupported", "knowledge_content_unavailable")
	}
	b, err := s.Ports.Content.Read(ctx, scope, auth, ref, purpose)
	if err != nil {
		return nil, err
	}
	if uint64(len(b)) != ref.ByteLength || uint64(len(b)) > limit || api.Hash(b) != ref.Hash || !utf8.Valid(b) {
		return nil, api.E("invalid_request", "knowledge_exact_bytes_invalid")
	}
	return b, nil
}
func validateSkillUsage(in SkillDefinition, usage SkillUsageContract) error {
	if usage.Purpose == "" || len(usage.Purpose) > 128 || len(usage.Preconditions) == 0 || len(usage.Preconditions) > 16 || len(usage.Counterexamples) == 0 || len(usage.Counterexamples) > 16 || len(usage.ExitRules) == 0 || len(usage.ExitRules) > 16 || len(usage.ToolDependencies) > 16 || len(usage.EvidenceDependencies) > 8 || len(usage.Conflicts) > 16 {
		return api.E("invalid_request", "skill_usage_bounds_invalid")
	}
	for _, lines := range [][]string{usage.Preconditions, usage.Counterexamples, usage.ExitRules} {
		for _, line := range lines {
			if line == "" || len(line) > 1024 {
				return api.E("invalid_request", "skill_usage_statement_invalid")
			}
		}
	}
	for _, refs := range [][]api.ComponentRef{usage.ToolDependencies, usage.Conflicts} {
		for _, ref := range refs {
			if err := api.ValidateRecord("ComponentRef", ref); err != nil {
				return err
			}
		}
	}
	for _, ref := range usage.EvidenceDependencies {
		declared := false
		for _, source := range in.SourceRefs {
			declared = declared || api.Equal(ref, source)
		}
		if !declared {
			return api.E("invalid_request", "skill_evidence_not_in_sources")
		}
	}
	return nil
}
func (s *Service) validateKnowledge(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var validation knowledgeValidation
	if _, err := store.Read(ctx, scope, ns("knowledge_validations"), work.Job.ResponsibilityKey, 1, &validation); err != nil {
		return err
	}
	if validation.Kind == "agent_config" {
		return s.validateAgentConfig(ctx, store, scope, work, validation)
	}
	if validation.Kind != "skill" {
		return api.E("invalid_state", "knowledge_validation_kind_invalid")
	}
	var birth SkillRecord
	if _, err := store.Read(ctx, scope, ns("skills"), validation.RecordID, 1, &birth); err != nil {
		return err
	}
	var usage SkillUsageContract
	status, ioErr := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		return s.checkKnowledgeTx(ctx, tx, validation.Auth, skillContents(birth.Definition), "skill.validate")
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if ioErr == nil {
		_, ioErr = s.knowledgeBytes(ctx, scope, validation.Auth, birth.Definition.BodyRef, "skill.validate", maxSkillBodyBytes)
	}
	if ioErr == nil {
		var bytes []byte
		bytes, ioErr = s.knowledgeBytes(ctx, scope, validation.Auth, birth.Definition.UsageContractRef, "skill.validate", maxSkillUsageBytes)
		if ioErr == nil {
			var validator *api.Validator
			validator, ioErr = api.NewValidator(api.SchemaFor[SkillUsageContract]())
			if ioErr == nil {
				ioErr = validator.Validate(bytes)
			}
		}
		if ioErr == nil {
			ioErr = api.Decode(bytes, &usage)
		}
		if ioErr == nil {
			ioErr = validateSkillUsage(birth.Definition, usage)
		}
	}
	if ioErr != nil && knowledgeRejection(ioErr) == nil {
		return ioErr
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := tx.LoadCommand(ctx, validation.ID); err != nil {
			return err
		}
		if ioErr == nil {
			if err := s.checkKnowledgeTx(ctx, tx, validation.Auth, skillContents(birth.Definition), "skill.validate"); err != nil {
				if knowledgeRejection(err) == nil {
					return err
				}
				ioErr = err
			}
		}
		var current SkillRecord
		rev, err := tx.Get(ctx, ns("skills"), birth.ID, &current)
		if err != nil {
			return err
		}
		if current.State != "validating" || current.ValidationCommandID != validation.ID {
			return nil
		}
		current.Revision = rev + 1
		if ioErr == nil {
			current.State, current.Usage = "active", &usage
			current.FailureReason = ""
		} else {
			current.State, current.FailureReason = "invalid", knowledgeRejection(ioErr).Reason
		}
		if err := tx.Put(ctx, ns("skills"), current.ID, rev, current); err != nil {
			return err
		}
		return runtime.Decide(ctx, tx, validation.ID, StateOutput{Ref: scope.Ref(current.ID, current.Revision), State: current.State}, knowledgeRejection(ioErr))
	})
}
