package simulator

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
)

// Bill 是供应商独立保存的收费事实；不使用被测预算的余额或结算类型。
type Bill struct {
	Rule           string `json:"rule"`
	Namespace      string `json:"namespace"`
	Account        string `json:"account"`
	NativeInstance string `json:"native_instance"`
	Component      string `json:"component"`
	SendID         string `json:"send_id"`
	ExternalKey    string `json:"external_key"`
	SourceVersion  uint64 `json:"source_version"`
	Unit           string `json:"unit"`
	Amount         int64  `json:"amount"`
	Final          bool   `json:"final"`
	PriceVersion   string `json:"price_version"`
}
type BillingTarget struct {
	withhold bool
	Target   *Target
	mu       sync.Mutex
	amount   int64
	drop     bool
	bills    []Bill
	alias    string
}

func NewBillingTarget(amount int64) *BillingTarget {
	return &BillingTarget{Target: New("idempotent"), amount: amount}
}
func (t *BillingTarget) WithholdBill(withhold bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.withhold = withhold
}
func (t *BillingTarget) DropReceipt(drop bool) { t.mu.Lock(); defer t.mu.Unlock(); t.drop = drop }
func (t *BillingTarget) SetNativeInstance(alias string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.alias = alias
}
func (t *BillingTarget) Bills() []Bill {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]Bill(nil), t.bills...)
}
func (t *BillingTarget) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	t.mu.Lock()
	bill := Bill{Rule: "reference-billing-v1", Namespace: "lerna-reference", Account: "reference-account", NativeInstance: "charge:" + r.Header.Get("Lerna-Send-Id"), Component: "call", SendID: r.Header.Get("Lerna-Send-Id"), ExternalKey: r.Header.Get("Idempotency-Key"), SourceVersion: 1, Unit: "USD_MICRO", Amount: t.amount, Final: true, PriceVersion: "reference-price-v1"}
	if t.alias != "" {
		bill.NativeInstance = t.alias
	}
	t.bills = append(t.bills, bill)
	drop := t.drop
	withhold := t.withhold
	t.mu.Unlock()
	response := httptest.NewRecorder()
	t.Target.ServeHTTP(response, r)
	if drop {
		if h, ok := w.(http.Hijacker); ok {
			c, _, e := h.Hijack()
			if e == nil {
				_ = c.Close()
			}
		}
		return
	}
	var envelope map[string]any
	if json.Unmarshal(response.Body.Bytes(), &envelope) != nil {
		envelope = map[string]any{}
	}
	if !withhold {
		envelope["billing"] = bill
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(response.Code)
	_ = json.NewEncoder(w).Encode(envelope)
}
