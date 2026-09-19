package fetchcheck

import (
	"context"
	"fmt"
	taskcontent "lerna/adapters/tasks/content"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResearchContentQueriesRetainFailedReadsAndReopenBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Evidence.")
	}))
	defer server.Close()
	root := t.TempDir()
	urls := []string{server.URL + "/start", server.URL + "/final"}
	h, err := open(ctx, root, "", urls)
	if err != nil {
		t.Fatal(err)
	}
	run, ref := prepareActionAnswerProcess(t, ctx, h)
	bind := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	port, err := h.work.Actions(run.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	content, err := taskcontent.New(h.content, port, tasks.QualificationOf(run))
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := answers.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	request := &wire.ContentRequest{Method: "READ", Ref: parsed, Purpose: "task", Limit: 1}
	stale := tasks.QualificationOf(run)
	stale.Version++
	fenced, err := taskcontent.New(h.content, port, stale)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = fenced.Call(ctx, bind, request); err == nil {
		t.Fatal("stale worker queried Content")
	}
	denied := bind
	denied.Token = "not-a-token"
	_, readErr := content.Call(ctx, denied, request)
	if readErr == nil {
		t.Fatal("unauthenticated read accepted")
	}
	current, err := h.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Actions.Queries) != len(run.Actions.Queries)+1 {
		t.Fatalf("failed read was refunded: before=%d after=%d read=%v", len(run.Actions.Queries), len(current.Actions.Queries), readErr)
	}
	for i := len(current.Actions.Queries); i < int(run.Actions.Limits.MaxQueries); i++ {
		if _, err = content.Call(ctx, bind, request); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = content.Call(ctx, bind, request); err == nil || !strings.Contains(err.Error(), "QUERY_BUDGET_EXCEEDED") {
		t.Fatalf("read exceeded task quota: %v", err)
	}
	checkAssessment := func(h *harness, port *tasks.ActionPort, run tasks.RunSnapshot) {
		var assessor brain.MeteredAssessor = &researchActionHost{fetchActionHost: &fetchActionHost{h: h, queries: port}, search: &fetchActionHost{h: h}, task: run.Task.Ref, outcomes: map[string]fetch.Outcome{}}
		if _, err := assessor.AssessMetered(ctx, run); err == nil || !strings.Contains(err.Error(), "QUERY_BUDGET_EXCEEDED") {
			t.Fatalf("assessment observed outcomes outside the original quota: %v", err)
		}
	}
	checkAssessment(h, port, current)
	token := h.token
	h.close()
	reopened, err := open(ctx, root, token, urls)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.close()
	current, err = reopened.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Actions.Queries) != int(run.Actions.Limits.MaxQueries) {
		t.Fatal("reopening reset charged reads")
	}
	port, err = reopened.work.Actions(current.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	content, err = taskcontent.New(reopened.content, port, tasks.QualificationOf(current))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = content.Call(ctx, bind, request); err == nil || !strings.Contains(err.Error(), "QUERY_BUDGET_EXCEEDED") {
		t.Fatalf("reopened adapter reset quota: %v", err)
	}
	checkAssessment(reopened, port, current)
}
