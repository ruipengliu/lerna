package development

import (
	"context"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type existingContentLookup struct {
	ContentRef api.ContentRef `json:"content_ref"`
}
type capabilityLookup struct {
	CapabilityRef api.ComponentRef `json:"capability_ref"`
}
type originalFactLookup struct {
	Kind string        `json:"kind"`
	Ref  api.ObjectRef `json:"ref"`
}
type memoryLookupReport struct {
	LookupID string                 `json:"lookup_id"`
	Input    memory.QueryInput      `json:"input"`
	Page     api.Page[memory.Match] `json:"page"`
}

// ContextLookupSchemas 是 need_context 的有限只读查询合同；没有 URL/搜索/工具动作。
// memory_query 正文直接采用 memory.query 的闭合输入，owner 只收窄原期限。
func ContextLookupSchemas() map[string]api.Schema {
	fact := api.SchemaFor[originalFactLookup]()
	fact["properties"].(map[string]any)["kind"] = api.Enum("task", "operation", "check")
	return map[string]api.Schema{"existing_content": api.SchemaFor[existingContentLookup](), "memory_query": api.SchemaFor[memory.QueryInput](), "capability_describe": api.SchemaFor[capabilityLookup](), "original_fact": fact}
}

type contextLookup struct{ a *App }

func (r contextLookup) Resolve(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in task.ContextLookupRequest) (task.ContextLookupResult, error) {
	out := task.ContextLookupResult{Materials: []task.ContextMaterial{}}
	if !api.Equal(scope, r.a.Scope) || !api.ValidID(in.LookupID) || in.MaxBytes < in.Lookup.QueryRef.ByteLength || in.MaxBytes > api.MaxJSONBytes || in.MaxTokens < in.MaxBytes {
		return out, api.E("invalid_request", "invalid_original_context_budget")
	}
	if err := r.a.Identity.CheckCurrent(ctx, auth); err != nil {
		return out, err
	}
	t, err := r.a.Task.Read(ctx, r.a.Store, scope, auth, in.TaskRef.ObjectID)
	if err != nil {
		return out, err
	}
	if t.Status != "active" || t.Control != "running" || t.GoalRevision != in.GoalRevision || t.ControlRevision != in.ControlRevision {
		return out, api.E("revision_conflict", "context_lookup_control_stale")
	}
	expires, err := api.ParseTime(in.ExpiresAt)
	if err != nil {
		return out, err
	}
	now, err := r.a.now(ctx, scope)
	if err != nil {
		return out, err
	}
	if !now.Before(expires) {
		return out, api.E("expired", "original_context_lookup_expired")
	}
	bounded, cancel := context.WithDeadline(ctx, expires)
	defer cancel()
	ctx = bounded
	var snap api.Snapshot
	status, err := r.a.Store.Within(ctx, scope, []string{"task"}, func(tx runtime.Tx) error {
		var e error
		snap, e = r.a.Task.DecisionSnapshotTx(ctx, tx, r.a.ServiceAuth, in.DecisionID)
		return e
	})
	if status == runtime.CommitUnknown {
		return out, runtime.ErrCommitUnknown
	}
	if err != nil {
		return out, err
	}
	if snap.TaskRef.ObjectID != t.TaskID || snap.GoalRevision != in.GoalRevision || snap.ControlRevision != in.ControlRevision {
		return out, api.E("forbidden", "original_context_snapshot_mismatch")
	}
	query, err := r.a.Memory.Read(ctx, scope, auth, in.Lookup.QueryRef, "task.context")
	if err != nil {
		return out, err
	}
	schema, ok := ContextLookupSchemas()[in.Lookup.Kind]
	if !ok {
		return out, api.E("unsupported", "context_lookup_kind_not_registered")
	}
	v, err := api.NewValidator(schema)
	if err != nil {
		return out, err
	}
	if err = v.Validate(query); err != nil {
		return out, err
	}
	readBound := uint64(len(query))
	add := func(ref api.ContentRef, memoryRef *api.ObjectRef) error {
		if ref.ByteLength > in.MaxBytes-readBound {
			return api.E("overloaded", "original_context_byte_limit")
		}
		body, e := r.a.Memory.Read(ctx, scope, auth, ref, "task.context")
		if e != nil {
			return e
		}
		readBound += uint64(len(body))
		out.Materials = append(out.Materials, task.ContextMaterial{ContentRef: ref, MemoryRef: memoryRef, Kind: in.Lookup.Kind, LookupRef: scope.Ref(in.LookupID, 1)})
		return nil
	}
	sources := []api.ContentRef{in.Lookup.QueryRef}
	publish := func(media string, body []byte) error {
		if uint64(len(body)) > in.MaxBytes-readBound {
			return api.E("overloaded", "original_context_byte_limit")
		}
		ref, e := r.a.Publish(ctx, scope, auth, stableID("content", "lookup/"+in.LookupID), media, body, uniqueSources(sources), []api.ContentRef{})
		if e != nil {
			return e
		}
		// 上界包括本次输出材料的完整 bytes；没有将引用数当 token 数。
		readBound += uint64(len(body))
		out.Materials = append(out.Materials, task.ContextMaterial{ContentRef: ref, Kind: in.Lookup.Kind, LookupRef: scope.Ref(in.LookupID, 1)})
		return nil
	}
	matchTarget := func(ref api.ContentRef) bool {
		return api.Equal(in.Lookup.TargetRef, api.ObjectRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID, ObjectID: ref.ContentID, Revision: ref.Version})
	}
	switch in.Lookup.Kind {
	case "existing_content":
		var q existingContentLookup
		if err = api.Decode(query, &q); err != nil {
			return out, err
		}
		if !matchTarget(q.ContentRef) {
			return out, api.E("forbidden", "context_lookup_target_mismatch")
		}
		err = add(q.ContentRef, nil)
	case "memory_query":
		var q memory.QueryInput
		if err = api.Decode(query, &q); err != nil {
			return out, err
		}
		if !matchTarget(q.ScopeRef) || q.Cursor != "" || len(q.Purposes) != 1 || q.Purposes[0] != "task.context" || q.Limit > 5 || q.Limits.MaxReadBytes > (in.MaxBytes-readBound)/2 {
			return out, api.E("invalid_request", "context_memory_query_limits")
		}
		deadline, e := api.ParseTime(q.Limits.Deadline)
		if e != nil {
			return out, e
		}
		if expires.Before(deadline) {
			q.Limits.Deadline = in.ExpiresAt // 仅原批次允许的较早期限，重试不刷新。
		}
		page, e := r.a.Memory.QueryMemory(ctx, scope, auth, in.LookupID, q)
		if e != nil {
			return out, e
		}
		readBound += q.Limits.MaxReadBytes // 包含 query/text 与隐藏扫描的保守累计上界。
		sources = append(sources, q.QueryRef, q.ScopeRef)
		for _, match := range page.Items {
			record, e := r.a.Memory.ReadMemory(ctx, scope, auth, memory.ReadMemoryInput{MemoryID: match.MemoryRef.ObjectID, Revision: match.MemoryRef.Revision, Purpose: "task.context"})
			if e != nil {
				return out, e
			}
			if record.Revision != match.MemoryRef.Revision || !api.Equal(record.Values.ContentRef, match.ContentRef) {
				return out, api.E("revision_conflict", "context_memory_changed")
			}
			ref := match.MemoryRef
			if e = add(match.ContentRef, &ref); e != nil {
				return out, e
			}
			sources = append(sources, match.ContentRef, record.Values.ScopeRef)
		}
		err = publish("application/vnd.harness.memory-query-report+json", api.Raw(memoryLookupReport{in.LookupID, q, page}))
	case "capability_describe":
		var q capabilityLookup
		if err = api.Decode(query, &q); err != nil {
			return out, err
		}
		if in.Lookup.TargetRef.ObjectID != q.CapabilityRef.ComponentID || in.Lookup.TargetRef.OwnerID != scope.OwnerID || in.Lookup.TargetRef.TenantID != scope.TenantID || in.Lookup.TargetRef.Revision != 1 {
			return out, api.E("forbidden", "context_lookup_target_mismatch")
		}
		declared := false
		for _, ref := range snap.CapabilityRefs {
			declared = declared || api.Equal(ref, q.CapabilityRef)
		}
		if !declared {
			return out, api.E("forbidden", "capability_not_in_original_snapshot")
		}
		var frozen actionSnapshot
		if _, err = r.a.Store.Read(ctx, scope, "platform.action_snapshots", snap.SnapshotID, 1, &frozen); err != nil {
			return out, err
		}
		found := false
		for _, entry := range frozen.Entries {
			if !api.Equal(entry.Capability.Ref, q.CapabilityRef) {
				continue
			}
			found = true
			body, e := r.a.prepareContextCapability(ctx, scope, in, entry)
			if e != nil {
				return out, e
			}
			err = publish("application/vnd.harness.capability-description+json", body)
			break
		}
		if !found {
			return out, api.E("forbidden", "capability_not_prepared")
		}
	case "original_fact":
		var q originalFactLookup
		if err = api.Decode(query, &q); err != nil {
			return out, err
		}
		if !api.Equal(in.Lookup.TargetRef, q.Ref) {
			return out, api.E("forbidden", "context_lookup_target_mismatch")
		}
		declared := api.Equal(q.Ref, snap.TaskRef)
		for _, ref := range snap.FactRefs {
			declared = declared || api.Equal(q.Ref, ref)
		}
		if !declared {
			return out, api.E("forbidden", "fact_not_in_original_snapshot")
		}
		sources = append(sources, in.SnapshotRef)
		var fact any
		switch q.Kind {
		case "task":
			if !api.Equal(q.Ref, snap.TaskRef) {
				return out, api.E("forbidden", "fact_kind_mismatch")
			}
			raw, e := r.a.Dispatcher.Query(ctx, auth, api.Raw(api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, QueryID: stableID("query", in.LookupID), Method: "task.read", TargetID: q.Ref.ObjectID, Payload: api.Raw(task.ReadInput{Revision: q.Ref.Revision})}))
			if e != nil {
				return out, e
			}
			var original api.Task
			if e = api.Decode(raw, &original); e != nil {
				return out, e
			}
			fact = original
		case "operation":
			operation, e := (executionBridge{r.a}).Read(ctx, scope, q.Ref)
			if e != nil {
				return out, e
			}
			if operation.Revision != q.Ref.Revision || operation.TaskRef.ObjectID != t.TaskID {
				return out, api.E("revision_conflict", "original_operation_fact_changed")
			}
			fact = operation
			sources = append(sources, operation.EvidenceRefs...)
			if operation.ResultRef != nil {
				sources = append(sources, *operation.ResultRef)
			}
		case "check":
			facts, e := r.a.Task.ContextFacts(ctx, r.a.Store, scope, auth, t.TaskID)
			if e != nil {
				return out, e
			}
			for _, check := range facts.Checks {
				if check.CheckID == q.Ref.ObjectID && q.Ref.Revision == 1 {
					fact = check
					sources = append(sources, check.ArtifactRef, check.ScopeRef)
					sources = append(sources, check.EvidenceRefs...)
				}
			}
			if fact == nil {
				return out, api.E("revision_conflict", "original_fact_changed")
			}
		}
		if q.Kind == "operation" && !strings.HasPrefix(q.Ref.ObjectID, "operation_") {
			return out, api.E("forbidden", "fact_kind_mismatch")
		}
		err = publish("application/vnd.harness.original-fact+json", api.Raw(struct {
			Ref  api.ObjectRef `json:"ref"`
			Fact any           `json:"fact"`
		}{q.Ref, fact}))
	}
	if err != nil {
		return out, err
	}
	out.ReadBytesUpperBound, out.TokensBound = readBound, readBound
	return out, nil
}

func (r contextLookup) CheckMaterialsTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, materials []task.ContextMaterial) error {
	if len(materials) > 64 {
		return api.E("overloaded", "context_material_capacity")
	}
	refs := []api.ObjectRef{}
	seen := map[string]api.ObjectRef{}
	for _, material := range materials {
		if material.MemoryRef != nil {
			if prior, ok := seen[material.MemoryRef.ObjectID]; ok {
				if !api.Equal(prior, *material.MemoryRef) {
					return api.E("invalid_request", "context_memory_reference_conflict")
				}
				continue
			}
			refs = append(refs, *material.MemoryRef)
			seen[material.MemoryRef.ObjectID] = *material.MemoryRef
		}
	}
	if _, err := r.a.Memory.CheckMemoriesTx(ctx, tx, auth, refs, "task.context"); err != nil {
		return err
	}
	for _, material := range materials {
		if err := r.a.checkContextCapabilityTx(ctx, tx, material); err != nil {
			return err
		}
		if _, err := r.a.Memory.CheckContentTx(ctx, tx, auth, material.ContentRef, "task.context", "cloud", true); err != nil {
			return err
		}
		if material.QueryRef != nil {
			if _, err := r.a.Memory.CheckContentTx(ctx, tx, auth, *material.QueryRef, "task.context", "cloud", true); err != nil {
				return err
			}
		}
	}
	return nil
}

// SnapshotID 的私有有限查询期限在任何模型/查询出口之前固定；恢复不续期。
func (a *App) freezeContextLookupDeadline(ctx context.Context, scope runtime.Scope, snapshotID, taskDeadline string) (string, error) {
	var frozen struct {
		Deadline     string `json:"deadline"`
		TaskDeadline string `json:"task_deadline"`
	}
	status, err := a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, "platform.context_lookup_deadlines", snapshotID, &frozen)
		if err == nil {
			if frozen.TaskDeadline != taskDeadline {
				return api.E("idempotency_conflict", "original_lookup_deadline_changed")
			}
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		until, err := api.ParseTime(taskDeadline)
		if err != nil {
			return err
		}
		if bound := now.Add(5 * time.Minute); bound.Before(until) {
			until = bound
		}
		frozen.Deadline, frozen.TaskDeadline = api.Time(until), taskDeadline
		return tx.Create(ctx, "platform.context_lookup_deadlines", snapshotID, scope.OwnerID, frozen)
	})
	if status == runtime.CommitUnknown {
		return "", runtime.ErrCommitUnknown
	}
	return frozen.Deadline, err
}
