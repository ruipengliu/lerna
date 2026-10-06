package egress

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type taskClosingLedger interface {
	CloseForTaskClose(context.Context, *v1.Caller, *v1.CloseTaskEndpointCommand) (*v1.CommandReceipt, error)
}

// CloseForTaskClose 在同一实际出口临界区取得原端点停止推进的证明。
func (s *Service) CloseForTaskClose(ctx context.Context, c *v1.Caller, request *v1.CloseTaskEndpointCommand) (*v1.CommandReceipt, error) {
	ledger, ok := s.ledger.(taskClosingLedger)
	if !ok {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	release, e := s.enter(ctx)
	if e != nil {
		return nil, e
	}
	defer release()
	return ledger.CloseForTaskClose(ctx, c, request)
}
