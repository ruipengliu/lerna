package ledger

import (
	"context"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// resendPlan 判断能否安全重发并持久安排。
func (m *Module) resendPlan(*durable.Tx, *opState, *lernav1.Operation) (durable.Transition, bool) {
	return durable.Transition{}, false
}

// resend 按原尝试标识安全重发。
func (m *Module) resend(ctx context.Context, c *durable.Claim, _ *opState) error {
	return m.Domain.Advance(ctx, c, "ledger:resend_unavailable", func(*durable.Tx) (durable.Transition, error) {
		return durable.Done(), nil
	})
}
