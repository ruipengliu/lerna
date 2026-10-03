package alternate_test

import (
	"bufio"
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/ruipengliu/lerna/adapters/alternate/contracts"
	file "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

var buildOnce sync.Once
var buildError error
var versions string

type fixture struct {
	repo, dir, configFile, token, address string
	tenant, owner, subject                string
	http                                  *http.Client
	discovery                             harness.Discovery
	process                               *exec.Cmd
	log                                   lockedBuffer
	transport                             *harness.WSTransport
	client                                *harness.Client
	journal                               *harness.FileJournal
	keys                                  *platform.Keyring
	peerPolicy                            api.ComponentRef
}
type lockedBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.buffer.String() }

func newFixture(t *testing.T, system string, configure ...func(*fixture, map[string]any)) *fixture {
	t.Helper()
	repo, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	buildOnce.Do(func() {
		cmd := exec.Command("pnpm", "--filter", "@harness/alternate", "build")
		cmd.Dir = repo
		out, err := cmd.CombinedOutput()
		if err != nil {
			buildError = fmt.Errorf("alternate build: %w\n%s", err, out)
			return
		}
		out, err = exec.Command("node", "--input-type=module", "-e", `import { DatabaseSync } from 'node:sqlite'; const d = new DatabaseSync(':memory:'); process.stdout.write(JSON.stringify({node:process.versions.node,sqlite:d.prepare('select sqlite_version() v').get().v})); d.close();`).CombinedOutput()
		if err != nil {
			buildError = fmt.Errorf("actual native runtime versions: %w", err)
			return
		}
		versions = string(out)
	})
	if buildError != nil {
		t.Fatal(buildError)
	}
	f := &fixture{repo: repo, dir: t.TempDir(), tenant: api.NewID("tenant"), owner: api.NewID("owner"), subject: api.NewID("orchestrator")}
	var token [32]byte
	if _, err = rand.Read(token[:]); err != nil {
		t.Fatal(err)
	}
	f.token = hex.EncodeToString(token[:])
	cert, key, pool := certificates(t, f.dir)
	f.http = &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}, Timeout: 5 * time.Second}
	f.configFile = filepath.Join(f.dir, "config.json")
	config := map[string]any{"system": system, "tenant_id": f.tenant, "owner_id": f.owner, "database": filepath.Join(f.dir, "owner.sqlite"), "port": 0, "tls_cert_file": cert, "tls_key_file": key, "credentials": []any{map[string]any{"subject_id": f.subject, "generation": 1, "token_hash": api.Hash([]byte(f.token)), "roles": []string{"admin", "service", "memory_admin", "orchestrator", "executor"}, "expires_at": api.Time(time.Now().Add(time.Hour)), "active": true}}}
	for _, apply := range configure {
		apply(f, config)
	}
	config["tenant_id"], config["owner_id"] = f.tenant, f.owner
	config["credentials"] = []any{map[string]any{"subject_id": f.subject, "generation": 1, "token_hash": api.Hash([]byte(f.token)), "roles": []string{"admin", "service", "memory_admin", "orchestrator", "executor"}, "expires_at": api.Time(time.Now().Add(time.Hour)), "active": true}}
	writeJSON(t, f.configFile, config)
	cmd := exec.Command("node", filepath.Join(repo, "adapters/alternate/ts/dist/main.mjs"), "migrate", "--config", f.configFile)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("explicit migration: %v\n%s", err, out)
	}
	t.Cleanup(func() {
		if f.transport != nil {
			_ = f.transport.Close()
		}
		f.stop(t)
	})
	f.start(t)
	f.connect(t)
	return f
}

func (f *fixture) start(t *testing.T) {
	t.Helper()
	f.process = exec.Command("node", filepath.Join(f.repo, "adapters/alternate/ts/dist/main.mjs"), "serve", "--config", f.configFile)
	stdout, err := f.process.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	f.process.Stderr = &f.log
	if err = f.process.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan string, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		if scanner.Scan() {
			ready <- scanner.Text()
		} else {
			ready <- ""
		}
		for scanner.Scan() {
		}
	}()
	select {
	case line := <-ready:
		var r struct {
			Ready   bool   `json:"ready"`
			Port    int    `json:"port"`
			System  string `json:"system"`
			OwnerID string `json:"owner_id"`
		}
		if err = api.Decode([]byte(line), &r); err != nil || !r.Ready {
			t.Fatalf("independent node readiness: %v %q\n%s", err, line, f.log.String())
		}
		f.address = fmt.Sprintf("https://127.0.0.1:%d", r.Port)
	case <-time.After(10 * time.Second):
		t.Fatal("alternate readiness timeout")
	}
}
func (f *fixture) stop(t *testing.T) {
	t.Helper()
	if f.process == nil {
		return
	}
	_ = f.process.Process.Signal(os.Interrupt)
	done := make(chan error, 1)
	go func() { done <- f.process.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("alternate exit: %v\n%s", err, f.log.String())
		}
	case <-time.After(5 * time.Second):
		_ = f.process.Process.Kill()
		<-done
		t.Error("alternate shutdown failed")
	}
	f.process = nil
}
func (f *fixture) connect(t *testing.T) {
	t.Helper()
	req, err := http.NewRequest("GET", f.address+"/api/discovery", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	res, err := f.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("actual authenticated discovery: %d", res.StatusCode)
	}
	if err = json.NewDecoder(res.Body).Decode(&f.discovery); err != nil {
		t.Fatal(err)
	}
	digest, err := api.DigestLimit(f.discovery.Methods, 1<<20)
	if err != nil || digest != f.discovery.MethodsDigest {
		t.Fatalf("manifest digest: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	f.transport, err = harness.DialWebSocketWithHTTP(ctx, "wss"+f.address[5:]+"/connect", f.token, f.discovery, false, f.http)
	if err != nil {
		t.Fatal(err)
	}
	journal, err := harness.OpenJournal(filepath.Join(f.dir, "original-journal"), f.discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = journal.Close() })
	f.journal = journal
	f.client, err = harness.NewClient(f.transport, journal, f.discovery)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("actual independent component evidence: %s", api.Raw(map[string]any{"runtime": json.RawMessage(versions), "tenant_id": f.tenant, "owner_id": f.owner, "subject_id": f.subject, "identity_scope": f.discovery.IdentityScope, "identity_revision": f.discovery.IdentityRevision, "schema_digest": f.discovery.SchemaDigest, "methods_digest": f.discovery.MethodsDigest, "method_count": len(f.discovery.Methods), "transport": "real TLS harness-wss/1", "database": "independent native SQLite WAL/FULL"}))
}
func writeJSON(t *testing.T, name string, value any) {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(name, b, 0600); err != nil {
		t.Fatal(err)
	}
}
func certificates(t *testing.T, dir string) (string, string, *x509.CertPool) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "alternate conformance"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true, IsCA: true, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	cert := filepath.Join(dir, "cert.pem")
	key := filepath.Join(dir, "key.pem")
	if err = os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600); err != nil {
		t.Fatal(err)
	}
	pk, err := x509.MarshalPKCS8PrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: pk}), 0600); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool.AddCert(parsed)
	return cert, key, pool
}

func TestIndependentMemoryAdmitsPolicyThroughActualGoToNodeTLSWSS(t *testing.T) {
	f := newFixture(t, "memory")
	values := memory.PolicyValues{Subjects: []string{f.subject}, Purposes: []string{"content.write", "read", "memory.save", "memory.read", "memory.query", "memory.sync"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(30 * time.Minute)), Continuous: true}
	digest, err := api.Digest(values)
	if err != nil {
		t.Fatal(err)
	}
	ref := api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, CommandID: api.NewID("command"), Method: "content.policy.install", TargetID: ref.ComponentID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.Policy{PolicyRef: ref, Values: values, Revision: 1, State: "active"})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	r, err := f.client.Send(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	if r.Stage != "applied" {
		t.Fatalf("independent Memory must save exact policy and original receipt: %+v", r)
	}
	lookup, err := f.transport.Call(ctx, "receipt_lookup", api.Raw(api.ReceiptLookup{LogicalServiceID: f.owner, CommandID: c.CommandID}))
	if err != nil {
		t.Fatal(err)
	}
	var original api.Receipt
	if err = api.Decode(lookup, &original); err != nil {
		t.Fatal(err)
	}
	if !api.Equal(r, original) {
		t.Fatal("original decision changed on actual transport lookup")
	}
}

func (f *fixture) command(t *testing.T, method, target string, expected *uint64, payload any) api.Receipt {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, CommandID: api.NewID("command"), Method: method, TargetID: target, ExpectedRevision: expected, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(payload)}
	r, err := f.client.Send(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *fixture) query(t *testing.T, method, target string, payload, out any) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, QueryID: api.NewID("query"), Method: method, TargetID: target, Payload: api.Raw(payload)}
	b, err := f.transport.Call(ctx, "query", api.Raw(q))
	if err != nil {
		return err
	}
	return api.Decode(b, out)
}
func (f *fixture) policy(t *testing.T) api.ComponentRef {
	t.Helper()
	v := memory.PolicyValues{Subjects: []string{f.subject}, Purposes: []string{"content.write", "read", "memory.save", "memory.read", "memory.query", "memory.sync", "brain.input", "brain.output", "execution.arguments", "execution.result", "control.proof"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(30 * time.Minute)), Continuous: true}
	d, e := api.Digest(v)
	if e != nil {
		t.Fatal(e)
	}
	ref := api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: d}
	r := f.command(t, "content.policy.install", ref.ComponentID, nil, memory.Policy{PolicyRef: ref, Values: v, Revision: 1, State: "active"})
	if r.Stage != "applied" {
		t.Fatalf("policy: %+v", r)
	}
	return ref
}
func (f *fixture) upload(t *testing.T, policy api.ComponentRef, body []byte, media string, sources ...api.ContentRef) api.ContentRef {
	return f.uploadUntil(t, policy, body, media, time.Now().Add(10*time.Minute), sources...)
}
func (f *fixture) uploadUntil(t *testing.T, policy api.ComponentRef, body []byte, media string, retention time.Time, sources ...api.ContentRef) api.ContentRef {
	t.Helper()
	if sources == nil {
		sources = []api.ContentRef{}
	}
	ref := api.ContentRef{TenantID: f.tenant, OwnerID: f.owner, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(body), MediaType: media, ByteLength: uint64(len(body))}
	transfer := api.NewID("transfer")
	until := api.Time(retention)
	deadlineAt := time.Now().Add(time.Minute)
	if retention.Before(deadlineAt) {
		deadlineAt = retention
	}
	deadline := api.Time(deadlineAt)
	r := f.command(t, "content.upload_reserve", ref.ContentID, nil, memory.ReserveInput{TransferID: transfer, ContentRef: ref, PolicyRef: policy, ProcessedSources: sources, RetentionUntil: until, TransferDeadline: deadline})
	if r.Stage != "applied" {
		t.Fatalf("reserve: %+v", r)
	}
	req, e := http.NewRequest("POST", f.address+"/api/transfers/"+transfer, bytes.NewReader(body))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	res, e := f.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	b, e := io.ReadAll(res.Body)
	res.Body.Close()
	if e != nil || res.StatusCode != 200 {
		t.Fatalf("real bytes upload: %d %v %s", res.StatusCode, e, b)
	}
	r = f.command(t, "content.put", ref.ContentID, nil, memory.PutInput{ContentRef: ref, TransferID: transfer, PolicyRef: policy, ProcessedSources: sources, DisclosedSources: []api.ContentRef{}, RetentionUntil: until})
	if r.Stage != "applied" {
		t.Fatalf("publish: %+v", r)
	}
	return ref
}
func TestIndependentMemoryActualBytesCorrectionViewsAndWithdrawal(t *testing.T) {
	f := newFixture(t, "memory")
	p := f.policy(t)
	scope := f.upload(t, p, []byte("精确同步范围"), "text/plain")
	source := f.upload(t, p, []byte("原用户事实"), "text/plain")
	first := f.upload(t, p, []byte("旧事实"), "text/plain", source)
	second := f.upload(t, p, []byte("新事实"), "text/plain", source)
	id := api.NewID("memory")
	v := memory.MemoryValues{Type: "fact", ContentRef: first, Sources: []api.SourceEvidence{{ContentRef: source, SourceKind: "user_input"}}, ScopeRef: scope, PolicyRef: p, ObservedAt: api.Time(time.Now())}
	r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: v})
	if r.Stage != "applied" {
		t.Fatalf("create: %+v", r)
	}
	var got memory.MemoryRecord
	if e := f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &got); e != nil || got.Values.ContentRef != first {
		t.Fatalf("actual read: %+v %v", got, e)
	}
	viewID := api.NewID("view")
	r = f.command(t, "memory.view.open", viewID, nil, memory.OpenViewInput{ViewID: viewID, ScopeRef: scope, Purposes: []string{"memory.sync"}, HolderRef: api.ObjectRef{TenantID: f.tenant, OwnerID: f.owner, ObjectID: f.subject, Revision: 1}, Location: "local", MaxCandidates: 20})
	if r.Stage != "applied" {
		t.Fatalf("view: %+v", r)
	}
	var opened memory.ViewOutput
	if e := api.Decode(r.Output, &opened); e != nil {
		t.Fatal(e)
	}
	var page memory.ViewPage
	if e := f.query(t, "memory.view.pull", viewID, memory.PullViewInput{ViewID: viewID, Cursor: opened.Cursor, Limit: 20}, &page); e != nil || len(page.Records) != 1 || page.Records[0].MemoryID != id {
		t.Fatalf("view actual snapshot: %+v %v", page, e)
	}
	r = f.command(t, "memory.view.ack", viewID, nil, memory.AckViewInput{ViewID: viewID, Cursor: page.Cursor, ReceiptRef: api.ObjectRef{TenantID: f.tenant, OwnerID: f.owner, ObjectID: api.NewID("receipt"), Revision: 1}})
	if r.Stage != "applied" {
		t.Fatalf("ack: %+v", r)
	}
	var rev uint64 = 1
	v.ContentRef = second
	r = f.command(t, "memory.replace", id, &rev, memory.ReplaceInput{MemoryID: id, Values: v})
	if r.Stage != "applied" {
		t.Fatalf("replace: %+v", r)
	}
	if e := f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id, Revision: 1}, &got); !api.IsCode(e, "gone") {
		t.Fatalf("superseded body must be unavailable: %v", e)
	}
	if e := f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &got); e != nil || got.Revision != 2 || got.Values.ContentRef != second {
		t.Fatalf("current corrected fact: %+v %v", got, e)
	}
	textRef := f.upload(t, p, []byte("新事实"), "text/plain")
	spec := memory.MemoryQuerySpec{TextRef: textRef, TypeFilter: []string{"fact"}, RankingProfileRef: memory.LiteralProfile()}
	queryRef := f.upload(t, p, api.Raw(spec), "application/json", textRef)
	var matches api.Page[memory.Match]
	qi := memory.QueryInput{QueryRef: queryRef, ScopeRef: scope, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 20, MaxReadBytes: 1 << 18, MaxPermissionChecks: 100, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 20}
	if e := f.query(t, "memory.query", f.owner, qi, &matches); e != nil || len(matches.Items) != 1 || matches.Items[0].MemoryRef.Revision != 2 {
		t.Fatalf("literal query current revision: %+v %v", matches, e)
	}
	rev = 1
	r = f.command(t, "content.close", source.ContentID, &rev, memory.CloseInput{ContentRef: source, Reason: "撤回原事实"})
	if r.Stage != "applied" {
		t.Fatalf("source close: %+v", r)
	}
	if e := f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &got); e == nil {
		t.Fatal("withdrawn source remained readable")
	}
	var content memory.GetContentOutput
	if e := f.query(t, "content.get", source.ContentID, memory.GetContentInput{ContentRef: source, Mode: "bytes", Purpose: "read", Location: "local"}, &content); !api.IsCode(e, "gone") {
		t.Fatalf("original source body still accessible: %v %q", e, content.BytesBase64)
	}
	if e := f.query(t, "memory.cleanup.get", id, memory.ReadMemoryInput{MemoryID: id}, &got); e != nil {
		t.Fatalf("current management cleanup facts: %v", e)
	}
	if got.State == "active" {
		t.Fatal("withdrawn source did not close derived memory gate")
	}
}

func (f *fixture) admin(t *testing.T, namespace string, value any) {
	t.Helper()
	path := filepath.Join(f.dir, api.NewID("record")+".json")
	writeJSON(t, path, value)
	cmd := exec.Command("node", filepath.Join(f.repo, "adapters/alternate/ts/dist/main.mjs"), "admin", "--config", f.configFile, "--namespace", namespace, "--record", path)
	if out, e := cmd.CombinedOutput(); e != nil {
		t.Fatalf("explicit authority management: %v %s", e, out)
	}
}
func peerConfig(t *testing.T, target *fixture, p api.ComponentRef, keys *platform.Keyring) func(*fixture, map[string]any) {
	return func(f *fixture, c map[string]any) {
		f.tenant, f.subject = target.tenant, target.subject
		f.keys, f.peerPolicy = keys, p
		tokenFile := filepath.Join(f.dir, "content-token")
		if e := os.WriteFile(tokenFile, []byte(target.token), 0600); e != nil {
			t.Fatal(e)
		}
		c["memory"] = map[string]any{"origin": target.address, "owner_id": target.owner, "token_file": tokenFile, "ca_file": filepath.Join(target.dir, "cert.pem"), "policy_ref": p, "location": "local"}
		registered := []any{}
		for kid, k := range keys.Keys {
			registered = append(registered, map[string]any{"kid": kid, "tenant_id": k.TenantID, "issuer": k.Issuer, "purposes": k.Purposes, "jwk": json.RawMessage(platform.PublicJWK(k.Public))})
		}
		c["keys"] = registered
	}
}
func brainCase(t *testing.T, fault map[string]any, goalBytes []byte) (*fixture, *fixture, brain.DecideInput, api.ContentRef, governance.UseReceipt) {
	m := newFixture(t, "memory")
	policy := m.policy(t)
	keys, e := platform.NewDevelopmentKey(m.tenant, api.NewID("authority"), []string{"grant_use", "control"})
	if e != nil {
		t.Fatal(e)
	}
	manifest, e := contracts.Build()
	if e != nil {
		t.Fatal(e)
	}
	b := newFixture(t, "brain", func(f *fixture, c map[string]any) {
		peerConfig(t, m, policy, keys)(f, c)
		c["model_profile_ref"] = manifest.BrainProfile
		c["answer_schema_ref"] = manifest.AnswerSchema
		if fault != nil {
			c["fault"] = fault
		}
	})
	goal := m.upload(t, policy, goalBytes, "application/json")
	empty := m.upload(t, policy, []byte("准确选择报告"), "text/plain")
	taskRef := api.ObjectRef{TenantID: m.tenant, OwnerID: m.subject, ObjectID: api.NewID("task"), Revision: 1}
	requirements := []api.Requirement{}
	rd, e := api.Digest(requirements)
	if e != nil {
		t.Fatal(e)
	}
	component := api.ComponentRef{ComponentID: api.NewID("component"), Version: "1", Digest: api.Hash([]byte("registered empty component"))}
	s := api.Snapshot{SnapshotID: api.NewID("snapshot"), Revision: 1, TaskRef: taskRef, GoalRevision: 1, ControlRevision: 1, GoalRef: goal, Requirements: requirements, RequirementsDigest: rd, RequirementsState: "draft", Purpose: "answer", FactRefs: []api.ObjectRef{}, UnresolvedCollections: []api.CollectionSummary{}, PolicyRef: policy, InstallLockRef: component, ModelProfileRef: manifest.BrainProfile, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, MaterialRefs: []api.ContentRef{}, SelectionReportRef: empty, ProcessedSources: []api.ContentRef{goal, empty}, ReservedOutputTokens: 65536, SafetyMarginTokens: 1, CountMode: "upper_bound", TokenizerRef: component}
	var snapshotJSON map[string]any
	if e = json.Unmarshal(api.Raw(s), &snapshotJSON); e != nil {
		t.Fatal(e)
	}
	encoded, e := api.Canonical(api.Raw(map[string]any{"snapshot": snapshotJSON, "goal_bytes": base64.StdEncoding.EncodeToString(goalBytes)}))
	if e != nil {
		t.Fatal(e)
	}
	s.InputTokens = uint64(len(encoded))
	s.EncodedDigest = api.Hash(encoded)
	snapshot := m.upload(t, policy, api.Raw(s), "application/vnd.harness.snapshot+json", goal, empty)
	decisionID, useID := api.NewID("decision"), api.NewID("use")
	issuer := keys.Keys["development-es256"].Issuer
	useRef := api.ObjectRef{TenantID: m.tenant, OwnerID: issuer, ObjectID: useID, Revision: 1}
	in := brain.DecideInput{DecisionID: decisionID, TaskRef: taskRef, SnapshotRef: snapshot, SnapshotRevision: 1, ModelProfileRef: manifest.BrainProfile, UseRefs: []api.ObjectRef{useRef}, Limits: []api.Amount{{Unit: "USD", Value: "0"}}, Deadline: api.Time(time.Now().Add(time.Minute))}
	digest, e := api.Digest(in)
	if e != nil {
		t.Fatal(e)
	}
	u := governance.UseReceipt{UseID: useID, SubjectRef: api.ObjectRef{TenantID: m.tenant, OwnerID: issuer, ObjectID: b.subject, Revision: 1}, TargetRef: api.ObjectRef{TenantID: m.tenant, OwnerID: b.owner, ObjectID: decisionID, Revision: 1}, TargetKind: "decision", IntentHash: digest, RequestDigest: api.Hash([]byte("original independent host intent")), GrantRefs: []api.ObjectRef{{TenantID: m.tenant, OwnerID: issuer, ObjectID: api.NewID("grant"), Revision: 1}}, Decision: "allowed", Reserved: []api.Amount{{Unit: "USD", Value: "0"}}, CostBound: []api.Amount{{Unit: "USD", Value: "0"}}, Recipient: b.owner, Location: "local", Purposes: []string{"decision"}, IssuedAt: api.Time(time.Now()), StartBefore: in.Deadline}
	proofDigest, e := governance.UseReceiptDigest(u)
	if e != nil {
		t.Fatal(e)
	}
	u.Proof, e = keys.Sign("development-es256", platform.ProofClaims{TenantID: m.tenant, Issuer: issuer, Audience: b.owner, Purpose: "grant_use", ObjectRef: useRef, Digest: proofDigest, WindowID: useID, IssuedAt: u.IssuedAt, StartBefore: u.StartBefore})
	if e != nil {
		t.Fatal(e)
	}
	b.admin(t, "uses", u)
	return b, m, in, goal, u
}

func TestIndependentBrainActualForeignContentAndOriginalDecision(t *testing.T) {
	b, m, in, _, _ := brainCase(t, nil, []byte(`{"kind":"answer","body":"准确跨语言答案"}`))
	decisionID := in.DecisionID
	var e error
	r := b.command(t, "brain.decide", decisionID, nil, in)
	if r.Stage != "accepted" {
		t.Fatalf("independent decision admission: %+v", r)
	}
	var view brain.View
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if e = b.query(t, "brain.get", decisionID, brain.GetInput{DecisionID: decisionID}, &view); e != nil {
			t.Fatal(e)
		}
		if view.Decision.Status == "completed" {
			break
		}
		if view.Decision.Status == "failed" {
			t.Fatalf("native decision failed: %+v", view)
		}
		time.Sleep(100 * time.Millisecond)
	}
	if view.Decision.Status != "completed" || view.Decision.PhysicalRequestCount != 0 || !view.Decision.UsageFinal || view.Decision.ProposalRef == nil {
		t.Fatalf("real independent advice publication: %+v", view)
	}
	if view.Decision.OwnerID != b.owner || view.Decision.ProposalRef.OwnerID != m.owner || b.owner == m.owner {
		t.Fatal("original component/content owners were rewritten")
	}
	var output memory.GetContentOutput
	if e = m.query(t, "content.get", view.Decision.ProposalRef.ContentID, memory.GetContentInput{ContentRef: *view.Decision.ProposalRef, Mode: "bytes", Purpose: "brain.output", Location: "local"}, &output); e != nil {
		t.Fatal(e)
	}
	body, e := base64.StdEncoding.DecodeString(output.BytesBase64)
	if e != nil {
		t.Fatal(e)
	}
	var proposal brain.Proposal
	if e = api.Decode(body, &proposal); e != nil {
		t.Fatal(e)
	}
	if proposal.Kind != "complete" || len(proposal.ArtifactRefs) != 1 {
		t.Fatalf("truthful answer advice: %+v", proposal)
	}
	if e = m.query(t, "content.get", proposal.ArtifactRefs[0].ContentID, memory.GetContentInput{ContentRef: proposal.ArtifactRefs[0], Mode: "bytes", Purpose: "brain.output", Location: "local"}, &output); e != nil {
		t.Fatal(e)
	}
	body, e = base64.StdEncoding.DecodeString(output.BytesBase64)
	if e != nil || string(body) != "准确跨语言答案" {
		t.Fatalf("original answer bytes: %q %v", body, e)
	}
}

func executorCase(t *testing.T, fault map[string]any, path string) (*fixture, *fixture, execution.InvokeInput, string, governance.UseReceipt) {
	m := newFixture(t, "memory")
	policy := m.policy(t)
	authority := api.NewID("authority")
	keys, e := platform.NewDevelopmentKey(m.tenant, authority, []string{"grant_use"})
	if e != nil {
		t.Fatal(e)
	}
	orchestratorKey, e := platform.NewDevelopmentKey(m.tenant, m.subject, []string{"control"})
	if e != nil {
		t.Fatal(e)
	}
	keys.Keys["orchestrator-es256"] = orchestratorKey.Keys["development-es256"]
	var managed string
	var binding api.ObjectRef
	lock := api.ComponentRef{ComponentID: api.NewID("install"), Version: "1", Digest: api.Hash([]byte("node24.19/sqlite3.53/alternate-read-only-v1"))}
	x := newFixture(t, "executor", func(f *fixture, c map[string]any) {
		peerConfig(t, m, policy, keys)(f, c)
		managed = filepath.Join(f.dir, "managed")
		if e := os.Mkdir(managed, 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(filepath.Join(managed, "truth.txt"), []byte("真正磁盘原文"), 0600); e != nil {
			t.Fatal(e)
		}
		binding = api.ObjectRef{TenantID: m.tenant, OwnerID: f.owner, ObjectID: api.NewID("binding"), Revision: 1}
		c["managed_root"] = managed
		c["binding_ref"] = binding
		c["install_lock_ref"] = lock
		if fault != nil {
			c["fault"] = fault
		}
	})
	operationID, useID := api.NewID("operation"), api.NewID("use")
	task := api.ObjectRef{TenantID: m.tenant, OwnerID: m.subject, ObjectID: api.NewID("task"), Revision: 1}
	deadline := api.Time(time.Now().Add(time.Minute))
	args := m.upload(t, policy, api.Raw(file.FileReadArguments{Path: path}), "application/json")
	intent := execution.ExecutionIntent{OperationID: operationID, TaskRef: task, GoalRevision: 1, ControlRevision: 1, AdmissionSourceKind: "decision", AdmissionSourceRef: api.ObjectRef{TenantID: m.tenant, OwnerID: m.subject, ObjectID: api.NewID("decision"), Revision: 1}, SourcePosition: "0", AdmissionPurpose: "execute", CapabilityRef: file.FileReadCapability().Ref, BindingRef: binding, InstallLockRef: lock, ArgumentsRef: args, ResourceRefs: []api.ObjectRef{}, RequirementRefs: []api.RequirementRef{}, CostBound: []api.Amount{{Unit: "USD", Value: "0"}}, ExecutorID: x.owner, Deadline: deadline, TaskDeadline: deadline, LogicalStepKey: "exact-read", ProcessedSourceRefs: []api.ContentRef{args}, DisclosedSourceRefs: []api.ContentRef{}}
	intentDigest, e := api.Digest(intent)
	if e != nil {
		t.Fatal(e)
	}
	intentRef := m.upload(t, policy, api.Raw(intent), "application/vnd.harness.execution-intent+json", args)
	useRef := api.ObjectRef{TenantID: m.tenant, OwnerID: authority, ObjectID: useID, Revision: 1}
	u := governance.UseReceipt{UseID: useID, SubjectRef: api.ObjectRef{TenantID: m.tenant, OwnerID: authority, ObjectID: x.subject, Revision: 1}, TargetRef: api.ObjectRef{TenantID: m.tenant, OwnerID: x.owner, ObjectID: operationID, Revision: 1}, TargetKind: "operation", IntentHash: intentDigest, RequestDigest: api.Hash([]byte("original host file admission")), GrantRefs: []api.ObjectRef{{TenantID: m.tenant, OwnerID: authority, ObjectID: api.NewID("grant"), Revision: 1}}, Decision: "allowed", Reserved: []api.Amount{{Unit: "USD", Value: "0"}}, CostBound: []api.Amount{{Unit: "USD", Value: "0"}}, Recipient: x.owner, Location: "local", Purposes: []string{"execute"}, IssuedAt: api.Time(time.Now()), StartBefore: deadline}
	proofDigest, e := governance.UseReceiptDigest(u)
	if e != nil {
		t.Fatal(e)
	}
	u.Proof, e = keys.Sign("development-es256", platform.ProofClaims{TenantID: m.tenant, Issuer: authority, Audience: x.owner, Purpose: "grant_use", ObjectRef: useRef, Digest: proofDigest, WindowID: useID, IssuedAt: u.IssuedAt, StartBefore: deadline})
	if e != nil {
		t.Fatal(e)
	}
	x.admin(t, "uses", u)
	issuedAt := time.Now()
	window := api.ControlSnapshot{OrchestratorID: m.subject, TaskID: task.ObjectID, GoalRevision: 1, ControlRevision: 1, Status: "active", Control: "running", IssuedAt: api.Time(issuedAt), StartBefore: api.Time(issuedAt.Add(5 * time.Second)), WindowID: api.NewID("window")}
	wd, e := api.Digest(window)
	if e != nil {
		t.Fatal(e)
	}
	compact, e := keys.Sign("orchestrator-es256", platform.ProofClaims{TenantID: m.tenant, Issuer: m.subject, Audience: x.owner, Purpose: "control", ObjectRef: task, Digest: wd, ControlRevision: 1, WindowID: window.WindowID, IssuedAt: window.IssuedAt, StartBefore: window.StartBefore})
	if e != nil {
		t.Fatal(e)
	}
	window.ProofRef = m.upload(t, policy, []byte(compact), "application/jose")
	in := execution.InvokeInput{OperationID: operationID, TaskRef: task, GoalRevision: 1, ControlRevision: 1, CapabilityRef: file.FileReadCapability().Ref, BindingRef: binding, IntentRef: intentRef, IntentHash: intentDigest, UseRefs: []api.ObjectRef{useRef}, Deadline: deadline, ControlSnapshot: window, ReservationRef: api.ObjectRef{TenantID: m.tenant, OwnerID: m.subject, ObjectID: api.NewID("reservation"), Revision: 1}}
	return x, m, in, managed, u
}

func TestIndependentExecutorActualFileTruthAndForeignResult(t *testing.T) {
	x, m, in, managed, _ := executorCase(t, nil, "truth.txt")
	operationID := in.OperationID
	var e error
	r := x.command(t, "execution.invoke", operationID, nil, in)
	if r.Stage != "applied" {
		t.Fatalf("independent executor must durably admit operation: %+v", r)
	}
	var view execution.OperationView
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		if e = x.query(t, "execution.get", operationID, execution.OperationIDInput{OperationID: operationID}, &view); e != nil {
			t.Fatal(e)
		}
		if view.Operation.ExecutionState == "closed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if view.Operation.Effect != "applied" || view.Operation.ResultRef == nil || view.Attempts.Items == nil || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].AttemptNo != 1 || !view.NewAttemptsClosed || !view.ActuallyStopped {
		t.Fatalf("actual one-attempt result facts: %+v", view)
	}
	if view.Operation.OwnerID != x.owner || view.Operation.ResultRef.OwnerID != m.owner || x.owner == m.owner {
		t.Fatal("independent original owners were rewritten")
	}
	var got memory.GetContentOutput
	if e = m.query(t, "content.get", view.Operation.ResultRef.ContentID, memory.GetContentInput{ContentRef: *view.Operation.ResultRef, Mode: "bytes", Purpose: "execution.result", Location: "local"}, &got); e != nil {
		t.Fatal(e)
	}
	raw, e := base64.StdEncoding.DecodeString(got.BytesBase64)
	if e != nil {
		t.Fatal(e)
	}
	var result file.FileReadResult
	if e = api.Decode(raw, &result); e != nil {
		t.Fatal(e)
	}
	data, e := base64.StdEncoding.DecodeString(result.DataBase64)
	truth, e2 := os.ReadFile(filepath.Join(managed, "truth.txt"))
	if e != nil || e2 != nil || !bytes.Equal(data, truth) || result.Version != api.Hash(truth) {
		t.Fatalf("actual target truth mismatch: %q %v %v", data, e, e2)
	}
	var listed api.Page[execution.OperationOutput]
	if e = x.query(t, "execution.list", x.owner, api.ListInput{Limit: 20}, &listed); e != nil || len(listed.Items) != 1 || listed.Items[0].OperationRef.ObjectID != operationID {
		t.Fatalf("mandatory execution.list: %+v %v", listed, e)
	}
	var usage api.UsageSnapshot
	if e = x.query(t, "execution.usage.get", operationID, execution.OperationIDInput{OperationID: operationID}, &usage); e != nil || !usage.UsageFinal || !usage.SpendingClosed || len(usage.ProofRefs) != 1 {
		t.Fatalf("actual original usage closure: %+v %v", usage, e)
	}
}

func (f *fixture) restart(t *testing.T, kill bool, removeFault bool) {
	t.Helper()
	if f.transport != nil {
		_ = f.transport.Close()
		f.transport = nil
	}
	parsed, e := url.Parse(f.address)
	if e != nil {
		t.Fatal(e)
	}
	port, e := strconv.Atoi(parsed.Port())
	if e != nil {
		t.Fatal(e)
	}
	var config map[string]any
	b, e := os.ReadFile(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &config); e != nil {
		t.Fatal(e)
	}
	config["port"] = port
	if removeFault {
		delete(config, "fault")
	}
	writeJSON(t, f.configFile, config)
	if kill {
		if e = f.process.Process.Kill(); e != nil {
			t.Fatal(e)
		}
		e = f.process.Wait()
		if e == nil {
			t.Fatal("SIGKILL did not stop actual owner")
		}
		f.process = nil
	} else {
		f.stop(t)
	}
	f.start(t)
	f.connect(t)
}
func original(t *testing.T, f *fixture, id string) api.Receipt {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	b, e := f.transport.Call(ctx, "receipt_lookup", api.Raw(api.ReceiptLookup{LogicalServiceID: f.owner, CommandID: id}))
	if e != nil {
		t.Fatal(e)
	}
	var r api.Receipt
	if e = api.Decode(b, &r); e != nil {
		t.Fatal(e)
	}
	return r
}
func TestIndependentMemoryLostReceiptSIGKILLAndExactRecovery(t *testing.T) {
	f := newFixture(t, "memory", func(_ *fixture, c map[string]any) {
		c["fault"] = map[string]any{"drop_response_method": "memory.create"}
	})
	p := f.policy(t)
	body := f.upload(t, p, []byte("已提交的原事实"), "text/plain")
	scope := f.upload(t, p, []byte("原范围"), "text/plain")
	id := api.NewID("memory")
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, CommandID: api.NewID("command"), TargetID: id, Method: "memory.create", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CreateInput{MemoryID: id, Values: memory.MemoryValues{Type: "fact", ContentRef: body, ScopeRef: scope, PolicyRef: p, Sources: []api.SourceEvidence{}, ObservedAt: api.Time(time.Now())}})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, e := f.client.Send(ctx, c); e == nil {
		t.Fatal("injected committed reply loss was hidden")
	}
	pending, _, e := f.journal.Pending(ctx, 20)
	if e != nil || len(pending) != 1 || !api.Equal(pending[0].Command, c) {
		t.Fatalf("original durable client responsibility: %v %+v", e, pending)
	}
	f.restart(t, true, true)
	r := original(t, f, c.CommandID)
	if r.Stage != "applied" || r.RequestDigest != pending[0].Digest {
		t.Fatalf("committed original after SIGKILL: %+v", r)
	}
	r2, e := f.client.Send(ctx, c)
	if e != nil || !api.Equal(r, r2) {
		t.Fatalf("original exact retransmit changed decision: %v %+v", e, r2)
	}
	var record memory.MemoryRecord
	if e = f.query(t, "memory.read", id, memory.ReadMemoryInput{MemoryID: id}, &record); e != nil || record.Revision != 1 || record.Values.ContentRef != body {
		t.Fatalf("recovery duplicated or replaced original memory: %+v %v", record, e)
	}
	c.Payload = api.Raw(memory.CreateInput{MemoryID: id, Values: memory.MemoryValues{Type: "preference", ContentRef: body, ScopeRef: scope, PolicyRef: p, Sources: []api.SourceEvidence{}, ObservedAt: api.Time(time.Now())}})
	if _, e = f.transport.Call(ctx, "command", api.Raw(c)); !api.IsCode(e, "idempotency_conflict") {
		t.Fatalf("changed original wire command was accepted: %v", e)
	}
}
func TestIndependentBrainLostReceiptOriginalResumeAndWithdrawnSource(t *testing.T) {
	b, m, in, goal, _ := brainCase(t, map[string]any{"pause_jobs": true, "drop_response_method": "brain.decide"}, []byte(`{"kind":"answer","body":"原输出恢复"}`))
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: b.owner, CommandID: api.NewID("command"), Method: "brain.decide", TargetID: in.DecisionID, ExpiresAt: in.Deadline, Payload: api.Raw(in)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, e := b.client.Send(ctx, c); e == nil {
		t.Fatal("accepted original decision reply loss was hidden")
	}
	pending, _, e := b.journal.Pending(ctx, 20)
	if e != nil || len(pending) != 1 || !api.Equal(pending[0].Command, c) {
		t.Fatalf("original brain pending: %v %+v", e, pending)
	}
	b.restart(t, true, true)
	var view brain.View
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		if e = b.query(t, "brain.get", in.DecisionID, brain.GetInput{DecisionID: in.DecisionID}, &view); e != nil {
			t.Fatal(e)
		}
		if view.Decision.Status == "completed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	r := original(t, b, c.CommandID)
	if view.Decision.Status != "completed" || r.Stage != "applied" || r.RequestDigest != pending[0].Digest || view.Decision.PhysicalRequestCount != 0 {
		t.Fatalf("original decision recovery truth: %+v %+v", view, r)
	}
	var revision uint64 = 1
	r = m.command(t, "content.close", goal.ContentID, &revision, memory.CloseInput{ContentRef: goal, Reason: "撤回原目标"})
	if r.Stage != "applied" {
		t.Fatalf("source withdrawal: %+v", r)
	}
	if e = b.query(t, "brain.get", in.DecisionID, brain.GetInput{DecisionID: in.DecisionID}, &view); !api.IsCode(e, "gone") {
		t.Fatalf("cached proposal bypassed current original source gate: %v", e)
	}
}
func TestIndependentBrainKnownRevocationAndOriginalCancellation(t *testing.T) {
	for _, action := range []string{"revocation", "cancel"} {
		t.Run(action, func(t *testing.T) {
			b, _, in, _, u := brainCase(t, map[string]any{"pause_jobs": true}, []byte(`{"kind":"answer","body":"不应继续出版"}`))
			r := b.command(t, "brain.decide", in.DecisionID, nil, in)
			if r.Stage != "accepted" {
				t.Fatalf("original admission: %+v", r)
			}
			if action == "revocation" {
				b.admin(t, "denials", map[string]any{"ref": u.GrantRefs[0], "reason": "当前许可撤销"})
			} else {
				r = b.command(t, "brain.cancel", in.DecisionID, nil, brain.CancelInput{DecisionID: in.DecisionID, TaskRef: in.TaskRef, Reason: "明确原决策取消"})
				if r.Stage != "applied" {
					t.Fatalf("cancel: %+v", r)
				}
			}
			b.restart(t, true, true)
			var view brain.View
			until := time.Now().Add(3 * time.Second)
			for time.Now().Before(until) {
				if e := b.query(t, "brain.get", in.DecisionID, brain.GetInput{DecisionID: in.DecisionID}, &view); e != nil {
					t.Fatal(e)
				}
				if view.Decision.Status == "failed" || view.Decision.Status == "cancelled" {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			want := "failed"
			if action == "cancel" {
				want = "cancelled"
			}
			if view.Decision.Status != want || view.Decision.ProposalRef != nil || view.Decision.PhysicalRequestCount != 0 || !view.Decision.UsageFinal {
				t.Fatalf("current gate did not close original work: %+v", view)
			}
		})
	}
}
func TestIndependentExecutorLostReplyAndQueuedOriginalRecovery(t *testing.T) {
	x, m, in, _, _ := executorCase(t, map[string]any{"pause_jobs": true, "drop_response_method": "execution.invoke"}, "truth.txt")
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: x.owner, CommandID: api.NewID("command"), Method: "execution.invoke", TargetID: in.OperationID, ExpiresAt: in.Deadline, Payload: api.Raw(in)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, e := x.client.Send(ctx, c); e == nil {
		t.Fatal("admitted original operation reply loss was hidden")
	}
	pending, _, e := x.journal.Pending(ctx, 20)
	if e != nil || len(pending) != 1 || !api.Equal(pending[0].Command, c) {
		t.Fatalf("original operation pending: %v %+v", e, pending)
	}
	x.restart(t, true, true)
	receipt := original(t, x, c.CommandID)
	if receipt.Stage != "applied" || receipt.RequestDigest != pending[0].Digest {
		t.Fatalf("original operation admission changed: %+v", receipt)
	}
	var view execution.OperationView
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		if e = x.query(t, "execution.get", in.OperationID, execution.OperationIDInput{OperationID: in.OperationID}, &view); e != nil {
			t.Fatal(e)
		}
		if view.Operation.ExecutionState == "closed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if view.Operation.Effect != "applied" || view.Operation.ResultRef == nil || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].AttemptNo != 1 {
		t.Fatalf("original one-attempt recovery: %+v", view)
	}
	var result memory.GetContentOutput
	if e = m.query(t, "content.get", view.Operation.ResultRef.ContentID, memory.GetContentInput{ContentRef: *view.Operation.ResultRef, Mode: "bytes", Purpose: "execution.result", Location: "local"}, &result); e != nil {
		t.Fatal(e)
	}
	if got := original(t, x, c.CommandID); !api.Equal(got, receipt) {
		t.Fatal("worker completion rewrote original applied admission receipt")
	}
}
func TestIndependentExecutorSIGKILLAfterBarrierCannotReadAgain(t *testing.T) {
	x, _, in, root, _ := executorCase(t, map[string]any{"crash_after_start": true}, "truth.txt")
	r := x.command(t, "execution.invoke", in.OperationID, nil, in)
	if r.Stage != "applied" {
		t.Fatalf("original invoke: %+v", r)
	}
	done := make(chan error, 1)
	go func() { done <- x.process.Wait() }()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("actual owner was not killed")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("SIGKILL fault did not reach durable barrier")
	}
	x.process = nil
	if x.transport != nil {
		_ = x.transport.Close()
		x.transport = nil
	}
	if e := os.WriteFile(filepath.Join(root, "truth.txt"), []byte("重启后新正文不得被原尝试读取"), 0600); e != nil {
		t.Fatal(e)
	}
	parsed, e := url.Parse(x.address)
	if e != nil {
		t.Fatal(e)
	}
	port, e := strconv.Atoi(parsed.Port())
	if e != nil {
		t.Fatal(e)
	}
	var config map[string]any
	raw, e := os.ReadFile(x.configFile)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(raw, &config); e != nil {
		t.Fatal(e)
	}
	config["port"] = port
	delete(config, "fault")
	writeJSON(t, x.configFile, config)
	x.start(t)
	x.connect(t)
	var view execution.OperationView
	until := time.Now().Add(3 * time.Second)
	for time.Now().Before(until) {
		if e = x.query(t, "execution.get", in.OperationID, execution.OperationIDInput{OperationID: in.OperationID}, &view); e != nil {
			t.Fatal(e)
		}
		if view.Operation.ExecutionState == "closed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if view.Operation.Effect != "unknown" || view.Operation.ResultRef != nil || len(view.Attempts.Items) != 1 || !view.NewAttemptsClosed || !view.ActuallyStopped || view.Operation.MayApplyLater != false {
		t.Fatalf("abrupt original start must remain unknown without new read: %+v", view)
	}
	r = x.command(t, "execution.reconcile", in.OperationID, nil, execution.ReconcileInput{OperationID: in.OperationID})
	if r.Stage != "applied" {
		t.Fatalf("original reconcile: %+v", r)
	}
	if e = x.query(t, "execution.get", in.OperationID, execution.OperationIDInput{OperationID: in.OperationID}, &view); e != nil || view.Operation.ResultRef != nil || len(view.Attempts.Items) != 1 {
		t.Fatalf("reconcile issued fresh file read: %+v %v", view, e)
	}
}
func TestIndependentExecutorCurrentRevocationAndNoPathEscape(t *testing.T) {
	for _, kind := range []string{"revoked", "symlink", "hardlink", "outside", "fifo"} {
		t.Run(kind, func(t *testing.T) {
			path := "truth.txt"
			if kind == "outside" {
				path = "../escape.txt"
			}
			x, _, in, root, u := executorCase(t, map[string]any{"pause_jobs": true}, path)
			if kind == "fifo" {
				if e := os.Remove(filepath.Join(root, "truth.txt")); e != nil {
					t.Fatal(e)
				}
				if e := syscall.Mkfifo(filepath.Join(root, "truth.txt"), 0600); e != nil {
					t.Fatal(e)
				}
			}
			if kind == "symlink" || kind == "hardlink" {
				if e := os.Remove(filepath.Join(root, "truth.txt")); e != nil {
					t.Fatal(e)
				}
				outside := filepath.Join(x.dir, "outside.txt")
				if e := os.WriteFile(outside, []byte("不得越界披露"), 0600); e != nil {
					t.Fatal(e)
				}
				var e error
				if kind == "symlink" {
					e = os.Symlink(outside, filepath.Join(root, "truth.txt"))
				} else {
					e = os.Link(outside, filepath.Join(root, "truth.txt"))
				}
				if e != nil {
					t.Fatal(e)
				}
			}
			r := x.command(t, "execution.invoke", in.OperationID, nil, in)
			if r.Stage != "applied" {
				t.Fatalf("original bounded admission: %+v", r)
			}
			if kind == "revoked" {
				x.admin(t, "denials", map[string]any{"ref": u.GrantRefs[0], "reason": "实际当前许可撤销"})
			}
			x.restart(t, true, true)
			var view execution.OperationView
			until := time.Now().Add(3 * time.Second)
			for time.Now().Before(until) {
				if e := x.query(t, "execution.get", in.OperationID, execution.OperationIDInput{OperationID: in.OperationID}, &view); e != nil {
					t.Fatal(e)
				}
				if view.Operation.ExecutionState == "closed" {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if view.Operation.Effect != "not_started" || view.Operation.ResultRef != nil || len(view.Attempts.Items) != 0 || !view.NewAttemptsClosed || !view.ActuallyStopped {
				t.Fatalf("current authority/path gate allowed actual read: %+v", view)
			}
		})
	}
}

func TestIndependentBrainPlainOriginalBytesRequireClarification(t *testing.T) {
	b, m, in, goal, _ := brainCase(t, nil, []byte("自由文本原目标；并不是JSON模板。"))
	r := b.command(t, "brain.decide", in.DecisionID, nil, in)
	if r.Stage != "accepted" {
		t.Fatalf("original plain bytes: %+v", r)
	}
	var view brain.View
	until := time.Now().Add(10 * time.Second)
	for time.Now().Before(until) {
		if e := b.query(t, "brain.get", in.DecisionID, brain.GetInput{DecisionID: in.DecisionID}, &view); e != nil {
			t.Fatal(e)
		}
		if view.Decision.Status == "completed" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if view.Decision.ProposalRef == nil {
		t.Fatalf("plain original bytes panicked/guessed success: %+v", view)
	}
	var got memory.GetContentOutput
	if e := m.query(t, "content.get", view.Decision.ProposalRef.ContentID, memory.GetContentInput{ContentRef: *view.Decision.ProposalRef, Mode: "bytes", Purpose: "brain.output", Location: "local"}, &got); e != nil {
		t.Fatal(e)
	}
	raw, e := base64.StdEncoding.DecodeString(got.BytesBase64)
	if e != nil {
		t.Fatal(e)
	}
	var proposal brain.Proposal
	if e = api.Decode(raw, &proposal); e != nil {
		t.Fatal(e)
	}
	if proposal.Kind != "request_input" || proposal.Purpose != "clarify_goal" || proposal.QuestionRef == nil || len(proposal.ArtifactRefs) != 0 {
		t.Fatalf("plain input became fake completion: %+v", proposal)
	}
	if e = m.query(t, "content.get", goal.ContentID, memory.GetContentInput{ContentRef: goal, Mode: "bytes", Purpose: "read", Location: "local"}, &got); e != nil {
		t.Fatal(e)
	}
	raw, e = base64.StdEncoding.DecodeString(got.BytesBase64)
	if e != nil || string(raw) != "自由文本原目标；并不是JSON模板。" {
		t.Fatalf("original bytes/hash rewritten: %q %v", raw, e)
	}
}
func TestIndependentExecutorMandatoryPaginationAndUnknownMethods(t *testing.T) {
	x, _, in, _, _ := executorCase(t, map[string]any{"pause_jobs": true}, "truth.txt")
	ids := []string{}
	for i := 0; i < 3; i++ {
		id := api.NewID("operation")
		ids = append(ids, id)
		r := x.command(t, "execution.cancel", id, nil, execution.CancelInput{OperationID: id, TaskRef: in.TaskRef, OrchestratorID: x.subject, Reason: "原未启动身份永久取消"})
		if r.Stage != "applied" {
			t.Fatalf("cancel tombstone: %+v", r)
		}
	}
	var first, second, third api.Page[execution.OperationOutput]
	if e := x.query(t, "execution.list", x.owner, api.ListInput{Limit: 1}, &first); e != nil || len(first.Items) != 1 || first.NextCursor == "" || first.Partial {
		t.Fatalf("first closed page: %+v %v", first, e)
	}
	if e := x.query(t, "execution.list", x.owner, api.ListInput{Limit: 1, Cursor: first.NextCursor}, &second); e != nil || len(second.Items) != 1 || second.Items[0].OperationRef.ObjectID == first.Items[0].OperationRef.ObjectID {
		t.Fatalf("independent bound page: %+v %v", second, e)
	}
	if e := x.query(t, "execution.list", x.owner, api.ListInput{Limit: 1, Cursor: second.NextCursor}, &third); e != nil || !third.Exhausted || len(third.Items) != 1 {
		t.Fatalf("last original page: %+v %v", third, e)
	}
	newID := api.NewID("operation")
	if r := x.command(t, "execution.cancel", newID, nil, execution.CancelInput{OperationID: newID, TaskRef: in.TaskRef, OrchestratorID: x.subject, Reason: "新的独立取消身份"}); r.Stage != "applied" {
		t.Fatalf("new original tombstone: %+v", r)
	}
	var changed api.Page[execution.OperationOutput]
	if e := x.query(t, "execution.list", x.owner, api.ListInput{Limit: 20}, &changed); e != nil || len(changed.Items) != 4 || changed.CollectionRevision <= first.CollectionRevision {
		t.Fatalf("changed operation collection reused previous revision: old=%d new=%+v %v", first.CollectionRevision, changed, e)
	}
	var ignored any
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: x.owner, QueryID: api.NewID("query"), Method: "execution.private.reset", TargetID: x.owner, Payload: api.Raw(map[string]any{})}
	if _, e := x.transport.Call(ctx, "query", api.Raw(q)); !api.IsCode(e, "unsupported") {
		t.Fatalf("unsupported private method accepted: %v", e)
	}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: x.owner, CommandID: api.NewID("command"), Method: "execution.private.reset", TargetID: x.owner, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(map[string]any{})}
	unknown, e := x.transport.Call(ctx, "command", api.Raw(c))
	if e != nil {
		t.Fatal(e)
	}
	var rejected api.Receipt
	if e = api.Decode(unknown, &rejected); e != nil || rejected.Stage != "rejected" || rejected.Error == nil || rejected.Error.Code != "unsupported" {
		t.Fatalf("unsupported command crossed component boundary: %+v %v", rejected, e)
	}
	if saved := original(t, x, c.CommandID); saved.RequestDigest != rejected.RequestDigest || saved.Stage != "rejected" {
		t.Fatalf("original unsupported receipt was not retained: %+v", saved)
	}
	if e := x.query(t, "execution.list", x.owner, api.ListInput{Limit: 1, Cursor: first.NextCursor + "f"}, &ignored); !api.IsCode(e, "invalid_request") {
		t.Fatalf("forged cursor: %v", e)
	}
	in.OperationID = ids[0]
	r := x.command(t, "execution.invoke", in.OperationID, nil, in)
	if r.Stage != "rejected" || r.Error == nil || r.Error.Code != "invalid_state" {
		t.Fatalf("cancel-before-invoke tombstone was reset: %+v", r)
	}
}
func TestIndependentExecutorPauseIsCurrentSignedFactAndCannotReopenOriginal(t *testing.T) {
	x, m, in, _, _ := executorCase(t, map[string]any{"pause_jobs": true}, "truth.txt")
	r := x.command(t, "execution.invoke", in.OperationID, nil, in)
	if r.Stage != "applied" {
		t.Fatalf("original invoke: %+v", r)
	}
	// 新停止窗口必须由原 Orchestrator 真实签名；客户端不能凭布尔打开 gate。
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	fake := platform.Keyring{Keys: map[string]platform.RegisteredKey{"orchestrator-es256": {TenantID: m.tenant, Issuer: m.subject, Purposes: []string{"control"}, Public: &key.PublicKey, Private: key}}}
	w := in.ControlSnapshot
	w.ControlRevision++
	w.Control = "paused"
	w.WindowID = api.NewID("window")
	issuedAt := time.Now()
	w.IssuedAt = api.Time(issuedAt)
	w.StartBefore = api.Time(issuedAt.Add(5 * time.Second))
	w.ProofRef = api.ContentRef{}
	digest, e := api.Digest(w)
	if e != nil {
		t.Fatal(e)
	}
	compact, e := fake.Sign("orchestrator-es256", platform.ProofClaims{TenantID: m.tenant, Issuer: m.subject, Audience: x.owner, Purpose: "control", ObjectRef: in.TaskRef, Digest: digest, ControlRevision: w.ControlRevision, WindowID: w.WindowID, IssuedAt: w.IssuedAt, StartBefore: w.StartBefore})
	if e != nil {
		t.Fatal(e)
	}
	w.ProofRef = m.upload(t, x.peerPolicy, []byte(compact), "application/jose")
	r = x.command(t, "execution.control", in.TaskRef.ObjectID, nil, execution.ControlInput{TaskRef: in.TaskRef, Snapshot: w})
	if r.Stage != "rejected" || r.Error == nil || r.Error.Code != "forbidden" {
		t.Fatalf("unregistered signing key opened current gate: %+v", r)
	}
	var view execution.ControlView
	if e = x.query(t, "execution.control.get", in.TaskRef.ObjectID, execution.ControlGetInput{TaskID: in.TaskRef.ObjectID}, &view); e != nil || view.Gate.ControlRevision != 1 || view.Gate.Control != "running" {
		t.Fatalf("invalid source altered original control fact: %+v %v", view, e)
	}
	w.ProofRef = api.ContentRef{}
	compact, e = x.keys.Sign("orchestrator-es256", platform.ProofClaims{TenantID: m.tenant, Issuer: m.subject, Audience: x.owner, Purpose: "control", ObjectRef: in.TaskRef, Digest: digest, ControlRevision: w.ControlRevision, WindowID: w.WindowID, IssuedAt: w.IssuedAt, StartBefore: w.StartBefore})
	if e != nil {
		t.Fatal(e)
	}
	w.ProofRef = m.upload(t, x.peerPolicy, []byte(compact), "application/jose")
	r = x.command(t, "execution.control", in.TaskRef.ObjectID, nil, execution.ControlInput{TaskRef: in.TaskRef, Snapshot: w})
	if r.Stage != "applied" {
		t.Fatalf("original orchestrator signed pause not admitted: %+v", r)
	}
	x.restart(t, true, true)
	if e = x.query(t, "execution.control.get", in.TaskRef.ObjectID, execution.ControlGetInput{TaskID: in.TaskRef.ObjectID}, &view); e != nil || view.Gate.ControlRevision != 2 || view.Gate.Control != "paused" {
		t.Fatalf("actual signed current pause disappeared on SIGKILL: %+v %v", view, e)
	}
	var originalOperation execution.OperationView
	if e = x.query(t, "execution.get", in.OperationID, execution.OperationIDInput{OperationID: in.OperationID}, &originalOperation); e != nil || originalOperation.Operation.Effect != "not_started" || len(originalOperation.Attempts.Items) != 0 || !originalOperation.NewAttemptsClosed || !originalOperation.ActuallyStopped {
		t.Fatalf("recovery reopened originally paused work: %+v %v", originalOperation, e)
	}
}

func TestIndependentExecutorOriginalUsePurposeCannotAuthorizeRead(t *testing.T) {
	x, _, in, _, u := executorCase(t, map[string]any{"pause_jobs": true}, "truth.txt")
	u.UseID = api.NewID("use")
	u.Purposes = []string{"memory.read"}
	u.Proof = ""
	digest, e := governance.UseReceiptDigest(u)
	if e != nil {
		t.Fatal(e)
	}
	ref := in.UseRefs[0]
	ref.ObjectID = u.UseID
	u.Proof, e = x.keys.Sign("development-es256", platform.ProofClaims{TenantID: x.tenant, Issuer: ref.OwnerID, Audience: x.owner, Purpose: "grant_use", ObjectRef: ref, Digest: digest, WindowID: u.UseID, IssuedAt: u.IssuedAt, StartBefore: u.StartBefore})
	if e != nil {
		t.Fatal(e)
	}
	x.admin(t, "uses", u)
	in.UseRefs = []api.ObjectRef{ref}
	r := x.command(t, "execution.invoke", in.OperationID, nil, in)
	if r.Stage != "rejected" || r.Error == nil || r.Error.Code != "forbidden" {
		t.Fatalf("authentic receipt for another purpose authorized a read: %+v", r)
	}
}

func TestIndependentCookieLogoutClosesCurrentSocketPreservesOriginalContent(t *testing.T) {
	f := newFixture(t, "memory")
	policy := f.policy(t)
	ref := f.upload(t, policy, []byte("退出会话不撤回已发布原文"), "text/plain")
	req, e := http.NewRequest("POST", f.address+"/auth/session", bytes.NewReader(api.Raw(map[string]string{"token": f.token})))
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Origin", f.address)
	res, e := f.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	var session struct {
		Authenticated bool   `json:"authenticated"`
		CSRF          string `json:"csrf_token"`
	}
	e = json.NewDecoder(res.Body).Decode(&session)
	res.Body.Close()
	if e != nil || !session.Authenticated || len(res.Cookies()) != 1 {
		t.Fatalf("real private cookie login: status=%d %v", res.StatusCode, e)
	}
	cookie := res.Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode {
		t.Fatal("cookie session lost required transport attributes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, e := websocket.Dial(ctx, "wss"+f.address[5:]+"/connect", &websocket.DialOptions{HTTPClient: f.http, HTTPHeader: http.Header{"Cookie": []string{cookie.String()}, "Origin": []string{f.address}}, Subprotocols: []string{"harness-wss.v1"}})
	if e != nil {
		t.Fatal(e)
	}
	defer conn.CloseNow()
	_, ready, e := conn.Read(ctx)
	if e != nil {
		t.Fatal(e)
	}
	var readyFrame harness.WSReady
	if e = api.Decode(ready, &readyFrame); e != nil || readyFrame.IdentityScope != f.discovery.IdentityScope {
		t.Fatalf("cookie native ready scope: %v", e)
	}
	req, e = http.NewRequest("POST", f.address+"/auth/logout", nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Origin", f.address)
	req.Header.Set("X-CSRF-Token", session.CSRF)
	req.AddCookie(cookie)
	res, e = f.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("original browser session logout: %d", res.StatusCode)
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.owner, QueryID: api.NewID("query"), Method: "content.get", TargetID: ref.ContentID, Payload: api.Raw(memory.GetContentInput{ContentRef: ref, Mode: "bytes", Purpose: "read", Location: "local"})}
	if e = conn.Write(ctx, websocket.MessageText, api.Raw(map[string]any{"type": "request", "request_seq": 1, "kind": "query", "payload": q})); e == nil {
		if _, body, readErr := conn.Read(ctx); readErr == nil {
			t.Fatalf("logged-out cookie still received business data: %s", body)
		}
	}
	var original memory.GetContentOutput
	if e = f.query(t, "content.get", ref.ContentID, memory.GetContentInput{ContentRef: ref, Mode: "bytes", Purpose: "read", Location: "local"}, &original); e != nil || original.BytesBase64 != base64.StdEncoding.EncodeToString([]byte("退出会话不撤回已发布原文")) {
		t.Fatalf("logout changed independent publication responsibility: %+v %v", original, e)
	}
}

func TestIndependentCredentialRevocationSurvivesOwnerRestart(t *testing.T) {
	f := newFixture(t, "memory")
	policy := f.policy(t)
	ref := f.upload(t, policy, []byte("原发布责任保留"), "text/plain")
	var cfg struct {
		Credentials []map[string]any `json:"credentials"`
	}
	b, e := os.ReadFile(f.configFile)
	if e != nil {
		t.Fatal(e)
	}
	if e = json.Unmarshal(b, &cfg); e != nil || len(cfg.Credentials) != 1 {
		t.Fatalf("original private credential configuration: %v", e)
	}
	cfg.Credentials[0]["active"] = false
	f.admin(t, "credentials", cfg.Credentials[0])
	var ignored any
	if e := f.query(t, "content.get", ref.ContentID, memory.GetContentInput{ContentRef: ref, Mode: "bytes", Purpose: "read", Location: "local"}, &ignored); e == nil {
		t.Fatal("current revoked credential disclosed original bytes")
	}
	f.stop(t)
	f.start(t)
	req, e := http.NewRequest("GET", f.address+"/api/discovery", nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+f.token)
	res, e := f.http.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	res.Body.Close()
	if res.StatusCode != 403 {
		t.Fatalf("old seed configuration resurrected revoked credential: %d", res.StatusCode)
	}
}

func TestIndependentMemoryExpiredOriginalSourceCleanupResumesAfterRestart(t *testing.T) {
	f := newFixture(t, "memory")
	policy := f.policy(t)
	scope := f.upload(t, policy, []byte("原长期范围"), "text/plain")
	until := time.Now().Add(3 * time.Second)
	source := f.uploadUntil(t, policy, []byte("原短期来源"), "text/plain", until)
	copyID := api.NewID("copy")
	holder := api.ObjectRef{TenantID: f.tenant, OwnerID: f.owner, ObjectID: api.NewID("holder"), Revision: 1}
	intent := api.ObjectRef{TenantID: f.tenant, OwnerID: f.owner, ObjectID: api.NewID("reference"), Revision: 1}
	if r := f.command(t, "content.register_copy", source.ContentID, nil, memory.RegisterCopyInput{CopyID: copyID, ContentRef: source, HolderRef: holder, ReferenceIntentRef: intent, Purpose: "read", Location: "local", RetainUntil: api.Time(until)}); r.Stage != "applied" {
		t.Fatalf("original expiring copy holder: %+v", r)
	}
	id := api.NewID("memory")
	values := memory.MemoryValues{Type: "fact", ContentRef: source, Sources: []api.SourceEvidence{{ContentRef: source, SourceKind: "user_input"}}, ScopeRef: scope, PolicyRef: policy, ObservedAt: api.Time(time.Now())}
	r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values})
	if r.Stage != "applied" {
		t.Fatalf("original short-retention fact: %+v", r)
	}
	if e := f.transport.Close(); e != nil {
		t.Fatal(e)
	}
	f.transport = nil
	f.stop(t)
	if remaining := time.Until(until); remaining > 0 {
		time.Sleep(remaining + 10*time.Millisecond)
	}
	f.start(t)
	f.connect(t)
	var body memory.GetContentOutput
	if e := f.query(t, "content.get", source.ContentID, memory.GetContentInput{ContentRef: source, Mode: "bytes", Purpose: "read", Location: "local"}, &body); !api.IsCode(e, "gone") {
		t.Fatalf("restart renewed original retention: %v", e)
	}
	var cleanup memory.MemoryRecord
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if e := f.query(t, "memory.cleanup.get", id, memory.ReadMemoryInput{MemoryID: id}, &cleanup); e != nil {
			t.Fatal(e)
		}
		if cleanup.State == "quarantined" {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if cleanup.State != "quarantined" || cleanup.Revision != 2 || cleanup.Values.ContentRef != source {
		t.Fatalf("durable original expiry did not retire local Memory holder: %+v", cleanup)
	}
	if e := f.query(t, "content.get", source.ContentID, memory.GetContentInput{ContentRef: source, Mode: "control", CopyID: copyID}, &body); e != nil || body.UseState != "closing" || body.CleanupState != "pending" || body.BytesBase64 != "" {
		t.Fatalf("source expiry lost original external holder responsibility: %+v %v", body, e)
	}
	t.Logf("original expiry facts: %s", api.Raw(map[string]any{"source_ref": source, "memory_id": id, "memory_revision": cleanup.Revision, "holder_state": cleanup.State, "content_use_state": body.UseState, "content_cleanup_state": body.CleanupState, "original_retention_until": api.Time(until)}))
}

func TestIndependentContentOriginalCopyControlAndResidualRelease(t *testing.T) {
	f := newFixture(t, "memory")
	p := f.policy(t)
	source := f.upload(t, p, []byte("原来源准确正文"), "text/plain")
	consumer := api.NewID("owner")
	in := memory.RegisterCopyInput{CopyID: api.NewID("copy"), ContentRef: source, HolderRef: api.ObjectRef{TenantID: f.tenant, OwnerID: consumer, ObjectID: api.NewID("holder"), Revision: 1}, ReferenceIntentRef: api.ObjectRef{TenantID: f.tenant, OwnerID: consumer, ObjectID: api.NewID("reference"), Revision: 1}, Purpose: "read", Location: "local", RetainUntil: api.Time(time.Now().Add(time.Minute))}
	r := f.command(t, "content.register_copy", source.ContentID, nil, in)
	if r.Stage != "applied" {
		t.Fatalf("original source copy registration: %+v", r)
	}
	var got memory.GetContentOutput
	q := memory.GetContentInput{ContentRef: source, Mode: "control", CopyID: in.CopyID}
	if e := f.query(t, "content.get", source.ContentID, q, &got); e != nil || got.UseState != "allowed" || got.ControlRevision != 1 || got.BytesBase64 != "" {
		t.Fatalf("source holder current control: %+v %v", got, e)
	}
	altered := in
	altered.ReferenceIntentRef.ObjectID = api.NewID("reference")
	if r = f.command(t, "content.register_copy", source.ContentID, nil, altered); r.Stage != "rejected" || r.Error == nil || r.Error.Code != "idempotency_conflict" {
		t.Fatalf("original reference intent changed: %+v", r)
	}
	rev := uint64(1)
	if r = f.command(t, "content.close", source.ContentID, &rev, memory.CloseInput{ContentRef: source, Reason: "原来源撤回"}); r.Stage != "applied" {
		t.Fatalf("source close: %+v", r)
	}
	if e := f.query(t, "content.get", source.ContentID, q, &got); e != nil || got.UseState != "closing" || got.ControlRevision != 2 || got.CleanupState != "pending" || got.BytesBase64 != "" {
		t.Fatalf("closed source lost original holder cleanup responsibility: %+v %v", got, e)
	}
	report := memory.ReleaseCopyInput{CopyID: in.CopyID, ContentRef: source, ControlRevision: 2, UseStopped: true, CleanupState: "residual", EvidenceRefs: []api.ContentRef{}, ResidualReason: "原消费者保留法定元数据，未证明全部介质擦除"}
	if r = f.command(t, "content.release_copy", source.ContentID, nil, report); r.Stage != "applied" {
		t.Fatalf("holder current residual report: %+v", r)
	}
	f.restart(t, true, false)
	if e := f.query(t, "content.get", source.ContentID, q, &got); e != nil || got.UseState != "use_stopped" || got.CleanupState != "residual" {
		t.Fatalf("original copy responsibility disappeared after SIGKILL: %+v %v", got, e)
	}
	report.UseStopped = false
	if r = f.command(t, "content.release_copy", source.ContentID, nil, report); r.Stage != "rejected" || r.Error == nil || r.Error.Code != "invalid_state" {
		t.Fatalf("holder release reopened stopped original use: %+v", r)
	}
	t.Logf("original copy facts: %s", api.Raw(map[string]any{"registration": in, "current": got, "copy_release_command_id": r.CommandID}))
}
