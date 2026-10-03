package providers

import (
	"encoding/json"
	"regexp"
	"strings"
	"sync"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
)

var localID = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
var outputOnce sync.Once
var outputValidator *api.Validator
var outputError error

func localSchema() api.Schema {
	return api.Schema{"type": "string", "pattern": localID.String(), "minLength": 1, "maxLength": 64}
}

// ModelOutputSchema 仅接受本地草稿与待发布内容；不存在 Grant、效果或成功字段。
func ModelOutputSchema() api.Schema {
	variant := func(kind string, extra map[string]any, required ...string) api.Schema {
		props := map[string]any{"kind": map[string]any{"const": kind}, "reason_local_id": localSchema()}
		for k, v := range extra {
			props[k] = v
		}
		return api.Object(props, append([]string{"kind", "reason_local_id"}, required...)...)
	}
	draft := api.Schema{"oneOf": []any{
		variant("refine_requirements", map[string]any{"requirements": api.Array(api.SchemaFor[brain.DraftRequirement](), 1, 100)}, "requirements"),
		variant("act", map[string]any{"actions": api.Array(api.SchemaFor[brain.DraftAction](), 1, 4)}, "actions"),
		variant("complete", map[string]any{"artifact_local_ids": api.Array(localSchema(), 0, 100), "existing_artifact_refs": api.Array(api.Ref("ContentRef"), 0, 100)}),
		variant("request_input", map[string]any{"question_local_id": localSchema(), "answer_schema_ref": api.Ref("ComponentRef"), "purpose": api.Enum("clarify_goal", "supply_context")}, "question_local_id", "answer_schema_ref", "purpose"),
		variant("fail", map[string]any{"reason_code": api.String()}, "reason_code"),
	}}
	content := api.Object(map[string]any{
		"local_id": localSchema(), "media_type": api.Schema{"type": "string", "pattern": "^[A-Za-z0-9.+-]+/[A-Za-z0-9.+-]+$", "maxLength": 256}, "body": map[string]any{"type": "string", "maxLength": api.MaxJSONBytes},
		"disclosed_sources": api.Array(api.Ref("ContentRef"), 0, 100), "content_local_id": localSchema(),
	}, "local_id", "media_type", "body", "disclosed_sources")
	return api.Object(map[string]any{"draft": draft, "contents": api.Array(content, 1, 20)}, "draft", "contents")
}

type modelOutput struct {
	Draft    brain.Draft              `json:"draft"`
	Contents []brain.GeneratedContent `json:"contents"`
}

// ParseGenerated 拒绝重复键、不闭合字段和越界本地引用。费用由供应商账本填充。
func ParseGenerated(raw []byte) (brain.Generated, error) {
	outputOnce.Do(func() { outputValidator, outputError = api.NewValidator(ModelOutputSchema()) })
	if outputError != nil {
		return brain.Generated{}, outputError
	}
	if err := outputValidator.Validate(raw); err != nil {
		return brain.Generated{}, err
	}
	var input modelOutput
	if err := api.Decode(raw, &input); err != nil {
		return brain.Generated{}, err
	}
	found := map[string]bool{}
	total := 0
	for _, c := range input.Contents {
		if found[c.LocalID] || c.ContentLocalID != "" && !found[c.ContentLocalID] {
			return brain.Generated{}, api.E("invalid_request", "output_local_identity_invalid")
		}
		found[c.LocalID] = true
		total += len(c.Body)
		if total > api.MaxJSONBytes {
			return brain.Generated{}, api.E("invalid_request", "output_bytes_over_limit")
		}
	}
	d := input.Draft
	if !found[d.ReasonLocalID] {
		return brain.Generated{}, api.E("invalid_request", "output_reason_missing")
	}
	keys := map[string]bool{}
	for _, r := range d.Requirements {
		if !localID.MatchString(r.CandidateKey) || keys[r.CandidateKey] || !found[r.StatementLocalID] || r.ParametersLocalID != "" && !found[r.ParametersLocalID] {
			return brain.Generated{}, api.E("invalid_request", "output_requirement_invalid")
		}
		keys[r.CandidateKey] = true
	}
	for _, a := range d.Actions {
		if !localID.MatchString(a.LocalKey) || keys[a.LocalKey] || !found[a.ArgumentsLocalID] {
			return brain.Generated{}, api.E("invalid_request", "output_action_invalid")
		}
		keys[a.LocalKey] = true
	}
	if d.Kind == "request_input" && !found[d.QuestionLocalID] {
		return brain.Generated{}, api.E("invalid_request", "output_question_missing")
	}
	if d.Kind == "complete" && len(d.ArtifactLocalIDs)+len(d.ExistingArtifactRefs) == 0 {
		return brain.Generated{}, api.E("invalid_request", "output_artifact_missing")
	}
	for _, id := range d.ArtifactLocalIDs {
		if !found[id] {
			return brain.Generated{}, api.E("invalid_request", "output_artifact_missing")
		}
	}
	return brain.Generated{Draft: d, Contents: input.Contents, Usage: []api.Amount{}, UsageFinal: false}, nil
}

// promptSchema 收集实际引用的核心定义，避免把不相关的完整协议发给模型。
func promptSchema() ([]byte, error) {
	var core map[string]any
	if err := json.Unmarshal(api.CoreSchemaBytes(), &core); err != nil {
		return nil, err
	}
	defs := core["$defs"].(map[string]any)
	used := map[string]any{}
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok && strings.HasPrefix(ref, "#/$defs/") {
				name := strings.TrimPrefix(ref, "#/$defs/")
				if _, done := used[name]; !done {
					used[name] = defs[name]
					visit(defs[name])
				}
			}
			for _, child := range x {
				visit(child)
			}
		case []any:
			for _, child := range x {
				visit(child)
			}
		}
	}
	// JSON round-trip normalizes helper []string and alias map types before walking.
	var schema map[string]any
	if err := json.Unmarshal(api.Raw(ModelOutputSchema()), &schema); err != nil {
		return nil, err
	}
	visit(schema)
	schema["$defs"] = used
	return json.Marshal(schema)
}
