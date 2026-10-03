package development

import (
	"context"

	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 此路由只消费已显式配对的准确设备 owner/database/key；不由响应或参数另选来源。
type deviceSources struct{ a *App }

func (d deviceSources) source(ctx context.Context, scope runtime.Scope, ref api.ContentRef) (*executor.SourceClient, error) {
	if scope != d.a.Scope || ref.TenantID != scope.TenantID {
		return nil, api.E("forbidden", "foreign_source_scope_mismatch")
	}
	route, err := d.a.remoteExecutors.client(ctx, ref.OwnerID)
	if err != nil {
		return nil, err
	}
	return &executor.SourceClient{Client: route.Client, Keys: route.Keys}, nil
}

func (d deviceSources) RegisterCopy(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	source, err := d.source(ctx, scope, ref.ContentRef)
	if err != nil {
		return memory.ForeignProof{}, err
	}
	return source.RegisterCopy(ctx, scope, auth, ref)
}

func (d deviceSources) Current(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	source, err := d.source(ctx, scope, ref.ContentRef)
	if err != nil {
		return memory.ForeignProof{}, err
	}
	return source.Current(ctx, scope, auth, ref)
}

func (d deviceSources) Read(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference, proof memory.ForeignProof) ([]byte, error) {
	source, err := d.source(ctx, scope, ref.ContentRef)
	if err != nil {
		return nil, err
	}
	return source.Read(ctx, scope, auth, ref, proof)
}

func (d deviceSources) Control(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference) (memory.ForeignProof, error) {
	source, err := d.source(ctx, scope, ref.ContentRef)
	if err != nil {
		return memory.ForeignProof{}, err
	}
	return source.Control(ctx, scope, auth, ref)
}

func (d deviceSources) Release(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref memory.ForeignReference, report memory.ReleaseCopyInput) (memory.ForeignProof, error) {
	source, err := d.source(ctx, scope, ref.ContentRef)
	if err != nil {
		return memory.ForeignProof{}, err
	}
	return source.Release(ctx, scope, auth, ref, report)
}

func (d deviceSources) VerifyTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref memory.ForeignReference, proof memory.ForeignProof) error {
	if tx.Scope() != d.a.Scope || ref.ContentRef.TenantID != tx.Scope().TenantID || d.a.remoteExecutors == nil {
		return api.E("forbidden", "foreign_source_scope_mismatch")
	}
	// 本次证明取得前必已在 Tx 外建立准确客户端。这里不拨号、不打开 journal。
	route := d.a.remoteExecutors.routes[ref.ContentRef.OwnerID]
	var source *executor.SourceClient
	if route != nil {
		route.mu.Lock()
		if route.Client != nil && !route.closed {
			source = &executor.SourceClient{Client: route.Client, Keys: route.Keys}
		}
		route.mu.Unlock()
	}
	if source == nil {
		return api.E("dependency_unavailable", "paired_source_current_required")
	}
	return source.VerifyTx(ctx, tx, auth, ref, proof)
}

var _ memory.ForeignContentPort = deviceSources{}
