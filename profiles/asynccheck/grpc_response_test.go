package asynccheck

import (
	"context"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"net"
	"strings"
	"testing"
)

type malformedCapability struct {
	wire.UnimplementedCapabilityServiceServer
	wire.UnimplementedConnectionServiceServer
	kind string
}

func (s *malformedCapability) Negotiate(context.Context, *wire.NegotiateRequest) (*wire.Negotiated, error) {
	return &wire.Negotiated{Configuration: strings.Repeat("a", 64), Version: 1, Capabilities: hello().Required, MaxMessageBytes: 65536, PendingWindow: 1, ExpiresUnixNano: 1}, nil
}
func (s *malformedCapability) Invoke(_ context.Context, in *wire.CapabilityRequest) (*wire.CapabilityResponse, error) {
	out := &wire.CapabilityResponse{MessageId: "server-reply", ReplyTo: in.MessageId, Namespace: in.Namespace, Evidence: "durable_capability_execution"}
	switch s.kind {
	case "branch":
		out.Body = &wire.CapabilityResponse_Snapshot{Snapshot: &wire.InvocationSnapshot{}}
	case "operation":
		out.Body = &wire.CapabilityResponse_Receipt{Receipt: &wire.InvocationReceipt{OperationId: "different-operation", Revision: 1}}
	case "revision":
		out.Body = &wire.CapabilityResponse_Receipt{Receipt: &wire.InvocationReceipt{OperationId: in.GetInvoke().GetInvocation().GetOperationId(), Revision: 2}}
	case "failure":
		out.Body = &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: "made-up"}}
	}
	return out, nil
}
func TestGRPCMalformedMutationResponseIsUnknown(t *testing.T) {
	for _, kind := range []string{"branch", "operation", "revision", "failure"} {
		t.Run(kind, func(t *testing.T) {
			f := prepareNetwork(t)
			defer f.h.close()
			ep := endpoint(t, f.server, "execution-local", f.h.auth, f.h.clock)
			service := &malformedCapability{kind: kind}
			server := grpc.NewServer(grpc.Creds(credentials.NewTLS(ep.ServerConfig())))
			wire.RegisterCapabilityServiceServer(server, service)
			wire.RegisterConnectionServiceServer(server, service)
			listener, e := net.Listen("tcp", "127.0.0.1:0")
			mustGRPC(t, e)
			go server.Serve(listener)
			defer server.Stop()
			_, api := connect(t, f, listener.Addr().String())
			_, e = api.Invoke(context.Background(), f.request, f.material)
			if !authorization.Is(e, authorization.OutcomeUnknown) {
				t.Fatalf("%s response incorrectly implied rejection: %v", kind, e)
			}
		})
	}
}
