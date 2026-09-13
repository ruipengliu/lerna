package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/execution"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestExpiredEvidencePreservesAcquiredFactWithoutRefetch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	raw, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 2, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	result, err := h.exec.Run(ctx, request.OperationID)
	if err != nil || result.Result != "SUCCESS" {
		t.Fatalf("initial acquisition: %s %v", result.Result, err)
	}
	original, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
	if err != nil || !known || original.Status != "acquired" {
		t.Fatal("missing original acquisition")
	}
	h.clock.advance(11 * time.Minute)
	body, err := h.evidence.Read(ctx, original.Reference)
	if err != fetch.Expired || len(body.Body) != 0 {
		ref, _ := answers.ParseReference(original.Reference)
		detail, e := h.content.Call(ctx, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
		t.Fatalf("expired evidence was not distinguished: %v state=%s kind=%s content_error=%v", err, detail.GetRecord().GetState(), detail.GetRecord().GetSpec().GetKind(), e)
	}
	observation, err := h.target.Inspect(ctx, execution.Call{Request: request})
	var failure struct{ Status, Reference string }
	if err != nil || observation.Phase != "FINISHED" || observation.Result != "FAILURE" || observation.Effect != "CONFIRMED" || json.Unmarshal(observation.Output, &failure) != nil || failure.Status != "expired" || failure.Reference != "" {
		t.Fatalf("expired evidence erased known effect or became success: %+v %v", observation, err)
	}
	saved, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
	if err != nil || !known || saved != original || hits.Load() != 1 {
		t.Fatal("expiry rewrote original facts or refetched")
	}
	budget, err := h.attempts.Budget(ctx, request.Qualification.Ref)
	if err != nil || budget.Charged != 2 {
		t.Fatal("expiry refunded original budget")
	}
	if err = h.content.Clean(ctx); err != nil {
		t.Fatal(err)
	}
	body, err = h.evidence.Read(ctx, original.Reference)
	if err != fetch.Expired || len(body.Body) != 0 {
		t.Fatalf("cleaned expiry lost retention facts: %v", err)
	}
	// Revocation remains stronger than metadata-only expiry classification.
	if err = h.policy.Replace(nil); err != nil {
		t.Fatal(err)
	}
	observation, err = h.target.Inspect(ctx, execution.Call{Request: request})
	if err != fetch.Denied || observation.Phase != "UNKNOWN" || len(observation.Output) != 0 || len(observation.Evidence) != 0 {
		t.Fatalf("expiry bypassed revoked source: %+v %v", observation, err)
	}

}
