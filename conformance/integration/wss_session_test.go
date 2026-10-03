package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

func cookieSocket(t *testing.T, ctx context.Context, f *wireFixture, server *httptest.Server) (*websocket.Conn, *http.Cookie, string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/auth/session", bytes.NewReader(api.Raw(map[string]string{"token": f.token})))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Origin", "https://renderer.example")
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	err = errors.Join(err, response.Body.Close())
	var session struct {
		Authenticated bool   `json:"authenticated"`
		CSRFToken     string `json:"csrf_token"`
	}
	if err != nil || response.StatusCode != 200 || api.Decode(body, &session) != nil || !session.Authenticated {
		t.Fatalf("actual cookie login %d %v", response.StatusCode, err)
	}
	var cookie *http.Cookie
	for _, c := range response.Cookies() {
		if c.Name == "harness_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("cookie login omitted original session")
	}
	conn, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(server.URL, "https")+"/connect", &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: http.Header{"Cookie": []string{cookie.String()}, "Origin": []string{"https://renderer.example"}}, Subprotocols: []string{"harness-wss.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = conn.Read(ctx); err != nil {
		conn.CloseNow()
		t.Fatal(err)
	}
	return conn, cookie, session.CSRFToken
}

func logoutCookie(t *testing.T, ctx context.Context, server *httptest.Server, cookie *http.Cookie, csrf string) {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, "POST", server.URL+"/auth/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.AddCookie(cookie)
	req.Header.Set("Origin", "https://renderer.example")
	req.Header.Set("X-CSRF-Token", csrf)
	response, err := server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	err = response.Body.Close()
	if response.StatusCode != 200 || err != nil {
		t.Fatalf("actual session logout %d %v", response.StatusCode, err)
	}
}

// HTTP 关闭原 cookie 会话后，另一 tab 的原 socket 不得保留查询、命令或心跳授权。
func TestRealTLSCookieLogoutClosesExistingSocketAuthority(t *testing.T) {
	for _, kind := range []string{"query", "command", "ping"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			f := newWireFixture(t)
			server := f.server(t, nil)
			command := f.command()
			receipt, err := f.processor.dispatcher.Command(ctx, f.auth, api.Raw(command))
			if err != nil || receipt.Stage != "applied" {
				t.Fatal(err)
			}
			conn, cookie, csrf := cookieSocket(t, ctx, f, server)
			defer conn.CloseNow()
			logoutCookie(t, ctx, server, cookie, csrf)
			newCommand := f.command()
			frame := api.Raw(map[string]string{"type": "ping", "nonce": "closed-session"})
			if kind == "query" {
				frame = api.Raw(harness.WSRequest{Type: "request", RequestSeq: 1, Kind: kind, Payload: api.Raw(f.query(command.TargetID, "read"))})
			}
			if kind == "command" {
				frame = api.Raw(harness.WSRequest{Type: "request", RequestSeq: 1, Kind: kind, Payload: api.Raw(newCommand)})
			}
			if err = conn.Write(ctx, websocket.MessageText, frame); err == nil {
				if _, disclosed, err := conn.Read(ctx); err == nil {
					t.Fatalf("closed cookie retained %s authority: %s", kind, disclosed)
				}
			}
			if _, err = f.processor.dispatcher.Lookup(ctx, f.auth, newCommand.CommandID); !api.IsCode(err, "not_found") {
				t.Fatalf("closed cookie created a command responsibility: %v", err)
			}
			// Logout 不撤销独立 bearer；原已接纳责任和回执仍可由当前主体核对。
			bearer, _ := f.connect(t, server, ctx)
			body, err := bearer.Call(ctx, "receipt_lookup", api.Raw(api.ReceiptLookup{LogicalServiceID: f.scope.OwnerID, CommandID: command.CommandID}))
			var original api.Receipt
			if err != nil || api.Decode(body, &original) != nil || !api.Equal(original, receipt) {
				t.Fatalf("logout revoked independent bearer or original receipt: %v", err)
			}
		})
	}
}

func TestRealTLSCookieSessionExpiryClosesExistingSocket(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f := newWireFixture(t)
	f.identity.SessionTTL = 250 * time.Millisecond
	server := f.server(t, nil)
	conn, _, _ := cookieSocket(t, ctx, f, server)
	defer conn.CloseNow()
	timer := time.NewTimer(300 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := conn.Write(ctx, websocket.MessageText, api.Raw(map[string]string{"type": "ping", "nonce": "expired-session"})); err == nil {
		if _, disclosed, err := conn.Read(ctx); err == nil {
			t.Fatalf("expired cookie session disclosed heartbeat %s", disclosed)
		}
	}
}

type delayedSessionReply struct {
	next    wss.Processor
	entered chan api.Receipt
	release chan struct{}
}

func (p *delayedSessionReply) Call(ctx context.Context, a runtime.Auth, kind string, b json.RawMessage) (string, json.RawMessage, error) {
	resultKind, body, err := p.next.Call(ctx, a, kind, b)
	var receipt api.Receipt
	if err == nil && resultKind == "receipt" && api.Decode(body, &receipt) == nil {
		p.entered <- receipt
		<-p.release
	}
	return resultKind, body, err
}

func TestRealTLSLogoutFencesLateReplyWithoutDeletingCommittedCommand(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f := newWireFixture(t)
	p := &delayedSessionReply{next: f.processor, entered: make(chan api.Receipt, 1), release: make(chan struct{})}
	released := false
	gateway, err := wss.New(wss.Config{OwnerID: f.scope.OwnerID, Store: f.store, Registry: f.registry, Identity: f.identity, Processor: p, Origins: []string{"https://renderer.example"}, Location: "cloud", MaxConnections: 8, MaxQueuedBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(gateway.Handler())
	defer func() {
		if !released {
			close(p.release)
		}
		server.Close()
	}()
	conn, cookie, csrf := cookieSocket(t, ctx, f, server)
	defer conn.CloseNow()
	command := f.command()
	if err = conn.Write(ctx, websocket.MessageText, api.Raw(harness.WSRequest{Type: "request", RequestSeq: 1, Kind: "command", Payload: api.Raw(command)})); err != nil {
		t.Fatal(err)
	}
	var committed api.Receipt
	select {
	case committed = <-p.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if committed.Stage != "applied" {
		t.Fatalf("actual command not committed %+v", committed)
	}
	logoutCookie(t, ctx, server, cookie, csrf)
	close(p.release)
	released = true
	if _, disclosed, err := conn.Read(ctx); err == nil {
		t.Fatalf("writer disclosed late committed reply after logout: %s", disclosed)
	}
	original, err := f.processor.dispatcher.Lookup(ctx, f.auth, command.CommandID)
	if err != nil || !api.Equal(original, committed) {
		t.Fatalf("session revocation erased original committed responsibility %+v %v", original, err)
	}
}
