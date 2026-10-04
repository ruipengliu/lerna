package development

import (
	"context"
	"fmt"
	"sync"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 计划只选择原消费方，不是来源策略快照或许可。准确 holder 和完整当前来源
// 门禁仍由 Memory 核验；本阶段不创建正文、holder 登记或授权身份。
type remoteCurrentGroup struct {
	auth     runtime.Auth
	refs     []api.ContentRef
	purpose  string
	location string
}

// 原准备完成后，在原消费事务外刷新 Current。保留原 ctx 的 Flow/Work/Claim/Auth、
// 取消及父控制值；本次空载体不得继承此前的正面证明。
func (a *App) refreshRemoteConsumerSources(ctx context.Context, scope runtime.Scope, groups []remoteCurrentGroup) (context.Context, error) {
	if scope != a.Scope {
		return nil, api.E("forbidden", "foreign_source_scope_mismatch")
	}
	if len(groups) > 100 {
		return nil, api.E("overloaded", "foreign_source_limit")
	}
	carrier := &foreignUseCarrier{Scope: scope, uses: map[string]stagedForeignUse{}}
	consumer := memory.WithForeignUseProvider(context.WithValue(ctx, foreignCarrierKey{}, carrier), carrier)
	// 保持原 CopyID 优先级。worker 只通过 flowSources 把实际返回的证明放入
	// 共享载体；不采用 PrepareForeignContext 返回的局部显式证明 ctx。
	workCtx, cancel := context.WithCancel(consumer)
	defer cancel()
	var workers sync.WaitGroup
	var first sync.Once
	var failure error
	queue := make(chan remoteCurrentGroup, len(groups))
	for _, group := range groups {
		queue <- group
	}
	close(queue)
	count := 2
	if len(groups) < count {
		count = len(groups)
	}
	workers.Add(count)
	for n := 0; n < count; n++ {
		go func() {
			defer workers.Done()
			for group := range queue {
				if workCtx.Err() != nil {
					return
				}
				_, err := a.Memory.PrepareForeignContext(workCtx, scope, group.auth, group.refs, group.purpose, group.location)
				if err != nil {
					first.Do(func() {
						failure = fmt.Errorf("current original remote consumer purpose=%s location=%s subject=%s: %w", group.purpose, group.location, group.auth.SubjectID, err)
						cancel()
					})
					return
				}
			}
		}()
	}
	workers.Wait()
	if failure != nil {
		return nil, failure
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// 原纯 Tx 消费门禁仍核原签名窗口（包括 device 的 5 秒）、known-deny、
	// 当前主体/代次、准确用途/位置/holder 和完整来源闭包。
	return consumer, nil
}
