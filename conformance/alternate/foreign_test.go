package alternate_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

// 只预置显式主体/peer配对；Body、CopyHolder、签名、源许可与原回执均是真实服务事实。
type sourceCurrentAuthority struct {
	peer, holder runtime.Auth
	consumer     atomic.Value
	denied       atomic.Bool
}

func (a *sourceCurrentAuthority) credential(ctx context.Context, tx runtime.Tx, auth runtime.Auth) error {
	var record struct {
		Revision   uint64   `json:"revision"`
		SubjectID  string   `json:"subject_id"`
		Generation uint64   `json:"generation"`
		State      string   `json:"state"`
		Roles      []string `json:"roles"`
	}
	if _, err := tx.Get(ctx, "platform.credentials", auth.SubjectID, &record); err != nil {
		return err
	}
	if record.State != "active" || record.Generation != auth.CredentialGeneration || !api.Equal(record.Roles, auth.Roles) {
		return api.E("forbidden", "original_source_credential_not_current")
	}
	return nil
}
func (a *sourceCurrentAuthority) ResolveSourceSubjectTx(ctx context.Context, tx runtime.Tx, peer runtime.Auth, ref memory.ForeignReference, control bool) (runtime.Auth, error) {
	if !api.Equal(peer, a.peer) || ref.HolderRef.OwnerID != a.consumer.Load() || ref.ReferenceIntentRef.OwnerID != a.consumer.Load() || ref.HolderRef.ObjectID != a.holder.SubjectID || ref.HolderRef.TenantID != a.holder.TenantID || ref.HolderRef.Revision != a.holder.CredentialGeneration {
		return runtime.Auth{}, api.E("forbidden", "original_source_pair_not_registered")
	}
	if err := a.credential(ctx, tx, peer); err != nil {
		return runtime.Auth{}, err
	}
	if err := a.credential(ctx, tx, a.holder); err != nil {
		return runtime.Auth{}, err
	}
	return a.holder, nil
}
func (a *sourceCurrentAuthority) Check(ctx context.Context, tx runtime.Tx, auth runtime.Auth, _ api.ComponentRef, _, _ string, _ bool) (uint64, error) {
	if err := a.credential(ctx, tx, auth); err != nil {
		return 0, err
	}
	if a.denied.Load() {
		return 0, api.E("forbidden", "source_current_grant_revoked")
	}
	return 1, nil
}
func (a *sourceCurrentAuthority) Visibility(ctx context.Context, tx runtime.Tx, auth runtime.Auth) (string, error) {
	if err := a.credential(ctx, tx, auth); err != nil {
		return "", err
	}
	return api.Digest([]any{auth.SubjectID, auth.CredentialGeneration, a.denied.Load()})
}

type nativeSource struct {
	dir, tokenFile, caFile string
	scope                  runtime.Scope
	auth, peer             runtime.Auth
	store                  *sqlite.Store
	service                *memory.Service
	policy                 api.ComponentRef
	keys                   *platform.Keyring
	authority              *sourceCurrentAuthority
	server                 *httptest.Server
	client                 *harness.Client
	loseRegister           atomic.Bool
	failCurrent            atomic.Bool
	failAfterGet           atomic.Bool
	forceControl           atomic.Bool
	gets                   atomic.Int64
}

func newNativeSource(t *testing.T) *nativeSource {
	t.Helper()
	s := &nativeSource{dir: t.TempDir()}
	ctx := context.Background()
	var err error
	s.store, err = sqlite.Open(filepath.Join(s.dir, "actual-source.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	if err = s.store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	s.scope = runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: s.store.ID()}
	s.auth = runtime.Auth{TenantID: s.scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1, Roles: []string{"memory_admin", "content_admin"}}
	s.peer = runtime.Auth{TenantID: s.scope.TenantID, SubjectID: api.NewID("peer"), CredentialGeneration: 1, Roles: []string{"foreign_content_peer"}}
	s.authority = &sourceCurrentAuthority{peer: s.peer, holder: s.auth}
	s.keys, err = platform.NewDevelopmentKey(s.scope.TenantID, s.scope.OwnerID, []string{"foreign_content"})
	if err != nil {
		t.Fatal(err)
	}
	objects, err := objectstore.OpenLocal(filepath.Join(s.dir, "objects"), memory.MaxContentBytes)
	if err != nil {
		t.Fatal(err)
	}
	s.service = memory.New(s.store, objects)
	s.service.Authorization = s.authority
	if err = s.service.ConfigureParticipants("platform"); err != nil {
		t.Fatal(err)
	}
	userToken, peerToken := api.NewID("credential"), api.NewID("credential")
	s.tokenFile = filepath.Join(s.dir, "paired-peer-token")
	if err = os.WriteFile(s.tokenFile, []byte(peerToken), 0600); err != nil {
		t.Fatal(err)
	}
	identity := &platform.DevIdentity{Store: s.store, OwnerID: s.scope.OwnerID, SessionTTL: time.Hour, Principals: []platform.Principal{{Auth: s.auth, TokenHash: api.Hash([]byte(userToken))}, {Auth: s.peer, TokenHash: api.Hash([]byte(peerToken))}}}
	if err = identity.Initialize(ctx); err != nil {
		t.Fatal(err)
	}
	values := memory.PolicyValues{Subjects: []string{s.auth.SubjectID}, Purposes: []string{"content.write", "read", "memory.save", "memory.read", "memory.query", "memory.sync", "brain.input", "brain.output", "execution.arguments", "execution.result", "control.proof"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(2 * time.Hour)), Continuous: true}
	digest, _ := api.Digest(values)
	s.policy = api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}
	policy, err := memory.NewPolicy(s.policy, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = s.service.InstallPolicy(ctx, s.scope, s.auth, policy); err != nil {
		t.Fatalf("explicit actual source policy %v", err)
	}
	registry := runtime.NewRegistry()
	s.service.Register(registry)
	fa, err := providers.NewForeignSource(providers.ForeignSourceConfig{Store: s.store, Scope: s.scope, Memory: s.service, Keys: s.keys, SigningKeyID: "development-es256", Authority: s.authority, Participants: []string{"platform"}})
	if err != nil {
		t.Fatal(err)
	}
	if err = fa.Register(registry); err != nil {
		t.Fatal(err)
	}
	dispatch := &runtime.Dispatcher{Store: s.store, OwnerID: s.scope.OwnerID, Registry: registry}
	gateway, err := wss.New(wss.Config{OwnerID: s.scope.OwnerID, Store: s.store, Registry: registry, Identity: identity, Processor: wss.LocalProcessor{Dispatcher: dispatch}, Content: s.service, Uploader: s.service, Location: "local", MaxConnections: 8, MaxQueuedBytes: 4 << 20})
	if err != nil {
		t.Fatal(err)
	}
	handler := gateway.Handler()
	s.server = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "POST" && r.URL.Path == "/api/call" {
			body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
			if err != nil {
				t.Error(err)
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			var frame struct {
				Kind    string          `json:"kind"`
				Payload json.RawMessage `json:"payload"`
			}
			var call struct {
				Method string `json:"method"`
			}
			_ = json.Unmarshal(body, &frame)
			_ = json.Unmarshal(frame.Payload, &call)
			if call.Method == "content.foreign.get" {
				s.gets.Add(1)
				if s.failAfterGet.CompareAndSwap(true, false) {
					defer s.failCurrent.Store(true)
				}
			}
			if call.Method == "content.foreign.current" && s.failCurrent.Load() {
				w.WriteHeader(503)
				return
			}
			if call.Method == "content.foreign.current" && s.forceControl.Load() {
				var query api.Query
				if err := api.Decode(frame.Payload, &query); err != nil {
					t.Error(err)
					return
				}
				var payload map[string]json.RawMessage
				if err := json.Unmarshal(query.Payload, &payload); err != nil {
					t.Error(err)
					return
				}
				payload["control"] = json.RawMessage("true")
				query.Payload = api.Raw(payload)
				r.Body = io.NopCloser(bytes.NewReader(api.Raw(map[string]any{"kind": frame.Kind, "payload": query})))
			}
			if call.Method == "content.foreign.register" && s.loseRegister.CompareAndSwap(true, false) {
				capture := httptest.NewRecorder()
				handler.ServeHTTP(capture, r)
				conn, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error(err)
					return
				}
				_ = conn.Close()
				return
			}
		}
		handler.ServeHTTP(w, r)
	}))
	cert, key, _ := certificates(t, s.dir)
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	s.caFile = cert
	s.server.TLS = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	s.server.StartTLS()
	transport := &harness.HTTPTransport{BaseURL: s.server.URL, Token: userToken, HTTP: s.server.Client()}
	discovery, err := transport.Discover(ctx)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := harness.OpenJournal(filepath.Join(s.dir, "original-source-journal"), discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	s.client, err = harness.NewClient(transport, journal, discovery)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		s.server.Close()
		if err := journal.Close(); err != nil {
			t.Error(err)
		}
		if err := s.store.Close(); err != nil {
			t.Error(err)
		}
	})
	return s
}

func (s *nativeSource) configure(t *testing.T) func(*fixture, map[string]any) {
	return func(f *fixture, c map[string]any) {
		f.tenant, f.subject = s.scope.TenantID, s.auth.SubjectID
		s.authority.consumer.Store(f.owner)
		key := s.keys.Keys["development-es256"]
		c["keys"] = []any{map[string]any{"kid": "development-es256", "tenant_id": s.scope.TenantID, "issuer": s.scope.OwnerID, "purposes": []string{"foreign_content"}, "jwk": map[string]any{"kty": "EC", "crv": "P-256", "x": base64.RawURLEncoding.EncodeToString(key.Public.X.FillBytes(make([]byte, 32))), "y": base64.RawURLEncoding.EncodeToString(key.Public.Y.FillBytes(make([]byte, 32)))}}}
		c["foreign_sources"] = []any{map[string]any{"owner_id": s.scope.OwnerID, "database_id": s.scope.DatabaseID, "origin": s.server.URL, "token_file": s.tokenFile, "ca_file": s.caFile, "key_id": "development-es256", "peer_subject_id": s.peer.SubjectID, "peer_generation": s.peer.CredentialGeneration, "purposes": []string{"read", "memory.save", "memory.read", "memory.query", "memory.sync"}, "location": "local"}}
	}
}

func (s *nativeSource) command(t *testing.T, method, target string, revision *uint64, payload any) api.Receipt {
	t.Helper()
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: s.scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: target, ExpectedRevision: revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(payload)}
	receipt, err := s.client.Send(context.Background(), command)
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("actual source %s %+v %v", method, receipt, err)
	}
	return receipt
}
func (s *nativeSource) publish(t *testing.T, body []byte) api.ContentRef {
	t.Helper()
	ref := api.ContentRef{TenantID: s.scope.TenantID, OwnerID: s.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(body), MediaType: "text/plain", ByteLength: uint64(len(body))}
	transfer := api.NewID("transfer")
	retention, deadline := api.Time(time.Now().Add(time.Hour)), api.Time(time.Now().Add(time.Minute))
	s.command(t, "content.upload_reserve", ref.ContentID, nil, memory.ReserveInput{TransferID: transfer, ContentRef: ref, PolicyRef: s.policy, ProcessedSources: []api.ContentRef{}, RetentionUntil: retention, TransferDeadline: deadline})
	req, err := http.NewRequest("POST", s.server.URL+"/api/transfers/"+transfer, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	transport := s.client.Transport.(*harness.HTTPTransport)
	req.Header.Set("Authorization", "Bearer "+transport.Token)
	response, err := s.server.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	if err != nil || response.StatusCode != 200 {
		t.Fatalf("actual source bytes status %d %v %s", response.StatusCode, err, b)
	}
	s.command(t, "content.put", ref.ContentID, nil, memory.PutInput{ContentRef: ref, TransferID: transfer, PolicyRef: s.policy, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: retention})
	return ref
}
func foreignReference(f *fixture, ref api.ContentRef, purpose string) memory.ForeignReference {
	return memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: api.ObjectRef{TenantID: f.tenant, OwnerID: f.owner, ObjectID: api.NewID("intent"), Revision: 1}, HolderRef: api.ObjectRef{TenantID: f.tenant, OwnerID: f.owner, ObjectID: f.subject, Revision: 1}, Purpose: purpose, Location: "local", RetainUntil: api.Time(time.Now().Add(45 * time.Minute))}
}
func (f *fixture) foreignCLI(t *testing.T, mode string, input any, copyID string) ([]byte, error) {
	t.Helper()
	args := []string{filepath.Join(f.repo, "adapters/alternate/ts/dist/main.mjs"), mode, "--config", f.configFile, "--subject", f.subject}
	if input != nil {
		path := filepath.Join(f.dir, "original-host-reference.json")
		writeJSON(t, path, input)
		args = append(args, "--record", path)
	}
	if copyID != "" {
		args = append(args, "--copy-id", copyID)
	}
	return exec.Command("node", args...).CombinedOutput()
}

func (s *nativeSource) originalReceipt(t *testing.T, id string) api.Receipt {
	t.Helper()
	token, err := os.ReadFile(s.tokenFile)
	if err != nil {
		t.Fatal(err)
	}
	transport := &harness.HTTPTransport{BaseURL: s.server.URL, Token: string(token), HTTP: s.server.Client()}
	out, err := transport.Call(context.Background(), "receipt_lookup", api.Raw(map[string]any{"logical_service_id": s.scope.OwnerID, "command_id": id}))
	if err != nil {
		t.Fatal(err)
	}
	var receipt api.Receipt
	if err = api.Decode(out, &receipt); err != nil {
		t.Fatal(err)
	}
	return receipt
}

type foreignCopyView struct {
	Reference    memory.ForeignReference  `json:"reference"`
	Phase        string                   `json:"phase"`
	KnownDeny    bool                     `json:"known_deny"`
	WriteIntent  bool                     `json:"write_intent"`
	Written      bool                     `json:"written"`
	CleanupState string                   `json:"cleanup_state"`
	StopReport   *memory.ReleaseCopyInput `json:"stop_report"`
}

func (f *fixture) inspectForeign(t *testing.T, copyID string) foreignCopyView {
	t.Helper()
	out, err := f.foreignCLI(t, "inspect-foreign-copy", nil, copyID)
	if err != nil {
		t.Fatalf("public original holder inspection %v %s", err, out)
	}
	var view foreignCopyView
	if err := api.Decode(out, &view); err != nil {
		t.Fatal(err)
	}
	return view
}

func pauseForeignJobs(f *fixture, c map[string]any) { c["fault"] = map[string]any{"pause_jobs": true} }

func TestIndependentMemoryForeignLostRegistrationAndWrittenPendingCleanup(t *testing.T) {
	source := newNativeSource(t)
	f := newFixture(t, "memory", source.configure(t), pauseForeignJobs)
	ref := source.publish(t, []byte("原命令的回执已持久，写后当前许可答复故障仍保留责任。"))
	reference := foreignReference(f, ref, "memory.read")
	source.loseRegister.Store(true)
	out, err := f.foreignCLI(t, "prepare-foreign-use", reference, "")
	if err == nil {
		t.Fatalf("lost actual source registration reply fabricated host success: %s", out)
	}
	original := source.originalReceipt(t, reference.RegisterCommandID)
	if original.Stage != "applied" || original.CommandID != reference.RegisterCommandID {
		t.Fatalf("source lost reply was not committed original receipt: %+v", original)
	}
	view := f.inspectForeign(t, reference.CopyID)
	if view.Reference != reference || view.Phase != "reference_intent" || view.Written || view.CleanupState != "pending" {
		t.Fatalf("lost reply erased original host responsibility: %+v", view)
	}
	source.failAfterGet.Store(true)
	out, err = f.foreignCLI(t, "prepare-foreign-use", reference, "")
	if err == nil {
		t.Fatalf("post-write actual source current failure fabricated held gate: %s", out)
	}
	view = f.inspectForeign(t, reference.CopyID)
	if view.Reference != reference || view.Phase != "writing" || !view.WriteIntent || !view.Written || view.CleanupState != "pending" {
		t.Fatalf("written original mirror lost pending cleanup: %+v", view)
	}
	if receipt := source.originalReceipt(t, reference.RegisterCommandID); !api.Equal(receipt, original) {
		t.Fatal("host recovery renewed original registration identity/defaults/receipt")
	}
	source.failCurrent.Store(false)
	one := uint64(1)
	source.command(t, "content.close", ref.ContentID, &one, memory.CloseInput{ContentRef: ref, Reason: "写后关闭源；只恢复原控制及清理"})
	before := source.gets.Load()
	out, err = f.foreignCLI(t, "stop-foreign-copy", nil, reference.CopyID)
	if err != nil {
		t.Fatalf("recover original post-write cleanup %v %s", err, out)
	}
	view = f.inspectForeign(t, reference.CopyID)
	if view.Phase != "released" || view.CleanupState != "residual" || !view.KnownDeny || view.StopReport == nil || !view.StopReport.UseStopped || source.gets.Load() != before {
		t.Fatalf("original stop fabricated physical erasure or read withdrawn body: %+v", view)
	}
	if receipt := source.originalReceipt(t, reference.ReleaseCommandID); receipt.Stage != "applied" || receipt.CommandID != reference.ReleaseCommandID {
		t.Fatalf("release did not retain original source identity: %+v", receipt)
	}
}

func TestIndependentMemoryForeignActualSIGKILLAfterWriteOriginalCleanup(t *testing.T) {
	source := newNativeSource(t)
	f := newFixture(t, "memory", source.configure(t), pauseForeignJobs)
	f.stop(t)
	config := map[string]any{}
	b, err := os.ReadFile(f.configFile)
	if err != nil || json.Unmarshal(b, &config) != nil {
		t.Fatal(err)
	}
	config["fault"] = map[string]any{"pause_jobs": true, "crash_after_foreign_write": true}
	writeJSON(t, f.configFile, config)
	ref := source.publish(t, []byte("SIGKILL 必须保留真实写后原副本责任，不再读取已撤回源。"))
	reference := foreignReference(f, ref, "memory.read")
	out, err := f.foreignCLI(t, "prepare-foreign-use", reference, "")
	var exit *exec.ExitError
	if !errors.As(err, &exit) || !exit.ProcessState.Sys().(syscall.WaitStatus).Signaled() || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGKILL {
		t.Fatalf("write fault did not actually kill original Node process: %v %s", err, out)
	}
	view := f.inspectForeign(t, reference.CopyID)
	if view.Phase != "writing" || !view.WriteIntent || !view.Written || view.CleanupState != "pending" || view.Reference != reference {
		t.Fatalf("SIGKILL erased already written original responsibility: %+v", view)
	}
	config["fault"] = map[string]any{"pause_jobs": true}
	writeJSON(t, f.configFile, config)
	one := uint64(1)
	source.command(t, "content.close", ref.ContentID, &one, memory.CloseInput{ContentRef: ref, Reason: "故障期间真实源关闭"})
	before := source.gets.Load()
	// 原进程 Claim 的实际十秒期限不刷新、不绕过；新实例等待再领取原 Job。
	deadline := time.Now().Add(12 * time.Second)
	for {
		out, err = f.foreignCLI(t, "stop-foreign-copy", nil, reference.CopyID)
		if err == nil {
			break
		}
		if time.Now().After(deadline) || !strings.Contains(string(out), "original_foreign_work_in_progress") {
			t.Fatalf("original crashed claim cleanup recovery %v %s", err, out)
		}
		time.Sleep(500 * time.Millisecond)
	}
	view = f.inspectForeign(t, reference.CopyID)
	if view.Phase != "released" || view.CleanupState != "residual" || !view.KnownDeny || source.gets.Load() != before {
		t.Fatalf("crashed original copy cleanup false complete or reread: %+v", view)
	}
	f.start(t)
	f.connect(t)
	if again := source.originalReceipt(t, reference.RegisterCommandID); again.Stage != "applied" {
		t.Fatalf("restart lost exact registration receipt: %+v", again)
	}
}

func TestIndependentMemoryForeignCurrentGrantRevocationPersistsKnownDeny(t *testing.T) {
	source := newNativeSource(t)
	f := newFixture(t, "memory", source.configure(t), pauseForeignJobs)
	ref := source.publish(t, []byte("Current authority 不能使用上次签名代替。"))
	reference := foreignReference(f, ref, "memory.read")
	if out, err := f.foreignCLI(t, "prepare-foreign-use", reference, ""); err != nil {
		t.Fatalf("initial exact hold %v %s", err, out)
	}
	var current memory.GetContentOutput
	input := memory.GetContentInput{ContentRef: ref, Mode: "bytes", Purpose: "memory.read", Location: "local"}
	if err := f.query(t, "content.get", ref.ContentID, input, &current); err != nil {
		t.Fatalf("current original held content was not readable: %v", err)
	}
	source.authority.denied.Store(true)
	if err := f.query(t, "content.get", ref.ContentID, input, &current); !api.IsCode(err, "forbidden") {
		t.Fatalf("revoked current source grant reused old proof: %v", err)
	}
	view := f.inspectForeign(t, reference.CopyID)
	if !view.KnownDeny || view.Phase != "stopping" || view.CleanupState != "pending" {
		t.Fatalf("signed current deny did not persist original stopping gate: %+v", view)
	}
	source.authority.denied.Store(false)
	before := source.gets.Load()
	if out, err := f.foreignCLI(t, "prepare-foreign-use", reference, ""); err == nil {
		t.Fatalf("later permit erased locally known original denial: %s", out)
	}
	view = f.inspectForeign(t, reference.CopyID)
	if !view.KnownDeny || view.Phase != "released" || source.gets.Load() != before {
		t.Fatalf("denied original responsibility reused source body: %+v", view)
	}
}

func TestIndependentMemoryForeignExactSourceBindingsRejectBeforeBody(t *testing.T) {
	source := newNativeSource(t)
	ref := source.publish(t, []byte("原 source owner、真实数据库、已登记键、holder 与 use mode 不能替换。"))
	for _, test := range []struct {
		name   string
		config func(*fixture, map[string]any)
		ref    func(*memory.ForeignReference)
	}{
		{"database", func(_ *fixture, c map[string]any) {
			c["foreign_sources"].([]any)[0].(map[string]any)["database_id"] = api.NewID("database")
		}, nil},
		{"key", func(_ *fixture, c map[string]any) {
			other, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"foreign_content"})
			if err != nil {
				t.Fatal(err)
			}
			c["keys"].([]any)[0].(map[string]any)["jwk"] = json.RawMessage(platform.PublicJWK(other.Keys["development-es256"].Public))
		}, nil},
		{"holder_generation", nil, func(r *memory.ForeignReference) { r.HolderRef.Revision = 2 }},
		{"original_owner", nil, func(r *memory.ForeignReference) { r.ContentRef.OwnerID = r.HolderRef.OwnerID }},
		{"control_is_not_use", func(_ *fixture, _ map[string]any) { source.forceControl.Store(true) }, nil},
	} {
		t.Run(test.name, func(t *testing.T) {
			source.forceControl.Store(false)
			configuration := []func(*fixture, map[string]any){source.configure(t), pauseForeignJobs}
			if test.config != nil {
				configuration = append(configuration, test.config)
			}
			f := newFixture(t, "memory", configuration...)
			reference := foreignReference(f, ref, "memory.read")
			if test.ref != nil {
				test.ref(&reference)
			}
			before := source.gets.Load()
			if out, err := f.foreignCLI(t, "prepare-foreign-use", reference, ""); err == nil {
				t.Fatalf("unbound source %s was admitted: %s", test.name, out)
			}
			if source.gets.Load() != before {
				t.Fatalf("invalid %s proof/reference reached source body", test.name)
			}
		})
	}
}

func TestIndependentMemoryForeignOriginalExpiryDoesNotRenewOrLoseCleanup(t *testing.T) {
	source := newNativeSource(t)
	f := newFixture(t, "memory", source.configure(t), pauseForeignJobs)
	ref := source.publish(t, []byte("原准确 retain_until，不由重开或较新的在线 proof 延长。"))
	reference := foreignReference(f, ref, "memory.read")
	until := time.Now().Add(12 * time.Second)
	reference.RetainUntil = api.Time(until)
	if out, err := f.foreignCLI(t, "prepare-foreign-use", reference, ""); err != nil {
		t.Fatalf("initial bounded actual hold %v %s", err, out)
	}
	var body memory.GetContentOutput
	input := memory.GetContentInput{ContentRef: ref, Mode: "bytes", Purpose: "memory.read", Location: "local"}
	if err := f.query(t, "content.get", ref.ContentID, input, &body); err != nil {
		t.Fatalf("original hold was not readable before its deadline: %v", err)
	}
	if remaining := time.Until(until); remaining > 0 {
		time.Sleep(remaining + 10*time.Millisecond)
	}
	if err := f.query(t, "content.get", ref.ContentID, input, &body); !api.IsCode(err, "forbidden") {
		t.Fatalf("fresh source signature renewed original retain_until: %v", err)
	}
	view := f.inspectForeign(t, reference.CopyID)
	if !view.KnownDeny || view.Phase != "stopping" || view.CleanupState != "pending" || view.Reference != reference {
		t.Fatalf("lease expiry fabricated cleanup completion or changed original reference: %+v", view)
	}
	before := source.gets.Load()
	if out, err := f.foreignCLI(t, "stop-foreign-copy", nil, reference.CopyID); err != nil {
		t.Fatalf("expired original responsibility control/release %v %s", err, out)
	}
	view = f.inspectForeign(t, reference.CopyID)
	if view.Phase != "released" || view.CleanupState != "residual" || view.Reference != reference || source.gets.Load() != before {
		t.Fatalf("expiry cleanup fabricated physical erasure/read/rebinding: %+v", view)
	}
}

func TestIndependentMemoryForeignQueryViewsAndCurrentWithdrawal(t *testing.T) {
	source := newNativeSource(t)
	f := newFixture(t, "memory", source.configure(t), pauseForeignJobs)
	policy := f.policy(t)
	scope := f.upload(t, policy, []byte("原独立查询/同步范围"), "text/plain")
	first := source.publish(t, []byte("alpha 原事实"))
	second := source.publish(t, []byte("alpha 已纠正事实"))
	textRef := source.publish(t, []byte("alpha"))
	spec := memory.MemoryQuerySpec{TextRef: textRef, TypeFilter: []string{"fact"}, RankingProfileRef: memory.LiteralProfile()}
	queryRef := source.publish(t, api.Raw(spec))
	for _, ref := range []api.ContentRef{first, second} {
		for _, purpose := range []string{"memory.save", "memory.read", "memory.query", "memory.sync"} {
			if out, err := f.foreignCLI(t, "prepare-foreign-use", foreignReference(f, ref, purpose), ""); err != nil {
				t.Fatalf("original foreign candidate hold %v %s", err, out)
			}
		}
	}
	for _, ref := range []api.ContentRef{queryRef, textRef} {
		if out, err := f.foreignCLI(t, "prepare-foreign-use", foreignReference(f, ref, "memory.query"), ""); err != nil {
			t.Fatalf("original foreign query input hold %v %s", err, out)
		}
	}
	id := api.NewID("memory")
	values := memory.MemoryValues{Type: "fact", ContentRef: first, Sources: []api.SourceEvidence{{ContentRef: first, SourceKind: "user_input"}}, ScopeRef: scope, PolicyRef: policy, ObservedAt: api.Time(time.Now())}
	if receipt := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values}); receipt.Stage != "applied" {
		t.Fatalf("original foreign semantic record admission %+v", receipt)
	}
	var records api.Page[memory.MemoryRecord]
	if err := f.query(t, "memory.list", f.owner, memory.ListMemoryInput{Purpose: "memory.read", Limit: 20}, &records); err != nil || len(records.Items) != 1 || records.Items[0].Values.ContentRef != first || records.Partial {
		t.Fatalf("foreign list truthful current snapshot %+v %v", records, err)
	}
	query := memory.QueryInput{QueryRef: queryRef, ScopeRef: scope, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 20, MaxReadBytes: 65536, MaxPermissionChecks: 100, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 20}
	var matches api.Page[memory.Match]
	if err := f.query(t, "memory.query", f.owner, query, &matches); err != nil || len(matches.Items) != 1 || matches.Items[0].ContentRef != first || matches.Partial {
		t.Fatalf("foreign exact query spec/text did not use current original source %+v %v", matches, err)
	}
	viewID := api.NewID("view")
	receipt := f.command(t, "memory.view.open", viewID, nil, memory.OpenViewInput{ViewID: viewID, ScopeRef: scope, Purposes: []string{"memory.sync"}, HolderRef: api.ObjectRef{TenantID: f.tenant, OwnerID: f.owner, ObjectID: f.subject, Revision: 1}, Location: "local", MaxCandidates: 20})
	if receipt.Stage != "applied" {
		t.Fatalf("foreign current view did not open %+v", receipt)
	}
	var opened memory.ViewOutput
	if err := api.Decode(receipt.Output, &opened); err != nil {
		t.Fatal(err)
	}
	var page memory.ViewPage
	if err := f.query(t, "memory.view.pull", viewID, memory.PullViewInput{ViewID: viewID, Cursor: opened.Cursor, Limit: 20}, &page); err != nil || len(page.Records) != 1 || page.Records[0].Values.ContentRef != first {
		t.Fatalf("foreign view exact snapshot %+v %v", page, err)
	}
	if receipt = f.command(t, "memory.view.ack", viewID, nil, memory.AckViewInput{ViewID: viewID, Cursor: page.Cursor, ReceiptRef: api.ObjectRef{TenantID: f.tenant, OwnerID: f.owner, ObjectID: api.NewID("receipt"), Revision: 1}}); receipt.Stage != "applied" {
		t.Fatalf("foreign view original delivery ack %+v", receipt)
	}
	values.ContentRef, values.Sources = second, []api.SourceEvidence{{ContentRef: second, SourceKind: "user_input"}}
	one := uint64(1)
	if receipt = f.command(t, "memory.replace", id, &one, memory.ReplaceInput{MemoryID: id, Values: values}); receipt.Stage != "applied" {
		t.Fatalf("foreign current correction %+v", receipt)
	}
	if err := f.query(t, "memory.query", f.owner, query, &matches); err != nil || len(matches.Items) != 1 || matches.Items[0].MemoryRef.Revision != 2 || matches.Items[0].ContentRef != second {
		t.Fatalf("foreign query did not observe actual correction %+v %v", matches, err)
	}
	source.failCurrent.Store(true)
	if err := f.query(t, "memory.list", f.owner, memory.ListMemoryInput{Purpose: "memory.read", Limit: 20}, &records); err != nil || len(records.Items) != 0 || !records.Partial || !slices.Contains(records.Gaps, "permission_authority_unavailable") {
		t.Fatalf("unavailable source yielded invented exhaustive list %+v %v", records, err)
	}
	source.failCurrent.Store(false)
	source.command(t, "content.close", second.ContentID, &one, memory.CloseInput{ContentRef: second, Reason: "原纠正事实当前撤回"})
	if err := f.query(t, "memory.query", f.owner, query, &matches); err != nil || len(matches.Items) != 0 || matches.Partial {
		t.Fatalf("withdrawn foreign current fact remained a query result %+v %v", matches, err)
	}
	var record memory.MemoryRecord
	if err := f.query(t, "memory.cleanup.get", id, memory.ReadMemoryInput{MemoryID: id, Revision: 1}, &record); err != nil || record.Revision != 3 || record.State != "quarantined" || record.Values.ContentRef != second {
		t.Fatalf("foreign cleanup metadata lost current original revision %+v %v", record, err)
	}
	if err := f.query(t, "memory.view.pull", viewID, memory.PullViewInput{ViewID: viewID, Cursor: page.Cursor, Limit: 20}, &page); !api.IsCode(err, "snapshot_required") {
		t.Fatalf("old foreign view reused permit after actual source withdrawal: %v", err)
	}
	var index memory.IndexStatus
	if err := f.query(t, "memory.index.inspect", f.owner, struct{}{}, &index); err != nil || index.ChangeHead < 3 || index.ContiguousWatermark != index.ChangeHead {
		t.Fatalf("current foreign correction/withdrawal watermark %+v %v", index, err)
	}
}

func TestIndependentMemoryForeignHostOriginalRegistrationReadAndClose(t *testing.T) {
	source := newNativeSource(t)
	f := newFixture(t, "memory", source.configure(t))
	body := []byte(strings.Repeat("真实原来源字节\n", 8000))
	ref := source.publish(t, body)
	p := f.policy(t)
	scope := f.upload(t, p, []byte("foreign-original-scope"), "text/plain")
	var reference memory.ForeignReference
	for _, purpose := range []string{"memory.save", "memory.read", "memory.query", "memory.sync", "read"} {
		reference = foreignReference(f, ref, purpose)
		out, err := f.foreignCLI(t, "prepare-foreign-use", reference, "")
		if err != nil {
			t.Fatalf("explicit public Node host prepare %s %v %s", purpose, err, out)
		}
		var use memory.ForeignUse
		if err = api.Decode(out, &use); err != nil || use.Reference != reference || use.Proof.ContentRef != ref || use.Proof.SourceDatabaseID != source.scope.DatabaseID || use.Proof.Mode != "use" {
			t.Fatalf("host changed original source %v %s", err, out)
		}
	}
	id := api.NewID("memory")
	r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: memory.MemoryValues{Type: "fact", ContentRef: ref, ScopeRef: scope, PolicyRef: p, ObservedAt: api.Time(time.Now()), Sources: []api.SourceEvidence{{ContentRef: ref, SourceKind: "user_input"}}}})
	if r.Stage != "applied" {
		t.Fatalf("independent foreign memory admission %+v", r)
	}
	var record memory.MemoryRecord
	if err := f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &record); err != nil || record.Values.ContentRef != ref {
		t.Fatalf("original foreign memory read %+v %v", record, err)
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
		t.Fatalf("native mirror did not preserve full source bytes: %d %v", res.StatusCode, err)
	}
	one := uint64(1)
	source.command(t, "content.close", ref.ContentID, &one, memory.CloseInput{ContentRef: ref, Reason: "原来源关闭"})
	if err := f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &record); !api.IsCode(err, "forbidden") && !api.IsCode(err, "gone") {
		t.Fatalf("source close reused cached byte permit: %v", err)
	}
	before := source.gets.Load()
	out, err := f.foreignCLI(t, "control-foreign-copy", nil, reference.CopyID)
	if err != nil {
		t.Fatalf("original control after close %v %s", err, out)
	}
	var proof memory.ForeignProof
	if err := api.Decode(out, &proof); err != nil || proof.Mode != "control" || proof.ContentRef != ref || proof.UseState == "allowed" || source.gets.Load() != before {
		t.Fatalf("control did not preserve stopped original %v %s", err, out)
	}
	out, err = f.foreignCLI(t, "stop-foreign-copy", nil, reference.CopyID)
	if err != nil {
		t.Fatalf("original native holder cleanup %v %s", err, out)
	}
	if source.gets.Load() != before {
		t.Fatal("cleanup reread withdrawn source bytes")
	}
}
