// Package doubaosearch discovers candidates via the Doubao search API. Returned
// content is discovery data, never independently acquired page evidence.
package doubaosearch

import (
	"context"
	"encoding/json"
	"lerna/fetch"
	"lerna/internal/jsonvalue"
	"lerna/websearch"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const Endpoint = "https://open.feedcoopapi.com/search_api/web_search"

type Transport interface {
	PostJSON(context.Context, fetch.Request, []byte, string) (fetch.Result, error)
}
type Credential func(context.Context) (string, error)
type Adapter struct {
	transport  Transport
	endpoint   string
	credential Credential
	summaries  bool
}

func New(t Transport, endpoint string, key Credential) (*Adapter, error) {
	u, err := url.Parse(endpoint)
	if t == nil || key == nil || err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.Path != "/search_api/web_search" {
		return nil, fetch.Invalid
	}
	if endpoint != Endpoint {
		ip, e := netip.ParseAddr(u.Hostname())
		if e != nil || !ip.IsLoopback() || u.Scheme != "http" {
			return nil, fetch.Denied
		}
	}
	return &Adapter{t, endpoint, key, false}, nil
}

// NewForSummaries requests provider summaries, retained with the original response.
func NewForSummaries(t Transport, endpoint string, key Credential) (*Adapter, error) {
	a, err := New(t, endpoint, key)
	if err == nil {
		a.summaries = true
	}
	return a, err
}
func (a *Adapter) Search(ctx context.Context, in websearch.Request) (websearch.Result, error) {
	if !utf8.ValidString(in.Query) || strings.TrimSpace(in.Query) == "" || utf8.RuneCountInString(in.Query) > 100 || in.MaxResults < 1 || in.MaxResults > 16 || in.MaxBytes < 1 || in.MaxBytes > 1048576 || in.MaxRequests < 1 || in.MaxRequests > 5 || in.Timeout < time.Millisecond || in.Timeout > 5*time.Second {
		return websearch.Result{}, fetch.Invalid
	}
	if err := fetch.CheckGuard(ctx); err != nil {
		return websearch.Result{}, err
	}
	key, err := a.credential(ctx)
	if err != nil {
		return websearch.Result{}, fetch.Denied
	}
	payload := map[string]any{"Query": in.Query, "SearchType": "web", "Count": in.MaxResults, "Filter": map[string]bool{"NeedContent": false, "NeedUrl": true}}
	if a.summaries {
		payload["NeedSummary"] = true
	}
	body, _ := json.Marshal(payload)
	acquired, err := a.transport.PostJSON(ctx, fetch.Request{URL: a.endpoint, MaxBytes: in.MaxBytes, MaxRequests: in.MaxRequests, Timeout: in.Timeout}, body, key)
	if err != nil {
		return failure(acquired), err
	}
	return decode(acquired, in.MaxResults)
}
func failure(r fetch.Result) websearch.Result {
	return websearch.Result{Acquisition: fetch.Result{Mode: r.Mode, Requests: r.Requests}}
}
func decode(r fetch.Result, limit int) (websearch.Result, error) {
	failed := failure(r)
	if r.MediaType != "application/json" || len(r.Body) > 1048576 {
		return failed, fetch.Unsupported
	}
	value, err := jsonvalue.Decode(r.Body)
	m, ok := value.(map[string]any)
	if err != nil || !ok {
		return failed, fetch.Unsupported
	}
	meta, ok := m["ResponseMetadata"].(map[string]any)
	if !ok || meta["Error"] != nil {
		return failed, fetch.Unavailable
	}
	result, ok := m["Result"].(map[string]any)
	if !ok {
		return failed, fetch.Unsupported
	}
	rows, ok := result["WebResults"].([]any)
	if !ok {
		return failed, fetch.Unsupported
	}
	if len(rows) > limit {
		return failed, fetch.TooLarge
	}
	candidates := make([]websearch.Candidate, 0, len(rows))
	seen := map[string]bool{}
	for _, row := range rows {
		fields, ok := row.(map[string]any)
		if !ok {
			return failed, fetch.Unsupported
		}
		raw, okURL := fields["Url"].(string)
		title, okTitle := fields["Title"].(string)
		snippet, okSnippet := fields["Snippet"].(string)
		summary := ""
		if v, exists := fields["Summary"]; exists {
			var valid bool
			summary, valid = v.(string)
			if !valid || len(summary) > 16384 {
				return failed, fetch.Unsupported
			}
		}
		u, e := url.Parse(raw)
		if !okURL || !okTitle || !okSnippet || e != nil || len(raw) > 4096 || u.Hostname() == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.ForceQuery || seen[raw] || strings.TrimSpace(title) == "" || len(title) > 1024 || len(snippet) > 4096 {
			return failed, fetch.Unsupported
		}
		seen[raw] = true
		candidates = append(candidates, websearch.Candidate{URL: raw, Title: title, Snippet: snippet, Summary: summary})
	}
	return websearch.Result{Candidates: candidates, Acquisition: r}, nil
}

type Evidence interface {
	Read(context.Context, string) (fetch.Result, error)
}
type EvidenceReader struct {
	evidence Evidence
	limit    int
}

func NewEvidenceReader(e Evidence, limit int) (*EvidenceReader, error) {
	if e == nil || limit < 1 || limit > 16 {
		return nil, fetch.Invalid
	}
	return &EvidenceReader{e, limit}, nil
}
func (r *EvidenceReader) Read(ctx context.Context, ref string) (websearch.Result, error) {
	a, e := r.evidence.Read(ctx, ref)
	if e != nil {
		return websearch.Result{}, e
	}
	return decode(a, r.limit)
}
