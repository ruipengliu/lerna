// Package duckduckgo implements bounded first-page discovery using DuckDuckGo's
// public HTML interface. It neither solves challenges nor retries other routes.
package duckduckgo

import (
	"context"
	"lerna/fetch"
	"lerna/websearch"
	"net/netip"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"
)

const Endpoint = "https://html.duckduckgo.com/html/"

type Adapter struct {
	fetcher  fetch.Fetcher
	endpoint string
}

var _ websearch.Searcher = (*Adapter)(nil)

// New receives a governed transport. An alternate numeric loopback endpoint is
// allowed for protocol verification; the transport must separately authorize it.
func New(f fetch.Fetcher, endpoint string) (*Adapter, error) {
	u, err := url.Parse(endpoint)
	if f == nil || err != nil || u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || u.Opaque != "" || u.Path != "/html/" {
		return nil, fetch.Invalid
	}
	if endpoint != Endpoint {
		ip, err := netip.ParseAddr(u.Hostname())
		if err != nil || !ip.IsLoopback() || u.Scheme != "http" {
			return nil, fetch.Invalid
		}
	}
	return &Adapter{f, endpoint}, nil
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
	if err != nil {
		return failure(acquired), err
	}
	return decode(ctx, acquired, in.MaxResults, in.MaxBytes)
}

func failure(r fetch.Result) websearch.Result {
	return websearch.Result{Acquisition: fetch.Result{Mode: r.Mode, Requests: r.Requests}}
}
