package fetchcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/adapters/sqlitecatalog"
	"lerna/catalog"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestRuntimeResumesUninitializedActions(t *testing.T) {
	for _, stage := range []string{"queued", "claimed", "started"} {
		t.Run(stage, func(t *testing.T) { checkRuntimeStartResume(t, stage) })
	}
}

func checkRuntimeStartResume(t *testing.T, stage string) {
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
	search, err := bindConfiguredSearchHost(h, cfg.SearchEndpoint, cfg.NetworkLimit, nil, searchProviderConfig{MaxBytes: cfg.SearchMaxBytes, Recipient: cfg.SearchRecipient})
	if err != nil {
		t.Fatal(err)
	}
	directory, err := sqlitecatalog.Open(filepath.Join(root, "fetch-catalog.db"), func(catalog.Entry) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	entries := []catalog.Entry{}
	for _, cap := range []execution.Capability{h.cap, search.h.cap} {
		entries = append(entries, catalog.Entry{Source: catalog.Source{Kind: "capability", Key: "fetch", Revision: 1}, Ref: catalog.Ref{Namespace: "local", Name: cap.Name, Version: cap.Version, Implementation: cap.Implementation, ImplementationVersion: cap.ImplementationVersion, Digest: cap.Digest()}, Capability: cap, Title: "Acquire bounded evidence", Category: "fetch", ResourceType: "web", Purpose: "task", Location: "local", Resource: "root", Preconditions: "Authorized source and bounded input", Effects: "Retain actual HTTP evidence", Unsupported: "Arbitrary sources", Guarantees: "Original operation recovery", Available: true})
	}
	_, err = directory.Replace(ctx, 0, entries)
	directory.Close()
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"query": cfg.Query})
	input, err := h.put(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	op, err := h.operation(ctx)
	if err != nil {
		t.Fatal(err)
	}
	task, err := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: cfg.Goal, InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: cfg.MaxSteps, ModelRequests: 3, ModelTokens: cfg.ModelTokens, DeadlineUnix: h.now().Add(time.Minute).Unix()}})
	if err != nil {
		t.Fatal(err)
	}
	if err := bindRunConfiguration(ctx, root, "research-task.json", true, runtimeTaskManifest{1, task.Ref}); err != nil {
		t.Fatal(err)
	}
	run, err := h.core.Load(ctx, task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if stage != "queued" {
		run, err = h.work.Commit(ctx, tasks.WorkChange{Kind: "claim", ChangeID: "initial-claim", Qualification: tasks.QualificationOf(run)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if stage == "started" {
		run, err = h.work.Commit(ctx, tasks.WorkChange{Kind: "start", ChangeID: "initial-start", Qualification: tasks.QualificationOf(run)})
		if err != nil {
			t.Fatal(err)
		}
	}
	if run.Actions != nil || run.Task.ModelUsedRequests != 0 {
		t.Fatal("startup checkpoint already executed actions")
	}
	h.close()
	view, err := ResumeResearch(ctx, root, model)
	if err != nil {
		t.Fatal(err)
	}
	if view.Task.Ref != task.Ref || view.Task.State != "COMPLETED" || view.Task.Attempts != 1 || view.Task.ModelUsedRequests != 3 || searches.Load() != 1 || pages.Load() != 1 || model.calls != 1 || len(view.Body) == 0 {
		t.Fatal("startup recovery replaced identity, claim or work")
	}
}
