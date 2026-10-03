package grpc_test

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	rt "github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type endpointAuthority struct {
	f         *unaryFixture
	reg       transport.EndpointRegistration
	mu        sync.Mutex
	proofRoot string
	keys      *platform.Keyring
	loseOnce  atomic.Bool
}

func (p *endpointAuthority) Bind(ctx context.Context, gateway string, a rt.Auth, b grpcwire.Bind) (transport.EndpointRegistration, error) {
	if gateway != "spiffe://harness.test/gateway" || a.SubjectID != p.f.auth.SubjectID || b.EndpointID != p.reg.EndpointID || b.InstanceID != p.reg.InstanceID || b.EndpointGeneration != p.reg.Generation {
		return transport.EndpointRegistration{}, api.E("forbidden", "endpoint_pairing_mismatch")
	}
	return p.reg, nil
}
func (p *endpointAuthority) Check(ctx context.Context, r transport.EndpointRegistration) error {
	if !api.Equal(r, p.reg) {
		return api.E("forbidden", "endpoint_pairing_changed")
	}
	return p.f.identity.CheckCurrent(ctx, p.f.auth)
}
func (p *endpointAuthority) VerifyDelivery(ctx context.Context, reg transport.EndpointRegistration, d grpcwire.Delivery) error {
	if p.keys == nil {
		return api.E("unsupported", "delivery_proof_not_configured")
	}
	if d.SenderServiceID != p.f.owner || d.RecipientEndpointID != reg.EndpointID || d.RecipientInstanceID != reg.InstanceID || d.ProofRef.TenantID != reg.TenantID || d.ProofRef.OwnerID != p.f.owner {
		return api.E("forbidden", "delivery_scope_mismatch")
	}
	body, e := os.ReadFile(filepath.Join(p.proofRoot, d.ProofRef.ContentID))
	if e != nil {
		return e
	}
	if api.Hash(body) != d.ProofRef.Hash || uint64(len(body)) != d.ProofRef.ByteLength {
		return api.E("forbidden", "proof_bytes_mismatch")
	}
	copy := d
	copy.ProofRef = api.ContentRef{}
	digest, e := api.Digest(copy)
	if e != nil {
		return e
	}
	_, e = p.keys.Verify(string(body), platform.ProofClaims{TenantID: reg.TenantID, Issuer: d.SenderServiceID, Audience: reg.EndpointID, Purpose: "delivery", ObjectRef: api.ObjectRef{TenantID: reg.TenantID, OwnerID: d.SenderServiceID, ObjectID: d.DeliveryID, Revision: 1}, Digest: digest, ControlRevision: reg.Generation, WindowID: d.DeliveryID, StartBefore: d.DeliverBefore}, time.Now())
	return e
}
func (p *endpointAuthority) ReceiveReply(ctx context.Context, r transport.EndpointRegistration, d grpcwire.Delivery, reply grpcwire.Reply) (bool, error) {
	state, e := p.f.store.Within(ctx, rt.Scope{TenantID: r.TenantID, OwnerID: p.f.owner, DatabaseID: p.f.store.ID()}, []string{"replyowner"}, func(tx rt.Tx) error {
		var old grpcwire.Reply
		_, e := tx.Get(ctx, "replyowner.received", d.DeliveryID, &old)
		if e == nil {
			if !api.Equal(old, reply) {
				return api.E("idempotency_conflict", "owner_reply_changed")
			}
			return nil
		}
		if !api.IsCode(e, "not_found") {
			return e
		}
		return tx.Create(ctx, "replyowner.received", d.DeliveryID, r.EndpointID, reply)
	})
	if state == rt.CommitUnknown {
		return false, rt.ErrCommitUnknown
	}
	if e != nil {
		return false, e
	}
	if p.loseOnce.CompareAndSwap(true, false) {
		return false, api.E("dependency_unavailable", "owner_reply_lost_after_commit")
	}
	return true, nil
}
func (p *endpointAuthority) ReadReply(ctx context.Context, id string) (grpcwire.Reply, error) {
	var reply grpcwire.Reply
	_, e := p.f.store.Read(ctx, rt.Scope{TenantID: p.f.auth.TenantID, OwnerID: p.f.owner, DatabaseID: p.f.store.ID()}, "replyowner.received", id, 0, &reply)
	return reply, e
}
func serveEndpoint(t *testing.T, f *unaryFixture, p transport.EndpointAuthority) (*transport.Server, string) {
	t.Helper()
	s, e := transport.New(transport.Config{OwnerID: f.owner, Identity: f.identity, Processor: f.processor, Store: f.store, MethodsDigest: f.discovery.MethodsDigest, ApplicationInstanceID: api.NewID("instance"), EndpointAuthority: p, GatewayIdentities: []string{"spiffe://harness.test/gateway"}})
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.Serve(ctx, l, f.serverTLS) }()
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(7 * time.Second):
			t.Error("endpoint server did not actually exit")
		}
	})
	return s, l.Addr().String()
}
func channelClient(t *testing.T, f *unaryFixture, address string) (rpcv1.HarnessServiceClient, func()) {
	t.Helper()
	cc, e := grpcgo.NewClient(address, grpcgo.WithTransportCredentials(credentials.NewTLS(f.clientTLS)), grpcgo.WithDisableRetry(), grpcgo.WithDefaultCallOptions(grpcgo.ForceCodec(grpcwire.Codec{})))
	if e != nil {
		t.Fatal(e)
	}
	return rpcv1.NewHarnessServiceClient(cc), func() { cc.Close() }
}
func openFor(f *unaryFixture, p *endpointAuthority) grpcwire.Bind {
	return grpcwire.Bind{Type: "bind", LogicalServiceID: f.owner, ConnectionID: api.NewID("connection"), GatewayInstanceID: api.NewID("instance"), EndpointID: p.reg.EndpointID, InstanceID: p.reg.InstanceID, EndpointGeneration: p.reg.Generation, Protocol: api.Protocol, Profile: api.Profile, TransportProfile: grpcwire.EndpointProfile, MethodsDigest: f.discovery.MethodsDigest}
}
func sendFrame(t *testing.T, st grpcgo.BidiStreamingClient[rpcv1.ChannelFrame, rpcv1.ChannelFrame], id string, rev uint64, body any) {
	t.Helper()
	if e := st.Send(&rpcv1.ChannelFrame{BindingId: id, BindingRevision: rev, FrameJson: api.Raw(body)}); e != nil {
		t.Fatal(e)
	}
}
func readyFrame(t *testing.T, st grpcgo.BidiStreamingClient[rpcv1.ChannelFrame, rpcv1.ChannelFrame], id string, rev uint64) grpcwire.Ready {
	t.Helper()
	msg, e := st.Recv()
	if e != nil {
		t.Fatal(e)
	}
	decoded, e := grpcwire.DecodeFrame(msg.FrameJson)
	if e != nil {
		t.Fatal(e)
	}
	ready, ok := decoded.(*grpcwire.Ready)
	if !ok || msg.BindingId != id || msg.BindingRevision != rev {
		t.Fatalf("ready binding mismatch %+v", msg)
	}
	return *ready
}
func TestEndpointChannelMTLSBindingRebindRetainsSequenceAndOriginalSender(t *testing.T) {
	f := newUnaryFixture(t)
	p := &endpointAuthority{f: f, reg: transport.EndpointRegistration{TenantID: f.auth.TenantID, SubjectID: f.auth.SubjectID, CredentialGeneration: 1, EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), Generation: 1, RecipientServiceID: f.owner}}
	_, address := serveEndpoint(t, f, p)
	rpc, closeClient := channelClient(t, f, address)
	defer closeClient()
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+f.token), 4*time.Second)
	defer cancel()
	first, e := rpc.EndpointChannel(ctx)
	if e != nil {
		t.Fatal(e)
	}
	bind := openFor(f, p)
	id1 := api.NewID("binding")
	sendFrame(t, first, id1, 1, bind)
	ready := readyFrame(t, first, id1, 1)
	if ready.ConnectionID != bind.ConnectionID || ready.EndpointID != p.reg.EndpointID {
		t.Fatal("ready changed external endpoint identity")
	}
	c := commandFor(f)
	sendFrame(t, first, id1, 1, grpcwire.Request{Type: "request", RequestSeq: 1, Kind: "command", Payload: api.Raw(c)})
	msg, e := first.Recv()
	if e != nil {
		t.Fatal(e)
	}
	var response grpcwire.Response
	if e = api.Decode(msg.FrameJson, &response); e != nil {
		t.Fatal(e)
	}
	var receipt api.Receipt
	if e = api.Decode(response.Payload, &receipt); e != nil || receipt.Stage != "applied" {
		t.Fatalf("original command %+v %v", receipt, e)
	}
	second, e := rpc.EndpointChannel(ctx)
	if e != nil {
		t.Fatal(e)
	}
	id2 := api.NewID("binding")
	sendFrame(t, second, id2, 2, bind)
	readyFrame(t, second, id2, 2)
	if _, e = first.Recv(); e == nil {
		t.Fatal("superseded native stream remained live")
	}
	lookup := api.ReceiptLookup{LogicalServiceID: f.owner, CommandID: c.CommandID}
	sendFrame(t, second, id2, 2, grpcwire.Request{Type: "request", RequestSeq: 2, Kind: "receipt_lookup", Payload: api.Raw(lookup)})
	msg, e = second.Recv()
	if e != nil {
		t.Fatal(e)
	}
	if e = api.Decode(msg.FrameJson, &response); e != nil {
		t.Fatal(e)
	}
	var recovered api.Receipt
	if e = api.Decode(response.Payload, &recovered); e != nil || !api.Equal(receipt, recovered) {
		t.Fatalf("rebind lost original command: %+v %v", recovered, e)
	}
	// 原外 connection 的序号不能因换内部流复用。
	sendFrame(t, second, id2, 2, grpcwire.Request{Type: "request", RequestSeq: 1, Kind: "receipt_lookup", Payload: api.Raw(lookup)})
	if _, e = second.Recv(); e == nil {
		t.Fatal("old sequence accepted after rebind")
	}
	third, e := rpc.EndpointChannel(ctx)
	if e != nil {
		t.Fatal(e)
	}
	sendFrame(t, third, api.NewID("binding"), 1, bind)
	if _, e = third.Recv(); e == nil {
		t.Fatal("lower binding generation replaced new stream")
	}
}

func TestEndpointChannelRejectsMissingGatewayCertificateAndUnpairedEndpoint(t *testing.T) {
	f := newUnaryFixture(t)
	p := &endpointAuthority{f: f, reg: transport.EndpointRegistration{TenantID: f.auth.TenantID, SubjectID: f.auth.SubjectID, CredentialGeneration: 1, EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), Generation: 1, RecipientServiceID: f.owner}}
	_, address := serveEndpoint(t, f, p)
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+f.token), 3*time.Second)
	defer cancel()
	for _, missingCert := range []bool{true, false} {
		cfg := f.clientTLS.Clone()
		bind := openFor(f, p)
		if missingCert {
			cfg.Certificates = nil
		} else {
			bind.EndpointID = api.NewID("endpoint")
		}
		cc, e := grpcgo.NewClient(address, grpcgo.WithTransportCredentials(credentials.NewTLS(cfg)), grpcgo.WithDefaultCallOptions(grpcgo.ForceCodec(grpcwire.Codec{})))
		if e != nil {
			t.Fatal(e)
		}
		stream, e := rpcv1.NewHarnessServiceClient(cc).EndpointChannel(ctx)
		if e != nil {
			cc.Close()
			continue
		}
		_ = stream.Send(&rpcv1.ChannelFrame{BindingId: api.NewID("binding"), BindingRevision: 1, FrameJson: api.Raw(bind)})
		if _, e = stream.Recv(); e == nil {
			cc.Close()
			t.Fatal("missing gateway mTLS or unpaired endpoint accepted")
		}
		cc.Close()
	}
}

func TestEndpointDeliveryLostOwnerReplyKeepsOriginalDurableReplyUntilMatchingAck(t *testing.T) {
	f := newUnaryFixture(t)
	p := &endpointAuthority{f: f, reg: transport.EndpointRegistration{TenantID: f.auth.TenantID, SubjectID: f.auth.SubjectID, CredentialGeneration: 1, EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), Generation: 1, RecipientServiceID: f.owner}, proofRoot: t.TempDir()}
	var e error
	p.keys, e = platform.NewDevelopmentKey(f.auth.TenantID, f.owner, []string{"delivery"})
	if e != nil {
		t.Fatal(e)
	}
	s, address := serveEndpoint(t, f, p)
	rpc, closeClient := channelClient(t, f, address)
	defer closeClient()
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+f.token), 5*time.Second)
	defer cancel()
	stream, e := rpc.EndpointChannel(ctx)
	if e != nil {
		t.Fatal(e)
	}
	bind := openFor(f, p)
	id := api.NewID("binding")
	sendFrame(t, stream, id, 1, bind)
	readyFrame(t, stream, id, 1)
	command := commandFor(f)
	requestDigest, _ := api.Digest(command)
	d := grpcwire.Delivery{Type: "delivery", DeliveryID: api.NewID("delivery"), SenderServiceID: f.owner, RecipientEndpointID: p.reg.EndpointID, RecipientInstanceID: p.reg.InstanceID, RequestDigest: requestDigest, Kind: "command", Request: api.Raw(command), DeliverBefore: api.Time(time.Now().Add(time.Minute))}
	unsignedDigest, _ := api.Digest(d)
	claims := platform.ProofClaims{TenantID: f.auth.TenantID, Issuer: f.owner, Audience: p.reg.EndpointID, Purpose: "delivery", ObjectRef: api.ObjectRef{TenantID: f.auth.TenantID, OwnerID: f.owner, ObjectID: d.DeliveryID, Revision: 1}, Digest: unsignedDigest, ControlRevision: 1, WindowID: d.DeliveryID, IssuedAt: api.Time(time.Now().Add(-time.Second)), StartBefore: d.DeliverBefore}
	compact, e := p.keys.Sign("development-es256", claims)
	if e != nil {
		t.Fatal(e)
	}
	proofID := api.NewID("content")
	if e = os.WriteFile(filepath.Join(p.proofRoot, proofID), []byte(compact), 0600); e != nil {
		t.Fatal(e)
	}
	d.ProofRef = api.ContentRef{TenantID: f.auth.TenantID, OwnerID: f.owner, ContentID: proofID, Version: 1, Hash: api.Hash([]byte(compact)), MediaType: "application/jose", ByteLength: uint64(len(compact))}
	tampered := d
	changedCommand := command
	changedCommand.Payload = api.Raw(echoInput{"unsigned changed body"})
	tampered.Request = api.Raw(changedCommand)
	if e = s.Deliver(ctx, f.auth.TenantID, tampered); e == nil {
		t.Fatal("unsigned changed request accepted")
	}
	tampered = d
	tampered.DeliverBefore = api.Time(time.Now().Add(2 * time.Hour))
	if e = s.Deliver(ctx, f.auth.TenantID, tampered); e == nil {
		t.Fatal("changed signature window accepted")
	}
	if e = s.Deliver(ctx, f.auth.TenantID, d); e != nil {
		t.Fatal(e)
	}
	msg, e := stream.Recv()
	if e != nil {
		t.Fatal(e)
	}
	var delivered grpcwire.Delivery
	if e = api.Decode(msg.FrameJson, &delivered); e != nil || !api.Equal(d, delivered) {
		t.Fatalf("accurate original delivery %+v %v", delivered, e)
	}
	if e = p.VerifyDelivery(ctx, p.reg, delivered); e != nil {
		t.Fatal(e)
	}
	kind, result, e := f.processor.Call(ctx, f.auth, "command", delivered.Request)
	if e != nil {
		t.Fatal(e)
	}
	reply := grpcwire.Reply{Type: "reply", DeliveryID: d.DeliveryID, RequestDigest: d.RequestDigest, ResultKind: kind, Payload: result}
	journalPath := filepath.Join(t.TempDir(), "reply-journal")
	journal, e := harness.OpenReplyJournal(journalPath, f.discovery.IdentityScope, p.reg.EndpointID, p.reg.InstanceID, p.reg.Generation)
	if e != nil {
		t.Fatal(e)
	}
	if e = journal.Save(ctx, d, reply); e != nil {
		t.Fatal(e)
	}
	p.loseOnce.Store(true)
	sendFrame(t, stream, id, 1, reply)
	if _, e = stream.Recv(); e == nil {
		t.Fatal("acknowledged owner response whose commit confirmation was lost")
	}
	stored, e := p.ReadReply(ctx, d.DeliveryID)
	if e != nil || !api.Equal(stored, reply) {
		t.Fatalf("original owner responsibility not durable %+v %v", stored, e)
	}
	if e = journal.Close(); e != nil {
		t.Fatal(e)
	}
	journal, e = harness.OpenReplyJournal(journalPath, f.discovery.IdentityScope, p.reg.EndpointID, p.reg.InstanceID, p.reg.Generation)
	if e != nil {
		t.Fatal(e)
	}
	defer journal.Close()
	pending, partial, e := journal.Pending(ctx, 32)
	if e != nil || partial || len(pending) != 1 || !api.Equal(pending[0].Reply, reply) {
		t.Fatalf("durable pending reply %+v %v %v", pending, partial, e)
	}
	stream, e = rpc.EndpointChannel(ctx)
	if e != nil {
		t.Fatal(e)
	}
	id = api.NewID("binding")
	sendFrame(t, stream, id, 2, bind)
	readyFrame(t, stream, id, 2)
	sendFrame(t, stream, id, 2, pending[0].Reply)
	msg, e = stream.Recv()
	if e != nil {
		t.Fatal(e)
	}
	var ack grpcwire.ReplyAck
	if e = api.Decode(msg.FrameJson, &ack); e != nil || !ack.Stored || ack.DeliveryID != d.DeliveryID || ack.RequestDigest != d.RequestDigest {
		t.Fatalf("accurate durable ack %+v %v", ack, e)
	}
	wrong := ack
	wrong.RequestDigest = api.Hash([]byte("wrong"))
	if e = journal.Acknowledge(ctx, wrong); e == nil {
		t.Fatal("foreign ack cleared original reply")
	}
	if e = journal.Acknowledge(ctx, ack); e != nil {
		t.Fatal(e)
	}
	pending, partial, e = journal.Pending(ctx, 32)
	if e != nil || partial || len(pending) != 0 {
		t.Fatal("matching confirmed ack did not close original reply")
	}
	stored, e = p.ReadReply(ctx, d.DeliveryID)
	if e != nil || !api.Equal(stored, reply) {
		t.Fatal("repeated reply changed owner decision")
	}
}
