package grpc_test

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/endpointchannel"
	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

func realChannelApplications(t *testing.T, f *unaryFixture, authority transport.EndpointAuthority) ([]string, []func()) {
	t.Helper()
	addresses := []string{}
	stops := []func(){}
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
		stop := func() {
			once.Do(func() {
				cancel()
				select {
				case err := <-done:
					if err != nil {
						t.Error(err)
					}
				case <-time.After(7 * time.Second):
					t.Error("application handlers did not actually exit")
				}
			})
		}
		t.Cleanup(stop)
		stops = append(stops, stop)
		addresses = append(addresses, "grpcs://"+listener.Addr().String())
	}
	return addresses, stops
}
func realChannelGateway(t *testing.T, f *unaryFixture, router *endpointchannel.Router) (harness.Discovery, string, *http.Client) {
	t.Helper()
	registry := f.processor.(wss.LocalProcessor).Dispatcher.Registry
	s, err := wss.New(wss.Config{OwnerID: f.owner, Store: f.store, Registry: registry, Identity: f.identity, Processor: router, MaxConnections: 16, MaxQueuedBytes: 64 << 20})
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(s.Handler())
	server.TLS = f.serverTLS.Clone()
	server.StartTLS()
	t.Cleanup(func() { router.Close(); s.Close(); server.Close() })
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: f.clientTLS.Clone()}}
	discovery, err := (&harness.HTTPTransport{BaseURL: server.URL, Token: f.token, HTTP: client}).Discover(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return discovery, "wss" + strings.TrimPrefix(server.URL, "https") + "/connect", client
}
func newStaticChannel(t *testing.T, f *unaryFixture) (*transport.StaticEndpointAuthority, transport.EndpointRegistration) {
	t.Helper()
	reg := transport.EndpointRegistration{TenantID: f.auth.TenantID, SubjectID: f.auth.SubjectID, CredentialGeneration: f.auth.CredentialGeneration, EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), Generation: 1, RecipientServiceID: f.owner}
	a, err := transport.NewStaticEndpointAuthority(transport.StaticEndpointAuthorityConfig{OwnerID: f.owner, GatewayIdentities: []string{"spiffe://harness.test/gateway"}, Identity: f.identity, Pairs: []transport.StaticEndpointPair{{Registration: reg, Methods: f.discovery.Methods}}})
	if err != nil {
		t.Fatal(err)
	}
	return a, reg
}
func TestWSSStaticChannelRebindPreservesConnectionReceiptAndSequence(t *testing.T) {
	f := newUnaryFixture(t)
	authority, registration := newStaticChannel(t, f)
	addresses, stops := realChannelApplications(t, f, authority)
	router, err := endpointchannel.NewRouter(endpointchannel.Config{OwnerID: f.owner, GatewayInstanceID: api.NewID("instance"), MethodsDigest: f.discovery.MethodsDigest, Applications: []endpointchannel.Application{{Address: addresses[0], ClientTLS: f.clientTLS}, {Address: addresses[1], ClientTLS: f.clientTLS}}, Registrations: []transport.EndpointRegistration{registration}, Identity: f.identity, Credentials: func(ctx context.Context, a rt.Auth) (string, error) {
		if !api.Equal(a, f.auth) {
			return "", api.E("forbidden", "original_auth_changed")
		}
		return f.token, f.identity.CheckCurrent(ctx, a)
	}})
	if err != nil {
		t.Fatal(err)
	}
	discovery, address, httpClient := realChannelGateway(t, f, router)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ws, err := harness.DialWebSocketWithHTTP(ctx, address, f.token, discovery, false, httpClient)
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	journal, err := harness.OpenJournal(filepath.Join(t.TempDir(), "journal"), discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	client, err := harness.NewClient(ws, journal, discovery)
	if err != nil {
		t.Fatal(err)
	}
	original := commandFor(f)
	before, err := client.Send(ctx, original)
	if err != nil || before.Stage != "applied" {
		t.Fatalf("original channel command: %+v %v", before, err)
	}
	first := router.States()
	if len(first) != 1 || !first[0].Connected || first[0].LastRequestSeq != 1 {
		t.Fatalf("original external connection: %+v", first)
	}
	stops[0]()
	var replacement endpointchannel.State
	for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline); {
		states := router.States()
		if len(states) == 1 && states[0].Connected && states[0].BindingRevision > first[0].BindingRevision {
			replacement = states[0]
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(10 * time.Millisecond):
		}
	}
	if replacement.ConnectionID != first[0].ConnectionID || replacement.BindingID == first[0].BindingID || replacement.BindingRevision <= first[0].BindingRevision || replacement.EndpointID != first[0].EndpointID || replacement.EndpointGeneration != first[0].EndpointGeneration || replacement.GatewayInstanceID != first[0].GatewayInstanceID || replacement.MethodsDigest != first[0].MethodsDigest {
		t.Fatalf("rebind changed original external identity: before=%+v after=%+v", first[0], replacement)
	}
	after, err := client.Send(ctx, original)
	if err != nil || !api.Equal(before, after) {
		t.Fatalf("same original receipt on replacement app: %+v %v", after, err)
	}
	query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, QueryID: api.NewID("query"), Method: "testing.get", TargetID: original.TargetID, Payload: api.Raw(struct{}{})}
	raw, err := client.Query(ctx, query)
	if err != nil {
		t.Fatal(err)
	}
	var actual echoOutput
	if err = api.Decode(raw, &actual); err != nil || actual.Message != "exact original message" || actual.SubjectID != f.auth.SubjectID {
		t.Fatalf("new application actual original facts: %+v %v", actual, err)
	}
	states := router.States()
	if len(states) != 1 || states[0].ConnectionID != first[0].ConnectionID || states[0].LastRequestSeq != 3 {
		t.Fatalf("outer socket/seq/Ready changed: %+v", states)
	}
	stored, err := f.store.List(ctx, rt.Scope{TenantID: f.auth.TenantID, OwnerID: f.owner, DatabaseID: f.store.ID()}, "grpctest.messages", "", "", 10)
	if err != nil || len(stored) != 1 {
		t.Fatalf("original domain receipt duplicated facts: %d %v", len(stored), err)
	}
	if err = ws.Close(); err != nil {
		t.Fatal(err)
	}
	if err = router.Close(); err != nil {
		t.Fatal(err)
	}
	if len(router.States()) != 0 {
		t.Fatal("actual channel owned lifecycle did not retire")
	}
	t.Logf("CHANNEL_REBIND_EVIDENCE %s", api.Raw(struct {
		First, Replacement     endpointchannel.State
		CommandID, OriginalTTL string
		DatabaseID             string
	}{first[0], states[0], original.CommandID, original.ExpiresAt, f.store.ID()}))
}

var _ *tls.Config
