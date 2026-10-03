package memory_test

import (
	"context"
	"os"
	"testing"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func TestCurrentMemoryTxRetainsExactAssertionAndCurrentSourceGates(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			var f fixture
			if driver == "sqlite" {
				f = newFixture(t)
			} else {
				if os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
					t.Skip("actual PostgreSQL fixture required")
				}
				store, err := postgres.Open(context.Background(), os.Getenv("HARNESS_TEST_POSTGRES_DSN"))
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := store.Close(); err != nil {
						t.Error(err)
					}
				})
				if err = store.Migrate(context.Background()); err != nil {
					t.Fatal(err)
				}
				f = newFixtureWithStore(t, store)
			}
			id := api.NewID("memory")
			source := f.upload(t, "actual licensed original preference")
			values := f.values(t, "preference format bullet")
			values.Type = "preference"
			values.Sources = []api.SourceEvidence{{ContentRef: source, SourceKind: "user_input"}}
			if r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values}); r.Stage != "applied" {
				t.Fatalf("memory create %+v", r)
			}
			check := func(auth runtime.Auth, ref api.ObjectRef) (memory.MemoryRecord, error) {
				var out memory.MemoryRecord
				status, err := f.service.Store.Within(f.ctx, f.scope, []string{"memory", "content"}, func(tx runtime.Tx) error {
					var e error
					out, e = f.service.CheckMemoryTx(f.ctx, tx, auth, ref, "memory.read")
					return e
				})
				if err == nil && status != runtime.Committed {
					t.Fatalf("memory check commit %v", status)
				}
				return out, err
			}
			ref := f.scope.Ref(id, 1)
			if record, err := check(f.auth, ref); err != nil || record.Revision != 1 || !api.Equal(record.Values, values) {
				t.Fatalf("legitimate exact current assertion denied %+v %v", record, err)
			}
			other := f.auth
			other.SubjectID = api.NewID("subject")
			if _, err := check(other, ref); !api.IsCode(err, "forbidden") {
				t.Fatalf("foreign subject saw current assertion: %v", err)
			}
			cross := ref
			cross.TenantID = api.NewID("tenant")
			if _, err := check(f.auth, cross); !api.IsCode(err, "forbidden") {
				t.Fatalf("cross-scope ref accepted: %v", err)
			}
			one := uint64(1)
			values.ContentRef = f.upload(t, "preference format plain")
			if r := f.command(t, "memory.replace", id, &one, memory.ReplaceInput{MemoryID: id, Values: values}); r.Stage != "applied" {
				t.Fatalf("replace %+v", r)
			}
			if _, err := check(f.auth, ref); !api.IsCode(err, "gone") {
				t.Fatalf("old MemoryRef still authorizes current material: %v", err)
			}
			ref.Revision = 2
			if record, err := check(f.auth, ref); err != nil || record.Revision != 2 || record.Values.ContentRef != values.ContentRef {
				t.Fatalf("accurate correction missing %+v %v", record, err)
			}
			if r := f.command(t, "content.close", source.ContentID, &one, memory.CloseInput{ContentRef: source, Reason: "withdraw original preference source"}); r.Stage != "applied" {
				t.Fatalf("source close %+v", r)
			}
			if _, err := check(f.auth, ref); !api.IsCode(err, "forbidden") {
				t.Fatalf("closed source still authorizes derived ordinary preference: %v", err)
			}
			record, err := f.service.InspectMemory(f.ctx, f.scope, f.auth, id)
			if err != nil || record.Revision != 2 || record.State != "active" {
				t.Fatalf("pure validation rewrote assertion %+v %v", record, err)
			}
		})
	}
}
