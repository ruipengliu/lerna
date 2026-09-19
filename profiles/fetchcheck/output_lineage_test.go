package fetchcheck

import (
	"context"
	"encoding/json"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/answers"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestExecutionOutputInheritsNewlyAcquiredRedirectSource(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("hello"))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	h.inputSources = []*wire.ContentSource{{Kind: "web", Key: "start", Revision: 1}}
	body, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 2, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	outcome, err := h.exec.Run(ctx, request.OperationID)
	if err != nil || outcome.Result != "SUCCESS" {
		t.Fatalf("acquisition: %s %v", outcome.Result, err)
	}
	ref, err := answers.ParseReference(outcome.Reference)
	if err != nil {
		t.Fatal(err)
	}
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	record, err := h.content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, source := range record.Record.Spec.Sources {
		if source.Kind == "web" && source.Key == "final" && source.Revision == 1 {
			found = true
		}
	}
	if !found {
		t.Fatal("Execution wrapper discarded newly acquired source")
	}
	raw, err := h.access.Read(ctx, h.token, outcome.Reference, h.cap)
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Output   json.RawMessage `json:"output"`
		Evidence json.RawMessage `json:"evidence"`
	}
	if json.Unmarshal(raw, &envelope) != nil {
		t.Fatal("invalid outer artifact")
	}
	h.clock.advance(time.Second)
	replay, err := h.access.Save(ctx, h.token, outcome.OutputOperation, request.InputRef, h.cap, envelope.Output, envelope.Evidence)
	if err != nil || replay != outcome.Reference {
		t.Fatalf("outer save changed immutable source facts: %v", err)
	}
	if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "web", Key: "start", Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(10 * time.Minute).Unix()}}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"}); artifacts.Code(err) != "PERMISSION_DENIED" {
		t.Fatal("new source revocation did not restrict wrapper")
	}
}
