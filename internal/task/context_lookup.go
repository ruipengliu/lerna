package task

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const contextBatches = "task.context_lookup_batches"
const contextWaitPrefix = "original_context_lookup:"

func clearContextWait(t *taskState) {
	t.PendingContextID = ""
	waits := []api.WaitReason{}
	for _, reason := range t.Task.WaitReasons {
		if !strings.HasPrefix(reason.ResumeCondition, contextWaitPrefix) {
			waits = append(waits, reason)
		}
	}
	t.Task.WaitReasons = waits
}

type contextBatch struct {
	ID        string                 `json:"id"`
	Revision  uint64                 `json:"revision"`
	TaskID    string                 `json:"task_id"`
	State     string                 `json:"state"`
	Requests  []ContextLookupRequest `json:"requests"`
	Next      uint64                 `json:"next"`
	Materials []ContextMaterial      `json:"materials"`
}

func contextID(prefix, value string) string { return prefix + "_" + api.Hash([]byte(value))[7:39] }
func validLookupKind(kind string) bool {
	switch kind {
	case "existing_content", "memory_query", "capability_describe", "original_fact":
		return true
	}
	return false
}
func (s *Service) beginContextLookupsTx(ctx context.Context, tx runtime.Tx, t *taskState, d decisionState, lookups []ContextLookup) error {
	if len(lookups) < 1 || len(lookups) > 3 || t.PendingContextID != "" {
		return invalid("context_lookup_required")
	}
	budget := t.ContextBudget
	if budget.Limits.MaxCalls == 0 {
		budget.Limits = s.config.ContextLimits
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	expiry, err := api.ParseTime(t.Task.Deadline)
	if err != nil {
		return err
	}
	if expiry.After(now.Add(5 * time.Minute)) {
		expiry = now.Add(5 * time.Minute)
	}
	batch := contextBatch{ID: contextID("context", d.Intent.DecisionID), Revision: 1, TaskID: t.Task.TaskID, State: "pending", Requests: []ContextLookupRequest{}, Materials: []ContextMaterial{}}
	seen := map[string]bool{}
	queryRefs := []api.ContentRef{}
	targets := []api.ObjectRef{}
	for i, lookup := range lookups {
		if !validLookupKind(lookup.Kind) {
			return invalid("unsupported_context_lookup_kind")
		}
		if err = runtime.CheckRef(tx.Scope(), lookup.TargetRef); err != nil {
			return err
		}
		if err = contentScope(tx.Scope(), lookup.QueryRef); err != nil {
			return err
		}
		digest, e := api.Digest(lookup)
		if e != nil {
			return e
		}
		if seen[digest] {
			return invalid("duplicate_context_lookup")
		}
		seen[digest] = true
		if budget.Calls >= budget.Limits.MaxCalls || budget.BytesBound >= budget.Limits.MaxBytes || budget.TokenBound >= budget.Limits.MaxTokens {
			return api.E("overloaded", "context_lookup_budget_exhausted")
		}
		bound := min(budget.Limits.PerLookupBytes, budget.Limits.MaxBytes-budget.BytesBound, budget.Limits.MaxTokens-budget.TokenBound)
		if bound <= lookup.QueryRef.ByteLength {
			return api.E("overloaded", "context_lookup_budget_exhausted")
		}
		budget.Calls++
		budget.BytesBound += bound
		budget.TokenBound += bound
		batch.Requests = append(batch.Requests, ContextLookupRequest{LookupID: contextID("lookup", batch.ID+"/"+string(api.Raw(i))), DecisionID: d.Intent.DecisionID, TaskRef: taskRef(tx, *t), SnapshotRef: d.Intent.SnapshotRef, GoalRevision: t.Task.GoalRevision, ControlRevision: t.Task.ControlRevision, Lookup: lookup, MaxBytes: bound, MaxTokens: bound, ExpiresAt: api.Time(expiry)})
		queryRefs = append(queryRefs, lookup.QueryRef)
		targets = append(targets, lookup.TargetRef)
	}
	if err = s.authorize(ctx, tx, submitterAuth(tx.Scope(), *t), "task.need_context", queryRefs, targets); err != nil {
		return err
	}
	if err = tx.Create(ctx, contextBatches, batch.ID, t.Task.TaskID, batch); err != nil {
		return err
	}
	t.ContextBudget = budget
	t.ContextRounds++
	t.PendingContextID = batch.ID
	ref := tx.Scope().Ref(batch.ID, 1)
	if len(t.Task.WaitReasons) >= 100 {
		return api.E("overloaded", "wait_reason_capacity")
	}
	t.Task.WaitReasons = append(t.Task.WaitReasons, api.WaitReason{Kind: "dependency", ObjectRef: &ref, ResumeCondition: contextWaitPrefix + " resolve the original bounded material queries"})
	if err = s.saveTask(ctx, tx, t); err != nil {
		return err
	}
	return queueJob(ctx, tx, JobContextLookup, "context/"+batch.ID, ref)
}

func contextDefinitive(err error) bool {
	var business *api.Error
	if !errors.As(err, &business) {
		return false
	}
	switch business.Code {
	case "forbidden", "invalid_request", "invalid_state", "revision_conflict", "expired", "gone", "unsupported":
		return true
	}
	return false
}
func (s *Service) contextBatchCurrentTx(ctx context.Context, tx runtime.Tx, t taskState, b contextBatch) error {
	if t.PendingContextID != b.ID || len(b.Requests) < 1 || len(b.Requests) > 3 || b.Next >= uint64(len(b.Requests)) {
		return api.E("revision_conflict", "context_lookup_stale")
	}
	r := b.Requests[b.Next]
	if r.GoalRevision != t.Task.GoalRevision || r.ControlRevision != t.Task.ControlRevision {
		return api.E("revision_conflict", "context_lookup_stale")
	}
	if err := s.CheckCurrent(ctx, tx, t, true); err != nil {
		return err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	expiry, err := api.ParseTime(r.ExpiresAt)
	if err != nil {
		return err
	}
	if !now.Before(expiry) {
		return api.E("expired", "context_lookup_expired")
	}
	return nil
}

// 每次领取至多读一个原查询。查询等待或恢复不会增加 Calls，也不会续原期限。
func (s *Service) contextLookupJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var birth contextBatch
	if _, err := store.Read(ctx, scope, contextBatches, work.Job.SourceRef.ObjectID, 1, &birth); err != nil {
		return err
	}
	var batch contextBatch
	var t taskState
	var gateErr error
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		var err error
		t, err = getTask(ctx, tx, birth.TaskID)
		if err != nil {
			return err
		}
		if _, err = tx.Get(ctx, contextBatches, birth.ID, &batch); err != nil {
			return err
		}
		if err = tx.Guard(ctx, work.Claim); err != nil {
			return err
		}
		if batch.State != "pending" {
			return nil
		}
		gateErr = s.contextBatchCurrentTx(ctx, tx, t, batch)
		if gateErr != nil && !contextDefinitive(gateErr) {
			return gateErr
		}
		return nil
	})
	if err != nil {
		return err
	}
	if batch.State != "pending" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if gateErr != nil {
		return s.finishContextLookup(ctx, store, scope, work, birth, nil, gateErr)
	}
	if s.ports.ContextLookup == nil {
		return s.wait(ctx, store, scope, work)
	}
	if err = s.preIO(ctx, store, scope, work); err != nil {
		return err
	}
	request := batch.Requests[batch.Next]
	expiry, err := api.ParseTime(request.ExpiresAt)
	if err != nil {
		return err
	}
	bounded, cancel := context.WithDeadline(ctx, expiry)
	result, readErr := s.ports.ContextLookup.Resolve(bounded, scope, submitterAuth(scope, t), request)
	cancel()
	if readErr != nil && !contextDefinitive(readErr) {
		return s.wait(ctx, store, scope, work)
	}
	return s.finishContextLookup(ctx, store, scope, work, birth, &result, readErr)
}

func (s *Service) finishContextLookup(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, birth contextBatch, result *ContextLookupResult, readErr error) error {
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		t, err := getTask(ctx, tx, birth.TaskID)
		if err != nil {
			return err
		}
		var b contextBatch
		if _, err = tx.Get(ctx, contextBatches, birth.ID, &b); err != nil {
			return err
		}
		if b.State != "pending" {
			return nil
		}
		currentErr := s.contextBatchCurrentTx(ctx, tx, t, b)
		if currentErr != nil && !contextDefinitive(currentErr) {
			return currentErr
		}
		if currentErr != nil {
			readErr = currentErr
		}
		if readErr == nil {
			if result == nil {
				return invalid("context_result_required")
			}
			request := b.Requests[b.Next]
			if result.ReadBytesUpperBound > request.MaxBytes || result.TokensBound > request.MaxTokens || result.TokensBound < result.ReadBytesUpperBound || len(result.Materials) < 1 || len(result.Materials) > 20 {
				readErr = invalid("context_result_budget_or_materials")
			}
			var declaredBytes uint64
			refs := []api.ContentRef{}
			objects := []api.ObjectRef{}
			for _, material := range result.Materials {
				if material.Kind != request.Lookup.Kind || !api.Equal(material.LookupRef, scope.Ref(request.LookupID, 1)) {
					readErr = invalid("context_result_identity")
				}
				if material.ContentRef.ByteLength > request.MaxBytes || declaredBytes > request.MaxBytes-material.ContentRef.ByteLength {
					readErr = invalid("context_result_budget_or_materials")
					break
				}
				declaredBytes += material.ContentRef.ByteLength
				refs = append(refs, material.ContentRef)
				if material.MemoryRef != nil {
					objects = append(objects, *material.MemoryRef)
				}
			}
			if declaredBytes > result.ReadBytesUpperBound {
				readErr = invalid("context_result_read_bound")
			}
			if readErr == nil {
				if err = s.authorize(ctx, tx, submitterAuth(scope, t), "task.context", refs, objects); err != nil {
					if !contextDefinitive(err) {
						return err
					}
					readErr = err
				}
			}
			if readErr == nil && len(t.ContextMaterials)+len(b.Materials)+len(result.Materials) > 64 {
				readErr = api.E("overloaded", "context_material_capacity")
			}
		}
		if readErr != nil {
			b.State = "blocked"
			if t.PendingContextID != b.ID || terminal(t) || b.Requests[b.Next].GoalRevision != t.Task.GoalRevision || b.Requests[b.Next].ControlRevision != t.Task.ControlRevision {
				b.State = "stale"
			} else {
				for i := range t.Task.WaitReasons {
					if t.Task.WaitReasons[i].ObjectRef != nil && t.Task.WaitReasons[i].ObjectRef.ObjectID == b.ID {
						t.Task.WaitReasons[i].ResumeCondition = contextWaitPrefix + " " + readErr.Error() + "; valid new input or goal change required"
					}
				}
				if err = s.saveTask(ctx, tx, &t); err != nil {
					return err
				}
			}
		} else {
			for i := range result.Materials {
				query := b.Requests[b.Next].Lookup.QueryRef
				result.Materials[i].QueryRef = &query
			}
			b.Materials = append(b.Materials, result.Materials...)
			b.Next++
			if b.Next == uint64(len(b.Requests)) {
				b.State = "completed"
				t.ContextMaterials = append(t.ContextMaterials, b.Materials...)
				t.PendingContextID = ""
				t.NoProgress = 0
				waits := []api.WaitReason{}
				for _, reason := range t.Task.WaitReasons {
					if !strings.HasPrefix(reason.ResumeCondition, contextWaitPrefix) {
						waits = append(waits, reason)
					}
				}
				t.Task.WaitReasons = waits
				if err = s.saveTask(ctx, tx, &t); err != nil {
					return err
				}
				if err = queueJob(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, t)); err != nil {
					return err
				}
			} else {
				if err = queueJob(ctx, tx, JobContextLookup, "context/"+b.ID, scope.Ref(b.ID, b.Revision+1)); err != nil {
					return err
				}
			}
		}
		b.Revision++
		return tx.Put(ctx, contextBatches, b.ID, b.Revision-1, b)
	})
}
