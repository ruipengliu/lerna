package task_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 仅在真实数据库的原 child→command 映射读取边界注入一次故障。
// 子 Session 仍使用独立真实 SQLite，原命令、Job 和 ChildHandle 不由测试代写。
type childCommandFaultStore struct {
	runtime.Store
	err error
}

func (s *childCommandFaultStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error {
		return fn(childCommandFaultTx{Tx: tx, store: s})
	})
}

type childCommandFaultTx struct {
	runtime.Tx
	store *childCommandFaultStore
}

func (tx childCommandFaultTx) Get(ctx context.Context, namespace, id string, out any) (uint64, error) {
	if namespace == "task.child_commands" && tx.store.err != nil {
		err := tx.store.err
		tx.store.err = nil
		return 0, err
	}
	return tx.Tx.Get(ctx, namespace, id, out)
}

func TestOriginalChildPreparationPreservesCommandMappingReadFailureAndRecovers(t *testing.T) {
	ctx := context.Background()
	probe := newSessionProbe(t, false)
	h := newHarness(t, task.Ports{Collaboration: probe})
	id := api.NewID("child")
	command := h.command("child.create", id, nil, task.ChildCreateInput{ChildID: id, SessionOwnerID: api.NewID("receiver"), SessionConfigRef: h.policy.PolicyRef, AgentBindingRef: h.scope.Ref(api.NewID("binding"), 1), InstallLockRef: h.policy.PolicyRef, AccessScopeRef: h.content("original fixed child access"), PrepareDeadline: api.Time(time.Now().Add(time.Minute))})
	receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(command))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("original child preparation %+v %v", receipt, err)
	}
	work, status, err := h.store.Claim(ctx, h.scope, api.NewID("worker"), []string{task.JobChildPrepare}, 1, time.Minute)
	if err != nil || status != runtime.Committed || len(work) != 1 {
		t.Fatalf("original child worker %s %v", status, err)
	}
	fault := errors.New("original child command mapping persistence fault")
	store := &childCommandFaultStore{Store: h.store, err: fault}
	handler, ok := h.dispatch.Registry.Job(task.JobChildPrepare)
	if !ok {
		t.Fatal("child worker missing")
	}
	if err = handler(ctx, store, h.scope, work[0]); !errors.Is(err, fault) {
		t.Fatalf("original mapping failure was relabelled: %v", err)
	}
	originalSession := probe.last
	stillAccepted, err := h.dispatch.Lookup(ctx, h.auth, command.CommandID)
	if err != nil || !api.Equal(stillAccepted, receipt) {
		t.Fatalf("read failure decided original command %+v %v", stillAccepted, err)
	}
	if err = handler(ctx, store, h.scope, work[0]); err != nil {
		t.Fatal(err)
	}
	opened, err := h.service.ChildRead(ctx, h.store, h.scope, h.auth, id)
	if err != nil || opened.ChildSessionRef == nil || !api.Equal(*opened.ChildSessionRef, originalSession) || opened.State != "open" {
		t.Fatalf("recovery changed the original child session %+v %v", opened, err)
	}
	applied, err := h.dispatch.Lookup(ctx, h.auth, command.CommandID)
	if err != nil || applied.Stage != "applied" {
		t.Fatalf("original child command did not recover %+v %v", applied, err)
	}
	replay, err := h.dispatch.Command(ctx, h.auth, api.Raw(command))
	if err != nil || !api.Equal(replay, applied) {
		t.Fatal("child retry changed original receipt")
	}
}
