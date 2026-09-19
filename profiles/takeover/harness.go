package takeover

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
	executioncontent "lerna/adapters/execution/content"
	executionlocal "lerna/adapters/execution/local"
	"lerna/adapters/execution/simresource"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/schema"
	"lerna/sdk"
	"lerna/tasks"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() (time.Time, error) { c.mu.Lock(); defer c.mu.Unlock(); return c.now, nil }
func (c *clock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }

type harness struct {
	store       authorization.Store
	client      *sdk.CapabilityClient
	root, token string
	clock       *clock
	db          *sqliteauth.Store
	auth        *authorization.Service
	grants      *authorization.GrantAuthority
	core        *tasks.Service
	work        *tasks.WorkPort
	content     *artifacts.Service
	blobs       *filecontent.Store
	policy      *contentpolicy.Policy
	access      *executioncontent.Adapter
	target      *simresource.Driver
	exec        *execution.Service
	binding     execution.Binding
	cap         execution.Capability
}

func config() execution.Config {
	return execution.Config{MaxOperations: 32, MaxReports: 8, MaxChecks: 4, MaxOutbox: 64, MaxInput: 32768, MaxOutput: 8192, IOTimeout: time.Second, DriverTimeout: time.Second}
}
func limits() tasks.RunLimits {
	return tasks.RunLimits{Lease: 10 * time.Second, RenewEvery: time.Second, DecisionTimeout: 5 * time.Second, IOTimeout: time.Second, MaxAttempts: 3, MaxConcurrent: 1}
}
func controlLimits() tasks.ControlLimits {
	return tasks.ControlLimits{MaxOperations: 32, MaxObservations: 32, MaxChecks: 4, PollInterval: 100 * time.Millisecond, StopTimeout: time.Second, IOTimeout: time.Second}
}
func capability() execution.Capability {
	return execution.Capability{Name: "counter.add", Version: "1", Implementation: "sqlite-counter", ImplementationVersion: "1", Resource: "root", Purpose: "task", Location: "local", Exclusive: true, Synchronous: false, Async: &execution.AsyncPolicy{Target: "counter-jobs/main", Source: "job-driver", Correlation: true, Cancel: true, PollInterval: time.Second, RecoveryWindow: time.Minute, Lease: 3 * time.Second, MaxEvidence: 16},
		Input:  schema.Resource{Type: "counter.input", ID: "urn:counter:input", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:counter:input","type":"object","required":["delta"],"additionalProperties":false,"properties":{"delta":{"type":"integer","minimum":1,"maximum":10}}}`)},
		Output: schema.Resource{Type: "counter.output", ID: "urn:counter:output", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:counter:output","type":"object","required":["value","version"],"additionalProperties":false,"properties":{"value":{"type":"integer","minimum":0},"version":{"type":"integer","minimum":1}}}`)}}
}
func open(ctx context.Context, root, token string) (*harness, error) {
	return openEntry(ctx, root, token, "add")
}
func openEntry(ctx context.Context, root, token, entry string) (*harness, error) {
	return openTarget(ctx, root, token, entry, "main")
}
func openTarget(ctx context.Context, root, token, entry, key string) (h *harness, err error) {
	h = &harness{root: root, token: token, clock: &clock{now: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}, cap: capabilityAt(entry, key)}
	h.db, err = sqliteauth.Open(filepath.Join(root, "authority.db"))
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			h.close()
		}
	}()
	if token != "" {
		snapshot, e := h.db.Load(ctx)
		if e != nil {
			return nil, e
		}
		h.clock.now = time.Unix(0, snapshot.State.LastTime)
	}
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
		scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "task.pause", "task.cancel", "task.resume", "task.append", "task.revise", "task.adjust", "capability.invoke", "capability.read", "capability.reconcile", "capability.cancel", "resource.takeover", "resource.resume", "resource.control.read", "resource.reconcile", "resource.change", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: h.now().Add(time.Hour).Unix()}
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
	signingKey := keyAny.(*ecdsa.PrivateKey)
	crypto, err := josegrant.New("key1", signingKey, map[string]*ecdsa.PublicKey{"key1": &signingKey.PublicKey})
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
	h.policy, err = contentpolicy.New([]contentpolicy.Rule{{Kind: "input", Key: "public-counter", Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(time.Hour).Unix()}})
	if err != nil {
		return nil, err
	}
	h.content, err = artifacts.New(h.auth, h.blobs, h.policy, artifacts.Config{Inline: 64, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 64, MaxChunk: 65536, MaxFiles: 128, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		return nil, err
	}
	h.access = executioncontent.New(h.content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.clock)
	targetEntry := entry
	if entry == "sync" {
		targetEntry = "add"
	}
	h.target, err = simresource.Open(filepath.Join(root, "target-"+key+".db"), targetEntry, key, h.clock)
	if err != nil {
		return nil, err
	}
	h.binding = execution.Binding{Token: h.token, Namespace: "local", Subject: "operator", Audience: "execution-local", Presenter: "local-host", CertificateSHA256: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), Worker: "api-worker"}
	h.exec, err = execution.New(h.grants, h.work, h.access, h.target, h.binding, h.cap, config(), h.operation)
	if err == nil {
		h.exec, err = h.exec.WithResourceControl(scopeAt(key), h.target)
		if err == nil {
			h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, "local"), "local")
		}
	}
	if err == nil && (token == "" || key != "main") {
		_, err = h.exec.AdvanceResourceControl(ctx, scopeAt(key).Ref)
	}
	return h, err
}
func (h *harness) now() time.Time { n, _ := h.clock.Now(); return n }
func (h *harness) operation(ctx context.Context) (string, error) {
	return h.auth.NewOperation(ctx, h.token)
}
func (h *harness) close() {
	if h.target != nil {
		h.target.Close()
	}
	if h.blobs != nil {
		h.blobs.Close()
	}
	if h.db != nil {
		h.db.Close()
	}
}
func (h *harness) destroy() { h.close(); os.RemoveAll(h.root) }
func fresh(ctx context.Context) (*harness, error) {
	root, e := os.MkdirTemp("", "execution-profile-")
	if e != nil {
		return nil, e
	}
	h, e := open(ctx, root, "")
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
	out, e := h.content.Call(ctx, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: data, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "task", Sources: []*wire.ContentSource{{Kind: "input", Key: "public-counter", Revision: 1}}, AcquiredAt: h.now().Unix(), MediaType: "application/json", Size: uint64(len(data)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: h.now().Add(10 * time.Minute).Unix()}})
	if e != nil {
		return "", e
	}
	return answers.Reference(out.Record.Ref), nil
}
func (h *harness) request(ctx context.Context) (execution.Request, string, error) {
	input, e := h.put(ctx, []byte(`{"delta":3}`))
	if e != nil {
		return execution.Request{}, "", fmt.Errorf("request checkpoint: %w", e)
	}
	op, e := h.operation(ctx)
	if e != nil {
		return execution.Request{}, "", fmt.Errorf("request checkpoint: %w", e)
	}
	t, e := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Add three to the controlled counter", InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: h.now().Add(5 * time.Minute).Unix()}})
	if e != nil {
		return execution.Request{}, "", fmt.Errorf("request checkpoint: %w", e)
	}
	controls, e := h.core.Controls(controlLimits())
	if e != nil {
		return execution.Request{}, "", fmt.Errorf("request checkpoint: %w", e)
	}
	s, e := h.core.Load(ctx, t.Ref)
	if e != nil {
		return execution.Request{}, "", fmt.Errorf("request checkpoint: %w", e)
	}
	for _, kind := range []string{"claim", "start"} {
		s, e = h.work.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(s)})
		if e != nil {
			return execution.Request{}, "", fmt.Errorf("work %s: %w", kind, e)
		}
	}
	if _, e = controls.PrepareDisposition(ctx, h.token, t.Ref); e != nil {
		return execution.Request{}, "", fmt.Errorf("request checkpoint: %w", e)
	}
	op, e = h.operation(ctx)
	if e != nil {
		return execution.Request{}, "", fmt.Errorf("request checkpoint: %w", e)
	}
	r := execution.Request{OperationID: op, Qualification: tasks.QualificationOf(s), Capability: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, DescriptorSHA256: h.cap.Digest(), InputRef: input, ResourceVersion: 1, ControlVersion: 1}
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
	spec := &wire.SignedGrantSpec{Subject: h.binding.Subject, Audience: h.binding.Audience, Presenter: h.binding.Presenter, CertificateSha256: h.binding.CertificateSHA256, Scope: &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"resource.change"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: h.now().Add(10 * time.Minute).Unix()}, NotBefore: h.now().Unix(), Units: 1, Mode: "single", OperationBinding: r.OperationID, SemanticSha256: r.Fingerprint()}
	out, e := h.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, Kind: "ISSUE", ExpectedRevision: snap.State.Revision, Spec: spec})
	if e != nil {
		return "", e
	}
	return out.Material, nil
}
