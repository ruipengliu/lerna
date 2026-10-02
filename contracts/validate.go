package contracts

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed schemas/protocol.schema.json
var schemaJSON []byte

var schema = sync.OnceValue(func() map[string]any {
	var doc map[string]any
	d := json.NewDecoder(bytes.NewReader(schemaJSON))
	d.UseNumber()
	if err := d.Decode(&doc); err != nil {
		panic(err)
	}
	return doc["$defs"].(map[string]any)
})
var patterns sync.Map
var ErrSchema = errors.New("contract: schema validation failed")

// Validate checks the frozen local schema profile. Callers validate original
// bytes (duplicates, Unicode, safe numbers) before invoking this structural step.
// This is not an arbitrary remote-schema compiler; references are local only.
func Validate(definition string, raw []byte) error {
	s, ok := schema()[definition].(map[string]any)
	if !ok || len(raw) == 0 || len(raw) > 1<<20 {
		return ErrSchema
	}
	var value any
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if err := d.Decode(&value); err != nil {
		return ErrSchema
	}
	budget := 100000
	if err := validate(s, value, 0, &budget); err != nil {
		return fmt.Errorf("%w: %s", ErrSchema, definition)
	}
	return nil
}
func number(v any) (*big.Rat, bool) {
	x, ok := v.(json.Number)
	if !ok {
		return nil, false
	}
	r, ok := new(big.Rat).SetString(string(x))
	return r, ok
}
func bound(s map[string]any, key string) (int, bool) {
	v, ok := s[key].(json.Number)
	if !ok {
		return 0, false
	}
	n, err := v.Int64()
	return int(n), err == nil
}
func validate(s map[string]any, v any, depth int, budget *int) error {
	*budget--
	if depth > 64 || *budget < 0 {
		return ErrSchema
	}
	if ref, ok := s["$ref"].(string); ok {
		if !strings.HasPrefix(ref, "#/$defs/") {
			return ErrSchema
		}
		next, ok := schema()[strings.TrimPrefix(ref, "#/$defs/")].(map[string]any)
		if !ok {
			return ErrSchema
		}
		if err := validate(next, v, depth+1, budget); err != nil {
			return err
		}
	}
	for _, key := range []string{"allOf", "anyOf", "oneOf"} {
		if list, ok := s[key].([]any); ok {
			passed := 0
			for _, child := range list {
				if validate(child.(map[string]any), v, depth+1, budget) == nil {
					passed++
				}
			}
			if key == "allOf" && passed != len(list) || key == "anyOf" && passed == 0 || key == "oneOf" && passed != 1 {
				return ErrSchema
			}
		}
	}
	if n, ok := s["not"].(map[string]any); ok && validate(n, v, depth+1, budget) == nil {
		return ErrSchema
	}
	if cond, ok := s["if"].(map[string]any); ok {
		branch := "else"
		if validate(cond, v, depth+1, budget) == nil {
			branch = "then"
		}
		if child, ok := s[branch].(map[string]any); ok {
			if err := validate(child, v, depth+1, budget); err != nil {
				return err
			}
		}
	}
	if c, ok := s["const"]; ok && !reflect.DeepEqual(c, v) {
		return ErrSchema
	}
	if choices, ok := s["enum"].([]any); ok {
		found := false
		for _, c := range choices {
			found = found || reflect.DeepEqual(c, v)
		}
		if !found {
			return ErrSchema
		}
	}
	if t, ok := s["type"].(string); ok {
		matches := false
		switch t {
		case "null":
			matches = v == nil
		case "object":
			_, matches = v.(map[string]any)
		case "array":
			_, matches = v.([]any)
		case "string":
			_, matches = v.(string)
		case "boolean":
			_, matches = v.(bool)
		case "number":
			_, matches = number(v)
		case "integer":
			n, good := number(v)
			matches = good && n.IsInt()
		default:
			return ErrSchema
		}
		if !matches {
			return ErrSchema
		}
	}
	if obj, ok := v.(map[string]any); ok {
		if required, ok := s["required"].([]any); ok {
			for _, k := range required {
				if _, exists := obj[k.(string)]; !exists {
					return ErrSchema
				}
			}
		}
		props, _ := s["properties"].(map[string]any)
		for k, value := range obj {
			if p, ok := props[k].(map[string]any); ok {
				if err := validate(p, value, depth+1, budget); err != nil {
					return err
				}
			} else if additional, exists := s["additionalProperties"]; exists {
				if allowed, ok := additional.(bool); ok && !allowed {
					return ErrSchema
				}
				if p, ok := additional.(map[string]any); ok {
					if err := validate(p, value, depth+1, budget); err != nil {
						return err
					}
				}
			}
		}
	}
	if a, ok := v.([]any); ok {
		if n, ok := bound(s, "minItems"); ok && len(a) < n {
			return ErrSchema
		}
		if n, ok := bound(s, "maxItems"); ok && len(a) > n {
			return ErrSchema
		}
		if unique, _ := s["uniqueItems"].(bool); unique {
			seen := map[string]bool{}
			for _, item := range a {
				b, _ := json.Marshal(item)
				if seen[string(b)] {
					return ErrSchema
				}
				seen[string(b)] = true
			}
		}
		if item, ok := s["items"].(map[string]any); ok {
			for _, value := range a {
				if err := validate(item, value, depth+1, budget); err != nil {
					return err
				}
			}
		}
	}
	if x, ok := v.(string); ok {
		if n, ok := bound(s, "minLength"); ok && utf8.RuneCountInString(x) < n {
			return ErrSchema
		}
		if n, ok := bound(s, "maxLength"); ok && utf8.RuneCountInString(x) > n {
			return ErrSchema
		}
		if p, ok := s["pattern"].(string); ok {
			cached, exists := patterns.Load(p)
			if !exists {
				compiled, err := regexp.Compile(p)
				if err != nil {
					return ErrSchema
				}
				cached, _ = patterns.LoadOrStore(p, compiled)
			}
			if !cached.(*regexp.Regexp).MatchString(x) {
				return ErrSchema
			}
		}
		if f, _ := s["format"].(string); f != "" {
			if f != "date-time" {
				return ErrSchema
			}
			if _, err := time.Parse(time.RFC3339Nano, x); err != nil {
				return ErrSchema
			}
		}
	}
	if n, ok := number(v); ok {
		for _, key := range []string{"minimum", "maximum", "exclusiveMinimum", "exclusiveMaximum"} {
			b, ok := number(s[key])
			if !ok {
				continue
			}
			comparison := n.Cmp(b)
			if key == "minimum" && comparison < 0 || key == "maximum" && comparison > 0 || key == "exclusiveMinimum" && comparison <= 0 || key == "exclusiveMaximum" && comparison >= 0 {
				return ErrSchema
			}
		}
	}
	return nil
}
