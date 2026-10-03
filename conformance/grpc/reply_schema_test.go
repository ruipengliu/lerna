package grpc_test

import (
	"context"
	"testing"
	"time"

	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
	"google.golang.org/grpc/metadata"
)

func TestStaticEndpointRejectsFirstInvalidOutputWithoutPoisoningOriginalReply(t *testing.T) {
	verifyFirstInvalidEndpointReply(t, newUnaryFixture(t))
}
func TestPostgresStaticEndpointRejectsFirstInvalidOutputWithoutPoisoningOriginalReply(t *testing.T) {
	verifyFirstInvalidEndpointReply(t, newPostgresEndpointFixture(t))
}

func verifyFirstInvalidEndpointReply(t *testing.T, f *unaryFixture) {
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
	server, address := serveEndpoint(t, f, authority)
	rpc, closeClient := channelClient(t, f, address)
	defer closeClient()
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+f.token), 8*time.Second)
	defer cancel()
	first, err := rpc.EndpointChannel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	bind := openFor(f, receiver)
	firstID := api.NewID("binding")
	sendFrame(t, first, firstID, 1, bind)
	readyFrame(t, first, firstID, 1)
	d := signedStaticDelivery(t, f, reg, keys, proofRoot)
	if err = server.Deliver(ctx, reg.TenantID, d); err != nil {
		t.Fatal(err)
	}
	msg, err := first.Recv()
	if err != nil {
		t.Fatal(err)
	}
	frame, err := grpcwire.DecodeFrame(msg.FrameJson)
	delivered, ok := frame.(*grpcwire.Delivery)
	if err != nil || !ok || !api.Equal(*delivered, d) {
		t.Fatal("actual native Channel did not deliver original signed input")
	}
	kind, output, err := f.processor.Call(ctx, f.auth, d.Kind, d.Request)
	if err != nil {
		t.Fatal(err)
	}
	reply := grpcwire.Reply{Type: "reply", DeliveryID: d.DeliveryID, RequestDigest: d.RequestDigest, ResultKind: kind, Payload: output}
	var badReceipt api.Receipt
	if err = api.Decode(output, &badReceipt); err != nil {
		t.Fatal(err)
	}
	badReceipt.Output = api.Raw(map[string]any{"message": 23, "subject_id": f.auth.SubjectID})
	badReply := reply
	badReply.Payload = api.Raw(badReceipt)
	// 外层仍是准确原receipt；只有recipient的方法输出Schema可以拒绝此输入。
	if err = grpcwire.ValidateReply(d, badReply); err != nil {
		t.Fatal("fixture did not reach method-output Schema boundary")
	}
	sendFrame(t, first, firstID, 1, badReply)
	if _, err = first.Recv(); err == nil {
		t.Fatal("invalid first output received Ack")
	}
	if _, err = receiver.ReadReply(ctx, d.DeliveryID); !api.IsCode(err, "not_found") {
		t.Fatal("invalid output reached original business Reply owner")
	}
	second, err := rpc.EndpointChannel(ctx)
	if err != nil {
		t.Fatal(err)
	}
	secondID := api.NewID("binding")
	sendFrame(t, second, secondID, 2, bind)
	readyFrame(t, second, secondID, 2)
	sendFrame(t, second, secondID, 2, reply)
	var ack *grpcwire.ReplyAck
	for range 2 {
		msg, err = second.Recv()
		if err != nil {
			t.Fatalf("invalid first output poisoned original valid Reply recovery: %v", err)
		}
		frame, err = grpcwire.DecodeFrame(msg.FrameJson)
		if err != nil {
			t.Fatal(err)
		}
		if candidate, ok := frame.(*grpcwire.ReplyAck); ok {
			ack = candidate
			break
		}
		if recovered, ok := frame.(*grpcwire.Delivery); !ok || !api.Equal(*recovered, d) {
			t.Fatal("rebind changed original Delivery")
		}
	}
	if ack == nil || !ack.Stored || ack.DeliveryID != d.DeliveryID || ack.RequestDigest != d.RequestDigest {
		t.Fatal("original valid Reply was not acknowledged after invalid input")
	}
	stored, err := receiver.ReadReply(ctx, d.DeliveryID)
	if err != nil || !api.Equal(stored, reply) {
		t.Fatal("source owner did not persist exact original valid Reply")
	}
	replyDigest, err := api.Digest(reply)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("CHANNEL_FIRST_REPLY_SCHEMA_EVIDENCE %s", api.Raw(struct {
		DeliveryID, ConnectionID, DatabaseID, ReplyDigest, OriginalTTL string
		ProofRef                                                       api.ContentRef
		FirstBindingID, ReplacementBindingID                           string
	}{d.DeliveryID, bind.ConnectionID, f.store.ID(), replyDigest, d.DeliverBefore, d.ProofRef, firstID, secondID}))
}
