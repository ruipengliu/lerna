package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/ruipengliu/lerna/api"
)

type Limits struct {
	MaxDomainBytes uint64 `json:"max_domain_bytes"`
	MaxFrameBytes  uint64 `json:"max_frame_bytes"`
	MaxPending     uint64 `json:"max_pending"`
}
type Discovery struct {
	Protocol         string               `json:"protocol"`
	Profile          string               `json:"profile"`
	LogicalServiceID string               `json:"logical_service_id"`
	SchemaDigest     string               `json:"schema_digest"`
	CoreSchemaPath   string               `json:"core_schema_path"`
	Methods          []api.MethodContract `json:"methods"`
	MethodsDigest    string               `json:"methods_digest"`
	Limits           Limits               `json:"limits"`
	IdentityScope    string               `json:"identity_scope"`
	IdentityRevision uint64               `json:"identity_revision"`
}
type Transport interface {
	Call(context.Context, string, json.RawMessage) (json.RawMessage, error)
}
type HTTPTransport struct {
	BaseURL               string
	Token                 string
	HTTP                  *http.Client
	AllowInsecureLoopback bool
}
type callFrame struct {
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}
type resultFrame struct {
	ResultKind string          `json:"result_kind"`
	Payload    json.RawMessage `json:"payload"`
}

func loopback(u *url.URL) bool {
	return u.Hostname() == "127.0.0.1" || u.Hostname() == "localhost" || u.Hostname() == "::1"
}
func (t *HTTPTransport) request(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	u, e := url.Parse(t.BaseURL)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return nil, api.E("invalid_request", "invalid_endpoint")
	}
	if u.Scheme != "https" && !(t.AllowInsecureLoopback && u.Scheme == "http" && loopback(u)) {
		return nil, api.E("forbidden", "tls_required")
	}
	if strings.HasPrefix(path, "//") || !strings.HasPrefix(path, "/") {
		return nil, api.E("invalid_request", "invalid_endpoint_path")
	}
	u.Path = strings.TrimRight(u.Path, "/") + path
	req, e := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(body))
	if e != nil {
		return nil, e
	}
	req.Header.Set("Authorization", "Bearer "+t.Token)
	req.Header.Set("Content-Type", "application/json")
	client := t.HTTP
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	}
	response, e := client.Do(req)
	if e != nil {
		return nil, e
	}
	defer response.Body.Close()
	b, e := io.ReadAll(io.LimitReader(response.Body, 1<<20+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 1<<20 {
		return nil, api.E("invalid_request", "response_too_large")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var business api.Error
		if e = api.Decode(b, &business); e == nil && business.Code != "" {
			return nil, &business
		}
		return nil, api.E("dependency_unavailable", "http_call_failed")
	}
	return b, nil
}
func (t *HTTPTransport) Call(ctx context.Context, kind string, payload json.RawMessage) (json.RawMessage, error) {
	b, e := t.request(ctx, "POST", "/api/call", api.Raw(callFrame{kind, payload}))
	if e != nil {
		return nil, e
	}
	var result resultFrame
	if e = api.Decode(b, &result); e != nil {
		return nil, e
	}
	if result.ResultKind == "error" {
		var problem api.Error
		if e = api.Decode(result.Payload, &problem); e != nil {
			return nil, e
		}
		return nil, &problem
	}
	return result.Payload, nil
}
func (t *HTTPTransport) Discover(ctx context.Context) (Discovery, error) {
	b, e := t.request(ctx, "GET", "/api/discovery", nil)
	if e != nil {
		return Discovery{}, e
	}
	var d Discovery
	if e = api.Decode(b, &d); e != nil {
		return d, e
	}
	hash, e := api.Digest(d.Methods)
	if e != nil || hash != d.MethodsDigest || d.Protocol != api.Protocol || d.Profile != api.Profile || d.IdentityScope == "" {
		return d, api.E("unsupported", "discovery_mismatch")
	}
	core, e := t.request(ctx, "GET", d.CoreSchemaPath, nil)
	if e != nil {
		return d, e
	}
	if api.Hash(core) != d.SchemaDigest || d.SchemaDigest != api.CoreDigest() {
		return d, api.E("unsupported", "schema_mismatch")
	}
	return d, nil
}

type Client struct {
	Transport Transport
	Journal   Journal
	Discovery Discovery
	inputs    map[string]*api.Validator
	outputs   map[string]*api.Validator
}

func NewClient(t Transport, j Journal, d Discovery) (*Client, error) {
	if t == nil || j == nil || d.IdentityScope == "" || !api.ValidID(d.LogicalServiceID) {
		return nil, api.E("invalid_request", "client_configuration_missing")
	}
	c := &Client{t, j, d, map[string]*api.Validator{}, map[string]*api.Validator{}}
	for _, m := range d.Methods {
		digest, e := api.Digest([]any{m.InputSchema, m.OutputSchema})
		if e != nil || digest != m.SchemaDigest {
			return nil, api.E("unsupported", "method_schema_mismatch")
		}
		v, e := api.NewValidator(m.InputSchema)
		if e != nil {
			return nil, e
		}
		c.inputs[m.Name] = v
		v, e = api.NewValidator(m.OutputSchema)
		if e != nil {
			return nil, e
		}
		c.outputs[m.Name] = v
	}
	return c, nil
}
func (c *Client) Send(ctx context.Context, command api.Command) (api.Receipt, error) {
	if command.LogicalServiceID != c.Discovery.LogicalServiceID || command.Protocol != api.Protocol || command.Profile != api.Profile {
		return api.Receipt{}, api.E("invalid_request", "command_owner_mismatch")
	}
	v, ok := c.inputs[command.Method]
	if !ok {
		return api.Receipt{}, api.E("unsupported", "method_not_supported")
	}
	if e := v.Validate(command.Payload); e != nil {
		return api.Receipt{}, e
	}
	digest, e := api.Digest(command)
	if e != nil {
		return api.Receipt{}, e
	}
	entry := Entry{IdentityScope: c.Discovery.IdentityScope, SchemaDigest: c.Discovery.SchemaDigest, Command: command, Digest: digest}
	if e = c.Journal.Save(ctx, entry); e != nil {
		return api.Receipt{}, e
	}
	return c.sendEntry(ctx, entry)
}
func (c *Client) sendEntry(ctx context.Context, entry Entry) (api.Receipt, error) {
	raw, e := c.Transport.Call(ctx, "command", api.Raw(entry.Command))
	if e != nil {
		return api.Receipt{}, e
	}
	return c.saveReceipt(ctx, entry, raw)
}
func (c *Client) saveReceipt(ctx context.Context, entry Entry, raw json.RawMessage) (api.Receipt, error) {
	var receipt api.Receipt
	if e := api.Decode(raw, &receipt); e != nil {
		return receipt, e
	}
	if receipt.CommandID != entry.Command.CommandID || receipt.RequestDigest != entry.Digest || receipt.Stage != "accepted" && receipt.Stage != "applied" && receipt.Stage != "rejected" {
		return receipt, api.E("invalid_request", "receipt_identity_mismatch")
	}
	if receipt.Stage == "rejected" {
		if receipt.Error == nil || len(receipt.Output) != 0 {
			return receipt, api.E("invalid_request", "invalid_receipt")
		}
	} else {
		if receipt.Error != nil || len(receipt.Output) == 0 {
			return receipt, api.E("invalid_request", "invalid_receipt")
		}
		v, ok := c.outputs[entry.Command.Method]
		if !ok {
			return receipt, api.E("unsupported", "original_decoder_unavailable")
		}
		if e := v.Validate(receipt.Output); e != nil {
			return receipt, e
		}
	}
	entry.Receipt = &receipt
	if e := c.Journal.Save(ctx, entry); e != nil {
		return receipt, e
	}
	return receipt, nil
}
func (c *Client) Recover(ctx context.Context) ([]api.Receipt, bool, error) {
	entries, partial, e := c.Journal.Pending(ctx, 128)
	if e != nil {
		return nil, partial, e
	}
	results := []api.Receipt{}
	for _, entry := range entries {
		if entry.IdentityScope != c.Discovery.IdentityScope || entry.Command.LogicalServiceID != c.Discovery.LogicalServiceID {
			return nil, partial, api.E("forbidden", "recovery_scope_mismatch")
		}
		raw, e := c.Transport.Call(ctx, "receipt_lookup", api.Raw(api.ReceiptLookup{LogicalServiceID: entry.Command.LogicalServiceID, CommandID: entry.Command.CommandID}))
		var r api.Receipt
		if api.IsCode(e, "not_found") {
			r, e = c.sendEntry(ctx, entry)
		} else if e == nil {
			r, e = c.saveReceipt(ctx, entry, raw)
		}
		if e != nil {
			return results, partial, e
		}
		results = append(results, r)
	}
	return results, partial, nil
}
func (c *Client) Query(ctx context.Context, q api.Query) (json.RawMessage, error) {
	if q.LogicalServiceID != c.Discovery.LogicalServiceID {
		return nil, api.E("invalid_request", "query_owner_mismatch")
	}
	v, ok := c.inputs[q.Method]
	if !ok {
		return nil, api.E("unsupported", "method_not_supported")
	}
	if e := v.Validate(q.Payload); e != nil {
		return nil, e
	}
	raw, e := c.Transport.Call(ctx, "query", api.Raw(q))
	if e != nil {
		return nil, e
	}
	if e = c.outputs[q.Method].Validate(raw); e != nil {
		return nil, e
	}
	return raw, nil
}

type WSReady struct {
	Type             string `json:"type"`
	ConnectionID     string `json:"connection_id"`
	LogicalServiceID string `json:"logical_service_id"`
	Profile          string `json:"profile"`
	TransportProfile string `json:"transport_profile"`
	MethodsDigest    string `json:"methods_digest"`
	Limits           Limits `json:"limits"`
}
type WSRequest struct {
	Type       string          `json:"type"`
	RequestSeq uint64          `json:"request_seq"`
	Kind       string          `json:"kind"`
	Payload    json.RawMessage `json:"payload"`
}
type WSResponse struct {
	Type       string          `json:"type"`
	RequestSeq uint64          `json:"request_seq"`
	ResultKind string          `json:"result_kind"`
	Payload    json.RawMessage `json:"payload"`
}
type wsResult struct {
	body json.RawMessage
	err  error
}
type WSTransport struct {
	conn    *websocket.Conn
	ready   WSReady
	mu      sync.Mutex
	write   sync.Mutex
	seq     uint64
	pending map[uint64]chan wsResult
	closed  bool
	cancel  context.CancelFunc
}

func DialWebSocket(ctx context.Context, address, token string, expected Discovery, allowDev bool) (*WSTransport, error) {
	u, e := url.Parse(address)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Scheme != "wss" && !(allowDev && u.Scheme == "ws" && loopback(u)) {
		return nil, api.E("forbidden", "tls_required")
	}
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	conn, _, e := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPHeader: header, Subprotocols: []string{"harness-wss.v1"}})
	if e != nil {
		return nil, e
	}
	conn.SetReadLimit(1 << 20)
	kind, b, e := conn.Read(ctx)
	if e != nil || kind != websocket.MessageText {
		conn.CloseNow()
		return nil, api.E("dependency_unavailable", "ready_missing")
	}
	var ready WSReady
	if e = api.Decode(b, &ready); e != nil || ready.Type != "ready" || ready.LogicalServiceID != expected.LogicalServiceID || ready.Profile != api.Profile || ready.TransportProfile != "harness-wss/1" || ready.MethodsDigest != expected.MethodsDigest || ready.Limits.MaxPending < 1 || ready.Limits.MaxPending > 32 {
		conn.CloseNow()
		return nil, api.E("unsupported", "ready_mismatch")
	}
	readCtx, cancel := context.WithCancel(context.Background())
	t := &WSTransport{conn: conn, ready: ready, pending: map[uint64]chan wsResult{}, cancel: cancel}
	go t.readLoop(readCtx)
	return t, nil
}
func (t *WSTransport) Close() error {
	t.cancel()
	e := t.conn.Close(websocket.StatusNormalClosure, "closed")
	t.fail(api.E("dependency_unavailable", "connection_closed"))
	return e
}
func (t *WSTransport) fail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return
	}
	t.closed = true
	for seq, ch := range t.pending {
		ch <- wsResult{err: err}
		delete(t.pending, seq)
	}
}
func (t *WSTransport) readLoop(ctx context.Context) {
	defer t.conn.CloseNow()
	for {
		kind, b, e := t.conn.Read(ctx)
		if e != nil {
			t.fail(e)
			return
		}
		if kind != websocket.MessageText {
			t.fail(api.E("invalid_request", "binary_frame"))
			t.conn.CloseNow()
			return
		}
		var tag struct {
			Type string `json:"type"`
		}
		if e = json.Unmarshal(b, &tag); e != nil {
			t.fail(e)
			return
		}
		if tag.Type == "ping" {
			var ping struct {
				Type  string `json:"type"`
				Nonce string `json:"nonce"`
			}
			if e = api.Decode(b, &ping); e != nil {
				t.fail(e)
				return
			}
			ping.Type = "pong"
			t.write.Lock()
			e = t.conn.Write(ctx, websocket.MessageText, api.Raw(ping))
			t.write.Unlock()
			if e != nil {
				t.fail(e)
				return
			}
			continue
		}
		var response WSResponse
		if e = api.Decode(b, &response); e != nil || response.Type != "response" {
			t.fail(api.E("invalid_request", "invalid_response_frame"))
			return
		}
		t.mu.Lock()
		ch, ok := t.pending[response.RequestSeq]
		if ok {
			delete(t.pending, response.RequestSeq)
		}
		t.mu.Unlock()
		if !ok {
			continue
		}
		if response.ResultKind == "error" {
			var problem api.Error
			if e = api.Decode(response.Payload, &problem); e != nil {
				ch <- wsResult{err: e}
			} else {
				ch <- wsResult{err: &problem}
			}
		} else {
			ch <- wsResult{body: response.Payload}
		}
	}
}
func (t *WSTransport) Call(ctx context.Context, kind string, payload json.RawMessage) (json.RawMessage, error) {
	if _, e := api.ParseJSON(payload); e != nil {
		return nil, e
	}
	t.write.Lock()
	t.mu.Lock()
	if t.closed || len(t.pending) >= int(t.ready.Limits.MaxPending) || t.seq >= api.MaxSafeInteger {
		t.mu.Unlock()
		t.write.Unlock()
		return nil, api.E("overloaded", "connection_limit")
	}
	t.seq++
	seq := t.seq
	ch := make(chan wsResult, 1)
	t.pending[seq] = ch
	t.mu.Unlock()
	e := t.conn.Write(ctx, websocket.MessageText, api.Raw(WSRequest{"request", seq, kind, payload}))
	t.write.Unlock()
	if e != nil {
		t.mu.Lock()
		delete(t.pending, seq)
		t.mu.Unlock()
		return nil, e
	}
	select {
	case r := <-ch:
		return r.body, r.err
	case <-ctx.Done():
		t.mu.Lock()
		delete(t.pending, seq)
		t.mu.Unlock()
		return nil, ctx.Err()
	}
}

var _ Transport = (*HTTPTransport)(nil)
var _ Transport = (*WSTransport)(nil)
