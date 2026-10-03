package governance_test

import (
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"
)

func TestGrantManagementDetectsOriginalWithoutOverwritingPermission(t *testing.T) {
	f := environment(t, governance.Options{})
	id := api.NewID("grant")
	status, e := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		exists, e := f.svc.GrantExistsTx(f.ctx, tx, f.auth, id)
		if e != nil {
			return e
		}
		if exists {
			t.Fatal("new grant already exists")
		}
		return nil
	})
	if e != nil || status != runtime.Committed {
		t.Fatal(e)
	}
	grant := api.Grant{GrantID: id, OwnerID: f.scope.OwnerID, Revision: 1, SubjectRef: f.auth.Ref(f.scope.OwnerID), Resources: []string{"document"}, Actions: []string{"write"}, Purposes: []string{"save"}, Recipients: []string{"executor"}, Locations: []string{"device"}, Mode: "once", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: api.Time(time.Now().Add(time.Hour)), Limits: []api.Amount{{Unit: "USD", Value: "10"}}}
	_, e = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error { return f.svc.ProvisionGrantTx(f.ctx, tx, f.auth, grant) })
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		exists, e := f.svc.GrantExistsTx(f.ctx, tx, f.auth, id)
		if e != nil {
			return e
		}
		if !exists {
			t.Fatal("original grant hidden from trusted initializer")
		}
		ordinary := f.auth
		ordinary.Roles = nil
		if _, e = f.svc.GrantExistsTx(f.ctx, tx, ordinary, id); e == nil {
			t.Fatal("ordinary subject read management fact")
		}
		other := f.auth
		other.TenantID = api.NewID("tenant")
		if _, e = f.svc.GrantExistsTx(f.ctx, tx, other, id); e == nil {
			t.Fatal("cross tenant management fact")
		}
		return nil
	})
	if e != nil {
		t.Fatal(e)
	}
	_, e = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error { return f.svc.ProvisionGrantTx(f.ctx, tx, f.auth, grant) })
	if !api.IsCode(e, "revision_conflict") {
		t.Fatalf("provision silently changed original: %v", e)
	}
}
