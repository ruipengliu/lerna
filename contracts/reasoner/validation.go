package reasoner

import (
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// Validate 检查提议形状与快照允许的用途；授权和证据真实性仍由核心裁决。
func Validate(s *v1.ContextSnapshot, p *v1.Proposal) error {
	if s == nil || p == nil {
		return command.Fail("INVALID_OUTPUT")
	}
	plan := false
	interpret := false
	for _, purpose := range s.AllowedPurposes {
		plan = plan || purpose == "PLAN"
		interpret = interpret || purpose == "INTERPRET_INPUT"
	}
	if !plan && !interpret {
		return command.Fail("INVALID_OUTPUT")
	}
	bodies := 0
	if p.Step != nil {
		bodies++
	}
	if p.Question != nil {
		bodies++
	}
	if p.RequirementsChange != nil {
		bodies++
	}
	if len(p.Verdicts) > 0 || len(p.CompletionEvidence) > 0 || p.ResultDraft != "" {
		bodies++
	}
	switch p.Kind {
	case "ACTION":
		if !plan || bodies != 1 || p.Step == nil || p.Step.StepId == "" || p.Step.CapabilityRef == nil || (p.Step.ParametersRef == nil && len(p.Step.ArgumentsJson) == 0) || len(p.Step.Dependencies) > 0 || len(p.Step.MemoryDependencies) > 0 || (p.Step.WorkCategory != "" && p.Step.WorkCategory != "TARGET") {
			return command.Fail("INVALID_OUTPUT")
		}
	case "QUESTION":
		if bodies != 1 || p.Question == nil || p.Question.Question == "" {
			return command.Fail("INVALID_OUTPUT")
		}
	case "REQUIREMENTS":
		r := p.RequirementsChange
		if bodies != 1 || r == nil || r.OriginalVersion != s.RequirementsVersion || len(r.Conditions) == 0 || r.Reason == "" || r.Impact == "" {
			return command.Fail("INVALID_OUTPUT")
		}
		seen := map[string]bool{}
		for _, c := range r.Conditions {
			if c == nil || c.ConditionId == "" || seen[c.ConditionId] || c.RuleVersion != 1 || (c.VerificationRule != "TARGET_RECORD" && c.VerificationRule != "USER_EVALUATION") {
				return command.Fail("INVALID_OUTPUT")
			}
			seen[c.ConditionId] = true
		}
	case "COMPLETE":
		if !plan || bodies > 1 || p.Step != nil || p.Question != nil || p.RequirementsChange != nil {
			return command.Fail("INVALID_OUTPUT")
		}
		seen := map[string]bool{}
		for _, v := range p.Verdicts {
			if v == nil || v.ConditionId == "" || seen[v.ConditionId] {
				return command.Fail("INVALID_OUTPUT")
			}
			seen[v.ConditionId] = true
			switch v.Conclusion {
			case "SATISFIED":
				if len(v.EvidenceRefs) == 0 && v.ConfirmationRef == nil {
					return command.Fail("INVALID_OUTPUT")
				}
			case "UNSATISFIED", "UNKNOWN":
				if len(v.Gaps) == 0 {
					return command.Fail("INVALID_OUTPUT")
				}
			default:
				return command.Fail("INVALID_OUTPUT")
			}
		}
	default:
		return command.Fail("INVALID_OUTPUT")
	}
	return nil
}

// ValidateContext 限定完成判断与引用到固定事实；证据是否满足条件仍由完成门禁核验。
func ValidateContext(s *v1.ContextSnapshot, requirements *v1.Requirements, p *v1.Proposal) error {
	if e := Validate(s, p); e != nil {
		return e
	}
	known := append([]*v1.Ref(nil), s.ContentRefs...)
	known = append(known, s.CapabilityRefs...)
	known = append(known, s.ProgressRefs...)
	known = append(known, s.RequirementsRef)
	for _, progress := range s.ProgressFacts {
		known = append(known, progress.OperationRef, progress.EffectRef)
		known = append(known, progress.EvidenceRefs...)
	}
	for _, confirmation := range s.Confirmations {
		known = append(known, confirmation.Ref)
	}
	contains := func(ref *v1.Ref) bool {
		if ref == nil || ref.Name == nil || ref.Revision == 0 {
			return false
		}
		for _, candidate := range known {
			if proto.Equal(candidate, ref) {
				return true
			}
		}
		return false
	}
	for _, ref := range p.BasisRefs {
		if !contains(ref) {
			return command.Fail("INVALID_OUTPUT")
		}
	}
	if p.Kind != "COMPLETE" {
		return nil
	}
	if requirements == nil || !proto.Equal(requirements.Ref, s.RequirementsRef) || requirements.RequirementsVersion != s.RequirementsVersion {
		return command.Fail("INVALID_OUTPUT")
	}
	seen := map[string]bool{}
	for _, verdict := range p.Verdicts {
		var requirement *v1.Requirement
		for _, condition := range requirements.Conditions {
			if condition.ConditionId == verdict.ConditionId {
				requirement = condition
			}
		}
		if requirement == nil {
			return command.Fail("INVALID_OUTPUT")
		}
		seen[verdict.ConditionId] = true
		for _, ref := range verdict.EvidenceRefs {
			if !contains(ref) {
				return command.Fail("INVALID_OUTPUT")
			}
		}
		if verdict.Conclusion != "SATISFIED" {
			continue
		}
		switch requirement.VerificationRule {
		case "USER_EVALUATION":
			if verdict.OperationId != nil || verdict.ConfirmationRef == nil {
				return command.Fail("INVALID_OUTPUT")
			}
			found := false
			for _, confirmation := range s.Confirmations {
				if proto.Equal(confirmation.Ref, verdict.ConfirmationRef) && confirmation.MatterType == "CONDITION_EVALUATION" && confirmation.State == "APPROVED" && confirmation.ConditionId == verdict.ConditionId && proto.Equal(confirmation.RequirementsRef, requirements.Ref) && confirmation.InputVersion == s.InputVersion && confirmation.ControlGeneration == s.ControlGeneration {
					found = true
				}
			}
			if !found {
				return command.Fail("INVALID_OUTPUT")
			}
		case "TARGET_RECORD":
			if verdict.ConfirmationRef != nil || verdict.OperationId == nil || len(verdict.EvidenceRefs) == 0 {
				return command.Fail("INVALID_OUTPUT")
			}
			found := false
			for _, fact := range s.ProgressFacts {
				if proto.Equal(fact.OperationRef.GetName(), verdict.OperationId) {
					found = true
					for _, ref := range verdict.EvidenceRefs {
						exists := false
						for _, evidence := range fact.EvidenceRefs {
							exists = exists || proto.Equal(ref, evidence)
						}
						if !exists {
							return command.Fail("INVALID_OUTPUT")
						}
					}
				}
			}
			if !found {
				return command.Fail("INVALID_OUTPUT")
			}
		default:
			return command.Fail("INVALID_OUTPUT")
		}
	}
	for _, condition := range requirements.Conditions {
		if condition.Necessary && !seen[condition.ConditionId] {
			return command.Fail("INVALID_OUTPUT")
		}
	}
	return nil
}
