package governance_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func TestOriginalUseHeadsCanQualifyFiniteCacheAfterStartWindowWithoutRevivingOnce(t *testing.T) {
	f := environment(t, governance.Options{})
	grant := issue(t, f, "once")
	input := useRequest(f, grant)
	input.StartBefore = api.Time(time.Now().Add(time.Second))
	_, used := command(t, f, "grant.use", input.UseID, input, nil)
	var original governance.UseReceipt
	if err := api.Decode(used.Output, &original); err != nil || original.Decision != "allowed" {
		t.Fatalf("original allowed use %+v %v", used, err)
	}
	until, err := api.ParseTime(original.StartBefore)
	if err != nil {
		t.Fatal(err)
	}
	if wait := time.Until(until) + time.Millisecond; wait > 0 {
		time.Sleep(wait)
	}
	var heads governance.UseReceipt
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		var err error
		heads, err = f.svc.CheckUseHeadsTx(f.ctx, tx, f.auth, f.scope.Ref(input.UseID, 1), input.TargetRef, input.IntentHash)
		if err != nil {
			return err
		}
		if err = f.svc.CheckUseTx(f.ctx, tx, f.auth, f.scope.Ref(input.UseID, 1), input.TargetRef, input.IntentHash, time.Now()); err == nil {
			t.Fatal("expired original window authorized another physical start")
		}
		return nil
	})
	if status != runtime.Committed || err != nil || !api.Equal(heads, original) {
		t.Fatalf("original cache heads changed or consumed original use: %s %v %+v", status, err, heads)
	}
	current := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: grant.GrantID})
	if !current.OnceConsumed || len(current.Reserved) != 1 || current.Reserved[0].Value != "2" {
		t.Fatalf("cache check altered original once/reservation %+v", current)
	}
	for _, bad := range []runtime.Auth{{TenantID: api.NewID("tenant"), SubjectID: f.auth.SubjectID, CredentialGeneration: 1}, {TenantID: f.auth.TenantID, SubjectID: f.auth.SubjectID, CredentialGeneration: 2}} {
		status, err = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
			_, err := f.svc.CheckUseHeadsTx(f.ctx, tx, bad, f.scope.Ref(input.UseID, 1), input.TargetRef, input.IntentHash)
			return err
		})
		if status != runtime.RolledBack || err == nil {
			t.Fatalf("cache heads accepted changed authenticated subject %s %v", status, err)
		}
	}
	originalCommand, revoked := command(t, f, "grant.revoke", grant.GrantID, governance.GrantRevoke{GrantRef: f.scope.Ref(grant.GrantID, grant.Revision), PreviewRefs: []api.ContentRef{ref(t, f, "preview")}, ConfirmationExpiresAt: api.Time(time.Now().Add(time.Minute))}, &grant.Revision)
	if revoked.Stage != "accepted" {
		t.Fatalf("revoke %+v", revoked)
	}
	approveOriginal(t, f, originalCommand, revoked)
	status, err = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		_, err := f.svc.CheckUseHeadsTx(f.ctx, tx, f.auth, f.scope.Ref(input.UseID, 1), input.TargetRef, input.IntentHash)
		return err
	})
	if status != runtime.RolledBack || !api.IsCode(err, "forbidden") {
		t.Fatalf("cache accepted original use after current Grant closure %s %v", status, err)
	}
}
