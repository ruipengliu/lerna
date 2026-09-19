package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"google.golang.org/protobuf/proto"
	"io"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/internal/jsonvalue"
	"lerna/tasks"
)

type handoffRequest struct {
	Method   string
	Envelope tasks.HandoffEnvelope
}
type handoffReply struct {
	Envelope *tasks.HandoffEnvelope
	Active   bool
	Received bool
}

func decodeHandoff(raw []byte, v any) error {
	if len(raw) == 0 || len(raw) > 1<<20 {
		return failure(authorization.Invalid)
	}
	if _, e := jsonvalue.Decode(raw); e != nil {
		return failure(authorization.Invalid)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		return failure(authorization.Invalid)
	}
	return nil
}
func handoffQuery(ctx context.Context, p *tasks.HandoffPort, w *tasks.WorkPort, raw []byte) ([]byte, error) {
	var in handoffRequest
	if e := decodeHandoff(raw, &in); e != nil {
		return nil, e
	}
	var out handoffReply
	switch in.Method {
	case "validate-admission":
		v, e := p.ValidateOriginal(ctx, in.Envelope)
		if e != nil {
			return nil, e
		}
		out.Envelope = &v
	case "import-admission":
		if e := p.ImportOriginal(ctx, in.Envelope); e != nil {
			return nil, e
		}
		out.Received = true
	case "prepare":
		v, e := p.Prepare(ctx, in.Envelope)
		if e != nil {
			return nil, e
		}
		out.Envelope = &v
	case "activate":
		_, e := p.Activate(ctx, in.Envelope)
		if e != nil {
			return nil, e
		}
		out.Active = true
	case "discard":
		if e := p.DiscardPrepared(ctx, in.Envelope); e != nil {
			return nil, e
		}
		out.Received = true
	case "facts":
		if e := p.ReceiveFacts(ctx, in.Envelope, w); e != nil {
			return nil, e
		}
		out.Received = true
	default:
		return nil, failure(authorization.Unsupported)
	}
	return json.Marshal(out)
}
func (p *Peer) exchangeHandoff(ctx context.Context, method string, in tasks.HandoffEnvelope) (handoffReply, error) {
	var out handoffReply
	id, e := authorization.NewCredential()
	if e != nil {
		return out, e
	}
	data, e := json.Marshal(handoffRequest{method, in})
	if e != nil || len(data) > 1<<20 {
		return out, failure(authorization.Invalid)
	}
	request := &wire.CapabilityRequest{MessageId: id, Namespace: p.identity.Namespace, Body: &wire.CapabilityRequest_Handoff{Handoff: data}}
	raw, e := proto.Marshal(request)
	if e != nil {
		return out, e
	}
	raw, e = p.Exchange(ctx, raw)
	if e != nil {
		return out, e
	}
	response := new(wire.CapabilityResponse)
	if proto.Unmarshal(raw, response) != nil {
		return out, failure(authorization.Invalid)
	}
	if f := response.GetFailure(); f != nil {
		return out, &authorization.Error{Code: authorization.Code(f.Code)}
	}
	if e = decodeHandoff(response.GetHandoffReply(), &out); e != nil {
		return out, e
	}
	if (method == "prepare" || method == "validate-admission") && (out.Envelope == nil || out.Active || out.Received) || method == "activate" && (!out.Active || out.Envelope != nil || out.Received) || (method == "facts" || method == "discard" || method == "import-admission") && (!out.Received || out.Envelope != nil || out.Active) {
		return out, failure(authorization.Invalid)
	}
	return out, nil
}
func (p *Peer) PrepareHandoff(ctx context.Context, in tasks.HandoffEnvelope) (tasks.HandoffEnvelope, error) {
	out, e := p.exchangeHandoff(ctx, "prepare", in)
	if e != nil {
		return tasks.HandoffEnvelope{}, e
	}
	return *out.Envelope, nil
}
func (p *Peer) ActivateHandoff(ctx context.Context, in tasks.HandoffEnvelope) error {
	_, e := p.exchangeHandoff(ctx, "activate", in)
	return e
}
func (p *Peer) ForwardHandoffFacts(ctx context.Context, in tasks.HandoffEnvelope) error {
	_, e := p.exchangeHandoff(ctx, "facts", in)
	return e
}

func (p *Peer) DiscardPreparedHandoff(ctx context.Context, in tasks.HandoffEnvelope) error {
	_, e := p.exchangeHandoff(ctx, "discard", in)
	return e
}

func (p *Peer) ValidateOriginalHandoffOperation(ctx context.Context, in tasks.HandoffEnvelope) (tasks.HandoffEnvelope, error) {
	out, e := p.exchangeHandoff(ctx, "validate-admission", in)
	if e != nil {
		return tasks.HandoffEnvelope{}, e
	}
	if out.Envelope == nil {
		return tasks.HandoffEnvelope{}, failure(authorization.Invalid)
	}
	return *out.Envelope, nil
}
func (p *Peer) ImportOriginalHandoffOperation(ctx context.Context, in tasks.HandoffEnvelope) error {
	_, e := p.exchangeHandoff(ctx, "import-admission", in)
	return e
}
