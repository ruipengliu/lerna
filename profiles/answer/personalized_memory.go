package answer

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"lerna/adapters/authorization/josegrant"
	contentpolicy "lerna/adapters/content/policy"
	contextmemory "lerna/adapters/context/memory"
	contextpolicy "lerna/adapters/context/policy"
	sqlitecontext "lerna/adapters/context/sqlite"
	taskcontext "lerna/adapters/context/task"
	memoryauth "lerna/adapters/memory/auth"
	memorycleanup "lerna/adapters/memory/cleanup"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/authorization"
	"lerna/cleanup"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"lerna/tasks"
	"path/filepath"
	"time"
)

type personalizedMemory struct {
	checkpoints       *memorycleanup.Checkpoints
	checkpointBinding memory.ConsumerBinding
	cleanup           *cleanup.Running
	checkpoint        *answerContextCheckpoint
	service           *memory.Service
	binding           memory.Binding
	original          *wire.MemoryWrite
	store             *sqlitememory.Store
	snapshots         *sqlitecontext.Store
	grants            *authorization.GrantAuthority
	grantID           string
	session           *taskcontext.Session
}

func (p *personalizedMemory) close() {
	if p.cleanup != nil {
		p.cleanup.Close()
	}
	if p.snapshots != nil {
		p.snapshots.Close()
	}
	if p.store != nil {
		p.store.Close()
	}
}

func (h *harness) personalize(ctx context.Context, baseline tasks.RunSnapshot, style string, applicable bool) (*personalizedMemory, error) {
	return h.personalizeBound(ctx, baseline, style, applicable, nil, false)
}
func (h *harness) personalizeBound(ctx context.Context, baseline tasks.RunSnapshot, style string, applicable bool, saved *answerContextCheckpoint, referenceOnly bool) (p *personalizedMemory, err error) {
	recovering := saved != nil
	if !recovering {
		saved = &answerContextCheckpoint{Format: 2, ReferenceOnly: referenceOnly, Token: h.token, Location: h.location, Baseline: baseline}
		if style == "missing" {
			saved.MissingRetainUntil = time.Now().Add(10 * time.Minute).Unix()
		}
	}

	p = &personalizedMemory{checkpoint: saved}
	owned := p
	defer func() {
		if err != nil {
			owned.close()
		}
	}()
	change := func(command *wire.AuthorizationCommand) error {
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return e
		}
		snap, e := h.db.Load(ctx)
		if e != nil {
			return e
		}
		command.ExpectedRevision = snap.State.Revision
		_, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command})
		return e
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "task.pause", "task.cancel", "task.resume", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete", "memory.put", "memory.get", "memory.correct", "memory.delete", "memory.store", "memory.store_reference", "memory.process", "memory.discover", "memory.disclose"}, Purposes: []string{"task"}, Locations: []string{"local", h.location}, ExpiresUnix: time.Now().Add(time.Hour).Unix()}
	if !recovering {
		if err = change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "personalized", Scope: scope}}}}}); err != nil {
			return nil, err
		}
		if err = change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "personalized", Subject: "operator", Mode: "continuous", Scope: scope}}}); err != nil {
			return nil, err
		}
	}
	rule := h.rule(true)
	rule.Actions = append(rule.Actions, "store_reference")
	if recovering {
		rule = saved.Rule
	} else {
		saved.Rule = rule
	}
	if err = h.policy.Replace([]contentpolicy.Rule{rule}); err != nil {
		return nil, err
	}
	document := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:memory:style","type":"object","properties":{"style":{"enum":["concise","detailed"]}},"required":["style"],"additionalProperties":false}`)
	schemas, e := schema.New([]schema.Resource{{Type: "style", ID: "urn:memory:style", Version: "1", Document: document}})
	if e != nil {
		return nil, e
	}
	policy, e := memoryauth.New(h.auth, h.policy, []memoryauth.Collection{{MissingMetadataUntil: saved.MissingRetainUntil, Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "private", Purpose: "task", Storage: []string{"local"}, ReferenceStorage: []string{"local"}, Processing: []string{"local", h.location}, Recipients: []string{"local", h.location}, Schemas: map[string]string{"style": schema.Digest(document)}}})
	if e != nil {
		return nil, e
	}
	p.store, err = sqlitememory.Open(filepath.Join(h.root, "memory.db"))
	if err != nil {
		return nil, err
	}
	service, e := memory.New(p.store, policy, schemas, wallClock{}, memory.Config{Location: "local", Timeout: time.Second})
	if e != nil {
		return nil, e
	}
	binding := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: h.location}
	ref := &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "answer-style"}
	p.service, p.binding = service, binding
	if !recovering && saved.MissingRetainUntil == 0 {
		body, _ := json.Marshal(map[string]string{"style": style})
		condition := "when answering"
		if !applicable {
			condition = "when purchasing"
		}
		now := time.Now()
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return nil, e
		}
		write := &wire.MemoryWrite{OperationId: op, Ref: ref, Spec: &wire.MemorySpec{Kind: "preference", About: "operator", Conditions: condition, PolicyRef: "private", Purpose: "task", RecordedAt: now.Unix(), RetainUntil: now.Add(10 * time.Minute).Unix(), Confidence: &wire.MemoryConfidence{Assessment: "explicit", Basis: "public synthetic input", Method: "declaration"}, Sources: []*wire.MemorySource{{Ref: h.source, Method: "explicit"}}, Content: &wire.DynamicPayload{TypeName: "style", SchemaId: "urn:memory:style", SchemaVersion: "1", SchemaDigest: schema.Digest(document), Json: body}}}
		p.original = write
		_, e = service.Put(ctx, binding, write)
		if e != nil {
			return nil, e
		}
	}
	var key *ecdsa.PrivateKey
	if recovering {
		key, err = x509.ParseECPrivateKey(saved.KeyDER)
	} else {
		key, err = ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err == nil {
			saved.KeyDER, err = x509.MarshalECPrivateKey(key)
		}
	}
	if err != nil {
		return nil, err
	}
	crypto, e := josegrant.New("memory-signer", key, map[string]*ecdsa.PublicKey{"memory-signer": &key.PublicKey})
	if e != nil {
		return nil, e
	}
	p.grants, err = h.auth.SignedGrants(authorization.GrantConfig{Issuer: "local", MaxTTL: time.Hour, IOTimeout: time.Second, MaxDepth: 4, MaxRecords: 32}, crypto)
	if err != nil {
		return nil, err
	}
	peer := memoryauth.Peer{Audience: "memory", Presenter: "local", CertificateSHA256: base64.RawURLEncoding.EncodeToString(make([]byte, 32))}
	permits, e := memoryauth.NewReadPermits(policy, p.grants, peer)
	if e != nil {
		return nil, e
	}
	reader, e := memory.NewReader(service, permits)
	if e != nil {
		return nil, e
	}
	if !recovering {
		readID, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return nil, e
		}
		get := &wire.MemoryGet{ReadId: readID, Ref: ref, Revision: 1, Purpose: "task"}
		intent, e := memory.DescribeGet(binding, get)
		if e != nil {
			return nil, e
		}
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return nil, e
		}
		state, e := h.db.Load(ctx)
		if e != nil {
			return nil, e
		}
		grant, e := p.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: state.State.Revision, Kind: "ISSUE", Spec: &wire.SignedGrantSpec{Subject: "operator", Audience: peer.Audience, Presenter: peer.Presenter, CertificateSha256: peer.CertificateSHA256, Scope: scope, NotBefore: time.Now().Unix(), Units: 1, Mode: "single", OperationBinding: readID, SemanticSha256: intent.SemanticSHA256}})
		if e != nil {
			return nil, e
		}
		saved.ReadID, saved.Material, saved.GrantID = readID, grant.Material, grant.GrantId
	}
	p.grantID = saved.GrantID
	dependency := contextassembly.Reference{Namespace: "local", Collection: "personal", Key: "answer-style", Revision: 1}
	resolver, e := contextmemory.NewSources(service, func(view artifacts.SourceAuthority) memory.Checker { return policy.ReadPolicy(view) }, h.policy, "local", []contextassembly.Reference{dependency})
	if e != nil {
		return nil, e
	}
	h.content, err = artifacts.New(h.auth, h.blobs, resolver, artifacts.Config{Inline: 64, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour})
	if err != nil {
		return nil, err
	}
	h.access, err = answers.NewContentAccess(h.content, h.policy, h.binding(), h.source, "task", wallClock{}, 5*time.Minute)
	if err != nil {
		return nil, err
	}
	req := contextassembly.Request{Key: contextassembly.Key{Namespace: "local", TaskID: baseline.Task.Ref.TaskID, Decision: 1}, Subject: "operator", Purpose: "task", Location: h.location, Storage: "local", FactsVersion: baseline.UpdateVersion + 1, PolicyVersion: "answer-style-v1", MaxBytes: 32768, Candidates: []contextassembly.Candidate{{Reference: dependency}}}
	if saved.MissingRetainUntil > 0 {
		req.PolicyVersion = "answer-style-missing-v1"
	}
	if saved.ReferenceOnly {
		req.PolicyVersion = "answer-style-reference-v1"
	}
	projector, e := contextmemory.NewProjector(req.PolicyVersion, []contextmemory.ProjectionRule{{SchemaID: "urn:memory:style", SchemaVersion: "1", Kind: "preference", Condition: "when answering", Claim: "reply-style", Field: "style", Values: []string{"concise", "detailed"}}})
	if e != nil {
		return nil, e
	}
	var missingChecker memory.MissingChecker
	if saved.MissingRetainUntil > 0 {
		missingChecker = policy
	}
	adapter, e := contextmemory.New(service, reader, permits, projector, contextmemory.Config{MissingChecker: missingChecker, MissingRetainUntil: saved.MissingRetainUntil, ReferenceOnly: saved.ReferenceOnly, Binding: binding, Decision: req.Key, FactsVersion: req.FactsVersion, PolicyVersion: req.PolicyVersion, Purpose: req.Purpose, Storage: req.Storage, Authorizations: []contextmemory.Authorization{{Reference: dependency, ReadID: saved.ReadID, Material: saved.Material}}})
	if e != nil {
		return nil, e
	}
	facts, e := taskcontext.NewFacts(h.generation, h.access, wallClock{}, baseline, taskcontext.Scope{Purpose: req.Purpose, Location: req.Location, Storage: req.Storage, PolicyVersion: req.PolicyVersion})
	if e != nil {
		return nil, e
	}
	p.snapshots, err = sqlitecontext.Open(filepath.Join(h.root, "context.db"))
	if err != nil {
		return nil, err
	}
	policyHash := sha256.Sum256([]byte("answer-context:context.db:v1"))
	contextPolicy, e := contextpolicy.New(h.auth, p.snapshots, contextpolicy.Config{Namespace: "local", Consumer: "answer-context", ConfigSHA256: fmt.Sprintf("%x", policyHash), Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	bound, e := contextPolicy.Bind(h.token, adapter)
	if e != nil {
		return nil, e
	}
	assembler, e := contextassembly.NewWithPolicy(p.snapshots, facts, adapter, bound)
	if e != nil {
		return nil, e
	}
	p.session, err = taskcontext.NewSession(assembler, req, baseline.Task)
	if err != nil {
		return nil, err
	}
	lineage, e := p.session.Lineage(adapter)
	if e != nil {
		return nil, e
	}
	h.access, err = h.access.WithLineage(lineage)
	if err != nil {
		return nil, err
	}
	h.port, err = answers.BindContextPort(h.generation, h.access, h.location, p.session)
	if err != nil {
		return nil, err
	}
	contextSink, e := memorycleanup.NewContexts(p.snapshots)
	if e != nil {
		return nil, e
	}
	sourceHash := sha256.Sum256([]byte("answer-context-source-cleanup:context.db:v1"))
	sourceConsumer, e := memory.NewSourceConsumer(p.store, p.store, contextSink, memory.ConsumerConfig{Binding: memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "answer-contexts", ConfigSHA256: fmt.Sprintf("%x", sourceHash)}, Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	artifactSink, e := memorycleanup.NewArtifacts(h.content)
	if e != nil {
		return nil, e
	}
	artifactHash := sha256.Sum256([]byte("answer-artifact-source-cleanup:content:v1"))
	artifactConsumer, e := memory.NewSourceConsumer(p.store, p.store, artifactSink, memory.ConsumerConfig{Binding: memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "answer-artifacts", ConfigSHA256: fmt.Sprintf("%x", artifactHash)}, Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	admissionSink, e := memorycleanup.NewAdmissions(p.store, h.auth)
	if e != nil {
		return nil, e
	}
	admissionHash := sha256.Sum256([]byte("answer-admission-comparison-cleanup:authority.db:v1"))
	admissionConsumer, e := memory.NewSourceConsumer(p.store, p.store, admissionSink, memory.ConsumerConfig{Binding: memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: "answer-admissions", ConfigSHA256: fmt.Sprintf("%x", admissionHash)}, Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	checkpointKey := contextKey(p.checkpoint)
	checkpointScope, e := json.Marshal(struct {
		Root string
		Key  contextassembly.Key
	}{h.root, checkpointKey})
	if e != nil {
		return nil, e
	}
	checkpointHash := sha256.Sum256(append([]byte("answer-checkpoint-cleanup:v1\n"), checkpointScope...))
	p.checkpointBinding = memory.ConsumerBinding{Namespace: "local", Collection: "personal", Consumer: fmt.Sprintf("answer-checkpoints-%x", checkpointHash[:8]), ConfigSHA256: fmt.Sprintf("%x", checkpointHash)}
	p.checkpoints, e = memorycleanup.NewCheckpoints(p.store, p.store, memorycleanup.CheckpointFiles{
		Maintain: func(ctx context.Context) error { return cleanAnswerCheckpoint(ctx, h.root, p.snapshots, checkpointKey) },
		Inspect:  func(ctx context.Context) (bool, error) { return answerCheckpointClean(ctx, h.root, checkpointKey) },
	}, memory.ConsumerConfig{Binding: p.checkpointBinding, Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}

	p.cleanup, err = cleanup.Start([]cleanup.Job{
		{Name: "context-sources", Run: func(ctx context.Context) error { _, e := sourceConsumer.Run(ctx); return e }},
		{Name: "context-original-uses", Run: func(ctx context.Context) error { _, e := contextPolicy.Clean(ctx); return e }},
		{Name: "artifact-sources", Run: func(ctx context.Context) error { _, e := artifactConsumer.Run(ctx); return e }},
		{Name: "artifact-original-uses", Run: h.content.Clean},
		{Name: "admission-comparisons", Run: func(ctx context.Context) error { _, e := admissionConsumer.Run(ctx); return e }},
		{Name: "legacy-checkpoint", Run: func(ctx context.Context) error { _, e := p.checkpoints.Run(ctx); return e }},
	}, cleanup.Config{Interval: time.Second, Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}
	return p, nil
}
