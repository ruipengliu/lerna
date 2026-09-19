package duckduckgo

import (
	"bytes"
	"context"
	"lerna/fetch"
	"lerna/websearch"
	"net/url"
	"strings"
	"unicode/utf8"

	"golang.org/x/net/html"
)

func attribute(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}
func class(n *html.Node, value string) bool {
	for _, c := range strings.Fields(attribute(n, "class")) {
		if c == value {
			return true
		}
	}
	return false
}
func text(n *html.Node) string {
	var b strings.Builder
	for child := range n.Descendants() {
		if child.Type == html.TextNode && child.Parent.Data != "script" && child.Parent.Data != "style" {
			b.WriteString(child.Data)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
func candidateURL(raw string) (string, error) {
	if strings.HasPrefix(raw, "//") {
		raw = "https:" + raw
	}
	if strings.HasPrefix(raw, "/l/?") {
		raw = "https://duckduckgo.com" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.User != nil {
		return "", fetch.Unsupported
	}
	if (u.Hostname() == "duckduckgo.com" || u.Hostname() == "html.duckduckgo.com") && u.Path == "/l/" {
		q, err := url.ParseQuery(u.RawQuery)
		if err != nil || len(q["uddg"]) != 1 {
			return "", fetch.Unsupported
		}
		raw = q.Get("uddg")
		u, err = url.Parse(raw)
		if err != nil {
			return "", fetch.Unsupported
		}
	}
	if len(raw) > 4096 || !utf8.ValidString(raw) || u.Hostname() == "" || u.User != nil || u.Opaque != "" || u.Fragment != "" || (u.Scheme != "https" && u.Scheme != "http") {
		return "", fetch.Unsupported
	}
	return raw, nil
}

func decode(ctx context.Context, acquired fetch.Result, limit int, maxBytes int64) (websearch.Result, error) {
	bad := failure(acquired)
	if acquired.MediaType != "text/html" || int64(len(acquired.Body)) > maxBytes || !utf8.Valid(acquired.Body) {
		return bad, fetch.Unsupported
	}
	doc, err := html.Parse(bytes.NewReader(acquired.Body))
	if err != nil {
		return bad, fetch.Unsupported
	}
	results := []websearch.Candidate{}
	seen := map[string]bool{}
	empty := false
	blocks := []*html.Node{}
	for n := range doc.Descendants() {
		if err := ctx.Err(); err != nil {
			return bad, fetch.Cancelled
		}
		if n.Type != html.ElementNode {
			continue
		}
		if attribute(n, "id") == "challenge-form" || strings.HasPrefix(attribute(n, "class"), "anomaly-modal") {
			return bad, fetch.Denied
		}
		if class(n, "no-results") || class(n, "no-results__message") {
			empty = true
		}
		if !class(n, "result") || class(n, "result--ad") {
			continue
		}
		if len(blocks) >= 128 {
			return bad, fetch.TooLarge
		}
		blocks = append(blocks, n)
	}
	for _, n := range blocks {
		if err := ctx.Err(); err != nil {
			return bad, fetch.Cancelled
		}
		var candidate websearch.Candidate
		for child := range n.Descendants() {
			if child.Type != html.ElementNode {
				continue
			}
			if child.Data == "a" && class(child, "result__a") {
				if candidate.URL != "" {
					return bad, fetch.Unsupported
				}
				candidate.URL, err = candidateURL(attribute(child, "href"))
				if err != nil {
					return bad, err
				}
				candidate.Title = text(child)
			}
			if class(child, "result__snippet") {
				candidate.Snippet = text(child)
			}
		}
		if empty && candidate.URL == "" && candidate.Title == "" {
			continue
		}
		if candidate.URL == "" || candidate.Title == "" {
			return bad, fetch.Unsupported
		}
		if len(candidate.Title) > 1024 || len(candidate.Snippet) > 4096 {
			return bad, fetch.TooLarge
		}
		if !seen[candidate.URL] && len(results) < limit {
			results = append(results, candidate)
			seen[candidate.URL] = true
		}
	}
	// An unfamiliar/challenge page is never evidence that a query had no hits.
	if len(results) == 0 && !empty || len(results) > 0 && empty {
		return bad, fetch.Unsupported
	}
	return websearch.Result{Candidates: results, Acquisition: acquired}, nil
}
