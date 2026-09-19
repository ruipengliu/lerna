package ws

import (
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

func (p *Peer) ReadAuthorization(ctx context.Context, q authorization.OfflineQuery) (string, error) {
	id, e := authorization.NewCredential()
	if e != nil {
		return "", e
	}
	data, e := json.Marshal(q)
	if e != nil {
		return "", e
	}
	request := &wire.CapabilityRequest{MessageId: id, Namespace: p.identity.Namespace, Body: &wire.CapabilityRequest_AuthorizationSync{AuthorizationSync: data}}
	raw, e := proto.Marshal(request)
	if e != nil {
		return "", e
	}
	raw, e = p.Exchange(ctx, raw)
	if e != nil {
		return "", e
	}
	out := new(wire.CapabilityResponse)
	if proto.Unmarshal(raw, out) != nil {
		return "", failure(authorization.Invalid)
	}
	if f := out.GetFailure(); f != nil {
		return "", &authorization.Error{Code: authorization.Code(f.Code)}
	}
	if out.GetAuthorizationPage() == "" {
		return "", failure(authorization.Invalid)
	}
	return out.GetAuthorizationPage(), nil
}
