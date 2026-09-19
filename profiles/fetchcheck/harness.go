package fetchcheck

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"lerna/adapters/authorization/josegrant"
	sqliteauth "lerna/adapters/authorization/sqlite"
	filecontent "lerna/adapters/content/file"
	contentpolicy "lerna/adapters/content/policy"
	sqlitecontentpolicy "lerna/adapters/content/sqlitepolicy"
	executionlocal "lerna/adapters/execution/local"
	fetchauth "lerna/adapters/research/auth"
	fetchcontent "lerna/adapters/research/content"
	acquisitionexecution "lerna/adapters/research/execution"
	"lerna/adapters/research/httpfetch"
	fetchoutput "lerna/adapters/research/output"
	sqlitefetch "lerna/adapters/research/sqlite"
	fetchtask "lerna/adapters/research/taskguard"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/execution"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/schema"
	"lerna/sdk"
	"lerna/tasks"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"time"
)

type clock struct{ offset atomic.Int64 }

func (c *clock) Now() (time.Time, error) { return time.Now().Add(time.Duration(c.offset.Load())), nil }
func (c *clock) advance(d time.Duration) { c.offset.Add(int64(d)) }

type contentService interface {
	Call(context.Context, artifacts.Binding, *wire.ContentRequest) (*wire.ContentResponse, error)
	Clean(context.Context) error
}

type harness struct {
	lifetime           *executionLifetime
	pageMaxBytes       uint32
	answerOutputTokens uint64
	answerFromSearch   bool
	network            networkConfig
	searchCredential   SearchCredential
	searchFormat       string
	inputSources       []*wire.ContentSource
	http               fetch.Fetcher
	taskDeadline       time.Duration
	store              *sqliteauth.Store
	client             *sdk.CapabilityClient
	root, token        string
	clock              *clock
	db                 *sqliteauth.Store
	auth               *authorization.Service
	grants             *authorization.GrantAuthority
	core               *tasks.Service
	work               *tasks.WorkPort
	content            contentService
	blobs              *filecontent.Store
	policy             *contentpolicy.Persistent
	policyStore        *sqlitecontentpolicy.Store
	access             *fetchoutput.Adapter
	target             *acquisitionexecution.PageDriver
	attempts           *sqlitefetch.Store
	evidence           *fetchcontent.Adapter
	evidenceConfig     fetchcontent.Config
	researchEvidence   *fetchcontent.Adapter
	urls               []string
	exec               *execution.Service
	binding            execution.Binding
	cap                execution.Capability
}

// The reference call waits for network acquisition, governed evidence writes,
// inspection and output publication. Its total bound must leave room beyond
// the independently bounded one-second network request and I/O phases.
func config() execution.Config {
	return execution.Config{MaxOperations: 32, MaxReports: 8, MaxChecks: 4, MaxOutbox: 64, MaxInput: 32768, MaxOutput: 8192, IOTimeout: time.Second, DriverTimeout: 5 * time.Second}
}
func limits() tasks.RunLimits {
	return tasks.RunLimits{Lease: 10 * time.Second, RenewEvery: time.Second, DecisionTimeout: 5 * time.Second, IOTimeout: time.Second, MaxAttempts: 3, MaxConcurrent: 1}
}
func controlLimits() tasks.ControlLimits {
	return tasks.ControlLimits{MaxOperations: 32, MaxObservations: 32, MaxChecks: 4, PollInterval: 100 * time.Millisecond, StopTimeout: time.Second, IOTimeout: time.Second}
}
func capability() execution.Capability { return capabilityWithPageLimit(1024) }
func capabilityWithPageLimit(maxBytes uint32) execution.Capability {
	return execution.Capability{Name: "web.fetch", Version: "1", Implementation: "http", ImplementationVersion: "1", Resource: "root", Purpose: "task", Location: "local", Exclusive: true, Synchronous: true,
		Input:  schema.Resource{Type: "fetch.input", ID: "urn:fetch:input", Version: "1", Document: []byte(fmt.Sprintf(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:fetch:input","type":"object","additionalProperties":false,"required":["url","max_bytes","max_requests","timeout_ms"],"properties":{"url":{"type":"string","maxLength":4096},"max_bytes":{"type":"integer","minimum":1,"maximum":%d},"max_requests":{"type":"integer","minimum":1,"maximum":2},"timeout_ms":{"type":"integer","minimum":1,"maximum":1000}}}`, maxBytes))},
		Output: schema.Resource{Type: "fetch.output", ID: "urn:fetch:output", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:fetch:output","type":"object","additionalProperties":false,"required":["status","reference"],"properties":{"status":{"enum":["acquired","invalid","denied","unavailable","timed_out","cancelled","too_large","unsupported","limit_exceeded","expired"]},"reference":{"type":"string","maxLength":256}}}`)}}
}

// Network permission is independent of exact URL and source authorization.
// Only the trusted host supplies this configuration, never search candidates.
type networkConfig struct {
	Networks          []netip.Prefix
	AllowLoopbackHTTP bool
}

func open(ctx context.Context, root, token string, urls []string) (*harness, error) {
	return openWithNetwork(ctx, root, token, urls, networkConfig{Networks: []netip.Prefix{netip.MustParsePrefix("127.0.0.0/8")}, AllowLoopbackHTTP: true})
}

func openWithNetwork(ctx context.Context, root, token string, urls []string, network networkConfig) (h *harness, err error) {
	return openWithAcquisition(ctx, root, token, urls, network, 1024)
}
func openWithAcquisition(ctx context.Context, root, token string, urls []string, network networkConfig, pageBytes uint32) (h *harness, err error) {
	if pageBytes < 1 || pageBytes > 1<<20 {
		return nil, fetch.Invalid
	}
	network.Networks = append([]netip.Prefix(nil), network.Networks...)
	urls = append([]string(nil), urls...)
	if err = bindTransportConfig(ctx, root, token == "", urls, network, pageBytes); err != nil {
		return nil, err
	}
	h = &harness{lifetime: newExecutionLifetime(), root: root, token: token, clock: &clock{}, cap: capabilityWithPageLimit(pageBytes), pageMaxBytes: pageBytes, urls: urls, network: network, taskDeadline: 5 * time.Minute}
	h.db, err = sqliteauth.Open(filepath.Join(root, "authority.db"))
	if err != nil {
		return nil, err
	}
	defer func(opened *harness) {
		if err != nil {
			opened.close()
		}
	}(h)
	h.store = h.db
	h.auth, err = authorization.New(h.store, h.clock, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		return nil, err
	}
	if token == "" {
		h.token, err = h.auth.Bootstrap(ctx, "local", "operator")
		if err != nil {
			return nil, err
		}
		scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "task.pause", "task.cancel", "task.resume", "task.append", "task.revise", "task.adjust", "catalog.search", "catalog.describe", "capability.invoke", "capability.read", "capability.reconcile", "resource.change", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete", "fetch.read", "fetch.process", "fetch.disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: h.now().Add(time.Hour).Unix()}
		for i, change := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "execution", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "local", Subject: "operator", Scope: scope, Mode: "continuous"}}}} {
			op, e := h.operation(ctx)
			if e != nil {
				return nil, e
			}
			change.ExpectedRevision = uint64(i)
			if _, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: change}); err != nil {
				return nil, err
			}
		}
		key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if e != nil {
			return nil, e
		}
		der, e := x509.MarshalPKCS8PrivateKey(key)
		if e != nil {
			return nil, e
		}
		if err = os.WriteFile(filepath.Join(root, "signer.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), 0600); err != nil {
			return nil, err
		}
	}
	raw, err := os.ReadFile(filepath.Join(root, "signer.pem"))
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	keyAny, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	key := keyAny.(*ecdsa.PrivateKey)
	crypto, err := josegrant.New("key1", key, map[string]*ecdsa.PublicKey{"key1": &key.PublicKey})
	if err != nil {
		return nil, err
	}
	h.grants, err = h.auth.SignedGrants(authorization.GrantConfig{Issuer: "local-authority", MaxTTL: time.Hour, IOTimeout: time.Second, MaxDepth: 4, MaxRecords: 64}, crypto)
	if err != nil {
		return nil, err
	}
	h.core, err = tasks.New(h.auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "owner", MaxTasks: 100, MaxPage: 10})
	if err != nil {
		return nil, err
	}
	h.work, err = h.core.BindWorker(tasks.WorkerBinding{Token: h.token, Subject: "operator", WorkerID: "api-worker", AllowEffectEvidence: true}, limits())
	if err != nil {
		return nil, err
	}
	h.blobs, err = filecontent.Open(filepath.Join(root, "content"))
	if err != nil {
		return nil, err
	}
	until := h.now().Add(10 * time.Minute).Unix()
	// Public reference-host policy is installed at startup, never by an answer
	// handoff that could overwrite a later revocation or narrow configuration.
	rules := []contentpolicy.Rule{{Kind: "task-goal", Key: "inline", Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: until}}
	sources := map[string]*wire.ContentSource{}
	resources := map[string]string{}
	for i := range urls {
		key := fixtureSourceKey(i)
		rules = append(rules, contentpolicy.Rule{Kind: "web", Key: key, Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: until})
		sources[urls[i]] = &wire.ContentSource{Kind: "web", Key: key, Revision: 1}
		resources[urls[i]] = "root"
	}
	h.policyStore, err = sqlitecontentpolicy.Open(filepath.Join(root, "source-policy.db"))
	if err != nil {
		return nil, err
	}
	if token == "" {
		h.policy, err = contentpolicy.NewPersistent(ctx, h.policyStore, rules)
	} else {
		h.policy, err = contentpolicy.OpenPersistent(ctx, h.policyStore)
	}
	if err != nil {
		return nil, err
	}
	// Recovery and later observation scopes must not slide the original
	// source retention forward. Current Content checks still enforce each
	// individual rule, including revocation after this configuration is read.
	state, err := h.policyStore.Load(ctx)
	if err != nil {
		return nil, err
	}
	for _, rule := range state.Rules {
		until = min(until, rule.RetainUntil)
	}
	h.content, err = artifacts.New(h.auth, h.blobs, h.policy, artifacts.Config{Inline: 64, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 64, MaxChunk: 65536, MaxFiles: 128, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		return nil, err
	}
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	h.access, err = fetchoutput.New(h.content, binding, h.clock)
	if err != nil {
		return nil, err
	}
	h.evidenceConfig = fetchcontent.Config{Clock: h.clock, Resource: "root", Purpose: "task", RetainUntil: until, Sources: sources}
	h.evidence, err = fetchcontent.New(h.content, binding, h.evidenceConfig)
	if err != nil {
		return nil, err
	}
	h.attempts, err = sqlitefetch.Open(filepath.Join(root, "fetch.db"))
	if err != nil {
		return nil, err
	}
	authority, e := fetchauth.New(h.auth, fetchauth.Scope{Token: h.token, Namespace: "local", Subject: "operator", Purpose: "task", Location: "local", Recipient: "local", Resources: resources})
	if e != nil {
		return nil, e
	}
	httpAdapter, e := httpfetch.New(httpfetch.Config{Authority: authority, URLs: urls, Networks: network.Networks, AllowLoopbackHTTP: network.AllowLoopbackHTTP})
	if e != nil {
		return nil, e
	}
	h.http = httpAdapter
	taskGuard, e := fetchtask.New(h.auth, h.work, h.core, h.token)
	if e != nil {
		return nil, e
	}
	h.target, err = acquisitionexecution.NewPage(httpAdapter, h.attempts, h.evidence, h.auth, acquisitionexecution.Config{Guard: taskGuard, Token: h.token, Namespace: "local", Subject: "operator", Capability: h.cap, MaxBytes: int64(pageBytes), MaxRequests: 2, TaskLimit: 2, Timeout: time.Second})
	if err != nil {
		return nil, err
	}
	h.binding = execution.Binding{Token: h.token, Namespace: "local", Subject: "operator", Audience: "execution-local", Presenter: "local-host", CertificateSHA256: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), Worker: "api-worker"}
	h.exec, err = execution.New(h.grants, h.work, h.access, h.target, h.binding, h.cap, config(), h.operation)
	if err == nil {
		h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
	}
	return h, err
}
func (h *harness) now() time.Time { n, _ := h.clock.Now(); return n }
func (h *harness) operation(ctx context.Context) (string, error) {
	return h.auth.NewOperation(ctx, h.token)
}
func (h *harness) close() { h.lifetime.close(h.closeStores) }
func (h *harness) closeStores() {
	if h.policyStore != nil {
		h.policyStore.Close()
	}
	if h.attempts != nil {
		h.attempts.Close()
	}
	if h.blobs != nil {
		h.blobs.Close()
	}
	if h.db != nil {
		h.db.Close()
	}
}
func (h *harness) destroy() {
	h.close()
	select {
	case <-h.lifetime.done:
		os.RemoveAll(h.root)
	default:
		go func() { <-h.lifetime.done; os.RemoveAll(h.root) }()
	}
}
func fresh(ctx context.Context, urls []string) (*harness, error) {
	root, e := os.MkdirTemp("", "execution-profile-")
	if e != nil {
		return nil, e
	}
	h, e := open(ctx, root, "", urls)
	if e != nil {
		os.RemoveAll(root)
	}
	return h, e
}
func (h *harness) put(ctx context.Context, data []byte) (string, error) {
	op, e := h.operation(ctx)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(data)
	sources := h.inputSources
	if sources == nil {
		sources = []*wire.ContentSource{{Kind: "web", Key: "start", Revision: 1}, {Kind: "web", Key: "final", Revision: 1}}
	}
	out, e := h.content.Call(ctx, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: data, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "task", Sources: sources, AcquiredAt: h.now().Unix(), MediaType: "application/json", Size: uint64(len(data)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: h.now().Add(5 * time.Minute).Unix()}})
	if e != nil {
		return "", e
	}
	h.access.RememberInput(out.Record)
	return answers.Reference(out.Record.Ref), nil
}
func (h *harness) request(ctx context.Context, sourceInput ...[]byte) (execution.Request, string, error) {
	if len(sourceInput) > 1 {
		return execution.Request{}, "", fetch.Invalid
	}
	body := []byte(`{}`)
	if len(sourceInput) == 1 {
		body = sourceInput[0]
	}
	input, e := h.put(ctx, body)
	if e != nil {
		return execution.Request{}, "", e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return execution.Request{}, "", e
	}
	t, e := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Acquire bounded evidence from the authorized HTTP source", InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: h.now().Add(h.taskDeadline).Unix()}})
	if e != nil {
		return execution.Request{}, "", e
	}
	controls, e := h.core.Controls(controlLimits())
	if e != nil {
		return execution.Request{}, "", e
	}
	s, e := h.core.Load(ctx, t.Ref)
	if e != nil {
		return execution.Request{}, "", e
	}
	for _, kind := range []string{"claim", "start"} {
		s, e = h.work.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(s)})
		if e != nil {
			return execution.Request{}, "", e
		}
	}
	if _, e = controls.PrepareDisposition(ctx, h.token, t.Ref); e != nil {
		return execution.Request{}, "", e
	}
	op, e = h.operation(ctx)
	if e != nil {
		return execution.Request{}, "", e
	}
	r := execution.Request{OperationID: op, Qualification: tasks.QualificationOf(s), Capability: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, DescriptorSHA256: h.cap.Digest(), InputRef: input, ResourceVersion: 1}
	material, e := h.issue(ctx, r)
	return r, material, e
}
func (h *harness) issue(ctx context.Context, r execution.Request) (string, error) {
	op, e := h.operation(ctx)
	if e != nil {
		return "", e
	}
	snap, e := h.db.Load(ctx)
	if e != nil {
		return "", e
	}
	spec := &wire.SignedGrantSpec{Subject: h.binding.Subject, Audience: h.binding.Audience, Presenter: h.binding.Presenter, CertificateSha256: h.binding.CertificateSHA256, Scope: &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"resource.change"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: h.now().Add(30 * time.Minute).Unix()}, NotBefore: h.now().Unix(), Units: 1, Mode: "single", OperationBinding: r.OperationID, SemanticSha256: r.Fingerprint()}
	out, e := h.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, Kind: "ISSUE", ExpectedRevision: snap.State.Revision, Spec: spec})
	if e != nil {
		return "", e
	}
	return out.Material, nil
}

func fixtureSourceKey(i int) string {
	if i == 0 {
		return "start"
	}
	if i == 1 {
		return "final"
	}
	return "source-" + strconv.Itoa(i)
}
