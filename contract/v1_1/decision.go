package v1_1

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
)

func decodeMethod[T Value](data []byte, method string) (T, error) {
	var zero T
	envelope, err := ParseCommand(data)
	if err != nil {
		return zero, err
	}
	if envelope.ContractVersion != Version {
		return zero, refusal("version_unsupported", nil)
	}
	if envelope.Profile != "decision_engine" || envelope.Method != MethodName(method) {
		return zero, refusal("unsupported", nil)
	}
	value, err := Decode[T](data)
	if err != nil {
		return zero, refusal("schema_invalid", err)
	}
	return value, nil
}

// DecodeDecide validates a closed Component request. Method availability is
// declared separately; decoding does not confer execution permission.
func DecodeDecide(data []byte) (DecisionDecideRequest, error) {
	return decodeMethod[DecisionDecideRequest](data, "decision_engine.decide")
}
func DecodeGet(data []byte) (DecisionGetRequest, error) {
	return decodeMethod[DecisionGetRequest](data, "decision_engine.get")
}
func DecodeCancel(data []byte) (DecisionCancelRequest, error) {
	return decodeMethod[DecisionCancelRequest](data, "decision_engine.cancel")
}

const DecisionInputDigestAlgorithm = "lerna-decision-input-1"

// DecisionInputDigest excludes the delivery command identity and its acceptance
// deadline, while retaining every fixed input and the trusted principal.
func DecisionInputDigest(request DecisionDecideRequest, subject SubjectBinding) (string, error) {
	data, err := Encode(request)
	if err != nil {
		return "", err
	}
	if _, err = DecodeDecide(data); err != nil {
		return "", err
	}
	principal, err := Encode(subject)
	if err != nil {
		return "", err
	}
	input, err := ParseJSON(data)
	if err != nil {
		return "", err
	}
	binding, err := ParseJSON(principal)
	if err != nil {
		return "", err
	}
	envelope := input.(map[string]any)
	content := map[string]any{"contract_version": envelope["contract_version"], "profile": envelope["profile"], "subject_binding": binding}
	for _, key := range []string{"task_ref", "snapshot_ref", "component_ref", "use_refs", "limits", "deadline"} {
		content[key] = envelope["payload"].(map[string]any)[key]
	}
	var canonical strings.Builder
	if err = writeCanonical(&canonical, content); err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(DecisionInputDigestAlgorithm + "\n" + canonical.String()))
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func sameIdentity(a, b map[string]any) bool {
	for _, key := range []string{"tenant_id", "owner_id", "kind", "id"} {
		if a[key] != b[key] {
			return false
		}
	}
	return true
}
func decisionSemantics(name string, value any) error {
	v, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	invalid := func() error { return refusal("schema_invalid", nil) }
	object := func(x any) map[string]any { m, _ := x.(map[string]any); return m }
	unique := func(items any, key string) bool {
		seen := map[string]bool{}
		for _, item := range items.([]any) {
			x := item
			if key != "" {
				x = object(item)[key]
				if key == "requirement_ref" {
					r := object(x)
					x = []any{r["tenant_id"], r["owner_id"], r["kind"], r["id"]}
				}
			}
			data, _ := json.Marshal(x)
			s := string(data)
			if seen[s] {
				return false
			}
			seen[s] = true
		}
		return true
	}
	switch name {
	case "DecisionLimits":
		for key, max := range map[string]string{"max_input_bytes": "1048576", "max_output_bytes": "1048576", "max_rule_steps": "1024", "max_actions": "4"} {
			n := v[key].(string)
			if len(n) > len(max) || (len(n) == len(max) && n > max) {
				return invalid()
			}
		}
		if !strings.HasPrefix(object(v["max_cost"])["unit"].(string), "fixture") {
			return invalid()
		}
	case "DecisionUsage":
		if v["model_requests"] != "0" || !strings.HasPrefix(object(v["cost"])["unit"].(string), "fixture") {
			return invalid()
		}
	case "DecisionDecidePayload":
		if err := decisionSemantics("DecisionLimits", v["limits"]); err != nil {
			return err
		}
		if !unique(v["use_refs"], "") {
			return invalid()
		}
	case "DecisionDecideRequest":
		payload := object(v["payload"])
		if object(v["target"])["id"] != payload["decision_id"] {
			return invalid()
		}
		return decisionSemantics("DecisionDecidePayload", payload)
	case "DecisionGetRequest", "DecisionCancelRequest":
		if !sameIdentity(object(v["target"]), object(object(v["payload"])["decision_ref"])) {
			return invalid()
		}
	case "Proposal":
		if !unique(v["processed_source_refs"], "") || !unique(v["requirement_delta"], "local_key") {
			return invalid()
		}
		if x, ok := v["disclosed_source_refs"]; ok && !unique(x, "") {
			return invalid()
		}
		seen := map[string]bool{}
		for _, delta := range v["requirement_delta"].([]any) {
			m := object(delta)
			if !unique(m["source_refs"], "") {
				return invalid()
			}
			if r, ok := m["replaces_ref"]; ok {
				ref := object(r)
				b, _ := json.Marshal([]any{ref["tenant_id"], ref["owner_id"], ref["kind"], ref["id"]})
				if seen[string(b)] {
					return invalid()
				}
				seen[string(b)] = true
			}
		}
		return decisionSemantics("ProposalAdvance", v["advance"])
	case "ProposalEvidence":
		if !unique(v["evidence_refs"], "") {
			return invalid()
		}
	case "RequirementDelta", "ProposalAction":
		if !unique(v["source_refs"], "") {
			return invalid()
		}
	case "ProposalAdvance", "ProposalAdvanceActions", "ProposalAdvanceCandidateResult", "ProposalAdvanceInputRequest", "ProposalAdvanceCannotContinue":
		switch v["kind"] {
		case "actions":
			if !unique(v["actions"], "local_key") {
				return invalid()
			}
			for _, action := range v["actions"].([]any) {
				if err := decisionSemantics("ProposalAction", action); err != nil {
					return err
				}
			}
		case "candidate_result":
			if !unique(v["artifact_refs"], "") || !unique(v["evidence"], "requirement_ref") {
				return invalid()
			}
			for _, e := range v["evidence"].([]any) {
				if !unique(object(e)["evidence_refs"], "") {
					return invalid()
				}
			}
		case "input_request":
			if !unique(v["preview_refs"], "") {
				return invalid()
			}
		case "cannot_continue":
			if refs, ok := v["artifact_refs"]; ok && !unique(refs, "") {
				return invalid()
			}
			if !unique(v["missing_requirements"], "") {
				return invalid()
			}
		}
	case "Decision", "DecisionAccepted", "DecisionRunning", "DecisionWaiting", "DecisionCompleted", "DecisionFailed", "DecisionCancelled":
		if err := decisionSemantics("DecisionUsage", v["usage"]); err != nil {
			return err
		}
		if input, ok := v["input"]; ok {
			if object(v["decision_ref"])["id"] != object(input)["decision_id"] {
				return invalid()
			}
			if err := decisionSemantics("DecisionDecidePayload", input); err != nil {
				return err
			}
		}
		if v["status"] == "cancelled" && v["input"] != nil && !sameExact(object(v["task_ref"]), object(object(v["input"])["task_ref"])) {
			return invalid()
		}
		if v["status"] == "cancelled" && v["input"] == nil {
			usage := object(v["usage"])
			for _, name := range []string{"input_bytes", "output_bytes", "rule_steps", "rule_starts", "model_requests"} {
				if usage[name] != "0" {
					return invalid()
				}
			}
			if object(usage["cost"])["integer_value"] != "0" || usage["measurements_complete"] != true {
				return invalid()
			}
		}
		if v["status"] == "completed" {
			if !unique(v["artifact_refs"], "") {
				return invalid()
			}
			p := object(v["proposal"])
			if !sameIdentity(object(v["decision_ref"]), object(p["decision_ref"])) || !sameExact(object(object(v["input"])["snapshot_ref"]), object(p["snapshot_ref"])) {
				return invalid()
			}
			return decisionSemantics("Proposal", p)
		}
	case "DecisionGetResponse", "DecisionGetResponseFound":
		if v["status"] == "found" {
			decision := object(v["decision"])
			if !sameIdentity(object(v["decision_ref"]), object(decision["decision_ref"])) {
				return invalid()
			}
			if current, ok := v["current_control"]; ok {
				control := object(current)
				task := decision["task_ref"]
				if input, ok := decision["input"]; ok {
					task = object(input)["task_ref"]
				}
				if control["decision_input_digest"] != decision["input_digest"] || !sameExact(object(control["task_ref"]), object(task)) {
					return invalid()
				}
			}
			return decisionSemantics("Decision", decision)
		}
	}
	return nil
}
func sameExact(a, b map[string]any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}
