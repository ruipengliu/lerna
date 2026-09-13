package catalogcheck

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
	"lerna/adapters/contentpolicy"
	"lerna/adapters/executioncontent"
	"lerna/adapters/executionlocal"
	"lerna/adapters/executionrouter"
	"lerna/adapters/filecontent"
	"lerna/adapters/josegrant"
	"lerna/adapters/simworkflow"
	"lerna/catalog"

	"lerna/adapters/sqliteauth"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/execution"
	wire "lerna/gen/harness/v1"

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
	namespace   string
	sources     *sourceRouter
	def         simworkflow.Definition
	entries     []catalog.Entry
	services    map[string]*execution.Service
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
	target      *simworkflow.Driver
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
func open(ctx context.Context, root, token string, def simworkflow.Definition) (*harness, error) {
	return openRuntime(ctx, root, token, def, true)
}
func openRuntime(ctx context.Context, root, token string, def simworkflow.Definition, initializeControl bool) (h *harness, err error) {
	h = &harness{root: root, token: token, clock: &clock{now: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}, namespace: def.Kind, def: def, entries: def.Entries(), services: map[string]*execution.Service{}}
	h.db, err = sqliteauth.Open(filepath.Join(root, "authority.db"))
	if err != nil {
		return nil, err
	}
	cleanup := h
	defer func() {
		if err != nil {
			cleanup.close()
		}
	}()
	h.auth, err = authorization.New(h.db, h.clock, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if err != nil {
		return nil, err
	}
	if token == "" {
		h.token, err = h.auth.Bootstrap(ctx, h.namespace, "operator")
		if err != nil {
			return nil, err
		}
		scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "task.pause", "task.cancel", "task.resume", "task.append", "task.revise", "task.adjust", "capability.cancel", "resource.reconcile", "resource.control.read", "resource.takeover", "resource.resume", "catalog.list", "catalog.search", "catalog.describe", "capability.invoke", "capability.read", "capability.reconcile", "resource.change", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: h.now().Add(time.Hour).Unix()}
		for i, change := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "execution", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "local", Subject: "operator", Scope: scope, Mode: "continuous"}}}} {
			op, e := h.operation(ctx)
			if e != nil {
				return nil, e
			}
			change.ExpectedRevision = uint64(i)
			if _, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: h.namespace, OperationID: op, Command: change}); err != nil {
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
	h.core, err = tasks.New(h.auth, tasks.Config{Namespace: h.namespace, Resource: "root", Owner: "owner", MaxTasks: 100, MaxPage: 10})
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
	h.sources = &sourceRouter{base: h.policy}
	h.content, err = artifacts.New(h.auth, h.blobs, h.sources, artifacts.Config{Inline: 64, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 64, MaxChunk: 65536, MaxFiles: 128, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		return nil, err
	}
	h.access = executioncontent.New(h.content, artifacts.Binding{Token: h.token, Namespace: h.namespace, Location: "local", Recipient: "local"}, h.clock)
	h.target, err = simworkflow.Open(filepath.Join(root, "target.db"), def)
	if err != nil {
		return nil, err
	}
	h.binding = execution.Binding{Token: h.token, Namespace: h.namespace, Subject: "operator", Audience: "execution-local", Presenter: "local-host", CertificateSHA256: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), Worker: "api-worker"}
	for _, entry := range h.entries {
		svc, e := execution.New(h.grants, h.work, h.access, h.target, h.binding, entry.Capability, config(), h.operation)
		if e != nil {
			return nil, e
		}
		scope := h.scope()
		svc, e = svc.WithResourceControl(scope, h.target)
		if e != nil {
			return nil, e
		}
		h.services[entry.Ref.Digest] = svc
	}
	h.selectEntry(h.entries[0])
	if initializeControl {
		if _, err = h.exec.AdvanceResourceControl(ctx, h.target.Resource()); err != nil {
			return nil, err
		}
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
func fresh(ctx context.Context, def simworkflow.Definition) (*harness, error) {
	root, e := os.MkdirTemp("", "execution-profile-")
	if e != nil {
		return nil, e
	}
	h, e := open(ctx, root, "", def)
	if e != nil {
		os.RemoveAll(root)
	}
	return h, e
}
func (h *harness) put(ctx context.Context, data []byte) (string, error) {
	return h.putLineage(ctx, data, answers.Lineage{})
}
func (h *harness) putLineage(ctx context.Context, data []byte, lineage answers.Lineage) (string, error) {
	sources := []*wire.ContentSource{{Kind: "input", Key: "public-counter", Revision: 1}}
	sources = append(sources, lineage.Sources...)
	if len(sources) > 16 {
		return "", fmt.Errorf("source capacity")
	}
	until := h.now().Add(10 * time.Minute).Unix()
	if lineage.RetainUntil > 0 && lineage.RetainUntil < until {
		until = lineage.RetainUntil
	}

	op, e := h.operation(ctx)
	if e != nil {
		return "", e
	}
	sum := sha256.Sum256(data)
	out, e := h.content.Call(ctx, artifacts.Binding{Token: h.token, Namespace: h.namespace, Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: data, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "task", Sources: sources, AcquiredAt: h.now().Unix(), MediaType: "application/json", Size: uint64(len(data)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: until}})
	if e != nil {
		return "", e
	}
	return answers.Reference(out.Record.Ref), nil
}
func (h *harness) request(ctx context.Context, data []byte, version uint64) (execution.Request, string, error) {
	input, e := h.put(ctx, data)
	if e != nil {
		return execution.Request{}, "", e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return execution.Request{}, "", e
	}
	t, e := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: h.namespace, OperationID: op, Goal: "Apply selected business transition", InputRefs: []string{input}, Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: h.now().Add(5 * time.Minute).Unix()}})
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
	r := execution.Request{ControlVersion: 1, OperationID: op, Qualification: tasks.QualificationOf(s), Capability: h.cap.Name, Version: h.cap.Version, Implementation: h.cap.Implementation, ImplementationVersion: h.cap.ImplementationVersion, DescriptorSHA256: h.cap.Digest(), InputRef: input, ResourceVersion: version}
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

func (h *harness) selectEntry(e catalog.Entry) {
	h.cap = e.Capability
	h.exec = h.services[e.Ref.Digest]
	h.client = sdk.NewCapabilityClient(executionlocal.Bind(h.exec, h.namespace), h.namespace)
}
func (h *harness) Resolve(ctx context.Context, ns string) ([]executionrouter.Bound, error) {
	if ns != h.namespace {
		return nil, &authorization.Error{Code: authorization.Denied}
	}
	out := make([]executionrouter.Bound, 0, len(h.entries))
	for _, entry := range h.entries {
		out = append(out, executionrouter.Bound{Ref: entry.Ref, Service: h.services[entry.Ref.Digest]})
	}
	return out, nil
}

func (h *harness) scope() execution.ResourceScope {
	scope := execution.ResourceScope{Ref: h.target.Resource(), Authority: simworkflow.Authority, AuthorizationResource: "root", Purpose: "task", Location: "local", MaxOperations: 32, MaxChecks: 8, PollInterval: time.Second, Lease: 3 * time.Second, Window: time.Minute}
	for _, entry := range h.entries {
		scope.Participants = append(scope.Participants, entry.Ref.Digest)
	}
	return scope
}
