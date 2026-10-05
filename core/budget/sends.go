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

// ConsumeSendInTransaction 每次物理发送占用独立费用预留，与开始回执同事务提交。
func (s *Service) ConsumeSendInTransaction(ctx context.Context, a *v1.Admission, send *v1.Ref, id *v1.CommandIdentity) error {
	all, e := s.store.Reservations(ctx, a.TaskId)
	if e != nil {
		return e
	}
	var consumed uint64
	for _, r := range all {
		if proto.Equal(r.OperationId, a.OperationId) {
			consumed += uint64(r.ConsumedSends)
		}
	}
	if consumed >= uint64(a.CapabilitySnapshot.MaxSends) {
		return command.Fail("SEND_BUDGET_EXCEEDED")
	}
	reservation := a.BudgetBasis.ReservationRef
	if consumed > 0 {
		basis, err := s.ReserveInTransaction(ctx, a.TaskId, a.OperationId, a.Ref, a.CapabilitySnapshot)
		if err != nil {
			return err
		}
		reservation = basis.ReservationRef
	}
	r, e := s.store.(sendStore).LoadCurrentReservation(ctx, reservation)
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
	use := &v1.SendConsumption{Ref: command.NewRef(s.user, s.domain, "send-consumption", "lerna.v1.SendConsumption"), ReservationRef: reservation, SendRef: send, OperationId: a.OperationId, StartIdentity: id}
	r.Ref.Revision++
	r.ConsumedSends++
	if e = s.store.SaveReservation(ctx, r); e != nil {
		return e
	}
	if e = s.store.(sendStore).SaveSendConsumption(ctx, use); e != nil {
		return e
	}
	source := &v1.BillingSource{Ref: command.NewRef(s.user, s.domain, "billing-source", "lerna.v1.BillingSource"), SendRef: send, ReservationRef: reservation, AdmissionRef: a.Ref, TaskId: a.TaskId, OperationId: a.OperationId, Unit: r.Unit, Status: "PENDING", PriceRuleRef: a.BudgetBasis.RateBasisRef}
	return s.store.(billingStore).SaveBillingSource(ctx, source)
}
