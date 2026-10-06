package egress

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type cancellationLedger interface {
	CloseForCancellation(context.Context, *v1.Caller, *v1.CloseCancellationCommand) (*v1.CommandReceipt, error)
}

// CloseForCancellation 与 P4、P5 及实际使用共用同一跨进程出口临界区。
func (s *Service) CloseForCancellation(ctx context.Context, c *v1.Caller, request *v1.CloseCancellationCommand) (*v1.CommandReceipt, error) {
	ledger, ok := s.ledger.(cancellationLedger)
	if !ok {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	release, e := s.enter(ctx)
	if e != nil {
		return nil, e
	}
	defer release()
	return ledger.CloseForCancellation(ctx, c, request)
}
