// Package contractfixture implements a deterministic, in-memory contract sample.
// It has no authorization, durable acceptance or task execution guarantees.
package contractfixture

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
	"lerna/protocol"
	"sync"
)

type scopedID struct{ namespace, id string }
type message struct {
	request  *wire.Envelope
	response []byte
}
type Fixture struct {
	mu               sync.Mutex
	validator        protocol.PayloadValidator
	operations       map[scopedID]*wire.SubmitRequest
	messages         map[scopedID]message
	responseSequence uint64
}

func New(validator protocol.PayloadValidator) *Fixture {
	return &Fixture{validator: validator, operations: make(map[scopedID]*wire.SubmitRequest), messages: make(map[scopedID]message)}
}
func (f *Fixture) Exchange(ctx context.Context, data []byte) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	in, err := protocol.Decode(data)
	if err != nil {
		return nil, err
	}
	if err := protocol.ValidateRequest(in, f.validator); err != nil {
		return nil, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	mk := scopedID{in.Namespace, in.MessageId}
	if previous, ok := f.messages[mk]; ok {
		if !proto.Equal(previous.request, in) {
			return f.reply(in, identityConflict("message identity reused with different content"))
		}
		return append([]byte(nil), previous.response...), nil
	}
	key := scopedID{in.Namespace, in.OperationId}
	var operationErr error
	if previous, exists := f.operations[key]; exists && !sameIntent(previous, in.GetRequest()) {
		operationErr = identityConflict("operation identity reused with different intent")
	} else {
		f.operations[key] = proto.Clone(in.GetRequest()).(*wire.SubmitRequest)
	}
	response, err := f.reply(in, operationErr)
	if err != nil {
		return nil, err
	}
	f.messages[mk] = message{request: in, response: append([]byte(nil), response...)}
	return response, nil
}
func identityConflict(description string) error {
	return &protocol.Error{Code: wire.ErrorCode_ERROR_CODE_IDENTITY_CONFLICT, Description: description}
}
func (f *Fixture) reply(in *wire.Envelope, err error) ([]byte, error) {
	// Length-framed namespace and identity avoid delimiter collisions.
	taskID := fmt.Sprintf("fixture-task/%x", sha256.Sum256([]byte(fmt.Sprintf("%d:%s%s", len(in.Namespace), in.Namespace, in.OperationId))))
	result := &wire.SubmitResponse{Evidence: wire.Evidence_EVIDENCE_CONTRACT_FIXTURE, Result: &wire.SubmitResponse_Task{Task: &wire.TaskRef{Namespace: in.Namespace, TaskId: taskID}}}
	if err != nil {
		var failure *protocol.Error
		if !errors.As(err, &failure) {
			return nil, err
		}
		result.Result = &wire.SubmitResponse_Failure{Failure: &wire.Failure{Code: failure.Code, Description: failure.Description}}
	}
	// New responses have new identities; cached resends keep their original ID.
	// Allocation happens under mu and does not define a protocol-wide ordering.
	f.responseSequence++
	responseID := fmt.Sprintf("fixture-response/%d", f.responseSequence)
	if responseID == in.MessageId {
		f.responseSequence++
		responseID = fmt.Sprintf("fixture-response/%d", f.responseSequence)
	}
	return proto.Marshal(&wire.Envelope{
		ProtocolMajor: 1, MessageId: responseID, ReplyTo: proto.String(in.MessageId),
		Namespace: in.Namespace, OperationId: in.OperationId, Body: &wire.Envelope_Response{Response: result},
	})
}
func sameIntent(a, b *wire.SubmitRequest) bool {
	a = proto.Clone(a).(*wire.SubmitRequest)
	b = proto.Clone(b).(*wire.SubmitRequest)
	if a.Input != nil && b.Input != nil {
		x, xerr := jsonvalue.Decode(a.Input.Json)
		y, yerr := jsonvalue.Decode(b.Input.Json)
		if xerr != nil || yerr != nil || !jsonvalue.Equal(x, y) {
			return false
		}
		a.Input.Json, b.Input.Json = nil, nil
	}
	return proto.Equal(a, b)
}
