package worker

import (
	"context"
	"lerna/authorization"
	"lerna/tasks"
	"sync"
	"time"
)

// temporaryRuntime independently implements the runtime seam for the common
// task contract only. Its fixed identity is a fixture, not an auth backend.
type temporaryRuntime struct {
	mu        sync.Mutex
	data      []byte
	operation bool
}
type temporaryTransaction struct {
	data      []byte
	operation bool
}

func (m *temporaryRuntime) UpdateRuntime(ctx context.Context, fn func(authorization.RuntimeTransaction) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	tx := &temporaryTransaction{data: append([]byte(nil), m.data...), operation: m.operation}
	if err := fn(tx); err != nil {
		return err
	}
	m.data = append([]byte(nil), tx.data...)
	m.operation = tx.operation
	return nil
}
func (t *temporaryTransaction) Data() []byte        { return t.data }
func (t *temporaryTransaction) SetData(data []byte) { t.data = append([]byte(nil), data...) }
func (t *temporaryTransaction) Namespace() string   { return "local" }
func (t *temporaryTransaction) Now() time.Time      { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) }
func (t *temporaryTransaction) Authorize(token, resource, action string) (authorization.Identity, error) {
	if token != "fixture-only" || resource != "root" || (action != "task.submit" && action != "task.read" && action != "task.execute") {
		return authorization.Identity{}, &authorization.Error{Code: authorization.Denied}
	}
	return authorization.Identity{Namespace: "local", Subject: "operator"}, nil
}
func (t *temporaryTransaction) Operation(id, subject string, claim bool) error {
	if id != "fixture-operation" || subject != "operator" {
		return &authorization.Error{Code: authorization.Invalid}
	}
	t.operation = t.operation || claim
	return nil
}
func memoryCheck(ctx context.Context) error {
	runtime := &temporaryRuntime{}
	s, err := tasks.New(runtime, taskConfig())
	if err != nil {
		return err
	}
	task, err := s.Submit(ctx, "fixture-only", tasks.Submission{Namespace: "local", OperationID: "fixture-operation", Goal: "script", Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: time.Date(2026, 9, 10, 0, 1, 0, 0, time.UTC).Unix()}})
	if err != nil {
		return err
	}
	p, err := s.BindWorker(tasks.WorkerBinding{Token: "fixture-only", Subject: "operator", WorkerID: "memory-worker"}, limits())
	if err != nil {
		return err
	}
	initial, err := s.Load(ctx, task.Ref)
	if err != nil {
		return err
	}
	claimed, err := p.Commit(ctx, mutation("claim", "claim", initial))
	if err != nil {
		return err
	}
	renewed, err := p.Commit(ctx, mutation("renew", "renew", claimed))
	if err != nil {
		return err
	}
	done, err := complete(ctx, p, "finish", renewed)
	if err != nil {
		return err
	}
	again, err := complete(ctx, p, "finish", renewed)
	if err != nil {
		return err
	}
	got, err := s.Get(ctx, "fixture-only", task.Ref)
	if err != nil {
		return err
	}
	return require(got.State == "COMPLETED" && got.Result == "scripted answer" && got.Attempts == 1 && again.LastCommit == done.LastCommit, "independent temporary runtime violated common contract")
}
