package providers_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

func configuration(t *testing.T, endpoint string, options ...sqlite.Option) providers.OpenAIConfig {
	t.Helper()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "provider.db"), options...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return providers.OpenAIConfig{
		Store: store, Scope: runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()},
		Endpoint: endpoint, Model: "contract-model", Receiver: "contract-provider", Location: "cloud",
		APIKey: "contract-test-credential", CredentialID: "contract-credential-v1", AllowHTTPForLoopback: true,
		Tokenizer: providers.UTF8UpperBound{}, InputUSDPerMillion: "2", OutputUSDPerMillion: "4", CachedInputUSDPerMillion: "1", BillingFinal: true,
		Profile:          brain.Profile{Ref: api.ComponentRef{ComponentID: api.NewID("modelprofile"), Version: "1"}, ContextLimit: 100000, MaxInputTokens: 90000, MaxOutputTokens: 1000, SafetyMargin: 64, MaxInputBytes: api.MaxJSONBytes, RequestTimeout: 5 * time.Second},
		MaxResponseBytes: api.MaxJSONBytes, MaxConcurrent: 2,
	}
}

func encoded(t *testing.T, engine *providers.OpenAI, cfg providers.OpenAIConfig) brain.Encoding {
	t.Helper()
	goal := []byte("请输出报告草稿")
	ref := api.ContentRef{TenantID: cfg.Scope.TenantID, OwnerID: cfg.Scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(goal), MediaType: "text/plain", ByteLength: uint64(len(goal))}
	s := api.Snapshot{SnapshotID: api.NewID("snapshot"), Revision: 1, TaskRef: cfg.Scope.Ref(api.NewID("task"), 1), GoalRevision: 1, ControlRevision: 1, GoalRef: ref, Requirements: []api.Requirement{}, FactRefs: []api.ObjectRef{}, UnresolvedCollections: []api.CollectionSummary{}, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, MaterialRefs: []api.ContentRef{}, ProcessedSources: []api.ContentRef{}, ModelProfileRef: engine.Profile().Ref, TokenizerRef: engine.TokenizerRef()}
	enc, err := engine.Encode(context.Background(), s, goal, engine.Profile())
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func providerReply(t *testing.T) []byte {
	t.Helper()
	model := map[string]any{"draft": map[string]any{"kind": "complete", "reason_local_id": "reason", "artifact_local_ids": []string{"report"}}, "contents": []any{
		map[string]any{"local_id": "reason", "media_type": "text/plain", "body": "只建议产物，完成由条件检查裁决。", "disclosed_sources": []any{}},
		map[string]any{"local_id": "report", "media_type": "text/plain", "body": "报告草稿", "disclosed_sources": []any{}},
	}}
	return api.Raw(map[string]any{"id": "provider-original-123", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(model))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}})
}

func TestOriginalCallBytesUsageAndDurableRecovery(t *testing.T) {
	var sends atomic.Int64
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		if r.Method != "POST" || r.Header.Get("X-Harness-Call-ID") == "" {
			t.Error("missing original call identity")
		}
		if r.Header.Get("Authorization") != "Bearer contract-test-credential" {
			t.Error("credential not scoped to request header")
		}
		received, _ := io.ReadAll(r.Body)
		requests <- received
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(providerReply(t))
	}))
	defer server.Close()
	cfg := configuration(t, server.URL+"/v1/chat/completions")
	engine, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if sends.Load() != 0 {
		t.Fatal("construction made a physical request")
	}
	enc := encoded(t, engine, cfg)
	if sends.Load() != 0 || enc.CountMode != "upper_bound" {
		t.Fatal("encoding must remain local and declare its token bound")
	}
	callID := api.NewID("call")
	out, err := engine.Request(context.Background(), callID, enc)
	if err != nil {
		t.Fatal(err)
	}
	received := <-requests
	if string(received) != string(enc.Body) {
		t.Fatal("physical bytes differ from frozen encoding")
	}
	if out.Draft.Kind != "complete" || len(out.Contents) != 2 || len(out.Usage) != 1 || out.Usage[0] != (api.Amount{Unit: "USD", Value: "0.00024"}) || !out.UsageFinal {
		t.Fatalf("generated output/actual price: %+v", out)
	}
	view, err := engine.Call(context.Background(), callID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Status != "received" || view.ProviderResponseID != "provider-original-123" || view.Tokens.Prompt != 100 || view.Tokens.CachedInput != 40 || view.Tokens.Completion != 20 || view.Tokens.Total != 120 {
		t.Fatalf("actual provider facts: %+v", view)
	}
	restored, err := providers.NewOpenAI(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range []*providers.OpenAI{engine, restored} {
		for _, lookup := range []bool{false, true} {
			var got brain.Generated
			if lookup {
				got, err = e.Lookup(context.Background(), callID, enc)
			} else {
				got, err = e.Request(context.Background(), callID, enc)
			}
			if err != nil || !api.Equal(got, out) {
				t.Fatalf("recover original output: %+v %v", got, err)
			}
		}
	}
	if sends.Load() != 1 {
		t.Fatalf("original call retransmitted %d physical times", sends.Load())
	}
	var wire map[string]json.RawMessage
	if err = json.Unmarshal(received, &wire); err != nil || string(wire["model"]) != `"contract-model"` {
		t.Fatal("real OpenAI compatible request")
	}
}
