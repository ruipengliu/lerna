// Package sdkcontract defines the first executable contract profile.
package sdkcontract

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/protobuf/proto"
	"lerna/conformance"
	"lerna/contractfixture"
	wire "lerna/gen/harness/v1"
	"lerna/protocol"
	"lerna/schema"
	"lerna/sdk"
)

const ID = "sdk-contract-v1"

func Profile() (conformance.Profile, error) {
	resource := contractfixture.SampleResource()
	registry, err := schema.New([]schema.Resource{resource})
	if err != nil {
		return conformance.Profile{}, err
	}
	p := conformance.Profile{
		ID: ID, Version: "1", Method: "go run ./cmd/contractcheck -profile sdk-contract-v1",
		Configuration: map[string]string{"protocol_major": "1", "schema_dialect": schema.Dialect, "sample_schema_digest": schema.Digest(resource.Document), "topology": "single process; fresh in-memory contract fixture per case", "max_message_bytes": "2097152", "max_json_bytes": "1048576", "max_json_depth": "64", "numeric_token_limit": "128 characters; exponent [-308,308]", "toolchain_target": "go1.26.1; protoc 36.1; protoc-gen-go v1.36.11"},
		Limitations:   []string{"Contract fixture evidence only; no durable acceptance, authorization, task execution or external effects.", "No real gRPC/WebSocket, device, model, MCP/A2A or multi-language interop validation.", "Generation and compilation evidence is recorded by scripts/verify.sh separately; this runner does not claim to execute either.", "Only the sample protocol subset and registered dynamic schema are supported; this is not full M1 qualification."},
	}
	add := func(id, input, expected string, check func(context.Context) (string, error)) {
		p.Cases = append(p.Cases, conformance.Case{ID: id, Evidence: "contract_fixture", Input: input, Expected: expected, Required: true, Check: check})
	}
	add("sdk-roundtrip", "sample task through SDK and binary Protobuf transport", "correlated fixture task", func(ctx context.Context) (string, error) {
		result, err := sdk.NewClient(contractfixture.New(registry), registry).Submit(ctx, submission("m1", "o1", `{"count":1}`))
		if err != nil {
			return "", err
		}
		if result.ReplyTo != "m1" || result.MessageID == "" || result.MessageID == "m1" || result.TaskID == "" || result.Evidence != "contract_fixture" {
			return "invalid response", nil
		}
		return "correlated fixture task", nil
	})
	for _, name := range []string{"same-message", "same-operation-new-message", "new-operation", "intent-conflict", "message-conflict", "semantic-json-equivalence", "namespace-isolation"} {
		add(name, name+" after accepted sample request", "identity semantics preserved", func(ctx context.Context) (string, error) {
			return identityCase(ctx, registry, name)
		})
	}
	for _, tc := range []struct{ id, raw, expected string }{
		{"valid-null-and-large", `{"count":1,"large":"9007199254740993","note":null,"when":"2026-09-10T08:00:00Z"}`, "accepted"},
		{"valid-missing-optional", `{"count":0}`, "accepted"},
		{"duplicate-key", `{"count":1,"count":2}`, "rejected"},
		{"escaped-duplicate-key", `{"count":1,"co\u0075nt":2}`, "rejected"},
		{"nested-duplicate-key", `{"count":1,"note":{"a":1,"a":2}}`, "rejected"},
		{"missing-required", `{}`, "rejected"},
		{"null-required", `{"count":null}`, "rejected"},
		{"unsafe-integer", `{"count":9007199254740992}`, "rejected"},
		{"unrounded-fraction", `{"count":1.00000000000000001}`, "rejected"},
		{"large-as-number", `{"count":1,"large":9007199254740993}`, "rejected"},
		{"invalid-time", `{"count":1,"when":"2026-02-30T08:00:00Z"}`, "rejected"},
		{"missing-timezone", `{"count":1,"when":"2026-09-10T08:00:00"}`, "rejected"},
		{"trailing-json", `{"count":1} {}`, "rejected"},
		{"invalid-utf8", "{\"count\":1,\"note\":\"\xff\"}", "rejected"},
	} {
		add(tc.id, fmt.Sprintf("JSON bytes: %q", tc.raw), tc.expected, func(context.Context) (string, error) {
			return validationOutcome(registry.Validate(contractfixture.SamplePayload(tc.raw))), nil
		})
	}
	for _, field := range []string{"type", "schema-id", "schema-version", "schema-digest"} {
		add("unknown-"+field, "sample payload with mismatched "+field, "rejected", func(context.Context) (string, error) {
			payload := contractfixture.SamplePayload(`{"count":1}`)
			switch field {
			case "type":
				payload.TypeName = "unknown.required"
			case "schema-id":
				payload.SchemaId = "https://unknown.invalid/schema"
			case "schema-version":
				payload.SchemaVersion = "2"
			case "schema-digest":
				payload.SchemaDigest = "sha256:wrong"
			}
			return validationOutcome(registry.Validate(payload)), nil
		})
	}
	add("unregistered-reference", "schema referring to unregistered HTTPS resource", "rejected", func(context.Context) (string, error) {
		r := schema.Resource{Type: "remote", ID: "https://harness.invalid/remote", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"https://harness.invalid/remote","$ref":"https://unregistered.invalid/schema"}`)}
		_, err := schema.New([]schema.Resource{r})
		return validationOutcome(err), nil
	})
	for _, field := range []string{"goal-absent", "reply-to-present", "unknown-body", "version", "oversized"} {
		add("invalid-envelope-"+field, field, "rejected", func(ctx context.Context) (string, error) {
			e := &wire.Envelope{ProtocolMajor: 1, Namespace: "sample", MessageId: "m1", OperationId: "o1", Body: &wire.Envelope_Request{Request: &wire.SubmitRequest{Goal: proto.String("sample")}}}
			switch field {
			case "goal-absent":
				e.GetRequest().Goal = nil
			case "reply-to-present":
				e.ReplyTo = proto.String("")
			case "unknown-body":
				e.Body = nil
			case "version":
				e.ProtocolMajor = 99
			case "oversized":
				e.GetRequest().Goal = proto.String(strings.Repeat("x", protocol.MaxMessageBytes))
			}
			raw, err := proto.Marshal(e)
			if err != nil {
				return "", err
			}
			_, err = contractfixture.New(registry).Exchange(ctx, raw)
			return validationOutcome(err), nil
		})
	}
	p.Cases = append(p.Cases,
		conformance.Case{ID: "real-task-runtime", Evidence: "real_runtime", Availability: conformance.NotImplemented, Input: "requires subsequent implementation tickets", Expected: "real task evidence"},
		conformance.Case{ID: "cross-process-interop", Evidence: "real_transport", Availability: conformance.NotRun, Input: "gRPC/WebSocket bindings", Expected: "equivalent observed behavior"},
		conformance.Case{ID: "other-protocol-major", Evidence: "compatibility", Availability: conformance.Unsupported, Input: "unimplemented protocol versions", Expected: "separate version profile"},
	)
	return p, nil
}

func validationOutcome(err error) string {
	if err != nil {
		return "rejected"
	}
	return "accepted"
}
func submission(message, operation, raw string) sdk.Submission {
	return sdk.Submission{Namespace: "sample", MessageID: message, OperationID: operation, Goal: "sample task", Input: contractfixture.SamplePayload(raw)}
}
func identityCase(ctx context.Context, registry *schema.Registry, name string) (string, error) {
	client := sdk.NewClient(contractfixture.New(registry), registry)
	in := submission("m1", "o1", `{"count":1,"note":null}`)
	first, err := client.Submit(ctx, in)
	if err != nil {
		return "", err
	}
	switch name {
	case "same-operation-new-message":
		in.MessageID = "m2"
	case "new-operation":
		in.MessageID = "m2"
		in.OperationID = "o2"
	case "intent-conflict":
		in.MessageID = "m2"
		in.Goal = "different"
	case "message-conflict":
		in.OperationID = "o2"
	case "semantic-json-equivalence":
		in.MessageID = "m2"
		in.Input = contractfixture.SamplePayload(`{ "note":null, "count":1e0 }`)
	case "namespace-isolation":
		in.Namespace = "other"
	}
	second, err := client.Submit(ctx, in)
	if name == "intent-conflict" || name == "message-conflict" {
		var failure *protocol.Error
		if errors.As(err, &failure) && failure.Code == wire.ErrorCode_ERROR_CODE_IDENTITY_CONFLICT {
			return "identity semantics preserved", nil
		}
		return "expected identity conflict", err
	}
	if err != nil {
		return "", err
	}
	if second.ReplyTo != in.MessageID || second.Evidence != "contract_fixture" {
		return "wrong response association", nil
	}
	if name == "same-message" && first != second {
		return "resend changed response", nil
	}
	newTask := name == "new-operation" || name == "namespace-isolation"
	if (first.TaskID != second.TaskID) != newTask {
		return "wrong task identity", nil
	}
	if in.MessageID != "m1" && first.MessageID == second.MessageID {
		return "new response reused message identity", nil
	}
	return "identity semantics preserved", nil
}
