// Package httpfetch acquires bounded response bytes from exact host-configured
// URLs. Fetch accepts no headers or credentials; PostJSON accepts a host-supplied
// bearer credential for one non-redirecting request. Neither uses proxy configuration.
package httpfetch

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"lerna/fetch"
)

// Config is supplied only by the trusted host. Both exact URLs and dialed
// address ranges must be allowed. Plain HTTP is restricted to explicit numeric
// loopback test endpoints, and also requires an allowed Networks entry.
type Config struct {
	Authority         fetch.Authority
	URLs              []string
	Networks          []netip.Prefix
	AllowLoopbackHTTP bool
}

type Adapter struct{ config Config }

var _ fetch.Fetcher = (*Adapter)(nil)

func New(c Config) (*Adapter, error) {
	if c.Authority == nil || len(c.URLs) == 0 || len(c.URLs) > 128 || len(c.Networks) == 0 || len(c.Networks) > 128 {
		return nil, fetch.Invalid
	}
	for _, p := range c.Networks {
		if !p.IsValid() || p != p.Masked() {
			return nil, fetch.Invalid
		}
	}
	for _, raw := range c.URLs {
		u, err := url.Parse(raw)
		if err != nil || len(raw) > 4096 || u.Host == "" || u.User != nil || u.Fragment != "" || u.Opaque != "" || u.ForceQuery {
			return nil, fetch.Invalid
		}
		if u.Scheme != "https" {
			ip, err := netip.ParseAddr(u.Hostname())
			if u.Scheme != "http" || !c.AllowLoopbackHTTP || err != nil || !ip.IsLoopback() {
				return nil, fetch.Denied
			}
		}
	}
	c.URLs = slices.Clone(c.URLs)
	c.Networks = slices.Clone(c.Networks)
	return &Adapter{c}, nil
}

func (a *Adapter) dial(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fetch.Denied
	}
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fetch.Unavailable
	}
	// Public CDN responses can exceed sixteen A/AAAA records. Bound the
	// work while still checking every returned address before dialing.
	if len(ips) == 0 || len(ips) > 64 {
		return nil, fetch.Denied
	}
	for _, ip := range ips {
		if ip.Zone() != "" {
			return nil, fetch.Denied
		}
		allowed := false
		for _, prefix := range a.config.Networks {
			if prefix.Contains(ip.Unmap()) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fetch.Denied
		}
	}
	// Recheck current request authority after resolution and before dialing.
	target, ok := ctx.Value(dialURLKey{}).(string)
	if !ok {
		return nil, fetch.Denied
	}
	if err = a.authorize(ctx, target, "request"); err != nil {
		return nil, err
	}
	// Dial the checked numeric address; do not resolve the hostname again.
	var d net.Dialer
	return d.DialContext(ctx, network, net.JoinHostPort(ips[0].Unmap().String(), port))
}

func (a *Adapter) Fetch(ctx context.Context, in fetch.Request) (fetch.Result, error) {
	return a.acquire(ctx, in, nil, "")
}

// PostJSON sends one host-authorized JSON request. The credential is supplied by
// the trusted host, never retained in evidence, and never forwarded on redirects.
// Task admission and budget reservation remain the caller's responsibility.
func (a *Adapter) PostJSON(ctx context.Context, in fetch.Request, body []byte, bearer string) (fetch.Result, error) {
	if len(body) == 0 || len(body) > 32768 || !json.Valid(body) || !utf8.Valid(body) || len(bearer) == 0 || len(bearer) > 4096 {
		return fetch.Result{}, fetch.Invalid
	}
	for _, c := range bearer {
		if c < 33 || c > 126 {
			return fetch.Result{}, fetch.Invalid
		}
	}
	return a.acquire(ctx, in, bytes.Clone(body), bearer)
}

func (a *Adapter) acquire(ctx context.Context, in fetch.Request, body []byte, bearer string) (fetch.Result, error) {
	if in.MaxBytes < 1 || in.MaxBytes > 1<<20 || in.MaxRequests < 1 || in.MaxRequests > 5 || in.Timeout < time.Millisecond || in.Timeout > 5*time.Second {
		return fetch.Result{}, fetch.Invalid
	}
	if !slices.Contains(a.config.URLs, in.URL) {
		return fetch.Result{}, fetch.Denied
	}
	ctx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()
	if ctx.Err() != nil {
		return fetch.Result{}, requestFailure(ctx, ctx.Err())
	}
	ctx, stopGuard := fetch.WatchGuard(ctx)
	defer stopGuard()
	transport := &http.Transport{DialContext: a.dial, DisableKeepAlives: true, DisableCompression: true, MaxResponseHeaderBytes: 32 << 10, TLSHandshakeTimeout: in.Timeout}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	attempts := fetch.Result{Mode: "http"}
	visited := make([]string, 0, in.MaxRequests)
	next := in.URL
	var response *http.Response
	for {
		if !slices.Contains(a.config.URLs, next) {
			return attempts, fetch.Denied
		}
		if attempts.Requests >= in.MaxRequests {
			return attempts, fetch.LimitExceeded
		}
		if ctx.Err() != nil {
			return attempts, requestFailure(ctx, ctx.Err())
		}
		if err := a.authorize(ctx, next, "request"); err != nil {
			return attempts, err
		}
		requestContext := context.WithValue(ctx, dialURLKey{}, next)
		method := http.MethodGet
		var input io.Reader
		if body != nil {
			method = http.MethodPost
			input = bytes.NewReader(body)
		}
		request, err := http.NewRequestWithContext(requestContext, method, next, input)
		if err != nil {
			return attempts, fetch.Invalid
		}
		request.Header.Set("Accept-Encoding", "identity")
		if body != nil {
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Authorization", "Bearer "+bearer)
			// A failed transport must not replay an authenticated body automatically.
			request.GetBody = nil
		}
		attempts.Requests++
		visited = append(visited, next)
		response, err = client.Do(request)
		if err != nil {
			if response != nil && response.Body != nil {
				response.Body.Close()
			}
			return attempts, requestFailure(ctx, err)
		}
		switch response.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther, http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
			if body != nil {
				response.Body.Close()
				return attempts, fetch.Denied
			}
			location, err := response.Location()
			response.Body.Close()
			if err != nil {
				return attempts, fetch.Unavailable
			}
			next = location.String()
			// Each hop gets a fresh request. Never propagate cookies, Authorization or
			// Referer from the previous target, including same-origin redirects.
			continue
		}
		break
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		return attempts, fetch.Denied
	case http.StatusGone:
		return attempts, fetch.Expired
	}
	if response.StatusCode != http.StatusOK {
		return attempts, fetch.Unavailable
	}
	if response.Header.Get("Content-Encoding") != "" && response.Header.Get("Content-Encoding") != "identity" {
		return attempts, fetch.Unsupported
	}
	mediaType, params, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil {
		return attempts, fetch.Unsupported
	}
	if mediaType != "text/plain" && mediaType != "text/html" && mediaType != "text/markdown" && mediaType != "application/json" {
		return attempts, fetch.Unsupported
	}
	charset := strings.ToLower(params["charset"])
	if charset != "" && charset != "utf-8" && charset != "us-ascii" {
		return attempts, fetch.Unsupported
	}
	if response.ContentLength > in.MaxBytes {
		return attempts, fetch.TooLarge
	}
	body, err = io.ReadAll(io.LimitReader(response.Body, in.MaxBytes+1))
	if err != nil {
		return attempts, requestFailure(ctx, err)
	}
	if int64(len(body)) > in.MaxBytes {
		return attempts, fetch.TooLarge
	}
	if ctx.Err() != nil {
		return attempts, requestFailure(ctx, ctx.Err())
	}
	// This reference adapter returns complete UTF-8 source bytes; it performs
	// no charset conversion, HTML execution, instruction following or JSON repair.
	if !utf8.Valid(body) || (mediaType == "application/json" && !json.Valid(body)) {
		return attempts, fetch.Unsupported
	}
	if charset == "us-ascii" {
		for _, b := range body {
			if b >= 128 {
				return attempts, fetch.Unsupported
			}
		}
	}
	for _, target := range visited {
		if err := a.authorize(ctx, target, "release"); err != nil {
			return attempts, err
		}
	}
	return fetch.Result{Mode: "http", RequestedURL: in.URL, FinalURL: response.Request.URL.String(), MediaType: mediaType, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), FetchedAt: time.Now().UTC(), HTTPStatus: response.StatusCode, Requests: attempts.Requests, Body: body, Sources: visited}, nil
}

// Only finite failure categories escape; transport errors may contain URLs.
func requestFailure(ctx context.Context, err error) error {
	if errors.Is(context.Cause(ctx), fetch.TimedOut) || errors.Is(err, fetch.TimedOut) {
		return fetch.TimedOut
	}
	if errors.Is(context.Cause(ctx), fetch.Denied) {
		return fetch.Denied
	}
	if errors.Is(err, fetch.Denied) {
		return fetch.Denied
	}
	if errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled) {
		return fetch.Cancelled
	}
	var networkError net.Error
	if errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &networkError) && networkError.Timeout()) {
		return fetch.TimedOut
	}
	return fetch.Unavailable
}

type dialURLKey struct{}

func (a *Adapter) authorize(ctx context.Context, url, phase string) error {
	if err := fetch.CheckGuard(ctx); err != nil {
		if errors.Is(err, fetch.TimedOut) {
			return fetch.TimedOut
		}
		if ctx.Err() != nil {
			return requestFailure(ctx, ctx.Err())
		}
		return fetch.Denied
	}
	if err := a.config.Authority.Check(ctx, url, phase); err != nil {
		if ctx.Err() != nil {
			return requestFailure(ctx, ctx.Err())
		}
		return fetch.Denied
	}
	if ctx.Err() != nil {
		return requestFailure(ctx, ctx.Err())
	}
	return nil
}
