package brain_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

// 只在已授权Gate边界注入冻结字段变化；不改SQL或Decision原记录。
type changedEncodingReceiver struct{ change bool }

func (g *changedEncodingReceiver) CheckTx(_ context.Context, _ runtime.Tx, _ runtime.Auth, _ brain.DecideInput, e *brain.Encoding) error {
	if g.change && e != nil {
		e.Receiver = "different-receiver-with-the-original-body-digest"
	}
	return nil
}

func TestLegalLargeFrozenEncodingReopensAndSendsExactOriginalCallOnce(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
			defer cancel()
			root := t.TempDir()
			path := filepath.Join(root, "original.sqlite")
			open := func() runtime.Store {
				t.Helper()
				if driver == "sqlite" {
					return openBrainStore(t, path)
				}
				dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("requires actual PostgreSQL")
				}
				store, err := postgres.Open(ctx, dsn)
				if err == nil {
					err = store.Migrate(ctx)
				}
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := store.Close(); err != nil {
						t.Error(err)
					}
				})
				return store
			}
			store := open()
			scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
			auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
			content := &fileContents{root: root}
			var posts atomic.Int32
			var request []byte
			var callID string
			var captured sync.Mutex
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxJSONBytes+1))
				header := r.Header.Get("X-Harness-Call-ID")
				captured.Lock()
				request, callID = body, header
				captured.Unlock()
				if err != nil || r.Method != http.MethodPost || header == "" {
					t.Errorf("actual original model request: bytes=%d call=%s err=%v", len(body), header, err)
				}
				_, _ = w.Write(largeProviderReply("The original large request completed without rebuilding its sources."))
			}))
			t.Cleanup(server.Close)
			cfg := brainProviderConfig(store, scope, server.URL)
			provider, err := providers.NewOpenAI(cfg)
			if err != nil {
				t.Fatal(err)
			}
			originalProvider := provider
			t.Cleanup(func() {
				if err := originalProvider.Close(); err != nil {
					t.Error(err)
				}
			})
			goal := []byte(strings.Repeat("f", 200<<10))
			snapshot := providerSnapshot(t, scope, content, provider, goal)
			for i := 0; i < 8; i++ {
				ref, err := content.put(scope, api.NewID("source"), "text/plain", []byte("accurate declared original source"))
				if err != nil {
					t.Fatal(err)
				}
				snapshot.ProcessedSources = append(snapshot.ProcessedSources, ref)
			}
			originalEncoding, err := provider.Encode(ctx, snapshot, goal, provider.Profile())
			if err != nil {
				t.Fatal(err)
			}
			privateContainer, err := json.Marshal(originalEncoding)
			if err != nil || len(privateContainer) <= api.MaxJSONBytes || len(originalEncoding.Body) > api.MaxJSONBytes {
				t.Fatalf("fixture must have a legal public body and a larger private base64 container: body=%d container=%d err=%v", len(originalEncoding.Body), len(privateContainer), err)
			}
			if _, err = provider.Lookup(ctx, api.NewID("call"), originalEncoding); !api.IsCode(err, "effect_unknown") || posts.Load() != 0 {
				t.Fatalf("a legal frozen encoding was rejected before any original model send: posts=%d err=%v", posts.Load(), err)
			}
			changed := originalEncoding
			changed.InputTokens--
			if _, err = provider.Lookup(ctx, api.NewID("call"), changed); !api.IsCode(err, "forbidden") || posts.Load() != 0 {
				t.Fatalf("same body/digest with a changed frozen count reached a model call: posts=%d err=%v", posts.Load(), err)
			}
			g := &changedEncodingReceiver{}
			assemble := func(provider *providers.OpenAI) (*brain.Service, *runtime.Registry) {
				t.Helper()
				service, err := brain.New(brain.Config{Profiles: []brain.Profile{provider.Profile()}, Content: content, Engine: provider, Gate: g})
				if err != nil {
					t.Fatal(err)
				}
				registry := runtime.NewRegistry()
				if err = service.Register(registry); err != nil {
					t.Fatal(err)
				}
				return service, registry
			}
			service, registry := assemble(provider)
			command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
			work, status, err := store.Claim(ctx, scope, api.NewID("worker"), []string{brain.JobAdvance}, 1, time.Minute)
			if err != nil || status != runtime.Committed || len(work) != 1 {
				t.Fatalf("original accepted claim: count=%d status=%s err=%v", len(work), status, err)
			}
			handler, _ := registry.Job(brain.JobAdvance)
			if err = handler(ctx, store, scope, work[0]); err != nil {
				t.Fatal(err)
			}
			encoded, err := service.Get(ctx, store, scope, auth, command.TargetID)
			if err != nil || encoded.Publication != "encoded" || encoded.Decision.SendStarted || posts.Load() != 0 {
				t.Fatalf("original encoding was not durably frozen before send: %+v %v", encoded, err)
			}
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store = open()
			if store.ID() != scope.DatabaseID {
				t.Fatal("reopen replaced original database")
			}
			// 恢复只沿完整原编码；原Goal不可读也不能重新编译新的模型请求。
			if err = os.Remove(filepath.Join(root, snapshot.GoalRef.ContentID+".blob")); err != nil {
				t.Fatal(err)
			}
			cfg.Store = store
			provider, err = providers.NewOpenAI(cfg)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := provider.Close(); err != nil {
					t.Error(err)
				}
			})
			service, registry = assemble(provider)
			// Digest相同但receiver变化仍拒绝，不能只核Body/Digest放宽冻结一致性。
			g.change = true
			work, status, err = store.Claim(ctx, scope, api.NewID("worker"), []string{brain.JobAdvance}, 1, 2*time.Second)
			if err != nil || status != runtime.Committed || len(work) != 1 {
				t.Fatalf("original encoded claim: count=%d status=%s err=%v", len(work), status, err)
			}
			handler, _ = registry.Job(brain.JobAdvance)
			if err = handler(ctx, store, scope, work[0]); !api.IsCode(err, "revision_conflict") {
				t.Fatalf("changed frozen receiver was not rejected: %v", err)
			}
			blocked, err := service.Get(ctx, store, scope, auth, command.TargetID)
			if err != nil || blocked.CallID != encoded.CallID || blocked.Decision.SendStarted || posts.Load() != 0 || blocked.Publication != "encoded" {
				t.Fatalf("changed receiver changed the original responsibility: %+v posts=%d err=%v", blocked, posts.Load(), err)
			}
			g.change = false
			select {
			case <-time.After(2100 * time.Millisecond):
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			completed := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "completed")
			captured.Lock()
			actualRequest, actualCallID := append([]byte(nil), request...), callID
			captured.Unlock()
			if completed.CallID != encoded.CallID || actualCallID != encoded.CallID || posts.Load() != 1 || completed.Decision.PhysicalRequestCount != 1 || !completed.Decision.UsageFinal || !bytes.Equal(actualRequest, originalEncoding.Body) || api.Hash(actualRequest) != originalEncoding.Digest {
				t.Fatalf("legal original large encoding did not survive recovery/send: bytes=%d posts=%d %+v", len(actualRequest), posts.Load(), completed)
			}
			dispatch := runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}
			if receipt, err := dispatch.Command(ctx, auth, api.Raw(command)); err != nil || receipt.Stage != "applied" || posts.Load() != 1 {
				t.Fatalf("original command replay repeated the model request: %+v posts=%d err=%v", receipt, posts.Load(), err)
			}
			t.Logf("public_body_bytes=%d private_encoding_bytes=%d declared_sources=%d physical_requests=%d", len(originalEncoding.Body), len(privateContainer), len(snapshot.ProcessedSources), posts.Load())
		})
	}
}
