package memory

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const ExperienceMediaType = "application/vnd.harness.experience+json"

// ExperienceSpec 区分实际结果与策略建议；未知和失败不能编码为成功经验。
type ExperienceSpec struct {
	GoalRef          api.ContentRef   `json:"goal_ref"`
	PrerequisiteRefs []api.ContentRef `json:"prerequisite_refs"`
	Outcome          string           `json:"outcome"`
	ResultRef        api.ContentRef   `json:"result_ref"`
	EvidenceRefs     []api.ContentRef `json:"evidence_refs"`
	BoundaryRef      api.ContentRef   `json:"boundary_ref"`
	OperationRef     *api.ObjectRef   `json:"operation_ref,omitempty"`
}

// ExperienceAuthority 在 Tx 外核验准确原效果，返回可恢复的原负责方证据引用。
type ExperienceAuthority interface {
	ValidateExperience(context.Context, runtime.Scope, runtime.Auth, ExperienceSpec) (api.ObjectRef, error)
}

func (s *Service) validateExperience(ctx context.Context, scope runtime.Scope, auth runtime.Auth, transfer Transfer, body []byte) (string, *api.ObjectRef, error) {
	validator, err := api.NewValidator(api.SchemaFor[ExperienceSpec]())
	if err != nil {
		return "", nil, err
	}
	if err = validator.Validate(body); err != nil {
		return "", nil, err
	}
	var spec ExperienceSpec
	if err = api.Decode(body, &spec); err != nil {
		return "", nil, err
	}
	if !contains([]string{"success", "failure", "unknown", "cancelled"}, spec.Outcome) || len(spec.PrerequisiteRefs) > 100 || len(spec.EvidenceRefs) > 100 {
		return "", nil, api.E("invalid_request", "invalid_experience_outcome")
	}
	refs := append([]api.ContentRef{spec.GoalRef, spec.ResultRef, spec.BoundaryRef}, spec.PrerequisiteRefs...)
	refs = append(refs, spec.EvidenceRefs...)
	err = s.within(ctx, scope, func(tx runtime.Tx) error {
		for _, ref := range refs {
			present := false
			for _, source := range transfer.ProcessedSources {
				if api.Equal(ref, source) {
					present = true
					break
				}
			}
			if !present {
				return api.E("invalid_request", "experience_source_not_processed")
			}
			if _, err := s.CheckContentTx(ctx, tx, auth, ref, "memory.save", s.Location, false); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return "", nil, err
	}
	if spec.Outcome == "unknown" {
		return spec.Outcome, nil, nil
	}
	if s.ExperienceAuthority == nil || spec.OperationRef == nil || len(spec.EvidenceRefs) == 0 {
		return "", nil, api.E("unsupported", "experience_effect_authority_unconfigured")
	}
	if err = runtime.CheckRef(scope, *spec.OperationRef); err != nil {
		return "", nil, err
	}
	proof, err := s.ExperienceAuthority.ValidateExperience(ctx, scope, auth, spec)
	if err != nil {
		return "", nil, err
	}
	if err = runtime.CheckRef(scope, proof); err != nil {
		return "", nil, err
	}
	return spec.Outcome, &proof, nil
}
