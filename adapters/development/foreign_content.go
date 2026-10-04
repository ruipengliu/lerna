package development

import (
	"context"

	"github.com/ruipengliu/lerna/adapters/executor"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type deviceContentRoute struct {
	ContentRef  api.ContentRef `json:"content_ref"`
	Operation   api.ObjectRef  `json:"operation"`
	RetainUntil string         `json:"retain_until"`
	RouteDigest string         `json:"route_digest"`
}

func contentRouteKey(ref api.ContentRef) (string, error) { return api.Digest(ref) }

// 准确公开Operation的ResultRef只提供引用路由，不提供正文读取许可。
// Retention上限沿原已签输入与本方policy冻结，实际源policy仍在Register/Current核验。
func (a *App) rememberDeviceResult(ctx context.Context, scope runtime.Scope, intent task.OperationIntent, ref api.ContentRef) error {
	route, err := a.remoteExecutors.binding(intent.BindingRef, intent.CapabilityRef, intent.InstallLockRef)
	if err != nil {
		return err
	}
	if ref.OwnerID != intent.ExecutorID || ref.TenantID != scope.TenantID || api.ValidateRecord("ContentRef", ref) != nil {
		return api.E("forbidden", "original_device_result_scope_mismatch")
	}
	var original executor.AdmissionBundle
	if _, err = a.Store.Read(ctx, scope, "platform.remote_bundles", intent.OperationID, 0, &original); err != nil {
		return err
	}
	until, err := api.ParseTime(a.ContentPolicy.Values.RetainUntil)
	if err != nil {
		return err
	}
	for _, source := range original.Contents {
		limit, err := api.ParseTime(source.RetainUntil)
		if err != nil {
			return err
		}
		if limit.Before(until) {
			until = limit
		}
	}
	value := deviceContentRoute{ref, remoteTarget(scope, intent.ExecutorID, intent.OperationID), api.Time(until), route.Digest}
	key, err := contentRouteKey(ref)
	if err != nil {
		return err
	}
	status, err := a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		var old deviceContentRoute
		if _, err := tx.Get(ctx, "platform.device_sources", key, &old); err == nil {
			if !api.Equal(old, value) {
				return api.E("idempotency_conflict", "original_device_result_route_changed")
			}
			return nil
		} else if !api.IsCode(err, "not_found") {
			return err
		}
		return tx.Create(ctx, "platform.device_sources", key, intent.TaskRef.ObjectID, value)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}

// 本方publication plan只用于有界路由；它不能授权来源，也不替换准确外部ContentRef。
func (a *App) foreignSources(ctx context.Context, scope runtime.Scope, refs []api.ContentRef) ([]api.ContentRef, error) {
	pending := append([]api.ContentRef{}, refs...)
	seen := map[api.ContentRef]bool{}
	foreign := []api.ContentRef{}
	for len(pending) > 0 {
		ref := pending[0]
		pending = pending[1:]
		if seen[ref] {
			continue
		}
		seen[ref] = true
		if len(seen) > 200 || len(pending) > 200 {
			return nil, api.E("overloaded", "source_closure_limit")
		}
		if ref.TenantID != scope.TenantID || api.ValidateRecord("ContentRef", ref) != nil {
			return nil, api.E("forbidden", "foreign_source_scope_mismatch")
		}
		if ref.OwnerID != scope.OwnerID {
			foreign = append(foreign, ref)
			continue
		}
		var original publicationPlan
		if _, err := a.Store.Read(ctx, scope, "platform.publications", ref.ContentID, 1, &original); err == nil {
			if original.Ref != ref {
				return nil, api.E("idempotency_conflict", "original_publication_source_changed")
			}
			pending = append(pending, original.Processed...)
			pending = append(pending, original.Disclosed...)
		} else if !api.IsCode(err, "not_found") {
			return nil, err
		}
	}
	return foreign, nil
}

func (a *App) prepareForeignSources(ctx context.Context, scope runtime.Scope, auth runtime.Auth, refs []api.ContentRef, purpose, location string) (context.Context, error) {
	if scope != a.Scope {
		return ctx, api.E("forbidden", "foreign_source_scope_mismatch")
	}
	if a.Memory.Foreign == nil {
		return ctx, nil
	}
	if currentForeignCarrier(ctx) == nil {
		carrier := &foreignUseCarrier{Scope: scope, uses: map[string]stagedForeignUse{}}
		ctx = memory.WithForeignUseProvider(context.WithValue(ctx, foreignCarrierKey{}, carrier), carrier)
	}
	status, err := a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		return currentCredentialTx(ctx, tx, auth)
	})
	if status == runtime.CommitUnknown {
		return ctx, runtime.ErrCommitUnknown
	}
	if err != nil {
		return ctx, err
	}
	sources, err := a.foreignSources(ctx, scope, refs)
	if err != nil {
		return ctx, err
	}
	for _, ref := range sources {
		// Agent 等其它明确来源只沿已有 reference/held gate 刷新；宿主不替它
		// 发明 device route 或创建新的跨 owner copy。
		if a.remoteExecutors == nil || a.remoteExecutors.routes[ref.OwnerID] == nil {
			continue
		}
		key, err := contentRouteKey(ref)
		if err != nil {
			return ctx, err
		}
		var original deviceContentRoute
		if _, err = a.Store.Read(ctx, scope, "platform.device_sources", key, 1, &original); err != nil {
			return ctx, err
		}
		route := a.remoteExecutors.routes[ref.OwnerID]
		if route == nil || original.ContentRef != ref || original.RouteDigest != route.Digest {
			return ctx, api.E("forbidden", "original_device_source_not_paired")
		}
		identity, err := api.Digest([]any{scope, auth.Ref(scope.OwnerID), ref, purpose, location})
		if err != nil {
			return ctx, err
		}
		request := memory.ForeignReference{ContentRef: ref, CopyID: stableID("copy", identity), RegisterCommandID: stableID("command", identity+"/register"), ReleaseCommandID: stableID("command", identity+"/release"), ReferenceIntentRef: scope.Ref(stableID("reference", identity), 1), HolderRef: auth.Ref(scope.OwnerID), Purpose: purpose, Location: location, RetainUntil: original.RetainUntil}
		use, err := a.Memory.PrepareForeignUse(ctx, scope, auth, request)
		if err != nil {
			return ctx, err
		}
		if err = stageForeignUse(ctx, scope, auth, use.Reference, use.Proof); err != nil {
			return ctx, err
		}
	}
	// 已有Memory派生来源的闭包仍由Memory沿本方held记录复核；无法路由即拒绝。
	return a.Memory.PrepareForeignContext(ctx, scope, auth, refs, purpose, location)
}

// ReadContent 是宿主明确的普通读取；任何新使用都在Tx外取得当前来源证明。
func (a *App) ReadContent(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref api.ContentRef, purpose string) ([]byte, error) {
	return a.ReadContentBytes(ctx, scope, auth, ref, purpose, "cloud")
}

func (a *App) ReadContentBytes(ctx context.Context, scope runtime.Scope, auth runtime.Auth, ref api.ContentRef, purpose, location string) ([]byte, error) {
	ctx, err := a.prepareForeignSources(ctx, scope, auth, []api.ContentRef{ref}, purpose, location)
	if err != nil {
		return nil, err
	}
	// 请求地点和本地介质地点都由 Memory 的原完整门禁核验；本次入口
	// 必须分别取得同一准确用途的当前来源证明，不能借用上次 cloud 许可。
	if location != a.Memory.Location {
		ctx, err = a.prepareForeignSources(ctx, scope, auth, []api.ContentRef{ref}, purpose, a.Memory.Location)
		if err != nil {
			return nil, err
		}
	}
	return a.Memory.ReadBytes(ctx, scope, auth, ref, purpose, location)
}

// 提议投影只按下游真实用例的准确用途取当前来源证明，不能把brain.output
// 的许可改称 task.complete/task.action。最终Task共同提交仍逐项纯门禁。
func (a *App) prepareProposalSources(ctx context.Context, scope runtime.Scope, proposal task.Proposal) error {
	prepare := func(refs []api.ContentRef, purpose string) error {
		var err error
		ctx, err = a.prepareForeignSources(ctx, scope, a.ServiceAuth, uniqueSources(refs), purpose, "cloud")
		return err
	}
	switch proposal.Kind {
	case "complete":
		refs := []api.ContentRef{}
		for _, request := range proposal.CheckRequests {
			refs = append(refs, request.ArtifactRef)
			refs = append(refs, request.EvidenceRefs...)
		}
		if len(refs) > 0 {
			if err := prepare(refs, "task.attach_evidence"); err != nil {
				return err
			}
		}
		return prepare(proposal.ArtifactRefs, "task.complete")
	case "act":
		refs := []api.ContentRef{}
		for _, action := range proposal.Actions {
			refs = append(refs, action.ArgumentsRef, action.ResourcesRef)
			refs = append(refs, action.ProcessedSourceRefs...)
		}
		return prepare(refs, "task.action")
	case "need_context":
		return prepare(proposal.ContextRefs, "task.need_context")
	}
	return nil
}
