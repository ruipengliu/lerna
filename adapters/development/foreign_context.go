package development

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

type foreignCarrierKey struct{}
type foreignFlowKey struct{}
type stagedForeignUse struct {
	Auth runtime.Auth
	Use  memory.ForeignUse
	Size int
}

// 每个领取/网络入口从空集合开始；只有本调用树实际取回的签名进入载体。
// 它不保存正文、不读取磁盘，也不替代 Memory 的当前签名/holder/deny 裁决。
type foreignUseCarrier struct {
	mu    sync.Mutex
	Scope runtime.Scope
	uses  map[string]stagedForeignUse
	bytes int
	fault error
}

func carrierKey(auth runtime.Auth, use memory.ForeignUse) (string, error) {
	return api.Digest([]any{auth, use.Reference})
}

func cloneForeignUse(use memory.ForeignUse) (memory.ForeignUse, error) {
	var result memory.ForeignUse
	err := api.Decode(api.Raw(use), &result)
	return result, err
}

func (c *foreignUseCarrier) stage(scope runtime.Scope, auth runtime.Auth, use memory.ForeignUse) error {
	if c == nil {
		return nil
	}
	if c.Scope != scope || !api.Equal(use.Reference.HolderRef, auth.Ref(scope.OwnerID)) || use.Proof.Mode != "use" || !api.Equal(use.Reference.ContentRef, use.Proof.ContentRef) || use.Reference.CopyID != use.Proof.CopyID || use.Reference.Purpose != use.Proof.Purpose || use.Reference.Location != use.Proof.Location {
		return api.E("forbidden", "foreign_flow_scope_mismatch")
	}
	copyUse, err := cloneForeignUse(use)
	if err != nil {
		return err
	}
	key, err := carrierKey(auth, copyUse)
	if err != nil {
		return err
	}
	size := len(api.Raw(copyUse))
	auth.Roles = append([]string{}, auth.Roles...)
	c.mu.Lock()
	defer c.mu.Unlock()
	old := c.uses[key]
	if (old.Size == 0 && len(c.uses) >= 100) || size > 128<<10 || c.bytes-old.Size+size > 1<<20 {
		return api.E("overloaded", "foreign_flow_proof_limit")
	}
	c.bytes += size - old.Size
	c.uses[key] = stagedForeignUse{auth, copyUse, size}
	return nil
}

func (c *foreignUseCarrier) discard(ref memory.ForeignReference) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, entry := range c.uses {
		if entry.Use.Reference.ContentRef == ref.ContentRef && entry.Use.Reference.CopyID == ref.CopyID {
			c.bytes -= entry.Size
			delete(c.uses, key)
		}
	}
}

func (c *foreignUseCarrier) ForeignUses() ([]memory.ForeignUse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.fault != nil {
		return nil, c.fault
	}
	result := make([]memory.ForeignUse, 0, len(c.uses))
	latest := map[string]memory.ForeignUse{}
	for _, entry := range c.uses {
		use, err := cloneForeignUse(entry.Use)
		if err != nil {
			return nil, err
		}
		old, ok := latest[use.Reference.CopyID]
		if !ok || use.Proof.ControlRevision > old.Proof.ControlRevision || (use.Proof.ControlRevision == old.Proof.ControlRevision && use.Proof.IssuedAt > old.Proof.IssuedAt) {
			latest[use.Reference.CopyID] = use
		}
	}
	keys := make([]string, 0, len(latest))
	for key := range latest {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		result = append(result, latest[key])
	}
	return result, nil
}

func currentForeignCarrier(ctx context.Context) *foreignUseCarrier {
	c, _ := ctx.Value(foreignCarrierKey{}).(*foreignUseCarrier)
	return c
}

func (a *App) foreignContextFactory(ctx context.Context, flow runtime.Flow) context.Context {
	parent := currentForeignCarrier(ctx)
	carrier := &foreignUseCarrier{Scope: flow.Scope, uses: map[string]stagedForeignUse{}}
	// 同调用树的继续处理可传递原证据；新 Job 从空集合开始。网络根 ctx 不存载体。
	if parent != nil && flow.Kind != "job" && parent.Scope == flow.Scope {
		parent.mu.Lock()
		entries := make([]stagedForeignUse, 0, len(parent.uses))
		for _, entry := range parent.uses {
			if api.Equal(entry.Auth, flow.Auth) {
				until, err := api.ParseTime(entry.Use.Proof.StartBefore)
				if err == nil && time.Now().Before(until) {
					entries = append(entries, entry)
				}
			}
		}
		parent.mu.Unlock()
		for _, entry := range entries {
			if err := carrier.stage(flow.Scope, flow.Auth, entry.Use); err != nil {
				carrier.fault = err
				break
			}
		}
	}
	ctx = context.WithValue(ctx, foreignCarrierKey{}, carrier)
	ctx = context.WithValue(ctx, foreignFlowKey{}, flow)
	ctx = memory.WithForeignUseProvider(ctx, carrier)
	return a.remoteAgentEntryContext(ctx, flow)
}

func stageForeignUse(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference, proof memory.ForeignProof) error {
	carrier := currentForeignCarrier(ctx)
	if carrier == nil {
		return nil
	}
	return carrier.stage(scope, auth, memory.ForeignUse{Reference: ref, Proof: proof})
}
