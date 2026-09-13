package asynccheck

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"lerna/adapters/grpcbinding"
	"lerna/adapters/nodetls"
	"lerna/adapters/sqliteauth"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func grpcConfig() grpcbinding.Config {
	return grpcbinding.Config{MaxConnections: 4, MaxSessions: 8, MaxStreams: 4, MaxMessageBytes: 65536, SessionTTL: time.Minute, IOTimeout: time.Second, PollInterval: 20 * time.Millisecond}
}
func nodeConfig() authorization.NodeConfig {
	return authorization.NodeConfig{MaxTTL: time.Hour, RequestTTL: time.Minute, IOTimeout: time.Second, MaxRecords: 8, MaxOperations: 64}
}

type noIssuance struct{}

func (noIssuance) Issue(context.Context, authorization.NodeMutation, time.Time) ([]byte, error) {
	return nil, fmt.Errorf("runtime has no enrollment signer")
}
func mustGRPC(t *testing.T, e error) {
	t.Helper()
	if e != nil {
		t.Fatal(e)
	}
}
func privateJSON(t *testing.T, path string, v any) {
	t.Helper()
	b, e := json.Marshal(v)
	mustGRPC(t, e)
	mustGRPC(t, os.WriteFile(path, b, 0600))
}
func saveCertificate(t *testing.T, dir string, c tls.Certificate) {
	t.Helper()
	der, e := x509.MarshalPKCS8PrivateKey(c.PrivateKey)
	mustGRPC(t, e)
	mustGRPC(t, os.WriteFile(filepath.Join(dir, "node.key"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600))
	mustGRPC(t, os.WriteFile(filepath.Join(dir, "node.crt"), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Certificate[0]}), 0600))
}
func endpoint(t *testing.T, dir, node string, a *authorization.Service, clock authorization.Clock) *nodetls.Endpoint {
	t.Helper()
	c, e := tls.LoadX509KeyPair(filepath.Join(dir, "node.crt"), filepath.Join(dir, "node.key"))
	mustGRPC(t, e)
	ca, e := os.ReadFile(filepath.Join(dir, "ca.der"))
	mustGRPC(t, e)
	nodes, e := a.Nodes(nodeConfig(), noIssuance{})
	mustGRPC(t, e)
	now, e := clock.Now()
	mustGRPC(t, e)
	ep, e := nodetls.NewEndpoint(nodes, clock, "local", node, c, []nodetls.Trust{{Certificate: ca, Until: now.Add(time.Hour)}}, time.Second)
	mustGRPC(t, e)
	return ep
}

type networkFixture struct {
	server, client   string
	request          execution.Request
	material, cancel string
	endpoint         *nodetls.Endpoint
	clientDB         *sqliteauth.Store
	h                *harness
}

func prepareNetwork(t *testing.T) *networkFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	f := &networkFixture{server: filepath.Join(root, "server"), client: filepath.Join(root, "client")}
	mustGRPC(t, os.Mkdir(f.server, 0700))
	mustGRPC(t, os.Mkdir(f.client, 0700))
	h, e := open(ctx, f.server, "")
	mustGRPC(t, e)
	f.h = h
	ca, e := nodetls.GenerateCA(h.now(), 2*time.Hour)
	mustGRPC(t, e)
	issuer, e := nodetls.NewIssuer(ca)
	mustGRPC(t, e)
	nodes, e := h.auth.Nodes(nodeConfig(), issuer)
	mustGRPC(t, e)
	certs := map[string]tls.Certificate{}
	for _, name := range []string{"execution-local", "local-host"} {
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		mustGRPC(t, e)
		op, e := h.operation(ctx)
		mustGRPC(t, e)
		snap, e := h.db.Load(ctx)
		mustGRPC(t, e)
		in := authorization.NodeMutation{OperationID: op, Namespace: "local", Node: name, Kind: "ENROLL", ExpectedRevision: snap.State.Revision, Subjects: []string{"operator"}, RequestExpires: h.now().Add(time.Minute).Unix(), Expires: h.now().Add(time.Hour).Unix()}
		in.CSR, e = nodetls.Request(key, in)
		mustGRPC(t, e)
		record, e := nodes.Mutate(ctx, h.token, in)
		mustGRPC(t, e)
		certs[name] = tls.Certificate{Certificate: [][]byte{record.Certificate}, PrivateKey: key}
	}
	saveCertificate(t, f.server, certs["execution-local"])
	saveCertificate(t, f.client, certs["local-host"])
	for _, dir := range []string{f.server, f.client} {
		mustGRPC(t, os.WriteFile(filepath.Join(dir, "ca.der"), ca.Certificate[0], 0600))
	}
	h.binding.CertificateSHA256 = authorization.CertificateDigest(certs["local-host"].Certificate[0])
	privateJSON(t, filepath.Join(f.server, "host.json"), h.binding)
	h.exec, e = execution.New(h.grants, h.work, h.access, h.target, h.binding, h.cap, config(), h.operation)
	mustGRPC(t, e)
	f.request, f.material, e = h.request(ctx)
	mustGRPC(t, e)
	f.cancel, e = h.operation(ctx)
	mustGRPC(t, e)
	// Trusted installation copies only public enrollment records to the caller's
	// independent authority. The caller receives no server token, store or node key.
	db, e := sqliteauth.Open(filepath.Join(f.client, "authority.db"))
	mustGRPC(t, e)
	f.clientDB = db
	a, e := authorization.New(db, h.clock, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	mustGRPC(t, e)
	_, e = a.Bootstrap(ctx, "local", "operator")
	mustGRPC(t, e)
	clientState, e := db.Load(ctx)
	mustGRPC(t, e)
	serverState, e := h.db.Load(ctx)
	mustGRPC(t, e)
	clientState.State.Nodes = &authorization.NodeJournal{Config: nodeConfig(), Records: serverState.State.Nodes.Records, Operations: map[string]authorization.NodeOperation{}}
	mustGRPC(t, db.Commit(ctx, clientState.Version, clientState.State))
	f.endpoint = endpoint(t, f.client, "local-host", a, h.clock)
	t.Cleanup(func() { db.Close() })
	return f
}
func openNetwork(t *testing.T, root string) (*harness, *nodetls.Endpoint) {
	t.Helper()
	raw, e := os.ReadFile(filepath.Join(root, "host.json"))
	mustGRPC(t, e)
	var b execution.Binding
	mustGRPC(t, json.Unmarshal(raw, &b))
	h, e := open(context.Background(), root, b.Token)
	mustGRPC(t, e)
	var restored time.Time
	if raw, err := os.ReadFile(filepath.Join(root, "clock.json")); err == nil {
		mustGRPC(t, json.Unmarshal(raw, &restored))
		h.clock.mu.Lock()
		h.clock.now = restored
		h.clock.mu.Unlock()
	}
	h.binding = b
	h.exec, e = execution.New(h.grants, h.work, h.access, h.target, b, h.cap, config(), h.operation)
	mustGRPC(t, e)
	return h, endpoint(t, root, "execution-local", h.auth, h.clock)
}

// The same executable runs an independent server, with no inherited service
// objects. stdin is a bounded test-control channel, never the business transport.
func TestGRPCServerProcess(t *testing.T) {
	root := os.Getenv("HARNESS_GRPC_SERVER")
	if root == "" {
		t.Skip("subprocess only")
	}
	h, ep := openNetwork(t, root)
	defer h.close()
	j, e := grpcbinding.OpenJournal(filepath.Join(root, "outbox.db"), 128)
	mustGRPC(t, e)
	defer j.Close()
	s, e := grpcbinding.NewServer(ep, h.clock, grpcConfig(), func(context.Context, authorization.GrantPresentation) (*execution.Service, error) { return h.exec, nil }, j)
	mustGRPC(t, e)
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	fmt.Println(l.Addr().String())
	done := make(chan error, 1)
	go func() { done <- s.Serve(l) }()
	defer s.Stop()
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var command struct{ Kind, Operation string }
		mustGRPC(t, json.Unmarshal(scanner.Bytes(), &command))
		ctx := context.Background()
		switch command.Kind {
		case "start":
			_, e = h.exec.Run(ctx, command.Operation)
		case "complete":
			e = h.target.Complete(ctx, command.Operation)
			if e == nil {
				h.clock.advance(time.Second)
				_, e = h.exec.Reconcile(ctx, command.Operation)
			}
		case "cancel":
			_, e = h.exec.RunCancel(ctx, command.Operation)
		case "revoke":
			record, err := h.exec.GetInvocation(ctx, command.Operation)
			mustGRPC(t, err)
			op, err := h.operation(ctx)
			mustGRPC(t, err)
			snap, err := h.db.Load(ctx)
			mustGRPC(t, err)
			_, e = h.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, Kind: "REVOKE", GrantId: record.Permit.GrantID, ExpectedRevision: snap.State.Revision, ExpectedGrantRevision: snap.State.Signed.Grants[record.Permit.GrantID].Record.Revision})
		case "drain":
			e = h.exec.Drain(ctx, 16)
		case "advance":
			h.clock.advance(2 * time.Minute)
		case "disable":
			nodes, err := h.auth.Nodes(nodeConfig(), noIssuance{})
			mustGRPC(t, err)
			snap, err := h.db.Load(ctx)
			mustGRPC(t, err)
			op, err := h.operation(ctx)
			mustGRPC(t, err)
			_, e = nodes.Mutate(ctx, h.token, authorization.NodeMutation{OperationID: op, Namespace: "local", Node: "local-host", Kind: "DISABLE", ExpectedRevision: snap.State.Revision})
		case "stop":
			return
		default:
			t.Fatal("unknown test control")
		}
		mustGRPC(t, e)
		privateJSON(t, filepath.Join(root, "clock.json"), h.now())
		fmt.Println("controlled")
	}
}

type serverProcess struct {
	cmd   *exec.Cmd
	input *json.Encoder
	lines *bufio.Scanner
	stop  func()
}

func launchServer(t *testing.T, root string) *serverProcess {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestGRPCServerProcess$")
	cmd.Env = append(os.Environ(), "HARNESS_GRPC_SERVER="+root)
	in, e := cmd.StdinPipe()
	mustGRPC(t, e)
	out, e := cmd.StdoutPipe()
	mustGRPC(t, e)
	cmd.Stderr = os.Stderr
	mustGRPC(t, cmd.Start())
	p := &serverProcess{cmd: cmd, input: json.NewEncoder(in), lines: bufio.NewScanner(out)}
	p.stop = func() { in.Close(); cmd.Process.Kill(); cmd.Wait(); cancel() }
	t.Cleanup(p.stop)
	return p
}
func (p *serverProcess) address(t *testing.T) string {
	t.Helper()
	if !p.lines.Scan() {
		t.Fatal("server failed before listen")
	}
	address := p.lines.Text()
	if host, _, e := net.SplitHostPort(address); e != nil || net.ParseIP(host) == nil {
		for p.lines.Scan() {
			address += "\n" + p.lines.Text()
		}
		t.Fatal(address)
	}
	t.Logf("gRPC caller PID=%d executor PID=%d address=%s", os.Getpid(), p.cmd.Process.Pid, address)
	return address
}
func (p *serverProcess) control(t *testing.T, kind, op string) {
	t.Helper()
	mustGRPC(t, p.input.Encode(struct{ Kind, Operation string }{kind, op}))
	if !p.lines.Scan() || p.lines.Text() != "controlled" {
		t.Fatalf("control %s: %s", kind, p.lines.Text())
	}
}
func connect(t *testing.T, f *networkFixture, address string) (*grpcbinding.Client, *sdk.CapabilityClient) {
	t.Helper()
	c, e := grpcbinding.Dial(address, "execution-local", f.endpoint, grpcConfig())
	mustGRPC(t, e)
	t.Cleanup(func() { c.Close() })
	_, e = c.Negotiate(context.Background(), "operator")
	mustGRPC(t, e)
	return c, sdk.NewCapabilityClient(c, "local")
}

func TestGRPCExecutionAndDurableResult(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	address := p.address(t)
	c, api := connect(t, f, address)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	receipt, e := api.Invoke(ctx, f.request, f.material)
	mustGRPC(t, e)
	if receipt.OperationID != f.request.OperationID {
		t.Fatal("operation changed")
	}
	replay, e := api.Invoke(ctx, f.request, f.material)
	mustGRPC(t, e)
	if replay != receipt {
		t.Fatal("replay changed receipt")
	}
	changed := f.request
	changed.InputRef = "changed"
	_, e = api.Invoke(ctx, changed, f.material)
	if !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatalf("conflict: %v", e)
	}
	inbox, e := grpcbinding.OpenJournal(filepath.Join(f.client, "inbox.db"), 128)
	mustGRPC(t, e)
	defer inbox.Close()
	sub, e := c.Subscribe(ctx, f.request.OperationID, inbox)
	mustGRPC(t, e)
	defer sub.Close()
	u, _, e := sub.Receive()
	mustGRPC(t, e)
	if u.Snapshot.Phase != "NOT_STARTED" {
		t.Fatal(u)
	}
	p.control(t, "start", f.request.OperationID)
	u, _, e = receivePhase(sub, "IN_PROGRESS")
	mustGRPC(t, e)
	if u.Snapshot.Phase != "IN_PROGRESS" || u.Reliable {
		t.Fatal(u)
	}
	sub.Close() // Closing the stream cannot stop the admitted job.
	p.control(t, "complete", f.request.OperationID)
	result, e := api.GetInvocation(ctx, f.request.OperationID)
	mustGRPC(t, e)
	if result.Effect != "CONFIRMED" || result.Result != "SUCCESS" {
		t.Fatal(result)
	}
	sub, e = c.Subscribe(ctx, f.request.OperationID, inbox)
	mustGRPC(t, e)
	u, fresh, e := receivePhase(sub, "FINISHED")
	mustGRPC(t, e)
	time.Sleep(50 * time.Millisecond) // allow receipt processing before a separate restart case
	sub.Close()
	if !u.Reliable || !fresh || u.Snapshot.Effect != "CONFIRMED" {
		t.Fatal(u, fresh)
	}
	p.control(t, "drain", "")
	p.stop()
	p = launchServer(t, f.server)
	address = p.address(t)
	c, api = connect(t, f, address)
	result, e = api.GetInvocation(ctx, f.request.OperationID)
	mustGRPC(t, e)
	if result.Effect != "CONFIRMED" {
		t.Fatal("restart lost operation")
	}
	sub, e = c.Subscribe(ctx, f.request.OperationID, inbox)
	mustGRPC(t, e)
	u, fresh, e = receivePhase(sub, "FINISHED")
	mustGRPC(t, e)
	sub.Close()
	if fresh || u.Snapshot.Effect != "CONFIRMED" {
		t.Fatal("duplicate result was new")
	}
	p.stop()
	h, _ := openNetwork(t, f.server)
	defer h.close()
	truth, e := h.target.Snapshot(ctx)
	mustGRPC(t, e)
	if truth.Changes != 1 || truth.Value != 3 || truth.Jobs != 1 {
		t.Fatal(truth)
	}
}

// Drop encrypted server-to-client bytes only after negotiation. The server still
// receives the mutation, while its reply is lost at the actual TCP boundary.
func responseLoss(t *testing.T, target string) (string, *atomic.Bool) {
	t.Helper()
	l, e := net.Listen("tcp", "127.0.0.1:0")
	mustGRPC(t, e)
	t.Cleanup(func() { l.Close() })
	drop := new(atomic.Bool)
	go func() {
		a, e := l.Accept()
		if e != nil {
			return
		}
		b, e := net.Dial("tcp", target)
		if e != nil {
			a.Close()
			return
		}
		t.Cleanup(func() { a.Close(); b.Close() })
		go func() {
			buf := make([]byte, 32768)
			for {
				n, e := a.Read(buf)
				if n > 0 {
					if _, w := b.Write(buf[:n]); w != nil {
						return
					}
				}
				if e != nil {
					return
				}
			}
		}()
		buf := make([]byte, 32768)
		for {
			n, e := b.Read(buf)
			if n > 0 && !drop.Load() {
				if _, w := a.Write(buf[:n]); w != nil {
					return
				}
			}
			if e != nil {
				return
			}
		}
	}()
	return l.Addr().String(), drop
}
func TestGRPCLostReplyKeepsOriginalOperation(t *testing.T) {
	f := prepareNetwork(t)
	f.h.close()
	p := launchServer(t, f.server)
	address := p.address(t)
	proxy, drop := responseLoss(t, address)
	_, api := connect(t, f, proxy)
	drop.Store(true)
	_, e := api.Invoke(context.Background(), f.request, f.material)
	if !authorization.Is(e, authorization.OutcomeUnknown) {
		t.Fatalf("lost response: %v", e)
	}
	_, api = connect(t, f, address)
	r, e := api.GetInvocation(context.Background(), f.request.OperationID)
	mustGRPC(t, e)
	if r.Started {
		t.Fatal("transport started driver")
	}
	p.control(t, "start", f.request.OperationID)
	p.control(t, "complete", f.request.OperationID)
	p.stop()
	p = launchServer(t, f.server)
	_, api = connect(t, f, p.address(t))
	r, e = api.GetInvocation(context.Background(), f.request.OperationID)
	mustGRPC(t, e)
	if r.Effect != "CONFIRMED" {
		t.Fatal(r)
	}
	p.stop()
	h, _ := openNetwork(t, f.server)
	defer h.close()
	truth, e := h.target.Snapshot(context.Background())
	mustGRPC(t, e)
	if truth.Changes != 1 || truth.Jobs != 1 {
		t.Fatal(truth)
	}
}

func receivePhase(sub *grpcbinding.Subscription, phase string) (*wire.InvocationUpdate, bool, error) {
	for range 32 {
		u, fresh, e := sub.Receive()
		if e != nil {
			return u, fresh, e
		}
		if u.Snapshot.Phase == phase {
			return u, fresh, nil
		}
	}
	return nil, false, fmt.Errorf("too many progress revisions")
}
