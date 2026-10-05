package fingerprint_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/fingerprint"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// 规则：G3
func TestEquivalentIntentHasSameFingerprint(t *testing.T) {
	a := &lernav1.PlanStep{StepId: "s", Arguments: map[string]string{"a": "1", "b": "2", "c": "3"}}
	b := &lernav1.PlanStep{StepId: "s", Arguments: map[string]string{"c": "3", "b": "2", "a": "1"}}
	fa := fingerprint.MustOf("k", a)
	fb := fingerprint.MustOf("k", b)
	if !fingerprint.Equal(fa, fb) {
		t.Fatal("map insertion order must not change the fingerprint")
	}
	b.Arguments["c"] = "4"
	if fingerprint.Equal(fa, fingerprint.MustOf("k", b)) {
		t.Fatal("a semantic change must change the fingerprint")
	}
	if fingerprint.Equal(fa, fingerprint.MustOf("other", a)) {
		t.Fatal("the command kind is part of the fingerprint")
	}
}

// 规则：G3
func TestUnknownFieldsAreRefused(t *testing.T) {
	raw, _ := proto.Marshal(&lernav1.PlanStep{StepId: "s"})
	raw = protowire.AppendTag(raw, 999, protowire.VarintType)
	raw = protowire.AppendVarint(raw, 1)
	m := &lernav1.PlanStep{}
	if err := proto.Unmarshal(raw, m); err != nil {
		t.Fatal(err)
	}
	if _, err := fingerprint.Of("k", m); err == nil {
		t.Fatal("unknown fields must not be silently ignored")
	}
}
