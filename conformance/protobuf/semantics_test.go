package protobuf_test

import (
	"bytes"
	"testing"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G7、G10、G12
func TestBindingPreservesPresencePrecisionAndUnknownFields(t *testing.T) {
	zero := int64(0)
	for _, ceiling := range []*int64{nil, &zero} {
		original := &v1.Capability{FeeCeiling: ceiling}
		b, err := proto.Marshal(original)
		if err != nil {
			t.Fatal(err)
		}
		decoded := &v1.Capability{}
		if err = proto.Unmarshal(b, decoded); err != nil {
			t.Fatal(err)
		}
		if (decoded.FeeCeiling == nil) != (ceiling == nil) {
			t.Fatal("unknown fee became known zero")
		}
	}
	ref := &v1.Ref{Revision: 9007199254740993}
	b, err := protojson.Marshal(ref)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"9007199254740993"`)) {
		t.Fatalf("uint64 lost JSON precision: %s", b)
	}
	decoded := &v1.Ref{}
	if err = protojson.Unmarshal(b, decoded); err != nil || decoded.Revision != ref.Revision {
		t.Fatalf("%v %v", decoded, err)
	}
	// 已知类型遇到新字段可保留字节，但执行入口仍拒绝未理解的安全语义。
	goal := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{UserId: "u", IssuerId: "host", TargetDomainId: "d", CommandId: "future"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "synthetic"}
	goal.Identity.ProtoReflect().SetUnknown([]byte{0x98, 0x06, 0x01})
	b, err = proto.Marshal(goal)
	if err != nil {
		t.Fatal(err)
	}
	roundtrip := &v1.SubmitGoalCommand{}
	if err = proto.Unmarshal(b, roundtrip); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(goal, roundtrip) {
		t.Fatal("binary unknown field lost")
	}
	if err = command.ValidateGoal(roundtrip); err == nil || err.Error() != "UNSUPPORTED_FEATURE" {
		t.Fatalf("unknown nested authority accepted: %v", err)
	}
	if err = protojson.Unmarshal([]byte(`{"futurePermission":true}`), &v1.Grant{}); err == nil {
		t.Fatal("JSON unknown field accepted")
	}
}

// 规则：G1、G7、G12
func TestSemanticValidationIsSeparateFromSuccessfulDecode(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*v1.SubmitGoalCommand)
		code   string
	}{
		{"empty-goal", func(c *v1.SubmitGoalCommand) { c.Goal = "" }, "INVALID_INPUT"},
		{"unknown-schema", func(c *v1.SubmitGoalCommand) { c.SchemaId = "lerna.v99.SubmitGoal" }, "UNSUPPORTED_CONTRACT"},
		{"must-understand", func(c *v1.SubmitGoalCommand) { c.MustUnderstand = []string{"future-authority"} }, "UNSUPPORTED_FEATURE"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := &v1.SubmitGoalCommand{Identity: &v1.CommandIdentity{CommandId: "semantic"}, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "synthetic"}
			tc.change(c)
			wire, err := proto.Marshal(c)
			if err != nil {
				t.Fatal(err)
			}
			d := &v1.SubmitGoalCommand{}
			if err = proto.Unmarshal(wire, d); err != nil {
				t.Fatal(err)
			}
			if err = command.ValidateGoal(d); err == nil || err.Error() != tc.code {
				t.Fatalf("got %v, want %s", err, tc.code)
			}
		})
	}
}
