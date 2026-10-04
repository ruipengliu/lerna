package component_test

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	contract "github.com/ruipengliu/lerna/contract/v1_1"
)

// Names and profiles come from the public method contract, fingerprints from
// its independent shared goldens, never from the advertised implementation.
func completedDecisionMethods(t *testing.T) []contract.MethodSupport {
	t.Helper()
	data, err := os.ReadFile("../fixtures/1.1.0/schema-digests.json")
	if err != nil {
		t.Fatal(err)
	}
	var goldens []struct {
		Schema contract.ID           `json:"schema"`
		Digest contract.SchemaDigest `json:"digest"`
	}
	if err := json.Unmarshal(data, &goldens); err != nil {
		t.Fatal(err)
	}
	names := []struct{ profile, method, input, output string }{
		{"command", "command.get", "CommandGetRequest", "CommandGetResponse"},
		{"decision_engine", "decision_engine.decide", "DecisionDecideRequest", "CommandReceipt"},
		{"decision_engine", "decision_engine.get", "DecisionGetRequest", "DecisionGetResponse"},
		{"decision_engine", "decision_engine.cancel", "DecisionCancelRequest", "CommandReceipt"},
	}
	if len(goldens) != 2*len(names) {
		t.Fatal("incomplete independent schema fingerprints")
	}
	var methods []contract.MethodSupport
	for i, name := range names {
		if string(goldens[2*i].Schema) != name.input || string(goldens[2*i+1].Schema) != name.output {
			t.Fatal("independent schema inventory changed")
		}
		methods = append(methods, contract.MethodSupport{
			ContractVersion: "1.1.0", Profile: contract.ProfileName(name.profile), Method: contract.MethodName(name.method),
			InputSchema: contract.ID(name.input), OutputSchema: contract.ID(name.output),
			InputSchemaDigest: goldens[2*i].Digest, OutputSchemaDigest: goldens[2*i+1].Digest,
		})
	}
	return methods
}

func negotiationFor(method contract.MethodSupport) contract.NegotiationRequest {
	return contract.NegotiationRequest{
		ContractVersion: method.ContractVersion, Profile: method.Profile, Method: method.Method,
		InputSchemaDigest: method.InputSchemaDigest, OutputSchemaDigest: method.OutputSchemaDigest,
	}
}

func TestCompletedDecisionContractNegotiatesAllMethods(t *testing.T) {
	expected := completedDecisionMethods(t)
	// Compare the exact set: descriptor order is not a compatibility promise.
	got := contract.SupportedMethods()
	if len(got) != len(expected) {
		t.Errorf("advertised %d methods, want all %d completed methods", len(got), len(expected))
	}
	for _, method := range expected {
		t.Run(string(method.Method), func(t *testing.T) {
			matches := 0
			for _, supported := range got {
				if supported == method {
					matches++
				}
			}
			if matches != 1 {
				t.Errorf("descriptor must be advertised exactly once: %+v", method)
			}
			// Raw public input, without using the same encoder as negotiation's decoder.
			data, err := json.Marshal(negotiationFor(method))
			if err != nil {
				t.Fatal(err)
			}
			negotiated, err := contract.Negotiate(data)
			if err != nil || !reflect.DeepEqual(negotiated, method) {
				t.Fatalf("exact version/profile/both fingerprints: got %+v, want %+v: %v", negotiated, method, err)
			}
		})
	}
}

func TestCompletedDecisionContractRefusesInexactNegotiation(t *testing.T) {
	const wrongDigest contract.SchemaDigest = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	for _, method := range completedDecisionMethods(t) {
		for _, invalid := range []struct {
			name, code string
			change     func(*contract.NegotiationRequest)
		}{
			{"version", "version_unsupported", func(r *contract.NegotiationRequest) { r.ContractVersion = "1.0.0" }},
			{"profile", "unsupported", func(r *contract.NegotiationRequest) {
				if r.Profile == "command" {
					r.Profile = "decision_engine"
				} else {
					r.Profile = "command"
				}
			}},
			{"input_digest", "version_unsupported", func(r *contract.NegotiationRequest) { r.InputSchemaDigest = wrongDigest }},
			{"output_digest", "version_unsupported", func(r *contract.NegotiationRequest) { r.OutputSchemaDigest = wrongDigest }},
			{"both_digests", "version_unsupported", func(r *contract.NegotiationRequest) {
				r.InputSchemaDigest, r.OutputSchemaDigest = wrongDigest, wrongDigest
			}},
			{"missing_method", "unsupported", func(r *contract.NegotiationRequest) { r.Method = "decision_engine.unknown" }},
		} {
			t.Run(string(method.Method)+"/"+invalid.name, func(t *testing.T) {
				request := negotiationFor(method)
				invalid.change(&request)
				data, err := json.Marshal(request)
				if err != nil {
					t.Fatal(err)
				}
				got, err := contract.Negotiate(data)
				var refusal *contract.ContractError
				if !errors.As(err, &refusal) || string(refusal.Code) != invalid.code || got != (contract.MethodSupport{}) {
					t.Fatalf("inexact negotiation returned %+v: %v, want %s", got, err, invalid.code)
				}
			})
		}
	}
}
