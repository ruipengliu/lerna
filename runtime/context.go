package runtime

import (
	"context"

	"github.com/ruipengliu/lerna/api"
)

// Flow 是本次经过封套核验或原领取得到的入口，不是新增业务权限。
// Command/Query 的 Auth 是原已认证主体；Job 不推断为原命令主体。
type Flow struct {
	Kind    string
	Scope   Scope
	Auth    Auth
	Command *api.Command
	Query   *api.Query
	Work    *Work
}

// ContextFactory 只建立一次入口的有界内存载体，不执行 I/O、不读取磁盘事实。
// 领域仍在显式端口取得证据，并在每个纯 Tx 中核准确范围、当前代次和期限。
type ContextFactory func(context.Context, Flow) context.Context

// SetContextFactory 供显式宿主装配；nil 保持原行为。宿主组合自己的有限载体。
func (r *Registry) SetContextFactory(factory ContextFactory) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.contextFactory = factory
}
func (r *Registry) entryContext(ctx context.Context, flow Flow) context.Context {
	r.mu.RLock()
	factory := r.contextFactory
	r.mu.RUnlock()
	if factory == nil {
		return ctx
	}
	flow.Auth.Roles = append([]string{}, flow.Auth.Roles...)
	if flow.Command != nil {
		c := *flow.Command
		c.Payload = append([]byte{}, c.Payload...)
		if c.ExpectedRevision != nil {
			revision := *c.ExpectedRevision
			c.ExpectedRevision = &revision
		}
		flow.Command = &c
	}
	if flow.Query != nil {
		q := *flow.Query
		q.Payload = append([]byte{}, q.Payload...)
		flow.Query = &q
	}
	if flow.Work != nil {
		work := *flow.Work
		flow.Work = &work
	}
	return factory(ctx, flow)
}
