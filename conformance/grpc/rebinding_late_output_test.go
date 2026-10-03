package grpc_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/endpointchannel"
	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// 外部公共PendingResponse seam延迟已经取得的原结果，不替换Channel处理或目标事实。
type delayedChannelOutput struct {
	*endpointchannel.Router
	completed chan struct{}
	release   chan struct{}
	once      atomic.Bool
	discarded atomic.Uint64
}
type delayedChannelConnection struct {
	wss.Connection
	parent *delayedChannelOutput
}
type delayedChannelPending struct {
	wss.PendingResponse
	parent *delayedChannelOutput
}

func (p *delayedChannelOutput) Open(ctx context.Context, a rt.Auth, info wss.ConnectionInfo) (wss.Connection, error) {
	c, err := p.Router.Open(ctx, a, info)
	return &delayedChannelConnection{Connection: c, parent: p}, err
}
func (c *delayedChannelConnection) Begin(ctx context.Context, a rt.Auth, seq uint64, kind string, raw json.RawMessage) (wss.PendingResponse, error) {
	p, err := c.Connection.(wss.SequentialConnection).Begin(ctx, a, seq, kind, raw)
	if err != nil {
		return nil, err
	}
	if c.parent.once.CompareAndSwap(false, true) {
		return &delayedChannelPending{PendingResponse: p, parent: c.parent}, nil
	}
	return p, nil
}
func (p *delayedChannelPending) Wait(ctx context.Context) (string, json.RawMessage, error) {
	kind, body, err := p.PendingResponse.Wait(ctx)
	close(p.parent.completed)
	select {
	case <-p.parent.release:
		return kind, body, err
	case <-ctx.Done():
		return "", nil, ctx.Err()
	}
}
func (p *delayedChannelPending) BeginDisclosure(ctx context.Context) (func(), error) {
	release, err := p.PendingResponse.(wss.DisclosureGate).BeginDisclosure(ctx)
	if err != nil {
		p.parent.discarded.Add(1)
	}
	return release, err
}

func TestActualWSSDiscardsCompletedOldBindingOutputAfterNewReady(t *testing.T) {
	path := filepath.Join(t.TempDir(), "original.sqlite")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	f := newUnaryFixtureWithStore(t, store)
	_, reg := newStaticChannel(t, f)
	first := startChannelApplicationProcess(t, f, "sqlite", path, reg, false)
	second := startChannelApplicationProcess(t, f, "sqlite", path, reg, false)
	router, err := endpointchannel.NewRouter(endpointchannel.Config{OwnerID: f.owner, GatewayInstanceID: api.NewID("instance"), MethodsDigest: f.discovery.MethodsDigest, Applications: []endpointchannel.Application{{Address: first.address, ClientTLS: f.clientTLS}, {Address: second.address, ClientTLS: f.clientTLS}}, Registrations: []transport.EndpointRegistration{reg}, Identity: f.identity, Credentials: func(ctx context.Context, a rt.Auth) (string, error) {
		return f.token, f.identity.CheckCurrent(ctx, a)
	}})
	if err != nil {
		t.Fatal(err)
	}
	delay := &delayedChannelOutput{Router: router, completed: make(chan struct{}), release: make(chan struct{})}
	discovery, address, httpClient := realChannelGateway(t, f, router, delay)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	ws, err := harness.DialWebSocketWithHTTP(ctx, address, f.token, discovery, false, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	journal, err := harness.OpenJournal(t.TempDir(), discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	client, err := harness.NewClient(ws, journal, discovery)
	if err != nil {
		t.Fatal(err)
	}
	original := commandFor(f)
	originalCtx, stopOriginal := context.WithTimeout(ctx, 2*time.Second)
	defer stopOriginal()
	finished := make(chan error, 1)
	go func() { _, e := client.Send(originalCtx, original); finished <- e }()
	select {
	case <-delay.completed:
	case <-originalCtx.Done():
		t.Fatal("original reply was not actually completed before the fault")
	}
	before := router.States()
	first.stop(t, true)
	var after []endpointchannel.State
	for deadline := time.Now().Add(time.Second); time.Now().Before(deadline); {
		after = router.States()
		if len(after) == 1 && after[0].Connected && after[0].BindingID != before[0].BindingID {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(after) != 1 || !after[0].Connected || after[0].ConnectionID != before[0].ConnectionID || after[0].BindingID == before[0].BindingID {
		t.Fatal("replacement did not attain Ready before the old output release")
	}
	close(delay.release)
	if e := <-finished; !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("old completed output was disclosed after new binding Ready: %v", e)
	}
	if delay.discarded.Load() != 1 {
		t.Fatal("old completed output never reached the actual disclosure gate")
	}
	rows, partial, err := client.Recover(ctx)
	if err != nil || partial || len(rows) != 1 || rows[0].CommandID != original.CommandID || rows[0].Stage != "applied" {
		t.Fatalf("discarded output lost the original responsibility: %+v %v", rows, err)
	}
	if router.States()[0].LastRequestSeq != 2 {
		t.Fatal("receipt recovery did not keep the original external sequence")
	}
	t.Logf("CHANNEL_OLD_OUTPUT_EVIDENCE %s", api.Raw(struct {
		OriginalCommandID string
		OriginalTTL       string
		First             endpointchannel.State
		Replacement       endpointchannel.State
		Discarded         uint64
	}{original.CommandID, original.ExpiresAt, before[0], after[0], delay.discarded.Load()}))
	ws.Close()
	router.Close()
	second.stop(t, false)
}
