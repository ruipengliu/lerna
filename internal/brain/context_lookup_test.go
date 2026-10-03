package brain_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

func TestPublicNeedContextProposalRetainsExactPublishedQueryAfterReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "brain.sqlite")
	store := openBrainStore(t, path)
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	contents := &fileContents{root: root, paused: true}
	target := scope.Ref(api.NewID("memoryscope"), 1)
	queryBody := `{"query":"current existing licensed preference only"}`
	model := api.Raw(map[string]any{"draft": map[string]any{"kind": "need_context", "reason_local_id": "reason", "lookups": []any{map[string]any{"kind": "memory_query", "target_ref": target, "query_local_id": "query"}}}, "contents": []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "request only the declared existing material", DisclosedSources: []api.ContentRef{}}, {LocalID: "query", MediaType: "application/json", Body: queryBody, DisclosedSources: []api.ContentRef{}}}})
	var posts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts.Add(1)
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(api.Raw(map[string]any{"id": "original-need-context", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(model)}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120}}))
	}))
	defer server.Close()
	cfg := brainProviderConfig(store, scope, server.URL)
	svc, registry, provider := newBrainProvider(t, cfg, contents)
	snapshot := providerSnapshot(t, scope, contents, provider, []byte("original task goal"))
	command := acceptProviderDecision(t, store, scope, auth, registry, contents, snapshot)
	before := drainBrainUntil(t, store, scope, auth, svc, registry, command.TargetID, "publishing")
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store = openBrainStore(t, path)
	cfg.Store = store
	contents.paused = false
	svc, registry, _ = newBrainProvider(t, cfg, contents)
	view := drainBrainUntil(t, store, scope, auth, svc, registry, command.TargetID, "completed")
	if view.CallID != before.CallID || posts.Load() != 1 || view.Decision.ProposalRef == nil {
		t.Fatal("reopen replaced original model responsibility")
	}
	raw, err := contents.Read(ctx, scope, auth, *view.Decision.ProposalRef, "brain.output")
	var proposal brain.Proposal
	if err != nil || api.Decode(raw, &proposal) != nil || len(proposal.Lookups) != 1 {
		t.Fatalf("missing legal need_context proposal %+v %v", proposal, err)
	}
	lookup := proposal.Lookups[0]
	actual, err := contents.Read(ctx, scope, auth, lookup.QueryRef, "brain.output")
	if err != nil || string(actual) != queryBody || lookup.QueryRef.Hash != api.Hash(actual) || !api.Equal(lookup.TargetRef, target) || lookup.Kind != "memory_query" {
		t.Fatalf("original query was replaced or lost: %+v %v", lookup, err)
	}
	if duplicate, err := (&runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}).Command(ctx, auth, api.Raw(command)); err != nil || duplicate.Stage != "applied" || posts.Load() != 1 {
		t.Fatalf("original command replay sent another model request %+v %v", duplicate, err)
	}
}

func TestProviderContextLookupDraftKeepsClosedKindAndKnownLocalQuery(t *testing.T) {
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner")}
	model := func(kind, local string, lookups int) []byte {
		queries := []any{}
		for i := 0; i < lookups; i++ {
			queries = append(queries, map[string]any{"kind": kind, "target_ref": scope.Ref(api.NewID("material"), 1), "query_local_id": local})
		}
		return api.Raw(map[string]any{"draft": map[string]any{"kind": "need_context", "reason_local_id": "reason", "lookups": queries}, "contents": []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "read the exact existing licensed material", DisclosedSources: []api.ContentRef{}}, {LocalID: "query", MediaType: "application/json", Body: `{}`, DisclosedSources: []api.ContentRef{}}}})
	}
	for _, kind := range []string{"existing_content", "memory_query", "capability_describe", "original_fact"} {
		generated, err := providers.ParseGenerated(model(kind, "query", 1))
		if err != nil || generated.Draft.Kind != "need_context" {
			t.Fatalf("legitimate existing-material lookup %s refused %+v %v", kind, generated.Draft, err)
		}
	}
	for _, tc := range []struct {
		kind, query string
		count       int
	}{{"https://arbitrary.invalid/", "query", 1}, {"search", "query", 1}, {"observe_phone", "query", 1}, {"memory_query", "unpublished", 1}, {"memory_query", "query", 0}, {"memory_query", "query", 4}} {
		if _, err := providers.ParseGenerated(model(tc.kind, tc.query, tc.count)); !api.IsCode(err, "invalid_request") {
			t.Fatalf("hidden action, unknown local query or unbounded lookup accepted: %+v %v", tc, err)
		}
	}
}
