// Package protocol checks the supported subset of the native message contract.
package protocol

import (
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"strings"
)

const MaxMessageBytes = 2 << 20

type PayloadValidator interface {
	Validate(*wire.DynamicPayload) error
}
type Error struct {
	Code        wire.ErrorCode
	Description string
}

func (e *Error) Error() string { return e.Code.String() + ": " + e.Description }
func invalid(description string) error {
	return &Error{wire.ErrorCode_ERROR_CODE_INVALID_ARGUMENT, description}
}

func Decode(data []byte) (*wire.Envelope, error) {
	if len(data) > MaxMessageBytes {
		return nil, invalid("message exceeds profile size limit")
	}
	e := new(wire.Envelope)
	if err := proto.Unmarshal(data, e); err != nil {
		return nil, invalid("malformed Protobuf")
	}
	return e, nil
}

func ValidateRequest(e *wire.Envelope, validator PayloadValidator) error {
	if e == nil {
		return invalid("missing envelope")
	}
	if e.ProtocolMajor != 1 {
		return &Error{wire.ErrorCode_ERROR_CODE_UNSUPPORTED_VERSION, "profile requires protocol major 1"}
	}
	if strings.TrimSpace(e.MessageId) == "" || strings.TrimSpace(e.Namespace) == "" || strings.TrimSpace(e.OperationId) == "" {
		return invalid("missing message, namespace or operation identity")
	}
	if proto.Size(e) > MaxMessageBytes {
		return invalid("message exceeds profile size limit")
	}
	r := e.GetRequest()
	if e.ReplyTo != nil || r == nil || r.Goal == nil || strings.TrimSpace(r.GetGoal()) == "" {
		return invalid("request requires a present, nonblank goal and no reply_to")
	}
	if r.Input != nil {
		if validator == nil {
			return &Error{wire.ErrorCode_ERROR_CODE_MISSING_CAPABILITY, "dynamic payload validator unavailable"}
		}
		if err := validator.Validate(r.Input); err != nil {
			return invalid("dynamic payload rejected: " + err.Error())
		}
	}
	return nil
}

func ValidateResponse(e, request *wire.Envelope) error {
	if e == nil || request == nil || e.ProtocolMajor != 1 || e.MessageId == "" || e.MessageId == request.MessageId || e.GetReplyTo() != request.MessageId || e.Namespace != request.Namespace || e.OperationId != request.OperationId {
		return invalid("invalid response association")
	}
	r := e.GetResponse()
	if r == nil || r.Evidence != wire.Evidence_EVIDENCE_CONTRACT_FIXTURE {
		return invalid("unsupported response evidence")
	}
	if task := r.GetTask(); task != nil {
		if task.Namespace != request.Namespace || task.TaskId == "" {
			return invalid("invalid task reference")
		}
		return nil
	}
	if failure := r.GetFailure(); failure != nil {
		if failure.Code < wire.ErrorCode_ERROR_CODE_INVALID_ARGUMENT || failure.Code > wire.ErrorCode_ERROR_CODE_IDENTITY_CONFLICT {
			return invalid("unknown failure code")
		}
		return &Error{failure.Code, failure.Description}
	}
	return invalid("missing response result")
}
