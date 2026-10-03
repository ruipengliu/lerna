package runner_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ruipengliu/lerna/adapters/development"
	rpc "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type channelRoleProcess struct {
	cmd  *exec.Cmd
	done chan error
	log  *os.File
	once sync.Once
}

func startChannelRole(t *testing.T, ctx context.Context, role, path string) *channelRoleProcess {
	t.Helper()
	if err := ctx.Err(); err != nil {
		t.Fatal(err)
	}
	log, err := os.OpenFile(path+".log", os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0600)
	if err != nil {
		t.Fatal(err)
	}
	p := &channelRoleProcess{cmd: exec.Command(binary(t, role), "--config", path), done: make(chan error, 1), log: log}
	p.cmd.Stdout, p.cmd.Stderr = log, log
	if err = p.cmd.Start(); err != nil {
		t.Fatal(err)
	}
	go func() { p.done <- p.cmd.Wait() }()
	t.Cleanup(func() { p.stop(t, false) })
	return p
}
func (p *channelRoleProcess) stop(t *testing.T, kill bool) {
	t.Helper()
	p.once.Do(func() {
		signal := os.Signal(syscall.SIGTERM)
		if kill {
			signal = syscall.SIGKILL
		}
		if err := p.cmd.Process.Signal(signal); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Error(err)
		}
		select {
		case err := <-p.done:
			if kill {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
					t.Error("actual application did not exit from SIGKILL")
				}
			} else if err != nil {
				t.Error("public role did not exit successfully", err)
			}
		case <-time.After(10 * time.Second):
			p.cmd.Process.Kill()
			t.Error("public role did not actually exit")
		}
		p.log.Close()
	})
}

func freeChannelAddress(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := l.Addr().String()
	if err = l.Close(); err != nil {
		t.Fatal(err)
	}
	return address
}

func channelCertificates(t *testing.T, root string) (development.EndpointTLSFiles, development.EndpointTLSFiles, *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Harness public role fixture CA"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	der, err := x509.CreateCertificate(rand.Reader, ca, ca, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	caPath := filepath.Join(root, "role-ca.pem")
	if err = os.WriteFile(caPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	issue := func(name string, template *x509.Certificate) development.EndpointTLSFiles {
		k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		body, err := x509.CreateCertificate(rand.Reader, template, parsed, &k.PublicKey, key)
		if err != nil {
			t.Fatal(err)
		}
		secret, err := x509.MarshalECPrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		files := development.EndpointTLSFiles{CertificateFile: filepath.Join(root, name+".pem"), KeyFile: filepath.Join(root, name+".key"), CAFile: caPath}
		if err = os.WriteFile(files.CertificateFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: body}), 0600); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(files.KeyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: secret}), 0600); err != nil {
			t.Fatal(err)
		}
		return files
	}
	server := issue("role-server", &x509.Certificate{SerialNumber: big.NewInt(2), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}})
	uri, _ := url.Parse("spiffe://harness.test/gateway")
	client := issue("role-gateway", &x509.Certificate{SerialNumber: big.NewInt(3), URIs: []*url.URL{uri}, NotBefore: ca.NotBefore, NotAfter: ca.NotAfter, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}})
	roots := x509.NewCertPool()
	roots.AddCert(parsed)
	return server, client, &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots}
}

func originalBinding(t *testing.T, ctx context.Context, store runtime.Store, scope runtime.Scope, connectionID string) map[string]any {
	t.Helper()
	var raw json.RawMessage
	if _, err := store.Read(ctx, scope, "grpc.bindings", connectionID, 0, &raw); err != nil {
		t.Fatal(err)
	}
	v, err := api.ParseJSON(raw)
	if err != nil {
		t.Fatal(err)
	}
	return v.(map[string]any)
}

func TestPublicGatewayChannelRebindsTwoApplicationProcessesOnOriginalConnection(t *testing.T) {
	if os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
		t.Skip("actual PostgreSQL configuration required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	root := t.TempDir()
	c, err := development.InitializeConfig(ctx, filepath.Join(root, "original.json"), root, "postgres")
	if err != nil {
		t.Fatal(err)
	}
	c.TenantID, c.OwnerID, c.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	c.DSNEnv, c.DevDatabaseEnvFile, c.StaticDir = "HARNESS_TEST_POSTGRES_DSN", "", ""
	if err = development.SaveConfig(filepath.Join(root, "original.json"), c); err != nil {
		t.Fatal(err)
	}
	_, diagnostic, err := run(t, binary(t, "migrate"), "--config", filepath.Join(root, "original.json"), "--development-init", "--data", root, "--driver", "postgres")
	if err != nil {
		t.Fatalf("actual management initialization failed: %v %s", err, diagnostic)
	}
	serverTLS, clientTLS, roots := channelCertificates(t, root)
	firstAddress, secondAddress, gatewayAddress := freeChannelAddress(t), freeChannelAddress(t), freeChannelAddress(t)
	channels := &development.EndpointChannelConfig{GatewayInstanceID: api.NewID("instance"), ApplicationInstanceID: api.NewID("instance"), ApplicationAddresses: []string{"grpcs://" + firstAddress, "grpcs://" + secondAddress}, GatewayIdentities: []string{"spiffe://harness.test/gateway"}, Registrations: []rpc.EndpointRegistration{{TenantID: c.TenantID, SubjectID: c.SubjectID, CredentialGeneration: 1, EndpointID: api.NewID("endpoint"), InstanceID: api.NewID("instance"), Generation: 1, RecipientServiceID: c.OwnerID}}, GatewayTLS: serverTLS, ApplicationTLS: serverTLS, GatewayClientTLS: clientTLS}
	c.EndpointChannels, c.HTTPAddr, c.GRPCAddr = channels, gatewayAddress, firstAddress
	firstPath, secondPath, gatewayPath := filepath.Join(root, "first.json"), filepath.Join(root, "second.json"), filepath.Join(root, "gateway.json")
	if err = development.SaveConfig(firstPath, c); err != nil {
		t.Fatal(err)
	}
	second := c
	secondChannels := *channels
	secondChannels.ApplicationInstanceID = api.NewID("instance")
	second.EndpointChannels, second.GRPCAddr = &secondChannels, secondAddress
	if err = development.SaveConfig(secondPath, second); err != nil {
		t.Fatal(err)
	}
	if err = development.SaveConfig(gatewayPath, c); err != nil {
		t.Fatal(err)
	}
	firstProcess := startChannelRole(t, ctx, "application", firstPath)
	secondProcess := startChannelRole(t, ctx, "application", secondPath)
	gatewayProcess := startChannelRole(t, ctx, "gateway", gatewayPath)
	tokenBytes, err := os.ReadFile(c.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimSpace(string(tokenBytes))
	httpClient := &http.Client{Transport: &http.Transport{TLSClientConfig: roots}, Timeout: 2 * time.Second}
	httpTransport := &harness.HTTPTransport{BaseURL: "https://" + gatewayAddress, Token: token, HTTP: httpClient}
	var discovery harness.Discovery
	for deadline := time.Now().Add(70 * time.Second); time.Now().Before(deadline); {
		discovery, err = httpTransport.Discover(ctx)
		if err == nil {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err != nil {
		t.Fatal("actual public gateway HTTPS discovery did not become ready", err)
	}
	conn, _, err := websocket.Dial(ctx, "wss://"+gatewayAddress+"/connect", &websocket.DialOptions{HTTPClient: httpClient, HTTPHeader: http.Header{"Authorization": []string{"Bearer " + token}}, Subprotocols: []string{"harness-wss.v1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	conn.SetReadLimit(1 << 20)
	_, raw, err := conn.Read(ctx)
	var ready harness.WSReady
	if err != nil || api.Decode(raw, &ready) != nil || ready.Type != "ready" || ready.IdentityScope != discovery.IdentityScope {
		t.Fatal("public channel did not return one accurate Ready")
	}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: c.OwnerID, CommandID: api.NewID("command"), Method: "session.create", TargetID: c.OwnerID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(interaction.CreateSessionInput{SessionID: api.NewID("session"), DefaultBranchID: api.NewID("branch"), ConfigRef: api.ComponentRef{ComponentID: api.NewID("component"), Version: "1.0.0", Digest: api.Hash([]byte("public-channel-session"))}})}
	request := func(seq uint64, kind string, payload any) harness.WSResponse {
		t.Helper()
		if err = conn.Write(ctx, websocket.MessageText, api.Raw(harness.WSRequest{Type: "request", RequestSeq: seq, Kind: kind, Payload: api.Raw(payload)})); err != nil {
			t.Fatal(err)
		}
		_, body, err := conn.Read(ctx)
		var response harness.WSResponse
		if err != nil || api.Decode(body, &response) != nil || response.Type != "response" || response.RequestSeq != seq {
			t.Fatalf("public original response/seq changed: %s %v", body, err)
		}
		return response
	}
	response := request(1, "command", original)
	var receipt api.Receipt
	if err = api.Decode(response.Payload, &receipt); err != nil || receipt.Stage != "applied" {
		t.Fatal("actual public application did not commit original command", err)
	}
	store, err := development.OpenStore(ctx, c, false)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	scope := runtime.Scope{TenantID: c.TenantID, OwnerID: c.OwnerID, DatabaseID: c.DatabaseID}
	before := originalBinding(t, ctx, store, scope, ready.ConnectionID)
	firstProcess.stop(t, true)
	var after map[string]any
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		after = originalBinding(t, ctx, store, scope, ready.ConnectionID)
		if after["application_instance_id"] == secondChannels.ApplicationInstanceID {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if before["binding_id"] == after["binding_id"] || after["application_instance_id"] != secondChannels.ApplicationInstanceID || !api.Equal(before["bind"], after["bind"]) {
		t.Fatal("public gateway did not rebind exact original external connection")
	}
	lastSeq := uint64(1)
	lookupDeadline := time.Now().Add(8 * time.Second)
	for {
		lastSeq++
		response = request(lastSeq, "receipt_lookup", api.ReceiptLookup{LogicalServiceID: c.OwnerID, CommandID: original.CommandID})
		if response.ResultKind != "error" {
			break
		}
		var problem api.Error
		if api.Decode(response.Payload, &problem) != nil || problem.Code != "dependency_unavailable" || problem.Reason != "endpoint_channel_rebinding" || !time.Now().Before(lookupDeadline) {
			t.Fatalf("replacement did not become Ready for original receipt: %s", response.Payload)
		}
		time.Sleep(10 * time.Millisecond)
	}
	var recovered api.Receipt
	if err = api.Decode(response.Payload, &recovered); err != nil || !api.Equal(receipt, recovered) {
		t.Fatal("public replacement changed original receipt/ID/deadline", err)
	}
	final := originalBinding(t, ctx, store, scope, ready.ConnectionID)
	if final["last_request_seq"] != float64(lastSeq) {
		t.Fatal("public channel reset external request high-water mark")
	}
	t.Logf("PUBLIC_CHANNEL_PROCESS_EVIDENCE %s", api.Raw(struct {
		SourceDatabaseID, TenantID, OwnerID, CommandID, OriginalTTL, ConnectionID string
		Before, After                                                             map[string]any
		Receipt                                                                   api.Receipt
	}{c.DatabaseID, c.TenantID, c.OwnerID, original.CommandID, original.ExpiresAt, ready.ConnectionID, before, final, receipt}))
	if err = conn.Close(websocket.StatusNormalClosure, "done"); err != nil {
		t.Fatal(err)
	}
	gatewayProcess.stop(t, false)
	secondProcess.stop(t, false)
}
