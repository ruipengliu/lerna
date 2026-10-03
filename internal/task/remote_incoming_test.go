package task_test

import (
	"context"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestRemoteIncomingCurrentKeepsCloseBeforeChildAndRejectsChangedScope(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, task.Ports{})
	parentOwner := api.NewID("owner")
	ref := api.ObjectRef{TenantID: h.scope.TenantID, OwnerID: parentOwner, ObjectID: api.NewID("allocation"), Revision: 1}
	parent := api.ObjectRef{TenantID: h.scope.TenantID, OwnerID: parentOwner, ObjectID: api.NewID("task"), Revision: 1}
	original := h.command("budget.close", ref.ObjectID, nil, task.AllocationCloseInput{AllocationRef: ref, ParentTaskRef: parent})
	receipt, err := h.dispatch.Command(ctx, h.trusted(), api.Raw(original))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("actual close before child: %+v %v", receipt, err)
	}
	read := func(auth runtime.Auth, source api.ObjectRef) (task.IncomingAllocation, runtime.CommitStatus, error) {
		var current task.IncomingAllocation
		status, err := h.store.Within(ctx, h.scope, []string{"task"}, func(tx runtime.Tx) error {
			var err error
			current, err = h.service.ReadIncomingAllocationTx(ctx, tx, auth, source)
			return err
		})
		return current, status, err
	}
	current, status, err := read(h.trusted(), ref)
	if status != runtime.Committed || err != nil || current.TaskRef != nil || current.Gate != "closed" || current.ParentTaskRef != parent || len(current.Cumulative) != 0 {
		t.Fatalf("unbound original tombstone changed: %s %+v %v", status, current, err)
	}
	wrongTenant := h.trusted()
	wrongTenant.TenantID = api.NewID("tenant")
	if _, status, err := read(wrongTenant, ref); status != runtime.RolledBack || !api.IsCode(err, "forbidden") {
		t.Fatalf("cross tenant accepted: %s %v", status, err)
	}
	foreign := ref
	foreign.OwnerID = api.NewID("owner")
	if _, status, err := read(h.trusted(), foreign); status != runtime.RolledBack || !api.IsCode(err, "not_found") {
		t.Fatalf("different source was mapped to original: %s %v", status, err)
	}
	if _, status, err := read(h.auth, ref); status != runtime.RolledBack || !api.IsCode(err, "forbidden") {
		t.Fatalf("ordinary child user read foreign allocation authority: %s %v", status, err)
	}
}
