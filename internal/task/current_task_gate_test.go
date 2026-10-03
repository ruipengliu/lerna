package task_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type parentCurrentBoundary struct {
	withdrawn atomic.Bool
	calls     atomic.Uint64
}

func (*parentCurrentBoundary) Authorize(context.Context, runtime.Tx, runtime.Auth, string, []api.ContentRef, []api.ObjectRef) error {
	return nil
}
func (*parentCurrentBoundary) Evidence(context.Context, runtime.Tx, api.Task, []api.ObjectRef, []api.ComponentRef) error {
	return nil
}
func (g *parentCurrentBoundary) CheckTaskCurrentTx(_ context.Context, _ runtime.Tx, _ api.Task, running bool) error {
	g.calls.Add(1)
	if running && g.withdrawn.Load() {
		return api.E("dependency_unavailable", "original_parent_proof_unavailable")
	}
	return nil
}
func TestCurrentParentProofBlocksNewDecisionAndPreservesOriginalCancellation(t *testing.T) {
	ctx := context.Background()
	gate := &parentCurrentBoundary{}
	h := newHarness(t, task.Ports{Gate: gate})
	original := h.submit(t)
	if _, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(original, "0")); err != nil {
		t.Fatal(err)
	}
	gate.withdrawn.Store(true)
	current, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(current, "0")); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("new decision ignored unavailable original parent proof: %v", err)
	}
	if gate.calls.Load() == 0 {
		t.Fatal("current proof boundary was never checked")
	}
	after, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil || !api.Equal(current, after) {
		t.Fatalf("failed new admission changed original Task: %+v %v", after, err)
	}
	receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.cancel", after.TaskID, &after.Revision, task.ControlInput{TaskID: after.TaskID, Reason: "close original child work"})))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("unavailable parent proof blocked original cancellation: %+v %v", receipt, err)
	}
	closed, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil || closed.Status != "cancelled" {
		t.Fatalf("original cancellation did not close work: %+v %v", closed, err)
	}
}
