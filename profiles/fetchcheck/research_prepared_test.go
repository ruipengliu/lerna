package fetchcheck

import (
	"context"
	"fmt"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
)

func TestPreparedResearchUsesProvidedSearchAndPageServices(t *testing.T) {
	checkPreparedResearch(t, "", false)
}
func TestPreparedResearchAppliesProviderConfiguration(t *testing.T) {
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) { checkPreparedResearch(t, "remote-search", allowed) })
	}
}
func checkPreparedResearch(t *testing.T, recipient string, allowed bool) {
	ctx := context.Background()
	var searches, pages atomic.Int32
	var endpoint string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/discover" {
			searches.Add(1)
			if r.URL.Query().Get("q") != "supplied query" {
				t.Errorf("unexpected query")
			}
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintf(w, `{"results":[{"url":%q,"title":"Provided record","snippet":"Read the record"}]}`, endpoint+"/record")
			if recipient != "" {
				w.Write([]byte(strings.Repeat(" ", 2048)))
			}
			return
		}
		if r.URL.Path != "/record" {
			t.Errorf("unexpected page route")
		}
		pages.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		w.Write([]byte("This source was supplied by the caller, outside the frozen materials."))
	}))
	defer server.Close()
	endpoint = server.URL
	h, err := fresh(ctx, []string{endpoint + "/discover?q=supplied+query", endpoint + "/record"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	if recipient != "" {
		configureSearchRecipient(t, ctx, h, true, allowed)
	}
	spec := researchRunSpec{AnswerModel: decisionFixtureModel{evidence: true}, Goal: "Read the provided record", Query: "supplied query", SearchEndpoint: endpoint + "/discover", Queries: 128, NetworkLimit: 2, Steps: 3}
	if recipient != "" {
		spec.SearchConfig = searchProviderConfig{MaxBytes: 4096, Recipient: recipient}
	}
	record, err := runPreparedResearch(ctx, h, spec)
	if recipient != "" && !allowed {
		if err != nil || record.Answer.Status != "fetch_failed" || record.Usage.NetworkCharged != 1 || searches.Load() != 0 || pages.Load() != 0 {
			t.Fatalf("full task disclosed query without source permission: err=%v searches=%d pages=%d", err, searches.Load(), pages.Load())
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if searches.Load() != 1 || pages.Load() != 1 || record.Usage.SearchRequests != 1 || record.Usage.PageRequests != 1 || record.Usage.NetworkCharged != 2 {
		t.Fatalf("provided services or original budget bypassed: %+v search=%d pages=%d", record.Usage, searches.Load(), pages.Load())
	}
	if record.Answer.Status != "answerable" || len(record.Answer.Claims) != 1 || len(record.Answer.Claims[0].Citations) != 1 || !strings.Contains(record.Answer.Claims[0].Citations[0].Quote, "supplied by the caller") {
		t.Fatalf("provided evidence did not reach published answer: %+v", record.Answer)
	}
}

func TestPreparedResearchReportsSearchFailureWithoutRetry(t *testing.T) {
	ctx := context.Background()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "text/html")
		w.WriteHeader(http.StatusAccepted)
		w.Write([]byte("Search service could not return results."))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/discover?q=query", server.URL + "/unused"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	record, err := runPreparedResearch(ctx, h, researchRunSpec{AnswerModel: decisionFixtureModel{evidence: true}, Goal: "Find evidence", Query: "query", SearchEndpoint: server.URL + "/discover", Queries: 128, NetworkLimit: 2, Steps: 3})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || record.Usage.SearchRequests != 1 || record.Usage.PageRequests != 0 || record.Usage.NetworkCharged != 1 || record.Usage.ModelRequests != 2 || record.Answer.Status != "fetch_failed" || len(record.Answer.Claims) != 0 || len(record.Answer.Gaps) != 1 {
		t.Fatalf("known search failure was retried or lost: hits=%d record=%+v", hits.Load(), record)
	}
}

func TestResearchVerificationRejectsMismatchedObservations(t *testing.T) {
	ctx := context.Background()
	var searches, pages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		searches.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	h, err := fresh(ctx, []string{server.URL + "/discover?q=query", server.URL + "/unused"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	record, err := checkPreparedResearchRun(ctx, h, researchRunSpec{Goal: "Find evidence", Query: "query", SearchEndpoint: server.URL + "/discover", Queries: 128, NetworkLimit: 2, Steps: 3}, "empty_search", nil, false, &researchVerification{
		actions: 1, decisions: 1, searchRequests: 2, searchHits: &searches, pageHits: &pages,
	})
	if err == nil || !strings.HasPrefix(err.Error(), "search-fetch loop:") || record.UsageStatus != "snapshot" || record.Usage.SearchRequests != 1 || record.Usage.NetworkCharged != 1 || record.Usage.ModelRequests != 1 {
		t.Fatalf("verification accepted mismatched counts or lost original usage: %+v err=%v", record, err)
	}
}

func TestPreparedResearchEnforcesConfiguredNetwork(t *testing.T) {
	ctx := context.Background()
	var hits atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"results":[]}`))
	}))
	defer server.Close()
	for _, allowed := range []bool{false, true} {
		t.Run(fmt.Sprint(allowed), func(t *testing.T) {
			prefix := "192.0.2.0/24"
			if allowed {
				prefix = "127.0.0.0/8"
			}
			h, err := openWithNetwork(ctx, t.TempDir(), "", []string{server.URL + "/discover?q=query"}, networkConfig{Networks: []netip.Prefix{netip.MustParsePrefix(prefix)}, AllowLoopbackHTTP: true})
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			h.inputSources = []*wire.ContentSource{{Kind: "task-goal", Key: "inline", Revision: 1}}
			record, err := checkPreparedResearchRun(ctx, h, researchRunSpec{Goal: "Find evidence", Query: "query", SearchEndpoint: server.URL + "/discover", Queries: 128, NetworkLimit: 2, Steps: 3}, "", nil, true, nil)
			if err != nil {
				t.Fatal(err)
			}
			status, requests := "fetch_failed", int32(0)
			if allowed {
				status, requests = "insufficient", 1
			}
			if record.Answer.Status != status || hits.Load() != requests || record.Usage.NetworkCharged != 1 {
				t.Fatalf("network configuration lost: status=%s hits=%d usage=%+v", record.Answer.Status, hits.Load(), record.Usage)
			}
			for _, change := range []string{"network", "url"} {
				changedNetwork, changedURLs := h.network, append([]string(nil), h.urls...)
				if change == "network" {
					changedNetwork.Networks = []netip.Prefix{netip.MustParsePrefix("0.0.0.0/0")}
				} else {
					changedURLs = append(changedURLs, server.URL+"/unapproved")
				}
				restored, err := openWithNetwork(ctx, h.root, h.token, changedURLs, changedNetwork)
				if restored != nil {
					restored.close()
				}
				if err != fetch.Invalid {
					t.Fatalf("changed %s accepted on reopen: %v", change, err)
				}
			}
			restored, err := openWithNetwork(ctx, h.root, h.token, h.urls, h.network)
			if err != nil {
				t.Fatalf("original configuration no longer reopens: %v", err)
			}
			restored.close()
		})
	}
}

func TestResearchPageBudgetCannotChangeOnReopen(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	urls := []string{"https://source.example/start", "https://source.example/final"}
	network := networkConfig{Networks: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}
	h, err := openWithAcquisition(ctx, root, "", urls, network, 4096)
	if err != nil {
		t.Fatal(err)
	}
	token := h.token
	h.close()
	changed, err := openWithNetwork(ctx, root, token, urls, network)
	if changed != nil {
		changed.close()
	}
	if err != fetch.Invalid {
		t.Fatalf("page budget changed on reopen: %v", err)
	}
	restored, err := openWithAcquisition(ctx, root, token, urls, network, 4096)
	if err != nil {
		t.Fatal(err)
	}
	restored.close()
}
