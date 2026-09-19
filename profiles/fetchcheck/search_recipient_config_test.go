package fetchcheck

import (
	"context"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/research/jsonsearch"
	"lerna/authorization"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestSearchAssemblyRejectsClosedHost(t *testing.T) {
	ctx := context.Background()
	h, err := fresh(ctx, []string{"http://127.0.0.1/search?q=public", "http://127.0.0.1/unused"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	if _, err := bindSearchActionHost(h, "http://127.0.0.1/search", 1, nil); err != nil {
		t.Fatal(err)
	}
	h.close()
	if host, err := bindSearchActionHost(h, "http://127.0.0.1/search", 1, nil); err != fetch.Unavailable || host != nil {
		t.Fatalf("closed host accepted new execution: host=%v err=%v", host, err)
	}
}

func TestSearchProviderConfigChecksRecipientAuthorities(t *testing.T) {
	for _, c := range []struct {
		name              string
		authority, source bool
	}{
		{"neither", false, false}, {"authority_only", true, false}, {"source_only", false, true}, {"both", true, true},
	} {
		t.Run(c.name, func(t *testing.T) { checkSearchRecipient(t, c.authority, c.source) })
	}
}
func checkSearchRecipient(t *testing.T, allowAuthority, allowSource bool) {
	ctx := context.Background()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/search?q=public", server.URL + "/unused"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()

	configureSearchRecipient(t, ctx, h, allowAuthority, allowSource)
	provider, err := jsonsearch.New(h.http, server.URL+"/search")
	if err != nil {
		t.Fatal(err)
	}
	host, err := bindSearchProviderConfig(h, provider, 1, nil, searchProviderConfig{MaxBytes: 1024, Recipient: "remote-search"})
	if err != nil {
		t.Fatal(err)
	}
	call, grant, err := host.h.request(ctx, []byte(`{"query":"public","max_results":1,"max_bytes":1024,"max_requests":1,"timeout_ms":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if _, err = host.h.client.Invoke(ctx, call, grant); err != nil {
			t.Fatal(err)
		}
		if _, err = host.h.exec.Run(ctx, call.OperationID); err != nil {
			t.Fatal(err)
		}
		if err = host.h.exec.Drain(ctx, 16); err != nil {
			t.Fatal(err)
		}
	}
	outcome, known, err := h.attempts.Outcome(ctx, "local", call.OperationID)
	if allowAuthority && allowSource {
		if err != nil || !known || outcome.Status != "acquired" || outcome.Requests != 1 || outcome.Reference == "" || hits.Load() != 1 {
			t.Fatalf("authorized search failed: %+v known=%v hits=%d err=%v", outcome, known, hits.Load(), err)
		}
	} else if err != nil || !known || outcome.Status != "denied" || outcome.Requests != 0 || outcome.Reference != "" || hits.Load() != 0 {
		t.Fatalf("unapproved query left local boundary: %+v known=%v hits=%d err=%v", outcome, known, hits.Load(), err)
	}
	budget, err := h.attempts.Budget(ctx, call.Qualification.Ref)
	if err != nil || budget.Charged != 1 {
		t.Fatalf("denied operation lost original reservation: %+v %v", budget, err)
	}
	local, err := bindSearchProvider(h, provider, 1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = local.h.client.Invoke(ctx, call, grant); err == nil {
		t.Fatal("original operation accepted under changed recipient configuration")
	}
}

func configureSearchRecipient(t *testing.T, ctx context.Context, h *harness, allowAuthority, allowSource bool) {
	t.Helper()
	if allowAuthority {
		state, err := h.db.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		rule := proto.Clone(state.State.Rules[0]).(*wire.PolicyRule)
		rule.Scope.Locations = append(rule.Scope.Locations, "remote-search")
		for _, command := range []*wire.AuthorizationCommand{
			{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{rule}}}},
			{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "external-search", Subject: "operator", Scope: rule.Scope, Mode: "continuous"}}},
		} {
			state, err := h.db.Load(ctx)
			if err != nil {
				t.Fatal(err)
			}
			command.ExpectedRevision = state.State.Revision
			op, err := h.operation(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); err != nil {
				t.Fatal(err)
			}
		}
	}
	if allowSource {
		policy, err := h.policyStore.Load(ctx)
		if err != nil {
			t.Fatal(err)
		}
		for i := range policy.Rules {
			policy.Rules[i].Locations = append(policy.Rules[i].Locations, "remote-search")
		}
		if err = h.policy.Replace(policy.Rules); err != nil {
			t.Fatal(err)
		}
	}
}
