package fetchcheck_test

import (
	"context"
	"encoding/json"
	"fmt"
	"html"
	contentpolicy "lerna/adapters/content/policy"
	sqlitecontentpolicy "lerna/adapters/content/sqlitepolicy"
	"lerna/brain"
	"lerna/profiles/fetchcheck"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type runtimeAnswerModel struct {
	expectedOutput uint64
	calls          int
	failureStatus  string
	delay          time.Duration
}

func (m *runtimeAnswerModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "runtime-protocol", Version: "1", Location: "external-provider", Text: true, Structured: true, HardBounds: true, InputUpper: 224 * 1024, ContextTokens: 256 * 1024}
}
func (m *runtimeAnswerModel) Generate(ctx context.Context, request brain.Request) (brain.Result, error) {
	m.calls++
	if m.delay > 0 {
		timer := time.NewTimer(m.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return brain.Result{}, ctx.Err()
		case <-timer.C:
		}
	}
	expected := m.expectedOutput
	if expected == 0 {
		expected = 512
	}
	if request.MaxInput != 224*1024 || request.MaxOutput != expected {
		return brain.Result{}, fmt.Errorf("wrong model reservation")
	}
	block := request.Input.Blocks[0]
	for _, candidate := range request.Input.Blocks {
		if candidate.Role == "external-evidence" || candidate.Role == "external-search-evidence" || candidate.Role == "external-evidence-gap" {
			block = candidate
			break
		}
	}
	if block.Role == "search-candidates" {
		raw, _ := json.Marshal(brain.EvidenceAnswer{Status: "insufficient", Text: "No provider summary was returned.", Scope: "Search response only; no page was requested.", Claims: []brain.EvidenceClaim{}, Gaps: []brain.EvidenceGap{{Kind: "insufficient", Detail: "Summary unavailable.", Sources: []string{block.Ref}}}})
		return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 100}}, nil
	}
	if block.Role == "external-evidence-gap" {
		var failure struct{ Status string }
		if err := json.Unmarshal([]byte(block.Text), &failure); err != nil {
			return brain.Result{}, err
		}
		m.failureStatus = failure.Status
		raw, _ := json.Marshal(brain.EvidenceAnswer{Status: "fetch_failed", Text: "The page was not acquired.", Scope: "This attempt only.", Claims: []brain.EvidenceClaim{}, Gaps: []brain.EvidenceGap{{Kind: "fetch_failed", Detail: failure.Status, Sources: []string{block.Ref}}}})
		return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 100}}, nil
	}

	var page struct{ Body, SHA256, FetchedAt string }
	if err := json.Unmarshal([]byte(block.Text), &page); err != nil {
		return brain.Result{}, err
	}
	raw, _ := json.Marshal(brain.EvidenceAnswer{Status: "answerable", Text: "The record says 2001.", Scope: "The supplied public record.", Gaps: []brain.EvidenceGap{}, Claims: []brain.EvidenceClaim{{Text: "The record says 2001.", Citations: []brain.EvidenceCitation{{Source: block.Ref, Start: 0, End: len(page.Body), Quote: page.Body, SHA256: page.SHA256, FetchedAt: page.FetchedAt}}}}})
	outputUsed := uint64(100)
	if m.expectedOutput > 512 {
		outputUsed = 600
	}
	return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: outputUsed}}, nil
}

func TestConfiguredResearchRunsThroughPublicEntry(t *testing.T) {
	for _, format := range []string{"json", "duckduckgo-html"} {
		t.Run(format, func(t *testing.T) { checkConfiguredResearch(t, format, 0) })
	}
}
func TestDoubaoSummaryRespectsResultCount(t *testing.T) {
	checkConfiguredResearchMode(t, "doubao", 0, "search-summary-two")
}

func TestDoubaoMissingSummaryDoesNotUseSnippet(t *testing.T) {
	checkConfiguredResearchMode(t, "doubao", 0, "search-summary-missing")
}
func TestDoubaoSummaryAnswerResumesWithoutPages(t *testing.T) {
	checkConfiguredResearchMode(t, "doubao", 0, "search-summary-resume")
}

func TestDoubaoSummaryAnswerDoesNotFetchPages(t *testing.T) {
	checkConfiguredResearchMode(t, "doubao", 0, "search-summary")
}

func TestConfiguredResearchResumesLargerOutputBudget(t *testing.T) {
	checkConfiguredResearchMode(t, "doubao", 0, "large-output")
}

func TestDoubaoResearchAllowsBoundedSearchLatency(t *testing.T) {
	checkConfiguredResearchMode(t, "doubao", 0, "slow-search")
}

func TestDoubaoResearchRunsThroughPublicEntry(t *testing.T) { checkConfiguredResearch(t, "doubao", 0) }

func TestDoubaoResearchResumesBeforeAnswer(t *testing.T) {
	checkConfiguredResearchMode(t, "doubao", 0, "resume")
}

func TestConfiguredResearchUsesPageBudget(t *testing.T) {
	checkConfiguredResearch(t, "duckduckgo-html", 4096)
}
func TestConfiguredResearchEnforcesPageBudget(t *testing.T) {
	checkConfiguredResearch(t, "duckduckgo-html", 32)
}
func TestConfiguredResearchRenewsModelLease(t *testing.T) {
	checkConfiguredResearch(t, "json", 0, 11*time.Second)
}

type interruptBeforeAnswer struct {
	*runtimeAnswerModel
	cancel   context.CancelFunc
	capCalls int
}

func (m *interruptBeforeAnswer) Capabilities() brain.Capabilities {
	m.capCalls++
	if m.capCalls == 2 {
		m.cancel()
	}
	return m.runtimeAnswerModel.Capabilities()
}
func TestConfiguredResearchResumesBeforeAnswer(t *testing.T) {
	for _, mode := range []string{"resume", "revoked"} {
		t.Run(mode, func(t *testing.T) { checkConfiguredResearchMode(t, "duckduckgo-html", 0, mode) })
	}
}
func checkConfiguredResearch(t *testing.T, format string, pageBytes uint32, delay ...time.Duration) {
	checkConfiguredResearchMode(t, format, pageBytes, "", delay...)
}
func checkConfiguredResearchMode(t *testing.T, format string, pageBytes uint32, resume string, delay ...time.Duration) {
	body := "This public record opened in 2001."
	wantPages := int32(1)
	wantNetwork := uint64(2)
	if strings.HasPrefix(resume, "search-summary") {
		wantPages = 0
		wantNetwork = 1
	}
	if pageBytes != 0 {
		body += strings.Repeat(".", 2048)
	}
	path := "/search"
	if format == "doubao" {
		path = "/search_api/web_search"
	}
	if format == "duckduckgo-html" {
		path = "/html/"
	}
	var endpoint string
	var searches, pages atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == path {
			searches.Add(1)
			if resume == "slow-search" {
				time.Sleep(1200 * time.Millisecond)
			}
			if format == "doubao" {
				if r.Method != "POST" || r.Header.Get("Authorization") != "Bearer test-search-key" {
					t.Error("invalid search authentication")
				}
				expectedCount := 4
				if resume == "search-summary-two" {
					expectedCount = 2
				}
				var input struct {
					Query       string
					Count       int
					NeedSummary bool
				}
				if json.NewDecoder(r.Body).Decode(&input) != nil || input.Query != "record" || input.Count != expectedCount || input.NeedSummary != (strings.HasPrefix(resume, "search-summary")) {
					t.Error("invalid search payload")
				}
				w.Header().Set("Content-Type", "application/json")
				if resume == "search-summary-missing" {
					fmt.Fprintf(w, `{"ResponseMetadata":{},"Result":{"ResultCount":1,"WebResults":[{"Url":%q,"Title":"Record","Snippet":"An unverified snippet says 1999."}]}}`, endpoint+"/page")
				} else {
					fmt.Fprintf(w, `{"ResponseMetadata":{},"Result":{"ResultCount":1,"WebResults":[{"Url":%q,"Title":"Record","Snippet":"An unverified snippet says 1999.","Summary":"This public record opened in 2001."}]}}`, endpoint+"/page")
				}
			} else if format == "duckduckgo-html" {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprintf(w, `<div class="result"><a class="result__a" href="%s">Record</a><span class="result__snippet">Read the record</span></div>`, html.EscapeString(endpoint+"/page"))
			} else {
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"results":[{"url":%q,"title":"Record","snippet":"Read the record"}]}`, endpoint+"/page")
			}
		} else {
			pages.Add(1)
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(body))
		}
	}))
	defer server.Close()
	endpoint = server.URL
	model := &runtimeAnswerModel{}
	if len(delay) > 0 {
		model.delay = delay[0]
	}
	config := fetchcheck.RuntimeConfig{Goal: "When did this record open?", Query: "record", SearchEndpoint: endpoint + path, SearchFormat: format, SearchRecipient: "external-provider", SearchMaxBytes: 4096, PageMaxBytes: pageBytes, URLs: []string{endpoint + path + "?q=record", endpoint + "/page"}, Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true, DiscloseTo: []string{"external-provider"}, MaxQueries: 128, NetworkLimit: 2, MaxSteps: 3, ModelTokens: 256 * 1024}
	if pageBytes != 0 {
		config.NetworkLimit = 3
	}
	var credentials []fetchcheck.SearchCredential
	if format == "doubao" {
		config.URLs[0] = endpoint + path
		credentials = append(credentials, func(context.Context) (string, error) { return "test-search-key", nil })
	}
	if strings.HasPrefix(resume, "search-summary") {
		if resume == "search-summary-two" {
			config.SearchMaxResults = 2
		}
		config.AnswerFromSearch = true
		config.URLs = config.URLs[:1]
	}
	if resume == "large-output" {
		config.AnswerOutputTokens = 4096
		model.expectedOutput = 4096
	}
	if resume == "slow-search" {
		config.SearchTimeoutMS = 3000
	}
	root := t.TempDir()
	if resume != "" && resume != "slow-search" && (!strings.HasPrefix(resume, "search-summary") || resume == "search-summary-resume") {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		interrupted := &interruptBeforeAnswer{runtimeAnswerModel: model, cancel: cancel}
		if _, err := fetchcheck.RunResearch(ctx, root, config, interrupted, credentials...); err == nil || model.calls != 0 || searches.Load() != 1 || pages.Load() != wantPages {
			t.Fatalf("did not interrupt between acquisition and answer: %v", err)
		}
		before, err := fetchcheck.QueryResearch(context.Background(), root)
		if err != nil || before.Task.State != "RUNNING" || before.Availability != "not_published" {
			t.Fatalf("missing original unfinished task: %v", err)
		}
		if _, err := fetchcheck.ResumeResearch(context.Background(), root, differentRuntimeModel{model}); err == nil || model.calls != 0 {
			t.Fatal("changed model contract was accepted")
		}
		if resume == "revoked" {
			revokeRuntimeSources(t, root)
			view, err := fetchcheck.ResumeResearch(context.Background(), root, model)
			if err == nil || len(view.Body) != 0 || model.calls != 0 || searches.Load() != 1 || pages.Load() != wantPages {
				t.Fatal("revoked source reached a resumed model or answer")
			}
			return
		}
		after, err := fetchcheck.ResumeResearch(context.Background(), root, model)
		if err != nil {
			t.Fatal(err)
		}
		if resume == "large-output" && after.Task.ModelUsedTokens != before.Task.ModelUsedTokens+700 {
			t.Fatal("larger output usage was not settled on original task")
		}
		var answer brain.EvidenceAnswer
		if json.Unmarshal(after.Body, &answer) != nil || answer.Status != "answerable" || len(answer.Claims) != 1 || answer.Claims[0].Citations[0].Quote != body || after.Task.Ref != before.Task.Ref || after.Task.ModelUsedRequests != before.Task.ModelUsedRequests+1 || model.calls != 1 || searches.Load() != 1 || pages.Load() != wantPages {
			t.Fatal("resume replaced original task, work or evidence")
		}
		repeated, err := fetchcheck.ResumeResearch(context.Background(), root, model)
		if err != nil || !reflect.DeepEqual(repeated.Body, after.Body) || model.calls != 1 || searches.Load() != 1 || pages.Load() != wantPages {
			t.Fatalf("completed resume repeated work: %v", err)
		}
		return
	}
	denied := config
	denied.DiscloseTo = nil
	if _, err := fetchcheck.RunResearch(context.Background(), root, denied, model, credentials...); err == nil || model.calls != 0 || searches.Load() != 0 || pages.Load() != 0 {
		t.Fatal("missing disclosure permission performed external work")
	}
	record, err := fetchcheck.RunResearch(context.Background(), root, config, model, credentials...)
	if err != nil {
		t.Fatal(err)
	}
	if resume == "search-summary-missing" {
		if record.Answer.Status != "insufficient" || len(record.Answer.Claims) != 0 || pages.Load() != 0 || searches.Load() != 1 {
			t.Fatal("missing summary promoted snippet or fetched page")
		}
		return
	}
	if pageBytes == 32 {
		if model.calls != 1 || searches.Load() != 1 || pages.Load() != wantPages || model.failureStatus != "too_large" || record.Answer.Status != "fetch_failed" || len(record.Answer.Claims) != 0 || record.Usage.NetworkCharged != wantNetwork {
			t.Fatalf("page limit lost: %+v failure=%s", record, model.failureStatus)
		}
		return
	}
	if model.calls != 1 || searches.Load() != 1 || pages.Load() != wantPages || record.Answer.Status != "answerable" || len(record.Answer.Claims) != 1 || record.Answer.Claims[0].Citations[0].Quote != body || record.Usage.NetworkCharged != wantNetwork {
		t.Fatalf("runtime bypassed evidence or budget: %+v", record)
	}
	view, err := fetchcheck.QueryResearch(context.Background(), root)
	if err != nil {
		t.Fatal(err)
	}
	var restored brain.EvidenceAnswer
	if err := json.Unmarshal(view.Body, &restored); err != nil || !reflect.DeepEqual(restored, record.Answer) || view.Task.State != "COMPLETED" || view.Availability != "available" || view.Delivered {
		t.Fatalf("reopened answer differs: %+v, %v", view, err)
	}
	if model.calls != 1 || searches.Load() != 1 || pages.Load() != wantPages {
		t.Fatal("query repeated external work")
	}
	revokeRuntimeSources(t, root)
	view, err = fetchcheck.QueryResearch(context.Background(), root)
	if len(view.Body) != 0 || err == nil && view.Availability == "available" {
		t.Fatal("reopened query disclosed an answer after source-policy revocation")
	}
}

func revokeRuntimeSources(t *testing.T, root string) {
	t.Helper()
	policyStore, err := sqlitecontentpolicy.Open(filepath.Join(root, "source-policy.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer policyStore.Close()
	policy, err := contentpolicy.OpenPersistent(context.Background(), policyStore)
	if err != nil {
		t.Fatal(err)
	}
	if err := policy.Replace([]contentpolicy.Rule{}); err != nil {
		t.Fatal(err)
	}
}

type differentRuntimeModel struct{ brain.Model }

func (m differentRuntimeModel) Capabilities() brain.Capabilities {
	c := m.Model.Capabilities()
	c.Version = "different"
	return c
}
