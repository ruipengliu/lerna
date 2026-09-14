package asynccheck

import (
	"bytes"
	"context"
	"lerna/adapters/wsbinding"
	"lerna/authorization"
	"lerna/sdk"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

type deliveryFailureStore struct {
	authorization.Store
	fail atomic.Bool
}

func (s *deliveryFailureStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	if s.fail.Load() {
		old, e := s.Store.Load(ctx)
		if e != nil {
			return e
		}
		if !bytes.Equal(old.State.DeliveryData, state.DeliveryData) {
			return &authorization.Error{Code: authorization.Unavailable}
		}
	}
	return s.Store.Commit(ctx, v, state)
}
func TestWSReliableInboxWriteFailureHasNoReceipt(t *testing.T) {
	edge, cloud := prepareWS(t)
	a, ah, _ := openWS(t, edge)
	defer a.close()
	b, bh, _ := openWS(t, cloud)
	defer b.close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, material, e := b.request(ctx)
	mustGRPC(t, e)
	ah = reliableHost(t, a, ah)
	bh = reliableHost(t, b, bh)
	fault := &deliveryFailureStore{Store: b.db}
	state, e := b.db.Load(ctx)
	mustGRPC(t, e)
	authority, e := authorization.New(fault, b.clock, state.State.Config)
	mustGRPC(t, e)
	j, e := wsbinding.NewJournal(authority, "local/operator/edge", wsbinding.JournalConfig{Records: 64, Bytes: 262144, Reorder: 8, Attempts: 3, Retention: time.Minute})
	mustGRPC(t, e)
	resolve := bh.Resolve
	bh.Resolve = func(ctx context.Context, p authorization.GrantPresentation) (*wsbinding.Binding, error) {
		binding, e := resolve(ctx, p)
		if e != nil {
			return nil, e
		}
		binding.Journal = j
		return binding, nil
	}
	s, e := wsbinding.NewServer(bh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	p, e := ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer p.Close()
	// Session claim and bootstrap are durable; reject only the incoming mutation.
	fault.fail.Store(true)
	_, e = sdk.NewCapabilityClient(p, "local").Invoke(ctx, req, material)
	if e == nil {
		t.Fatal("failed inbox reported acceptance")
	}
	source, e := wsbinding.NewJournal(a.auth, "local/operator/cloud", wsbinding.JournalConfig{Records: 64, Bytes: 262144, Reorder: 8, Attempts: 3, Retention: time.Minute})
	mustGRPC(t, e)
	status, e := source.Status(ctx)
	mustGRPC(t, e)
	if status.Confirmed != 0 || status.Outbox != 1 {
		t.Fatal("PERSISTED before inbox commit", status)
	}
	fault.fail.Store(false)
	received, e := j.Status(ctx)
	mustGRPC(t, e)
	if received.Inbox != 0 || received.Received != 0 {
		t.Fatal("partial inbox", received)
	}
	p.Close()
	p, e = ah.Dial(ctx, "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer p.Close()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		record, e := b.exec.GetInvocation(ctx, req.OperationID)
		if e == nil && record.Request.OperationID == req.OperationID {
			break
		}
		select {
		case <-tick.C:
		case <-ctx.Done():
			t.Fatal("outbox did not recover", e)
		}
	}
}
