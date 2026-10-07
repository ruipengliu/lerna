package sessions

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type DeliveryStore interface {
	LoadReceipt(context.Context, *v1.CommandIdentity) (*v1.CommandReceipt, error)
	SaveInputDelivery(context.Context, *v1.InputDelivery) error
	LoadInputDelivery(context.Context, *v1.Ref) (*v1.InputDelivery, error)
}

func (s *Service) dependencies(ctx context.Context, c *v1.SubmitInputCommand) (bool, error) {
	ready := true
	for _, id := range c.DependsOn {
		if id == nil || id.UserId != s.user || id.TargetDomainId != s.domain || id.IssuerId == "" || id.CommandId == "" || proto.Equal(id, c.Header.Identity) {
			return false, command.Fail("INVALID_DEPENDENCY")
		}
		r, e := s.store.LoadReceipt(ctx, id)
		if e != nil {
			return false, e
		}
		if r == nil || r.Phase != v1.ReceiptPhase_RECEIPT_PHASE_DECIDED {
			ready = false
			continue
		}
		if r.Decision != v1.Decision_DECISION_ACCEPTED {
			return false, command.Fail("DEPENDENCY_REJECTED")
		}
	}
	return ready, nil
}
func (s *Service) QueryInput(ctx context.Context, caller *v1.Caller, r *v1.Ref) (*v1.InputDelivery, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.SessionInput" {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(caller, r.Name, s.user, s.domain, "input"); e != nil {
		return nil, e
	}
	d, e := s.store.LoadInputDelivery(ctx, r)
	if e == nil && d != nil && !proto.Equal(d.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return d, e
}

// RouteInput 重查原始依赖和版本，不能将已保存的输入改投另一个任务。
func (s *Service) RouteInput(ctx context.Context, caller *v1.Caller, c *v1.RouteInputCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.durable.Execute(ctx, caller, c.Header, command.SemanticFingerprint("route-input", c.InputRef), "sessions.input", func(tx context.Context) (*v1.Ref, error) {
		d, e := s.QueryInput(tx, caller, c.InputRef)
		if e != nil {
			return nil, e
		}
		if d == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if d.Input.RoutingStatus != "WAITING_DEPENDENCY" {
			return nil, command.Fail("ALREADY_ROUTED")
		}
		ready, e := s.dependencies(tx, d.OriginalCommand)
		if e != nil {
			return nil, e
		}
		if !ready {
			return nil, command.Fail("DEPENDENCY_PENDING")
		}
		session, e := s.QuerySession(tx, caller, d.OriginalCommand.SessionId)
		if e != nil {
			return nil, e
		}
		if session == nil || session.Status != "ACTIVE" {
			return nil, command.Fail("SESSION_INACTIVE")
		}
		if d.OriginalCommand.InputKind != "CONTROL" || d.Input.ContentRef != nil {
			if e = s.content.CheckUsable(tx, caller, d.Input.ContentRef); e != nil {
				return nil, e
			}
		}
		d.Input.RoutingStatus = "RECORDED"
		if e = s.deliver(tx, caller, d.OriginalCommand, session, d.Input); e != nil {
			return nil, e
		}
		for i, input := range session.Inputs {
			if proto.Equal(input.InputId, d.Input.InputId) {
				session.Inputs[i] = d.Input
			}
		}
		session.Revision++
		if e = s.store.SaveSession(tx, session); e != nil {
			return nil, e
		}
		return d.Ref, s.saveInputDelivery(tx, d)
	})
}
