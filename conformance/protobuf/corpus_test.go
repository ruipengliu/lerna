package protobuf_test

import (
	"testing"

	"github.com/ruipengliu/lerna/conformance/protobuf"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

// 规则：G1、G3、G12
func TestCapturedObjectsRoundTrip(t *testing.T) {
	samples, err := protobuf.Load("testdata/mainline.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(samples) == 0 {
		t.Fatal("no real objects captured")
	}
	for _, s := range samples {
		t.Run(s.Name, func(t *testing.T) {
			wire, err := proto.Marshal(s.Message)
			if err != nil {
				t.Fatal(err)
			}
			decoded := s.Message.ProtoReflect().Type().New().Interface()
			if err := proto.Unmarshal(wire, decoded); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(s.Message, decoded) {
				t.Fatal("public object changed across wire")
			}
			jsonWire, err := protojson.Marshal(s.Message)
			if err != nil {
				t.Fatal(err)
			}
			jsonDecoded := s.Message.ProtoReflect().Type().New().Interface()
			if err = protojson.Unmarshal(jsonWire, jsonDecoded); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(s.Message, jsonDecoded) {
				t.Fatal("public object changed across ProtoJSON")
			}
		})
	}
}

// 规则：G1、G12、R6
func TestReportDistinguishesGeneratedBindingsFromExercisedObjects(t *testing.T) {
	samples, err := protobuf.Load("testdata/mainline.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := protobuf.Inspect(samples)
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaSHA256 == "" || report.GeneratedBindings == 0 {
		t.Fatal("binding inventory absent")
	}
	partial, err := protobuf.Inspect(samples[:1])
	if err != nil {
		t.Fatal(err)
	}
	if partial.SampledTypes >= partial.GeneratedBindings || len(partial.UnsampledTypes) == 0 {
		t.Fatal("partial mainline falsely reported complete")
	}
	found := false
	for _, s := range report.Samples {
		if s.Type == "lerna.v1.Result" {
			found = true
			if s.BinaryBytes <= 0 || s.JSONBytes <= s.BinaryBytes {
				t.Fatal("missing measured Result sizes")
			}
		}
	}
	if !found {
		t.Fatal("real Result not measured")
	}
}

// 规则：G3、G7、G10、G11
func TestCapturedCurrentAuthorityAndBillingVariants(t *testing.T) {
	samples, err := protobuf.Load("testdata/mainline.json")
	if err != nil {
		t.Fatal(err)
	}
	required := map[string]bool{"file-root": false, "file-bind-root-command-CREATE": false, "file-register-resources-command-CREATE": false, "file-io-request-CREATE": false, "file-io-result-REPLACE": false, "file-observation-READ": false, "file-history-original": false, "file-resources-CREATE": false, "resend-command": false, "resend-original-send": false, "resend-historical-operation": false, "trace-source-pending": false, "trace-source-acknowledged": false, "trace-view-gap": false, "trace-cutoff-query": false, "reconciliation-completed": false, "reconciliation-finding": false, "closure-confirmation-command": false, "reconciliation-progress-handoff": false, "reconciliation-weak-finding": false, "model-call-completed": false, "model-outcome-accepted": false, "model-input-published": false, "model-request-stopped": false, "content-body-receipt": false, "content-derived-published": false, "content-derivation-taken-over": false, "content-version-two": false, "physical-io-request": false, "physical-io-result": false, "confirmation-consumed": false, "confirmation-withdrawn": false, "grant-issuance": false, "revocation-complete": false, "reservation-release": false, "billing-conflict": false, "billing-entry": false}
	for _, name := range []string{"pending-goal-payload", "pending-goal-claim-receipt", "pending-goal-claim-replay", "default-model-api-driver", "default-model-api-result", "default-model-api-request-0-proposal-full", "default-model-api-request-1-proposal-body", "api-native-RATE-wait", "api-native-CONCURRENCY-wait", "api-native-RESOURCE_CONFLICT-wait", "api-native-UNKNOWN-wait", "api-native-REDACTED-observation"} {
		required[name] = false
	}
	for _, s := range samples {
		if _, ok := required[s.Name]; ok {
			required[s.Name] = true
		}
	}
	for name, seen := range required {
		if !seen {
			t.Errorf("missing real scenario: %s", name)
		}
	}
}
