package fetchcheck

import (
	"context"
	"encoding/json"
	contentpolicy "lerna/adapters/content/policy"
	researchlineage "lerna/adapters/research/lineage"
	"lerna/answers"
	"lerna/artifacts"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestAnswerInheritsAcquiredSourceAbsentFromOriginalInput(t *testing.T) {
	ctx := context.Background()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/start" {
			http.Redirect(w, r, "/final", 302)
			return
		}
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("acquired page"))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	source := &wire.ContentSource{Kind: "web", Key: "start", Revision: 1}
	h.inputSources = []*wire.ContentSource{source}
	raw, _ := json.Marshal(map[string]any{"url": server.URL + "/start", "max_bytes": 1024, "max_requests": 2, "timeout_ms": 1000})
	request, grant, err := h.request(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, request, grant); err != nil {
		t.Fatal(err)
	}
	if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
		t.Fatal(err)
	}
	result, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
	if err != nil || !known || result.Status != "acquired" {
		t.Fatal("no acquired response")
	}
	task, err := h.core.Get(ctx, h.token, request.Qualification.Ref)
	if err != nil {
		t.Fatal(err)
	}
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	lineage, err := researchlineage.New(h.content, binding, task, "task", []string{result.Reference})
	if err != nil {
		t.Fatal(err)
	}
	output, err := answers.NewContentAccess(h.content, h.policy, binding, source, "task", h.clock, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	output, err = output.WithLineage(lineage)
	if err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := output.Save(ctx, tasks.DecisionInput{Task: task, Generation: tasks.GenerationReservation{OutputOperation: op}}, []byte(`{"answer":"fixture","sources":[]}`))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := answers.ParseReference(saved)
	if err != nil {
		t.Fatal(err)
	}
	record, err := h.content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, s := range record.Record.Spec.Sources {
		if s.Key == "final" {
			found = true
		}
	}
	if !found {
		t.Fatal("answer omitted source learned during acquisition")
	}
	if err = h.policy.Replace([]contentpolicy.Rule{{Kind: "web", Key: "start", Revision: 1, Actions: []string{"store", "retain", "process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(10 * time.Minute).Unix()}}); err != nil {
		t.Fatal(err)
	}
	if _, err = h.content.Call(ctx, binding, &wire.ContentRequest{Method: "GET", Ref: ref, Purpose: "task"}); err == nil {
		t.Fatal("answer survived acquired source revocation")
	}
	if _, err = lineage.Sources(ctx, task, "local"); err == nil {
		t.Fatal("lineage revived revoked acquired source")
	}
}
