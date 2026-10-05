package contract

import (
	"encoding/json"
	wire "github.com/ruipengliu/lerna/contract/gen/go"
)

// ContractError is a classified refusal from a public contract boundary.
// Cause supports local diagnosis; only PublicError may cross a wire boundary.
type ContractError struct {
	PublicError
	Cause error `json:"-"`
}

func (e *ContractError) Error() string { return string(e.Code) }
func (e *ContractError) Unwrap() error { return e.Cause }
func refusal(code ErrorCode, cause error) error {
	return &ContractError{PublicError: PublicError{Code: code}, Cause: cause}
}

// ParseCommand validates only the common envelope, never execution eligibility.
// Unknown methods may be retained for canonical digests or historical records.
func ParseCommand(data []byte) (CommandEnvelope, error) {
	value, err := Decode[CommandEnvelope](data)
	if err != nil {
		return value, refusal("schema_invalid", err)
	}
	return value, nil
}

// DecodeCommand checks execution eligibility and the complete method schema.
// 1.0.0 currently registers only command.get; parsing an envelope is insufficient.
func DecodeCommand(data []byte) (CommandGetRequest, error) {
	var result CommandGetRequest
	envelope, err := ParseCommand(data)
	if err != nil {
		return result, err
	}
	if envelope.ContractVersion != Version {
		return result, refusal("version_unsupported", nil)
	}
	name, ok := wire.InputSchema(string(envelope.ContractVersion), string(envelope.Profile), string(envelope.Method))
	if !ok {
		return result, refusal("unsupported", nil)
	}
	value, err := ParseJSON(data)
	if err == nil {
		err = Validate(name, value)
	}
	if err != nil {
		return result, refusal("schema_invalid", err)
	}
	if err = json.Unmarshal(data, &result); err != nil {
		return result, refusal("schema_invalid", err)
	}

	if result.Target.TenantID != result.Payload.CommandRef.Owner.TenantID || result.Target.OwnerID != result.Payload.CommandRef.Owner.OwnerID || result.Target.ID != result.Payload.CommandRef.CommandID {
		return CommandGetRequest{}, refusal("schema_invalid", nil)
	}
	return result, nil
}
