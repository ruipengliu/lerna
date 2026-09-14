package wsbinding

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
	"reflect"
)

type childRequest struct {
	Route     *tasks.HandoffRoute
	Method    string
	Intent    tasks.ChildIntent
	Reference *tasks.ChildReference
}
type childReply struct {
	Task         *tasks.Task
	Report       *tasks.ChildReport
	Cancellation *tasks.ControlReceipt
}

func decodeChild(raw []byte, out any) error {
	if len(raw) == 0 || len(raw) > 65536 {
		return failure(authorization.Invalid)
	}
	if _, e := jsonvalue.Decode(raw); e != nil {
		return failure(authorization.Invalid)
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return failure(authorization.Invalid)
	}
	return nil
}
func childQuery(ctx context.Context, c *tasks.ChildPort, peer authorization.GrantPresentation, raw []byte) ([]byte, error) {
	var in childRequest
	if e := decodeChild(raw, &in); e != nil {
		return nil, e
	}
	if in.Route != nil {
		var e error
		c, e = c.FollowParent(ctx, *in.Route, peer)
		if e != nil {
			return nil, e
		}
	}
	if e := c.MatchPeer(peer); e != nil {
		return nil, e
	}
	if (in.Method == "accept" && in.Reference != nil) || (in.Method != "accept" && !reflect.DeepEqual(in.Intent, tasks.ChildIntent{})) {
		return nil, failure(authorization.Invalid)
	}
	var out childReply
	switch in.Method {
	case "accept", "lookup":
		var t tasks.Task
		var e error
		if in.Method == "accept" {
			t, e = c.Accept(ctx, in.Intent)
		} else {
			if in.Reference == nil {
				return nil, failure(authorization.Invalid)
			}
			t, e = c.Lookup(ctx, *in.Reference)
		}
		if e != nil {
			return nil, e
		}
		out.Task = &t
	case "cancel":
		if in.Reference == nil {
			return nil, failure(authorization.Invalid)
		}
		v, e := c.Cancel(ctx, *in.Reference)
		if e != nil {
			return nil, e
		}
		out.Cancellation = &v
	case "report":
		if in.Reference == nil {
			return nil, failure(authorization.Invalid)
		}
		v, e := c.Report(ctx, *in.Reference)
		if e != nil {
			return nil, e
		}
		out.Report = &v
	default:
		return nil, failure(authorization.Unsupported)
	}
	data, e := json.Marshal(out)
	if e != nil || len(data) > 65536 {
		return nil, failure(authorization.Invalid)
	}
	return data, nil
}
func (p *Peer) child(ctx context.Context, method string, in childRequest) (childReply, error) {
	in.Method = method

	var out childReply
	id, e := authorization.NewCredential()
	if e != nil {
		return out, e
	}
	data, e := json.Marshal(in)
	if e != nil || len(data) > 65536 {
		return out, failure(authorization.Invalid)
	}
	request := &wire.CapabilityRequest{MessageId: id, Namespace: p.identity.Namespace, Body: &wire.CapabilityRequest_Delegation{Delegation: data}}
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
	if e = decodeChild(response.GetDelegationReply(), &out); e != nil {
		return out, e
	}
	count := 0
	if out.Task != nil {
		count++
	}
	if out.Report != nil {
		count++
	}
	if out.Cancellation != nil {
		count++
	}
	if count != 1 {
		return childReply{}, failure(authorization.Invalid)
	}
	return out, nil
}
func (p *Peer) Accept(ctx context.Context, in tasks.ChildIntent) (tasks.Task, error) {
	v, e := p.child(ctx, "accept", childRequest{Intent: in})
	if e != nil {
		return tasks.Task{}, e
	}
	if v.Task == nil {
		return tasks.Task{}, failure(authorization.Invalid)
	}
	return *v.Task, nil
}
func (p *Peer) Lookup(ctx context.Context, in tasks.ChildReference) (tasks.Task, error) {
	v, e := p.child(ctx, "lookup", childRequest{Reference: &in})
	if e != nil {
		return tasks.Task{}, e
	}
	if v.Task == nil {
		return tasks.Task{}, failure(authorization.Invalid)
	}
	return *v.Task, nil
}
func (p *Peer) Cancel(ctx context.Context, in tasks.ChildReference) (tasks.ControlReceipt, error) {
	v, e := p.child(ctx, "cancel", childRequest{Reference: &in})
	if e != nil {
		return tasks.ControlReceipt{}, e
	}
	if v.Cancellation == nil {
		return tasks.ControlReceipt{}, failure(authorization.Invalid)
	}
	return *v.Cancellation, nil
}
func (p *Peer) Report(ctx context.Context, in tasks.ChildReference) (tasks.ChildReport, error) {
	v, e := p.child(ctx, "report", childRequest{Reference: &in})
	if e != nil {
		return tasks.ChildReport{}, e
	}
	if v.Report == nil {
		return tasks.ChildReport{}, failure(authorization.Invalid)
	}
	return *v.Report, nil
}

// RoutedChildren uses the activated parent's proof with the existing connection.
type RoutedChildren struct {
	peer  *Peer
	route tasks.HandoffRoute
}

func (p *Peer) WithParentRoute(route tasks.HandoffRoute) *RoutedChildren {
	return &RoutedChildren{p, route}
}
func (p *RoutedChildren) Accept(ctx context.Context, in tasks.ChildIntent) (tasks.Task, error) {
	v, e := p.peer.child(ctx, "accept", childRequest{Intent: in, Route: &p.route})
	if e != nil {
		return tasks.Task{}, e
	}
	if v.Task == nil {
		return tasks.Task{}, failure(authorization.Invalid)
	}
	return *v.Task, nil
}
func (p *RoutedChildren) Lookup(ctx context.Context, in tasks.ChildReference) (tasks.Task, error) {
	v, e := p.peer.child(ctx, "lookup", childRequest{Reference: &in, Route: &p.route})
	if e != nil {
		return tasks.Task{}, e
	}
	if v.Task == nil {
		return tasks.Task{}, failure(authorization.Invalid)
	}
	return *v.Task, nil
}
func (p *RoutedChildren) Cancel(ctx context.Context, in tasks.ChildReference) (tasks.ControlReceipt, error) {
	v, e := p.peer.child(ctx, "cancel", childRequest{Reference: &in, Route: &p.route})
	if e != nil {
		return tasks.ControlReceipt{}, e
	}
	if v.Cancellation == nil {
		return tasks.ControlReceipt{}, failure(authorization.Invalid)
	}
	return *v.Cancellation, nil
}
func (p *RoutedChildren) Report(ctx context.Context, in tasks.ChildReference) (tasks.ChildReport, error) {
	v, e := p.peer.child(ctx, "report", childRequest{Reference: &in, Route: &p.route})
	if e != nil {
		return tasks.ChildReport{}, e
	}
	if v.Report == nil {
		return tasks.ChildReport{}, failure(authorization.Invalid)
	}
	return *v.Report, nil
}
