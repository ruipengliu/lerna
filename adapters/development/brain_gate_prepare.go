package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

// 每次独立 Brain Job 都从原 Task/预算/主体元数据核验开始；随后才能在
// Tx 外取得当前来源证明。完整 Brain gate 和物理模型出口仍各自重核。
func (g brainGate) PrepareGate(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in brain.DecideInput, encoding *brain.Encoding) (context.Context, error) {
	if scope != g.a.Scope || !auth.HasRole("service") {
		return ctx, api.E("forbidden", "trusted_brain_scope_required")
	}
	status, err := g.a.Store.Within(ctx, scope, []string{"task", "platform"}, func(tx runtime.Tx) error {
		if err := g.a.Task.CheckDecisionTx(ctx, tx, auth, in.DecisionID); err != nil {
			return err
		}
		return currentCredentialTx(ctx, tx, auth)
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ctx, err
	}
	refs := []api.ContentRef{in.SnapshotRef}
	if encoding != nil {
		refs = uniqueSources(append(refs, encoding.ProcessedSources...))
	}
	return g.a.prepareForeignSources(ctx, scope, auth, refs, "brain.input", "cloud")
}

var _ brain.GatePreparer = brainGate{}
