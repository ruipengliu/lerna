package integration_test

import (
	"context"
	"encoding/json"
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

// 取消只发送停止信号；忽略取消的实际调用退出前，宿主不能关闭其数据库。
type shutdownProcessor struct {
	entered   chan struct{}
	cancelled chan struct{}
	release   chan struct{}
}

func (p *shutdownProcessor) Call(ctx context.Context, _ runtime.Auth, _ string, _ json.RawMessage) (string, json.RawMessage, error) {
	close(p.entered)
	<-ctx.Done()
	close(p.cancelled)
	<-p.release
	return "", nil, ctx.Err()
}

func TestRealTLSGatewayShutdownJoinsUpgradedConnectionAndActualCall(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	f := newWireFixture(t)
	p := &shutdownProcessor{entered: make(chan struct{}), cancelled: make(chan struct{}), release: make(chan struct{})}
	defer close(p.release)
	gateway, err := wss.New(wss.Config{OwnerID: f.scope.OwnerID, Store: f.store, Registry: f.registry, Identity: f.identity, Processor: p, Location: "cloud", MaxConnections: 8, MaxQueuedBytes: 8 << 20})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewTLSServer(gateway.Handler())
	defer server.Close()
	conn, _, err := websocket.Dial(ctx, "wss"+strings.TrimPrefix(server.URL, "https")+"/connect", &websocket.DialOptions{HTTPClient: server.Client(), HTTPHeader: http.Header{"Authorization": []string{"Bearer " + f.token}}, Subprotocols: []string{"harness-wss.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if _, _, err = conn.Read(ctx); err != nil {
		t.Fatal(err)
	}
	if err = conn.Write(ctx, websocket.MessageText, api.Raw(harness.WSRequest{Type: "request", RequestSeq: 1, Kind: "query", Payload: api.Raw(f.query(api.NewID("object"), "read"))})); err != nil {
		t.Fatal(err)
	}
	select {
	case <-p.entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	closer, ok := any(gateway).(interface{ Close() error })
	if !ok {
		t.Fatal("gateway has no owned shutdown for upgraded connections")
	}
	closed := make(chan error, 1)
	go func() { closed <- closer.Close() }()
	select {
	case <-p.cancelled:
	case <-ctx.Done():
		t.Fatal("shutdown did not cancel the actual call")
	}
	if _, _, err = conn.Read(ctx); err == nil {
		t.Fatal("upgraded connection remained open during shutdown")
	}
	select {
	case err := <-closed:
		t.Fatalf("shutdown returned before the actual call exited: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	response, err := server.Client().Get(server.URL + "/health/live")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("shutdown admitted a new handler: %d", response.StatusCode)
	}
	p.release <- struct{}{}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("shutdown failed to observe actual exit")
	}
	if err = closer.Close(); err != nil {
		t.Fatal(err)
	}
}
