package grpc_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"math/big"
	"net"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/api/proto/rpcv1"
	rt "github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
	"github.com/ruipengliu/lerna/sdk/go/grpcwire"
	grpcgo "google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
)

type echoInput struct {
	Message string `json:"message"`
}
type echoOutput struct {
	Message   string `json:"message"`
	SubjectID string `json:"subject_id"`
}
type unaryFixture struct {
	store                rt.Store
	identity             *platform.DevIdentity
	auth                 rt.Auth
	owner, token         string
	discovery            harness.Discovery
	processor            transport.Processor
	serverTLS, clientTLS *tls.Config
}

func newUnaryFixture(t *testing.T, maximumMessage ...int) *unaryFixture {
	t.Helper()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "grpc.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err = st.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	return newUnaryFixtureWithStore(t, st, maximumMessage...)
}
func newUnaryFixtureWithStore(t *testing.T, st rt.Store, maximumMessage ...int) *unaryFixture {
	t.Helper()
	f := &unaryFixture{store: st, owner: api.NewID("service"), token: api.NewID("token"), auth: rt.Auth{TenantID: api.NewID("tenant"), SubjectID: api.NewID("subject"), CredentialGeneration: 1, Roles: []string{"admin"}}}
	f.identity = &platform.DevIdentity{Store: st, OwnerID: f.owner, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: f.auth, TokenHash: api.Hash([]byte(f.token))}}}
	if err := f.identity.Initialize(context.Background()); err != nil {
		t.Fatal(err)
	}
	r := rt.NewRegistry()
	persist := api.Contract[echoInput, echoOutput]("testing.persist", "grpctest", "command", false, false)
	read := api.Contract[struct{}, echoOutput]("testing.get", "grpctest", "query", false, false)
	if len(maximumMessage) > 0 {
		for _, schema := range []api.Schema{persist.InputSchema, persist.OutputSchema, read.OutputSchema} {
			schema["properties"].(map[string]any)["message"].(api.Schema)["maxLength"] = maximumMessage[0]
		}
	}
	r.MustRegister(rt.Method{Contract: persist, Participants: []string{"grpctest"}, Apply: func(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command) (rt.Outcome, error) {
		var in echoInput
		if e := api.Decode(c.Payload, &in); e != nil {
			return rt.Outcome{}, e
		}
		out := echoOutput{in.Message, a.SubjectID}
		if e := tx.Create(ctx, "grpctest.messages", c.TargetID, "", out); e != nil {
			return rt.Outcome{}, e
		}
		return rt.Applied(out), nil
	}})
	r.MustRegister(rt.Method{Contract: read, Query: func(ctx context.Context, st rt.Store, sc rt.Scope, a rt.Auth, q api.Query) (any, error) {
		var out echoOutput
		_, e := st.Read(ctx, sc, "grpctest.messages", q.TargetID, 0, &out)
		return out, e
	}})
	f.processor = wss.LocalProcessor{Dispatcher: &rt.Dispatcher{Store: st, OwnerID: f.owner, Registry: r}}
	methods := r.Contracts()
	digest, _ := api.Digest(methods)
	f.discovery = harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, SchemaDigest: api.CoreDigest(), Methods: methods, MethodsDigest: digest, IdentityScope: f.auth.TenantID + "/" + f.auth.SubjectID, IdentityRevision: 1}
	f.serverTLS, f.clientTLS = testTLS(t)
	return f
}
func testTLS(t *testing.T) (*tls.Config, *tls.Config) {
	t.Helper()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Harness test CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, e := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	root, e := x509.ParseCertificate(der)
	if e != nil {
		t.Fatal(e)
	}
	leafKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	leafDER, e := x509.CreateCertificate(rand.Reader, leaf, root, &leafKey.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	roots := x509.NewCertPool()
	roots.AddCert(root)
	gatewayKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	uri, _ := url.Parse("spiffe://harness.test/gateway")
	gateway := &x509.Certificate{SerialNumber: big.NewInt(3), Subject: pkix.Name{CommonName: "Harness test gateway"}, URIs: []*url.URL{uri}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}
	gatewayDER, e := x509.CreateCertificate(rand.Reader, gateway, root, &gatewayKey.PublicKey, key)
	if e != nil {
		t.Fatal(e)
	}
	return &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{{Certificate: [][]byte{leafDER, der}, PrivateKey: leafKey}}, ClientCAs: roots, ClientAuth: tls.VerifyClientCertIfGiven}, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, Certificates: []tls.Certificate{{Certificate: [][]byte{gatewayDER, der}, PrivateKey: gatewayKey}}}
}
func (f *unaryFixture) serve(t *testing.T, dev bool) string {
	t.Helper()
	s, e := transport.New(transport.Config{OwnerID: f.owner, Identity: f.identity, Processor: f.processor, AllowInsecureLoopback: dev})
	if e != nil {
		t.Fatal(e)
	}
	l, e := net.Listen("tcp", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	cfg := f.serverTLS
	scheme := "grpcs://"
	if dev {
		cfg = nil
		scheme = "grpc://"
	}
	go func() { done <- s.Serve(ctx, l, cfg) }()
	t.Cleanup(func() {
		cancel()
		select {
		case e := <-done:
			if e != nil {
				t.Error(e)
			}
		case <-time.After(6 * time.Second):
			t.Error("gRPC server did not stop")
		}
	})
	return scheme + l.Addr().String()
}
func commandFor(f *unaryFixture) api.Command {
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, CommandID: api.NewID("command"), Method: "testing.persist", TargetID: api.NewID("object"), ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(echoInput{"exact original message"})}
}
func TestGRPCUnaryTLSAuthenticationAndOriginalSDKRecovery(t *testing.T) {
	f := newUnaryFixture(t)
	address := f.serve(t, false)
	ctx := context.Background()
	g, e := harness.DialGRPC(ctx, address, f.token, f.discovery, f.clientTLS, false)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	j, e := harness.OpenJournal(filepath.Join(t.TempDir(), "journal"), f.discovery.IdentityScope)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	client, e := harness.NewClient(g, j, f.discovery)
	if e != nil {
		t.Fatal(e)
	}
	c := commandFor(f)
	digest, _ := api.Digest(c)
	methodSchemaDigest := ""
	for _, method := range f.discovery.Methods {
		if method.Name == c.Method {
			methodSchemaDigest = method.SchemaDigest
		}
	}
	// 模拟进程在发出后、SDK记录回执前失效，真实服务已经保存原决定。
	if e = j.Save(ctx, harness.Entry{IdentityScope: f.discovery.IdentityScope, SchemaDigest: f.discovery.SchemaDigest, MethodSchemaDigest: methodSchemaDigest, Command: c, Digest: digest}); e != nil {
		t.Fatal(e)
	}
	raw, e := g.Call(ctx, "command", api.Raw(c))
	if e != nil {
		t.Fatal(e)
	}
	var first api.Receipt
	if e = api.Decode(raw, &first); e != nil {
		t.Fatal(e)
	}
	if first.Stage != "applied" {
		t.Fatalf("receipt %+v", first)
	}
	if e = g.Close(); e != nil {
		t.Fatal(e)
	}
	g, e = harness.DialGRPC(ctx, address, f.token, f.discovery, f.clientTLS, false)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	client, e = harness.NewClient(g, j, f.discovery)
	if e != nil {
		t.Fatal(e)
	}
	recovered, partial, e := client.Recover(ctx)
	if e != nil || partial || len(recovered) != 1 || !api.Equal(first, recovered[0]) {
		t.Fatalf("original receipt recovery %+v %v %v", recovered, partial, e)
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, QueryID: api.NewID("query"), Method: "testing.get", TargetID: c.TargetID, Payload: json.RawMessage(`{}`)}
	result, e := client.Query(ctx, q)
	if e != nil {
		t.Fatal(e)
	}
	var out echoOutput
	if e = api.Decode(result, &out); e != nil || out.SubjectID != f.auth.SubjectID || out.Message != "exact original message" {
		t.Fatalf("authenticated original sender %+v %v", out, e)
	}
	if e = f.identity.Revoke(ctx, f.auth); e != nil {
		t.Fatal(e)
	}
	if _, e = g.Call(ctx, "query", api.Raw(q)); e == nil {
		t.Fatal("revoked credential disclosed result")
	}
}
func TestGRPCUnaryRejectsBadAuthOwnerAndUnverifiedTLS(t *testing.T) {
	f := newUnaryFixture(t)
	address := f.serve(t, false)
	ctx := context.Background()
	bad, e := harness.DialGRPC(ctx, address, "bad-token", f.discovery, f.clientTLS, false)
	if e != nil {
		t.Fatal(e)
	}
	defer bad.Close()
	if _, e = bad.Call(ctx, "command", api.Raw(commandFor(f))); e == nil {
		t.Fatal("bad token accepted")
	}
	untrusted, e := harness.DialGRPC(ctx, address, f.token, f.discovery, &tls.Config{MinVersion: tls.VersionTLS13}, false)
	if e != nil {
		t.Fatal(e)
	}
	defer untrusted.Close()
	timeout, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if _, e = untrusted.Call(timeout, "command", api.Raw(commandFor(f))); e == nil {
		t.Fatal("unverified server certificate accepted")
	}
	cc, e := grpcgo.NewClient(address[len("grpcs://"):], grpcgo.WithTransportCredentials(credentials.NewTLS(f.clientTLS)), grpcgo.WithDefaultCallOptions(grpcgo.ForceCodec(grpcwire.Codec{})))
	if e != nil {
		t.Fatal(e)
	}
	defer cc.Close()
	rpc := rpcv1.NewHarnessServiceClient(cc)
	authctx := metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+f.token)
	response, e := rpc.Call(authctx, &rpcv1.CallRequest{LogicalServiceId: api.NewID("service"), Request: &rpcv1.CallRequest_CommandJson{CommandJson: api.Raw(commandFor(f))}})
	if e == nil && len(response.GetErrorJson()) == 0 {
		t.Fatal("wrong logical owner accepted")
	}
	duplicate := metadata.AppendToOutgoingContext(authctx, "authorization", "Bearer "+f.token)
	if _, e = rpc.Call(duplicate, &rpcv1.CallRequest{LogicalServiceId: f.owner, Request: &rpcv1.CallRequest_CommandJson{CommandJson: api.Raw(commandFor(f))}}); e == nil {
		t.Fatal("duplicate credentials accepted")
	}
}

func TestGRPCValidLargeOriginalAndReceiptRemainDurableAcrossClientRestart(t *testing.T) {
	f := newUnaryFixture(t, 150<<10)
	ctx := context.Background()
	g, err := harness.DialGRPC(ctx, f.serve(t, false), f.token, f.discovery, f.clientTLS, false)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	path := filepath.Join(t.TempDir(), "large-journal")
	j, err := harness.OpenJournal(path, f.discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = j.Close() })
	client, err := harness.NewClient(g, j, f.discovery)
	if err != nil {
		t.Fatal(err)
	}
	original := commandFor(f)
	original.Payload = api.Raw(echoInput{Message: strings.Repeat("v", 130<<10)})
	receipt, err := client.Send(ctx, original)
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("valid large receipt was not durable: %v", err)
	}
	if err = j.Close(); err != nil {
		t.Fatal(err)
	}
	j, err = harness.OpenJournal(path, f.discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	entry, err := j.Read(ctx, original.CommandID)
	if err != nil || !api.Equal(entry.Command, original) || !api.Equal(entry.Receipt, receipt) {
		t.Fatalf("restart lost valid original or receipt: %v", err)
	}
}
func TestGRPCDevelopmentCleartextRequiresExplicitLoopback(t *testing.T) {
	f := newUnaryFixture(t)
	address := f.serve(t, true)
	ctx := context.Background()
	if _, e := harness.DialGRPC(ctx, address, f.token, f.discovery, nil, false); e == nil {
		t.Fatal("cleartext enabled without explicit development option")
	}
	if _, e := harness.DialGRPC(ctx, "grpc://192.0.2.1:1234", f.token, f.discovery, nil, true); e == nil {
		t.Fatal("cleartext non-loopback enabled")
	}
	g, e := harness.DialGRPC(ctx, address, f.token, f.discovery, nil, true)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	if _, e = g.Call(ctx, "command", api.Raw(commandFor(f))); e != nil {
		t.Fatal(e)
	}
}

type blockingQueries struct {
	next    transport.Processor
	entered chan struct{}
	release chan struct{}
}

func (p blockingQueries) Call(ctx context.Context, a rt.Auth, kind string, b json.RawMessage) (string, json.RawMessage, error) {
	if kind == "query" {
		p.entered <- struct{}{}
		select {
		case <-p.release:
		case <-ctx.Done():
			return "", nil, ctx.Err()
		}
	}
	return p.next.Call(ctx, a, kind, b)
}
func TestGRPCControlCallRemainsAvailableWhenOrdinarySlotsAreFull(t *testing.T) {
	f := newUnaryFixture(t)
	block := blockingQueries{next: f.processor, entered: make(chan struct{}, 32), release: make(chan struct{})}
	f.processor = block
	address := f.serve(t, false)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()
	g, e := harness.DialGRPC(ctx, address, f.token, f.discovery, f.clientTLS, false)
	if e != nil {
		t.Fatal(e)
	}
	defer g.Close()
	c := commandFor(f)
	if _, e = g.Call(ctx, "command", api.Raw(c)); e != nil {
		t.Fatal(e)
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, QueryID: api.NewID("query"), Method: "testing.get", TargetID: c.TargetID, Payload: json.RawMessage(`{}`)}
	var workers sync.WaitGroup
	results := make(chan error, 32)
	for range 32 {
		workers.Add(1)
		go func() { defer workers.Done(); _, e := g.Call(ctx, "query", api.Raw(q)); results <- e }()
	}
	defer func() { close(block.release); workers.Wait() }()
	for range 32 {
		select {
		case <-block.entered:
		case <-ctx.Done():
			t.Fatal("ordinary slots did not enter")
		}
	}
	if _, e = g.Call(ctx, "query", api.Raw(q)); !api.IsCode(e, "overloaded") {
		t.Fatalf("ordinary overload %v", e)
	}
	raw, e := g.Call(ctx, "receipt_lookup", api.Raw(api.ReceiptLookup{LogicalServiceID: f.owner, CommandID: c.CommandID}))
	if e != nil {
		t.Fatalf("control slot unavailable: %v", e)
	}
	var receipt api.Receipt
	if e = api.Decode(raw, &receipt); e != nil || receipt.CommandID != c.CommandID || receipt.Stage != "applied" {
		t.Fatalf("original control receipt %+v %v", receipt, e)
	}
}
