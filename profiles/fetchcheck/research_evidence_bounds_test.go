package fetchcheck

import (
	"context"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResearchEvidenceLengthsExcludeIndependentObservers(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Actual evidence."))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	var port *tasks.ActionPort
	run, ref := prepareActionAnswerProcess(t, ctx, h, func(p *tasks.ActionPort) { port = p })
	if _, err = h.evidence.Read(ctx, ref); err != nil {
		t.Fatal(err)
	}
	before := len(run.Actions.Queries)
	for i := 0; i < 2; i++ {
		reader, err := meteredResearchEvidence(h, port, run)
		if err != nil {
			t.Fatal(err)
		}
		result, err := reader.Read(ctx, ref)
		if err != nil || string(result.Body) != "Actual evidence." {
			t.Fatalf("task evidence: %v", err)
		}
	}
	after, err := h.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Actions.Queries)-before != 3 {
		t.Fatalf("observer changed task accounting or task views lost lengths: %d", len(after.Actions.Queries)-before)
	}
}
