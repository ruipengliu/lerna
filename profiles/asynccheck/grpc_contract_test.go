package asynccheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"lerna/adapters/grpcbinding"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"net"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func rawGRPC(t *testing.T, f *networkFixture, address, target string) *grpc.ClientConn {
	t.Helper()
	tc, e := f.endpoint.ClientConfig(target)
	mustGRPC(t, e)
	c, e := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(tc)), grpc.WithDisableRetry(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(65536), grpc.MaxCallSendMsgSize(65536)))
	mustGRPC(t, e)
	t.Cleanup(func() { c.Close() })
	return c
}
func hello() *wire.NegotiateRequest {
	return &wire.NegotiateRequest{Bootstrap: 1, Versions: []uint32{1}, Required: []string{"capability.invoke.v1", "capability.query.v1", "capability.cancel.v1", "invocation.snapshots.v1"}, Subject: "operator", MaxMessageBytes: 65536, PendingWindow: 1}
}
func TestGRPCNegotiationAndAuthentication(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	address := p.address(t)
	raw := rawGRPC(t, f, address, "execution-local")
	api := wire.NewCapabilityServiceClient(raw)
	connection := wire.NewConnectionServiceClient(raw)
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	_, e := api.GetInvocation(ctx, &wire.CapabilityRequest{MessageId: "read", Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: f.request.OperationID}})
	if status.Code(e) != codes.FailedPrecondition {
		t.Fatal("unnegotiated", e)
	}
	for _, change := range []func(*wire.NegotiateRequest){func(v *wire.NegotiateRequest) { v.Versions = []uint32{2} }, func(v *wire.NegotiateRequest) { v.Required = []string{"unsupported"} }, func(v *wire.NegotiateRequest) { v.PendingWindow = 0 }, func(v *wire.NegotiateRequest) { v.Subject = "other" }} {
		h := hello()
		change(h)
		if _, e = connection.Negotiate(ctx, h); e == nil {
			t.Fatal("invalid negotiation accepted")
		}
	}
	selected, e := connection.Negotiate(ctx, hello())
	mustGRPC(t, e)
	bound := metadata.AppendToOutgoingContext(ctx, "harness-configuration", selected.Configuration)
	// Another enrolled TLS principal cannot borrow this configuration. Its
	// public enrollment is valid; only the authenticated presenter differs.
	otherTLS, e := f.endpoint.ClientConfig("execution-local")
	mustGRPC(t, e)
	otherCertificate, e := tls.LoadX509KeyPair(filepath.Join(f.server, "node.crt"), filepath.Join(f.server, "node.key"))
	mustGRPC(t, e)
	otherTLS.Certificates = []tls.Certificate{otherCertificate}
	other, e := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(otherTLS)))
	mustGRPC(t, e)
	_, e = wire.NewCapabilityServiceClient(other).GetInvocation(bound, &wire.CapabilityRequest{MessageId: "borrowed", Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: f.request.OperationID}})
	other.Close()
	if status.Code(e) != codes.PermissionDenied {
		t.Fatal("configuration borrowed by other peer", e)
	}
	mismatch, e := api.GetInvocation(bound, &wire.CapabilityRequest{MessageId: "wrong", Namespace: "local", Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{}}})
	mustGRPC(t, e)
	if mismatch.GetFailure().GetCode() != "INVALID_ARGUMENT" {
		t.Fatal(mismatch)
	}
	_, e = api.List(bound, &wire.CapabilityRequest{})
	if status.Code(e) != codes.Unimplemented {
		t.Fatal("unsupported method", e)
	}
	noKey, e := f.endpoint.ClientConfig("execution-local")
	mustGRPC(t, e)
	noKey.Certificates = nil
	noCert, e := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(noKey)))
	mustGRPC(t, e)
	defer noCert.Close()
	spoof := metadata.AppendToOutgoingContext(ctx, "node", "local-host", "subject", "operator")
	if _, e = wire.NewConnectionServiceClient(noCert).Negotiate(spoof, hello()); e == nil {
		t.Fatal("headers replaced client certificate")
	}
	wrong := rawGRPC(t, f, address, "wrong-node")
	if _, e = wire.NewConnectionServiceClient(wrong).Negotiate(ctx, hello()); e == nil {
		t.Fatal("wrong server identity accepted")
	}
	// Removing client trust cannot be repaired by a claimed server name.
	untrusted := &tls.Config{MinVersion: tls.VersionTLS13}
	noTrust, e := grpc.NewClient(address, grpc.WithTransportCredentials(credentials.NewTLS(untrusted)))
	mustGRPC(t, e)
	defer noTrust.Close()
	if _, e = wire.NewConnectionServiceClient(noTrust).Negotiate(ctx, hello()); e == nil {
		t.Fatal("untrusted root accepted")
	}
	p.control(t, "advance", "")
	if _, e = api.GetInvocation(bound, &wire.CapabilityRequest{MessageId: "expired", Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: f.request.OperationID}}); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("expired configuration", e)
	}
	renewed, e := connection.Negotiate(ctx, hello())
	mustGRPC(t, e)
	if renewed.Configuration == selected.Configuration {
		t.Fatal("configuration reused")
	}
	p.stop()
	p = launchServer(t, f.server)
	restarted := rawGRPC(t, f, p.address(t), "execution-local")
	stale := metadata.AppendToOutgoingContext(ctx, "harness-configuration", renewed.Configuration)
	if _, e = wire.NewCapabilityServiceClient(restarted).GetInvocation(stale, &wire.CapabilityRequest{MessageId: "old-config", Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: f.request.OperationID}}); status.Code(e) != codes.FailedPrecondition {
		t.Fatal("restart retained old configuration", e)
	}

}
func TestGRPCCurrentAuthorizationStopsDisclosure(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	c, api := connect(t, f, p.address(t))
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_, e := api.Invoke(ctx, f.request, f.material)
	mustGRPC(t, e)
	j, e := grpcbinding.OpenJournal(filepath.Join(f.client, "inbox.db"), 64)
	mustGRPC(t, e)
	defer j.Close()
	sub, e := c.Subscribe(ctx, f.request.OperationID, j)
	mustGRPC(t, e)
	defer sub.Close()
	_, _, e = sub.Receive()
	mustGRPC(t, e)
	p.control(t, "disable", "")
	if _, e = api.GetInvocation(ctx, f.request.OperationID); e == nil {
		t.Fatal("disabled peer read result")
	}
	if _, _, e = sub.Receive(); e == nil {
		t.Fatal("disabled subscriber remained authorized")
	}
	if _, e = c.Negotiate(ctx, "operator"); e == nil {
		t.Fatal("disabled peer negotiated")
	}
}
func TestGRPCCancelConformance(t *testing.T) {
	for _, when := range []string{"before", "during", "completed"} {
		t.Run(when, func(t *testing.T) {
			type observation struct {
				initial, final string
				effect         string
				changes        int
			}
			run := func(network bool) observation {
				f := prepareNetwork(t)
				ctx := context.Background()
				var api *sdk.CapabilityClient
				var p *serverProcess
				if network {
					f.h.close()
					p = launchServer(t, f.server)
					_, api = connect(t, f, p.address(t))
				} else {
					api = sdk.NewCapabilityClient(grpcbinding.Local(f.h.exec, "local"), "local")
				}
				control := func(kind, op string) {
					if network {
						p.control(t, kind, op)
						return
					}
					var e error
					switch kind {
					case "start":
						_, e = f.h.exec.Run(ctx, op)
					case "complete":
						e = f.h.target.Complete(ctx, op)
						if e == nil {
							f.h.clock.advance(time.Second)
							_, e = f.h.exec.Reconcile(ctx, op)
						}
					case "cancel":
						_, e = f.h.exec.RunCancel(ctx, op)
					}
					mustGRPC(t, e)
				}
				_, e := api.Invoke(ctx, f.request, f.material)
				mustGRPC(t, e)
				if when != "before" {
					control("start", f.request.OperationID)
				}
				if when == "completed" {
					control("complete", f.request.OperationID)
				}
				receipt, e := api.RequestCancel(ctx, execution.CancelRequest{OperationID: f.cancel, Invocation: f.request.OperationID})
				mustGRPC(t, e)
				if receipt.OperationID == f.request.OperationID {
					t.Fatal("cancel reused invocation identity")
				}
				initial, e := api.GetCancel(ctx, f.cancel)
				mustGRPC(t, e)
				control("cancel", f.cancel)
				after, e := api.GetCancel(ctx, f.cancel)
				mustGRPC(t, e)
				r, e := api.GetInvocation(ctx, f.request.OperationID)
				mustGRPC(t, e)
				h := f.h
				if network {
					p.stop()
					h, _ = openNetwork(t, f.server)
				}
				defer h.close()
				truth, e := h.target.Snapshot(ctx)
				mustGRPC(t, e)
				return observation{initial.Progress, after.Progress, r.Effect, truth.Changes}
			}
			local, remote := run(false), run(true)
			if local != remote {
				t.Fatalf("local=%+v gRPC=%+v", local, remote)
			}
			want := 0
			if when == "completed" {
				want = 1
			}
			if remote.changes != want {
				t.Fatal(remote)
			}
		})
	}
}
func TestGRPCSlowConsumerAndUnacknowledgedRecovery(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	address := p.address(t)
	_, api := connect(t, f, address)
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	_, e := api.Invoke(ctx, f.request, f.material)
	mustGRPC(t, e)
	p.control(t, "start", f.request.OperationID)
	p.control(t, "complete", f.request.OperationID)
	raw := rawGRPC(t, f, address, "execution-local")
	conn := wire.NewConnectionServiceClient(raw)
	selected, e := conn.Negotiate(ctx, hello())
	mustGRPC(t, e)
	bound := metadata.AppendToOutgoingContext(ctx, "harness-configuration", selected.Configuration)
	stream, e := conn.Exchange(bound)
	mustGRPC(t, e)
	mustGRPC(t, stream.Send(&wire.ExchangeFrame{Body: &wire.ExchangeFrame_Subscribe{Subscribe: f.request.OperationID}}))
	result, e := stream.Recv()
	mustGRPC(t, e)
	if !result.GetUpdate().GetReliable() {
		t.Fatal(result)
	}
	// No PERSISTED is sent. The server must stop waiting at its declared bound.
	begin := time.Now()
	_, e = stream.Recv()
	if status.Code(e) != codes.DeadlineExceeded || time.Since(begin) > 3*time.Second {
		t.Fatal("unbounded receipt wait", e)
	}
	p.stop()
	p = launchServer(t, f.server)
	c, _ := connect(t, f, p.address(t))
	j, e := grpcbinding.OpenJournal(filepath.Join(f.client, "inbox.db"), 64)
	mustGRPC(t, e)
	defer j.Close()
	sub, e := c.Subscribe(ctx, f.request.OperationID, j)
	mustGRPC(t, e)
	defer sub.Close()
	u, fresh, e := sub.Receive()
	mustGRPC(t, e)
	if !fresh || u.Seq != result.GetUpdate().Seq {
		t.Fatal("unacknowledged result lost", u)
	}
}

func TestGRPCBoundedConcurrentStreams(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	address := p.address(t)
	c, api := connect(t, f, address)
	ctx, stop := context.WithTimeout(context.Background(), 8*time.Second)
	defer stop()
	_, e := api.Invoke(ctx, f.request, f.material)
	mustGRPC(t, e)
	j, e := grpcbinding.OpenJournal(filepath.Join(f.client, "inbox.db"), 64)
	mustGRPC(t, e)
	defer j.Close()
	subs := make([]*grpcbinding.Subscription, 4)
	for i := range subs {
		subs[i], e = c.Subscribe(ctx, f.request.OperationID, j)
		mustGRPC(t, e)
		defer subs[i].Close()
		_, _, e = subs[i].Receive()
		mustGRPC(t, e)
	}
	// A different connection avoids the client's native per-connection stream
	// queue, so this probes the server's installation-wide admission bound.
	extra := rawGRPC(t, f, address, "execution-local")
	connection := wire.NewConnectionServiceClient(extra)
	selected, e := connection.Negotiate(ctx, hello())
	mustGRPC(t, e)
	bound := metadata.AppendToOutgoingContext(ctx, "harness-configuration", selected.Configuration)
	overflow, e := connection.Exchange(bound)
	mustGRPC(t, e)
	_ = overflow.Send(&wire.ExchangeFrame{Body: &wire.ExchangeFrame_Subscribe{Subscribe: f.request.OperationID}})
	if _, e = overflow.Recv(); status.Code(e) != codes.ResourceExhausted {
		t.Fatal("stream capacity", e)
	}
	p.control(t, "start", f.request.OperationID)
	for _, sub := range subs {
		u, _, e := receivePhase(sub, "IN_PROGRESS")
		mustGRPC(t, e)
		if u.Snapshot.Phase != "IN_PROGRESS" {
			t.Fatal(u)
		}
	}
	_, e = api.RequestCancel(ctx, execution.CancelRequest{OperationID: f.cancel, Invocation: f.request.OperationID})
	mustGRPC(t, e) // control retains capacity beside four subscriptions
	p.control(t, "complete", f.request.OperationID)
	complete := make(chan error, 4)
	for _, sub := range subs {
		go func(sub *grpcbinding.Subscription) {
			u, _, e := receivePhase(sub, "FINISHED")
			if e == nil && u.Snapshot.Effect != "CONFIRMED" {
				e = fmt.Errorf("unexpected result")
			}
			complete <- e
		}(sub)
	}
	for range subs {
		select {
		case e := <-complete:
			mustGRPC(t, e)
		case <-ctx.Done():
			t.Fatal("duplex progress deadlocked")
		}
	}
}

func TestGRPCRevocationStopsExistingSubscription(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	c, api := connect(t, f, p.address(t))
	ctx, stop := context.WithTimeout(context.Background(), 5*time.Second)
	defer stop()
	_, e := api.Invoke(ctx, f.request, f.material)
	mustGRPC(t, e)
	j, e := grpcbinding.OpenJournal(filepath.Join(f.client, "inbox.db"), 64)
	mustGRPC(t, e)
	defer j.Close()
	sub, e := c.Subscribe(ctx, f.request.OperationID, j)
	mustGRPC(t, e)
	defer sub.Close()
	_, _, e = sub.Receive()
	mustGRPC(t, e)
	p.control(t, "revoke", f.request.OperationID)
	if _, e = api.GetInvocation(ctx, f.request.OperationID); !authorization.Is(e, authorization.Denied) {
		t.Fatal("revoked result", e)
	}
	if _, e = api.Invoke(ctx, f.request, f.material); !authorization.Is(e, authorization.Denied) {
		t.Fatal("revoked replay", e)
	}
	if _, _, e = sub.Receive(); e == nil {
		t.Fatal("revoked subscription disclosed")
	}
}
func TestGRPCInvocationConformance(t *testing.T) {
	run := func(network bool) []string {
		f := prepareNetwork(t)
		ctx := context.Background()
		api := sdk.NewCapabilityClient(grpcbinding.Local(f.h.exec, "local"), "local")
		var p *serverProcess
		if network {
			f.h.close()
			p = launchServer(t, f.server)
			_, api = connect(t, f, p.address(t))
		}
		observations := []string{}
		_, e := api.Invoke(ctx, f.request, "invalid")
		if !authorization.Is(e, authorization.Denied) {
			t.Fatal("unauthorized invoke", e)
		}
		observations = append(observations, "DENIED")
		receipt, e := api.Invoke(ctx, f.request, f.material)
		mustGRPC(t, e)
		again, e := api.Invoke(ctx, f.request, f.material)
		mustGRPC(t, e)
		if again != receipt {
			t.Fatal("replay")
		}
		changed := f.request
		changed.InputRef = "different"
		_, e = api.Invoke(ctx, changed, f.material)
		if !authorization.Is(e, authorization.IdentityConflict) {
			t.Fatal("identity conflict", e)
		}
		observations = append(observations, "IDENTITY_CONFLICT")
		r, e := api.GetInvocation(ctx, f.request.OperationID)
		mustGRPC(t, e)
		observations = append(observations, r.Phase, r.Result, r.Effect)
		if network {
			p.control(t, "start", f.request.OperationID)
		} else {
			_, e = f.h.exec.Run(ctx, f.request.OperationID)
			mustGRPC(t, e)
		}
		r, e = api.GetInvocation(ctx, f.request.OperationID)
		mustGRPC(t, e)
		observations = append(observations, r.Phase, r.Result, r.Effect)
		if network {
			p.control(t, "complete", f.request.OperationID)
		} else {
			mustGRPC(t, f.h.target.Complete(ctx, f.request.OperationID))
			f.h.clock.advance(time.Second)
			_, e = f.h.exec.Reconcile(ctx, f.request.OperationID)
			mustGRPC(t, e)
		}
		r, e = api.GetInvocation(ctx, f.request.OperationID)
		mustGRPC(t, e)
		observations = append(observations, r.Phase, r.Result, r.Effect)
		if network {
			p.stop()
		} else {
			f.h.close()
		}
		return observations
	}
	local, remote := run(false), run(true)
	if !slices.Equal(local, remote) {
		t.Fatalf("local %v gRPC %v", local, remote)
	}
}

func TestGRPCIncompleteHandshakeIsBounded(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	address := p.address(t)
	slow, e := net.Dial("tcp", address)
	mustGRPC(t, e)
	defer slow.Close()
	mustGRPC(t, slow.SetReadDeadline(time.Now().Add(3*time.Second)))
	var b [1]byte
	begin := time.Now()
	_, e = slow.Read(b[:])
	if e == nil || time.Since(begin) > 2*time.Second {
		t.Fatal("incomplete handshake exceeded budget", e)
	}
	if timeout, ok := e.(net.Error); ok && timeout.Timeout() {
		t.Fatal("client timed out before server released connection")
	}
	_, api := connect(t, f, address)
	_, e = api.Invoke(context.Background(), f.request, f.material)
	mustGRPC(t, e)
}
