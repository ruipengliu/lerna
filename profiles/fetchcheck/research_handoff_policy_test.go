package fetchcheck

import (
	"context"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResearchAnswerHandoffDoesNotRestoreRevokedSources(t *testing.T) {
	t.Run("same_host", func(t *testing.T) { checkHandoffPolicy(t, false, false) })
	t.Run("reopened_stores", func(t *testing.T) { checkHandoffPolicy(t, true, false) })
	t.Run("goal_revoked_only", func(t *testing.T) { checkHandoffPolicy(t, false, true) })
}
func checkHandoffPolicy(t *testing.T, reopen, goalOnly bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("Acquired before source revocation."))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/start", server.URL + "/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	var port *tasks.ActionPort
	run, _ := prepareActionAnswerProcess(t, ctx, h, func(p *tasks.ActionPort) { port = p })
	if goalOnly {
		state, e := h.policyStore.Load(ctx)
		if e != nil {
			t.Fatal(e)
		}
		rules := state.Rules[:0]
		for _, rule := range state.Rules {
			if rule.Kind != "task-goal" {
				rules = append(rules, rule)
			}
		}
		err = h.policy.Replace(rules)
	} else {
		err = h.policy.Replace(nil)
	}
	if err != nil {
		t.Fatal(err)
	}
	at := h.now().Unix()
	before, err := h.policy.Revision(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if reopen {
		root, token, urls := h.root, h.token, h.urls
		h.close()
		h, err = open(ctx, root, token, urls)
		if err != nil {
			t.Fatal(err)
		}
		defer h.close()
		port, err = h.work.Actions(run.Actions.Limits)
		if err != nil {
			t.Fatal(err)
		}
	}
	model := &alternativeEvidenceModel{location: "local"}
	_, answerErr := processResearchAnswer(ctx, h, port, run, model, nil, false)
	after, err := h.policy.Revision(ctx, at)
	if err != nil {
		t.Fatal(err)
	}
	if after != before {
		t.Fatal("answer handoff replaced the host's revoked source policy")
	}
	if answerErr == nil || model.calls != 0 {
		t.Fatalf("revoked source reached answer model: calls=%d err=%v", model.calls, answerErr)
	}
	current, err := h.core.Load(ctx, run.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if current.Task.State == "COMPLETED" || current.Task.Result != "" {
		t.Fatal("revoked source was published")
	}
}

func TestResearchReopenDoesNotSeedMissingPolicyStore(t *testing.T) {
	t.Run("missing", func(t *testing.T) { checkUnavailablePolicyStore(t, false) })
	t.Run("corrupt", func(t *testing.T) { checkUnavailablePolicyStore(t, true) })
}
func checkUnavailablePolicyStore(t *testing.T, corrupt bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	h, err := fresh(ctx, []string{"https://source.example/start", "https://source.example/final"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	root, token, urls := h.root, h.token, h.urls
	h.close()
	if corrupt {
		err = os.WriteFile(filepath.Join(root, "source-policy.db"), []byte("corrupted policy database"), 0600)
	} else {
		err = os.Remove(filepath.Join(root, "source-policy.db"))
	}
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := open(ctx, root, token, urls)
	if err == nil {
		reopened.close()
		t.Fatal("existing host reseeded permissions after losing its policy store")
	}
}
