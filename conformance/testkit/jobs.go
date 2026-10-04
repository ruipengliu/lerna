// Package testkit 在真实 Store 边界观察原责任，不替代提交、回执或领域裁决。
package testkit

import (
	"context"
	"errors"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

type JobBoundary struct {
	runtime.Store
	runtime.QueryBindingStore
	Jobs          []api.Job
	ForbidChanges bool
}

func ObserveJobs(store runtime.Store) *JobBoundary {
	binding, _ := store.(runtime.QueryBindingStore)
	return &JobBoundary{Store: store, QueryBindingStore: binding}
}
func (s *JobBoundary) Within(ctx context.Context, scope runtime.Scope, parts []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, parts, func(tx runtime.Tx) error { return fn(jobTx{Tx: tx, owner: s}) })
}

type jobTx struct {
	runtime.Tx
	owner *JobBoundary
}

func (tx jobTx) Peek(ctx context.Context, namespace, id string, value any) (uint64, error) {
	reader, ok := tx.Tx.(runtime.TxSnapshotReader)
	if !ok {
		return 0, api.E("unsupported", "route_snapshot_unconfigured")
	}
	return reader.Peek(ctx, namespace, id, value)
}

func (tx jobTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error { return fn(jobTx{Tx: inner, owner: tx.owner}) })
}
func (tx jobTx) Raise(ctx context.Context, kind, key string, source api.ObjectRef, due time.Time) (api.Job, error) {
	if tx.owner.ForbidChanges {
		return api.Job{}, errors.New("same-state control attempted to change original Job responsibility")
	}
	job, err := tx.Tx.Raise(ctx, kind, key, source, due)
	if err == nil {
		tx.owner.Jobs = append(tx.owner.Jobs, job)
	}
	return job, err
}
func (tx jobTx) Hint(ctx context.Context, id string, due time.Time) error {
	if tx.owner.ForbidChanges {
		return errors.New("same-state control attempted to move original Job due")
	}
	return tx.Tx.Hint(ctx, id, due)
}
