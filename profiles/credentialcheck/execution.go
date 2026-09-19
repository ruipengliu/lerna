package credentialcheck

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"lerna/adapters/authorization/josegrant"
	filecontent "lerna/adapters/content/file"
	contentpolicy "lerna/adapters/content/policy"
	credentialexecution "lerna/adapters/credentials/execution"
	executioncontent "lerna/adapters/execution/content"
	executionlocal "lerna/adapters/execution/local"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/credentials"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/schema"
	"lerna/sdk"
	"lerna/tasks"
	"path/filepath"
	"time"
)

type observer struct{ f *fixture }

func (o observer) Target() credentials.Binding { return o.f.binding }
func (o observer) Observe(ctx context.Context, op string) (credentialexecution.State, error) {
	var delta int
	e := o.f.target.QueryRowContext(ctx, "SELECT delta FROM effects WHERE op=?", op).Scan(&delta)
	if e == sql.ErrNoRows {
		return credentialexecution.State{}, nil
	}
	if e != nil {
		return credentialexecution.State{}, e
	}
	var value, version uint64
	e = o.f.target.QueryRowContext(ctx, "SELECT coalesce(sum(delta),0),count(*)+1 FROM effects").Scan(&value, &version)
	return credentialexecution.State{Applied: true, Value: value, Version: version}, e
}
func executionCase(ctx context.Context) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	if e = f.setPolicy(ctx, []string{"credential.manage", "credential.use", "task.submit", "task.read", "task.execute", "task.reconcile", "capability.invoke", "capability.read", "capability.reconcile", "resource.change", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete"}); e != nil {
		return e
	}
	now, _ := f.clock.Now()
	operation := func(ctx context.Context) (string, error) { return f.auth.NewOperation(ctx, f.token) }
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return e
	}
	crypto, e := josegrant.New("key", key, map[string]*ecdsa.PublicKey{"key": &key.PublicKey})
	if e != nil {
		return e
	}
	grants, e := f.auth.SignedGrants(authorization.GrantConfig{Issuer: "local-authority", MaxTTL: time.Hour, IOTimeout: time.Second, MaxDepth: 4, MaxRecords: 64}, crypto)
	if e != nil {
		return e
	}
	core, e := tasks.New(f.auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "owner", MaxTasks: 64, MaxPage: 10})
	if e != nil {
		return e
	}
	work, e := core.BindWorker(tasks.WorkerBinding{Token: f.token, Subject: "alice", WorkerID: "worker", AllowEffectEvidence: true}, tasks.RunLimits{Lease: 10 * time.Second, RenewEvery: time.Second, DecisionTimeout: 5 * time.Second, IOTimeout: time.Second, MaxAttempts: 3, MaxConcurrent: 1})
	if e != nil {
		return e
	}
	blobs, e := filecontent.Open(filepath.Join(f.root, "content"))
	if e != nil {
		return e
	}
	defer blobs.Close()
	policy, e := contentpolicy.New([]contentpolicy.Rule{{Kind: "input", Key: "public-counter", Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: now.Add(time.Hour).Unix()}})
	if e != nil {
		return e
	}
	content, e := artifacts.New(f.auth, blobs, policy, artifacts.Config{Inline: 64, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 64, MaxChunk: 65536, MaxFiles: 128, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if e != nil {
		return e
	}
	access := executioncontent.New(content, artifacts.Binding{Token: f.token, Namespace: "local", Location: "local", Recipient: "local"}, f.clock)
	cap := execution.Capability{Name: "counter.add", Version: "1", Implementation: "credential-http", ImplementationVersion: "1", Resource: "root", Purpose: "task", Location: "local", Exclusive: true, Synchronous: true,
		Input:  schema.Resource{Type: "counter.input", ID: "urn:credential:counter:input", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:credential:counter:input","type":"object","required":["delta"],"additionalProperties":false,"properties":{"delta":{"type":"integer","minimum":1,"maximum":9}}}`)},
		Output: schema.Resource{Type: "counter.output", ID: "urn:credential:counter:output", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:credential:counter:output","type":"object","required":["value","version"],"additionalProperties":false,"properties":{"value":{"type":"integer","minimum":0},"version":{"type":"integer","minimum":1}}}`)}}
	driver, e := credentialexecution.New(f.driver, observer{f}, f.token, "credential", cap.Name)
	if e != nil {
		return e
	}
	binding := execution.Binding{Token: f.token, Namespace: "local", Subject: "alice", Audience: "execution-local", Presenter: "local-host", CertificateSHA256: base64.RawURLEncoding.EncodeToString(make([]byte, 32)), Worker: "worker"}
	executor, e := execution.New(grants, work, access, driver, binding, cap, execution.Config{MaxOperations: 32, MaxReports: 8, MaxChecks: 4, MaxOutbox: 64, MaxInput: 32768, MaxOutput: 8192, IOTimeout: time.Second, DriverTimeout: time.Second}, operation)
	if e != nil {
		return e
	}
	op, e := operation(ctx)
	if e != nil {
		return e
	}
	body := []byte(`{"delta":3}`)
	sum := sha256.Sum256(body)
	put, e := content.Call(ctx, artifacts.Binding{Token: f.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentRequest{Method: "PUT", OperationId: op, Data: body, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "task", Sources: []*wire.ContentSource{{Kind: "input", Key: "public-counter", Revision: 1}}, AcquiredAt: now.Unix(), MediaType: "application/json", Size: uint64(len(body)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: now.Add(time.Hour).Unix()}})
	if e != nil {
		return e
	}
	ref := answers.Reference(put.Record.Ref)
	op, e = operation(ctx)
	if e != nil {
		return e
	}
	task, e := core.Submit(ctx, f.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Add three through the credential-bound API", InputRefs: []string{ref}, Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: now.Add(5 * time.Minute).Unix()}})
	if e != nil {
		return e
	}
	run, e := core.Load(ctx, task.Ref)
	if e != nil {
		return e
	}
	for _, phase := range []string{"claim", "start"} {
		run, e = work.Commit(ctx, tasks.WorkChange{ChangeID: phase, Kind: phase, Qualification: tasks.QualificationOf(run)})
		if e != nil {
			return e
		}
	}
	op, e = operation(ctx)
	if e != nil {
		return e
	}
	request := execution.Request{OperationID: op, Qualification: tasks.QualificationOf(run), Capability: cap.Name, Version: cap.Version, Implementation: cap.Implementation, ImplementationVersion: cap.ImplementationVersion, DescriptorSHA256: cap.Digest(), InputRef: ref, ResourceVersion: 1}
	grantOp, e := operation(ctx)
	if e != nil {
		return e
	}
	snap, e := f.db.Load(ctx)
	if e != nil {
		return e
	}
	grant, e := grants.Mutate(ctx, f.token, &wire.GrantMutation{OperationId: grantOp, Kind: "ISSUE", ExpectedRevision: snap.State.Revision, Spec: &wire.SignedGrantSpec{Subject: binding.Subject, Audience: binding.Audience, Presenter: binding.Presenter, CertificateSha256: binding.CertificateSHA256, Scope: &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"resource.change"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: now.Add(time.Hour).Unix()}, NotBefore: now.Unix(), Units: 1, Mode: "single", OperationBinding: op, SemanticSha256: request.Fingerprint()}})
	if e != nil {
		return e
	}
	client := sdk.NewCapabilityClient(executionlocal.Bind(executor, "local"), "local")
	if _, e = client.Invoke(ctx, request, grant.Material); e != nil {
		return e
	}
	if _, e = executor.Run(ctx, op); e != nil {
		return e
	}
	if e = executor.Drain(ctx, 16); e != nil {
		return e
	}
	result, e := core.Get(ctx, f.token, task.Ref)
	if e != nil {
		return e
	}
	value, e := f.value(ctx)
	if e != nil || value != 3 || result.State != "COMPLETED" || f.sent() != 1 {
		return fmt.Errorf("credential-backed Execution did not complete: %s", result.State)
	}
	return nil
}
