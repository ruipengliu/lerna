package providers_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
)

func TestPostgresOriginalModelLedgerReleasesConnectionBeforeHTTP(t *testing.T) {
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("HARNESS_TEST_POSTGRES_DSN required for real PostgreSQL evidence")
	}
	ctx := context.Background()
	store, err := postgres.Open(ctx, dsn, postgres.WithMaxConnections(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	var active atomic.Pointer[providers.OpenAI]
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		bounded, cancel := context.WithTimeout(r.Context(), time.Second)
		defer cancel()
		view, err := active.Load().Call(bounded, r.Header.Get("X-Harness-Call-ID"))
		if err != nil || view.Status != "send_started" {
			t.Errorf("the only SQL connection remained occupied during HTTP or original marker absent: %+v %v", view, err)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(providerReply(t))
	}))
	defer server.Close()
	cfg := configuration(t, server.URL)
	cfg.Store = store
	cfg.Scope.DatabaseID = store.ID()
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	active.Store(engine)
	enc := encoded(t, engine, cfg)
	call := api.NewID("call")
	out, err := engine.Request(ctx, call, enc)
	if err != nil || out.Draft.Kind != "complete" {
		t.Fatalf("real PG/HTTP call: %+v %v", out, err)
	}
	restored, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := restored.Lookup(ctx, call, enc)
	if err != nil || !api.Equal(out, recovered) || sends.Load() != 1 {
		t.Fatalf("same original PG model output: %v sends=%d", err, sends.Load())
	}
}
