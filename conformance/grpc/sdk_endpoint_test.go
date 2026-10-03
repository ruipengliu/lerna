package grpc_test

import (
	"context"
	"crypto/ecdsa"
	"net/http"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/endpointchannel"
	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
)

type sdkEndpointReceiver struct {
	f               *unaryFixture
	journal         *harness.ReplyJournal
	invoked         atomic.Uint64
	startedSequence atomic.Uint64
	loseReplyOnce   atomic.Bool
	lost            chan struct{}
	lookedUp        atomic.Uint64
}

func (r *sdkEndpointReceiver) Invoke(ctx context.Context, d grpcwire.Delivery) (grpcwire.Reply, error) {
	entry, err := r.journal.Invocation(ctx, d.DeliveryID)
	if err != nil || entry.Invocation == nil || entry.Invocation.Phase != "started" || entry.Invocation.Sequence == 0 || !api.Equal(entry.Delivery, d) || entry.ReplyDigest != "" {
		return grpcwire.Reply{}, api.E("invalid_state", "endpoint_original_not_durable_before_handler")
	}
	r.startedSequence.Store(entry.Invocation.Sequence)
	r.invoked.Add(1)
	kind, raw, err := r.f.processor.Call(ctx, r.f.auth, d.Kind, d.Request)
	if err == nil && r.loseReplyOnce.CompareAndSwap(true, false) {
		close(r.lost)
		return grpcwire.Reply{}, api.E("effect_unknown", "endpoint_response_lost_after_original_commit")
	}
	return grpcwire.Reply{Type: "reply", DeliveryID: d.DeliveryID, RequestDigest: d.RequestDigest, ResultKind: kind, Payload: raw}, err
}
func (r *sdkEndpointReceiver) Lookup(ctx context.Context, d grpcwire.Delivery) (grpcwire.Reply, bool, error) {
	r.lookedUp.Add(1)
	var command api.Command
	if err := api.Decode(d.Request, &command); err != nil {
		return grpcwire.Reply{}, false, err
	}
	kind, raw, err := r.f.processor.Call(ctx, r.f.auth, "receipt_lookup", api.Raw(api.ReceiptLookup{LogicalServiceID: command.LogicalServiceID, CommandID: command.CommandID}))
	if api.IsCode(err, "not_found") {
		return grpcwire.Reply{}, false, nil
	}
	return grpcwire.Reply{Type: "reply", DeliveryID: d.DeliveryID, RequestDigest: d.RequestDigest, ResultKind: kind, Payload: raw}, err == nil, err
}

type checkedSDKReplyOwner struct {
	original *endpointAuthority
	journal  *harness.ReplyJournal
	observed atomic.Bool
}

func (p *checkedSDKReplyOwner) ReceiveDeliveryReply(ctx context.Context, r transport.EndpointRegistration, d grpcwire.Delivery, reply grpcwire.Reply) (bool, error) {
	pending, partial, err := p.journal.Pending(ctx, 32)
	if err != nil || partial || len(pending) != 1 || !api.Equal(pending[0].Reply, reply) || !api.Equal(pending[0].Delivery, d) {
		return false, api.E("invalid_state", "endpoint_reply_not_durable_before_actual_io")
	}
	p.observed.Store(true)
	return p.original.ReceiveReply(ctx, r, d, reply)
}

type sdkEndpointSetup struct {
	f          *unaryFixture
	reg        transport.EndpointRegistration
	keys       *platform.Keyring
	proofRoot  string
	owner      *checkedSDKReplyOwner
	servers    []*transport.Server
	router     *endpointchannel.Router
	discovery  harness.Discovery
	address    string
	httpClient *http.Client
	fault      *loseOuterAck
}

func newSDKEndpointSetup(t *testing.T, f *unaryFixture, loseAck ...bool) *sdkEndpointSetup {
	t.Helper()
	_, reg := newStaticChannel(t, f)
	keys, err := platform.NewDevelopmentKey(f.auth.TenantID, f.owner, []string{"delivery"})
	if err != nil {
		t.Fatal(err)
	}
	proofRoot := t.TempDir()
	owner := &checkedSDKReplyOwner{original: &endpointAuthority{f: f, reg: reg}}
	authority, err := transport.NewStaticEndpointAuthority(transport.StaticEndpointAuthorityConfig{OwnerID: f.owner, GatewayIdentities: []string{"spiffe://harness.test/gateway"}, Identity: f.identity, Pairs: []transport.StaticEndpointPair{{Registration: reg, Methods: f.discovery.Methods}}, Keys: keys, Proofs: originalDeliveryProofs(proofRoot), Replies: owner})
	if err != nil {
		t.Fatal(err)
	}
	servers, addresses := realDeliveryApplications(t, f, authority)
	router, err := endpointchannel.NewRouter(endpointchannel.Config{OwnerID: f.owner, GatewayInstanceID: api.NewID("instance"), MethodsDigest: f.discovery.MethodsDigest, Applications: []endpointchannel.Application{{Address: addresses[0], ClientTLS: f.clientTLS}, {Address: addresses[1], ClientTLS: f.clientTLS}}, Registrations: []transport.EndpointRegistration{reg}, Identity: f.identity, Credentials: func(ctx context.Context, a rt.Auth) (string, error) {
		return f.token, f.identity.CheckCurrent(ctx, a)
	}, DeliveryProofs: authority})
	if err != nil {
		t.Fatal(err)
	}
	var fault *loseOuterAck
	var processors []wss.Processor
	if len(loseAck) != 0 && loseAck[0] {
		fault = &loseOuterAck{Router: router, lost: make(chan struct{})}
		processors = []wss.Processor{fault}
	}
	discovery, address, httpClient := realChannelGateway(t, f, router, processors...)
	return &sdkEndpointSetup{f, reg, keys, proofRoot, owner, servers, router, discovery, address, httpClient, fault}
}
func (s *sdkEndpointSetup) config(journal *harness.ReplyJournal, receiver *sdkEndpointReceiver) harness.EndpointConfig {
	return harness.EndpointConfig{TenantID: s.f.auth.TenantID, IssuerServiceID: s.f.owner, RecipientServiceID: s.f.owner, EndpointID: s.reg.EndpointID, InstanceID: s.reg.InstanceID, Generation: s.reg.Generation, IdentityScope: s.discovery.IdentityScope, IdentityRevision: s.discovery.IdentityRevision, Profile: api.Profile, SchemaDigest: api.CoreDigest(), Methods: s.f.discovery.Methods, Keys: map[string]*ecdsa.PublicKey{"development-es256": s.keys.Keys["development-es256"].Public}, Proofs: originalDeliveryProofs(s.proofRoot), Current: func(ctx context.Context) error { return s.f.identity.CheckCurrent(ctx, s.f.auth) }, Receiver: receiver, Journal: journal}
}
func TestGoWSEndpointPersistsOriginalReplyBeforeOwnerAck(t *testing.T) {
	s := newSDKEndpointSetup(t, newUnaryFixture(t))
	f, reg, discovery, owner := s.f, s.reg, s.discovery, s.owner
	journalPath := t.TempDir()
	journal, err := harness.OpenReplyJournal(journalPath, discovery.IdentityScope, reg.EndpointID, reg.InstanceID, reg.Generation)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	owner.journal = journal
	receiver := &sdkEndpointReceiver{f: f, journal: journal}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	ws, err := harness.DialWebSocketEndpointWithHTTP(ctx, s.address, f.token, discovery, false, s.httpClient, s.config(journal, receiver))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	d := signedStaticDelivery(t, f, reg, s.keys, s.proofRoot)
	if err = s.servers[0].Deliver(ctx, reg.TenantID, d); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline); {
		pending, _, e := journal.Pending(ctx, 32)
		if e != nil {
			t.Fatal(e)
		}
		if owner.observed.Load() && len(pending) == 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !owner.observed.Load() || receiver.invoked.Load() != 1 || receiver.startedSequence.Load() != 1 {
		t.Fatal("SDK did not durably freeze original Reply before actual owner IO")
	}
	if pending, _, e := journal.Pending(ctx, 32); e != nil || len(pending) != 0 {
		t.Fatal("matching durable owner Ack did not close SDK reply responsibility")
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = harness.OpenReplyJournal(journalPath, discovery.IdentityScope, reg.EndpointID, reg.InstanceID, reg.Generation)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	if pending, _, e := journal.Pending(ctx, 32); e != nil || len(pending) != 0 {
		t.Fatal("reopen lost the exact confirmed owner Ack")
	}
}

func TestGoWSEndpointReopenStartedUnknownQueriesOriginalWithoutReinvoke(t *testing.T) {
	verifyGoWSEndpointUnknown(t, newUnaryFixture(t))
}
func TestGoWSEndpointPostgresReopenStartedUnknownQueriesOriginalWithoutReinvoke(t *testing.T) {
	verifyGoWSEndpointUnknown(t, newPostgresEndpointFixture(t))
}
func newPostgresEndpointFixture(t *testing.T) *unaryFixture {
	t.Helper()
	dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("actual PostgreSQL configuration required")
	}
	store, err := postgres.Open(context.Background(), dsn, postgres.WithMaxConnections(8))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	if err = store.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return newUnaryFixtureWithStore(t, store)
}
func verifyGoWSEndpointUnknown(t *testing.T, f *unaryFixture) {
	t.Helper()
	s := newSDKEndpointSetup(t, f)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	path := t.TempDir()
	journal, err := harness.OpenReplyJournal(path, s.discovery.IdentityScope, s.reg.EndpointID, s.reg.InstanceID, s.reg.Generation)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { journal.Close() }()
	s.owner.journal = journal
	receiver := &sdkEndpointReceiver{f: s.f, journal: journal, lost: make(chan struct{})}
	receiver.loseReplyOnce.Store(true)
	ws, err := harness.DialWebSocketEndpointWithHTTP(ctx, s.address, s.f.token, s.discovery, false, s.httpClient, s.config(journal, receiver))
	if err != nil {
		t.Fatal(err)
	}
	d := signedStaticDelivery(t, s.f, s.reg, s.keys, s.proofRoot)
	if err = s.servers[0].Deliver(ctx, s.reg.TenantID, d); err != nil {
		t.Fatal(err)
	}
	select {
	case <-receiver.lost:
	case <-ctx.Done():
		t.Fatal("original receiver did not actually commit before lost reply")
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	entry, err := journal.Invocation(ctx, d.DeliveryID)
	if err != nil || entry.Invocation == nil || entry.Invocation.Phase != "started" || entry.ReplyDigest != "" || entry.Invocation.Sequence != 1 {
		t.Fatalf("started unknown responsibility not durable: %+v %v", entry, err)
	}
	if err = journal.Close(); err != nil {
		t.Fatal(err)
	}
	journal, err = harness.OpenReplyJournal(path, s.discovery.IdentityScope, s.reg.EndpointID, s.reg.InstanceID, s.reg.Generation)
	if err != nil {
		t.Fatal(err)
	}
	s.owner.journal, receiver.journal = journal, journal
	ws, err = harness.DialWebSocketEndpointWithHTTP(ctx, s.address, s.f.token, s.discovery, false, s.httpClient, s.config(journal, receiver))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline); {
		entry, err = journal.Invocation(ctx, d.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Ack != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if entry.Ack == nil || !entry.Ack.Stored || receiver.invoked.Load() != 1 || receiver.lookedUp.Load() < 1 || entry.Invocation.Sequence != 1 || !api.Equal(entry.Delivery, d) {
		t.Fatalf("unknown replay reexecuted or lost original identity: %+v invoke=%d lookup=%d", entry, receiver.invoked.Load(), receiver.lookedUp.Load())
	}
	var original api.Command
	if err = api.Decode(d.Request, &original); err != nil {
		t.Fatal(err)
	}
	facts, err := s.f.store.List(ctx, rt.Scope{TenantID: s.f.auth.TenantID, OwnerID: s.f.owner, DatabaseID: s.f.store.ID()}, "grpctest.messages", "", "", 10)
	if err != nil || len(facts) != 1 {
		t.Fatal("unknown original receiver physically replayed the command")
	}
	t.Logf("SDK_ENDPOINT_UNKNOWN_EVIDENCE %s", api.Raw(struct {
		DeliveryID, CommandID, OriginalTTL, DatabaseID, JournalScope string
		Sequence, Invokes, Lookups                                   uint64
	}{d.DeliveryID, original.CommandID, original.ExpiresAt, s.f.store.ID(), s.discovery.IdentityScope, entry.Invocation.Sequence, receiver.invoked.Load(), receiver.lookedUp.Load()}))
}

func TestGoWSEndpointLostAckRecoversOriginalReplyAfterInternalRebind(t *testing.T) {
	verifyGoWSEndpointLostAck(t, newUnaryFixture(t))
}
func TestGoWSEndpointPostgresLostAckRecoversOriginalReplyAfterInternalRebind(t *testing.T) {
	verifyGoWSEndpointLostAck(t, newPostgresEndpointFixture(t))
}
func verifyGoWSEndpointLostAck(t *testing.T, f *unaryFixture) {
	t.Helper()
	s := newSDKEndpointSetup(t, f, true)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	journal, err := harness.OpenReplyJournal(t.TempDir(), s.discovery.IdentityScope, s.reg.EndpointID, s.reg.InstanceID, s.reg.Generation)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	s.owner.journal = journal
	receiver := &sdkEndpointReceiver{f: s.f, journal: journal}
	ws, err := harness.DialWebSocketEndpointWithHTTP(ctx, s.address, s.f.token, s.discovery, false, s.httpClient, s.config(journal, receiver))
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	before := s.router.States()
	d := signedStaticDelivery(t, s.f, s.reg, s.keys, s.proofRoot)
	if err = s.servers[0].Deliver(ctx, s.reg.TenantID, d); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s.fault.lost:
	case <-ctx.Done():
		t.Fatal("actual owner did not persist original Reply before lost outer Ack")
	}
	original, err := journal.Invocation(ctx, d.DeliveryID)
	if err != nil || original.ReplyDigest == "" || original.Ack != nil || original.Invocation.Sequence != 1 {
		t.Fatal("SDK lost original Reply responsibility before owner Ack")
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		states := s.router.States()
		if len(states) == 1 && states[0].Connected && states[0].BindingID != before[0].BindingID {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	after := s.router.States()
	if len(after) != 1 || !after[0].Connected || after[0].BindingID == before[0].BindingID || after[0].ConnectionID != before[0].ConnectionID {
		t.Fatal("lost Ack did not keep original external connection while rebinding")
	}
	if partial, err := ws.RecoverEndpoint(ctx); err != nil || partial {
		t.Fatalf("original Reply recovery failed: partial=%v err=%v", partial, err)
	}
	entry := original
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); {
		entry, err = journal.Invocation(ctx, d.DeliveryID)
		if err != nil {
			t.Fatal(err)
		}
		if entry.Ack != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if entry.Ack == nil || !entry.Ack.Stored || entry.ReplyDigest != original.ReplyDigest || !api.Equal(entry.Delivery, d) || entry.Invocation.Sequence != 1 || receiver.invoked.Load() != 1 || receiver.lookedUp.Load() != 0 {
		t.Fatalf("original Reply recovery changed responsibility or reinvoked: %+v", entry)
	}
	t.Logf("SDK_ENDPOINT_ACK_EVIDENCE %s", api.Raw(struct {
		DeliveryID, RequestDigest, ReplyDigest, OriginalTTL, DatabaseID string
		Before, After                                                   endpointchannel.State
		Sequence, Invokes                                               uint64
	}{d.DeliveryID, d.RequestDigest, entry.ReplyDigest, d.DeliverBefore, s.f.store.ID(), before[0], after[0], entry.Invocation.Sequence, receiver.invoked.Load()}))
}
