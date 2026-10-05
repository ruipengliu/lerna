package budget

import (
	"context"
	"math"
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type budgetStore struct {
	Store
	budget *v1.Budget
	writes int
}

func (s *budgetStore) LoadBudget(context.Context, *v1.GlobalName) (*v1.Budget, error) {
	return s.budget, nil
}
func (s *budgetStore) SaveBudget(context.Context, *v1.Budget) error { s.writes++; return nil }

// 规则：G10、准入-8
func TestReservationCannotOverflowOrReopenBudget(t *testing.T) {
	for _, b := range []*v1.Budget{{Limit: math.MaxInt64, Settled: math.MaxInt64 - 1, Reserved: 1, Unit: "USD_MICRO", Status: "OPEN"}, {Limit: math.MaxInt64, Unit: "USD_MICRO", Status: "CLOSED"}} {
		store := &budgetStore{budget: b}
		amount := int64(1)
		basis, e := New(store, nil, "u", "d", "host").ReserveInTransaction(context.Background(), nil, nil, nil, &v1.Capability{FeeCeiling: &amount, Unit: "USD_MICRO", RateBasisRef: &v1.Ref{Revision: 1}, MaxSends: 1})
		if e == nil || basis != nil || store.writes != 0 {
			t.Fatalf("invalid reserve: %v %v writes=%d", basis, e, store.writes)
		}
	}
}
