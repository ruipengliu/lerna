package collaboration_test

import (
	"context"
	"sync"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type agentFlowKey struct{}
type agentFlow struct {
	mu   sync.Mutex
	flow runtime.Flow
	uses map[string]memory.ForeignUse
}

func (p *agentFlow) ForeignUses() ([]memory.ForeignUse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	uses := []memory.ForeignUse{}
	for _, u := range p.uses {
		uses = append(uses, u)
	}
	return uses, nil
}
func (p *agentFlow) stage(ref memory.ForeignReference, proof memory.ForeignProof) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if ref.HolderRef.TenantID != p.flow.Scope.TenantID || ref.HolderRef.OwnerID != p.flow.Scope.OwnerID {
		return api.E("forbidden", "actual_flow_scope_changed")
	}
	if old, ok := p.uses[ref.CopyID]; ok && (!api.Equal(old.Reference, ref) || old.Proof.ControlRevision > proof.ControlRevision) {
		return api.E("revision_conflict", "actual_flow_source_regressed")
	}
	var frozen memory.ForeignUse
	if err := api.Decode(api.Raw(memory.ForeignUse{Reference: ref, Proof: proof}), &frozen); err != nil {
		return err
	}
	if _, exists := p.uses[ref.CopyID]; !exists && len(p.uses) >= 100 {
		return api.E("overloaded", "actual_flow_capacity")
	}
	p.uses[ref.CopyID] = frozen
	if len(api.Raw(p.uses)) > 1<<20 {
		return api.E("overloaded", "actual_flow_bytes")
	}
	return nil
}

type flowSource struct{ memory.ForeignContentPort }

func (p flowSource) Current(ctx context.Context, scope runtime.Scope, a runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	proof, err := p.ForeignContentPort.Current(ctx, scope, a, ref)
	if err == nil {
		if flow, ok := ctx.Value(agentFlowKey{}).(*agentFlow); ok {
			err = flow.stage(ref, proof)
		}
	}
	return proof, err
}
func (p flowSource) RegisterCopy(ctx context.Context, scope runtime.Scope, a runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	proof, err := p.ForeignContentPort.RegisterCopy(ctx, scope, a, ref)
	if err == nil {
		if flow, ok := ctx.Value(agentFlowKey{}).(*agentFlow); ok {
			err = flow.stage(ref, proof)
		}
	}
	return proof, err
}
func installAgentFlow(f *agentFixture) {
	f.memory.Foreign = flowSource{f.memory.Foreign}
	f.registry.SetContextFactory(func(ctx context.Context, flow runtime.Flow) context.Context {
		state := &agentFlow{flow: flow, uses: map[string]memory.ForeignUse{}}
		ctx = context.WithValue(ctx, agentFlowKey{}, state)
		ctx = memory.WithForeignUseProvider(ctx, state)
		return collaboration.NewParentScopeContext(ctx)
	})
}
func (b agentContent) prepare(ctx context.Context) (context.Context, error) {
	state, ok := ctx.Value(agentFlowKey{}).(*agentFlow)
	if !ok || state.flow.Kind != "job" || state.flow.Work == nil {
		return ctx, nil
	}
	work := state.flow.Work
	if work.Job.Kind != task.JobInput && work.Job.Kind != task.JobSteer {
		return ctx, nil
	}
	return b.f.remote.PrepareInputContext(ctx, work.Job.SourceRef.ObjectID)
}
