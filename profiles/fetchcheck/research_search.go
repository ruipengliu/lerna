package fetchcheck

import (
	"context"
	"encoding/json"
	"html"
	researchcontext "lerna/adapters/research/context"
	"lerna/adapters/research/doubaosearch"
	"lerna/adapters/research/duckduckgo"
	"lerna/adapters/research/jsonsearch"
	"lerna/fetch"
	"lerna/tasks"
	"strings"
)

type retainedSearchEvidence interface {
	Read(context.Context, string) (fetch.Result, error)
}

// This reference host has a fixed action planner whose original search request
// always admits four results. It is not an arbitrary-request evidence decoder.
func (h *harness) searchEvidence(e retainedSearchEvidence) (researchcontext.SearchEvidence, error) {
	if h.searchFormat == "doubao" {
		return doubaosearch.NewEvidenceReader(e, 4)
	}
	if h.searchFormat == "duckduckgo-html" {
		return duckduckgo.NewEvidenceReader(e, 4)
	}
	return jsonsearch.NewEvidenceReader(e)
}
func bindConfiguredSearchHost(h *harness, endpoint string, limit uint32, queries *tasks.ActionPort, settings searchProviderConfig) (*fetchActionHost, error) {
	switch h.searchFormat {
	case "", "json":
		provider, err := jsonsearch.New(h.http, endpoint)
		if err != nil {
			return nil, err
		}
		return bindSearchProviderConfig(h, provider, limit, queries, settings)
	case "doubao":
		transport, ok := h.http.(doubaosearch.Transport)
		if !ok {
			return nil, fetch.Invalid
		}
		constructor := doubaosearch.New
		if h.answerFromSearch {
			constructor = doubaosearch.NewForSummaries
		}
		provider, err := constructor(transport, endpoint, doubaosearch.Credential(h.searchCredential))
		if err != nil {
			return nil, err
		}
		return bindSearchProviderConfig(h, provider, limit, queries, settings)
	case "duckduckgo-html":
		provider, err := duckduckgo.New(h.http, endpoint)
		if err != nil {
			return nil, err
		}
		return bindSearchProviderConfig(h, provider, limit, queries, settings)
	default:
		return nil, fetch.Invalid
	}
}

// Both live loopback and replay serialize the same preregistered sources.
// This is synthetic provider protocol material, never a public search result.
func encodeReferenceSearch(format string, candidates []map[string]string) ([]byte, string, error) {
	if format != "duckduckgo-html" {
		body, err := json.Marshal(map[string]any{"results": candidates})
		return body, "application/json", err
	}
	var out strings.Builder
	if len(candidates) == 0 {
		out.WriteString(`<div class="no-results__message">No results found</div>`)
	}
	for _, c := range candidates {
		out.WriteString(`<div class="result"><a class="result__a" href="`)
		out.WriteString(html.EscapeString(c["url"]))
		out.WriteString(`">`)
		out.WriteString(html.EscapeString(c["title"]))
		out.WriteString(`</a><span class="result__snippet">`)
		out.WriteString(html.EscapeString(c["snippet"]))
		out.WriteString(`</span></div>`)
	}
	return []byte(out.String()), "text/html", nil
}
