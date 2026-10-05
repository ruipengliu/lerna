package budget

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type sendStore interface {
	LoadCurrentReservation(context.Context, *v1.Ref) (*v1.Reservation, error)
	SaveSendConsumption(context.Context, *v1.SendConsumption) error
	LoadSendConsumption(context.Context, *v1.Ref) (*v1.SendConsumption, error)
}

// ConsumeSendInTransaction 只消费原预留中的发送额度，不重新占用授权或整笔费用。
func (s *Service) ConsumeSendInTransaction(ctx context.Context, a *v1.Admission, send *v1.Ref, id *v1.CommandIdentity) error {
	r, e := s.store.(sendStore).LoadCurrentReservation(ctx, a.BudgetBasis.ReservationRef)
	if e != nil {
		return e
	}
	if r == nil || r.Status != "RESERVED" || !proto.Equal(r.AdmissionRef, a.Ref) || !proto.Equal(r.OperationId, a.OperationId) || !proto.Equal(r.TaskId, a.TaskId) || r.Unit != a.BudgetBasis.Unit || r.Ceiling != a.BudgetBasis.Ceiling || r.ConsumedSends >= r.SendCeiling {
		return command.Fail("SEND_BUDGET_EXCEEDED")
	}
	old, e := s.store.(sendStore).LoadSendConsumption(ctx, send)
	if e != nil {
		return e
	}
	if old != nil {
		return command.Fail("SEND_ALREADY_CONSUMED")
	}
	use := &v1.SendConsumption{Ref: command.NewRef(s.user, s.domain, "send-consumption", "lerna.v1.SendConsumption"), ReservationRef: a.BudgetBasis.ReservationRef, SendRef: send, OperationId: a.OperationId, StartIdentity: id}
	r.Ref.Revision++
	r.ConsumedSends++
	if e = s.store.SaveReservation(ctx, r); e != nil {
		return e
	}
	return s.store.(sendStore).SaveSendConsumption(ctx, use)
}
