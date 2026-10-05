package simulator

import (
	"encoding/json"
	"io"
	"net/http"
	"sync"
)

// ModelProvider 独立记录实际收到的请求和收费，不读取被测系统的持久状态。
type ModelProvider struct {
	mu       sync.Mutex
	Requests [][]byte
	Charges  []Bill
	Status   string
	Output   string
	Drop     bool
	Amount   int64
}

func NewModelProvider() *ModelProvider {
	return &ModelProvider{Status: "COMPLETED", Output: `{"kind":"COMPLETE"}`, Amount: 7}
}
func (p *ModelProvider) Calls() int { p.mu.Lock(); defer p.mu.Unlock(); return len(p.Requests) }
func (p *ModelProvider) Bills() []Bill {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Bill(nil), p.Charges...)
}
func (p *ModelProvider) Bodies() [][]byte {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out [][]byte
	for _, b := range p.Requests {
		out = append(out, append([]byte(nil), b...))
	}
	return out
}
func (p *ModelProvider) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	bill := Bill{Rule: "reference-billing-v1", Namespace: "lerna-reference", Account: "reference-account", NativeInstance: "charge:" + r.Header.Get("Lerna-Send-Id"), Component: "call", SendID: r.Header.Get("Lerna-Send-Id"), ExternalKey: r.Header.Get("Idempotency-Key"), SourceVersion: 1, Unit: "USD_MICRO", Amount: p.Amount, Final: true, PriceVersion: "reference-price-v1"}
	p.mu.Lock()
	p.Requests = append(p.Requests, body)
	p.Charges = append(p.Charges, bill)
	drop, status, output := p.Drop, p.Status, p.Output
	p.mu.Unlock()
	if drop {
		if h, ok := w.(http.Hijacker); ok {
			c, _, e := h.Hijack()
			if e == nil {
				_ = c.Close()
			}
		}
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"protocol": "lerna-model-v1", "external_key": r.Header.Get("Idempotency-Key"), "attempt_id": r.Header.Get("Lerna-Attempt"), "applied": true, "terminal": true, "model": map[string]any{"status": status, "output": output}, "billing": bill})
}
