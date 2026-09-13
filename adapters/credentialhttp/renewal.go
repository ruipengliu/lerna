package credentialhttp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"io"
	"lerna/credentials"
	"lerna/internal/jsonvalue"
	"net/http"
	"time"
)

type RenewalConfig struct {
	Binding                            credentials.Binding
	ProviderID, RenewPath, InspectPath string
	Timeout                            time.Duration
	AllowLoopbackHTTP, ReplaySafe      bool
}
type Renewal struct {
	renew, inspect *Exit
	id             string
	replaySafe     bool
}

func NewRenewal(c RenewalConfig) (*Renewal, error) {
	if len(c.ProviderID) == 0 || len(c.ProviderID) > 64 {
		return nil, credentials.Invalid
	}
	for _, ch := range c.ProviderID {
		if ch < 33 || ch > 126 {
			return nil, credentials.Invalid
		}
	}
	renew, e := New(Config{Binding: c.Binding, Path: c.RenewPath, Timeout: c.Timeout, AllowLoopbackHTTP: c.AllowLoopbackHTTP})
	if e != nil {
		return nil, e
	}
	inspect, e := New(Config{Binding: c.Binding, Path: c.InspectPath, Timeout: c.Timeout, AllowLoopbackHTTP: c.AllowLoopbackHTTP})
	if e != nil {
		renew.Close()
		return nil, e
	}
	for _, exit := range []*Exit{renew, inspect} {
		// One HTTP/1 connection per logical request prevents the transport from
		// replaying a request on a failed reused connection outside our journal.
		transport := exit.client.Transport.(*http.Transport)
		transport.DisableKeepAlives = true
		transport.ForceAttemptHTTP2 = false
		transport.TLSNextProto = map[string]func(string, *tls.Conn) http.RoundTripper{}
	}
	identity, _ := json.Marshal(struct {
		Name, Renew, Inspect string
		Binding              credentials.Binding
		ReplaySafe           bool
	}{c.ProviderID, renew.endpoint, inspect.endpoint, c.Binding, c.ReplaySafe})
	hash := sha256.Sum256(identity)
	return &Renewal{renew: renew, inspect: inspect, id: hex.EncodeToString(hash[:]), replaySafe: c.ReplaySafe}, nil
}
func (p *Renewal) Close()                      { p.renew.Close(); p.inspect.Close() }
func (p *Renewal) ID() string                  { return p.id }
func (p *Renewal) Target() credentials.Binding { return p.renew.binding }
func (p *Renewal) ReplaySafe() bool            { return p.replaySafe }
func (p *Renewal) Renew(ctx context.Context, op string, secret []byte) error {
	_, e := p.renew.Send(ctx, credentials.Call{OperationID: op, Payload: []byte("{}")}, secret)
	return e
}
func (p *Renewal) Inspect(ctx context.Context, op string, secret []byte) (credentials.RenewalObservation, error) {
	response, finish, e := p.inspect.request(ctx, http.MethodGet, credentials.Call{OperationID: op}, secret)
	if e != nil {
		return credentials.RenewalObservation{}, e
	}
	defer finish()
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return credentials.RenewalObservation{}, credentials.TargetUnavailable
	}
	raw, e := io.ReadAll(io.LimitReader(response.Body, 16385))
	defer clear(raw)
	if e != nil || len(raw) > 16384 {
		return credentials.RenewalObservation{}, credentials.Invalid
	}
	if _, e = jsonvalue.Decode(raw); e != nil {
		return credentials.RenewalObservation{}, credentials.Invalid
	}
	var body struct {
		State       string `json:"state"`
		Secret      []byte `json:"secret"`
		ExpiresUnix int64  `json:"expires_unix"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&body); e != nil {
		clear(body.Secret)
		return credentials.RenewalObservation{}, credentials.Invalid
	}
	switch body.State {
	case "applied":
		if len(body.Secret) == 0 || len(body.Secret) > 4096 || body.ExpiresUnix <= 0 {
			clear(body.Secret)
			return credentials.RenewalObservation{}, credentials.Invalid
		}
	case "not_occurred", "unknown":
		if len(body.Secret) != 0 || body.ExpiresUnix != 0 {
			clear(body.Secret)
			return credentials.RenewalObservation{}, credentials.Invalid
		}
	default:
		clear(body.Secret)
		return credentials.RenewalObservation{}, credentials.Invalid
	}
	return credentials.RenewalObservation{State: body.State, Secret: body.Secret, ExpiresUnix: body.ExpiresUnix}, nil
}
