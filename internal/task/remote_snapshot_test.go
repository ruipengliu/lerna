package task_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestLatestTaskSnapshotUsesExactOriginalAndRejectsOldControlAfterPause(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, task.Ports{})
	original := h.submit(t)
	read := func() (task.TaskSnapshotView, bool, error) {
		var view task.TaskSnapshotView
		var found bool
		status, err := h.store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
			var err error
			view, found, err = h.service.LatestTaskSnapshotTx(ctx, tx, h.trusted(), original.TaskID)
			return err
		})
		if status == runtime.CommitUnknown {
			return view, false, runtime.ErrCommitUnknown
		}
		return view, found, err
	}
	if _, found, err := read(); err != nil || found {
		t.Fatalf("snapshot synthesized without original admission: %v %v", found, err)
	}
	first := h.prepared(original, "0")
	intent, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), first)
	if err != nil {
		t.Fatal(err)
	}
	view, found, err := read()
	if err != nil || !found || !api.Equal(view.Snapshot, first.Snapshot) || !api.Equal(view.Intent, intent) {
		t.Fatalf("did not return original admitted snapshot: %+v %v %v", view.Intent, found, err)
	}
	current, err := h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), h.prepared(current, "0")); !isTaskRejection(err, "invalid_state", "decision_pending") {
		t.Fatalf("unconsumed original snapshot lost pending gate: %v", err)
	}
	if out, err := h.service.ConsumeProposal(ctx, h.store, h.scope, h.trusted(), task.Proposal{DecisionID: first.DecisionID, Kind: "refine_requirements", ReasonRef: current.GoalRef}, nil); err != nil || out.Outcome != "rejected" {
		t.Fatalf("original proposal consumption %+v %v", out, err)
	}
	current, err = h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	second := h.prepared(current, "0")
	secondIntent, err := h.service.PrepareDecision(ctx, h.store, h.scope, h.trusted(), second)
	if err != nil {
		t.Fatal(err)
	}
	view, found, err = read()
	if err != nil || !found || !api.Equal(view.Snapshot, second.Snapshot) || !api.Equal(view.Intent, secondIntent) {
		t.Fatalf("did not choose greatest original Task revision: %+v %v %v", view.Intent, found, err)
	}
	current, err = h.service.Read(ctx, h.store, h.scope, h.auth, original.TaskID)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := h.dispatch.Command(ctx, h.auth, api.Raw(h.command("task.pause", current.TaskID, &current.Revision, task.ControlInput{TaskID: current.TaskID, Reason: "new original control generation"})))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("pause %+v %v", receipt, err)
	}
	if _, found, err = read(); err != nil || found {
		t.Fatalf("old control snapshot interpreted as current: %v %v", found, err)
	}
}
