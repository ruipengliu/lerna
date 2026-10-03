package brain_test

import (
	"context"
	"encoding/json"
	"fmt"
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

func TestOriginalArgumentDisclosureSurvivesPublicBrainProposalAndReopen(t *testing.T) {
	for _, tc := range []struct {
		name     string
		declared bool
		locals   []string
	}{{name: "explicit_original_source", declared: true}, {name: "without_declaration"}, {name: "explicit_original_local_argument", locals: []string{"args"}}, {name: "source_and_local_argument", declared: true, locals: []string{"args"}}} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			root := t.TempDir()
			path := filepath.Join(root, "brain.sqlite")
			store := openBrainStore(t, path)
			scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
			auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
			content := &fileContents{root: root, paused: true}
			query, err := content.put(scope, api.NewID("query"), "application/json", []byte(`{"query":"exact previously authorized query"}`))
			if err != nil {
				t.Fatal(err)
			}
			disclosed := []api.ContentRef{}
			if tc.declared {
				disclosed = append(disclosed, query)
			}
			capability := api.ComponentRef{ComponentID: api.NewID("capability"), Version: "1", Digest: api.Hash([]byte("exact search contract"))}
			binding := scope.Ref(api.NewID("binding"), 1)
			actionDraft := map[string]any{"local_key": "original_search", "capability_ref": capability, "binding_ref": binding, "arguments_local_id": "args"}
			if len(tc.locals) > 0 {
				actionDraft["disclosed_local_ids"] = tc.locals
			}
			model := map[string]any{"draft": map[string]any{"kind": "act", "reason_local_id": "reason", "actions": []any{actionDraft}}, "contents": []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "read only the declared information source", DisclosedSources: []api.ContentRef{}}, {LocalID: "args", MediaType: "application/json", Body: string(api.Raw(map[string]any{"query_ref": query})), DisclosedSources: disclosed}}}
			if _, err := providers.ParseGenerated(api.Raw(model)); err != nil {
				t.Fatalf("fixture output must be legitimate: %v", err)
			}
			var posts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				_, _ = io.Copy(io.Discard, r.Body)
				_, _ = w.Write(api.Raw(map[string]any{"id": "original-action-disclosure", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(model))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}}))
			}))
			defer server.Close()
			cfg := brainProviderConfig(store, scope, server.URL)
			service, registry, provider := newBrainProvider(t, cfg, content)
			snapshot := providerSnapshot(t, scope, content, provider, []byte("consider only the original declared action"))
			snapshot.CapabilityRefs = []api.ComponentRef{capability}
			snapshot.BindingRefs = []api.ObjectRef{binding}
			snapshot.ProcessedSources = append(snapshot.ProcessedSources, query)
			command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
			before := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "publishing")
			if err = store.Close(); err != nil {
				t.Fatal(err)
			}
			store = openBrainStore(t, path)
			cfg.Store = store
			content.paused = false
			service, registry, _ = newBrainProvider(t, cfg, content)
			view := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "completed")
			if view.CallID != before.CallID || posts.Load() != 1 || view.Decision.ProposalRef == nil {
				t.Fatalf("reopen changed original physical call %+v", view)
			}
			raw, err := content.Read(ctx, scope, auth, *view.Decision.ProposalRef, "brain.output")
			var proposal brain.Proposal
			if err != nil || api.Decode(raw, &proposal) != nil || len(proposal.Actions) != 1 {
				t.Fatalf("public proposal %+v %v", proposal, err)
			}
			action := proposal.Actions[0]
			expected := append([]api.ContentRef{}, disclosed...)
			if len(tc.locals) > 0 {
				expected = append(expected, action.ArgumentsRef)
			}
			if !api.Equal(action.DisclosedSourceRefs, expected) {
				t.Fatalf("original parameter publication disclosure was lost or enlarged: got=%+v want=%+v", action.DisclosedSourceRefs, expected)
			}
			if !api.Equal(action.CapabilityRef, capability) || !api.Equal(action.BindingRef, binding) || !containsContent(action.ProcessedSourceRefs, query) || !containsContent(action.ProcessedSourceRefs, action.ArgumentsRef) {
				t.Fatal("action lost original frozen references")
			}
			if duplicate, err := (&runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}).Command(ctx, auth, api.Raw(command)); err != nil || duplicate.Stage != "applied" || posts.Load() != 1 {
				t.Fatalf("original receipt replay sent again %+v %v", duplicate, err)
			}
		})
	}
}

func containsContent(refs []api.ContentRef, ref api.ContentRef) bool {
	for _, current := range refs {
		if api.Equal(current, ref) {
			return true
		}
	}
	return false
}

func TestLocalDisclosureParserRejectsUnknownDuplicateAndUnboundedReferences(t *testing.T) {
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner")}
	capability := api.ComponentRef{ComponentID: api.NewID("capability"), Version: "1", Digest: api.Hash([]byte("original capability"))}
	binding := scope.Ref(api.NewID("binding"), 1)
	for _, tc := range []struct {
		name string
		ids  []string
	}{{"unknown", []string{"unpublished"}}, {"duplicate", []string{"args", "args"}}, {"over_twenty", make([]string, 21)}} {
		t.Run(tc.name, func(t *testing.T) {
			model := map[string]any{"draft": map[string]any{"kind": "act", "reason_local_id": "reason", "actions": []any{map[string]any{"local_key": "one", "capability_ref": capability, "binding_ref": binding, "arguments_local_id": "args", "disclosed_local_ids": tc.ids}}}, "contents": []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "original reason", DisclosedSources: []api.ContentRef{}}, {LocalID: "args", MediaType: "application/json", Body: `{"url":"https://declared.invalid/"}`, DisclosedSources: []api.ContentRef{}}}}
			if _, err := providers.ParseGenerated(api.Raw(model)); !api.IsCode(err, "invalid_request") {
				t.Fatalf("invalid explicit local disclosure was accepted: %v", err)
			}
		})
	}
	contents := []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "original reason", DisclosedSources: []api.ContentRef{}}, {LocalID: "args", MediaType: "application/json", Body: `{}`, DisclosedSources: []api.ContentRef{}}}
	ids := []string{"reason", "args"}
	for i := 0; i < 18; i++ {
		id := fmt.Sprintf("original_%d", i)
		ids = append(ids, id)
		contents = append(contents, brain.GeneratedContent{LocalID: id, MediaType: "text/plain", Body: "explicit original material", DisclosedSources: []api.ContentRef{}})
	}
	model := map[string]any{"draft": map[string]any{"kind": "act", "reason_local_id": "reason", "actions": []any{map[string]any{"local_key": "one", "capability_ref": capability, "binding_ref": binding, "arguments_local_id": "args", "disclosed_local_ids": ids}}}, "contents": contents}
	if out, err := providers.ParseGenerated(api.Raw(model)); err != nil || len(out.Draft.Actions) != 1 || !api.Equal(out.Draft.Actions[0].DisclosedLocalIDs, ids) {
		t.Fatalf("twenty exact distinct original disclosures must remain legal %+v %v", out.Draft, err)
	}
}

func TestPublicCompleteProposalIncludesEmptyCheckSuggestionsWithoutTaskSuccess(t *testing.T) {
	root := t.TempDir()
	store := openBrainStore(t, filepath.Join(root, "brain.sqlite"))
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	content := &fileContents{root: root}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write(largeProviderReply("a candidate artifact, never an authoritative Task success"))
	}))
	defer server.Close()
	service, registry, provider := newBrainProvider(t, brainProviderConfig(store, scope, server.URL), content)
	snapshot := providerSnapshot(t, scope, content, provider, []byte("original goal has no adopted requirements"))
	snapshot.Requirements = []api.Requirement{}
	snapshot.RequirementsDigest = api.Hash(api.Raw([]api.Requirement{}))
	snapshot.RequirementsState = "collecting"
	snapshot.Purpose = "interpret_requirements"
	command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
	view := drainBrainUntil(t, store, scope, auth, service, registry, command.TargetID, "completed")
	if view.Decision.ProposalRef == nil {
		t.Fatal("legitimate complete proposal never published")
	}
	raw, err := content.Read(context.Background(), scope, auth, *view.Decision.ProposalRef, "brain.output")
	var fields map[string]json.RawMessage
	if err != nil || api.Decode(raw, &fields) != nil || string(fields["check_suggestions"]) != "[]" {
		t.Fatalf("legal empty check suggestion list was omitted: %s %v", raw, err)
	}
	var proposal brain.Proposal
	if err = api.Decode(raw, &proposal); err != nil || proposal.Kind != "complete" || len(proposal.CheckSuggestions) != 0 || len(proposal.ArtifactRefs) != 1 {
		t.Fatalf("proposal changed into a Task result %+v %v", proposal, err)
	}
}
