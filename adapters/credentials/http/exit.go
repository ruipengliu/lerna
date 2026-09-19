// Package http binds one trusted HTTP exit to an exact service and
// account. Only an accepted bit escapes; raw target bodies and errors do not.
package http

import (
	"bytes"
	"context"
	"encoding/base64"
	"lerna/credentials"
	"net"
	"net/http"
	"net/url"
	"time"
)

type Config struct {
	Binding           credentials.Binding
	Path              string
	Timeout           time.Duration
	AllowLoopbackHTTP bool
}
type Exit struct {
	binding  credentials.Binding
	endpoint string
	client   *http.Client
}

func New(c Config) (*Exit, error) {
	if !c.Binding.Valid() || c.Timeout < time.Millisecond || c.Timeout > 5*time.Second {
		return nil, credentials.Invalid
	}
	u, e := url.Parse(c.Binding.Service)
	if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return nil, credentials.Invalid
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || !c.AllowLoopbackHTTP || ip == nil || !ip.IsLoopback() {
			return nil, credentials.Denied
		}
	}
	p, e := url.Parse(c.Path)
	if e != nil || p.IsAbs() || p.Host != "" || p.Path == "" || p.Path[0] != '/' || p.RawQuery != "" || p.Fragment != "" {
		return nil, credentials.Invalid
	}
	u.Path = p.Path
	u.RawPath = p.RawPath
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	return &Exit{c.Binding, u.String(), &http.Client{Transport: transport, Timeout: c.Timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (e *Exit) Close() { e.client.CloseIdleConnections() }
func (e *Exit) Send(ctx context.Context, call credentials.Call, secret []byte) (credentials.Result, error) {
	response, finish, err := e.request(ctx, http.MethodPost, call, secret)
	if err != nil {
		return credentials.Result{}, err
	}
	defer finish()
	defer response.Body.Close()
	if response.StatusCode != http.StatusNoContent {
		return credentials.Result{}, credentials.TargetUnavailable
	}
	return credentials.Result{Accepted: true}, nil
}

// request centralizes the secret-bearing request boundary for execution and
// renewal. Response interpretation remains in the specific trusted adapter.
func (e *Exit) request(ctx context.Context, method string, call credentials.Call, secret []byte) (*http.Response, func(), error) {
	noop := func() {}
	if len(secret) == 0 || len(secret) > 4096 || len(call.Payload) > 4096 || len(call.OperationID) == 0 || len(call.OperationID) > 256 {
		return nil, noop, credentials.Invalid
	}
	clean, cancel := context.WithCancel(context.Background())
	stop := context.AfterFunc(ctx, cancel)
	deadlineCancel := noop
	if deadline, ok := ctx.Deadline(); ok {
		clean, deadlineCancel = context.WithDeadline(clean, deadline)
	}
	finish := func() { stop(); deadlineCancel(); cancel() }
	if ctx.Err() != nil {
		finish()
		return nil, noop, credentials.TargetUnavailable
	}
	request, err := http.NewRequestWithContext(clean, method, e.endpoint, bytes.NewReader(call.Payload))
	if err != nil {
		finish()
		return nil, noop, credentials.TargetUnavailable
	}
	request.Header.Set("Authorization", "Bearer "+base64.RawStdEncoding.EncodeToString(secret))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", call.OperationID)
	request.Header.Set("X-Harness-Account", e.binding.Account)
	request.Header.Set("X-Harness-Purpose", e.binding.Purpose)
	request.Header.Set("X-Harness-Driver", e.binding.Driver)
	response, err := e.client.Do(request)
	if err != nil {
		finish()
		if response != nil && response.Body != nil {
			response.Body.Close()
		}
		return nil, noop, credentials.TargetUnavailable
	}
	return response, finish, nil
}

func (e *Exit) Target() credentials.Binding { return e.binding }
