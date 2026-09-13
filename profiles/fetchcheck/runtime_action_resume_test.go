package fetchcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/fetchcontent"
	"lerna/adapters/fetchoutput"
	"lerna/artifacts"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"reflect"
	"sync/atomic"
	"testing"
)

type interruptDiscoveryRead struct {
	contentService
	cancel context.CancelFunc
	fired  bool
	target string
}

func (c *interruptDiscoveryRead) Call(ctx context.Context, b artifacts.Binding, r *wire.ContentRequest) (*wire.ContentResponse, error) {
	out, err := c.contentService.Call(ctx, b, r)
	var acquired fetch.Result
	if err == nil && r.Method == "READ" && json.Unmarshal(out.GetData(), &acquired) == nil && acquired.RequestedURL == c.target {
		c.fired = true
		c.cancel()
	}
	return out, err
}

func TestRuntimeResumesOriginalSearchDispatch(t *testing.T) {
	for _, revoked := range []bool{false, true} {
		t.Run(fmt.Sprint(revoked), func(t *testing.T) { checkRuntimeSearchDispatch(t, revoked) })
	}
}
func checkRuntimeSearchDispatch(t *testing.T, revoked bool) {
	ctx := context.Background()
	var endpoint string
	var searches, pages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/search" {
			searches.Add(1)
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"results":[{"url":%q,"title":"Record","snippet":"Read page"}]}`, endpoint+"/page")
		} else {
			pages.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprint(w, "The record opened in 2001.")
		}
	}))
	defer server.Close()
	endpoint = server.URL
	root := t.TempDir()
	model := &interruptedRuntimeModel{Model: decisionFixtureModel{evidence: true}}
	cfg := RuntimeConfig{Goal: "Read record", Query: "record", SearchEndpoint: endpoint + "/search", SearchFormat: "json", SearchRecipient: "local", SearchMaxBytes: 4096, PageMaxBytes: 1024, URLs: []string{endpoint + "/search?q=record", endpoint + "/page"}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true, MaxQueries: 128, NetworkLimit: 2, MaxSteps: 3, ModelTokens: 32768}
	h, err := openWithAcquisition(ctx, root, "", cfg.URLs, networkConfig{Networks: cfg.Networks, AllowLoopbackHTTP: true}, cfg.PageMaxBytes)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	h.searchFormat = cfg.SearchFormat
	h.inputSources = []*wire.ContentSource{{Kind: "task-goal", Key: "inline", Revision: 1}}
	if err := bindRunConfiguration(ctx, root, "research-host.json", true, runtimeManifest{1, h.token, cfg, model.Capabilities()}); err != nil {
		t.Fatal(err)
	}
	interrupted, cancel := context.WithCancel(ctx)
	fault := &interruptDiscoveryRead{contentService: h.content, cancel: cancel, target: cfg.SearchEndpoint + "?q=record"}
	h.content = fault
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	h.evidence, err = fetchcontent.New(h.content, binding, h.evidenceConfig)
	if err != nil {
		t.Fatal(err)
	}
	h.access, err = fetchoutput.New(h.content, binding, h.clock)
	if err != nil {
		t.Fatal(err)
	}
	_, err = runPreparedResearch(interrupted, h, researchRunSpec{PersistTask: true, Goal: cfg.Goal, Query: cfg.Query, SearchEndpoint: cfg.SearchEndpoint, SearchConfig: searchProviderConfig{MaxBytes: cfg.SearchMaxBytes, Recipient: cfg.SearchRecipient}, Queries: cfg.MaxQueries, NetworkLimit: cfg.NetworkLimit, Steps: cfg.MaxSteps, ModelTokens: cfg.ModelTokens, AnswerModel: model})
	cancel()
	if err == nil || !fault.fired || searches.Load() != 1 || pages.Load() != 0 || model.calls != 0 {
		t.Fatalf("did not interrupt original search: %v", err)
	}
	inspector, _, ref, loadErr := openRecordedResearch(ctx, root)
	if loadErr != nil {
		t.Fatal(loadErr)
	}
	inspector.close()
	baseline, loadErr := h.core.Load(ctx, ref)
	if loadErr != nil || baseline.Actions == nil || len(baseline.Actions.Actions) != 1 || baseline.Actions.Actions[0].Status != "DISPATCHED" {
		t.Fatal("original dispatched search missing")
	}
	original, known, loadErr := h.attempts.Outcome(ctx, ref.Namespace, baseline.Actions.Actions[0].OperationID)
	if loadErr != nil || !known || original.Status != "acquired" {
		t.Fatal("original acquisition was not durable")
	}
	if revoked {
		if err := h.policy.Replace([]contentpolicy.Rule{}); err != nil {
			t.Fatal(err)
		}
	}
	h.close()
	before, err := QueryResearch(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	view, err := ResumeResearch(ctx, root, model)
	if revoked {
		if err == nil || len(view.Body) != 0 || searches.Load() != 1 || pages.Load() != 0 || model.calls != 0 {
			t.Fatal("revoked recovery disclosed or executed")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	restored, _, _, err := openRecordedResearch(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.close()
	after, err := restored.core.Load(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	observed, known, err := restored.attempts.Outcome(ctx, ref.Namespace, baseline.Actions.Actions[0].OperationID)
	if err != nil || !known || !reflect.DeepEqual(observed, original) || after.Actions.Actions[0].OperationID != baseline.Actions.Actions[0].OperationID || after.Actions.Limits != baseline.Actions.Limits || len(after.Actions.Queries) <= len(baseline.Actions.Queries) || len(after.Actions.Queries) > int(after.Actions.Limits.MaxQueries) {
		t.Fatal("recovery replaced facts or query budget")
	}
	budget, err := restored.attempts.Budget(ctx, ref)
	if err != nil || budget.Charged != 2 {
		t.Fatal("recovery reset network budget")
	}
	if view.Task.Ref != before.Task.Ref || view.Task.State != "COMPLETED" || len(view.Body) == 0 || searches.Load() != 1 || pages.Load() != 1 || model.calls != 1 {
		t.Fatal("recovery replaced search or task")
	}
}
