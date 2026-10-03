package runtime_test

import (
	"context"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type entryKey struct{}
type entryState struct {
	id    uint64
	kind  string
	scope runtime.Scope
	used  bool
}
type flowInput struct {
	Value string `json:"value"`
}
type flowOutput struct {
	Value string `json:"value"`
}

func TestContextFactoryIsolatesOriginalCommandQueryAndJobEntries(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "flows.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	a := runtime.Auth{TenantID: api.NewID("tenant"), SubjectID: api.NewID("subject"), CredentialGeneration: 1}
	scope := runtime.Scope{TenantID: a.TenantID, OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	registry := runtime.NewRegistry()
	var entries atomic.Uint64
	registry.SetContextFactory(func(ctx context.Context, flow runtime.Flow) context.Context {
		if flow.Scope != scope {
			t.Error("entry lost actual database scope")
		}
		if flow.Kind == "command" && (flow.Command == nil || flow.Auth.SubjectID != a.SubjectID) {
			t.Error("original command identity missing")
		}
		if flow.Kind == "query" && (flow.Query == nil || flow.Auth.SubjectID != a.SubjectID) {
			t.Error("original query identity missing")
		}
		if flow.Kind == "job" && (flow.Work == nil || flow.Work.Claim.HolderID == "") {
			t.Error("original Job Claim missing")
		}
		return context.WithValue(ctx, entryKey{}, &entryState{id: entries.Add(1), kind: flow.Kind, scope: flow.Scope})
	})
	d := runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}
	registry.MustRegister(runtime.Method{Contract: api.Contract[flowInput, flowOutput]("fixture.apply", "orchestrator", "command", false, false), Participants: []string{"fixture"}, Apply: func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command) (runtime.Outcome, error) {
		entry, ok := ctx.Value(entryKey{}).(*entryState)
		if !ok || entry.kind != "command" || entry.used {
			t.Error("command inherited another entry")
		}
		entry.used = true
		return runtime.Applied(flowOutput{"applied"}), nil
	}})
	registry.MustRegister(runtime.Method{Contract: api.Contract[flowInput, flowOutput]("fixture.query", "orchestrator", "query", false, false), Query: func(ctx context.Context, _ runtime.Store, _ runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		entry, ok := ctx.Value(entryKey{}).(*entryState)
		if !ok || entry.kind != "query" || entry.used {
			t.Error("query inherited another entry")
		}
		entry.used = true
		c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, Method: "fixture.apply", CommandID: api.NewID("command"), TargetID: api.NewID("target"), ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(flowInput{"original"})}
		if receipt, err := d.Command(ctx, auth, api.Raw(c)); err != nil || receipt.Stage != "applied" {
			t.Fatalf("nested actual command %+v %v", receipt, err)
		}
		if ctx.Value(entryKey{}) != entry || !entry.used {
			t.Error("nested command replaced query carrier")
		}
		return flowOutput{"queried"}, nil
	}})
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, QueryID: api.NewID("query"), Method: "fixture.query", TargetID: api.NewID("target"), Payload: api.Raw(flowInput{"original"})}
	if _, err = d.Query(ctx, a, api.Raw(q)); err != nil {
		t.Fatal(err)
	}
	if entries.Load() != 2 {
		t.Fatalf("factory entered %d times for query+nested command", entries.Load())
	}
	var seen []*entryState
	registry.MustRegisterJob("fixture.entry", func(ctx context.Context, store runtime.Store, scope runtime.Scope, w runtime.Work) error {
		entry, ok := ctx.Value(entryKey{}).(*entryState)
		if !ok || entry.kind != "job" || entry.used {
			t.Error("Job inherited another entry")
		}
		entry.used = true
		seen = append(seen, entry)
		return runtime.Finish(ctx, store, scope, []string{"fixture"}, w, runtime.Done(), nil)
	})
	for i := 0; i < 2; i++ {
		_, err = store.Within(ctx, scope, []string{"fixture"}, func(tx runtime.Tx) error {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			_, err = tx.Raise(ctx, "fixture.entry", api.NewID("responsibility"), scope.Ref(api.NewID("source"), 1), now)
			return err
		})
		if err != nil {
			t.Fatal(err)
		}
		if err = runtime.Drain(ctx, store, scope, registry, 2); err != nil {
			t.Fatal(err)
		}
	}
	if len(seen) != 2 || seen[0] == seen[1] || entries.Load() != 4 {
		t.Fatal("original Job entries share permission carrier")
	}
	registry.SetContextFactory(nil)
	old := &entryState{kind: "default"}
	registry.MustRegisterJob("fixture.nil_factory", func(ctx context.Context, _ runtime.Store, _ runtime.Scope, _ runtime.Work) error {
		if ctx.Value(entryKey{}) != old {
			t.Error("nil factory changed caller ctx")
		}
		return nil
	})
	h, _ := registry.Job("fixture.nil_factory")
	if err = h(context.WithValue(ctx, entryKey{}, old), store, scope, runtime.Work{}); err != nil {
		t.Fatal(err)
	}
}
