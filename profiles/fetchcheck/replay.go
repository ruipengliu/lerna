package fetchcheck

import (
	"context"
	"encoding/json"
	"fmt"
	executionlocal "lerna/adapters/execution/local"
	fetchauth "lerna/adapters/research/auth"
	acquisitionexecution "lerna/adapters/research/execution"
	"lerna/adapters/research/replayfetch"
	fetchtask "lerna/adapters/research/taskguard"
	"lerna/execution"
	"lerna/sdk"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"time"
)

// CheckFixedReplay exercises the alternate adapter through Core, SDK and Content.
// Its fixed bytes and zero HTTP requests are not public-network evidence.
func CheckFixedReplay(parent context.Context) error {
	ctx, cancel := context.WithTimeout(parent, 20*time.Second)
	defer cancel()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { hits.Add(1) }))
	defer server.Close()
	url := server.URL + "/start"
	h, err := fresh(ctx, []string{url, server.URL + "/final"})
	if err != nil {
		return err
	}
	defer h.destroy()
	authority, err := fetchauth.New(h.auth, fetchauth.Scope{Token: h.token, Namespace: "local", Subject: "operator", Purpose: "task", Location: "local", Recipient: "local", Resources: map[string]string{url: "root"}})
	if err != nil {
		return err
	}
	adapter, err := replayfetch.New(authority, h.clock, []replayfetch.Entry{{URL: url, Body: []byte("hello"), SHA256: "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"}})
	if err != nil {
		return err
	}
	h.cap.Implementation = "fixed-replay"
	guard, err := fetchtask.New(h.auth, h.work, h.core, h.token)
	if err != nil {
		return err
	}
	driver, err := acquisitionexecution.NewPage(adapter, h.attempts, h.evidence, h.auth, acquisitionexecution.Config{Guard: guard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: 1024, MaxRequests: 1, TaskLimit: 1, Timeout: time.Second})
	if err != nil {
		return err
	}
	h.exec, err = execution.New(h.grants, h.work, h.access, driver, h.binding, h.cap, config(), h.operation)
	if err != nil {
		return err
	}
	// Rebind the SDK to the alternate implementation, preserving the same capability.
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	raw, _ := json.Marshal(map[string]any{"url": url, "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, raw)
	if err != nil {
		return err
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		return err
	}
	before := time.Now()
	result, err := h.exec.Run(ctx, request.OperationID)
	if err != nil || result.Result != "SUCCESS" {
		return fmt.Errorf("replay task: %s %v", result.Result, err)
	}
	rawOutput, err := h.access.Read(ctx, h.token, result.Reference, h.cap)
	var envelope struct {
		Evidence struct {
			Mode     string `json:"mode"`
			Requests uint32 `json:"requests"`
		} `json:"evidence"`
	}
	if err != nil || json.Unmarshal(rawOutput, &envelope) != nil || envelope.Evidence.Mode != "fixed-replay" || envelope.Evidence.Requests != 0 {
		return fmt.Errorf("outer artifact lost the replay label")
	}
	outcome, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
	if err != nil || !known || outcome.Mode != "fixed-replay" || outcome.Requests != 0 || hits.Load() != 0 {
		return fmt.Errorf("fixed data masqueraded as network acquisition")
	}
	evidence, err := h.evidence.Read(ctx, outcome.Reference)
	if err != nil || evidence.Mode != "fixed-replay" || evidence.HTTPStatus != 0 || evidence.Requests != 0 || string(evidence.Body) != "hello" || evidence.FetchedAt.Before(before) {
		return fmt.Errorf("fixed replay lost explicit mode or actual local read time")
	}
	budget, err := h.attempts.Budget(ctx, request.Qualification.Ref)
	if err != nil || budget.Charged != 1 {
		return fmt.Errorf("replay escaped the original reservation")
	}
	if err = h.exec.Drain(ctx, 16); err != nil {
		return err
	}
	task, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil || task.State != "COMPLETED" {
		return fmt.Errorf("alternate adapter did not complete Core task")
	}
	return nil
}
