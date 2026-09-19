package httpfetch_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"sync/atomic"
	"testing"
	"time"

	"lerna/adapters/research/httpfetch"
	"lerna/fetch"
)

func TestHTTPFetchReturnsActualResponseEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/evidence" {
			t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	target := server.URL + "/evidence"
	adapter, err := authorizedAdapter(t, httpfetch.Config{URLs: []string{target}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	before := time.Now()
	got, err := adapter.Fetch(context.Background(), fetch.Request{URL: target, MaxBytes: 64, MaxRequests: 1, Timeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Body) != "hello" || got.RequestedURL != target || got.FinalURL != target || got.MediaType != "text/plain" || got.HTTPStatus != 200 || got.Requests != 1 || got.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		t.Fatalf("wrong response evidence: %+v", got)
	}
	if got.FetchedAt.Before(before) || got.FetchedAt.After(time.Now()) {
		t.Fatalf("not actual fetch time: %v", got.FetchedAt)
	}
}

func TestHTTPFetchRejectsUnconfiguredURLAndDialAddress(t *testing.T) {
	for _, mode := range []string{"url", "address"} {
		t.Run(mode, func(t *testing.T) {
			var received atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received.Add(1); w.Write([]byte("private response")) }))
			defer server.Close()
			target := server.URL + "/evidence"
			config := httpfetch.Config{URLs: []string{target}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true}
			if mode == "url" {
				config.URLs = []string{server.URL + "/other"}
			} else {
				config.Networks = []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}
			}
			adapter, err := authorizedAdapter(t, config)
			if err != nil {
				t.Fatal(err)
			}
			got, err := adapter.Fetch(context.Background(), fetch.Request{URL: target, MaxBytes: 64, MaxRequests: 1, Timeout: time.Second})
			if err != fetch.Denied || len(got.Body) != 0 || got.FinalURL != "" || received.Load() != 0 {
				t.Fatalf("denied target contacted or released: result=%+v err=%v received=%d", got, err, received.Load())
			}
		})
	}
}

func TestHTTPFetchFollowsAuthorizedRedirectWithoutForwardingHeaders(t *testing.T) {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/start" {
			w.Header().Set("Set-Cookie", "private=secret")
			w.Header().Set("Location", "/final")
			w.WriteHeader(http.StatusFound)
			return
		}
		if r.URL.Path != "/final" || r.Header.Get("Referer") != "" || r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Errorf("unexpected destination request: %s %v", r.URL.Path, r.Header)
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("actual final evidence"))
	}))
	defer server.Close()
	adapter, err := authorizedAdapter(t, httpfetch.Config{URLs: []string{server.URL + "/start", server.URL + "/final"}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
	if err != nil {
		t.Fatal(err)
	}
	got, err := adapter.Fetch(context.Background(), fetch.Request{URL: server.URL + "/start", MaxBytes: 64, MaxRequests: 2, Timeout: time.Second})
	if err != nil || got.Requests != 2 || hits.Load() != 2 || got.RequestedURL != server.URL+"/start" || got.FinalURL != server.URL+"/final" || string(got.Body) != "actual final evidence" {
		t.Fatalf("redirect evidence: %+v %v hits=%d", got, err, hits.Load())
	}
}

func TestHTTPRedirectCannotExceedSourceOrRequestBounds(t *testing.T) {
	for _, mode := range []string{"source", "budget", "loop"} {
		t.Run(mode, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				if r.URL.Path == "/final" {
					t.Error("forbidden extra request reached final target")
				}
				w.Header().Set("Location", "/final")
				if mode == "loop" {
					w.Header().Set("Location", "/start")
				}
				w.WriteHeader(http.StatusFound)
			}))
			defer server.Close()
			urls := []string{server.URL + "/start"}
			limit := 2
			expected := fetch.Denied
			expectedRequests := 1
			if mode == "budget" {
				urls = append(urls, server.URL+"/final")
				limit = 1
				expected = fetch.LimitExceeded
			}
			if mode == "loop" {
				expected = fetch.LimitExceeded
				expectedRequests = 2
			}
			adapter, err := authorizedAdapter(t, httpfetch.Config{URLs: urls, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			got, err := adapter.Fetch(context.Background(), fetch.Request{URL: server.URL + "/start", MaxBytes: 64, MaxRequests: limit, Timeout: time.Second})
			if err != expected || got.Requests != expectedRequests || int(hits.Load()) != expectedRequests || len(got.Body) != 0 || got.FinalURL != "" {
				t.Fatalf("redirect bound: %+v err=%v hits=%d", got, err, hits.Load())
			}
		})
	}
}

func TestHTTPFetchDistinguishesTimeoutAndCancellation(t *testing.T) {
	for _, mode := range []string{"timeout", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); <-r.Context().Done() }))
			defer server.Close()
			adapter, err := authorizedAdapter(t, httpfetch.Config{URLs: []string{server.URL}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			want := fetch.TimedOut
			requests := 1
			if mode == "cancel" {
				cancel()
				want = fetch.Cancelled
				requests = 0
			}
			got, err := adapter.Fetch(ctx, fetch.Request{URL: server.URL, MaxBytes: 64, MaxRequests: 1, Timeout: 100 * time.Millisecond})
			if err != want || got.Requests != requests || int(hits.Load()) != requests || len(got.Body) != 0 || got.FinalURL != "" {
				t.Fatalf("failure reason: %+v err=%v hits=%d", got, err, hits.Load())
			}
		})
	}
}
