package fetchcheck

import (
	"context"
	acquisitionexecution "lerna/adapters/research/execution"
	"lerna/fetch"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecutionFetchRequiresTaskGuard(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("configuration test dispatched HTTP") }))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	driver, err := acquisitionexecution.NewPage(h.http, h.attempts, h.evidence, h.auth, acquisitionexecution.Config{Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: 1024, MaxRequests: 2, TaskLimit: 2, Timeout: time.Second})
	if err != fetch.Invalid || driver != nil {
		t.Fatal("execution fetch driver accepted missing task guard")
	}
}
