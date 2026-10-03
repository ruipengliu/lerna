package governance

import (
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/governance"
)

// 这些登记只来自宿主配置。构造及准入均不触及文件、网络或数据库。
type BuiltinRegistry struct {
	installations map[string]domain.Installation
}

func NewBuiltinRegistry(xs []domain.Installation) (*BuiltinRegistry, error) {
	if len(xs) == 0 || len(xs) > 128 {
		return nil, api.E("invalid_request", "finite_builtin_allowlist_required")
	}
	allow := map[string]domain.Installation{}
	validator, e := api.NewValidator(api.SchemaFor[domain.Installation]())
	if e != nil {
		return nil, e
	}
	for _, original := range xs {
		if e := validator.Validate(api.Raw(original)); e != nil {
			return nil, e
		}
		var in domain.Installation
		if e := api.Decode(api.Raw(original), &in); e != nil {
			return nil, e
		}
		if !in.TrustedBuiltin || in.ABI != "go-static-v1" || in.Profile != api.Profile || len(in.Artifacts) == 0 || len(in.Artifacts) > 32 {
			return nil, api.E("unsupported", "only_registered_static_builtin_supported")
		}
		for _, r := range []api.ComponentRef{in.InstallLockRef, in.ConfigRef, in.PlatformRef} {
			if e := api.ValidateRecord("ComponentRef", r); e != nil {
				return nil, e
			}
		}
		for _, r := range in.Artifacts {
			if e := api.ValidateRecord("ContentRef", r); e != nil {
				return nil, e
			}
		}
		key := directory(in.InstallLockRef)
		if _, exists := allow[key]; exists {
			return nil, api.E("invalid_request", "duplicate_builtin_install_lock")
		}
		allow[key] = in
	}
	return &BuiltinRegistry{allow}, nil
}
func (r *BuiltinRegistry) CheckInstallation(in domain.Installation) error {
	original, ok := r.installations[directory(in.InstallLockRef)]
	if !ok || !api.Equal(original, in) {
		return api.E("unsupported", "installation_not_in_trusted_builtin_allowlist")
	}
	return nil
}

type ReferenceRegistry struct {
	implementations map[string]ReferenceImplementation
}

func NewReferenceRegistry(xs []ReferenceImplementation) (*ReferenceRegistry, error) {
	if len(xs) == 0 || len(xs) > 128 {
		return nil, api.E("unsupported", "registered_reference_code_required")
	}
	implementations := map[string]ReferenceImplementation{}
	for _, in := range xs {
		if e := api.ValidateRecord("ComponentRef", in.Ref); e != nil {
			return nil, e
		}
		if in.Strategy != ReferenceReportV1 && in.Strategy != ReferenceReportBodyV0 {
			return nil, api.E("unsupported", "external_or_natural_language_evaluation_not_configured")
		}
		key := directory(in.Ref)
		if _, exists := implementations[key]; exists {
			return nil, api.E("invalid_request", "duplicate_reference_implementation")
		}
		implementations[key] = in
	}
	return &ReferenceRegistry{implementations}, nil
}
func (r *ReferenceRegistry) implementation(ref api.ComponentRef) (ReferenceImplementation, error) {
	in, ok := r.implementations[directory(ref)]
	if !ok || !api.Equal(in.Ref, ref) {
		return in, api.E("unsupported", "implementation_not_registered_for_reference_file_rules")
	}
	return in, nil
}
func (r *ReferenceRegistry) CheckEvaluationPlan(plan domain.EvaluationPlan) error {
	for _, ref := range []api.ComponentRef{plan.CandidateRef, plan.BaselineRef} {
		if _, e := r.implementation(ref); e != nil {
			return e
		}
	}
	if e := api.ValidateAmounts(plan.Budget); e != nil {
		return e
	}
	if len(plan.Budget) != 1 || plan.Budget[0].Unit != "USD" {
		return api.E("unsupported", "reference_runner_has_only_zero_billed_usd_usage")
	}
	return nil
}

var _ domain.InstallationAdmission = (*BuiltinRegistry)(nil)
var _ domain.EvaluationPlanAdmission = (*ReferenceRegistry)(nil)
