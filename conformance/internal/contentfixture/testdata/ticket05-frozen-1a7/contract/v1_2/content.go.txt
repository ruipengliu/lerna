package v1_2

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
)

const MaxContentBytes = 256 * 1024

func DecodeBytes(text string) ([]byte, error) {
	data, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil || base64.StdEncoding.EncodeToString(data) != text {
		return nil, refusal("schema_invalid", err)
	}
	if len(data) > MaxContentBytes {
		return nil, refusal("input_over_limit", nil)
	}
	return data, nil
}
func contentSemantics(name string, value any) error {
	object, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	switch name {
	case "ContentPutRequest", "ContentGetRequest":
		payload := object["payload"].(map[string]any)
		if !targetRefEqual(object["target"], payload["content_ref"]) {
			return refusal("schema_invalid", nil)
		}
		return contentSemantics(map[bool]string{true: "ContentPutPayload", false: "ContentGetPayload"}[name == "ContentPutRequest"], payload)
	case "ContentPutPayload":
		if _, err := DecodeBytes(object["bytes_base64"].(string)); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, source := range object["sources"].([]any) {
			r := source.(map[string]any)
			o := r["owner"].(map[string]any)
			key, _ := json.Marshal([]any{o["tenant_id"], o["owner_id"], r["content_id"], r["version"]})
			if seen[string(key)] {
				return refusal("schema_invalid", nil)
			}
			seen[string(key)] = true
		}
	case "ContentGetResponse", "ContentGetResponsePublished":
		if object["status"] != "published" {
			return nil
		}
		data, err := DecodeBytes(object["bytes_base64"].(string))
		if err != nil {
			return err
		}
		ref := object["content_ref"].(map[string]any)
		total, _ := strconv.ParseInt(ref["byte_length"].(string), 10, 64)
		if total > MaxContentBytes {
			return refusal("schema_invalid", nil)
		}
		expected := total
		if part, present := object["range"]; present {
			r := part.(map[string]any)
			offset, _ := strconv.ParseInt(r["offset"].(string), 10, 64)
			length, _ := strconv.ParseInt(r["length"].(string), 10, 64)
			if offset > total || length > total-offset {
				return refusal("schema_invalid", nil)
			}
			expected = length
		} else {
			hash := sha256.Sum256(data)
			if "sha256:"+hex.EncodeToString(hash[:]) != ref["hash"] {
				return refusal("schema_invalid", nil)
			}
		}
		if int64(len(data)) != expected {
			return refusal("schema_invalid", nil)
		}
	}
	return nil
}
func targetRefEqual(target, ref any) bool {
	t := target.(map[string]any)
	r := ref.(map[string]any)
	o := r["owner"].(map[string]any)
	return t["tenant_id"] == o["tenant_id"] && t["owner_id"] == o["owner_id"] && t["id"] == r["content_id"] && t["kind"] == "content"
}
func decodeContent[T Value](data []byte, method string) (T, error) {
	var zero T
	envelope, err := ParseCommand(data)
	if err != nil {
		return zero, err
	}
	if envelope.ContractVersion != Version {
		return zero, refusal("version_unsupported", nil)
	}
	if envelope.Profile != "content" || string(envelope.Method) != method {
		return zero, refusal("unsupported", nil)
	}
	result, err := Decode[T](data)
	if err != nil {
		var classified *ContractError
		if errors.As(err, &classified) {
			return zero, err
		}
		return zero, refusal("schema_invalid", err)
	}
	return result, nil
}
func DecodePut(data []byte) (ContentPutRequest, error) {
	return decodeContent[ContentPutRequest](data, "content.put")
}
func DecodeGet(data []byte) (ContentGetRequest, error) {
	return decodeContent[ContentGetRequest](data, "content.get")
}
func DecodeContentResponse(data []byte, request ContentGetPayload) (ContentGetResponse, error) {
	result, err := Decode[ContentGetResponse](data)
	if err != nil {
		return result, refusal("schema_invalid", err)
	}
	raw, _ := ParseJSON(data)
	object := raw.(map[string]any)
	if object["status"] == "rejected" {
		return result, nil
	}
	wanted, err := Encode(request.ContentRef)
	if err != nil {
		return ContentGetResponse{}, refusal("schema_invalid", err)
	}
	ref, _ := ParseJSON(wanted)
	if !reflect.DeepEqual(ref, object["content_ref"]) {
		return ContentGetResponse{}, refusal("schema_invalid", fmt.Errorf("content response reference mismatch"))
	}
	if object["status"] == "published" {
		if request.Range == nil {
			if _, present := object["range"]; present {
				return ContentGetResponse{}, refusal("schema_invalid", nil)
			}
		} else {
			rangeJSON, err := Encode(*request.Range)
			if err != nil {
				return ContentGetResponse{}, refusal("schema_invalid", err)
			}
			part, _ := ParseJSON(rangeJSON)
			if !reflect.DeepEqual(part, object["range"]) {
				return ContentGetResponse{}, refusal("schema_invalid", nil)
			}
		}
	}
	return result, nil
}
func EncodeContentResponse(result ContentGetResponse, request ContentGetPayload) ([]byte, error) {
	data, err := Encode(result)
	if err != nil {
		return nil, err
	}
	_, err = DecodeContentResponse(data, request)
	return data, err
}
