package reasoner

import (
	"encoding/json"
	"sort"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

const ViewPolicy = "m1-default-v1"

// ViewMaterial 的正文由受信宿主实际读取；Required 由固定快照决定。
type ViewMaterial struct {
	Ref      *v1.Ref `json:"ref"`
	Body     []byte  `json:"body"`
	Required bool    `json:"required"`
}

const instructions = `Return exactly one JSON proposal object: kind ACTION, REQUIREMENTS, QUESTION, or COMPLETE. Do not return task IDs, version bindings, permissions, billing, or outcome status. ACTION has one step with stepId, capabilityRef, schemaDigest and concrete argumentsJson (base64 JSON object) or an existing parametersRef; expectedEvidence describes future evidence, never an observation. REQUIREMENTS has requirementsChange: originalVersion, conditions, reason, impact; this never accepts conditions. QUESTION has question: question, options, conditionIds, changesBasis, involvesConfirmation; text cannot confirm or grant. COMPLETE has verdicts for every necessary condition: conditionId, conclusion SATISFIED/UNSATISFIED/UNKNOWN, evidenceRefs or actual confirmationRef, gaps; resultDraft is only a draft. Use basisRefs and gaps. For INTERPRET_INPUT only QUESTION or REQUIREMENTS. Unknown effects and pending reports remain unknown. Never invent references or user confirmation.`

// BuildView 对完整条目裁剪；必保留层超限时明确失败，不增加摘要调用。
func BuildView(facts json.RawMessage, materials []ViewMaterial, maxBytes, maxTokens uint32) ([]byte, error) {
	if !json.Valid(facts) || maxBytes == 0 || maxTokens == 0 {
		return nil, command.Fail("INVALID_INPUT")
	}
	materials = append([]ViewMaterial(nil), materials...)
	key := func(m ViewMaterial) string { b, _ := json.Marshal(m.Ref); return string(b) }
	sort.SliceStable(materials, func(i, j int) bool {
		if materials[i].Required != materials[j].Required {
			return materials[i].Required
		}
		return key(materials[i]) < key(materials[j])
	})
	view := struct {
		Policy       string          `json:"policy"`
		Instructions string          `json:"instructions"`
		Facts        json.RawMessage `json:"facts"`
		Inputs       []ViewMaterial  `json:"inputs"`
		Omitted      []*v1.Ref       `json:"omitted"`
	}{ViewPolicy, instructions, facts, materials, []*v1.Ref{}}
	for {
		body, e := json.Marshal(view)
		if e != nil {
			return nil, e
		}
		if uint64(len(body)) <= uint64(maxBytes) && uint64(len(body)) <= uint64(maxTokens) {
			return body, nil
		}
		n := len(view.Inputs)
		if n == 0 || view.Inputs[n-1].Required {
			return nil, command.Fail("CONTEXT_TOO_LARGE")
		}
		view.Omitted = append(view.Omitted, view.Inputs[n-1].Ref)
		view.Inputs = view.Inputs[:n-1]
	}
}
