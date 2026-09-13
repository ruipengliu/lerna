// Package sdk provides the initial task contract client. It does not execute tasks.
package sdk

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/protocol"
)

// Transport exchanges encoded envelopes. Production bindings are future work.
type Transport interface {
	Exchange(context.Context, []byte) ([]byte, error)
}

type Submission struct {
	Namespace, MessageID, OperationID, Goal string
	Input                                   *wire.DynamicPayload
}

type Response struct {
	MessageID, ReplyTo, TaskID, Evidence string
}

type Client struct {
	transport Transport
	validator protocol.PayloadValidator
}

func NewClient(transport Transport, validator protocol.PayloadValidator) *Client {
	return &Client{transport: transport, validator: validator}
}

func (c *Client) Submit(ctx context.Context, in Submission) (Response, error) {
	request := &wire.Envelope{
		ProtocolMajor: 1, MessageId: in.MessageID, Namespace: in.Namespace, OperationId: in.OperationID,
		Body: &wire.Envelope_Request{Request: &wire.SubmitRequest{Goal: proto.String(in.Goal), Input: in.Input}},
	}
	if validationErr := protocol.ValidateRequest(request, c.validator); validationErr != nil {
		return Response{}, validationErr
	}
	if c.transport == nil {
		return Response{}, fmt.Errorf("missing transport")
	}
	data, err := proto.Marshal(request)
	if err != nil {
		return Response{}, err
	}
	data, err = c.transport.Exchange(ctx, data)
	if err != nil {
		return Response{}, err
	}
	envelope, err := protocol.Decode(data)
	if err != nil {
		return Response{}, err
	}
	if err := protocol.ValidateResponse(envelope, request); err != nil {
		return Response{}, err
	}
	out := envelope.GetResponse()
	return Response{MessageID: envelope.MessageId, ReplyTo: envelope.GetReplyTo(), TaskID: out.GetTask().TaskId, Evidence: "contract_fixture"}, nil
}
