//go:build fault && darwin && cgo

package fault_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync"
	"testing"

	"github.com/ruipengliu/lerna/conformance/executor"
)

func nativeAPIExecutionFixture(t *testing.T) executor.Fixture {
	t.Helper()
	var mu sync.Mutex
	calls := 0
	records := map[string]string{}
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		body, err := io.ReadAll(r.Body)
		key := r.Header.Get("Idempotency-Key")
		if err != nil || r.Method != "POST" || string(body) != `{"quantity":1,"value":"hello"}` || r.Header.Get("Authorization") != "Bearer synthetic-api-fault-secret-f17e4038" || r.Header.Get("Lerna-Account") != "synthetic-account" || r.Header.Get("Lerna-Attempt") == "" || key == "" {
			http.Error(w, "invalid fixed request", http.StatusBadRequest)
			return
		}
		if prior, exists := records[key]; exists && prior != string(body) {
			http.Error(w, "identity conflict", http.StatusConflict)
			return
		}
		records[key] = string(body)
		_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-reference-api-v1", "external_key": key, "attempt_id": r.Header.Get("Lerna-Attempt"), "account": "synthetic-account", "origin": "http://" + r.Host, "applied": true, "terminal": true})
	}))
	t.Cleanup(target.Close)
	h, _, admission, start := prepareAPIStartFault(t, filepath.Join(t.TempDir(), "state.db"), target.URL)
	t.Cleanup(func() { _ = h.Close() })
	return executor.Fixture{Harness: h, Context: context.Background(), Admission: admission, Start: start, ExpectedCalls: 1, LoseDispatchReceipt: loseExecutionDispatchReceipt, Actual: func() (int, int) {
		mu.Lock()
		defer mu.Unlock()
		return calls, len(records)
	}}
}

// 规则：G1、G2、G3、G4、G5、G10、G11
func TestAPIExecutionAdapterConformance(t *testing.T) {
	executor.Run(t, nativeAPIExecutionFixture)
}
