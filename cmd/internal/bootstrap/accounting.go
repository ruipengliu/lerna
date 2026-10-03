package bootstrap

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// 先在事务外核对原负责方的准确累计用量，再在授权owner短事务中归并。
// Task随后归并自身Reservation；双方分别保留原源revision/digest去重。
func (a *App) settleUses(ctx context.Context, scope runtime.Scope, refs []api.ObjectRef, usage api.UsageSnapshot) error {
	if len(refs) == 0 {
		return nil
	}
	if scope != a.Scope {
		return api.E("forbidden", "usage_scope_changed")
	}
	if err := (usageVerifier{a}).Verify(ctx, scope, usage.SourceRef, usage); err != nil {
		return err
	}
	status, err := a.Store.Within(ctx, scope, []string{"governance", "platform"}, func(tx runtime.Tx) error {
		if err := currentCredentialTx(ctx, tx, a.ServiceAuth); err != nil {
			return err
		}
		for _, ref := range refs {
			if err := runtime.CheckRef(scope, ref); err != nil {
				return err
			}
			if _, err := a.Governance.ApplySettlementTx(ctx, tx, governance.SettleRequest{UseID: ref.ObjectID, Usage: usage}); err != nil {
				return err
			}
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
