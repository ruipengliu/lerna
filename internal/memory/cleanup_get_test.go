package memory_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func TestPublicMemoryCleanupGetterRecoversOriginalMetadataOnlyResponsibility(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx := context.Background()
			path := filepath.Join(t.TempDir(), "cleanup.sqlite")
			open := func() runtime.Store {
				if driver == "postgres" {
					if os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
						t.Skip("actual PostgreSQL fixture required")
					}
					store, err := postgres.Open(ctx, os.Getenv("HARNESS_TEST_POSTGRES_DSN"))
					if err != nil {
						t.Fatal(err)
					}
					if err = store.Migrate(ctx); err != nil {
						t.Fatal(err)
					}
					return store
				}
				store, err := sqlite.Open(path)
				if err != nil {
					t.Fatal(err)
				}
				if err = store.Migrate(ctx); err != nil {
					t.Fatal(err)
				}
				return store
			}
			store := open()
			f := newFixtureWithStore(t, store)
			t.Cleanup(func() {
				if err := store.Close(); err != nil {
					t.Error(err)
				}
			})
			idForCleanup := api.NewID("memory")
			values := f.values(t, "metadata-only cleanup does not erase unrelated original Content")
			if r := f.command(t, "memory.create", idForCleanup, nil, memory.CreateInput{MemoryID: idForCleanup, Values: values}); r.Stage != "applied" {
				t.Fatalf("create %+v", r)
			}
			one := uint64(1)
			if r := f.command(t, "memory.delete", idForCleanup, &one, memory.DeleteInput{MemoryID: idForCleanup, Reason: "本人删除原记忆"}); r.Stage != "applied" {
				t.Fatalf("delete %+v", r)
			}
			query := func(auth runtime.Auth) (memory.MemoryRecord, error) {
				var out memory.MemoryRecord
				body, err := f.dispatcher.Query(ctx, auth, api.Raw(api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: api.NewID("query"), Method: "memory.cleanup.get", TargetID: idForCleanup, Payload: api.Raw(memory.ReadMemoryInput{MemoryID: idForCleanup})}))
				if err == nil {
					err = api.Decode(body, &out)
				}
				return out, err
			}
			pending, err := query(f.auth)
			if err != nil || pending.MemoryID != idForCleanup || pending.Revision != 2 || pending.State != "deleted" || pending.CleanupState != "pending" || !api.Equal(pending.Values, values) {
				t.Fatalf("public original pending cleanup metadata %+v %v", pending, err)
			}
			noRole := f.auth
			noRole.Roles = []string{}
			if _, err = query(noRole); !api.IsCode(err, "forbidden") {
				t.Fatalf("creator without current management role saw cleanup metadata %v", err)
			}
			cross := f.auth
			cross.TenantID = api.NewID("tenant")
			if _, err = query(cross); err == nil {
				t.Fatal("cross tenant saw original cleanup record")
			}
			for i := 0; i < 10; i++ {
				work, status, err := store.Claim(ctx, f.scope, api.NewID("worker"), []string{"memory.cleanup"}, 1, time.Minute)
				if err != nil || status != runtime.Committed {
					t.Fatalf("cleanup claim %v %v", status, err)
				}
				if len(work) == 0 {
					break
				}
				fn, ok := f.dispatcher.Registry.Job("memory.cleanup")
				if !ok {
					t.Fatal("original cleanup Job missing")
				}
				if err = fn(ctx, store, f.scope, work[0]); err != nil {
					t.Fatal(err)
				}
			}
			complete, err := query(f.auth)
			if err != nil || complete.CleanupState != "complete" || complete.Revision != 3 || !api.Equal(complete.Values, pending.Values) {
				t.Fatalf("completed cleanup metadata %+v %v", complete, err)
			}
			// Getter/记忆引用收尾不宣称原 Content 的全部物理副本擦除。
			if body, err := f.service.Read(ctx, f.scope, f.auth, values.ContentRef, "task.goal"); err != nil || string(body) != "metadata-only cleanup does not erase unrelated original Content" {
				t.Fatalf("getter altered original published bytes %q %v", body, err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store = open()
			f.service = memory.New(store, f.service.Objects)
			f.dispatcher.Store = store
			f.dispatcher.Registry = runtime.NewRegistry()
			f.service.Register(f.dispatcher.Registry)
			if after, err := query(f.auth); err != nil || !api.Equal(after, complete) {
				t.Fatalf("reopen changed original cleanup fact %+v %v", after, err)
			}
		})
	}
}
