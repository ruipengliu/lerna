package governance

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func agentContents(in AgentConfigDefinition) []api.ContentRef {
	return append([]api.ContentRef{in.ControlLimitsRef}, in.SourceRefs...)
}
func validateComponentSet(refs []api.ComponentRef, limit int) error {
	if len(refs) > limit {
		return api.E("invalid_request", "knowledge_component_limit")
	}
	seen := map[string]bool{}
	for _, ref := range refs {
		if err := api.ValidateRecord("ComponentRef", ref); err != nil {
			return err
		}
		if seen[componentKey(ref)] {
			return api.E("invalid_request", "duplicate_knowledge_component")
		}
		seen[componentKey(ref)] = true
	}
	return nil
}
func validateAgentConfig(scope runtime.Scope, in AgentConfigDefinition) error {
	if err := api.ValidateRecord("ComponentRef", in.AgentConfigRef); err != nil {
		return err
	}
	if err := api.ValidateRecord("ComponentRef", in.BrainRef); err != nil {
		return err
	}
	if err := validateComponentSet(in.CapabilityRefs, 32); err != nil {
		return err
	}
	sealed, err := SealAgentConfig(in)
	if err != nil || !api.Equal(sealed, in) {
		return api.E("invalid_request", "agent_config_manifest_digest_mismatch")
	}
	if len(in.SourceRefs) > 8 || in.ControlLimitsRef.ByteLength == 0 || in.ControlLimitsRef.ByteLength > maxSkillUsageBytes {
		return api.E("invalid_request", "agent_config_content_bounds_invalid")
	}
	seen := map[string]bool{}
	for _, ref := range agentContents(in) {
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
func validateKnowledgeControls(in KnowledgeControls) error {
	if in.MaxInputBytes == 0 || in.MaxInputBytes > 2<<20 || in.MaxOutputTokens == 0 || in.MaxOutputTokens > 65536 || in.MaxActionsPerDecision > 16 || in.MaxDelegationsPerDecision > 4 || in.MaxDepth > 8 || in.MaxActionDurationSeconds == 0 || in.MaxActionDurationSeconds > 3600 || len(in.MaxCallCostBound) > 8 {
		return api.E("invalid_request", "knowledge_control_bounds_invalid")
	}
	return api.ValidateAmounts(in.MaxCallCostBound)
}
func (s *Service) registerAgentConfig(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AgentConfigDefinition) (runtime.Outcome, error) {
	if s.Ports.Content == nil {
		return runtime.Outcome{}, api.E("unsupported", "knowledge_content_unavailable")
	}
	if err := validateAgentConfig(tx.Scope(), in); err != nil {
		return runtime.Outcome{}, err
	}
	if c.TargetID != in.AgentConfigRef.ComponentID {
		return runtime.Outcome{}, api.E("invalid_request", "agent_config_target_mismatch")
	}
	if err := s.checkKnowledgeTx(ctx, tx, auth, agentContents(in), "agent_config.register"); err != nil {
		return runtime.Outcome{}, err
	}
	id := componentKey(in.AgentConfigRef)
	record := AgentConfigRecord{ID: id, Revision: 1, Definition: in, AuthorRef: auth.Ref(tx.Scope().OwnerID), InstallLockRef: AgentConfigInstallLock(in), State: "validating", ValidationCommandID: c.CommandID}
	if err := tx.Create(ctx, ns("agent_configs"), id, auth.SubjectID, record); err != nil {
		return runtime.Outcome{}, err
	}
	if err := tx.Bind(ctx, ns("agent_configs"), in.AgentConfigRef.ComponentID+"/"+in.AgentConfigRef.Version, id, in.AgentConfigRef.Digest); err != nil {
		return runtime.Outcome{}, err
	}
	if err := tx.Create(ctx, ns("knowledge_validations"), c.CommandID, id, knowledgeValidation{ID: c.CommandID, Revision: 1, RecordID: id, Kind: "agent_config", Auth: auth}); err != nil {
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
func (s *Service) readAgentConfig(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, _ api.Query, in AgentConfigReference) (AgentConfigRecord, error) {
	var birth, out AgentConfigRecord
	id := componentKey(in.AgentConfigRef)
	if _, err := store.Read(ctx, scope, ns("agent_configs"), id, 1, &birth); err != nil {
		return out, err
	}
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		if err := s.checkKnowledgeTx(ctx, tx, auth, agentContents(birth.Definition), "agent_config.read"); err != nil {
			return err
		}
		_, err := tx.Get(ctx, ns("agent_configs"), id, &out)
		return err
	})
	if status == runtime.CommitUnknown {
		return AgentConfigRecord{}, runtime.ErrCommitUnknown
	}
	return out, err
}
func (s *Service) validateAgentConfig(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, validation knowledgeValidation) error {
	var birth AgentConfigRecord
	if _, err := store.Read(ctx, scope, ns("agent_configs"), validation.RecordID, 1, &birth); err != nil {
		return err
	}
	var controls KnowledgeControls
	status, ioErr := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		return s.checkKnowledgeTx(ctx, tx, validation.Auth, agentContents(birth.Definition), "agent_config.validate")
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if ioErr == nil {
		var bytes []byte
		bytes, ioErr = s.knowledgeBytes(ctx, scope, validation.Auth, birth.Definition.ControlLimitsRef, "agent_config.validate", maxSkillUsageBytes)
		if ioErr == nil {
			var validator *api.Validator
			validator, ioErr = api.NewValidator(api.SchemaFor[KnowledgeControls]())
			if ioErr == nil {
				ioErr = validator.Validate(bytes)
			}
		}
		if ioErr == nil {
			ioErr = api.Decode(bytes, &controls)
		}
		if ioErr == nil {
			ioErr = validateKnowledgeControls(controls)
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
			if err := s.checkKnowledgeTx(ctx, tx, validation.Auth, agentContents(birth.Definition), "agent_config.validate"); err != nil {
				if knowledgeRejection(err) == nil {
					return err
				}
				ioErr = err
			}
		}
		var current AgentConfigRecord
		rev, err := tx.Get(ctx, ns("agent_configs"), birth.ID, &current)
		if err != nil {
			return err
		}
		if current.State != "validating" || current.ValidationCommandID != validation.ID {
			return nil
		}
		current.Revision = rev + 1
		if ioErr == nil {
			current.State, current.Controls, current.FailureReason = "active", &controls, ""
		} else {
			current.State, current.FailureReason = "invalid", knowledgeRejection(ioErr).Reason
		}
		if err := tx.Put(ctx, ns("agent_configs"), current.ID, rev, current); err != nil {
			return err
		}
		return runtime.Decide(ctx, tx, validation.ID, StateOutput{Ref: scope.Ref(current.ID, current.Revision), State: current.State}, knowledgeRejection(ioErr))
	})
}
