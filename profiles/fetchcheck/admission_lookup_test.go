package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"slices"
	"sync/atomic"
	"testing"
)

func TestOriginalAdmissionReadableAfterInvokeRevocation(t *testing.T) {
	ctx := context.Background()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Original evidence"))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	raw, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := h.exec.Invoke(ctx, request, grant)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := h.exec.Run(ctx, request.OperationID); err != nil {
		t.Fatal(err)
	}
	state, err := h.db.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, rule := range state.State.Rules {
		rule.Scope.Actions = slices.DeleteFunc(rule.Scope.Actions, func(action string) bool { return action == "capability.invoke" })
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	command := &wire.AuthorizationCommand{ExpectedRevision: state.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: state.State.Rules}}}
	if _, err := h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); err != nil {
		t.Fatal(err)
	}
	restored, found, err := h.exec.LookupAdmission(ctx, request)
	if err != nil || !found || restored != receipt {
		t.Fatalf("original receipt unavailable: found=%v err=%v", found, err)
	}
	if _, err := h.exec.Run(ctx, request.OperationID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.exec.Reconcile(ctx, request.OperationID); err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 {
		t.Fatal("original request repeated")
	}
}
