package budget

import (
	"context"
	"math"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type versionStore interface {
	LoadBudgetVersion(context.Context, *v1.Ref) (*v1.Budget, error)
}

func projectBudget(b *v1.Budget) error {
	if b.Limit < 0 || b.Reserved < 0 || b.Settled < 0 || b.Settled > math.MaxInt64-b.Reserved {
		return command.Fail("AMOUNT_OVERFLOW")
	}
	used := b.Settled + b.Reserved
	b.Available = 0
	b.Deficit = 0
	if used > b.Limit {
		b.Deficit = used - b.Limit
	} else {
		b.Available = b.Limit - used
	}
	return nil
}
func (s *Service) QueryBudgetVersion(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.Budget, error) {
	if r == nil || r.Revision == 0 || r.SchemaId != "lerna.v1.Budget" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "budget"); e != nil {
		return nil, e
	}
	b, e := s.store.(versionStore).LoadBudgetVersion(ctx, r)
	if e != nil || b == nil {
		return b, e
	}
	if !proto.Equal(b.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	return b, nil
}

// AdjustLimit 保留原责任并追加额度版本；降额不能释放未知预留。
func (s *Service) AdjustLimit(ctx context.Context, caller *v1.Caller, c *v1.AdjustBudgetLimitCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("adjust-budget-limit", c.ExpectedRef, c.Limit, c.Reason), "budget.limit", func(tx context.Context) (*v1.Ref, error) {
		if !s.trustedCaller(caller) {
			return nil, command.Fail("PERMISSION_DENIED")
		}
		if c.Limit < 0 || c.Reason == "" {
			return nil, command.Fail("INVALID_BUDGET")
		}
		old, e := s.QueryBudgetVersion(tx, caller, c.ExpectedRef)
		if e != nil {
			return nil, e
		}
		if old == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		b, e := s.store.LoadBudget(tx, old.TaskId)
		if e != nil {
			return nil, e
		}
		if !proto.Equal(b.Ref, c.ExpectedRef) {
			return nil, command.Fail("STALE_REFERENCE")
		}
		b.Limit = c.Limit
		b.Ref.Revision++
		if e = projectBudget(b); e != nil {
			return nil, e
		}
		return b.Ref, s.saveBudget(tx, b)
	})
}
