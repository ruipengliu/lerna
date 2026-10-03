package integration_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type wireInput struct {
	Value string `json:"value"`
}
type wireOutput struct {
	Body string `json:"body"`
}
type wireProcessor struct {
	dispatcher *runtime.Dispatcher
	commands   atomic.Int32
}

func (p *wireProcessor) Call(ctx context.Context, auth runtime.Auth, kind string, payload json.RawMessage) (string, json.RawMessage, error) {
	if kind == "command" {
		p.commands.Add(1)
	}
	return (wss.LocalProcessor{Dispatcher: p.dispatcher}).Call(ctx, auth, kind, payload)
}

type wireFixture struct {
	store     *sqlite.Store
	scope     runtime.Scope
	auth      runtime.Auth
	identity  *platform.DevIdentity
	processor *wireProcessor
	registry  *runtime.Registry
	token     string
	release   chan struct{}
	entered   chan struct{}
}

func newWireFixture(t *testing.T) *wireFixture {
	t.Helper()
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "wire.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	f := &wireFixture{store: store, token: api.NewID("testkey"), release: make(chan struct{}), entered: make(chan struct{}, 28)}
	f.scope = runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	f.auth = runtime.Auth{TenantID: f.scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1, Roles: []string{}}
	f.identity = &platform.DevIdentity{Store: store, OwnerID: f.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: f.auth, TokenHash: api.Hash([]byte(f.token))}}}
	if err = f.identity.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	f.registry = runtime.NewRegistry()
	write := api.Contract[wireInput, wireOutput]("fixture.save", "fixture", "command", false, false)
	if err = f.registry.Register(runtime.Method{Contract: write, Participants: []string{"fixture"}, Apply: func(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command) (runtime.Outcome, error) {
		var in wireInput
		if err := api.Decode(c.Payload, &in); err != nil {
			return runtime.Outcome{}, err
		}
		out := wireOutput{in.Value}
		if err := tx.Create(ctx, "fixture.objects", c.TargetID, auth.SubjectID, out); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Applied(out), nil
	}}); err != nil {
		t.Fatal(err)
	}
	read := api.Contract[wireInput, wireOutput]("fixture.read", "fixture", "query", false, false)
	read.OutputSchema = api.Object(map[string]any{"body": api.Schema{"type": "string", "maxLength": api.MaxJSONBytes - 32}}, "body")
	if err = f.registry.Register(runtime.Method{Contract: read, Query: func(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, q api.Query) (any, error) {
		var in wireInput
		if err := api.Decode(q.Payload, &in); err != nil {
			return nil, err
		}
		if in.Value == "hold" {
			f.entered <- struct{}{}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-f.release:
			}
		}
		if in.Value == "large" {
			return wireOutput{strings.Repeat("x", api.MaxJSONBytes-40)}, nil
		}
		var out wireOutput
		_, err := store.Read(ctx, scope, "fixture.objects", q.TargetID, 0, &out)
		return out, err
	}}); err != nil {
		t.Fatal(err)
	}
	f.processor = &wireProcessor{dispatcher: &runtime.Dispatcher{Store: store, OwnerID: f.scope.OwnerID, Registry: f.registry}}
	return f
}
func (f *wireFixture) server(t *testing.T, mutate func(http.Handler) http.Handler) *httptest.Server {
	t.Helper()
	s, err := wss.New(wss.Config{OwnerID: f.scope.OwnerID, Store: f.store, Registry: f.registry, Identity: f.identity, Processor: f.processor, Origins: []string{"https://renderer.example"}, Location: "cloud", MaxConnections: 8, MaxQueuedBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	h := s.Handler()
	if mutate != nil {
		h = mutate(h)
	}
	server := httptest.NewTLSServer(h)
	t.Cleanup(server.Close)
	return server
}
func (f *wireFixture) command() api.Command {
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), TargetID: api.NewID("object"), Method: "fixture.save", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(wireInput{"original accurate bytes"})}
}
func (f *wireFixture) query(target, mode string) api.Query {
	return api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: api.NewID("query"), TargetID: target, Method: "fixture.read", Payload: api.Raw(wireInput{mode})}
}
func (f *wireFixture) connect(t *testing.T, server *httptest.Server, ctx context.Context) (*harness.WSTransport, harness.Discovery) {
	t.Helper()
	httpWire := &harness.HTTPTransport{BaseURL: server.URL, Token: f.token, HTTP: server.Client()}
	d, err := httpWire.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	conn, err := harness.DialWebSocketWithHTTP(ctx, "wss"+strings.TrimPrefix(server.URL, "https")+"/connect", f.token, d, false, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn, d
}

func TestRealTLSWebSocketPreservesLargeDomainAndControlCapacity(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	f := newWireFixture(t)
	server := f.server(t, nil)
	conn, discovery := f.connect(t, server, ctx)
	journal, err := harness.OpenJournal(filepath.Join(t.TempDir(), "journal"), discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	client, err := harness.NewClient(conn, journal, discovery)
	if err != nil {
		t.Fatal(err)
	}
	command := f.command()
	receipt, err := client.Send(ctx, command)
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("actual save: %v", err)
	}
	raw, err := client.Query(ctx, f.query(command.TargetID, "large"))
	var large wireOutput
	if err != nil || api.Decode(raw, &large) != nil || len(large.Body) != api.MaxJSONBytes-40 {
		t.Fatalf("valid domain in larger frame: %v", err)
	}
	results := make(chan error, 28)
	for i := 0; i < 28; i++ {
		go func() { _, err := client.Query(ctx, f.query(command.TargetID, "hold")); results <- err }()
	}
	for i := 0; i < 28; i++ {
		select {
		case <-f.entered:
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	if _, err = client.Query(ctx, f.query(command.TargetID, "read")); !api.IsCode(err, "overloaded") {
		t.Fatalf("ordinary capacity not bounded: %v", err)
	}
	controlCtx, stop := context.WithTimeout(ctx, time.Second)
	original, err := conn.Call(controlCtx, "receipt_lookup", api.Raw(api.ReceiptLookup{LogicalServiceID: f.scope.OwnerID, CommandID: command.CommandID}))
	stop()
	if err != nil || api.Decode(original, &receipt) != nil || receipt.CommandID != command.CommandID {
		t.Fatalf("control capacity blocked by normal work: %v", err)
	}
	close(f.release)
	for i := 0; i < 28; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
}

type dropReplyWriter struct {
	http.ResponseWriter
	armed *atomic.Bool
}

func (w dropReplyWriter) Write(b []byte) (int, error) {
	if w.armed.CompareAndSwap(true, false) {
		conn, _, err := http.NewResponseController(w.ResponseWriter).Hijack()
		if err != nil {
			return 0, err
		}
		conn.Close()
		return 0, io.ErrClosedPipe
	}
	return w.ResponseWriter.Write(b)
}
func TestRealTLSReplyLossRecoversDurableOriginalWithoutSecondCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f := newWireFixture(t)
	var armed atomic.Bool
	server := f.server(t, func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/call" {
				next.ServeHTTP(dropReplyWriter{w, &armed}, r)
			} else {
				next.ServeHTTP(w, r)
			}
		})
	})
	transport := &harness.HTTPTransport{BaseURL: server.URL, Token: f.token, HTTP: server.Client()}
	d, err := transport.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "original-journal")
	j, err := harness.OpenJournal(path, d.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	client, err := harness.NewClient(transport, j, d)
	if err != nil {
		t.Fatal(err)
	}
	command := f.command()
	armed.Store(true)
	if _, err = client.Send(ctx, command); err == nil {
		t.Fatal("dropped connection returned a receipt")
	}
	var truth wireOutput
	if _, err = f.store.Read(ctx, f.scope, "fixture.objects", command.TargetID, 0, &truth); err != nil || truth.Body != "original accurate bytes" {
		t.Fatalf("actual committed target: %v", err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = harness.OpenJournal(path, d.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	client, err = harness.NewClient(transport, j, d)
	if err != nil {
		t.Fatal(err)
	}
	receipts, partial, err := client.Recover(ctx)
	if err != nil || partial || len(receipts) != 1 || receipts[0].CommandID != command.CommandID || f.processor.commands.Load() != 1 {
		t.Fatalf("original recovery receipts=%d calls=%d partial=%v error=%v", len(receipts), f.processor.commands.Load(), partial, err)
	}
}

func TestRealTLSWebSocketRejectsBadFramesAndCurrentRevocation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f := newWireFixture(t)
	server := f.server(t, nil)
	header := http.Header{"Authorization": []string{"Bearer " + f.token}, "Origin": []string{"https://untrusted.example"}}
	address := "wss" + strings.TrimPrefix(server.URL, "https") + "/connect"
	if conn, _, err := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: header, Subprotocols: []string{"harness-wss.v1"}}); err == nil {
		conn.CloseNow()
		t.Fatal("bad Origin connected")
	}
	header.Del("Origin")
	conn, _, err := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: header, Subprotocols: []string{"harness-wss.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	if err = conn.Write(ctx, websocket.MessageText, []byte(`{"type":"request","type":"request","request_seq":1,"kind":"query","payload":{}}`)); err != nil {
		t.Fatal(err)
	}
	if _, _, err = conn.Read(ctx); err == nil {
		t.Fatal("duplicate raw key accepted")
	}
	conn.CloseNow()
	ws, _ := f.connect(t, server, ctx)
	if err = f.identity.Revoke(ctx, f.auth); err != nil {
		t.Fatal(err)
	}
	command := f.command()
	if _, err = ws.Call(ctx, "command", api.Raw(command)); err == nil {
		t.Fatal("revoked open connection accepted command")
	}
	var out wireOutput
	if _, err = f.store.Read(ctx, f.scope, "fixture.objects", command.TargetID, 0, &out); !api.IsCode(err, "not_found") {
		t.Fatalf("revoked command changed target: %v", err)
	}
}
