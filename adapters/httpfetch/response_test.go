package httpfetch_test

import (
	"context"
	"lerna/adapters/httpfetch"
	"lerna/fetch"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestHTTPFetchClassifiesResponseWithoutReleasingFailedBody(t *testing.T) {
	for _, test := range []struct {
		name, media, body string
		status            int
		want              error
	}{
		{"binary", "application/octet-stream", "secret bytes", 200, fetch.Unsupported},
		{"foreign-charset", "text/plain; charset=iso-8859-1", "secret bytes", 200, fetch.Unsupported},
		{"invalid-utf8", "text/plain; charset=utf-8", string([]byte{0xff}), 200, fetch.Unsupported},
		{"invalid-json", "application/json", "{broken", 200, fetch.Unsupported},
		{"unauthenticated", "text/plain", "secret denial details", 401, fetch.Denied},
		{"forbidden", "text/plain", "secret denial details", 403, fetch.Denied},
		{"gone", "text/plain", "expired body", 410, fetch.Expired},
		{"missing", "text/plain", "private path", 404, fetch.Unavailable},
		{"utf8-text", "text/plain; charset=utf-8", "公开资料", 200, nil},
		{"json", "application/json", `{"text":"hello"}`, 200, nil},
		{"html", "text/html; charset=utf-8", "<p>untrusted source</p>", 200, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", test.media)
				w.WriteHeader(test.status)
				w.Write([]byte(test.body))
			}))
			defer server.Close()
			adapter, err := authorizedAdapter(t, httpfetch.Config{URLs: []string{server.URL}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			got, err := adapter.Fetch(context.Background(), fetch.Request{URL: server.URL, MaxBytes: 128, MaxRequests: 1, Timeout: time.Second})
			if err != test.want || got.Requests != 1 {
				t.Fatalf("response classification: %v requests=%d", err, got.Requests)
			}
			if test.want != nil && (len(got.Body) != 0 || got.FinalURL != "" || len(got.Sources) != 0) {
				t.Fatal("failed response released content")
			}
			if test.want == nil && string(got.Body) != test.body {
				t.Fatal("successful response changed source bytes")
			}
		})
	}
}
