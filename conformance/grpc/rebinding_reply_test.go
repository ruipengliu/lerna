package grpc_test

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
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

type originalDeliveryProofs string

func (p originalDeliveryProofs) ReadDeliveryProof(ctx context.Context, ref api.ContentRef) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(string(p), ref.ContentID))
}
func (p *endpointAuthority) ReceiveDeliveryReply(ctx context.Context, r transport.EndpointRegistration, d grpcwire.Delivery, reply grpcwire.Reply) (bool, error) {
	return p.ReceiveReply(ctx, r, d, reply)
}
func realDeliveryApplications(t *testing.T, f *unaryFixture, authority transport.EndpointAuthority) ([]*transport.Server, []string) {
	t.Helper()
	servers := []*transport.Server{}
	addresses := []string{}
	for range 2 {
		s, err := transport.New(transport.Config{OwnerID: f.owner, Identity: f.identity, Processor: f.processor, Store: f.store, MethodsDigest: f.discovery.MethodsDigest, ApplicationInstanceID: api.NewID("instance"), EndpointAuthority: authority, GatewayIdentities: []string{"spiffe://harness.test/gateway"}})
		if err != nil {
			t.Fatal(err)
		}
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- s.Serve(ctx, listener, f.serverTLS) }()
		var once sync.Once
		t.Cleanup(func() {
			once.Do(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(7 * time.Second):
					t.Error("actual delivery application did not exit")
				}
			})
		})
		servers = append(servers, s)
		addresses = append(addresses, "grpcs://"+listener.Addr().String())
	}
	return servers, addresses
}
func signedStaticDelivery(t *testing.T, f *unaryFixture, reg transport.EndpointRegistration, keys *platform.Keyring, proofRoot string, windows ...time.Time) grpcwire.Delivery {
	t.Helper()
	command := commandFor(f)
	digest, err := api.Digest(command)
	if err != nil {
		t.Fatal(err)
	}
	until := time.Now().Add(time.Minute)
	if len(windows) == 1 {
		until = windows[0]
	}
	d := grpcwire.Delivery{Type: "delivery", DeliveryID: api.NewID("delivery"), SenderServiceID: f.owner, RecipientEndpointID: reg.EndpointID, RecipientInstanceID: reg.InstanceID, RequestDigest: digest, Kind: "command", Request: api.Raw(command), DeliverBefore: api.Time(until)}
	intentDigest, err := transport.DeliveryIntentDigest(d)
	if err != nil {
		t.Fatal(err)
	}
	compact, err := keys.Sign("development-es256", transport.DeliveryProofClaims(reg, d, intentDigest, api.Time(time.Now().Add(-time.Second))))
	if err != nil {
		t.Fatal(err)
	}
	id := api.NewID("content")
	if err = os.WriteFile(filepath.Join(proofRoot, id), []byte(compact), 0600); err != nil {
		t.Fatal(err)
	}
	d.ProofRef = api.ContentRef{TenantID: reg.TenantID, OwnerID: f.owner, ContentID: id, Version: 1, Hash: api.Hash([]byte(compact)), MediaType: "application/jose", ByteLength: uint64(len(compact))}
	return d
}

type loseOuterAck struct {
	*endpointchannel.Router
	lost chan struct{}
	once atomic.Bool
}

func (p *loseOuterAck) Open(ctx context.Context, a rt.Auth, info wss.ConnectionInfo) (wss.Connection, error) {
	emit := info.EmitChecked
	info.EmitChecked = func(raw json.RawMessage, control bool, valid func(context.Context) (func(), error)) error {
		frame, err := grpcwire.DecodeFrame(raw)
		if err != nil {
			return err
		}
		if _, ok := frame.(*grpcwire.ReplyAck); ok && p.once.CompareAndSwap(false, true) {
			close(p.lost)
			return api.E("dependency_unavailable", "outer_ack_lost_before_socket_write")
		}
		return emit(raw, control, valid)
	}
	return p.Router.Open(ctx, a, info)
}

func TestWSSStaticSignedDeliveryReplySurvivesLostOuterAckAndRebind(t *testing.T) {
	verifyStaticSignedDeliveryReply(t, newUnaryFixture(t))
}
func TestWSSPostgresStaticSignedDeliveryReplySurvivesLostOuterAckAndRebind(t *testing.T) {
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
	verifyStaticSignedDeliveryReply(t, newUnaryFixtureWithStore(t, store))
}
func verifyStaticSignedDeliveryReply(t *testing.T, f *unaryFixture) {
	t.Helper()
	_, reg := newStaticChannel(t, f)
	keys, err := platform.NewDevelopmentKey(f.auth.TenantID, f.owner, []string{"delivery"})
	if err != nil {
		t.Fatal(err)
	}
	proofRoot := t.TempDir()
	receiver := &endpointAuthority{f: f, reg: reg}
	authority, err := transport.NewStaticEndpointAuthority(transport.StaticEndpointAuthorityConfig{OwnerID: f.owner, GatewayIdentities: []string{"spiffe://harness.test/gateway"}, Identity: f.identity, Pairs: []transport.StaticEndpointPair{{Registration: reg, Methods: f.discovery.Methods}}, Keys: keys, Proofs: originalDeliveryProofs(proofRoot), Replies: receiver})
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
	fault := &loseOuterAck{Router: router, lost: make(chan struct{})}
	discovery, address, httpClient := realChannelGateway(t, f, router, fault)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, address, &websocket.DialOptions{HTTPClient: httpClient, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + f.token}}, Subprotocols: []string{"harness-wss.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	_, raw, err := conn.Read(ctx)
	var ready harness.WSReady
	if err != nil || api.Decode(raw, &ready) != nil || ready.Type != "ready" || ready.IdentityScope != discovery.IdentityScope || ready.MethodsDigest != discovery.MethodsDigest {
		t.Fatal("native endpoint did not receive the one accurate outer Ready")
	}
	d := signedStaticDelivery(t, f, reg, keys, proofRoot)
	tampered := d
	tampered.DeliverBefore = api.Time(time.Now().Add(2 * time.Hour))
	if err = servers[0].Deliver(ctx, reg.TenantID, tampered); err == nil {
		t.Fatal("changed signed deadline delivered")
	}
	tampered = d
	tampered.RecipientInstanceID = api.NewID("instance")
	if err = authority.VerifyDelivery(ctx, reg, tampered); err == nil {
		t.Fatal("changed recipient instance delivered")
	}
	if err = servers[0].Deliver(ctx, reg.TenantID, d); err != nil {
		t.Fatal(err)
	}
	_, raw, err = conn.Read(ctx)
	decoded, decodeErr := grpcwire.DecodeFrame(raw)
	delivered, ok := decoded.(*grpcwire.Delivery)
	if err != nil || decodeErr != nil || !ok || !api.Equal(*delivered, d) {
		t.Fatal("native endpoint did not receive original signed delivery")
	}
	if err = authority.VerifyDelivery(ctx, reg, *delivered); err != nil {
		t.Fatal(err)
	}
	kind, output, err := f.processor.Call(ctx, f.auth, delivered.Kind, delivered.Request)
	if err != nil {
		t.Fatal(err)
	}
	reply := grpcwire.Reply{Type: "reply", DeliveryID: d.DeliveryID, RequestDigest: d.RequestDigest, ResultKind: kind, Payload: output}
	journalPath := t.TempDir()
	journal, err := harness.OpenReplyJournal(journalPath, discovery.IdentityScope, reg.EndpointID, reg.InstanceID, reg.Generation)
	if err != nil {
		t.Fatal(err)
	}
	if err = journal.Save(ctx, d, reply); err != nil {
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
	before := router.States()
	if err = conn.Write(ctx, websocket.MessageText, api.Raw(reply)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fault.lost:
	case <-ctx.Done():
		t.Fatal("owner did not reach the exact lost outer Ack fault")
	}
	stored, err := receiver.ReadReply(ctx, d.DeliveryID)
	if err != nil || !api.Equal(stored, reply) {
		t.Fatal("owner Reply was not durable before lost outer Ack")
	}
	for deadline := time.Now().Add(2 * time.Second); time.Now().Before(deadline); {
		states := router.States()
		if len(states) == 1 && states[0].Connected && states[0].BindingID != before[0].BindingID {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if states := router.States(); len(states) != 1 || !states[0].Connected || states[0].BindingID == before[0].BindingID {
		t.Fatal("lost Ack did not rebind original connection")
	}
	pending, partial, err := journal.Pending(ctx, 32)
	if err != nil || partial || len(pending) != 1 || !api.Equal(pending[0].Reply, reply) {
		t.Fatal("original endpoint Reply responsibility disappeared before Ack")
	}
	if err = conn.Write(ctx, websocket.MessageText, api.Raw(pending[0].Reply)); err != nil {
		t.Fatal(err)
	}
	_, raw, err = conn.Read(ctx)
	if err != nil {
		t.Fatalf("matching original Reply was rejected after lost Ack: %v", err)
	}
	decoded, err = grpcwire.DecodeFrame(raw)
	ack, ok := decoded.(*grpcwire.ReplyAck)
	if err != nil || !ok || ack.DeliveryID != d.DeliveryID || ack.RequestDigest != d.RequestDigest || !ack.Stored {
		t.Fatalf("original durable Reply Ack mismatch: %s %v", raw, err)
	}
	wrong := *ack
	wrong.RequestDigest = api.Hash([]byte("foreign request"))
	if err = journal.Acknowledge(ctx, wrong); err == nil {
		t.Fatal("wrong Ack closed original endpoint responsibility")
	}
	if err = journal.Acknowledge(ctx, *ack); err != nil {
		t.Fatal(err)
	}
	if pending, partial, err = journal.Pending(ctx, 32); err != nil || partial || len(pending) != 0 {
		t.Fatal("matching owner Ack did not close durable endpoint responsibility")
	}
	stored, err = receiver.ReadReply(ctx, d.DeliveryID)
	if err != nil || !api.Equal(stored, reply) {
		t.Fatal("repeated original Reply changed owner decision")
	}
	t.Logf("CHANNEL_REPLY_EVIDENCE %s", api.Raw(struct {
		DeliveryID, RequestDigest, OriginalTTL, DatabaseID, TenantID string
		ProofRef                                                     api.ContentRef
		First, Replacement                                           endpointchannel.State
	}{d.DeliveryID, d.RequestDigest, d.DeliverBefore, f.store.ID(), f.auth.TenantID, d.ProofRef, before[0], router.States()[0]}))
	forged := reply
	var falseReceipt api.Receipt
	if err = api.Decode(reply.Payload, &falseReceipt); err != nil {
		t.Fatal(err)
	}
	falseReceipt.Output = api.Raw(echoOutput{Message: "forged result", SubjectID: f.auth.SubjectID})
	forged.Payload = api.Raw(falseReceipt)
	if err = conn.Write(ctx, websocket.MessageText, api.Raw(forged)); err != nil {
		t.Fatal(err)
	}
	noAckCtx, noAckStop := context.WithTimeout(ctx, 250*time.Millisecond)
	_, raw, err = conn.Read(noAckCtx)
	noAckStop()
	if err == nil {
		t.Fatalf("forged changed Reply was acknowledged: %s", raw)
	}
	stored, err = receiver.ReadReply(ctx, d.DeliveryID)
	if err != nil || !api.Equal(stored, reply) {
		t.Fatal("forged changed Reply overwrote the original owner decision")
	}
	conn.CloseNow()
	router.Close()
}
