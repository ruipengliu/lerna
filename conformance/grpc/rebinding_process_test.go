package grpc_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/endpointchannel"
	transport "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	rt "github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type applicationProcessConfig struct {
	Backend               string                         `json:"backend"`
	DatabasePath          string                         `json:"database_path"`
	DatabaseID            string                         `json:"database_id"`
	OwnerID               string                         `json:"owner_id"`
	ApplicationInstanceID string                         `json:"application_instance_id"`
	Auth                  rt.Auth                        `json:"auth"`
	TokenFile             string                         `json:"token_file"`
	CertificateFile       string                         `json:"certificate_file"`
	KeyFile               string                         `json:"key_file"`
	CAFile                string                         `json:"ca_file"`
	Registration          transport.EndpointRegistration `json:"registration"`
	ReadyFile             string                         `json:"ready_file"`
	CommittedFile         string                         `json:"committed_file"`
	CommandCallsFile      string                         `json:"command_calls_file"`
	HoldCommandReply      bool                           `json:"hold_command_reply"`
}
type committedOriginal struct {
	Command api.Command `json:"command"`
	Receipt api.Receipt `json:"receipt"`
}
type processApplication struct {
	cmd     *exec.Cmd
	config  applicationProcessConfig
	address string
	once    sync.Once
}

func (p *processApplication) stop(t *testing.T, kill bool) {
	t.Helper()
	p.once.Do(func() {
		sig := os.Signal(syscall.SIGTERM)
		if kill {
			sig = syscall.SIGKILL
		}
		if err := p.cmd.Process.Signal(sig); err != nil && !errors.Is(err, os.ErrProcessDone) {
			t.Error(err)
		}
		done := make(chan error, 1)
		go func() { done <- p.cmd.Wait() }()
		select {
		case err := <-done:
			if kill {
				var exit *exec.ExitError
				if !errors.As(err, &exit) || exit.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
					t.Error("original application did not exit from the actual SIGKILL")
				}
			} else if err != nil {
				t.Error(err)
			}
		case <-time.After(8 * time.Second):
			p.cmd.Process.Kill()
			t.Error("application process did not actually exit")
		}
	})
}
func echoProcessRegistry(st rt.Store, owner string) *rt.Registry {
	r := rt.NewRegistry()
	r.MustRegister(rt.Method{Contract: api.Contract[echoInput, echoOutput]("testing.persist", "grpctest", "command", false, false), Participants: []string{"grpctest"}, Apply: func(ctx context.Context, tx rt.Tx, a rt.Auth, c api.Command) (rt.Outcome, error) {
		var in echoInput
		if err := api.Decode(c.Payload, &in); err != nil {
			return rt.Outcome{}, err
		}
		out := echoOutput{in.Message, a.SubjectID}
		if err := tx.Create(ctx, "grpctest.messages", c.TargetID, "", out); err != nil {
			return rt.Outcome{}, err
		}
		return rt.Applied(out), nil
	}})
	r.MustRegister(rt.Method{Contract: api.Contract[struct{}, echoOutput]("testing.get", "grpctest", "query", false, false), Query: func(ctx context.Context, store rt.Store, scope rt.Scope, a rt.Auth, q api.Query) (any, error) {
		var out echoOutput
		_, err := store.Read(ctx, scope, "grpctest.messages", q.TargetID, 0, &out)
		return out, err
	}})
	return r
}

type holdCommittedProcessor struct {
	local  wss.LocalProcessor
	config applicationProcessConfig
}

func (p holdCommittedProcessor) Call(ctx context.Context, a rt.Auth, kind string, raw json.RawMessage) (string, json.RawMessage, error) {
	if kind == "command" {
		file, err := os.OpenFile(p.config.CommandCallsFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return "", nil, err
		}
		_, writeErr := file.Write(append(append([]byte{}, raw...), '\n'))
		if err = errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
			return "", nil, err
		}
	}
	resultKind, body, err := p.local.Call(ctx, a, kind, raw)
	if err == nil && kind == "command" && p.config.HoldCommandReply {
		var original committedOriginal
		if err = api.Decode(raw, &original.Command); err == nil {
			err = api.Decode(body, &original.Receipt)
		}
		if err == nil {
			err = os.WriteFile(p.config.CommittedFile, api.Raw(original), 0600)
		}
		if err != nil {
			return "", nil, err
		}
		<-ctx.Done()
		return "", nil, api.E("effect_unknown", "original_reply_interrupted_after_commit")
	}
	return resultKind, body, err
}

// 独立真实应用参考进程，只持有原Cloud owner/Store/配对，不取得物理目标目录。
func TestChannelApplicationHelperProcess(t *testing.T) {
	path := os.Getenv("HARNESS_CHANNEL_APPLICATION_CONFIG")
	if path == "" {
		t.Skip("private application subprocess entry")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c applicationProcessConfig
	if err = api.Decode(raw, &c); err != nil {
		t.Fatal(err)
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	var store rt.Store
	if c.Backend == "postgres" {
		store, err = postgres.Open(ctx, os.Getenv("HARNESS_TEST_POSTGRES_DSN"), postgres.WithExpectedDatabaseID(c.DatabaseID), postgres.WithMaxConnections(8))
	} else {
		store, err = sqlite.Open(c.DatabasePath, sqlite.WithExpectedDatabaseID(c.DatabaseID))
	}
	if err != nil {
		t.Fatal("configured original database unavailable")
	}
	defer store.Close()
	token, err := os.ReadFile(c.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	identity := &platform.DevIdentity{Store: store, OwnerID: c.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: c.Auth, TokenHash: api.Hash(token)}}}
	registry := echoProcessRegistry(store, c.OwnerID)
	methods := registry.Contracts()
	digest, err := api.Digest(methods)
	if err != nil {
		t.Fatal(err)
	}
	authority, err := transport.NewStaticEndpointAuthority(transport.StaticEndpointAuthorityConfig{OwnerID: c.OwnerID, GatewayIdentities: []string{"spiffe://harness.test/gateway"}, Identity: identity, Pairs: []transport.StaticEndpointPair{{Registration: c.Registration, Methods: methods}}})
	if err != nil {
		t.Fatal(err)
	}
	local := wss.LocalProcessor{Dispatcher: &rt.Dispatcher{Store: store, OwnerID: c.OwnerID, Registry: registry}}
	server, err := transport.New(transport.Config{OwnerID: c.OwnerID, Identity: identity, Processor: holdCommittedProcessor{local, c}, Store: store, MethodsDigest: digest, ApplicationInstanceID: c.ApplicationInstanceID, EndpointAuthority: authority, GatewayIdentities: []string{"spiffe://harness.test/gateway"}})
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := tls.LoadX509KeyPair(c.CertificateFile, c.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := os.ReadFile(c.CAFile)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		t.Fatal("invalid configured CA")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(c.ReadyFile, api.Raw(struct{ Address, DatabaseID, MethodsDigest, InstanceID string }{"grpcs://" + listener.Addr().String(), store.ID(), digest, c.ApplicationInstanceID}), 0600); err != nil {
		t.Fatal(err)
	}
	if err = server.Serve(ctx, listener, &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{certificate}, ClientCAs: roots, ClientAuth: tls.RequireAndVerifyClientCert}); err != nil {
		t.Fatal(err)
	}
}
func startChannelApplicationProcess(t *testing.T, f *unaryFixture, backend, databasePath string, reg transport.EndpointRegistration, hold bool) *processApplication {
	t.Helper()
	root := t.TempDir()
	c := applicationProcessConfig{Backend: backend, DatabasePath: databasePath, DatabaseID: f.store.ID(), OwnerID: f.owner, ApplicationInstanceID: api.NewID("instance"), Auth: f.auth, TokenFile: filepath.Join(root, "peer.token"), CertificateFile: filepath.Join(root, "server.pem"), KeyFile: filepath.Join(root, "server-key.pem"), CAFile: filepath.Join(root, "ca.pem"), Registration: reg, ReadyFile: filepath.Join(root, "ready.json"), CommittedFile: filepath.Join(root, "committed.json"), CommandCallsFile: filepath.Join(root, "command-calls.jsonl"), HoldCommandReply: hold}
	key, err := x509.MarshalPKCS8PrivateKey(f.serverTLS.Certificates[0].PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	cert := []byte{}
	for _, der := range f.serverTLS.Certificates[0].Certificate {
		cert = append(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	for path, bytes := range map[string][]byte{c.TokenFile: []byte(f.token), c.CertificateFile: cert, c.KeyFile: pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), c.CAFile: pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: f.serverTLS.Certificates[0].Certificate[1]})} {
		if err = os.WriteFile(path, bytes, 0600); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(root, "config.json")
	if err = os.WriteFile(path, api.Raw(c), 0600); err != nil {
		t.Fatal(err)
	}
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestChannelApplicationHelperProcess$", "-test.v")
	cmd.Env = append(os.Environ(), "HARNESS_CHANNEL_APPLICATION_CONFIG="+path)
	log, err := os.OpenFile(filepath.Join(root, "application.log"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { log.Close() })
	cmd.Stdout = log
	cmd.Stderr = log
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	p := &processApplication{cmd: cmd, config: c}
	t.Cleanup(func() { p.stop(t, false) })
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); {
		raw, err := os.ReadFile(c.ReadyFile)
		if err == nil {
			var ready struct{ Address, DatabaseID, MethodsDigest, InstanceID string }
			if err = api.Decode(raw, &ready); err != nil {
				t.Fatal(err)
			}
			if ready.DatabaseID != f.store.ID() || ready.MethodsDigest != f.discovery.MethodsDigest || ready.InstanceID != c.ApplicationInstanceID {
				t.Fatal("actual application identity/registry/database drift")
			}
			p.address = ready.Address
			return p
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual application did not start")
	return nil
}
func TestActualApplicationProcessSIGKILLRecoversOriginalInFlightCommand(t *testing.T) {
	for _, backend := range []string{"sqlite", "postgres"} {
		t.Run(backend, func(t *testing.T) {
			var store rt.Store
			var databasePath string
			var err error
			if backend == "postgres" {
				dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
				if dsn == "" {
					t.Skip("actual PostgreSQL configuration required")
				}
				p, e := postgres.Open(context.Background(), dsn, postgres.WithMaxConnections(8))
				if e != nil {
					t.Fatal(e)
				}
				if e = p.Migrate(context.Background()); e != nil {
					t.Fatal(e)
				}
				store = p
			} else {
				databasePath = filepath.Join(t.TempDir(), "shared-applications.sqlite")
				s, e := sqlite.Open(databasePath)
				if e != nil {
					t.Fatal(e)
				}
				if e = s.Migrate(context.Background()); e != nil {
					t.Fatal(e)
				}
				store = s
			}
			t.Cleanup(func() { store.Close() })
			f := newUnaryFixtureWithStore(t, store)
			_, reg := newStaticChannel(t, f)
			first := startChannelApplicationProcess(t, f, backend, databasePath, reg, true)
			second := startChannelApplicationProcess(t, f, backend, databasePath, reg, false)
			router, err := endpointchannel.NewRouter(endpointchannel.Config{OwnerID: f.owner, GatewayInstanceID: api.NewID("instance"), MethodsDigest: f.discovery.MethodsDigest, Applications: []endpointchannel.Application{{Address: first.address, ClientTLS: f.clientTLS}, {Address: second.address, ClientTLS: f.clientTLS}}, Registrations: []transport.EndpointRegistration{reg}, Identity: f.identity, Credentials: func(ctx context.Context, a rt.Auth) (string, error) {
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
			journal, err := harness.OpenJournal(t.TempDir(), discovery.IdentityScope)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			client, err := harness.NewClient(ws, journal, discovery)
			if err != nil {
				t.Fatal(err)
			}
			original := commandFor(f)
			type outcome struct {
				receipt api.Receipt
				err     error
			}
			finished := make(chan outcome, 1)
			started := time.Now()
			go func() { r, e := client.Send(ctx, original); finished <- outcome{r, e} }()
			var actual committedOriginal
			for deadline := time.Now().Add(4 * time.Second); time.Now().Before(deadline); {
				raw, e := os.ReadFile(first.config.CommittedFile)
				if e == nil {
					if err = api.Decode(raw, &actual); err != nil {
						t.Fatal(err)
					}
					break
				}
				time.Sleep(5 * time.Millisecond)
			}
			if !api.Equal(actual.Command, original) || actual.Receipt.Stage != "applied" {
				t.Fatal("original application has no actual committed original command")
			}
			before := router.States()
			first.stop(t, true)
			var recovered outcome
			select {
			case recovered = <-finished:
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			if recovered.err != nil || !api.Equal(recovered.receipt, actual.Receipt) {
				t.Fatalf("original in-flight receipt not recovered: %+v %v", recovered.receipt, recovered.err)
			}
			if time.Since(started) > 5*time.Second {
				t.Fatal("rebind refreshed original request waiting window")
			}
			after := router.States()
			if len(before) != 1 || len(after) != 1 || before[0].ConnectionID != after[0].ConnectionID || before[0].BindingID == after[0].BindingID || after[0].BindingRevision <= before[0].BindingRevision || after[0].LastRequestSeq != 1 {
				t.Fatalf("original in-flight connection/seq drift: %+v %+v", before, after)
			}
			entry, e := journal.Read(ctx, original.CommandID)
			if e != nil || !api.Equal(entry.Command, original) {
				t.Fatal("journal ID/payload/TTL changed during internal rebind")
			}
			facts, e := store.List(ctx, rt.Scope{TenantID: f.auth.TenantID, OwnerID: f.owner, DatabaseID: store.ID()}, "grpctest.messages", "", "", 10)
			if e != nil || len(facts) != 1 {
				t.Fatalf("original command resend duplicated domain fact: %d %v", len(facts), e)
			}
			calls, e := os.ReadFile(first.config.CommandCallsFile)
			if e != nil || string(calls) != string(api.Raw(original))+"\n" {
				t.Fatal("original physical command ingress is not exactly once")
			}
			if _, e = os.Stat(second.config.CommandCallsFile); !errors.Is(e, os.ErrNotExist) {
				t.Fatal("replacement application physically received a blind command resend")
			}
			binary, e := os.ReadFile(first.cmd.Path)
			if e != nil {
				t.Fatal(e)
			}
			t.Logf("CHANNEL_PROCESS_EVIDENCE %s", api.Raw(struct {
				Backend, DatabaseID, OwnerID, CommandID, OriginalTTL, BinarySHA256 string
				First, Replacement                                                 endpointchannel.State
				ElapsedMilliseconds                                                int64
				Processes                                                          []int
			}{backend, store.ID(), f.owner, original.CommandID, original.ExpiresAt, api.Hash(binary), before[0], after[0], time.Since(started).Milliseconds(), []int{first.cmd.Process.Pid, second.cmd.Process.Pid}}))
			ws.Close()
			router.Close()
			second.stop(t, false)
		})
	}
}
