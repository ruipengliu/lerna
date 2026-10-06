package reasoner

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math/big"
	"strings"

	"github.com/ruipengliu/lerna/contracts/command"
)

// DecodeJSON 保留十进制精度，拒绝重复键、多值和非具体的变量占位符。
func DecodeJSON(body []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(body))
	d.UseNumber()
	var read func(int) (any, error)
	read = func(depth int) (any, error) {
		if depth > 64 {
			return nil, command.Fail("INVALID_ARGUMENTS")
		}
		token, e := d.Token()
		if e != nil {
			return nil, e
		}
		if delimiter, ok := token.(json.Delim); ok {
			switch delimiter {
			case '{':
				object := map[string]any{}
				for d.More() {
					token, e := d.Token()
					if e != nil {
						return nil, e
					}
					key, ok := token.(string)
					if !ok {
						return nil, command.Fail("INVALID_ARGUMENTS")
					}
					if _, exists := object[key]; exists {
						return nil, command.Fail("INVALID_ARGUMENTS")
					}
					v, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					object[key] = v
				}
				end, e := d.Token()
				if e != nil || end != json.Delim('}') {
					return nil, command.Fail("INVALID_ARGUMENTS")
				}
				return object, nil
			case '[':
				array := []any{}
				for d.More() {
					v, e := read(depth + 1)
					if e != nil {
						return nil, e
					}
					array = append(array, v)
				}
				end, e := d.Token()
				if e != nil || end != json.Delim(']') {
					return nil, command.Fail("INVALID_ARGUMENTS")
				}
				return array, nil
			default:
				return nil, command.Fail("INVALID_ARGUMENTS")
			}
		}
		return token, nil
	}
	value, e := read(0)
	if e != nil {
		return nil, e
	}
	if _, e = d.Token(); e != io.EOF {
		return nil, command.Fail("INVALID_ARGUMENTS")
	}
	return value, nil
}

// NormalizeSchema 只接受 M1 明确实现的 Schema 子集；不把未知关键词当作已核验。
func NormalizeSchema(body []byte) ([]byte, string, error) {
	value, e := DecodeJSON(body)
	if e != nil {
		return nil, "", command.Fail("INVALID_PARAMETER_SCHEMA")
	}
	if e = checkSchema(value, 0); e != nil {
		return nil, "", e
	}
	if value.(map[string]any)["type"] != "object" {
		return nil, "", command.Fail("INVALID_PARAMETER_SCHEMA")
	}
	encoded, e := json.Marshal(value)
	if e != nil {
		return nil, "", e
	}
	digest := sha256.Sum256(encoded)
	return encoded, hex.EncodeToString(digest[:]), nil
}
func checkSchema(value any, depth int) error {
	schema, ok := value.(map[string]any)
	if !ok || depth > 32 {
		return command.Fail("INVALID_PARAMETER_SCHEMA")
	}
	kind, ok := schema["type"].(string)
	if !ok {
		return command.Fail("INVALID_PARAMETER_SCHEMA")
	}
	for key := range schema {
		switch key {
		case "type", "description", "enum":
		case "properties", "required", "additionalProperties":
			if kind != "object" {
				return command.Fail("INVALID_PARAMETER_SCHEMA")
			}
		case "items":
			if kind != "array" {
				return command.Fail("INVALID_PARAMETER_SCHEMA")
			}
		default:
			return command.Fail("UNSUPPORTED_PARAMETER_SCHEMA")
		}
	}
	if description, exists := schema["description"]; exists {
		if _, ok := description.(string); !ok {
			return command.Fail("INVALID_PARAMETER_SCHEMA")
		}
	}
	if enum, exists := schema["enum"]; exists {
		values, ok := enum.([]any)
		if !ok || len(values) == 0 {
			return command.Fail("INVALID_PARAMETER_SCHEMA")
		}
	}
	switch kind {
	case "object":
		properties := map[string]any{}
		if p, exists := schema["properties"]; exists {
			var ok bool
			properties, ok = p.(map[string]any)
			if !ok {
				return command.Fail("INVALID_PARAMETER_SCHEMA")
			}
		}
		for _, child := range properties {
			if e := checkSchema(child, depth+1); e != nil {
				return e
			}
		}
		if required, exists := schema["required"]; exists {
			items, ok := required.([]any)
			if !ok {
				return command.Fail("INVALID_PARAMETER_SCHEMA")
			}
			seen := map[string]bool{}
			for _, item := range items {
				name, ok := item.(string)
				if !ok || seen[name] || properties[name] == nil {
					return command.Fail("INVALID_PARAMETER_SCHEMA")
				}
				seen[name] = true
			}
		}
		if additional, exists := schema["additionalProperties"]; exists {
			if _, ok := additional.(bool); !ok {
				return command.Fail("UNSUPPORTED_PARAMETER_SCHEMA")
			}
		}
	case "array":
		if schema["items"] == nil {
			return command.Fail("INVALID_PARAMETER_SCHEMA")
		}
		return checkSchema(schema["items"], depth+1)
	case "string", "number", "integer", "boolean", "null":
	default:
		return command.Fail("UNSUPPORTED_PARAMETER_SCHEMA")
	}
	return nil
}

func ValidateArguments(schema, body []byte) error {
	declaration, e := DecodeJSON(schema)
	if e != nil {
		return command.Fail("INVALID_PARAMETER_SCHEMA")
	}
	if e = checkSchema(declaration, 0); e != nil {
		return e
	}
	value, e := DecodeJSON(body)
	if e != nil {
		return command.Fail("INVALID_ARGUMENTS")
	}
	if _, ok := value.(map[string]any); !ok {
		return command.Fail("INVALID_ARGUMENTS")
	}
	if !concrete(value) || !matches(declaration.(map[string]any), value) {
		return command.Fail("INVALID_ARGUMENTS")
	}
	return nil
}
func concrete(v any) bool {
	switch x := v.(type) {
	case string:
		return !strings.Contains(x, "${") && !strings.Contains(x, "{{") && !strings.HasPrefix(x, "$step.")
	case map[string]any:
		for k, v := range x {
			if k == "$ref" || k == "$var" || k == "$template" || !concrete(v) {
				return false
			}
		}
	case []any:
		for _, v := range x {
			if !concrete(v) {
				return false
			}
		}
	}
	return true
}
func matches(schema map[string]any, value any) bool {
	if values, ok := schema["enum"].([]any); ok {
		found := false
		for _, v := range values {
			found = found || equalJSON(value, v)
		}
		if !found {
			return false
		}
	}
	switch schema["type"] {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return false
		}
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]any)
		for _, key := range required {
			if _, exists := object[key.(string)]; !exists {
				return false
			}
		}
		for key, v := range object {
			if p, exists := properties[key]; exists {
				if !matches(p.(map[string]any), v) {
					return false
				}
			} else if additional, exists := schema["additionalProperties"]; exists && !additional.(bool) {
				return false
			}
		}
		return true
	case "array":
		array, ok := value.([]any)
		if !ok {
			return false
		}
		for _, v := range array {
			if !matches(schema["items"].(map[string]any), v) {
				return false
			}
		}
		return true
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "null":
		return value == nil
	case "number":
		_, ok := value.(json.Number)
		return ok
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return false
		}
		r, ok := new(big.Rat).SetString(string(number))
		return ok && r.IsInt()
	}
	return false
}

// equalJSON 按 JSON 值比较枚举，数值不经 float64 舍入。
func equalJSON(a, b any) bool {
	switch x := a.(type) {
	case json.Number:
		y, ok := b.(json.Number)
		if !ok {
			return false
		}
		left, ok := new(big.Rat).SetString(string(x))
		if !ok {
			return false
		}
		right, ok := new(big.Rat).SetString(string(y))
		return ok && left.Cmp(right) == 0
	case map[string]any:
		y, ok := b.(map[string]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for key, value := range x {
			other, exists := y[key]
			if !exists || !equalJSON(value, other) {
				return false
			}
		}
		return true
	case []any:
		y, ok := b.([]any)
		if !ok || len(x) != len(y) {
			return false
		}
		for i, value := range x {
			if !equalJSON(value, y[i]) {
				return false
			}
		}
		return true
	default:
		return a == b
	}
}
