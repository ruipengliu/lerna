package v1_2_test

import (
	"encoding/json"
	v "github.com/ruipengliu/lerna/contract/v1_2"
	"os"
	"testing"
)

func TestIndependentCommandDigestGolden(t *testing.T) {
	data, err := os.ReadFile("../../conformance/fixtures/1.2.0/digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var golden struct {
		Command json.RawMessage
		Subject json.RawMessage
		Digest  string
	}
	if err = json.Unmarshal(data, &golden); err != nil {
		t.Fatal(err)
	}
	digest, err := v.CommandDigest(golden.Command, golden.Subject)
	if err != nil || digest != golden.Digest {
		t.Fatal("independent canonical digest differs", digest, err)
	}
	request, err := v.DecodePut(golden.Command)
	if err != nil {
		t.Fatal(err)
	}
	request.TraceContext = &v.TraceContext{TraceID: "different-connection"}
	wire, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	trace, err := v.CommandDigest(wire, golden.Subject)
	if err != nil || trace != digest {
		t.Fatal("trace changed original digest", err)
	}
	request.Payload.Purpose = "different-purpose"
	wire, err = v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	changed, err := v.CommandDigest(wire, golden.Subject)
	if err != nil || changed == digest {
		t.Fatal("business purpose failed to change digest", err)
	}
}
