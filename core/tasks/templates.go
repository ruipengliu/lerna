package tasks

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 受信任务模板（任务编排 2.2）：可信输入落入模板，模板给出条件、必要性和核验规则。
// 模板把义务类型与核验规则的能力绑定；推理不能注册模板。
type template struct {
	id     string
	params []string
	build  func(p map[string]string) []*lernav1.Requirement
}

// RuleVersion 是 M1 核验规则的语义版本。
const RuleVersion = "m1"

var templates = map[string]template{
	// api_put：通过一个能力把资源 key 设为 value。
	"api_put": {
		id:     "api_put",
		params: []string{"capability", "key", "value"},
		build: func(p map[string]string) []*lernav1.Requirement {
			return []*lernav1.Requirement{{
				RequirementId: "r1",
				Description:   "资源 " + p["key"] + " 已通过 " + p["capability"] + " 设为指定值",
				Necessary:     true,
				Source:        lernav1.RequirementSource_REQUIREMENT_SOURCE_TEMPLATE,
				Rule: &lernav1.VerificationRule{
					Kind:         lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_OPERATION_APPLIED,
					Version:      RuleVersion,
					CapabilityId: p["capability"],
					Params:       map[string]string{"key": p["key"], "value": p["value"]},
				},
			}}
		},
	},
	// file_write：受管理目录中的文件内容等于给定内容，以读回核验。
	"file_write": {
		id:     "file_write",
		params: []string{"path", "content"},
		build: func(p map[string]string) []*lernav1.Requirement {
			sum := sha256.Sum256([]byte(p["content"]))
			return []*lernav1.Requirement{{
				RequirementId: "r1",
				Description:   "文件 " + p["path"] + " 的内容等于指定内容（读回核验）",
				Necessary:     true,
				Source:        lernav1.RequirementSource_REQUIREMENT_SOURCE_TEMPLATE,
				Rule: &lernav1.VerificationRule{
					Kind:           lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_FILE_CONTENT,
					Version:        RuleVersion,
					Params:         map[string]string{"path": p["path"]},
					ExpectedDigest: hex.EncodeToString(sum[:]),
				},
			}}
		},
	},
}

// TemplateIDs 返回受信模板的标识。
func TemplateIDs() []string {
	var out []string
	for id := range templates {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// buildRequirements 按模板或显式条件列表形成初始条件集（条件集接纳只支持这两种来源）。
// 都没有时返回 nil：模板外的目标请求用户澄清，不由模型自行发明完成条件。
func buildRequirements(draft *lernav1.RequirementSetDraft) ([]*lernav1.Requirement, lernav1.AcceptanceSource, string, error) {
	switch {
	case draft.GetTemplateId() != "":
		t, ok := templates[draft.GetTemplateId()]
		if !ok {
			return nil, 0, "", errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "unknown task template %q", draft.GetTemplateId())
		}
		for _, p := range t.params {
			if draft.GetTemplateParams()[p] == "" {
				return nil, 0, "", errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "template %s needs parameter %q", t.id, p)
			}
		}
		return t.build(draft.GetTemplateParams()), lernav1.AcceptanceSource_ACCEPTANCE_SOURCE_TEMPLATE, t.id, nil
	case len(draft.GetExplicit()) > 0:
		var out []*lernav1.Requirement
		seen := map[string]bool{}
		for _, r := range draft.GetExplicit() {
			if r.GetRequirementId() == "" || seen[r.GetRequirementId()] {
				return nil, 0, "", errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "explicit requirements need unique ids")
			}
			seen[r.GetRequirementId()] = true
			if err := checkRule(r.GetRule()); err != nil {
				return nil, 0, "", err
			}
			c := &lernav1.Requirement{
				RequirementId: r.GetRequirementId(),
				Description:   r.GetDescription(),
				Necessary:     r.GetNecessary(),
				Rule:          r.GetRule(),
				Source:        lernav1.RequirementSource_REQUIREMENT_SOURCE_USER,
			}
			c.Rule.Version = RuleVersion
			out = append(out, c)
		}
		return out, lernav1.AcceptanceSource_ACCEPTANCE_SOURCE_USER_LIST, "", nil
	default:
		return nil, 0, "", nil
	}
}

// checkRule 拒绝不能客观核验的规则：没有能力标识的"动作已生效"、没有期望摘要的文件核验。
func checkRule(r *lernav1.VerificationRule) error {
	switch r.GetKind() {
	case lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_OPERATION_APPLIED:
		if r.GetCapabilityId() == "" {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "OPERATION_APPLIED rule needs capability_id")
		}
	case lernav1.VerificationRuleKind_VERIFICATION_RULE_KIND_FILE_CONTENT:
		if r.GetParams()["path"] == "" || r.GetExpectedDigest() == "" {
			return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "FILE_CONTENT rule needs path and expected_digest")
		}
	default:
		return errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "unsupported verification rule %s", r.GetKind())
	}
	return nil
}
