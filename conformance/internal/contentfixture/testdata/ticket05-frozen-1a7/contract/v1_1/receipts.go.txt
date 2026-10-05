package v1_1

import (
	"fmt"
	"reflect"
	"strings"
)

func responseSchema(name string) bool {
	return strings.HasPrefix(name, "CommandReceipt") || strings.HasPrefix(name, "CommandProgress") || strings.HasPrefix(name, "CommandGetResponse") || strings.HasPrefix(name, "TransportOutcome")
}

func valueError[T Value](err error) error {
	if err != nil && responseSchema(reflect.TypeFor[T]().Name()) {
		return refusal("schema_invalid", err)
	}
	return err
}

// semanticResponse checks relationships JSON Schema cannot express. Values have
// already passed the schema, so only these cross-field invariants belong here.
func semanticResponse(name string, value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	if name == "TransportOutcome" || name == "TransportOutcomeReceived" {
		if object["status"] == "received" {
			return semanticResponse("CommandReceipt", object["receipt"])
		}
		return nil
	}
	if name == "CommandReceipt" || name == "CommandReceiptAccepted" || name == "CommandReceiptApplied" {
		if object["state"] != "rejected" {
			return revisionBinding(object)
		}
		return nil
	}
	if name == "CommandProgress" || name == "CommandProgressTask" || name == "CommandProgressDecision" {
		if object["kind"] == "task" {
			return revisionBinding(object)
		}
		return nil
	}
	if object["status"] != "found" {
		return nil
	}
	receipt := object["receipt"].(map[string]any)
	progress := object["progress"].(map[string]any)
	if !equalCommandRef(object["command_ref"], receipt["command_ref"]) {
		return fmt.Errorf("command reference mismatch")
	}
	if err := semanticResponse("CommandReceipt", receipt); err != nil {
		return err
	}
	if err := semanticResponse("CommandProgress", progress); err != nil {
		return err
	}
	if progress["kind"] != "task" && progress["kind"] != "decision" {
		return nil
	}
	original, ok := receipt["object_ref"].(map[string]any)
	if !ok {
		return fmt.Errorf("progress without receipt object")
	}
	current := progress["object_ref"].(map[string]any)
	for _, key := range []string{"tenant_id", "owner_id", "kind", "id"} {
		if original[key] != current[key] {
			return fmt.Errorf("object identity mismatch")
		}
	}
	baseline, _ := receipt["revision"].(string)
	if baseline == "" {
		baseline, _ = original["revision"].(string)
	}
	observed := progress["revision"].(string)
	if baseline != "" && (len(observed) < len(baseline) || len(observed) == len(baseline) && observed < baseline) {
		return fmt.Errorf("progress predates fixed receipt")
	}
	return nil
}
func revisionBinding(object map[string]any) error {
	ref := object["object_ref"].(map[string]any)
	outer, hasOuter := object["revision"]
	nested, hasNested := ref["revision"]
	if hasOuter && hasNested && outer != nested {
		return fmt.Errorf("nested revision mismatch")
	}
	return nil
}
func equalCommandRef(a, b any) bool {
	left := a.(map[string]any)
	right := b.(map[string]any)
	lo := left["owner"].(map[string]any)
	ro := right["owner"].(map[string]any)
	return left["command_id"] == right["command_id"] && lo["tenant_id"] == ro["tenant_id"] && lo["owner_id"] == ro["owner_id"]
}

// DecodeCommandResponse validates the closed result and the original identity.
// The read envelope's own command_id is deliberately not this reference.
func DecodeCommandResponse(data []byte, original CommandRef) (CommandGetResponse, error) {
	result, err := Decode[CommandGetResponse](data)
	if err != nil {
		return result, refusal("schema_invalid", err)
	}
	raw, err := ParseJSON(data)
	if err != nil {
		return result, refusal("schema_invalid", err)
	}
	object := raw.(map[string]any)
	if object["status"] != "rejected" {
		encoded, err := Encode(original)
		if err != nil {
			return CommandGetResponse{}, refusal("schema_invalid", err)
		}
		ref, _ := ParseJSON(encoded)
		if !equalCommandRef(object["command_ref"], ref) {
			return CommandGetResponse{}, refusal("schema_invalid", nil)
		}
	}
	return result, nil
}
func EncodeCommandResponse(result CommandGetResponse, original CommandRef) ([]byte, error) {
	data, err := Encode(result)
	if err != nil {
		return nil, refusal("schema_invalid", err)
	}
	_, err = DecodeCommandResponse(data, original)
	return data, err
}
