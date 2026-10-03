package brain

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"github.com/ruipengliu/lerna/api"
	"strings"
)

// GoalSpec 是已登记报告/答案模板；自然语言目标需明确补充或配置模型。
type GoalSpec struct {
	Kind     string `json:"kind"`
	Title    string `json:"title,omitempty"`
	Body     string `json:"body"`
	SavePath string `json:"save_path,omitempty"`
}

func GoalSchema() api.Schema {
	text := api.Schema{"type": "string", "minLength": 1, "maxLength": 65536}
	return api.Schema{"oneOf": []any{api.Object(map[string]any{"kind": api.Schema{"const": "answer"}, "body": text}, "kind", "body"), api.Object(map[string]any{"kind": api.Schema{"const": "report"}, "title": api.String(), "body": text, "save_path": api.String()}, "kind", "title", "body", "save_path")}}
}
func ReportBytes(g GoalSpec) []byte {
	if g.Kind == "report" {
		return []byte("# " + g.Title + "\n\n" + g.Body + "\n")
	}
	return []byte(g.Body)
}

type RuleParameters struct {
	Kind           string `json:"kind"`
	ExpectedHash   string `json:"expected_hash"`
	ExpectedLength uint64 `json:"expected_length"`
	SavePath       string `json:"save_path,omitempty"`
}
type ActionFact struct {
	Ref            api.ObjectRef
	CapabilityRef  api.ComponentRef
	LogicalStepKey string
	Operation      api.Operation
	ResultBytes    []byte
}
type FactSource interface {
	Operations(context.Context, api.Snapshot) ([]ActionFact, error)
}
type RuleEngine struct {
	Goals                                 GoalResolver
	Facts                                 FactSource
	ArtifactRule, SavedRule, AnswerSchema api.ComponentRef
	ReadCapability, WriteCapability       api.ComponentRef
	ReadBinding, WriteBinding             api.ObjectRef
}

// GoalResolver follows only the immutable GoalDocument references declared in
// the snapshot; the original document bytes remain part of the sealed encoding.
type GoalResolver interface {
	ResolveGoal(context.Context, api.Snapshot, json.RawMessage) (json.RawMessage, error)
}
type ruleEncoding struct {
	Snapshot api.Snapshot    `json:"snapshot"`
	Goal     json.RawMessage `json:"goal"`
}

func (*RuleEngine) Physical() bool { return false }
func (e *RuleEngine) Encode(_ context.Context, s api.Snapshot, goal []byte, p Profile) (Encoding, error) {
	s.EncodedDigest = ""
	s.InputTokens = 0
	raw := api.Raw(ruleEncoding{s, append([]byte(nil), goal...)})
	return Encoding{Body: raw, Digest: api.Hash(raw), Receiver: "builtin-rule-engine", Location: "cloud", InputTokens: uint64(len(raw)), CountMode: "upper_bound", ProcessedSources: append([]api.ContentRef{}, s.ProcessedSources...)}, nil
}
func (e *RuleEngine) Lookup(context.Context, string, Encoding) (Generated, error) {
	return Generated{}, api.E("unsupported", "rule_call_has_no_external_lookup")
}
func (e *RuleEngine) Request(ctx context.Context, _ string, enc Encoding) (Generated, error) {
	if api.Hash(enc.Body) != enc.Digest {
		return Generated{}, api.E("forbidden", "encoded_request_changed")
	}
	var input ruleEncoding
	if er := api.Decode(enc.Body, &input); er != nil {
		return Generated{}, er
	}
	if e.Goals != nil {
		goal, er := e.Goals.ResolveGoal(ctx, input.Snapshot, input.Goal)
		if er != nil {
			return Generated{}, er
		}
		input.Goal = goal
	}
	v, er := api.NewValidator(GoalSchema())
	if er != nil {
		return Generated{}, er
	}
	out := Generated{Usage: []api.Amount{{Unit: "USD", Value: "0"}}, UsageFinal: true, Contents: []GeneratedContent{}}
	add := func(id, media, body string) {
		out.Contents = append(out.Contents, GeneratedContent{LocalID: id, MediaType: media, Body: body, DisclosedSources: []api.ContentRef{}})
	}
	add("reason", "text/plain", "使用已登记的准确目标模板；工具效果与完成条件由独立负责方核验。")
	if er = v.Validate(input.Goal); er != nil {
		add("question", "text/plain", "请提供完整 GoalSpec：answer 的 body，或 report 的 title/body/save_path。自由文本不会被默认解释为已核验条件。")
		out.Draft = Draft{Kind: "request_input", ReasonLocalID: "reason", QuestionLocalID: "question", AnswerSchemaRef: &e.AnswerSchema, Purpose: "clarify_goal"}
		return out, nil
	}
	var goal GoalSpec
	if er = api.Decode(input.Goal, &goal); er != nil {
		return out, er
	}
	body := ReportBytes(goal)
	params := RuleParameters{Kind: goal.Kind, ExpectedHash: api.Hash(body), ExpectedLength: uint64(len(body)), SavePath: goal.SavePath}
	if input.Snapshot.Purpose == "interpret_requirements" {
		add("statement", "text/plain", "产物必须与原模板指定的准确正文和摘要一致。")
		add("parameters", "application/json", string(api.Raw(params)))
		out.Draft = Draft{Kind: "refine_requirements", ReasonLocalID: "reason", Requirements: []DraftRequirement{{CandidateKey: "artifact_exact", Kind: "quality", StatementLocalID: "statement", ParametersLocalID: "parameters", RuleRef: e.ArtifactRule, Required: true}}}
		if goal.Kind == "report" {
			add("saved_statement", "text/plain", "报告必须保存到原准确路径，独立读回与原正文一致。")
			out.Draft.Requirements = append(out.Draft.Requirements, DraftRequirement{CandidateKey: "file_saved_readback", Kind: "effect", StatementLocalID: "saved_statement", ParametersLocalID: "parameters", RuleRef: e.SavedRule, Required: true})
		}
		return out, nil
	}
	add("artifact", "text/markdown", string(body))
	out.Draft = Draft{Kind: "complete", ReasonLocalID: "reason", ArtifactLocalIDs: []string{"artifact"}}
	if goal.Kind == "answer" {
		return out, nil
	}
	if e.Facts == nil {
		return out, api.E("dependency_unavailable", "original_facts_unavailable")
	}
	facts, er := e.Facts.Operations(ctx, input.Snapshot)
	if er != nil {
		return out, er
	}
	var inspected *ActionFact
	var saved, verified *ActionFact
	for i := range facts {
		f := &facts[i]
		later, ok := f.Operation.MayApplyLater.(bool)
		if f.Operation.ExecutionState != "closed" || f.Operation.Effect == "unknown" || !ok || later {
			return out, api.E("dependency_unavailable", "original_effect_open")
		}
		if strings.HasSuffix(f.LogicalStepKey, "/inspect_file") {
			inspected = f
		}
		if strings.HasSuffix(f.LogicalStepKey, "/save_report") {
			saved = f
		}
		if strings.HasSuffix(f.LogicalStepKey, "/verify_file") {
			verified = f
		}
	}
	act := func(local string, cap api.ComponentRef, binding api.ObjectRef, arg any, dependency string) {
		add("arguments", "application/json", string(api.Raw(arg)))
		out.Contents[len(out.Contents)-1].ContentLocalID = dependency
		out.Draft = Draft{Kind: "act", ReasonLocalID: "reason", Actions: []DraftAction{{LocalKey: local, CapabilityRef: cap, BindingRef: binding, ArgumentsLocalID: "arguments"}}}
	}
	if inspected == nil {
		act("inspect_file", e.ReadCapability, e.ReadBinding, struct {
			Path string `json:"path"`
		}{goal.SavePath}, "")
		return out, nil
	}
	if saved == nil {
		var observation struct {
			Path       string `json:"path"`
			Version    string `json:"version"`
			DataBase64 string `json:"data_base64"`
			ObservedAt string `json:"observed_at"`
		}
		if er = api.Decode(inspected.ResultBytes, &observation); er != nil || observation.Path != goal.SavePath {
			return out, api.E("invalid_request", "file_observation_invalid")
		}
		act("save_report", e.WriteCapability, e.WriteBinding, struct {
			Path            string `json:"path"`
			ExpectedVersion string `json:"expected_version"`
		}{goal.SavePath, observation.Version}, "artifact")
		return out, nil
	}
	if saved.Operation.Effect != "applied" {
		out.Draft = Draft{Kind: "fail", ReasonLocalID: "reason", ReasonCode: "file_write_not_applied"}
		return out, nil
	}
	if verified == nil {
		act("verify_file", e.ReadCapability, e.ReadBinding, struct {
			Path string `json:"path"`
		}{goal.SavePath}, "")
		return out, nil
	}
	var readback struct {
		Path       string `json:"path"`
		Version    string `json:"version"`
		DataBase64 string `json:"data_base64"`
		ObservedAt string `json:"observed_at"`
	}
	if er = api.Decode(verified.ResultBytes, &readback); er != nil {
		return out, er
	}
	observed, er := base64.StdEncoding.DecodeString(readback.DataBase64)
	if er != nil || readback.Path != goal.SavePath || api.Hash(observed) != params.ExpectedHash {
		out.Draft = Draft{Kind: "fail", ReasonLocalID: "reason", ReasonCode: "file_readback_mismatch"}
		return out, nil
	}
	return out, nil
}

var _ Engine = (*RuleEngine)(nil)
