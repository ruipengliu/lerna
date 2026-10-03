package development

import (
	"context"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/runtime"
)

type remoteAgentEntryKey struct{}
type remoteAgentContextErrorKey struct{}
type remoteAgentEntry struct {
	scope runtime.Scope
}

// remoteAgentEntryContext只承载本次控制事实；它不登记副本、不读取正文、不出站。
// 跨主体数据proof的交接仍由foreignContextFactory核原holder及用途。
func (a *App) remoteAgentEntryContext(ctx context.Context, flow runtime.Flow) context.Context {
	if a.RemoteAgent == nil {
		return ctx
	}
	previous, nested := ctx.Value(remoteAgentEntryKey{}).(remoteAgentEntry)
	if nested && previous.scope == flow.Scope && flow.Scope == a.Scope && flow.Kind != "job" {
		cloned, err := collaboration.CloneParentScopeContext(ctx)
		if err == nil {
			ctx = cloned
		} else {
			// 不把装配/载体损坏变成旧disk许可；后续纯门禁保留原错误。
			ctx = collaboration.NewParentScopeContext(ctx)
			ctx = context.WithValue(ctx, remoteAgentContextErrorKey{}, err)
		}
	} else {
		ctx = collaboration.NewParentScopeContext(ctx)
	}
	return context.WithValue(ctx, remoteAgentEntryKey{}, remoteAgentEntry{scope: flow.Scope})
}
