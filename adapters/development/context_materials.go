package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func (a *App) contextMaterialSources(ctx context.Context, scope runtime.Scope, auth runtime.Auth, facts task.ContextFacts) ([]api.ContentRef, error) {
	status, err := a.Store.Within(ctx, scope, []string{"content", "memory", "platform", "governance"}, func(tx runtime.Tx) error {
		return (contextLookup{a}).CheckMaterialsTx(ctx, tx, auth, facts.ContextMaterials)
	})
	if status == runtime.CommitUnknown {
		return nil, runtime.ErrCommitUnknown
	}
	if err != nil {
		return nil, err
	}
	refs := []api.ContentRef{}
	for _, material := range facts.ContextMaterials {
		refs = append(refs, material.ContentRef)
		if material.QueryRef != nil {
			refs = append(refs, *material.QueryRef)
		}
	}
	refs = uniqueSources(refs)
	var total uint64
	for _, ref := range refs {
		if ref.ByteLength > api.MaxJSONBytes-total {
			return nil, api.E("overloaded", "required_context_material_bytes")
		}
		body, err := a.Memory.Read(ctx, scope, auth, ref, "task.context")
		if err != nil {
			return nil, err
		}
		total += uint64(len(body))
	}
	return refs, nil
}
