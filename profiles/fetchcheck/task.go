package fetchcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

// CheckTask uses the actual Core, signed grant, Capability SDK and HTTP adapter.
// Its server is a real local HTTP fixture, not public-network acceptance evidence.
func CheckTask(ctx context.Context) error {
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	urls := []string{server.URL + "/start", server.URL + "/final"}
	h, err := fresh(ctx, urls)
	if err != nil {
		return err
	}
	defer h.destroy()
	body, _ := json.Marshal(map[string]any{"url": urls[0], "max_bytes": 1024, "max_requests": 2, "timeout_ms": 1000})
	request, material, err := h.request(ctx, body)
	if err != nil {
		return err
	}
	// A separately signed but stale qualification must fail before acquisition.
	stale := request
	stale.OperationID, err = h.operation(ctx)
	if err != nil {
		return err
	}
	stale.Qualification.Version++
	staleGrant, err := h.issue(ctx, stale)
	if err != nil {
		return err
	}
	if _, err = h.client.Invoke(ctx, stale, staleGrant); err == nil {
		return fmt.Errorf("stale task qualification admitted")
	}
	if _, err = h.attempts.Inspect(ctx, "local", stale.OperationID); err != fetch.Missing || hits.Load() != 0 {
		return fmt.Errorf("stale qualification caused acquisition")
	}
	receipt, err := h.client.Invoke(ctx, request, material)
	if err != nil {
		return err
	}
	outcome, err := h.exec.Run(ctx, request.OperationID)
	if err != nil {
		return err
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		return err
	}
	task, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil {
		return err
	}
	if outcome.Result != "SUCCESS" || outcome.Effect != "CONFIRMED" || task.State != "COMPLETED" || hits.Load() != 2 {
		return fmt.Errorf("fetch task state=%s result=%s requests=%d", task.State, outcome.Result, hits.Load())
	}
	input, err := h.access.Read(ctx, h.token, outcome.Reference, h.cap)
	if err != nil {
		return err
	}
	var envelope struct {
		Output struct {
			Status    string `json:"status"`
			Reference string `json:"reference"`
		} `json:"output"`
	}
	if json.Unmarshal(input, &envelope) != nil || envelope.Output.Status != "acquired" {
		return fmt.Errorf("missing acquisition output")
	}
	evidence, err := h.evidence.Read(ctx, envelope.Output.Reference)
	if err != nil {
		return err
	}
	if string(evidence.Body) != "hello" || evidence.RequestedURL != urls[0] || evidence.FinalURL != urls[1] || len(evidence.Sources) != 2 || evidence.Requests != 2 || evidence.SHA256 != "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
		return fmt.Errorf("wrong acquired evidence")
	}
	budget, err := h.attempts.Budget(ctx, request.Qualification.Ref)
	if err != nil || budget.Charged != 2 {
		return fmt.Errorf("missing original task budget")
	}
	replay, err := h.client.Invoke(ctx, request, "")
	if err != nil {
		return err
	}
	if replay != receipt {
		return fmt.Errorf("replacement receipt")
	}
	if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
		return err
	}
	current, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil {
		return err
	}
	if current.Version != task.Version || hits.Load() != 2 {
		return fmt.Errorf("replay repeated task or acquisition")
	}
	// The outer Execution artifact must carry the source restrictions too.
	if err = h.policy.Replace(nil); err != nil {
		return err
	}
	ref, _ := answers.ParseReference(outcome.Reference)
	_, err = h.content.Call(ctx, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
	if artifacts.Code(err) != "PERMISSION_DENIED" {
		return fmt.Errorf("outer execution artifact lost source restrictions")
	}
	return nil
}
