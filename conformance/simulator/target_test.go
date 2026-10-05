package simulator_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ruipengliu/lerna/conformance/simulator"
)

// 规则：G1、G5
func TestTargetSeparatesEveryRequestFromIdempotentEffect(t *testing.T) {
	target := simulator.New("idempotent")
	server := httptest.NewServer(target)
	defer server.Close()
	for range 2 {
		req, e := http.NewRequest(http.MethodPost, server.URL, strings.NewReader("create"))
		if e != nil {
			t.Fatal(e)
		}
		req.Header.Set("Idempotency-Key", "stable-key")
		req.Header.Set("Lerna-Attempt", "attempt")
		req.Header.Set("Lerna-Send", "1")
		r, e := http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		r.Body.Close()
	}
	requests, effects := target.Snapshot()
	if len(requests) != 2 || len(effects) != 1 || requests[0].ExternalKey != "stable-key" || effects[0].AppliedAtUnixNano == 0 {
		t.Fatalf("requests=%v effects=%v", requests, effects)
	}
}
