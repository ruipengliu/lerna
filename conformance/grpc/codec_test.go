package grpc_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
	"google.golang.org/protobuf/encoding/protowire"
)

func field(number protowire.Number, body []byte) []byte {
	b := protowire.AppendTag(nil, number, protowire.BytesType)
	return protowire.AppendBytes(b, body)
}
func validCall() []byte {
	b := field(1, []byte("service_12345678901234567890123456789012"))
	return append(b, field(2, []byte(`{"b":2,"a":1}`))...)
}
func TestStrictProtoCodecPreservesExactJSONAndCanonicalDigest(t *testing.T) {
	var request rpcv1.CallRequest
	c := grpcwire.Codec{}
	if err := c.Unmarshal(validCall(), &request); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(request.GetCommandJson(), []byte(`{"b":2,"a":1}`)) {
		t.Fatal("transport changed exact domain JSON")
	}
	a, _ := api.Canonical(request.GetCommandJson())
	b, _ := api.Canonical([]byte(`{"a":1,"b":2}`))
	if api.Hash(a) != api.Hash(b) {
		t.Fatal("transport changed JCS semantics")
	}
	encoded, err := c.Marshal(&request)
	if err != nil || !bytes.Equal(encoded, validCall()) {
		t.Fatalf("round trip: %v", err)
	}
}
func TestStrictProtoCodecRejectsAmbiguousOrIllegalEnvelopes(t *testing.T) {
	tests := map[string][]byte{
		"unknown":                   append(validCall(), field(9, []byte("x"))...),
		"duplicate_singular":        append(validCall(), field(1, []byte("service_12345678901234567890123456789012"))...),
		"duplicate_same_oneof":      append(validCall(), field(2, []byte(`{}`))...),
		"duplicate_different_oneof": append(validCall(), field(3, []byte(`{}`))...),
		"invalid_utf8":              append(field(1, []byte{0xff}), field(2, []byte(`{}`))...),
		"duplicate_json_key":        append(field(1, []byte("service_12345678901234567890123456789012")), field(2, []byte(`{"a":1,"a":2}`))...),
		"invalid_json_utf8":         append(field(1, []byte("service_12345678901234567890123456789012")), field(2, []byte{'"', 0xff, '"'})...),
		"wrong_wire_type":           protowire.AppendVarint(protowire.AppendTag(nil, 1, protowire.VarintType), 1),
		"truncated":                 validCall()[:len(validCall())-1],
		"missing_request":           field(1, []byte("service_12345678901234567890123456789012")),
	}
	for name, wire := range tests {
		t.Run(name, func(t *testing.T) {
			if err := (grpcwire.Codec{}).Unmarshal(wire, &rpcv1.CallRequest{}); err == nil {
				t.Fatal("illegal wire admitted")
			}
		})
	}
}
