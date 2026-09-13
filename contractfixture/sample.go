package contractfixture

import (
	_ "embed"
	wire "lerna/gen/harness/v1"
	"lerna/schema"
)

//go:embed sample.schema.json
var sampleSchema []byte

func SampleResource() schema.Resource {
	return schema.Resource{Type: "sample.input", ID: "https://harness.invalid/contracts/sample-input", Version: "1", Document: append([]byte(nil), sampleSchema...)}
}

func SamplePayload(raw string) *wire.DynamicPayload {
	r := SampleResource()
	return &wire.DynamicPayload{TypeName: r.Type, SchemaId: r.ID, SchemaVersion: r.Version, SchemaDigest: schema.Digest(r.Document), Json: []byte(raw)}
}
