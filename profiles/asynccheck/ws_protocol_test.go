package asynccheck

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/gorilla/websocket"
	"google.golang.org/protobuf/proto"
	cataloglocal "lerna/adapters/catalog/local"
	wsbinding "lerna/adapters/transport/ws"
	"lerna/authorization"
	"lerna/catalog"
	wire "lerna/gen/harness/v1"
	"lerna/internal/executionwire"
	"lerna/internal/randomid"
	"lerna/schema"
	"lerna/sdk"
	"net"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type wsPair struct {
	client, server wsbinding.Host
	s              *wsbinding.Server
	edge, cloud    *harness
	d              wsDisk
	address        string
}

func newWSPair(t *testing.T) *wsPair {
	t.Helper()
	a, b := prepareWS(t)
	edge, ch, d := openWS(t, a)
	cloud, sh, _ := openWS(t, b)
	t.Cleanup(edge.close)
	t.Cleanup(cloud.close)
	s, e := wsbinding.NewServer(sh)
	mustGRPC(t, e)
	t.Cleanup(func() { s.Close() })
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	return &wsPair{client: ch, server: sh, s: s, edge: edge, cloud: cloud, d: d, address: "wss://" + l.Addr().String() + "/harness"}
}

type rawWS struct {
	c   *websocket.Conn
	seq [6]uint64
}

func (p *wsPair) raw(t *testing.T) *rawWS {
	t.Helper()
	cfg, e := p.client.Endpoint.ClientConfig("cloud")
	mustGRPC(t, e)
	d := websocket.Dialer{TLSClientConfig: cfg, Subprotocols: []string{"harness.bootstrap.v1"}, HandshakeTimeout: time.Second, WriteBufferSize: 256}
	c, _, e := d.Dial(p.address, nil)
	mustGRPC(t, e)
	t.Cleanup(func() { c.Close() })
	return &rawWS{c: c}
}
func rawWrite(t *testing.T, c *websocket.Conn, e *wire.Envelope) {
	t.Helper()
	b, err := proto.Marshal(e)
	mustGRPC(t, err)
	mustGRPC(t, c.SetWriteDeadline(time.Now().Add(2*time.Second)))
	mustGRPC(t, c.WriteMessage(websocket.BinaryMessage, b))
}
func rawRead(t *testing.T, c *websocket.Conn) *wire.Envelope {
	t.Helper()
	mustGRPC(t, c.SetReadDeadline(time.Now().Add(3*time.Second)))
	_, b, e := c.ReadMessage()
	mustGRPC(t, e)
	v := new(wire.Envelope)
	mustGRPC(t, proto.Unmarshal(b, v))
	return v
}
func helloWS() *wire.WSHello {
	return &wire.WSHello{Bootstrap: 1, Versions: []uint32{1}, Required: []string{"catalog.read.v1", "invocation.read.v1", "progress.v1", "chunks.v1"}, Subject: "operator", MaxMessage: 65536, Chunk: 1024, Window: 4, TimeoutMs: 2000, LifetimeMs: 60000, Nonce: strings.Repeat("a", 64)}
}
func (r *rawWS) negotiate(t *testing.T) {
	t.Helper()
	offer := helloWS()
	rawWrite(t, r.c, &wire.Envelope{Body: &wire.Envelope_Hello{Hello: offer}})
	other := rawRead(t, r.c).GetHello()
	if other == nil {
		t.Fatal("missing hello")
	}
	rawWrite(t, r.c, &wire.Envelope{Body: &wire.Envelope_Welcome{Welcome: &wire.WSWelcome{PeerNonce: other.Nonce, Version: 1, Capabilities: offer.Required, MaxMessage: offer.MaxMessage, Chunk: offer.Chunk, Window: offer.Window, TimeoutMs: offer.TimeoutMs, LifetimeMs: offer.LifetimeMs}}})
	if rawRead(t, r.c).GetWelcome() == nil {
		t.Fatal("missing welcome")
	}
}
func (r *rawWS) stamp(e *wire.Envelope, id uint32, class string) {
	r.seq[id]++
	e.StreamId = id
	e.Seq = r.seq[id]
	e.DeliveryClass = class
	e.ProtocolMajor = 1
	e.Namespace = "local"
	if e.MessageId == "" {
		e.MessageId, _ = randomid.New()
	}
}
func (r *rawWS) rejected(t *testing.T) {
	t.Helper()
	r.c.SetReadDeadline(time.Now().Add(4 * time.Second))
	for i := 0; i < 32; i++ {
		_, b, e := r.c.ReadMessage()
		if e != nil {
			if ne, ok := e.(net.Error); ok && ne.Timeout() {
				t.Fatal("peer did not reject before deadline")
			}
			return
		}
		v := new(wire.Envelope)
		proto.Unmarshal(b, v)
		if v.GetAnswer() != nil && v.GetAnswer().GetFailure() == nil {
			t.Fatal("malformed request succeeded")
		}
	}
	t.Fatal("connection remained open")
}
func queryWS(id, op string) *wire.Envelope {
	return &wire.Envelope{MessageId: id, Body: &wire.Envelope_Query{Query: &wire.CapabilityRequest{MessageId: id, Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: op}}}}
}
func TestWSNegotiationAndIdentity(t *testing.T) {
	for _, name := range []string{"version", "required", "optional", "limit", "subject", "unnegotiated", "wrong-node", "no-certificate", "origin"} {
		t.Run(name, func(t *testing.T) {
			p := newWSPair(t)
			if name == "wrong-node" {
				_, e := p.client.Dial(context.Background(), p.address, "edge")
				if e == nil {
					t.Fatal("wrong node accepted")
				}
				return
			}
			if name == "no-certificate" {
				cfg, _ := p.client.Endpoint.ClientConfig("cloud")
				cfg.Certificates = nil
				d := websocket.Dialer{TLSClientConfig: cfg, Subprotocols: []string{"harness.bootstrap.v1"}}
				c, _, e := d.Dial(p.address, nil)
				if e == nil {
					c.Close()
					t.Fatal("no certificate accepted")
				}
				return
			}
			if name == "origin" {
				cfg, _ := p.client.Endpoint.ClientConfig("cloud")
				d := websocket.Dialer{TLSClientConfig: cfg, Subprotocols: []string{"harness.bootstrap.v1"}}
				c, _, e := d.Dial(p.address, map[string][]string{"Origin": {"https://attacker.invalid"}})
				if e == nil {
					c.Close()
					t.Fatal("browser origin accepted")
				}
				return
			}
			if name == "optional" {
				p.client.Required = []string{"chunks.v1"}
				p.client.Optional = []string{"unknown.v1"} // server requirements intentionally still need query support
				_, e := p.client.Dial(context.Background(), p.address, "cloud")
				if e == nil {
					t.Fatal("missing peer required capabilities accepted")
				}
				return
			}
			r := p.raw(t)
			h := helloWS()
			switch name {
			case "version":
				h.Versions = []uint32{2}
			case "required":
				h.Required = append(h.Required, "mutation.v1")
			case "limit":
				h.Chunk = 0
			case "subject":
				h.Subject = "administrator"
			case "unnegotiated":
				e := queryWS("early", p.d.RemoteOperation)
				r.stamp(e, 2, "query")
				rawWrite(t, r.c, e)
				r.rejected(t)
				return
			}
			rawWrite(t, r.c, &wire.Envelope{Body: &wire.Envelope_Hello{Hello: h}})
			r.rejected(t)
		})
	}
}
func TestWSMalformedAndChunkBounds(t *testing.T) {
	for _, name := range []string{"text", "unknown-body", "unknown-type", "schema-version", "schema-digest", "json", "sequence", "namespace", "correlation", "chunk-size", "chunk-total", "chunk-offset", "chunk-aggregate", "chunk-count", "chunk-timeout", "frame-fragments"} {
		t.Run(name, func(t *testing.T) {
			p := newWSPair(t)
			r := p.raw(t)
			r.negotiate(t)
			e := queryWS("request", p.d.RemoteOperation)
			r.stamp(e, 2, "query")
			switch name {
			case "text":
				mustGRPC(t, r.c.WriteMessage(websocket.TextMessage, []byte("{}")))
				r.rejected(t)
				return
			case "unknown-body":
				e.Body = &wire.Envelope_Request{Request: &wire.SubmitRequest{}}
			case "sequence":
				e.Seq = 2
			case "namespace":
				e.Namespace = "elsewhere"
			case "correlation":
				e.GetQuery().MessageId = "other"
			case "unknown-type", "schema-version", "schema-digest", "json":
				cap := capability()
				payload := &wire.DynamicPayload{TypeName: cap.Input.Type, SchemaId: cap.Input.ID, SchemaVersion: cap.Input.Version, SchemaDigest: schema.Digest(cap.Input.Document), Json: []byte(`{"delta":1}`)}
				switch name {
				case "unknown-type":
					payload.TypeName = "unknown"
				case "schema-version":
					payload.SchemaVersion = "2"
				case "schema-digest":
					payload.SchemaDigest = strings.Repeat("0", 64)
				case "json":
					payload.Json = []byte(`{"delta":1,"delta":2}`)
				}
				e.Body = &wire.Envelope_Extension{Extension: payload}
			case "frame-fragments":
				// Multiple legal RFC frames exceed the cumulative binary-message limit.
				w, err := r.c.NextWriter(websocket.BinaryMessage)
				mustGRPC(t, err)
				_, err = w.Write(make([]byte, 2048))
				mustGRPC(t, err)
				_ = w.Close()
				r.rejected(t)
				return
			default:
				part := &wire.WSChunk{MessageId: "assembly", Total: 2048, Data: make([]byte, 512)}
				e = &wire.Envelope{Body: &wire.Envelope_Chunk{Chunk: part}}
				r.stamp(e, 5, "content")
				switch name {
				case "chunk-size":
					part.Data = make([]byte, 1025)
				case "chunk-total":
					part.Total = 65537
				case "chunk-offset":
					part.Offset = 1
				case "chunk-aggregate":
					part.Total = 65536
					rawWrite(t, r.c, e)
					e = proto.Clone(e).(*wire.Envelope)
					e.GetChunk().MessageId = "second"
					e.MessageId = ""
					r.stamp(e, 5, "content")
				case "chunk-count":
					for i := 0; i < 4; i++ {
						part.MessageId = fmt.Sprint("assembly-", i)
						rawWrite(t, r.c, e)
						e = proto.Clone(e).(*wire.Envelope)
						part = e.GetChunk()
						e.MessageId = ""
						r.stamp(e, 5, "content")
					}
					part.MessageId = "fifth"
				case "chunk-timeout":
				}
			}
			rawWrite(t, r.c, e)
			r.rejected(t)
		})
	}
}
func TestWSReadOnlyAndFragmentedQuery(t *testing.T) {
	p := newWSPair(t)
	r := p.raw(t)
	r.negotiate(t)
	e := queryWS("fragmented", p.d.RemoteOperation)
	e.GetQuery().Body = &wire.CapabilityRequest_Search{Search: &wire.CatalogQuery{Text: strings.Repeat("counter ", 100), Purpose: "task", Location: "local", Limit: 4, Budget: 8}}
	r.stamp(e, 2, "query")
	b, err := proto.Marshal(e)
	mustGRPC(t, err)
	w, err := r.c.NextWriter(websocket.BinaryMessage)
	mustGRPC(t, err)
	_, err = w.Write(b[:len(b)/2])
	mustGRPC(t, err)
	_, err = w.Write(b[len(b)/2:])
	mustGRPC(t, err)
	mustGRPC(t, w.Close())
	out := rawRead(t, r.c)
	if len(out.GetAnswer().GetCatalogPage().GetItems()) != 1 {
		t.Fatal(out)
	}
	e = queryWS("mutation", p.d.RemoteOperation)
	e.GetQuery().Body = &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{}}
	r.stamp(e, 2, "query")
	rawWrite(t, r.c, e)
	out = rawRead(t, r.c)
	if out.GetAnswer().GetFailure().GetCode() != string(authorization.Unsupported) {
		t.Fatal(out)
	}
}
func TestWSRestartAndDisabledPeer(t *testing.T) {
	p := newWSPair(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	c, e := p.client.Dial(ctx, p.address, "cloud")
	mustGRPC(t, e)
	mustGRPC(t, wsQueries(ctx, c, p.d.RemoteOperation))
	c.Close()
	c, e = p.client.Dial(ctx, p.address, "cloud")
	mustGRPC(t, e)
	defer c.Close()
	mustGRPC(t, wsQueries(ctx, c, p.d.RemoteOperation))
	nodes, e := p.cloud.auth.Nodes(nodeConfig(), noIssuance{})
	mustGRPC(t, e)
	snap, e := p.cloud.db.Load(ctx)
	mustGRPC(t, e)
	op, e := p.cloud.operation(ctx)
	mustGRPC(t, e)
	_, e = nodes.Mutate(ctx, p.cloud.token, authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "edge", Kind: "DISABLE", ExpectedRevision: snap.State.Revision})
	mustGRPC(t, e)
	_, e = sdk.NewCapabilityClient(c, "local").GetInvocation(ctx, p.d.RemoteOperation)
	if e == nil {
		t.Fatal("disabled node read state")
	}
	_, e = p.client.Dial(ctx, p.address, "cloud")
	if e == nil {
		t.Fatal("disabled node reconnected")
	}
}

func TestWSAssociationAndLateReadReplies(t *testing.T) {
	for _, bad := range []bool{false, true} {
		t.Run(fmt.Sprint("wrong-body=", bad), func(t *testing.T) {
			p := newWSPair(t)
			r := p.raw(t)
			r.negotiate(t)
			ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
			defer cancel()
			server, e := p.s.Accept(ctx)
			mustGRPC(t, e)
			done := make(chan error, 1)
			go func() {
				_, e := sdk.NewCapabilityClient(server, "local").GetInvocation(ctx, p.d.Request.OperationID)
				done <- e
			}()
			q := rawRead(t, r.c).GetQuery()
			if q == nil {
				t.Fatal("missing query")
			}
			record, e := p.edge.exec.GetInvocation(ctx, p.d.Request.OperationID)
			mustGRPC(t, e)
			// A late unrelated read reply is ignored, and cannot satisfy the live request.
			id := "reply"
			out := &wire.CapabilityResponse{MessageId: id, ReplyTo: "expired-request", Namespace: "local", Evidence: "durable_capability_execution", Body: &wire.CapabilityResponse_Snapshot{Snapshot: executionwire.Snapshot(record)}}
			envelope := &wire.Envelope{MessageId: id, ReplyTo: &out.ReplyTo, Body: &wire.Envelope_Answer{Answer: out}}
			r.stamp(envelope, 3, "response")
			rawWrite(t, r.c, envelope)
			out = proto.Clone(out).(*wire.CapabilityResponse)
			out.MessageId = "current-reply"
			out.ReplyTo = q.MessageId
			if bad {
				out.GetSnapshot().Invocation.OperationId = "wrong-operation"
			}
			envelope = &wire.Envelope{MessageId: out.MessageId, ReplyTo: &out.ReplyTo, Body: &wire.Envelope_Answer{Answer: out}}
			r.stamp(envelope, 3, "response")
			rawWrite(t, r.c, envelope)
			e = <-done
			if bad && e == nil {
				t.Fatal("wrong operation accepted")
			}
			if !bad {
				mustGRPC(t, e)
			}
		})
	}
}
func TestWSConnectionExpiryAndBackpressure(t *testing.T) {
	t.Run("expiry", func(t *testing.T) {
		p := newWSPair(t)
		p.client.Config.Lifetime = 2 * time.Second
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, e := p.client.Dial(ctx, p.address, "cloud")
		mustGRPC(t, e)
		defer c.Close()
		select {
		case <-c.Done():
		case <-ctx.Done():
			t.Fatal("configuration never expired")
		}
		_, e = sdk.NewCapabilityClient(c, "local").GetInvocation(ctx, p.d.RemoteOperation)
		if e == nil {
			t.Fatal("expired connection served query")
		}
	})
	t.Run("subscription-budget", func(t *testing.T) {
		p := newWSPair(t)
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		c, e := p.client.Dial(ctx, p.address, "cloud")
		mustGRPC(t, e)
		defer c.Close()
		for range 4 {
			sub, e := c.Subscribe(ctx, p.d.RemoteOperation)
			mustGRPC(t, e)
			defer sub.Close()
			_, e = sub.Receive(ctx)
			mustGRPC(t, e)
		}
		if _, e = c.Subscribe(ctx, p.d.RemoteOperation); !authorization.Is(e, authorization.Unavailable) {
			t.Fatalf("subscription cap: %v", e)
		}
		mustGRPC(t, wsQueries(ctx, c, p.d.RemoteOperation))
	})
	t.Run("slow-peer-queue", func(t *testing.T) {
		p := newWSPair(t)
		r := p.raw(t)
		r.negotiate(t)
		// Do not consume replies while flooding read requests. The bounded incoming
		// queue must end the connection, never accumulate an unbounded backlog.
		r.c.SetWriteDeadline(time.Now().Add(3 * time.Second))
		for i := 0; i < 128; i++ {
			e := queryWS(fmt.Sprintf("flood-%d", i), p.d.RemoteOperation)
			r.stamp(e, 2, "query")
			b, _ := proto.Marshal(e)
			if err := r.c.WriteMessage(websocket.BinaryMessage, b); err != nil {
				break
			}
		}
		r.c.SetReadDeadline(time.Now().Add(4 * time.Second))
		closed := false
		for range 256 {
			_, _, e := r.c.ReadMessage()
			if e != nil {
				if ne, ok := e.(net.Error); ok && ne.Timeout() {
					t.Fatal("queue pressure did not terminate")
				}
				closed = true
				break
			}
		}
		if !closed {
			t.Fatal("unbounded flood consumed")
		}
	})
}

// Force the actual TLS socket's send buffer small enough that a peer which
// stops reading cannot absorb all queued messages in the kernel.
type tightListener struct{ net.Listener }

func (l tightListener) Accept() (net.Conn, error) {
	c, e := l.Listener.Accept()
	if e == nil {
		e = c.(*net.TCPConn).SetWriteBuffer(1024)
		if e != nil {
			c.Close()
		}
	}
	return c, e
}
func TestWSBlockedSocketWrite(t *testing.T) {
	a, b := prepareWS(t)
	h, ch, _ := openWS(t, a)
	defer h.close()
	remote, sh, _ := openWS(t, b)
	defer remote.close()
	s, e := wsbinding.NewServer(sh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(tightListener{l})
	pair := &wsPair{client: ch, address: "wss://" + l.Addr().String() + "/harness"}
	r := pair.raw(t)
	r.negotiate(t)
	mustGRPC(t, r.c.UnderlyingConn().(*tls.Conn).NetConn().(*net.TCPConn).SetReadBuffer(1024))
	ctx, cancel := context.WithTimeout(context.Background(), 7*time.Second)
	defer cancel()
	p, e := s.Accept(ctx)
	mustGRPC(t, e)
	for i := 0; i < 4; i++ {
		go func(i int) {
			q := &wire.CapabilityRequest{MessageId: fmt.Sprintf("blocked-%d", i), Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: strings.Repeat("x", 60000)}}
			b, _ := proto.Marshal(q)
			p.Exchange(ctx, b)
		}(i)
	}
	select {
	case <-p.Done():
	case <-ctx.Done():
		t.Fatal("blocked writer did not stop")
	}
	if ne, ok := p.Err().(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("expected actual socket write timeout, got %v", p.Err())
	}
}

func TestWSOutOfOrderAndLocalEquivalence(t *testing.T) {
	p := newWSPair(t)
	r := p.raw(t)
	r.negotiate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	server, e := p.s.Accept(ctx)
	mustGRPC(t, e)
	done := make(chan error, 2)
	for _, id := range []string{"first", "second"} {
		go func(id string) {
			q := queryWS(id, p.d.Request.OperationID).GetQuery()
			b, _ := proto.Marshal(q)
			_, e := server.Exchange(ctx, b)
			done <- e
		}(id)
	}
	requests := []*wire.CapabilityRequest{rawRead(t, r.c).GetQuery(), rawRead(t, r.c).GetQuery()}
	record, e := p.edge.exec.GetInvocation(ctx, p.d.Request.OperationID)
	mustGRPC(t, e)
	for i := 1; i >= 0; i-- {
		q := requests[i]
		if q == nil {
			t.Fatal("no query")
		}
		out := &wire.CapabilityResponse{MessageId: "reply-" + q.MessageId, ReplyTo: q.MessageId, Namespace: "local", Evidence: "durable_capability_execution", Body: &wire.CapabilityResponse_Snapshot{Snapshot: executionwire.Snapshot(record)}}
		env := &wire.Envelope{MessageId: out.MessageId, ReplyTo: &out.ReplyTo, Body: &wire.Envelope_Answer{Answer: out}}
		r.stamp(env, 3, "response")
		rawWrite(t, r.c, env)
	}
	mustGRPC(t, <-done)
	mustGRPC(t, <-done)
	// A second actual connection compares the network SDK with the installed
	// in-process catalog and authorized state interfaces, including exact refs.
	c, e := p.client.Dial(ctx, p.address, "cloud")
	mustGRPC(t, e)
	defer c.Close()
	cert, _ := tls.LoadX509KeyPair(filepath.Join(p.edge.root, "node.crt"), filepath.Join(p.edge.root, "node.key"))
	peer := authorization.GrantPresentation{Namespace: "local", Subject: "operator", Audience: "cloud", Presenter: "edge", CertificateSHA256: authorization.CertificateDigest(cert.Certificate[0])}
	binding, e := p.server.Resolve(ctx, peer)
	mustGRPC(t, e)
	local := sdk.NewCapabilityClient(cataloglocal.Bind(binding.Catalog, "local", nil), "local")
	remote := sdk.NewCapabilityClient(c, "local")
	q := catalog.Query{Purpose: "task", Location: "local", Limit: 4, Budget: 8}
	a, e := local.List(ctx, q)
	mustGRPC(t, e)
	b, e := remote.List(ctx, q)
	mustGRPC(t, e)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("list differs")
	}
	q.Text = "counter"
	a, e = local.Search(ctx, q)
	mustGRPC(t, e)
	b, e = remote.Search(ctx, q)
	mustGRPC(t, e)
	if !reflect.DeepEqual(a, b) {
		t.Fatal("search differs")
	}
	x, e := local.Describe(ctx, a.Items[0].Ref)
	mustGRPC(t, e)
	y, e := remote.Describe(ctx, a.Items[0].Ref)
	mustGRPC(t, e)
	if !reflect.DeepEqual(x, y) {
		t.Fatal("describe differs")
	}
	expected, e := binding.Execution.ReadRemoteInvocation(ctx, p.d.RemoteOperation)
	mustGRPC(t, e)
	got, e := remote.GetInvocation(ctx, p.d.RemoteOperation)
	mustGRPC(t, e)
	if !proto.Equal(executionwire.Snapshot(expected), executionwire.Snapshot(got)) {
		t.Fatal("state differs")
	}
	hidden := a.Items[0].Ref
	hidden.Namespace = "hidden"
	_, localErr := local.Describe(ctx, hidden)
	_, remoteErr := remote.Describe(ctx, hidden)
	if !authorization.Is(localErr, authorization.Denied) || !authorization.Is(remoteErr, authorization.Denied) {
		t.Fatalf("denial differs: %v / %v", localErr, remoteErr)
	}
	sub, e := c.Subscribe(ctx, p.d.RemoteOperation)
	mustGRPC(t, e)
	_, e = sub.Receive(ctx)
	mustGRPC(t, e)
	sub.Close()
	if _, e = sub.Receive(ctx); e != context.Canceled {
		t.Fatalf("closed subscription delivered: %v", e)
	}
	mustGRPC(t, wsQueries(ctx, c, p.d.RemoteOperation))
}

func TestWSChunkedControlOrdering(t *testing.T) {
	p := newWSPair(t)
	p.client.Config.Chunk = 512
	var hold atomic.Bool
	release := make(chan struct{})
	original := p.client.Resolve
	p.client.Resolve = func(ctx context.Context, v authorization.GrantPresentation) (*wsbinding.Binding, error) {
		if hold.Load() {
			select {
			case <-release:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		return original(ctx, v)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	c, e := p.client.Dial(ctx, p.address, "cloud")
	mustGRPC(t, e)
	defer c.Close()
	hold.Store(true)
	subs := make([]*wsbinding.Subscription, 0, 4)
	for i := 0; i < 4; i++ {
		sub, e := c.Subscribe(ctx, strings.Repeat(fmt.Sprint(i), 512))
		mustGRPC(t, e)
		defer sub.Close()
		subs = append(subs, sub)
	}
	close(release)
	for _, sub := range subs {
		_, e = sub.Receive(ctx)
		if !authorization.Is(e, authorization.Denied) {
			t.Fatalf("control ordering: %v", e)
		}
	}
}

func TestWSOptionalCapabilitiesDisabled(t *testing.T) {
	a, b := prepareWS(t)
	h, ch, d := openWS(t, a)
	defer h.close()
	other, sh, _ := openWS(t, b)
	defer other.close()
	ch.Required = []string{"chunks.v1"}
	ch.Optional = []string{"catalog.read.v1", "unrecognized.optional"}
	sh.Required = []string{"chunks.v1"}
	sh.Optional = []string{"catalog.read.v1", "invocation.read.v1", "progress.v1"}
	s, e := wsbinding.NewServer(sh)
	mustGRPC(t, e)
	defer s.Close()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	go s.Serve(l)
	c, e := ch.Dial(context.Background(), "wss://"+l.Addr().String()+"/harness", "cloud")
	mustGRPC(t, e)
	defer c.Close()
	api := sdk.NewCapabilityClient(c, "local")
	page, e := api.List(context.Background(), catalog.Query{Purpose: "task", Location: "local", Limit: 4, Budget: 8})
	mustGRPC(t, e)
	if len(page.Items) != 1 {
		t.Fatal(page)
	}
	_, e = api.GetInvocation(context.Background(), d.RemoteOperation)
	if !authorization.Is(e, authorization.Unsupported) {
		t.Fatalf("disabled query: %v", e)
	}
	_, e = c.Subscribe(context.Background(), d.RemoteOperation)
	if !authorization.Is(e, authorization.Unsupported) {
		t.Fatalf("disabled subscription: %v", e)
	}
}
func TestWSLateReplyAfterDeadline(t *testing.T) {
	p := newWSPair(t)
	r := p.raw(t)
	r.negotiate(t)
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	server, e := p.s.Accept(ctx)
	mustGRPC(t, e)
	done := make(chan error, 1)
	short, stop := context.WithTimeout(ctx, 50*time.Millisecond)
	defer stop()
	go func() {
		_, e := sdk.NewCapabilityClient(server, "local").GetInvocation(short, p.d.Request.OperationID)
		done <- e
	}()
	q := rawRead(t, r.c).GetQuery()
	if q == nil {
		t.Fatal("missing timed query")
	}
	if e = <-done; e != context.DeadlineExceeded {
		t.Fatalf("request did not expire: %v", e)
	}
	record, e := p.edge.exec.GetInvocation(ctx, p.d.Request.OperationID)
	mustGRPC(t, e)
	out := &wire.CapabilityResponse{MessageId: "late", ReplyTo: q.MessageId, Namespace: "local", Evidence: "durable_capability_execution", Body: &wire.CapabilityResponse_Snapshot{Snapshot: executionwire.Snapshot(record)}}
	e1 := &wire.Envelope{MessageId: out.MessageId, ReplyTo: &out.ReplyTo, Body: &wire.Envelope_Answer{Answer: out}}
	r.stamp(e1, 3, "response")
	rawWrite(t, r.c, e1)
	go func() {
		_, e := sdk.NewCapabilityClient(server, "local").GetInvocation(ctx, p.d.Request.OperationID)
		done <- e
	}()
	q = rawRead(t, r.c).GetQuery()
	if q == nil {
		t.Fatal("missing next query")
	}
	out = proto.Clone(out).(*wire.CapabilityResponse)
	out.MessageId = "fresh"
	out.ReplyTo = q.MessageId
	e1 = &wire.Envelope{MessageId: out.MessageId, ReplyTo: &out.ReplyTo, Body: &wire.Envelope_Answer{Answer: out}}
	r.stamp(e1, 3, "response")
	rawWrite(t, r.c, e1)
	mustGRPC(t, <-done)
}
