package v1_2

import (
	"fmt"
	"reflect"
	"strings"
)

func responseSchema(name string) bool {
	return strings.HasPrefix(name, "CommandReceipt") || strings.HasPrefix(name, "CommandProgress") || strings.HasPrefix(name, "CommandGetResponse") || strings.HasPrefix(name, "TransportOutcome") || strings.HasPrefix(name, "ContentGetResponse")
}
func valueError[T Value](err error) error {
	if err != nil && responseSchema(reflect.TypeFor[T]().Name()) {
		return refusal("schema_invalid", err)
	}
	return err
}
func semanticResponse(name string, value any) error {
	o, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	if strings.HasPrefix(name, "TransportOutcome") && o["status"] == "received" {
		return semanticResponse("CommandReceipt", o["receipt"])
	}
	if strings.HasPrefix(name, "CommandReceipt") && o["state"] == "accepted" {
		if !targetRefEqual(o["object_ref"], o["content_ref"]) {
			return fmt.Errorf("accepted content binding mismatch")
		}
		return nil
	}
	if strings.HasPrefix(name, "CommandGetResponse") && o["status"] == "found" {
		receipt := o["receipt"].(map[string]any)
		progress := o["progress"].(map[string]any)
		if !reflect.DeepEqual(o["command_ref"], receipt["command_ref"]) {
			return fmt.Errorf("command reference mismatch")
		}
		if err := semanticResponse("CommandReceipt", receipt); err != nil {
			return err
		}
		if receipt["state"] == "rejected" && progress["kind"] != "none" {
			return fmt.Errorf("rejection has progress")
		}
		if receipt["state"] == "accepted" && progress["kind"] == "none" {
			return fmt.Errorf("accepted has no observation")
		}
		if progress["kind"] == "content" && !reflect.DeepEqual(receipt["content_ref"], progress["content_ref"]) {
			return fmt.Errorf("progress content reference mismatch")
		}
	}
	return nil
}
func DecodeCommandResponse(data []byte, original CommandRef) (CommandGetResponse, error) {
	result, err := Decode[CommandGetResponse](data)
	if err != nil {
		return result, refusal("schema_invalid", err)
	}
	raw, _ := ParseJSON(data)
	object := raw.(map[string]any)
	if object["status"] != "rejected" {
		encoded, err := Encode(original)
		if err != nil {
			return CommandGetResponse{}, refusal("schema_invalid", err)
		}
		ref, _ := ParseJSON(encoded)
		if !reflect.DeepEqual(ref, object["command_ref"]) {
			return CommandGetResponse{}, refusal("schema_invalid", nil)
		}
	}
	return result, nil
}
func EncodeCommandResponse(result CommandGetResponse, original CommandRef) ([]byte, error) {
	data, err := Encode(result)
	if err != nil {
		return nil, err
	}
	_, err = DecodeCommandResponse(data, original)
	return data, err
}
