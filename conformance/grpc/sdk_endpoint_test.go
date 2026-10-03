package grpc_test

import (
	"context"
	"crypto/ecdsa"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/endpointchannel"
	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/platform"
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
}

func (r *sdkEndpointReceiver) Invoke(ctx context.Context, d grpcwire.Delivery) (grpcwire.Reply, error) {
	entry, err := r.journal.Invocation(ctx, d.DeliveryID)
	if err != nil || entry.Invocation == nil || entry.Invocation.Phase != "started" || entry.Invocation.Sequence == 0 || !api.Equal(entry.Delivery, d) || entry.ReplyDigest != "" {
		return grpcwire.Reply{}, api.E("invalid_state", "endpoint_original_not_durable_before_handler")
	}
	r.startedSequence.Store(entry.Invocation.Sequence)
	r.invoked.Add(1)
	kind, raw, err := r.f.processor.Call(ctx, r.f.auth, d.Kind, d.Request)
	return grpcwire.Reply{Type: "reply", DeliveryID: d.DeliveryID, RequestDigest: d.RequestDigest, ResultKind: kind, Payload: raw}, err
}
func (r *sdkEndpointReceiver) Lookup(ctx context.Context, d grpcwire.Delivery) (grpcwire.Reply, bool, error) {
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

func TestGoWSEndpointPersistsOriginalReplyBeforeOwnerAck(t *testing.T) {
	f := newUnaryFixture(t)
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
	discovery, address, httpClient := realChannelGateway(t, f, router)
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
	ws, err := harness.DialWebSocketEndpointWithHTTP(ctx, address, f.token, discovery, false, httpClient, harness.EndpointConfig{TenantID: f.auth.TenantID, IssuerServiceID: f.owner, RecipientServiceID: f.owner, EndpointID: reg.EndpointID, InstanceID: reg.InstanceID, Generation: reg.Generation, IdentityScope: discovery.IdentityScope, IdentityRevision: discovery.IdentityRevision, Profile: api.Profile, SchemaDigest: api.CoreDigest(), Methods: f.discovery.Methods, Keys: map[string]*ecdsa.PublicKey{"development-es256": keys.Keys["development-es256"].Public}, Proofs: originalDeliveryProofs(proofRoot), Current: func(ctx context.Context) error { return f.identity.CheckCurrent(ctx, f.auth) }, Receiver: receiver, Journal: journal})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	d := signedStaticDelivery(t, f, reg, keys, proofRoot)
	if err = servers[0].Deliver(ctx, reg.TenantID, d); err != nil {
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
