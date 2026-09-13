package httpfetch_test

import (
	"context"
	"io"
	"lerna/adapters/doubaosearch"
	"lerna/adapters/httpfetch"
	"lerna/fetch"
	"lerna/websearch"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"
)

func TestAuthenticatedJSONPostIsBoundedAndNeverRedirected(t *testing.T) {
	for _, mode := range []string{"success", "redirect", "revoked", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			var calls atomic.Int32
			var fixture *authorityFixture
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				body, _ := io.ReadAll(r.Body)
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-key" || string(body) != `{"Query":"public"}` {
					t.Error("incorrect authenticated request")
				}
				if mode == "redirect" {
					w.Header().Set("Location", "/other")
					w.WriteHeader(307)
					return
				}
				if mode == "revoked" {
					if err := fixture.revoke(); err != nil {
						t.Error(err)
					}
				}
				w.Header().Set("Content-Type", "application/json")
				if mode == "oversized" {
					w.Write([]byte(`{"data":"this exceeds the byte budget"}`))
					return
				}
				w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			urls := []string{server.URL, server.URL + "/other"}
			auth, f := newAuthority(t, urls)
			fixture = f
			client, err := httpfetch.New(httpfetch.Config{Authority: auth, URLs: urls, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			result, err := client.PostJSON(context.Background(), fetch.Request{URL: server.URL, MaxBytes: 16, MaxRequests: 2, Timeout: time.Second}, []byte(`{"Query":"public"}`), "test-key")
			if calls.Load() != 1 || result.Requests != 1 {
				t.Fatalf("requests=%d, attempts=%d", calls.Load(), result.Requests)
			}
			if mode == "success" {
				if err != nil || string(result.Body) != `{"ok":true}` {
					t.Fatalf("success: %v", err)
				}
			} else if err == nil || len(result.Body) != 0 || result.FinalURL != "" {
				t.Fatalf("failure disclosed data: %v", err)
			}
		})
	}
}

func TestDoubaoRejectsInvalidDiscoveryWithoutReleasingBody(t *testing.T) {
	for _, body := range []string{
		`{"ResponseMetadata":{"Error":{"Code":"AccessDenied"}},"Result":{"WebResults":[]}}`,
		`{"ResponseMetadata":{},"Result":{}}`,
		`{"ResponseMetadata":{},"Result":{"WebResults":[{"Url":"file:///private","Title":"Bad","Snippet":"bad"}]}}`,
		`{"ResponseMetadata":{},"Result":{"WebResults":[],"WebResults":[]}}`,
	} {
		t.Run(body, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.Header().Set("Content-Type", "application/json")
				w.Write([]byte(body))
			}))
			defer server.Close()
			endpoint := server.URL + "/search_api/web_search"
			transport, err := authorizedAdapter(t, httpfetch.Config{URLs: []string{endpoint}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			provider, err := doubaosearch.New(transport, endpoint, func(context.Context) (string, error) { return "test-key", nil })
			if err != nil {
				t.Fatal(err)
			}
			got, err := provider.Search(context.Background(), websearch.Request{Query: "public", MaxResults: 1, MaxBytes: 4096, MaxRequests: 1, Timeout: time.Second})
			if err == nil || len(got.Candidates) != 0 || len(got.Acquisition.Body) != 0 || got.Acquisition.FinalURL != "" || got.Acquisition.Requests != 1 || calls.Load() != 1 {
				t.Fatalf("invalid discovery released or lost usage: %v", err)
			}
		})
	}
}
