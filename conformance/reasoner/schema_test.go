package reasoner_test

import (
	"testing"

	"github.com/ruipengliu/lerna/contracts/reasoner"
)

// 规则：G4、G6、准入-2
func TestConcreteSchemaChecksNestedArgumentsWithoutFloatLoss(t *testing.T) {
	schema := []byte(`{"type":"object","properties":{"count":{"type":"integer","enum":[100]},"items":{"type":"array","items":{"type":"object","properties":{"id":{"type":"integer"}},"required":["id"],"additionalProperties":false}}},"required":["count","items"],"additionalProperties":false}`)
	for _, value := range []string{`{"count":100,"items":[{"id":9007199254740993}]}`, `{"count":1e2,"items":[{"id":-9223372036854775809}]}`} {
		if e := reasoner.ValidateArguments(schema, []byte(value)); e != nil {
			t.Fatalf("concrete arguments %s: %v", value, e)
		}
	}
	for _, value := range []string{`{"count":100,"items":[{"id":1.01}]}`, `{"count":100,"items":[{"id":1,"extra":true}]}`, `{"count":100,"items":[{"id":1,"id":2}]}`, `{"count":100,"items":[{"id":"${prior.value}"}]}`, `{"count":100,"items":[],"extra":null}`} {
		if e := reasoner.ValidateArguments(schema, []byte(value)); e == nil {
			t.Fatalf("invalid arguments %s", value)
		}
	}
}

// 规则：G4、G6、准入-2
func TestParameterSchemaRejectsUnsupportedAndMalformedDeclarations(t *testing.T) {
	for _, schema := range []string{`{"type":"number"}`, `{"type":"object","minimum":1}`, `{"type":"object","properties":{"x":{"type":"array"}}}`, `{"type":"object","required":["missing"]}`, `{"type":"object","properties":{"x":{"type":"string"}},"required":["x","x"]}`, `{"type":"object","additionalProperties":{"type":"string"}}`} {
		if _, _, e := reasoner.NormalizeSchema([]byte(schema)); e == nil {
			t.Fatalf("invalid schema %s", schema)
		}
	}
	first, digest, e := reasoner.NormalizeSchema([]byte(`{"type":"object","additionalProperties":false}`))
	if e != nil {
		t.Fatal(e)
	}
	second, again, e := reasoner.NormalizeSchema([]byte(`{ "additionalProperties": false, "type": "object" }`))
	if e != nil || string(first) != string(second) || digest != again {
		t.Fatal("unstable schema normalization")
	}
}
