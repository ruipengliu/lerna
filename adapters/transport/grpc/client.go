package grpc

import (
	"context"
	"encoding/json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/transport/nodetls"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/internal/taskwire"
	"slices"
	"sync"
	"time"
)

type Client struct {
	conn     *grpc.ClientConn
	endpoint *nodetls.Endpoint
	target   string
	config   Config
	mu       sync.Mutex
	subject  string
	selected *wire.Negotiated
}

func Dial(address, target string, endpoint *nodetls.Endpoint, c Config) (*Client, error) {
	if address == "" || endpoint == nil {
		return nil, failure(authorization.Invalid)
	}
	if e := c.Validate(); e != nil {
		return nil, e
	}
	tlsConfig, e := endpoint.ClientConfig(target)
	if e != nil {
		return nil, e
	}
	conn, e := grpc.NewClient(address, grpc.WithStaticStreamWindowSize(65536), grpc.WithStaticConnWindowSize(65536), grpc.WithReadBufferSize(4096), grpc.WithWriteBufferSize(4096), grpc.WithMaxHeaderListSize(8192), grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig)), grpc.WithDisableRetry(), grpc.WithDisableServiceConfig(), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(c.MaxMessageBytes), grpc.MaxCallSendMsgSize(c.MaxMessageBytes), grpc.MaxRetryRPCBufferSize(0)))
	if e != nil {
		return nil, e
	}
	return &Client{conn: conn, endpoint: endpoint, target: target, config: c}, nil
}
func (c *Client) Close() error { return c.conn.Close() }
func (c *Client) authenticate(ctx context.Context, p peer.Peer) error {
	v, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return failure(authorization.Unauthenticated)
	}
	_, e := c.endpoint.Authenticate(ctx, v.State, c.target, false)
	return e
}
func (c *Client) Negotiate(ctx context.Context, subject string) (*wire.Negotiated, error) {
	ctx, stop := context.WithTimeout(ctx, c.config.IOTimeout)
	defer stop()
	var p peer.Peer
	out, e := wire.NewConnectionServiceClient(c.conn).Negotiate(ctx, &wire.NegotiateRequest{Bootstrap: 1, Versions: []uint32{1}, Required: slices.Clone(capabilities), Subject: subject, MaxMessageBytes: uint32(c.config.MaxMessageBytes), PendingWindow: 1}, grpc.Peer(&p))
	if e != nil {
		return nil, e
	}
	if e = c.authenticate(ctx, p); e != nil {
		return nil, e
	}
	if !taskwire.Known(out.ProtoReflect()) || out.Version != 1 || len(out.Configuration) != 64 || out.MaxMessageBytes < 4096 || out.MaxMessageBytes > uint32(c.config.MaxMessageBytes) || out.PendingWindow != 1 || !slices.Equal(out.Capabilities, capabilities) || out.ExpiresUnixNano <= 0 {
		return nil, failure(authorization.Invalid)
	}
	c.mu.Lock()
	c.subject = subject
	c.selected = proto.Clone(out).(*wire.Negotiated)
	c.mu.Unlock()
	return out, nil
}
func (c *Client) context(ctx context.Context) (context.Context, *wire.Negotiated, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.selected == nil {
		return ctx, nil, "", status.Error(codes.FailedPrecondition, "negotiate required")
	}
	v := proto.Clone(c.selected).(*wire.Negotiated)
	return metadata.AppendToOutgoingContext(ctx, configurationKey, v.Configuration), v, c.subject, nil
}

// Exchange implements sdk.Transport; each branch maps to its named unary RPC.
// A failed possible mutation is conservatively unknown. No application retry is
// performed, including after a configuration expires; callers explicitly refresh.
func (c *Client) Exchange(ctx context.Context, raw []byte) ([]byte, error) {
	if len(raw) > c.config.MaxMessageBytes {
		return nil, failure(authorization.Invalid)
	}
	in := new(wire.CapabilityRequest)
	if proto.Unmarshal(raw, in) != nil || !taskwire.Known(in.ProtoReflect()) {
		return nil, failure(authorization.Invalid)
	}
	ctx, v, _, e := c.context(ctx)
	if e != nil {
		return nil, e
	}
	if len(raw) > int(v.MaxMessageBytes) {
		return nil, failure(authorization.Invalid)
	}
	ctx, stop := context.WithTimeout(ctx, c.config.IOTimeout)
	defer stop()
	api := wire.NewCapabilityServiceClient(c.conn)
	var out *wire.CapabilityResponse
	var p peer.Peer
	mutating := false
	switch in.Body.(type) {
	case *wire.CapabilityRequest_Invoke:
		mutating = true
		out, e = api.Invoke(ctx, in, grpc.Peer(&p))
	case *wire.CapabilityRequest_GetInvocation:
		out, e = api.GetInvocation(ctx, in, grpc.Peer(&p))
	case *wire.CapabilityRequest_Reconcile:
		mutating = true
		out, e = api.Reconcile(ctx, in, grpc.Peer(&p))
	case *wire.CapabilityRequest_RequestCancel:
		mutating = true
		out, e = api.RequestCancel(ctx, in, grpc.Peer(&p))
	case *wire.CapabilityRequest_GetCancel:
		out, e = api.GetCancel(ctx, in, grpc.Peer(&p))
	default:
		return nil, failure(authorization.Unsupported)
	}
	if e != nil {
		if mutating {
			return nil, failure(authorization.OutcomeUnknown)
		}
		return nil, e
	}
	if e = c.authenticate(ctx, p); e != nil {
		if mutating {
			return nil, failure(authorization.OutcomeUnknown)
		}
		return nil, e
	}
	if proto.Size(out) > int(v.MaxMessageBytes) || !taskwire.Known(out.ProtoReflect()) || out.MessageId == "" || out.MessageId == in.MessageId || out.ReplyTo != in.MessageId || out.Namespace != in.Namespace || out.Evidence != "durable_capability_execution" || out.Body == nil || !validReply(in, out) {
		if mutating {
			return nil, failure(authorization.OutcomeUnknown)
		}
		return nil, failure(authorization.Invalid)
	}
	return proto.Marshal(out)
}

// Subscription has one receiver and one sender. Receive persists reliable
// snapshots before acknowledging them. fresh=false denotes durable duplicate
// delivery, not permission to apply an external effect again. Snapshots are
// views; callbacks or arbitrary application side effects are not acknowledged.
type Subscription struct {
	stream    grpc.BidiStreamingClient[wire.ExchangeFrame, wire.ExchangeFrame]
	client    *Client
	journal   *Journal
	key, op   string
	namespace string
	cancel    context.CancelFunc
	last      uint64
	limit     int
}

func (c *Client) Subscribe(ctx context.Context, op string, journal *Journal) (*Subscription, error) {
	if journal == nil || op == "" || len(op) > 512 {
		return nil, failure(authorization.Invalid)
	}
	ctx, v, subject, e := c.context(ctx)
	if e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(ctx, c.config.SessionTTL)
	stream, e := wire.NewConnectionServiceClient(c.conn).Exchange(ctx)
	if e != nil {
		cancel()
		return nil, e
	}
	if e = stream.Send(&wire.ExchangeFrame{Body: &wire.ExchangeFrame_Subscribe{Subscribe: op}}); e != nil {
		cancel()
		return nil, e
	}
	namespace, node := c.endpoint.LocalIdentity()
	scope, _ := json.Marshal([]string{namespace, node, c.target, subject})
	return &Subscription{stream: stream, client: c, journal: journal, key: string(scope), op: op, namespace: namespace, cancel: cancel, limit: int(v.MaxMessageBytes)}, nil
}
func (s *Subscription) Close() { s.cancel() }
func (s *Subscription) Receive() (u *wire.InvocationUpdate, fresh bool, err error) {
	f, e := s.stream.Recv()
	if e != nil {
		return nil, false, e
	}
	ctx := s.stream.Context()
	p, ok := peer.FromContext(ctx)
	if !ok {
		s.Close()
		return nil, false, failure(authorization.Unauthenticated)
	}
	if e = s.client.authenticate(ctx, *p); e != nil {
		s.Close()
		return nil, false, e
	}
	u = f.GetUpdate()
	if !taskwire.Known(f.ProtoReflect()) || proto.Size(f) > s.limit || u == nil || u.OperationId != s.op || u.Seq < 1 || u.Seq < s.last || !u.FullSnapshot || u.Snapshot.GetRevision() != u.Seq || u.Snapshot.GetInvocation().GetOperationId() != s.op || u.Snapshot.GetInvocation().GetQualification().GetTask().GetNamespace() != s.namespace || !execution.ValidRecord(executionwire.Record(u.Snapshot)) {
		s.Close()
		return nil, false, failure(authorization.Invalid)
	}
	fresh = true
	if u.Reliable {
		bounded, stop := context.WithTimeout(ctx, s.client.config.IOTimeout)
		identity, _ := json.Marshal([]string{s.key, u.Snapshot.GetInvocation().GetQualification().GetTask().GetNamespace()})
		fresh, e = s.journal.Save(bounded, string(identity), u)
		stop()
		if e != nil {
			s.Close()
			return nil, false, e
		}
		sent := make(chan error, 1)
		go func() {
			sent <- s.stream.Send(&wire.ExchangeFrame{Body: &wire.ExchangeFrame_Persisted{Persisted: u.Seq}})
		}()
		timer := time.NewTimer(s.client.config.IOTimeout)
		select {
		case e = <-sent:
			timer.Stop()
		case <-timer.C:
			e = context.DeadlineExceeded
		case <-ctx.Done():
			timer.Stop()
			e = ctx.Err()
		}
		if e != nil {
			s.Close()
			return u, fresh, e
		}
	}
	s.last = u.Seq
	return u, fresh, nil
}

// Check the method's business identity before returning bytes to an SDK. A
// malformed mutation response cannot prove whether the operation was accepted.
func validReply(in *wire.CapabilityRequest, out *wire.CapabilityResponse) bool {
	if f := out.GetFailure(); f != nil {
		switch authorization.Code(f.Code) {
		case authorization.Unauthenticated, authorization.Denied, authorization.Invalid, authorization.Unsupported, authorization.Conflict, authorization.IdentityConflict, authorization.Expired, authorization.NotFound, authorization.TimeUntrusted, authorization.Unavailable, authorization.OutcomeUnknown:
			return true
		}
		return false
	}
	switch v := in.Body.(type) {
	case *wire.CapabilityRequest_Invoke:
		return out.GetReceipt() != nil && out.GetReceipt().OperationId == v.Invoke.GetInvocation().GetOperationId() && out.GetReceipt().Revision == 1
	case *wire.CapabilityRequest_RequestCancel:
		return out.GetReceipt() != nil && out.GetReceipt().OperationId == v.RequestCancel.GetOperationId() && out.GetReceipt().Revision == 1
	case *wire.CapabilityRequest_GetInvocation:
		return validSnapshot(in.Namespace, v.GetInvocation, out.GetSnapshot())
	case *wire.CapabilityRequest_Reconcile:
		return validSnapshot(in.Namespace, v.Reconcile, out.GetSnapshot())
	case *wire.CapabilityRequest_GetCancel:
		c := out.GetCancellation()
		if c == nil || c.GetRequest().GetOperationId() != v.GetCancel || c.GetReceipt().GetOperationId() != v.GetCancel || c.GetReceipt().GetRevision() != 1 || c.GetRequest().GetInvocationId() == "" {
			return false
		}
		return slices.Contains([]string{"ACCEPTED", "UNKNOWN", "REQUESTED", "STOPPED", "NOT_PREVENTED", "UNSUPPORTED"}, c.Progress)
	}
	return false
}
func validSnapshot(namespace, op string, s *wire.InvocationSnapshot) bool {
	if s == nil {
		return false
	}
	r := executionwire.Record(s)
	return r.Request.OperationID == op && r.Request.Qualification.Ref.Namespace == namespace && r.Applied <= r.Revision && execution.ValidRecord(r)
}
