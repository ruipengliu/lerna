package component_test

import (
	"encoding/json"
	"errors"
	"github.com/ruipengliu/lerna/contract"
	"os"
	"strings"
	"testing"
)

func TestPublicErrorsAreClosedAndClassifiable(t *testing.T) {
	value, err := contract.Decode[contract.PublicError]([]byte(`{"code":"schema_invalid"}`))
	if err != nil {
		t.Fatalf("%v; cause %v", err, errors.Unwrap(err))
	}
	if value.Code != "schema_invalid" {
		t.Fatalf("wrong public code: %+v", value)
	}
	if _, err := contract.Decode[contract.PublicError]([]byte(`{"code":"backend_stack_trace"}`)); err == nil {
		t.Fatal("accepted undeclared code")
	}
	_, err = contract.ParseCommand([]byte(`{} {}`))
	var refusal *contract.ContractError
	if !errors.As(err, &refusal) || refusal.Code != "schema_invalid" {
		t.Fatalf("public refusal was not structured: %v", err)
	}
}

func TestEnvelopeAcceptsDefinedFutureMethodWithoutOfferingExecution(t *testing.T) {
	value, err := contract.ParseCommand([]byte(`{"contract_version":"1.0.0","profile":"core","command_id":"q1","target":{"tenant_id":"t1","owner_id":"o1","kind":"task","id":"task1"},"method":"task.pause","expected_revision":"9007199254740993","payload":{"reason":"用户暂停"},"accept_before":"2026-10-03T10:00:00.000000Z","trace_context":{"trace_id":"trace1"}}`))
	if err != nil {
		t.Fatalf("%v; cause %v", err, errors.Unwrap(err))
	}
	if value.Method != "task.pause" || value.ExpectedRevision == nil || *value.ExpectedRevision != "9007199254740993" {
		t.Fatalf("lost future-method envelope: %+v", value)
	}
}

const commandGetWire = `{"contract_version":"1.0.0","profile":"command","command_id":"query1","target":{"tenant_id":"t1","owner_id":"o1","kind":"command","id":"original1"},"method":"command.get","payload":{"command_ref":{"owner":{"tenant_id":"t1","owner_id":"o1"},"command_id":"original1"}},"accept_before":"2026-10-03T10:00:00.000000Z"}`

func TestCommandGetRetainsOriginalIdentitySeparateFromQuery(t *testing.T) {
	value, err := contract.DecodeCommand([]byte(commandGetWire))
	if err != nil {
		t.Fatal(err)
	}
	if value.CommandID != "query1" || value.Payload.CommandRef.CommandID != "original1" || value.Payload.CommandRef.Owner.OwnerID != "o1" {
		t.Fatalf("lost original command: %+v", value)
	}
}

func TestCommandGetRequiresExactTargetAndRejectsWriteRevision(t *testing.T) {
	for _, invalid := range []string{
		strings.Replace(commandGetWire, `"accept_before":`, `"expected_revision":"7","accept_before":`, 1),
		strings.Replace(commandGetWire, `"kind":"command"`, `"kind":"task"`, 1),
		strings.Replace(commandGetWire, `"id":"original1"`, `"id":"different"`, 1),
		strings.Replace(commandGetWire, `"tenant_id":"t1"`, `"tenant_id":"different"`, 1),
		strings.Replace(commandGetWire, `"owner_id":"o1"`, `"owner_id":"different"`, 1),
	} {
		_, err := contract.DecodeCommand([]byte(invalid))
		var refusal *contract.ContractError
		if !errors.As(err, &refusal) || refusal.Code != "schema_invalid" {
			t.Fatalf("accepted ambiguous command target: %s; %v", invalid, err)
		}
	}
}

func TestProgrammableEnvelopeCannotSmuggleJSONNumbers(t *testing.T) {
	envelope, err := contract.ParseCommand([]byte(commandGetWire))
	if err != nil {
		t.Fatal(err)
	}
	envelope.Payload = contract.CommandPayload(`{"counter":1}`)
	if _, err := contract.Encode(envelope); err == nil {
		t.Fatal("encoding accepted JSON number")
	}
	if err := contract.Validate("CommandPayload", map[string]any{"counter": 1.0}); err == nil {
		t.Fatal("validation accepted a prohibited JSON number")
	}
}

func TestUnknownVersionAndMethodCannotDowngrade(t *testing.T) {
	for _, sample := range []struct{ wire, code string }{
		{strings.Replace(commandGetWire, `"1.0.0"`, `"9.0.0"`, 1), "version_unsupported"},
		{strings.Replace(commandGetWire, `"1.0.0"`, `"v2-design-1"`, 1), "version_unsupported"},
		{strings.Replace(commandGetWire, `"command.get"`, `"task.pause"`, 1), "unsupported"},
		{strings.Replace(commandGetWire, `"profile":"command"`, `"profile":"core"`, 1), "unsupported"},
	} {
		_, err := contract.DecodeCommand([]byte(sample.wire))
		var refusal *contract.ContractError
		if !errors.As(err, &refusal) || string(refusal.Code) != sample.code {
			t.Fatalf("incorrect refusal %v, want %s", err, sample.code)
		}
	}
}

func TestSharedCommandFixtures(t *testing.T) {
	data, err := os.ReadFile("../fixtures/1.0.0/commands.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures []struct {
		Name, Schema, Wire, Code string
		Valid                    bool
		LeadingSpaces            int `json:"leading_spaces"`
	}
	if err = json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	for _, fixture := range fixtures {
		t.Run(fixture.Name, func(t *testing.T) {
			data := []byte(strings.Repeat(" ", fixture.LeadingSpaces) + fixture.Wire)
			switch fixture.Schema {
			case "CommandInput":
				_, err = contract.DecodeCommand(data)
			case "CommandEnvelope":
				_, err = contract.ParseCommand(data)
			case "PublicError":
				_, err = contract.Decode[contract.PublicError](data)
				if err != nil {
					err = &contract.ContractError{PublicError: contract.PublicError{Code: "schema_invalid"}, Cause: err}
				}
			default:
				t.Fatalf("unknown fixture schema %s", fixture.Schema)
			}
			if (err == nil) != fixture.Valid {
				t.Fatalf("valid=%v want %v: %v", err == nil, fixture.Valid, err)
			}
			if !fixture.Valid {
				var refusal *contract.ContractError
				if !errors.As(err, &refusal) || string(refusal.Code) != fixture.Code {
					t.Fatalf("code mismatch: %v want %s", err, fixture.Code)
				}
			}
		})
	}
}

func TestProgrammaticPayloadAlsoEnforcesContainerDepth(t *testing.T) {
	var nested any = nil
	for range 64 {
		nested = []any{nested}
	}
	if err := contract.Validate("CommandPayload", map[string]any{"nested": nested}); err == nil {
		t.Fatal("programmatic validation bypassed 64-container limit")
	}
}

func TestPublicErrorProjectionCannotDiscloseLocalCause(t *testing.T) {
	refusal := &contract.ContractError{PublicError: contract.PublicError{Code: "dependency_unavailable"}, Cause: errors.New("private database endpoint")}
	wire, err := json.Marshal(refusal)
	if err != nil {
		t.Fatal(err)
	}
	if string(wire) != `{"code":"dependency_unavailable"}` {
		t.Fatalf("local error escaped: %s", wire)
	}
}

func TestProgrammaticPayloadRejectsValuesOutsideJSON(t *testing.T) {
	if err := contract.Validate("CommandPayload", map[string]any{"callback": func() {}}); err == nil {
		t.Fatal("dynamic JSON object accepted a function")
	}
}
