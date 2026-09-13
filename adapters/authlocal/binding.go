// Package authlocal binds credentials outside ordinary protocol messages.
// It is a trusted in-process adapter, not a network authentication scheme.
package authlocal

import (
	"context"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/randomid"
	"lerna/protocol"
)

// Authority is the authentication and management surface consumed by this binding.
// Implementations may be replaced without changing the transport adapter.
type Authority interface {
	Authenticate(context.Context, string) (authorization.Identity, error)
	Execute(context.Context, string, authorization.Mutation) (*wire.AuthorizationReceipt, error)
	NewOperation(context.Context, string) (string, error)
	GetPolicy(context.Context, string) (*wire.PolicyView, error)
	GetGrant(context.Context, string, string) (*wire.LocalGrant, error)
	LookupOperation(context.Context, string, string) (*wire.AuthorizationReceipt, error)
	Evaluate(context.Context, string, *wire.AuthorizationAction) (*wire.AuthorizationDecision, error)
}
type SignedAuthority interface {
	Mutate(context.Context, string, *wire.GrantMutation) (*wire.GrantReceipt, error)
	Get(context.Context, string, string) (*wire.GrantRecord, error)
	LookupOperation(context.Context, string, string) (*wire.GrantReceipt, error)
}

func BindGrants(service Authority, grants SignedAuthority, credential string) *Binding {
	return &Binding{service: service, grants: grants, credential: credential}
}

type Binding struct {
	grants     SignedAuthority
	service    Authority
	credential string
}

func Bind(service Authority, credential string) *Binding {
	return &Binding{service: service, credential: credential}
}
func (b *Binding) Exchange(ctx context.Context, data []byte) ([]byte, error) {
	if b.service == nil || len(data) > protocol.MaxMessageBytes {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	request := new(wire.AuthorizationRequest)
	if err := proto.Unmarshal(data, request); err != nil {
		return nil, &authorization.Error{Code: authorization.Invalid}
	}
	id, err := randomid.New()
	if err != nil {
		return nil, err
	}
	response := &wire.AuthorizationResponse{MessageId: id, ReplyTo: request.MessageId, Namespace: request.Namespace}
	err = b.dispatch(ctx, request, response)
	if err != nil {
		code := authorization.Unavailable
		var failure *authorization.Error
		if errors.As(err, &failure) {
			code = failure.Code
		}
		response.Body = &wire.AuthorizationResponse_Failure{Failure: &wire.AuthorizationFailure{Code: string(code)}}
	}
	return proto.Marshal(response)
}
func (b *Binding) dispatch(ctx context.Context, request *wire.AuthorizationRequest, response *wire.AuthorizationResponse) error {
	identity, err := b.service.Authenticate(ctx, b.credential)
	if err != nil {
		return err
	}
	if request.Namespace != identity.Namespace {
		return &authorization.Error{Code: authorization.Denied}
	}
	if request.MessageId == "" || len(request.MessageId) > 128 {
		return &authorization.Error{Code: authorization.Invalid}
	}
	if len(request.ProtoReflect().GetUnknown()) > 0 {
		return &authorization.Error{Code: authorization.Unsupported}
	}
	if request.GetCommand() == nil && request.OperationId != "" {
		return &authorization.Error{Code: authorization.Invalid}
	}
	switch body := request.Body.(type) {
	case *wire.AuthorizationRequest_MutateSignedGrant:
		if b.grants == nil {
			return &authorization.Error{Code: authorization.Unsupported}
		}
		out, err := b.grants.Mutate(ctx, b.credential, body.MutateSignedGrant)
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_SignedGrantReceipt{SignedGrantReceipt: out}
	case *wire.AuthorizationRequest_GetSignedGrant:
		if b.grants == nil {
			return &authorization.Error{Code: authorization.Unsupported}
		}
		out, err := b.grants.Get(ctx, b.credential, body.GetSignedGrant)
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_SignedGrant{SignedGrant: out}
	case *wire.AuthorizationRequest_LookupSignedGrantOperation:
		if b.grants == nil {
			return &authorization.Error{Code: authorization.Unsupported}
		}
		out, err := b.grants.LookupOperation(ctx, b.credential, body.LookupSignedGrantOperation)
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_SignedGrantReceipt{SignedGrantReceipt: out}

	case *wire.AuthorizationRequest_Command:
		receipt, err := b.service.Execute(ctx, b.credential, authorization.Mutation{Namespace: request.Namespace, OperationID: request.OperationId, Command: body.Command})
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_Receipt{Receipt: receipt}
	case *wire.AuthorizationRequest_NewOperation:
		if !body.NewOperation {
			return &authorization.Error{Code: authorization.Invalid}
		}
		id, err := b.service.NewOperation(ctx, b.credential)
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_OperationId{OperationId: id}
	case *wire.AuthorizationRequest_GetPolicy:
		if !body.GetPolicy {
			return &authorization.Error{Code: authorization.Invalid}
		}
		view, err := b.service.GetPolicy(ctx, b.credential)
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_Policy{Policy: view}
	case *wire.AuthorizationRequest_GetGrant:
		grant, err := b.service.GetGrant(ctx, b.credential, body.GetGrant)
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_Grant{Grant: grant}
	case *wire.AuthorizationRequest_LookupOperation:
		receipt, err := b.service.LookupOperation(ctx, b.credential, body.LookupOperation)
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_Receipt{Receipt: receipt}
	case *wire.AuthorizationRequest_Evaluate:
		decision, err := b.service.Evaluate(ctx, b.credential, body.Evaluate)
		if err != nil {
			return err
		}
		response.Body = &wire.AuthorizationResponse_Decision{Decision: decision}
	default:
		return &authorization.Error{Code: authorization.Unsupported}
	}
	return nil
}
