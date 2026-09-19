// Package jsonsearch binds a host-configured JSON search endpoint. It reuses a
// governed acquisition interface; it does not create a client or widen network
// authority. An injected fixed replay Fetcher remains explicitly fixed replay.
package jsonsearch

import (
	"context"
	"encoding/json"
	"lerna/fetch"
	"lerna/internal/jsonvalue"
	"lerna/websearch"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

type Adapter struct {
	fetcher  fetch.Fetcher
	endpoint string
}

var _ websearch.Searcher = (*Adapter)(nil)

func New(f fetch.Fetcher, endpoint string) (*Adapter, error) {
	u, err := url.Parse(endpoint)
	if f == nil || err != nil || !validURL(endpoint) || u.RawQuery != "" || u.ForceQuery {
		return nil, fetch.Invalid
	}
	return &Adapter{f, endpoint}, nil
}
func validURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && len(raw) > 0 && len(raw) <= 4096 && utf8.ValidString(raw) && u.Host != "" && u.Hostname() != "" && (u.Scheme == "https" || u.Scheme == "http") && u.User == nil && u.Fragment == "" && u.Opaque == "" && !u.ForceQuery
}
func (a *Adapter) Search(ctx context.Context, in websearch.Request) (websearch.Result, error) {
	if !utf8.ValidString(in.Query) || strings.TrimSpace(in.Query) == "" || len(in.Query) > 2048 || in.MaxResults < 1 || in.MaxResults > 16 || in.MaxBytes < 1 || in.MaxBytes > 1048576 || in.MaxRequests < 1 || in.MaxRequests > 5 || in.Timeout < time.Millisecond || in.Timeout > 5*time.Second {
		return websearch.Result{}, fetch.Invalid
	}
	target := a.endpoint + "?q=" + url.QueryEscape(in.Query)
	if len(target) > 4096 {
		return websearch.Result{}, fetch.Invalid
	}
	acquired, err := a.fetcher.Fetch(ctx, fetch.Request{URL: target, MaxBytes: in.MaxBytes, MaxRequests: in.MaxRequests, Timeout: in.Timeout})
	failure := websearch.Result{Acquisition: fetch.Result{Mode: acquired.Mode, Requests: acquired.Requests}}
	if err != nil {
		return failure, err
	}
	return decodeResponse(acquired, in.MaxResults, in.MaxBytes)
}

func decodeResponse(acquired fetch.Result, maxResults int, maxBytes int64) (websearch.Result, error) {
	failure := websearch.Result{Acquisition: fetch.Result{Mode: acquired.Mode, Requests: acquired.Requests}}
	if acquired.MediaType != "application/json" || int64(len(acquired.Body)) > maxBytes {
		return failure, fetch.Unsupported
	}
	value, err := jsonvalue.Decode(acquired.Body)
	m, ok := value.(map[string]any)
	if err != nil || !ok || len(m) != 1 {
		return failure, fetch.Unsupported
	}
	results, ok := m["results"].([]any)
	if !ok {
		return failure, fetch.Unsupported
	}
	if len(results) > maxResults {
		return failure, fetch.TooLarge
	}
	for _, v := range results {
		fields, ok := v.(map[string]any)
		if !ok || len(fields) != 3 {
			return failure, fetch.Unsupported
		}
		for _, k := range []string{"url", "title", "snippet"} {
			if _, ok := fields[k].(string); !ok {
				return failure, fetch.Unsupported
			}
		}
	}
	var decoded struct {
		Results []websearch.Candidate `json:"results"`
	}
	if json.Unmarshal(acquired.Body, &decoded) != nil {
		return failure, fetch.Unsupported
	}
	seen := map[string]bool{}
	for _, c := range decoded.Results {
		if !validURL(c.URL) || seen[c.URL] || strings.TrimSpace(c.Title) == "" || len(c.Title) > 1024 || len(c.Snippet) > 4096 {
			return failure, fetch.Unsupported
		}
		seen[c.URL] = true
	}
	return websearch.Result{Candidates: decoded.Results, Acquisition: acquired}, nil
}
