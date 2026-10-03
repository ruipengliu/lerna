package api

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed schema/core.schema.json
var coreSchema []byte
var coreDefs map[string]any
var schemaOnce sync.Once

func CoreDigest() string { return Hash(coreSchema) }
func definitions() map[string]any {
	schemaOnce.Do(func() {
		var doc map[string]any
		if err := json.Unmarshal(coreSchema, &doc); err != nil {
			panic(err)
		}
		coreDefs = doc["$defs"].(map[string]any)
	})
	return coreDefs
}

type Schema = map[string]any

func Object(properties map[string]any, required ...string) Schema {
	return Schema{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}
func Ref(name string) Schema       { return Schema{"$ref": "#/$defs/" + name} }
func String() Schema               { return Schema{"type": "string", "minLength": 1, "maxLength": 4096} }
func Enum(values ...string) Schema { return Schema{"type": "string", "enum": values} }
func Array(item Schema, min, max int) Schema {
	return Schema{"type": "array", "items": item, "minItems": min, "maxItems": max}
}
func SchemaFor[T any]() Schema { return schemaType(reflect.TypeFor[T](), map[reflect.Type]bool{}) }
func schemaType(t reflect.Type, seen map[reflect.Type]bool) Schema {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.PkgPath() == "github.com/ruipengliu/lerna/api" && t.Name() != "Command" {
		if _, ok := definitions()[t.Name()]; ok {
			return Ref(t.Name())
		}
	}
	if t == reflect.TypeFor[json.RawMessage]() {
		return Schema{}
	}
	switch t.Kind() {
	case reflect.String:
		return Schema{"type": "string", "maxLength": 4096}
	case reflect.Bool:
		return Schema{"type": "boolean"}
	case reflect.Uint, reflect.Uint64, reflect.Int, reflect.Int64, reflect.Uint32, reflect.Int32:
		return Schema{"type": "integer", "minimum": 0, "maximum": MaxSafeInteger}
	case reflect.Float64, reflect.Float32:
		return Schema{"type": "number", "minimum": -float64(MaxSafeInteger), "maximum": float64(MaxSafeInteger)}
	case reflect.Slice, reflect.Array:
		return Array(schemaType(t.Elem(), seen), 0, 100)
	case reflect.Interface:
		return Schema{}
	case reflect.Struct:
		if seen[t] {
			panic("recursive wire type " + t.Name())
		}
		seen[t] = true
		defer delete(seen, t)
		p := map[string]any{}
		req := []string{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			if !f.IsExported() {
				continue
			}
			tag := f.Tag.Get("json")
			if f.Anonymous && tag == "" {
				embedded := schemaType(f.Type, seen)
				if ref, ok := embedded["$ref"].(string); ok {
					embedded = definitions()[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
				}
				if properties, ok := embedded["properties"].(map[string]any); ok {
					for name, value := range properties {
						p[name] = value
					}
					switch values := embedded["required"].(type) {
					case []string:
						req = append(req, values...)
					case []any:
						for _, value := range values {
							req = append(req, value.(string))
						}
					}
					continue
				}
			}
			if tag == "-" {
				continue
			}
			name, opts, _ := strings.Cut(tag, ",")
			if name == "" {
				name = f.Name
			}
			p[name] = schemaType(f.Type, seen)
			if !strings.Contains(opts, "omitempty") {
				req = append(req, name)
			}
		}
		return Object(p, req...)
	default:
		panic("unsupported wire type " + t.String())
	}
}

type Validator struct{ schema *jsonschema.Schema }

func NewValidator(s Schema) (*Validator, error) {
	doc := map[string]any{}
	for k, v := range s {
		doc[k] = v
	}
	doc["$schema"] = "https://json-schema.org/draft/2020-12/schema"
	doc["$defs"] = definitions()
	encoded, err := json.Marshal(doc)
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(encoded, &doc); err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.AssertFormat()
	if err := c.AddResource("https://harness.local/method", doc); err != nil {
		return nil, err
	}
	compiled, err := c.Compile("https://harness.local/method")
	if err != nil {
		return nil, err
	}
	return &Validator{compiled}, nil
}
func (v *Validator) Validate(raw []byte) error {
	x, err := ParseJSON(raw)
	if err != nil {
		return err
	}
	if err = v.schema.Validate(x); err != nil {
		return E("invalid_request", "schema_violation")
	}
	return nil
}
func ValidateRecord(name string, v any) error {
	validator, err := NewValidator(Ref(name))
	if err != nil {
		return err
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return validator.Validate(b)
}
func Raw(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("wire value: %w", err))
	}
	return b
}
func Equal(a, b any) bool {
	x, err := Canonical(Raw(a))
	if err != nil {
		return false
	}
	y, err := Canonical(Raw(b))
	return err == nil && bytes.Equal(x, y)
}

type MethodContract struct {
	Name           string `json:"name"`
	Owner          string `json:"owner"`
	Kind           string `json:"kind"`
	CAS            bool   `json:"cas"`
	AllowsAccepted bool   `json:"allows_accepted"`
	InputSchema    Schema `json:"input_schema"`
	OutputSchema   Schema `json:"output_schema"`
	SchemaDigest   string `json:"schema_digest"`
	Recovery       string `json:"recovery"`
}

func Contract[I, O any](name, owner, kind string, cas, accepted bool) MethodContract {
	return MethodContract{Name: name, Owner: owner, Kind: kind, CAS: cas, AllowsAccepted: accepted, InputSchema: SchemaFor[I](), OutputSchema: SchemaFor[O](), Recovery: "query original command/object; preserve exact owner, payload, revision and deadlines"}
}
