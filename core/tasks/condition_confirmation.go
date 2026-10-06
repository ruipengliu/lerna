package tasks

import (
	"context"
	"encoding/json"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type ConditionConfirmations interface {
	QueryTaskConfirmations(context.Context, *v1.Caller, *v1.GlobalName) ([]*v1.SnapshotConfirmation, error)
	ConsumeConditionConfirmationInTransaction(context.Context, *v1.Ref, *v1.Ref, string, *v1.Ref) (*v1.Ref, error)
	QueryConfirmation(context.Context, *v1.Caller, *v1.Ref) (*v1.Confirmation, error)
	QueryCurrentConfirmation(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Confirmation, error)
}

func (s *Service) WithConditionConfirmations(c ConditionConfirmations) *Service {
	s.conditionConfirmations = c
	return s
}

// RequestConditionConfirmation 由核心固定用户评价的具体条件与材料，模型只可引用其结果。
func (s *Service) RequestConditionConfirmation(ctx context.Context, caller *v1.Caller, c *v1.RequestConditionConfirmationCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("condition-confirmation", c), "tasks.confirmation", func(tx context.Context) (*v1.Ref, error) {
		if caller.GetIssuerId() != "host" || c.SessionId == nil || len(c.EvidenceRefs) == 0 {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		t, e := s.QueryTask(tx, caller, c.TaskId)
		if e != nil {
			return nil, e
		}
		if t == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		r, e := s.QueryRequirements(tx, caller, c.RequirementsRef)
		if e != nil {
			return nil, e
		}
		if r == nil {
			return nil, command.Fail("INVALID_REQUIREMENTS")
		}
		var condition *v1.Requirement
		for _, v := range r.Conditions {
			if v.ConditionId == c.ConditionId {
				condition = v
			}
		}
		if condition == nil || condition.VerificationRule != "USER_EVALUATION" || condition.RuleVersion != 1 {
			return nil, command.Fail("CONFIRMATION_INVALID")
		}
		description := struct {
			Condition    *v1.Requirement
			EvidenceRefs []*v1.Ref
		}{Condition: condition, EvidenceRefs: c.EvidenceRefs}
		body, e := json.Marshal(description)
		if e != nil {
			return nil, e
		}
		_, now, e := s.store.Position(tx)
		if e != nil {
			return nil, e
		}
		confirmation, e := s.confirmationPublisher.CreateConfirmationInTransaction(tx, &v1.Confirmation{SessionId: c.SessionId, MatterType: "CONDITION_EVALUATION", Description: string(body), ExpiresAtUnixMs: now + 300000, Matter: &v1.Confirmation_ConditionEvaluation{ConditionEvaluation: &v1.ConditionConfirmationMatter{TaskId: t.TaskId, RequirementsRef: r.Ref, ConditionId: c.ConditionId, InputVersion: t.InputVersion, ControlGeneration: t.ControlGeneration, EvidenceRefs: c.EvidenceRefs}}})
		if e != nil {
			return nil, e
		}
		return confirmation.Ref, s.store.SaveTraceSource(tx, "tasks", &v1.TraceEvent{EventType: "CONDITION_CONFIRMATION_REQUESTED", SourceRecordRef: confirmation.Ref, TaskId: t.TaskId, OriginCommand: c.Header.Identity, RequirementsVersion: r.RequirementsVersion, RelatedRefs: append([]*v1.Ref{r.Ref, condition.DescriptionRef}, c.EvidenceRefs...)})
	})
}
func (s *Service) checkConditionConfirmationMatter(ctx context.Context, c *v1.Confirmation) error {
	m := c.GetConditionEvaluation()
	if m == nil || m.RequirementsRef == nil || m.ConditionId == "" || len(m.EvidenceRefs) == 0 || c.SessionId == nil {
		return command.Fail("CONFIRMATION_INVALID")
	}
	caller := &v1.Caller{UserId: s.user, IssuerId: "host"}
	t, e := s.QueryTask(ctx, caller, m.TaskId)
	if e != nil {
		return e
	}
	if t == nil || t.Lifecycle != v1.TaskLifecycle_TASK_LIFECYCLE_OPEN || t.Control != v1.TaskControl_TASK_CONTROL_ACTIVE || t.InputVersion != m.InputVersion || t.BoundInputVersion != t.InputVersion || t.ControlGeneration != m.ControlGeneration || t.RequirementsStatus != v1.RequirementsStatus_REQUIREMENTS_STATUS_ACCEPTED {
		return command.Fail("CONFIRMATION_INVALID")
	}
	p, e := s.store.LoadPlanning(ctx, m.TaskId)
	if e != nil {
		return e
	}
	if p.Requirements == nil || !proto.Equal(p.Requirements.Ref, m.RequirementsRef) || p.VerificationFreeze != 0 {
		return command.Fail("CONFIRMATION_INVALID")
	}
	found := false
	for _, condition := range p.Requirements.Conditions {
		if condition.ConditionId == m.ConditionId && condition.VerificationRule == "USER_EVALUATION" && condition.RuleVersion == 1 {
			found = true
			if e = s.content.CheckUsable(ctx, caller, condition.DescriptionRef); e != nil {
				return e
			}
		}
	}
	if !found {
		return command.Fail("CONFIRMATION_INVALID")
	}
	for _, ref := range m.EvidenceRefs {
		if e = s.content.CheckUsable(ctx, caller, ref); e != nil {
			return e
		}
	}
	return nil
}

func (s *Service) checkSubjectiveCondition(ctx context.Context, caller *v1.Caller, r *v1.Requirements, condition *v1.Requirement, candidates []*v1.CompletionEvidence, verification *v1.Ref) (*v1.ConditionFinding, error) {
	finding := &v1.ConditionFinding{Condition: condition, Source: r.Source, SourceInputRef: r.SourceInputRef, Conclusion: "UNKNOWN", Gap: "USER_CONFIRMATION_MISSING"}
	for _, candidate := range candidates {
		if candidate.ConditionId != condition.ConditionId || candidate.ConfirmationRef == nil {
			continue
		}
		confirmed, e := s.conditionConfirmations.QueryCurrentConfirmation(ctx, caller, candidate.ConfirmationRef.Name)
		if e != nil {
			return nil, e
		}
		m := confirmed.GetConditionEvaluation()
		if confirmed == nil || !proto.Equal(confirmed.Ref, candidate.ConfirmationRef) || confirmed.State != "CONSUMED" || confirmed.RespondedBy == nil || confirmed.SessionId == nil || confirmed.MatterType != "CONDITION_EVALUATION" || m == nil || !proto.Equal(m.TaskId, r.TaskId) || !proto.Equal(m.RequirementsRef, r.Ref) || m.ConditionId != condition.ConditionId || !proto.Equal(confirmed.GetConsumedVerificationRef().GetName(), verification.GetName()) {
			finding.Gap = "USER_CONFIRMATION_INVALID"
			return finding, nil
		}
		for _, ref := range m.EvidenceRefs {
			if e = s.content.CheckUsable(ctx, caller, ref); e != nil {
				return nil, e
			}
		}
		finding.Conclusion = "SATISFIED"
		finding.Gap = ""
		finding.EvidenceRefs = append([]*v1.Ref{confirmed.Ref}, m.EvidenceRefs...)
		return finding, nil
	}
	return finding, nil
}

// ConditionConfirmationView 的正文只存在于经治理核验的展示结果，不进入确认审计记录。
type ConditionConfirmationView struct {
	Confirmation *v1.Confirmation
	Condition    *v1.Requirement
	Materials    []*v1.Content
}

// ReadConditionConfirmation 读取事项绑定的精确历史条件与证据，不替换为当前条件。
func (s *Service) ReadConditionConfirmation(ctx context.Context, caller *v1.Caller, ref *v1.Ref) (*ConditionConfirmationView, error) {
	confirmation, e := s.conditionConfirmations.QueryConfirmation(ctx, caller, ref)
	if e != nil {
		return nil, e
	}
	matter := confirmation.GetConditionEvaluation()
	if confirmation == nil || confirmation.MatterType != "CONDITION_EVALUATION" || matter == nil {
		return nil, command.Fail("CONFIRMATION_INVALID")
	}
	requirements, e := s.QueryRequirements(ctx, caller, matter.RequirementsRef)
	if e != nil {
		return nil, e
	}
	if requirements == nil || !proto.Equal(requirements.TaskId, matter.TaskId) {
		return nil, command.Fail("CONFIRMATION_INVALID")
	}
	var condition *v1.Requirement
	for _, candidate := range requirements.Conditions {
		if candidate.ConditionId == matter.ConditionId {
			condition = candidate
			break
		}
	}
	if condition == nil || condition.VerificationRule != "USER_EVALUATION" || condition.RuleVersion != 1 {
		return nil, command.Fail("CONFIRMATION_INVALID")
	}
	view := &ConditionConfirmationView{Confirmation: confirmation, Condition: condition}
	for _, ref := range uniqueRefs(append([]*v1.Ref{condition.DescriptionRef}, matter.EvidenceRefs...)) {
		if e = s.content.CheckUsable(ctx, caller, ref); e != nil {
			return nil, e
		}
		content, e := s.confirmationContent.Read(ctx, caller, ref)
		if e != nil {
			return nil, e
		}
		if content == nil || !proto.Equal(content.Ref, ref) {
			return nil, command.Fail("CONTENT_UNUSABLE")
		}
		view.Materials = append(view.Materials, content)
	}
	return view, nil
}
