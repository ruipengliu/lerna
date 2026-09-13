package fetchcheck

import (
	"context"
	"fmt"
	"lerna/adapters/contentpolicy"
	"lerna/brain"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestResearchInputValidationChargesCurrentAuthorityProbe(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprint(w, "Evidence.")
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	run, _ := prepareActionAnswerProcess(t, ctx, h)
	port, err := h.work.Actions(run.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	var input brain.Context = &fetchActionHost{h: h, queries: port}
	if err = input.Validate(ctx, run.Task, "local"); err != nil {
		t.Fatal(err)
	}
	current, err := h.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Actions.Queries) != len(run.Actions.Queries)+1 {
		t.Fatal("input validation bypassed task query budget")
	}
	rules := []contentpolicy.Rule{}
	state, err := h.policyStore.Load(ctx)
	if err != nil {
		t.Fatal(err)
	}
	// Keep the independent inline-goal authorization so this test reaches
	// the actual input READ whose denied attempt must consume query quota.
	for _, rule := range state.Rules {
		if rule.Kind == "task-goal" {
			rules = append(rules, rule)
		}
	}
	for _, key := range []string{"start", "final"} {
		rules = append(rules, contentpolicy.Rule{Kind: "web", Key: key, Revision: 1, Actions: []string{"discover"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()})
	}
	if err = h.policy.Replace(rules); err != nil {
		t.Fatal(err)
	}
	if err = input.Validate(ctx, run.Task, "local"); err == nil {
		t.Fatal("discovery permission substituted for processing permission")
	}
	after, err := h.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if len(after.Actions.Queries) != len(current.Actions.Queries)+1 {
		t.Fatal("denied authority probe was not charged")
	}
}
