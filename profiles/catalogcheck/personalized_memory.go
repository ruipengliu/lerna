package catalogcheck

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/contextmemory"
	"lerna/adapters/contextpolicy"
	"lerna/adapters/memoryauth"
	"lerna/adapters/memorycleanup"
	"lerna/adapters/sqlitecontext"
	"lerna/adapters/sqlitememory"
	"lerna/adapters/taskcontext"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	"lerna/cleanup"

	"lerna/authorization"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"lerna/tasks"
	"path/filepath"
	"time"
)

// Private host recovery material; contains no duplicate Memory body.
type actionMemoryBinding struct {
	Key                       contextassembly.Key
	Revision                  uint64
	GrantIDs                  []string
	ReadID, Material, GrantID string
	Scope                     []byte
	Rule                      contentpolicy.Rule
}

type personalizedMemory struct {
	checkpoints       *memorycleanup.Checkpoints
	checkpointBinding memory.ConsumerBinding
	cleanup           *cleanup.Running
	deletion          *memory.SourceEvent
	bindDecision      func(context.Context, tasks.RunSnapshot, uint32) error
	refreshDecision   func(context.Context, tasks.RunSnapshot, uint32) error
	latestRevision    uint64
	grantIDs          []string
	snapshotDigests   map[uint32][32]byte
	baseline          tasks.RunSnapshot
	checkpoint        actionMemoryBinding
	service           *memory.Service
	binding           memory.Binding
	original          *wire.MemoryWrite
	changed           bool
	lineage           answers.Lineage
	scope             *wire.AuthorizationScope
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

func (a *actionHost) personalize(ctx context.Context, core *tasks.ActionPort, baseline tasks.RunSnapshot, style string, applicable bool) (p *personalizedMemory, err error) {
	return a.personalizeBound(ctx, core, baseline, style, applicable, nil)
}

func (a *actionHost) personalizeBound(ctx context.Context, core *tasks.ActionPort, baseline tasks.RunSnapshot, style string, applicable bool, saved *actionMemoryBinding) (*personalizedMemory, error) {
	return a.personalizeBindings(ctx, core, baseline, style, applicable, saved, true)
}

// Reconciliation restores source governance without releasing or reassembling
// stale decision content. Only the trusted host chooses this for Started records.
func (a *actionHost) personalizeBindings(ctx context.Context, core *tasks.ActionPort, baseline tasks.RunSnapshot, style string, applicable bool, saved *actionMemoryBinding, assemble bool) (p *personalizedMemory, err error) {
	h := a.h
	locations := []string{"local"}
	if a.location != "local" {
		locations = append(locations, a.location)
	}
	p = &personalizedMemory{baseline: baseline, latestRevision: 1}
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
		_, e = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: h.namespace, OperationID: op, Command: command})
		return e
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "task.pause", "task.cancel", "task.resume", "task.input", "catalog.list", "catalog.search", "catalog.describe", "capability.invoke", "capability.read", "capability.reconcile", "resource.change", "resource.control.read", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete", "memory.put", "memory.get", "memory.correct", "memory.delete", "memory.store", "memory.store_reference", "memory.process", "memory.discover", "memory.disclose"}, Purposes: []string{"task"}, Locations: locations, ExpiresUnix: h.now().Add(time.Hour).Unix()}
	if saved == nil {
		if err = change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "personalized", Scope: scope}}}}}); err != nil {
			return nil, err
		}
		if err = change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "personalized", Subject: "operator", Mode: "continuous", Scope: scope}}}); err != nil {
			return nil, err
		}
	} else {
		scope = &wire.AuthorizationScope{}
		if len(saved.Scope) > 65536 || proto.Unmarshal(saved.Scope, scope) != nil {
			return nil, fmt.Errorf("invalid original Memory scope")
		}
		if saved.Key.Namespace != h.namespace || saved.Key.TaskID != baseline.Task.Ref.TaskID || saved.Key.Decision == 0 || saved.Revision > 1<<32 || saved.Key.Decision > uint64(^uint32(0)) || saved.ReadID == "" || saved.Material == "" || saved.GrantID == "" || scope == nil {
			return nil, fmt.Errorf("invalid original action Memory binding")
		}
	}
	rule := contentpolicy.Rule{Kind: "input", Key: "public-counter", Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: locations, RetainUntil: h.now().Add(time.Hour).Unix()}
	rule.Actions = append(rule.Actions, "store_reference")
	if saved != nil {
		rule = saved.Rule
	}
	if err = h.policy.Replace([]contentpolicy.Rule{rule}); err != nil {
		return nil, err
	}
	document := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:memory:record","type":"object","properties":{"style":{"enum":["item","alternative"]}},"required":["style"],"additionalProperties":false}`)
	schemas, e := schema.New([]schema.Resource{{Type: "style", ID: "urn:memory:record", Version: "1", Document: document}})
	if e != nil {
		return nil, e
	}
	policy, e := memoryauth.New(h.auth, h.policy, []memoryauth.Collection{{Namespace: h.namespace, Name: "personal", Resource: "root", PolicyRef: "private", Purpose: "task", Storage: []string{"local"}, ReferenceStorage: []string{"local"}, Processing: locations, Recipients: locations, Schemas: map[string]string{"style": schema.Digest(document)}}})
	if e != nil {
		return nil, e
	}
	p.store, err = sqlitememory.Open(filepath.Join(h.root, "memory.db"))
	if err != nil {
		return nil, err
	}
	service, e := memory.New(p.store, policy, schemas, h.clock, memory.Config{Location: "local", Timeout: time.Second})
	if e != nil {
		return nil, e
	}
	binding := memory.Binding{Token: h.token, Namespace: h.namespace, Subject: "operator", Location: "local", Recipient: a.location}
	ref := &wire.MemoryRef{Namespace: h.namespace, Collection: "personal", Key: "record-choice"}
	p.service, p.binding = service, binding
	if saved == nil {
		body, _ := json.Marshal(map[string]string{"style": style})
		condition := "when authorizing"
		if !applicable {
			condition = "when purchasing"
		}
		now := h.now()
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return nil, e
		}
		write := &wire.MemoryWrite{OperationId: op, Ref: ref, Spec: &wire.MemorySpec{Kind: "preference", About: "operator", Conditions: condition, PolicyRef: "private", Purpose: "task", RecordedAt: now.Unix(), RetainUntil: now.Add(10 * time.Minute).Unix(), Confidence: &wire.MemoryConfidence{Assessment: "explicit", Basis: "public synthetic input", Method: "declaration"}, Sources: []*wire.MemorySource{{Ref: &wire.ContentSource{Kind: "input", Key: "public-counter", Revision: 1}, Method: "explicit"}}, Content: &wire.DynamicPayload{TypeName: "style", SchemaId: "urn:memory:record", SchemaVersion: "1", SchemaDigest: schema.Digest(document), Json: body}}}
		p.service, p.binding, p.original = service, binding, write
		_, e = service.Put(ctx, binding, write)
		if e != nil {
			return nil, e
		}
	}
	p.grants = h.grants
	peer := memoryauth.Peer{Audience: "memory", Presenter: "local", CertificateSHA256: base64.RawURLEncoding.EncodeToString(make([]byte, 32))}
	permits, e := memoryauth.NewReadPermits(policy, p.grants, peer)
	if e != nil {
		return nil, e
	}
	reader, e := memory.NewReader(service, permits)
	if e != nil {
		return nil, e
	}
	var readID, material string
	issueRead := func(ctx context.Context, revision uint64) error {
		readID, e = h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return e
		}
		get := &wire.MemoryGet{ReadId: readID, Ref: ref, Revision: revision, Purpose: "task"}
		intent, e := memory.DescribeGet(binding, get)
		if e != nil {
			return e
		}
		op, e := h.auth.NewOperation(ctx, h.token)
		if e != nil {
			return e
		}
		state, e := h.db.Load(ctx)
		if e != nil {
			return e
		}
		grant, e := p.grants.Mutate(ctx, h.token, &wire.GrantMutation{OperationId: op, ExpectedRevision: state.State.Revision, Kind: "ISSUE", Spec: &wire.SignedGrantSpec{Subject: "operator", Audience: peer.Audience, Presenter: peer.Presenter, CertificateSha256: peer.CertificateSHA256, Scope: scope, NotBefore: h.now().Unix(), Units: 1, Mode: "single", OperationBinding: readID, SemanticSha256: intent.SemanticSHA256}})
		if e != nil {
			return e
		}
		p.grantID = grant.GrantId
		material = grant.Material
		p.grantIDs = append(p.grantIDs, grant.GrantId)
		return nil
	}
	if saved == nil {
		if e = issueRead(ctx, 1); e != nil {
			return nil, e
		}
	} else {
		readID, material, p.grantID = saved.ReadID, saved.Material, saved.GrantID
		p.grantIDs = append([]string(nil), saved.GrantIDs...)
		if len(p.grantIDs) == 0 {
			p.grantIDs = []string{saved.GrantID}
		}
		if len(p.grantIDs) > 3 || p.grantIDs[len(p.grantIDs)-1] != saved.GrantID {
			return nil, fmt.Errorf("invalid grant history")
		}
		seen := map[string]bool{}
		for _, id := range p.grantIDs {
			if id == "" || seen[id] {
				return nil, fmt.Errorf("invalid grant history")
			}
			seen[id] = true
		}
		if saved.Revision > 0 {
			p.latestRevision = saved.Revision
		}
	}
	p.scope = scope
	initialDecision := uint32(1)
	if saved != nil {
		initialDecision = uint32(saved.Key.Decision)
		if baseline.Actions == nil || len(baseline.Actions.Decisions) == 0 && initialDecision != 1 || len(baseline.Actions.Decisions) > 0 && baseline.Actions.Decisions[len(baseline.Actions.Decisions)-1].Number != initialDecision {
			return nil, fmt.Errorf("checkpoint decision does not match Core baseline")
		}
	}
	dependency := contextassembly.Reference{Namespace: h.namespace, Collection: "personal", Key: "record-choice", Revision: p.latestRevision}
	p.snapshots, err = sqlitecontext.Open(filepath.Join(h.root, "context.db"))
	if err != nil {
		return nil, err
	}
	policyHash := sha256.Sum256([]byte("action-context:context.db:v1"))
	contextPolicy, e := contextpolicy.New(h.auth, p.snapshots, contextpolicy.Config{Namespace: h.namespace, Consumer: "action-context", ConfigSHA256: fmt.Sprintf("%x", policyHash), Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	p.snapshotDigests = map[uint32][32]byte{}
	p.bindDecision = func(ctx context.Context, baseline tasks.RunSnapshot, decision uint32) error {
		var err error
		req := contextassembly.Request{Key: contextassembly.Key{Namespace: h.namespace, TaskID: baseline.Task.Ref.TaskID, Decision: uint64(decision)}, Subject: "operator", Purpose: "task", Location: a.location, Storage: "local", FactsVersion: baseline.UpdateVersion + 1, PolicyVersion: "record-choice-v1", MaxBytes: 32768, Candidates: []contextassembly.Candidate{{Reference: dependency}}}
		scopeBytes, e := proto.Marshal(scope)
		if e != nil {
			return e
		}
		p.checkpoint = actionMemoryBinding{Revision: dependency.Revision, GrantIDs: append([]string(nil), p.grantIDs...), Key: req.Key, ReadID: readID, Material: material, GrantID: p.grantID, Scope: scopeBytes, Rule: rule}
		projector, e := contextmemory.NewProjector(req.PolicyVersion, []contextmemory.ProjectionRule{{SchemaID: "urn:memory:record", SchemaVersion: "1", Kind: "preference", Condition: "when authorizing", Claim: "preferred-record", Field: "style", Values: []string{"item", "alternative"}}})
		if e != nil {
			return e
		}
		adapter, e := contextmemory.New(service, reader, permits, projector, contextmemory.Config{Binding: binding, Decision: req.Key, FactsVersion: req.FactsVersion, PolicyVersion: req.PolicyVersion, Purpose: req.Purpose, Storage: req.Storage, Authorizations: []contextmemory.Authorization{{Reference: dependency, ReadID: readID, Material: material}}})
		if e != nil {
			return e
		}
		facts, e := taskcontext.NewFacts(core, actionBaseContext{a}, h.clock, baseline, taskcontext.Scope{Purpose: req.Purpose, Location: req.Location, Storage: req.Storage, PolicyVersion: req.PolicyVersion})
		if e != nil {
			return e
		}
		bound, e := contextPolicy.Bind(h.token, adapter)
		if e != nil {
			return e
		}
		assembler, e := contextassembly.NewWithPolicy(p.snapshots, facts, adapter, bound)
		if e != nil {
			return e
		}
		p.session, err = taskcontext.NewSession(assembler, req, baseline.Task)
		if err != nil {
			return err
		}
		resolver, e := contextmemory.NewSources(service, func(view artifacts.SourceAuthority) memory.Checker { return policy.ReadPolicy(view) }, h.policy, "local", []contextassembly.Reference{dependency})
		if e != nil {
			return e
		}
		h.sources.bind(resolver)
		if !assemble && decision == initialDecision {
			return nil
		}
		// A real second correction before the second decision can bind any body.
		if a.mode == "personalized-reassemble-gap" && decision == 2 {
			p.changed = false
			if e = p.change(ctx, h, "related"); e != nil {
				return e
			}
		}
		if _, e = p.session.Assemble(ctx, baseline.Task, a.location, 32768); e != nil {
			return e
		}
		if a.mode == "personalized-reassemble-bound-failure" && decision == 2 {
			p.changed = false
			if e = p.change(ctx, h, "related"); e != nil {
				return e
			}
		}
		provider, e := p.session.Lineage(adapter)
		if e != nil {
			return e
		}
		p.lineage, e = provider.Sources(ctx, baseline.Task, "local")
		if e != nil {
			return e
		}
		snapshot, e := p.snapshots.Read(ctx, p.checkpoint.Key)
		if e != nil {
			return e
		}
		p.snapshotDigests[decision] = sha256.Sum256(snapshot.Document)
		p.baseline = baseline
		return nil
	}
	p.refreshDecision = func(ctx context.Context, baseline tasks.RunSnapshot, decision uint32) error {
		if p.latestRevision <= dependency.Revision || len(p.grantIDs) >= 3 {
			return contextassembly.Invalidated
		}
		// Credential preparation has its own budget; assembly receives the caller's
		// remaining deadline and enforces its existing independent five-second cap.
		authorize, cancel := context.WithTimeout(ctx, 5*time.Second)
		authorizeErr := func() error {
			// An old revoked permit must not be replaced by issuing a fresh one.
			original, e := memory.DescribeGet(binding, &wire.MemoryGet{ReadId: readID, Ref: ref, Revision: dependency.Revision, Purpose: "task"})
			if e != nil {
				return e
			}
			if e = permits.Authorize(authorize, binding, original, material); e != nil {
				return e
			}
			if e = service.ValidateProcessing(authorize, binding, ref, p.latestRevision, "task", a.location); e != nil {
				return e
			}
			if e = core.CheckDecision(authorize, tasks.QualificationOf(baseline)); e != nil {
				return e
			}
			if e = issueRead(authorize, p.latestRevision); e != nil {
				return e
			}
			return nil
		}()
		cancel()
		if authorizeErr != nil {
			return authorizeErr
		}
		dependency.Revision = p.latestRevision
		return p.bindDecision(ctx, baseline, decision)
	}
	if err = p.bindDecision(ctx, baseline, initialDecision); err != nil {
		return nil, err
	}

	contextSink, e := memorycleanup.NewContexts(p.snapshots)
	if e != nil {
		return nil, e
	}
	sourceHash := sha256.Sum256([]byte("action-context-source-cleanup:context.db:v1"))
	sourceConsumer, e := memory.NewSourceConsumer(p.store, p.store, contextSink, memory.ConsumerConfig{Binding: memory.ConsumerBinding{Namespace: h.namespace, Collection: "personal", Consumer: "action-contexts", ConfigSHA256: fmt.Sprintf("%x", sourceHash)}, Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	artifactSink, e := memorycleanup.NewArtifacts(h.content)
	if e != nil {
		return nil, e
	}
	artifactConsumer, e := memory.NewSourceConsumer(p.store, p.store, artifactSink, memory.ConsumerConfig{Binding: actionArtifactCleanupBinding(h.namespace), Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	admissionSink, e := memorycleanup.NewAdmissions(p.store, h.auth)
	if e != nil {
		return nil, e
	}
	admissionConsumer, e := memory.NewSourceConsumer(p.store, p.store, admissionSink, memory.ConsumerConfig{Binding: actionAdmissionCleanupBinding(h.namespace), Batch: 16, Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	checkpointRef, checkpointRoot := baseline.Task.Ref, a.f.root
	checkpointScope, e := json.Marshal(struct {
		Root string
		Ref  tasks.Ref
	}{checkpointRoot, checkpointRef})
	if e != nil {
		return nil, e
	}
	checkpointHash := sha256.Sum256(append([]byte("action-checkpoint-cleanup:v1\n"), checkpointScope...))
	p.checkpointBinding = memory.ConsumerBinding{Namespace: h.namespace, Collection: "personal", Consumer: fmt.Sprintf("action-checkpoints-%x", checkpointHash[:8]), ConfigSHA256: fmt.Sprintf("%x", checkpointHash)}
	p.checkpoints, e = memorycleanup.NewCheckpoints(p.store, p.store, memorycleanup.CheckpointFiles{
		Maintain: func(ctx context.Context) error {
			return cleanActionCheckpoint(ctx, checkpointRoot, p.snapshots, h.core, checkpointRef)
		},
		Inspect: func(ctx context.Context) (bool, error) {
			return actionCheckpointClean(ctx, checkpointRoot, checkpointRef)
		},
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
		{Name: "legacy-checkpoint", Run: func(ctx context.Context) error {
			_, e := p.checkpoints.Run(ctx)
			return e
		}},
	}, cleanup.Config{Interval: time.Second, Timeout: 5 * time.Second})
	if err != nil {
		return nil, err
	}

	return p, nil
}

type actionBaseContext struct{ host *actionHost }

func (b actionBaseContext) Assemble(ctx context.Context, t tasks.Task, location string, max int) (brain.Input, error) {
	return b.host.baseAssemble(ctx, t, location, max)
}
func (b actionBaseContext) Validate(ctx context.Context, t tasks.Task, location string) error {
	return b.host.baseValidate(ctx, t, location)
}
