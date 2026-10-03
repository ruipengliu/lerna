package alternate_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/development"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type configuredNativeSource struct {
	t                                *testing.T
	cfg                              development.Config
	app                              *development.App
	live                             atomic.Bool
	cancel                           context.CancelFunc
	runDone                          chan error
	server                           *httptest.Server
	gets, calls                      atomic.Int64
	loseRegister                     atomic.Bool
	peerToken, peerTokenFile, caFile string
	peerRef                          api.ObjectRef
	keys                             *platform.Keyring
}

func newConfiguredNativeSource(t *testing.T, driver string) *configuredNativeSource {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	cfg, err := development.InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	s := &configuredNativeSource{t: t, cfg: cfg, peerToken: api.NewID("credential") + api.NewID("credential"), peerTokenFile: filepath.Join(root, "native-peer-token")}
	s.peerRef = runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID}.Ref(api.NewID("subject"), 1)
	if err = os.WriteFile(s.peerTokenFile, []byte(s.peerToken), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HARNESS_CONFIGURED_NATIVE_SOURCE_PEER", s.peerToken)
	s.keys, err = platform.OpenDevelopmentKey(cfg.KeyFile, cfg.TenantID, cfg.OwnerID, []string{"foreign_content"})
	if err != nil {
		t.Fatal(err)
	}
	s.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !s.live.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		method := ""
		if r.Method == "POST" && r.URL.Path == "/api/call" {
			s.calls.Add(1)
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				t.Error(err)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var frame struct {
				Payload json.RawMessage `json:"payload"`
			}
			var call struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(body, &frame)
			_ = json.Unmarshal(frame.Payload, &call)
			method = call.Method
			if method == "content.foreign.get" {
				s.gets.Add(1)
			}
		}
		// 业务 HTTPS 来自原 App.Run；外层只注入真实网络故障。
		forward := r.Clone(r.Context())
		forward.URL, _ = url.Parse("https://" + s.cfg.HTTPAddr + r.URL.RequestURI())
		forward.RequestURI = ""
		response, err := s.server.Client().Do(forward)
		if err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer response.Body.Close()
		if method == "content.foreign.register" && s.loseRegister.CompareAndSwap(true, false) {
			_, _ = io.Copy(io.Discard, response.Body)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err != nil {
				t.Error(err)
				return
			}
			_ = conn.Close()
			return
		}
		for key, values := range response.Header {
			w.Header()[key] = values
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	cert, key, _ := certificates(t, root)
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	s.caFile = cert
	s.cfg.ForeignSourceTLS = &development.ForeignSourceTLSConfig{CertificateFile: cert, KeyFile: key}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.cfg.HTTPAddr = listener.Addr().String()
	if err = listener.Close(); err != nil {
		t.Fatal(err)
	}
	s.server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS13}
	s.server.StartTLS()
	t.Cleanup(func() { s.server.Close(); s.close() })
	return s
}
func (s *configuredNativeSource) nativeConfig(f *fixture, c map[string]any) {
	f.tenant = s.cfg.TenantID
	key := s.keys.Keys["development-es256"]
	c["keys"] = []any{map[string]any{"kid": "development-es256", "tenant_id": s.cfg.TenantID, "issuer": s.cfg.OwnerID, "purposes": []string{"foreign_content"}, "jwk": map[string]any{"kty": "EC", "crv": "P-256", "x": base64.RawURLEncoding.EncodeToString(key.Public.X.FillBytes(make([]byte, 32))), "y": base64.RawURLEncoding.EncodeToString(key.Public.Y.FillBytes(make([]byte, 32)))}}}
	c["foreign_sources"] = []any{map[string]any{"owner_id": s.cfg.OwnerID, "database_id": s.cfg.DatabaseID, "origin": s.server.URL, "token_file": s.peerTokenFile, "ca_file": s.caFile, "key_id": "development-es256", "peer_subject_id": s.peerRef.ObjectID, "peer_generation": s.peerRef.Revision, "purposes": []string{"read", "memory.save", "memory.read"}, "location": "local"}}
}
func (s *configuredNativeSource) pair(t *testing.T, f *fixture) {
	t.Helper()
	scope := f.ownerScope(t)
	configBytes, err := os.ReadFile(f.configFile)
	if err != nil {
		t.Fatal(err)
	}
	var nativeConfig map[string]any
	if err = json.Unmarshal(configBytes, &nativeConfig); err != nil {
		t.Fatal(err)
	}
	nativeConfig["expected_database_id"] = scope.DatabaseID
	writeJSON(t, f.configFile, nativeConfig)
	s.cfg.ForeignConsumers = []development.ForeignConsumerConfig{{TenantID: scope.TenantID, OwnerID: scope.OwnerID, DatabaseID: scope.DatabaseID, PeerSubjectRef: s.peerRef, InboundTokenEnvRef: "HARNESS_CONFIGURED_NATIVE_SOURCE_PEER", Holders: []development.ForeignConsumerHolder{{SubjectRef: scope.Ref(f.subject, 1), Roles: []string{}}}, Purposes: []string{"read", "memory.save", "memory.read"}, Locations: []string{"local"}}}
	if err := development.SaveConfig(filepath.Join(s.cfg.DataRoot, "config.json"), s.cfg); err != nil {
		t.Fatal(err)
	}
	s.open(true)
}
func (s *configuredNativeSource) open(initialize bool) {
	app, err := development.OpenApp(context.Background(), s.cfg, initialize)
	if err != nil {
		s.t.Fatal(err)
	}
	s.app = app
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel, s.runDone = cancel, make(chan error, 1)
	go func() { s.runDone <- app.Run(ctx, true, false) }()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		response, err := s.server.Client().Get("https://" + s.cfg.HTTPAddr + "/health/ready")
		if err == nil {
			_, _ = io.Copy(io.Discard, response.Body)
			_ = response.Body.Close()
			if response.StatusCode == http.StatusOK {
				s.live.Store(true)
				return
			}
		}
		select {
		case err := <-s.runDone:
			s.t.Fatalf("actual App.Run HTTPS exited before ready: %v", err)
		case <-time.After(25 * time.Millisecond):
		}
	}
	s.t.Fatal("actual App.Run HTTPS readiness timed out")
}
func (s *configuredNativeSource) close() {
	s.live.Store(false)
	if s.cancel != nil {
		s.cancel()
		select {
		case err := <-s.runDone:
			if err != nil {
				s.t.Error(err)
			}
		case <-time.After(10 * time.Second):
			s.t.Error("actual App.Run HTTPS did not join")
		}
		s.cancel = nil
	}
	if s.app != nil {
		if err := s.app.Close(); err != nil {
			s.t.Error(err)
		}
		s.app = nil
	}
}
func (s *configuredNativeSource) originalReceipt(t *testing.T, id string) api.Receipt {
	t.Helper()
	transport := &harness.HTTPTransport{BaseURL: s.server.URL, Token: s.peerToken, HTTP: s.server.Client()}
	raw, err := transport.Call(context.Background(), "receipt_lookup", api.Raw(map[string]any{"logical_service_id": s.cfg.OwnerID, "command_id": id}))
	if err != nil {
		t.Fatal(err)
	}
	var receipt api.Receipt
	if err = api.Decode(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

func TestIndependentMemoryUsesConfiguredAppSourceOriginalCopyAndCurrentGate(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual HARNESS_TEST_POSTGRES_DSN")
			}
			s := newConfiguredNativeSource(t, driver)
			f := newFixture(t, "memory", s.nativeConfig)
			s.pair(t, f)
			if s.calls.Load() != 0 {
				t.Fatal("Native/Source construction performed outbound business calls")
			}
			ctx := context.Background()
			body := []byte(strings.Repeat("默认App原Source与独立Native准确字节\n", 2500))
			ref, err := s.app.Publish(ctx, s.app.Scope, s.app.UserAuth, api.NewID("content"), "text/plain", body, []api.ContentRef{}, []api.ContentRef{})
			if err != nil {
				t.Fatal(err)
			}
			// 派生 Memory 只保留两端共同许可；原 Native 通用夹具的 control.proof
			// 不在默认 App 正文政策中，不能为互操作放宽原来源政策。
			values := memory.PolicyValues{Subjects: []string{f.subject}, Purposes: []string{"content.write", "read", "memory.save", "memory.read"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(30 * time.Minute)), Continuous: true}
			digest, err := api.Digest(values)
			if err != nil {
				t.Fatal(err)
			}
			policy := api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}
			if receipt := f.command(t, "content.policy.install", policy.ComponentID, nil, memory.Policy{PolicyRef: policy, Values: values, Revision: 1, State: "active"}); receipt.Stage != "applied" {
				t.Fatalf("explicit intersected Native policy: %+v", receipt)
			}
			localScope := f.upload(t, policy, []byte("明确配对的独立Memory范围"), "text/plain")
			var references []memory.ForeignReference
			for _, purpose := range []string{"memory.save", "memory.read", "read"} {
				r := foreignReference(f, ref, purpose)
				if purpose == "memory.save" {
					s.loseRegister.Store(true)
				}
				raw, err := f.foreignCLI(t, "prepare-foreign-use", r, "")
				if purpose == "memory.save" {
					if err == nil {
						t.Fatal("lost registration reply fabricated Native host success")
					}
					original := s.originalReceipt(t, r.RegisterCommandID)
					if original.Stage != "applied" || original.CommandID != r.RegisterCommandID {
						t.Fatalf("lost reply erased original Source responsibility: %+v", original)
					}
					raw, err = f.foreignCLI(t, "prepare-foreign-use", r, "")
				}
				if err != nil {
					t.Fatalf("Native consumes configured App Source %s: %v %s", purpose, err, raw)
				}
				var use memory.ForeignUse
				if err = api.Decode(raw, &use); err != nil || use.Reference != r || use.Proof.ContentRef != ref || use.Proof.SourceDatabaseID != s.cfg.DatabaseID || use.Proof.Mode != "use" {
					t.Fatalf("original Source binding changed: %v", err)
				}
				references = append(references, r)
			}
			original := s.originalReceipt(t, references[0].RegisterCommandID)
			if original.Stage != "applied" {
				t.Fatalf("lost reply lost original applied copy: %+v", original)
			}
			id := api.NewID("memory")
			receipt := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: memory.MemoryValues{Type: "fact", ContentRef: ref, ScopeRef: localScope, PolicyRef: policy, ObservedAt: api.Time(time.Now()), Sources: []api.SourceEvidence{{ContentRef: ref, SourceKind: "user_input"}}}})
			if receipt.Stage != "applied" {
				t.Fatalf("real native Memory admission %+v", receipt)
			}
			var record memory.MemoryRecord
			if err = f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &record); err != nil || record.Values.ContentRef != ref {
				t.Fatalf("original foreign Memory read: %v", err)
			}
			url := f.address + "/api/content?ref=" + base64.RawURLEncoding.EncodeToString(api.Raw(ref)) + "&purpose=read&location=local"
			req, _ := http.NewRequest("GET", url, nil)
			req.Header.Set("Authorization", "Bearer "+f.token)
			res, err := f.http.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(res.Body)
			_ = res.Body.Close()
			if err != nil || res.StatusCode != 200 || !bytes.Equal(got, body) {
				t.Fatalf("accurate original foreign bytes: status=%d err=%v", res.StatusCode, err)
			}
			before := s.gets.Load()
			s.close()
			s.open(false)
			f.restart(t, true, false)
			if after := s.originalReceipt(t, references[0].RegisterCommandID); !api.Equal(after, original) {
				t.Fatal("reopen replaced original Source registration receipt")
			}
			if err = f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &record); err != nil || record.Values.ContentRef != ref {
				t.Fatalf("reopened current foreign Memory: %v", err)
			}
			if s.gets.Load() != before {
				t.Fatal("original held copy was physically fetched again during reopen/read")
			}
			one := uint64(1)
			closeReceipt, err := s.app.Dispatcher.Command(ctx, s.app.UserAuth, api.Raw(api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: s.cfg.OwnerID, CommandID: api.NewID("command"), Method: "content.close", TargetID: ref.ContentID, ExpectedRevision: &one, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: ref, Reason: "原Source通过公开入口关闭"})}))
			if err != nil || closeReceipt.Stage != "applied" {
				t.Fatalf("actual original Source close: %v %+v", err, closeReceipt)
			}
			if err = f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &record); !api.IsCode(err, "forbidden") && !api.IsCode(err, "gone") {
				t.Fatalf("closed Source permitted new native use: %v", err)
			}
			for _, r := range references {
				if raw, err := f.foreignCLI(t, "stop-foreign-copy", nil, r.CopyID); err != nil {
					t.Fatalf("actual original cleanup: %v %s", err, raw)
				}
				view := f.inspectForeign(t, r.CopyID)
				if view.Reference != r || view.Phase != "released" || !view.KnownDeny || view.CleanupState != "residual" || view.StopReport == nil || !view.StopReport.UseStopped {
					t.Fatalf("original Native cleanup facts: %+v", view)
				}
				if released := s.originalReceipt(t, r.ReleaseCommandID); released.Stage != "applied" || released.CommandID != r.ReleaseCommandID {
					t.Fatalf("original Source release responsibility: %+v", released)
				}
			}
			if s.gets.Load() != before {
				t.Fatal("close/control/release fetched withdrawn Source body")
			}
			t.Logf("configured App Source interoperability: %s", api.Raw(map[string]any{"source_tenant": s.cfg.TenantID, "source_owner": s.cfg.OwnerID, "source_database": s.cfg.DatabaseID, "consumer_scope": f.ownerScope(t), "original_content_ref": ref, "register_receipt": original.CommandID, "copy_ids": []string{references[0].CopyID, references[1].CopyID, references[2].CopyID}, "physical_chunks": before, "after_reopen_close_cleanup_chunks": s.gets.Load(), "source_driver": driver, "consumer_runtime": "independent Node24/native SQLite"}))
		})
	}
}
