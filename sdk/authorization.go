package sdk

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/internal/taskwire"
	"lerna/protocol"
)

type AuthorizationClient struct {
	transport Transport
	namespace string
}

func NewAuthorizationClient(transport Transport, namespace string) *AuthorizationClient {
	return &AuthorizationClient{transport, namespace}
}
func (c *AuthorizationClient) call(ctx context.Context, request *wire.AuthorizationRequest) (*wire.AuthorizationResponse, error) {
	invalid := func() error { return &authorization.Error{Code: authorization.Invalid} }
	if c.transport == nil {
		return nil, invalid()
	}
	id, err := randomid.New()
	if err != nil {
		return nil, err
	}
	request.MessageId = id
	request.Namespace = c.namespace
	if proto.Size(request) > protocol.MaxMessageBytes {
		return nil, invalid()
	}
	data, err := proto.Marshal(request)
	if err != nil {
		return nil, invalid()
	}
	data, err = c.transport.Exchange(ctx, data)
	if err != nil {
		return nil, err
	}
	if len(data) > protocol.MaxMessageBytes {
		return nil, invalid()
	}
	response := new(wire.AuthorizationResponse)
	if err := proto.Unmarshal(data, response); err != nil {
		return nil, invalid()
	}
	if !taskwire.Known(response.ProtoReflect()) || response.MessageId == "" || response.MessageId == id || response.ReplyTo != id || response.Namespace != c.namespace {
		return nil, invalid()
	}
	if failure := response.GetFailure(); failure != nil {
		switch authorization.Code(failure.Code) {
		case authorization.Unauthenticated, authorization.Denied, authorization.Invalid, authorization.Unsupported, authorization.Conflict, authorization.IdentityConflict, authorization.Expired, authorization.NotFound, authorization.TimeUntrusted, authorization.Unavailable, authorization.OutcomeUnknown:
			return nil, &authorization.Error{Code: authorization.Code(failure.Code)}
		default:
			return nil, invalid()
		}
	}
	return response, nil
}
func (c *AuthorizationClient) NewOperation(ctx context.Context) (string, error) {
	r, err := c.call(ctx, &wire.AuthorizationRequest{Body: &wire.AuthorizationRequest_NewOperation{NewOperation: true}})
	if err != nil {
		return "", err
	}
	if r.GetOperationId() == "" {
		return "", &authorization.Error{Code: authorization.Invalid}
	}
	return r.GetOperationId(), nil
}
func (c *AuthorizationClient) GetPolicy(ctx context.Context) (*wire.PolicyView, error) {
	r, err := c.call(ctx, &wire.AuthorizationRequest{Body: &wire.AuthorizationRequest_GetPolicy{GetPolicy: true}})
	if err != nil {
		return nil, err
	}
	if r.GetPolicy() == nil {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	return r.GetPolicy(), nil
}
func (c *AuthorizationClient) GetGrant(ctx context.Context, id string) (*wire.LocalGrant, error) {
	r, err := c.call(ctx, &wire.AuthorizationRequest{Body: &wire.AuthorizationRequest_GetGrant{GetGrant: id}})
	if err != nil {
		return nil, err
	}
	if r.GetGrant() == nil || r.GetGrant().Id != id {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	return r.GetGrant(), nil
}
func (c *AuthorizationClient) Evaluate(ctx context.Context, action *wire.AuthorizationAction) (*wire.AuthorizationDecision, error) {
	r, err := c.call(ctx, &wire.AuthorizationRequest{Body: &wire.AuthorizationRequest_Evaluate{Evaluate: action}})
	if err != nil {
		return nil, err
	}
	if r.GetDecision() == nil {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	return r.GetDecision(), nil
}
func (c *AuthorizationClient) Execute(ctx context.Context, id string, command *wire.AuthorizationCommand) (*wire.AuthorizationReceipt, error) {
	return c.receipt(ctx, id, &wire.AuthorizationRequest{OperationId: id, Body: &wire.AuthorizationRequest_Command{Command: command}})
}
func (c *AuthorizationClient) LookupOperation(ctx context.Context, id string) (*wire.AuthorizationReceipt, error) {
	return c.receipt(ctx, id, &wire.AuthorizationRequest{Body: &wire.AuthorizationRequest_LookupOperation{LookupOperation: id}})
}
func (c *AuthorizationClient) receipt(ctx context.Context, id string, request *wire.AuthorizationRequest) (*wire.AuthorizationReceipt, error) {
	r, err := c.call(ctx, request)
	if err != nil {
		return nil, err
	}
	receipt := r.GetReceipt()
	if receipt == nil || receipt.OperationId != id || receipt.Evidence != "local_authorization_commit" {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	return receipt, nil
}

func (c *AuthorizationClient) MutateGrant(ctx context.Context, in *wire.GrantMutation) (*wire.GrantReceipt, error) {
	if in == nil {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	r, err := c.call(ctx, &wire.AuthorizationRequest{Body: &wire.AuthorizationRequest_MutateSignedGrant{MutateSignedGrant: in}})
	if err != nil {
		return nil, err
	}
	out := r.GetSignedGrantReceipt()
	if out == nil || out.OperationId != in.OperationId || out.GrantId == "" || out.Revision == 0 || len(out.Material) > 32768 || (in.Kind != "REVOKE" && out.Material == "") {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	return out, nil
}
func (c *AuthorizationClient) GetSignedGrant(ctx context.Context, id string) (*wire.GrantRecord, error) {
	r, err := c.call(ctx, &wire.AuthorizationRequest{Body: &wire.AuthorizationRequest_GetSignedGrant{GetSignedGrant: id}})
	if err != nil {
		return nil, err
	}
	out := r.GetSignedGrant()
	if out == nil || out.Id != id || out.Spec == nil || out.Revision == 0 {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	return out, nil
}
func (c *AuthorizationClient) LookupGrantOperation(ctx context.Context, id string) (*wire.GrantReceipt, error) {
	r, err := c.call(ctx, &wire.AuthorizationRequest{Body: &wire.AuthorizationRequest_LookupSignedGrantOperation{LookupSignedGrantOperation: id}})
	if err != nil {
		return nil, err
	}
	out := r.GetSignedGrantReceipt()
	if out == nil || out.OperationId != id || out.GrantId == "" || out.Revision == 0 || len(out.Material) > 32768 {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	return out, nil
}
