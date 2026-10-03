// Package contract provides versioned values at the Application and Component boundary.
package contract

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strconv"
	"sync"
	"time"
	"unicode/utf8"

	wire "github.com/ruipengliu/lerna/contract/gen/go"
	"github.com/santhosh-tekuri/jsonschema/v6"
)

const Version = "1.0.0"

var schemas sync.Map
var utcPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}\.[0-9]{6}Z$`)

// Validate rejects values that do not satisfy the named closed schema.
func Validate(name string, value any) error {
	if err := validateWireValue(reflect.ValueOf(value)); err != nil {
		return err
	}

	compiled, ok := schemas.Load(name)
	if !ok {
		compiler := jsonschema.NewCompiler()
		compiler.AssertFormat()
		compiler.RegisterFormat(&jsonschema.Format{Name: "int64-decimal", Validate: func(v any) error {
			s, ok := v.(string)
			if !ok {
				return nil
			}
			_, err := strconv.ParseInt(s, 10, 64)
			return err
		}})
		compiler.RegisterFormat(&jsonschema.Format{Name: "utc-microseconds", Validate: func(v any) error {
			s, ok := v.(string)
			if !ok {
				return nil
			}
			if !utcPattern.MatchString(s) {
				return fmt.Errorf("expected UTC with six microsecond digits")
			}
			parsed, err := time.Parse("2006-01-02T15:04:05.000000Z", s)
			if err == nil && parsed.Year() < 1 {
				return fmt.Errorf("year must be 0001..9999")
			}
			return err
		}})
		var source any
		if err := json.Unmarshal([]byte(wire.SchemaJSON), &source); err != nil {
			return err
		}
		if err := compiler.AddResource("https://lerna.dev/contract/1.0.0/values.json", source); err != nil {
			return err
		}
		schema, err := compiler.Compile("https://lerna.dev/contract/1.0.0/values.json#/$defs/" + name)
		if err != nil {
			return fmt.Errorf("unsupported value type %q: %w", name, err)
		}
		compiled, _ = schemas.LoadOrStore(name, schema)
	}
	if err := compiled.(*jsonschema.Schema).Validate(value); err != nil {
		if responseSchema(name) {
			return refusal("schema_invalid", err)
		}
		return err
	}
	if responseSchema(name) {
		if err := semanticResponse(name, value); err != nil {
			return refusal("schema_invalid", err)
		}
	}
	return nil
}

// Decode validates JSON before producing the public generated value type.
func Decode[T Value](data []byte) (T, error) {
	var result T
	raw, err := ParseJSON(data)
	if err != nil {
		return result, valueError[T](err)
	}
	name := reflect.TypeFor[T]().Name()
	if err := Validate(name, raw); err != nil {
		return result, valueError[T](err)
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return result, valueError[T](err)
	}
	return result, nil
}

// Encode validates a public generated value before serializing it.
func Encode[T Value](value T) ([]byte, error) {
	if err := validateWireValue(reflect.ValueOf(value)); err != nil {
		return nil, valueError[T](err)
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, valueError[T](err)
	}
	if _, err := Decode[T](encoded); err != nil {
		return nil, valueError[T](err)
	}
	return encoded, nil
}

// Generated values contain only strings, booleans, arrays and finite structs.
func validateWireValue(value reflect.Value) error { return wireValue(value, 0, 0) }

func wireValue(value reflect.Value, depth, steps int) error {
	if steps > 4*MaxDepth {
		return fmt.Errorf("cyclic or excessive JSON value indirection")
	}

	if !value.IsValid() {
		return nil
	}
	if value.Type() == reflect.TypeFor[CommandPayload]() {
		_, err := ParseJSON(value.Bytes())
		return err
	}
	switch value.Kind() {
	case reflect.Struct, reflect.Map, reflect.Array, reflect.Slice:
		depth++
		if depth > MaxDepth {
			return fmt.Errorf("JSON nesting exceeds %d", MaxDepth)
		}
	}
	switch value.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr, reflect.Float32, reflect.Float64, reflect.Complex64, reflect.Complex128:
		return fmt.Errorf("JSON numeric values are forbidden by this contract")

	case reflect.String:
		if !utf8.ValidString(value.String()) {
			return fmt.Errorf("invalid UTF-8 in value")
		}
	case reflect.Pointer, reflect.Interface:
		if !value.IsNil() {
			return wireValue(value.Elem(), depth, steps+1)
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			if err := wireValue(value.Field(i), depth, steps+1); err != nil {
				return err
			}
		}
	case reflect.Slice, reflect.Array:
		for i := 0; i < value.Len(); i++ {
			if err := wireValue(value.Index(i), depth, steps+1); err != nil {
				return err
			}
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			if err := wireValue(key, depth, steps+1); err != nil {
				return err
			}
			if err := wireValue(value.MapIndex(key), depth, steps+1); err != nil {
				return err
			}
		}
	case reflect.Bool:
	default:
		return fmt.Errorf("value kind %s is not JSON", value.Kind())
	}
	return nil
}
