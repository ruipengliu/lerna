package fetchcheck

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	researchcontext "lerna/adapters/research/context"
	"lerna/brain"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
)

// CheckFailure observes known HTTP failure through a real admitted task and
// verifies that replay preserves its original request charge and task state.
func CheckFailure(ctx context.Context, status int) error {
	expected := map[int]string{403: "denied", 404: "unavailable", 410: "expired"}[status]
	if expected == "" {
		return fmt.Errorf("unsupported failure fixture")
	}
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.WriteHeader(status)
		w.Write([]byte("private failure details"))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		return err
	}
	defer h.destroy()
	body, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 2, "timeout_ms": 1000})
	request, material, err := h.request(ctx, body)
	if err != nil {
		return err
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
	if outcome.Result != "FAILURE" || outcome.Effect != "CONFIRMED" || task.State != "QUEUED" || hits.Load() != 2 {
		return fmt.Errorf("HTTP failure task state=%s result=%s effect=%s requests=%d", task.State, outcome.Result, outcome.Effect, hits.Load())
	}
	visible, err := h.client.GetInvocation(ctx, request.OperationID)
	if err != nil || visible.Reference == "" {
		return fmt.Errorf("SDK lost failure facts reference")
	}
	facts, err := h.access.Read(ctx, h.token, visible.Reference, h.cap)
	var document struct {
		Output   struct{ Status, Reference string } `json:"output"`
		Evidence struct {
			Status, Mode string
			Requests     uint32
		} `json:"evidence"`
	}
	if err != nil || json.Unmarshal(facts, &document) != nil || document.Output.Status != expected || document.Output.Reference != "" || document.Evidence.Status != expected || document.Evidence.Requests != 2 || document.Evidence.Mode != "http" || bytes.Contains(facts, []byte("private failure details")) || bytes.Contains(facts, []byte(server.URL)) {
		return fmt.Errorf("SDK failure facts are missing or disclose response details")
	}
	projection, err := researchcontext.NewPagesWithFailures(h.evidence, h.access.Failures(h.token, h.cap), task, "local", nil, []string{visible.Reference})
	if err != nil {
		return err
	}
	input, err := projection.Assemble(ctx, task, "local", brain.MaxInputBytes)
	if err != nil || len(input.Blocks) != 1 || input.Blocks[0].Role != "external-evidence-gap" || input.Blocks[0].Ref != visible.Reference || !bytes.Contains([]byte(input.Blocks[0].Text), []byte(expected)) {
		return fmt.Errorf("Task Context lost known acquisition gap")
	}
	actual, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
	if err != nil || !known || actual.Status != expected || actual.Requests != 2 || actual.Reference != "" {
		return fmt.Errorf("HTTP failure lost finite acquisition facts")
	}
	budget, err := h.attempts.Budget(ctx, request.Qualification.Ref)
	if err != nil || budget.Charged != 2 {
		return fmt.Errorf("HTTP failure refunded original budget")
	}
	replay, err := h.client.Invoke(ctx, request, "")
	if err != nil {
		return err
	}
	if replay != receipt {
		return fmt.Errorf("failure receipt changed")
	}
	if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
		return err
	}
	current, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil {
		return err
	}
	if current.Version != task.Version || current.State != task.State || hits.Load() != 2 {
		return fmt.Errorf("failure replay repeated acquisition")
	}
	if err = h.policy.Replace(nil); err != nil {
		return err
	}
	if _, err = projection.Assemble(ctx, task, "local", brain.MaxInputBytes); err == nil {
		return fmt.Errorf("revoked failure facts remained in Task Context")
	}
	return nil
}
