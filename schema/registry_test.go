package schema_test

import (
	"testing"

	wire "lerna/gen/harness/v1"
	"lerna/schema"
)

const sample = `{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://fixtures.invalid/input","type":"object","properties":{"count":{"type":"integer","minimum":0,"maximum":9007199254740991},"large":{"type":"string","pattern":"^(0|[1-9][0-9]*)$"},"note":{"type":["string","null"]},"when":{"type":"string","format":"date-time"}},"required":["count"],"additionalProperties":false}`

func TestRegisteredSchemaValidatesExactNumbersAndNullableFields(t *testing.T) {
	r, err := schema.New([]schema.Resource{{Type: "sample.input", ID: "https://fixtures.invalid/input", Version: "1", Document: []byte(sample)}})
	if err != nil {
		t.Fatal(err)
	}
	p := &wire.DynamicPayload{TypeName: "sample.input", SchemaId: "https://fixtures.invalid/input", SchemaVersion: "1", SchemaDigest: schema.Digest([]byte(sample)), Json: []byte(`{"count":3,"large":"9007199254740993","note":null,"when":"2026-09-10T08:00:00Z"}`)}
	if err := r.Validate(p); err != nil {
		t.Fatal(err)
	}
}

func TestSchemaRejectsAmbiguousAndInvalidJSON(t *testing.T) {
	r, err := schema.New([]schema.Resource{{Type: "sample.input", ID: "https://fixtures.invalid/input", Version: "1", Document: []byte(sample)}})
	if err != nil {
		t.Fatal(err)
	}
	for name, raw := range map[string]string{
		"duplicate":                   `{"count":1,"count":2}`,
		"escaped duplicate":           `{"count":1,"co\u0075nt":2}`,
		"missing":                     `{}`,
		"null required":               `{"count":null}`,
		"unsafe integer":              `{"count":9007199254740992}`,
		"fraction rounded to integer": `{"count":1.00000000000000001}`,
		"wrong large type":            `{"count":1,"large":9007199254740993}`,
		"invalid date":                `{"count":1,"when":"2026-02-30T00:00:00Z"}`,
		"missing timezone":            `{"count":1,"when":"2026-09-10T00:00:00"}`,
		"trailing value":              `{"count":1} {}`,
		"invalid utf8":                "{\"count\":1,\"note\":\"\xff\"}",
	} {
		t.Run(name, func(t *testing.T) {
			p := &wire.DynamicPayload{TypeName: "sample.input", SchemaId: "https://fixtures.invalid/input", SchemaVersion: "1", SchemaDigest: schema.Digest([]byte(sample)), Json: []byte(raw)}
			if err := r.Validate(p); err == nil {
				t.Fatal("invalid payload accepted")
			}
		})
	}
}

func TestRegistryBindingAndOfflineReferences(t *testing.T) {
	resource := schema.Resource{Type: "sample.input", ID: "https://fixtures.invalid/input", Version: "1", Document: []byte(sample)}
	r, err := schema.New([]schema.Resource{resource})
	if err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*wire.DynamicPayload){
		"unknown required type": func(p *wire.DynamicPayload) { p.TypeName = "unknown" },
		"ID":                    func(p *wire.DynamicPayload) { p.SchemaId = "https://elsewhere.invalid/input" },
		"version":               func(p *wire.DynamicPayload) { p.SchemaVersion = "2" },
		"digest":                func(p *wire.DynamicPayload) { p.SchemaDigest = "sha256:wrong" },
	} {
		t.Run(name, func(t *testing.T) {
			p := &wire.DynamicPayload{TypeName: resource.Type, SchemaId: resource.ID, SchemaVersion: resource.Version, SchemaDigest: schema.Digest(resource.Document), Json: []byte(`{"count":1}`)}
			mutate(p)
			if err := r.Validate(p); err == nil {
				t.Fatal("invalid binding accepted")
			}
		})
	}
	ref := schema.Resource{Type: "ref", ID: "https://fixtures.invalid/ref", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://fixtures.invalid/ref","$ref":"https://fixtures.invalid/input"}`)}
	if _, err := schema.New([]schema.Resource{ref}); err == nil {
		t.Fatal("unregistered reference accepted")
	}
	if _, err := schema.New([]schema.Resource{ref, resource}); err != nil {
		t.Fatalf("registered reference rejected: %v", err)
	}
	duplicate := resource
	duplicate.Document = []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://fixtures.invalid/input","type":"object","type":"string"}`)
	if _, err := schema.New([]schema.Resource{duplicate}); err == nil {
		t.Fatal("duplicate schema keys accepted")
	}
	if _, err := schema.New([]schema.Resource{resource, resource}); err == nil {
		t.Fatal("duplicate registry identity accepted")
	}
}
