package component_test

import (
	"encoding/json"
	"errors"
	old "github.com/ruipengliu/lerna/contract"
	contract "github.com/ruipengliu/lerna/contract/v1_1"
	"os"
	"testing"
)

func TestFixedDecisionInputTypedRoundtrip(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.1.0/valid/decide.json")
	if err != nil {
		t.Fatal(err)
	}
	value, err := contract.DecodeDecide(data)
	if err != nil {
		t.Fatal(err)
	}
	if value.Payload.DecisionID != "decision-a" {
		t.Fatal("wrong original identity")
	}
	encoded, err := contract.Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	restored, err := contract.DecodeDecide(encoded)
	if err != nil || restored.Payload.DecisionID != value.Payload.DecisionID {
		t.Fatalf("typed roundtrip: %v", err)
	}
}

func TestDecisionDigestAndIsolatedInventory(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.1.0/digest-input.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Request        contract.DecisionDecideRequest `json:"request"`
		Subject        contract.SubjectBinding        `json:"subject"`
		DecisionDigest string                         `json:"decision_digest"`
		CommandDigest  string                         `json:"command_digest"`
	}
	if err = json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	decision, err := contract.DecisionInputDigest(fixture.Request, fixture.Subject)
	if err != nil || decision != fixture.DecisionDigest {
		t.Fatalf("decision golden %s: %v", decision, err)
	}
	command, err := contract.Encode(fixture.Request)
	if err != nil {
		t.Fatal(err)
	}
	subject, err := contract.Encode(fixture.Subject)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := contract.CommandDigest(command, subject)
	if err != nil || digest != fixture.CommandDigest {
		t.Fatalf("command golden %s: %v", digest, err)
	}
	fixture.Request.CommandID = "another-command"
	fixture.Request.AcceptBefore = "2026-10-04T00:00:09.000000Z"
	repeated, err := contract.DecisionInputDigest(fixture.Request, fixture.Subject)
	if err != nil || repeated != decision {
		t.Fatalf("delivery changed decision identity: %v", err)
	}
	fixture.Subject.SubjectID = "different-subject"
	different, err := contract.DecisionInputDigest(fixture.Request, fixture.Subject)
	if err != nil || different == decision {
		t.Fatalf("principal missing from input identity: %v", err)
	}
	if len(contract.SupportedMethods()) != 1 || len(contract.DeclaredMethods()) != 4 {
		t.Fatal("unfinished profile was advertised or lost typed schemas")
	}
	var goldens []struct {
		Schema string `json:"schema"`
		Digest string `json:"digest"`
	}
	data, err = os.ReadFile("../fixtures/1.1.0/schema-digests.json")
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}
	for i, m := range contract.DeclaredMethods() {
		if string(m.InputSchemaDigest) != goldens[2*i].Digest || string(m.OutputSchemaDigest) != goldens[2*i+1].Digest {
			t.Fatal("reachable schema digest differs from independent golden")
		}
	}
	request := contract.NegotiationRequest{ContractVersion: "1.1.0", Profile: "decision_engine", Method: "decision_engine.decide", InputSchemaDigest: contract.DeclaredMethods()[1].InputSchemaDigest, OutputSchemaDigest: contract.DeclaredMethods()[1].OutputSchemaDigest}
	encoded, err := contract.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	_, err = contract.Negotiate(encoded)
	var refusal *contract.ContractError
	if !errors.As(err, &refusal) || refusal.Code != "unsupported" {
		t.Fatalf("developing profile negotiated: %v", err)
	}
	// Same-named definitions are intentionally distinct in their schema caches.
	if _, err = old.Decode[old.ErrorCode]([]byte(`"decision_mismatch"`)); err == nil {
		t.Fatal("new error leaked into frozen schema")
	}
	if _, err = contract.Decode[contract.ErrorCode]([]byte(`"decision_mismatch"`)); err != nil {
		t.Fatal(err)
	}
}
