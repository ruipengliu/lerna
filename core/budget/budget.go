// Package budget 是预算与预留的唯一写入方。
package budget

import (
	"context"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Store interface {
	Reservations(context.Context, *v1.GlobalName) ([]*v1.Reservation, error)
	SaveBudget(context.Context, *v1.Budget) error
	LoadBudget(context.Context, *v1.GlobalName) (*v1.Budget, error)
	SaveReservation(context.Context, *v1.Reservation) error
	LoadReservation(context.Context, *v1.Ref) (*v1.Reservation, error)
}
type Decisions interface {
	Execute(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error)
}
type Service struct {
	store                       Store
	usageSource                 UsageSource
	decisions                   Decisions
	user, domain, trustedIssuer string
}

func New(s Store, d Decisions, user, domain, trustedIssuer string) *Service {
	return &Service{store: s, decisions: d, user: user, domain: domain, trustedIssuer: trustedIssuer}
}
func (s *Service) Configure(ctx context.Context, caller *v1.Caller, c *v1.ConfigureBudgetCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("configure-budget", c.TaskId, c.Unit, c.Limit), "budget.configure", func(tx context.Context) (*v1.Ref, error) {
		if caller.IssuerId != s.trustedIssuer {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.Limit < 0 || c.Unit != "USD_MICRO" {
			return nil, command.Fail("INVALID_BUDGET")
		}
		if c.TaskId != nil {
			if e := command.CheckName(caller, c.TaskId, s.user, s.domain, "task"); e != nil {
				return nil, e
			}
		}
		old, e := s.store.LoadBudget(tx, c.TaskId)
		if e != nil {
			return nil, e
		}
		if old != nil {
			return nil, command.Fail("BUDGET_ALREADY_CONFIGURED")
		}
		b := &v1.Budget{Ref: command.NewRef(s.user, s.domain, "budget", "lerna.v1.Budget"), TaskId: c.TaskId, Unit: c.Unit, Limit: c.Limit, Status: "OPEN"}
		return b.Ref, s.store.SaveBudget(tx, b)
	})
}

// ReserveInTransaction 同时检查用户总额度与任务额度；未知费用绝不折算为零。
func (s *Service) ReserveInTransaction(ctx context.Context, task, operation *v1.GlobalName, admission *v1.Ref, cap *v1.Capability) (*v1.BudgetBasis, error) {
	if cap.FeeCeiling == nil || cap.GetFeeCeiling() < 0 || cap.RateBasisRef == nil || cap.MaxSends == 0 || cap.MaxSends > 1 || cap.Unit != "USD_MICRO" || (cap.GetFeeCeiling() == 0 && !cap.Nonbillable) {
		return nil, command.Fail("COST_CEILING_UNKNOWN")
	}
	amount := cap.GetFeeCeiling()
	chain := make([]*v1.Budget, 0, 2)
	for _, id := range []*v1.GlobalName{nil, task} {
		b, e := s.store.LoadBudget(ctx, id)
		if e != nil {
			return nil, e
		}
		if b == nil || b.Status != "OPEN" || b.Unit != cap.Unit || b.Settled < 0 || b.Reserved < 0 || b.Settled > b.Limit || b.Reserved > b.Limit-b.Settled || amount > b.Limit-b.Settled-b.Reserved {
			return nil, command.Fail("BUDGET_EXCEEDED")
		}
		chain = append(chain, b)
	}
	refs := make([]*v1.Ref, 0, 2)
	for _, b := range chain {
		b.Reserved += amount
		b.Ref.Revision++
		if e := s.store.SaveBudget(ctx, b); e != nil {
			return nil, e
		}
		refs = append(refs, b.Ref)
	}
	r := &v1.Reservation{Ref: command.NewRef(s.user, s.domain, "reservation", "lerna.v1.Reservation"), AdmissionRef: admission, OperationId: operation, BudgetRefs: refs, Unit: cap.Unit, Ceiling: amount, SendCeiling: cap.MaxSends, Status: "RESERVED", TaskId: task}
	if e := s.store.SaveReservation(ctx, r); e != nil {
		return nil, e
	}
	return &v1.BudgetBasis{BudgetRef: refs[1], BudgetChainRefs: refs, Unit: cap.Unit, Ceiling: amount, RateBasisRef: cap.RateBasisRef, ReservationRef: r.Ref}, nil
}
func (s *Service) QueryBudget(ctx context.Context, c *v1.Caller, id *v1.GlobalName) (*v1.Budget, error) {
	if e := command.CheckCaller(c, s.user); e != nil {
		return nil, e
	}
	if id != nil {
		if e := command.CheckName(c, id, s.user, s.domain, "task"); e != nil {
			return nil, e
		}
	}
	return s.store.LoadBudget(ctx, id)
}
func (s *Service) QueryReservation(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Reservation, error) {
	if r == nil {
		return nil, command.Fail("INVALID_INPUT")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "reservation"); e != nil {
		return nil, e
	}
	v, e := s.store.LoadReservation(ctx, r)
	if e == nil && v != nil && !proto.Equal(v.Ref, r) {
		return nil, command.Fail("STALE_REFERENCE")
	}
	return v, e
}

func (s *Service) QueryReservations(ctx context.Context, c *v1.Caller, id *v1.GlobalName) ([]*v1.Reservation, error) {
	if e := command.CheckName(c, id, s.user, s.domain, "task"); e != nil {
		return nil, e
	}
	return s.store.Reservations(ctx, id)
}
