package development

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/runtime"
)

// 只观察公开工作流使用的实际 Store；读不可变版本不取得头锁。
// Content 独立发布等不包含 Task 的事务照常执行，不替换任何事实。
type upstreamTaskStore struct {
	runtime.Store
	runtime.QueryBindingStore
	report func(error)
}
type upstreamTaskTx struct {
	runtime.Tx
	tasks  map[string]bool
	domain bool
	report func(error)
}

func (s upstreamTaskStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error {
		return fn(&upstreamTaskTx{Tx: tx, tasks: map[string]bool{}, report: s.report})
	})
}

func (tx *upstreamTaskTx) Get(ctx context.Context, namespace, id string, out any) (uint64, error) {
	if namespace == "task.tasks" && !tx.tasks[id] && tx.domain {
		err := fmt.Errorf("Task root first locked after platform or execution domain")
		if tx.report != nil {
			tx.report(err)
		}
		return 0, err
	}
	revision, err := tx.Tx.Get(ctx, namespace, id, out)
	if err == nil {
		if namespace == "task.tasks" {
			tx.tasks[id] = true
		} else {
			tx.domain = true
		}
	}
	return revision, err
}

func (tx *upstreamTaskTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		child := &upstreamTaskTx{Tx: inner, domain: tx.domain, tasks: map[string]bool{}, report: tx.report}
		for id, locked := range tx.tasks {
			child.tasks[id] = locked
		}
		if err := fn(child); err != nil {
			return err
		}
		tx.domain, tx.tasks = child.domain, child.tasks
		return nil
	})
}

func (tx *upstreamTaskTx) Peek(ctx context.Context, namespace, id string, out any) (uint64, error) {
	reader, ok := tx.Tx.(runtime.TxSnapshotReader)
	if !ok {
		return 0, fmt.Errorf("actual store lacks routing snapshot reader")
	}
	return reader.Peek(ctx, namespace, id, out)
}
