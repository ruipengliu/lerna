package contract_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func TestDurableQueryBindingKeepsOriginalResultAndDeadline(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			f := fixture(t, backend, nil)
			scope := f.scope
			q, ok := f.store.(runtime.QueryBindingStore)
			if !ok {
				t.Fatal("adapter does not implement durable query identity")
			}
			ctx := context.Background()
			in := runtime.QueryBindingInput{QueryID: api.NewID("query"), PrincipalID: api.NewID("subject"), CredentialGeneration: 1, RolesDigest: api.Hash([]byte("roles")), QueryDigest: api.Hash([]byte("original exact query")), TTL: time.Minute}
			binding, status, e := q.BindQuery(ctx, scope, in)
			if e != nil || status != runtime.Committed || binding.ResultDigest != "" || !api.ValidID(binding.BindingID) {
				t.Fatalf("bind %+v %s %v", binding, status, e)
			}
			first := binding
			binding, status, e = q.SealQuery(ctx, scope, binding, api.Hash([]byte("current authorized result")))
			if e != nil || status != runtime.Committed {
				t.Fatalf("seal %s %v", status, e)
			}
			in.TTL = 5 * time.Minute
			repeated, status, e := q.BindQuery(ctx, scope, in)
			if e != nil || status != runtime.Committed || repeated.BindingID != first.BindingID || repeated.ExpiresAt != first.ExpiresAt || repeated.ResultDigest != binding.ResultDigest {
				t.Fatalf("repeat refreshed query %+v %s %v", repeated, status, e)
			}
			if _, status, e = q.SealQuery(ctx, scope, repeated, api.Hash([]byte("new snapshot"))); status != runtime.RolledBack || !api.IsCode(e, "cursor_expired") {
				t.Fatalf("query returned new snapshot %s %v", status, e)
			}
			in.QueryDigest = api.Hash([]byte("changed exact input"))
			if _, status, e = q.BindQuery(ctx, scope, in); status != runtime.RolledBack || !api.IsCode(e, "idempotency_conflict") {
				t.Fatalf("changed query reused identity %s %v", status, e)
			}
		})
	}
}
