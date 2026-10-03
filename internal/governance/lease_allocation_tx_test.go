package governance_test

import (
	"errors"
	"testing"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func TestOriginalLeaseAllocationSharesAdmissionRollbackAndConsumesOnceOnlyOnce(t *testing.T) {
	f := environment(t, governance.Options{})
	grant := issue(t, f, "once")
	scope := useRequest(f, grant)
	endpoint := api.NewID("executor")
	instance := api.NewID("instance")
	id := api.NewID("lease")
	input := governance.LeaseAllocate{LeaseID: id, EndpointID: endpoint, InstanceID: instance, Scope: scope, Limits: []api.Amount{{Unit: "USD", Value: "2"}}, ExpiresAt: scope.StartBefore, CostMode: "strict"}
	var lease governance.GrantLease
	rollback := errors.New("host admission rolls back")
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		var err error
		lease, err = f.svc.AllocateLeaseTx(f.ctx, tx, f.auth, input)
		if err != nil {
			return err
		}
		return rollback
	})
	if status != runtime.RolledBack || !errors.Is(err, rollback) {
		t.Fatalf("original lease admission must run in host Tx: %s %v", status, err)
	}
	current := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: grant.GrantID})
	if current.OnceConsumed || len(current.Reserved) != 0 {
		t.Fatalf("rollback consumed original authority: %+v", current)
	}
	status, err = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		var err error
		lease, err = f.svc.AllocateLeaseTx(f.ctx, tx, f.auth, input)
		return err
	})
	if status != runtime.Committed || err != nil {
		t.Fatalf("original allocation %s %v", status, err)
	}
	read := query[governance.GrantLease](t, f, "grant.lease.read", governance.IDInput{ID: id})
	if read.LeaseID != id || read.EndpointID != endpoint || read.InstanceID != instance || !api.Equal(read, lease) {
		t.Fatalf("host Tx did not persist exact original lease %+v %+v", read, lease)
	}
	current = query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: grant.GrantID})
	if !current.OnceConsumed || len(current.Reserved) != 1 || current.Reserved[0].Value != "2" {
		t.Fatalf("single original once/reservation %+v", current)
	}
	input.LeaseID = api.NewID("lease")
	status, err = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error { _, err := f.svc.AllocateLeaseTx(f.ctx, tx, f.auth, input); return err })
	if status != runtime.RolledBack || err == nil {
		t.Fatalf("another allocation revived once: %s %v", status, err)
	}
	current = query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: grant.GrantID})
	if len(current.Reserved) != 1 || current.Reserved[0].Value != "2" {
		t.Fatalf("failed second lease double reserved %+v", current)
	}
}
