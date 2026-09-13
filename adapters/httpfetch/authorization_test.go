package httpfetch_test

import (
	"context"
	"lerna/adapters/fetchauth"
	"lerna/adapters/httpfetch"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

type authorityFixture struct {
	service *authorization.Service
	token   string
}

func newAuthority(t *testing.T, urls []string) (*fetchauth.Authority, *authorityFixture) {
	t.Helper()
	db, err := sqliteauth.Open(filepath.Join(t.TempDir(), "authority.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	service, err := authorization.New(db, authorization.SystemClock{}, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Bootstrap(context.Background(), "local", "alice")
	if err != nil {
		t.Fatal(err)
	}
	f := &authorityFixture{service, token}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"fetch.read", "fetch.process", "fetch.disclose"}, Purposes: []string{"answer"}, Locations: []string{"device"}, ExpiresUnix: time.Now().Add(30 * time.Minute).Unix()}
	for _, command := range []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "fetch", Scope: scope}}}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "fetch", Subject: "alice", Scope: scope, Mode: "continuous"}}},
	} {
		if err = f.apply(command); err != nil {
			t.Fatal(err)
		}
	}
	resources := map[string]string{}
	for _, url := range urls {
		resources[url] = "root"
	}
	authority, err := fetchauth.New(service, fetchauth.Scope{Token: token, Namespace: "local", Subject: "alice", Purpose: "answer", Location: "device", Recipient: "device", Resources: resources})
	if err != nil {
		t.Fatal(err)
	}
	return authority, f
}
func (f *authorityFixture) apply(command *wire.AuthorizationCommand) error {
	ctx := context.Background()
	policy, err := f.service.GetPolicy(ctx, f.token)
	if err != nil {
		return err
	}
	operation, err := f.service.NewOperation(ctx, f.token)
	if err != nil {
		return err
	}
	command.ExpectedRevision = policy.Revision
	_, err = f.service.Execute(ctx, f.token, authorization.Mutation{Namespace: "local", OperationID: operation, Command: command})
	return err
}
func (f *authorityFixture) revoke() error {
	return f.apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}})
}

func TestHTTPFetchRechecksAuthorityBeforeRedirectAndRelease(t *testing.T) {
	for _, mode := range []string{"redirect", "release"} {
		t.Run(mode, func(t *testing.T) {
			var f *authorityFixture
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if err := f.revoke(); err != nil {
					t.Error(err)
				}
				if mode == "redirect" && r.URL.Path == "/start" {
					w.Header().Set("Location", "/final")
					w.WriteHeader(http.StatusFound)
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("must not release"))
			}))
			defer server.Close()
			urls := []string{server.URL + "/start", server.URL + "/final"}
			authority, fixture := newAuthority(t, urls)
			f = fixture
			adapter, err := httpfetch.New(httpfetch.Config{Authority: authority, URLs: urls, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			got, err := adapter.Fetch(context.Background(), fetch.Request{URL: urls[0], MaxBytes: 64, MaxRequests: 2, Timeout: time.Second})
			if err != fetch.Denied || got.Requests != 1 || hits.Load() != 1 || len(got.Body) != 0 || got.FinalURL != "" {
				t.Fatalf("revoked source leaked: %+v err=%v hits=%d", got, err, hits.Load())
			}
		})
	}
}

func authorizedAdapter(t *testing.T, config httpfetch.Config) (*httpfetch.Adapter, error) {
	t.Helper()
	config.Authority, _ = newAuthority(t, config.URLs)
	return httpfetch.New(config)
}

func TestHTTPFetchRequiresIndependentReadProcessAndDisclosure(t *testing.T) {
	for _, missing := range []string{"fetch.read", "fetch.process", "fetch.disclose"} {
		t.Run(missing, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("restricted evidence"))
			}))
			defer server.Close()
			authority, f := newAuthority(t, []string{server.URL})
			actions := []string{}
			for _, name := range []string{"fetch.read", "fetch.process", "fetch.disclose"} {
				if name != missing {
					actions = append(actions, name)
				}
			}
			scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: actions, Purposes: []string{"answer"}, Locations: []string{"device"}, ExpiresUnix: time.Now().Add(30 * time.Minute).Unix()}
			if err := f.apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "remaining", Scope: scope}}}}}); err != nil {
				t.Fatal(err)
			}
			adapter, err := httpfetch.New(httpfetch.Config{Authority: authority, URLs: []string{server.URL}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			got, err := adapter.Fetch(context.Background(), fetch.Request{URL: server.URL, MaxBytes: 64, MaxRequests: 1, Timeout: time.Second})
			requests := 0
			if missing == "fetch.disclose" {
				requests = 1
			}
			if err != fetch.Denied || got.Requests != requests || int(hits.Load()) != requests || len(got.Body) != 0 || got.FinalURL != "" {
				t.Fatalf("independent phase %s: %+v err=%v hits=%d", missing, got, err, hits.Load())
			}
		})
	}
}
