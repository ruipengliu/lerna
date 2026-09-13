package memoryauth_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/base64"
	"google.golang.org/protobuf/proto"
	"lerna/adapters/josegrant"
	"lerna/adapters/memoryauth"
	"lerna/adapters/memorylocal"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/sqlitememory"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"lerna/sdk"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{}

func (clock) Now() (time.Time, error) { return time.Unix(1900000000, 0), nil }

type source struct{ unavailable bool }

func (p source) Check(_ context.Context, s *wire.ContentSource, action string, _ string, location string, _ int64) error {
	if p.unavailable && action == "process" {
		return memory.Unavailable
	}
	if s.Key != "input-1" || s.Revision != 1 || location != "device-a" {
		return memory.Denied
	}
	return nil
}
func TestCurrentHarnessPolicyAndResidencyConstrainMemory(t *testing.T) {
	ctx := context.Background()
	db, e := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	auth, e := authorization.New(db, clock{}, authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	token, e := auth.Bootstrap(ctx, "local", "alice")
	if e != nil {
		t.Fatal(e)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"content.store", "content.process", "content.discover", "content.disclose", "content.retain", "content.delete", "memory.put", "memory.store", "memory.store_reference", "memory.discover", "memory.process", "memory.disclose", "memory.query", "memory.get", "memory.correct", "memory.delete"}, Purposes: []string{"assist"}, Locations: []string{"device-a"}, ExpiresUnix: 1900003600}
	change := func(c *wire.AuthorizationCommand) {
		t.Helper()
		op, e := auth.NewOperation(ctx, token)
		if e != nil {
			t.Fatal(e)
		}
		snap, e := db.Load(ctx)
		if e != nil {
			t.Fatal(e)
		}
		c.ExpectedRevision = snap.State.Revision
		if _, e = auth.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: op, Command: c}); e != nil {
			t.Fatal(e)
		}
	}
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "memory", Scope: scope}}}}})
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "memory", Subject: "alice", Scope: scope, Mode: "continuous"}}})
	document := []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:memory:text","type":"object","properties":{"text":{"type":"string"}},"required":["text"],"additionalProperties":false}`)
	schemas, e := schema.New([]schema.Resource{{Type: "text", ID: "urn:memory:text", Version: "1", Document: document}})
	if e != nil {
		t.Fatal(e)
	}
	policy := memoryauth.Collection{MissingMetadataUntil: 1900001000, Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "private", Purpose: "assist", Storage: []string{"device-a"}, ReferenceStorage: []string{"device-a"}, Processing: []string{"device-a"}, Recipients: []string{"device-a"}, Schemas: map[string]string{"text": schema.Digest(document)}}
	adapter, e := memoryauth.New(auth, source{}, []memoryauth.Collection{policy})
	if e != nil {
		t.Fatal(e)
	}
	b := memory.Binding{Token: token, Namespace: "local", Subject: "alice", Location: "device-a", Recipient: "device-a"}
	ref := &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "preference"}
	spec := &wire.MemorySpec{PolicyRef: "private", Purpose: "assist", Content: &wire.DynamicPayload{TypeName: "text", SchemaDigest: schema.Digest(document)}, Sources: []*wire.MemorySource{{Ref: &wire.ContentSource{Kind: "user", Key: "input-1", Revision: 1}}}, RetainUntil: 1900001000}
	if e = adapter.Check(ctx, b, ref, spec, "put"); e != nil {
		t.Fatal(e)
	}
	b.Location = "cloud"
	if e = adapter.Check(ctx, b, ref, spec, "put"); e != memory.Denied {
		t.Fatalf("cloud write %v", e)
	}
	b.Location = "device-a"
	spec.PolicyRef = "public"
	if e = adapter.Check(ctx, b, ref, spec, "put"); e != memory.Denied {
		t.Fatalf("policy override %v", e)
	}
	spec.PolicyRef = "private"
	spec.Sources[0].Ref.Revision = 2
	if e = adapter.Check(ctx, b, ref, spec, "put"); e != memory.Denied {
		t.Fatalf("invalid source %v", e)
	}
	spec.Sources[0].Ref.Revision = 1
	offline, e := memoryauth.New(auth, source{unavailable: true}, []memoryauth.Collection{policy})
	if e != nil {
		t.Fatal(e)
	}
	if e = offline.Check(ctx, b, ref, spec, "discover"); e != nil {
		t.Fatalf("visible source metadata %v", e)
	}
	if e = offline.Check(ctx, b, ref, spec, "read"); e != memory.Unavailable {
		t.Fatalf("known unavailable source %v", e)
	}
	// Configuration ownership stays with the adapter after construction.
	policy.Storage[0] = "cloud"
	policy.Schemas["text"] = "changed"
	if e = adapter.Check(ctx, b, ref, spec, "put"); e != nil {
		t.Fatalf("caller mutated adapter config %v", e)
	}
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	crypto, e := josegrant.New("signer", key, map[string]*ecdsa.PublicKey{"signer": &key.PublicKey})
	if e != nil {
		t.Fatal(e)
	}
	grants, e := auth.SignedGrants(authorization.GrantConfig{Issuer: "local", MaxTTL: time.Hour, IOTimeout: time.Second, MaxDepth: 4, MaxRecords: 32}, crypto)
	if e != nil {
		t.Fatal(e)
	}
	peer := memoryauth.Peer{Audience: "memory", Presenter: "device-a", CertificateSHA256: base64.RawURLEncoding.EncodeToString(make([]byte, 32))}
	permits, e := memoryauth.NewReadPermits(adapter, grants, peer)
	if e != nil {
		t.Fatal(e)
	}
	readID, e := auth.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	memoryStore, e := sqlitememory.Open(filepath.Join(t.TempDir(), "memory.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer memoryStore.Close()
	service, e := memory.New(memoryStore, adapter, schemas, clock{}, memory.Config{Location: "device-a", Timeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	t.Run("missing-metadata", func(t *testing.T) {
		missing := &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "not-recorded"}
		if err := service.ValidateMissing(ctx, b, missing, "assist", "store", "device-a", 1900000500, adapter); err != nil {
			t.Fatal(err)
		}

		disabled := policy
		disabled.MissingMetadataUntil = 0
		closed, err := memoryauth.New(auth, source{}, []memoryauth.Collection{disabled})
		if err != nil {
			t.Fatal(err)
		}
		if err = service.ValidateMissing(ctx, b, missing, "assist", "store", "device-a", 1900000500, closed); err != memory.Denied {
			t.Fatalf("metadata enabled implicitly: %v", err)
		}
		if err = auth.UpdateContent(ctx, func(tx authorization.ContentTransaction) error {
			return service.ValidateMissing(ctx, b, missing, "assist", "disclose", "device-a", 1900000500, adapter.ReadPolicy(tx).(memory.MissingChecker))
		}); err != nil {
			t.Fatalf("missing metadata in outer authorization snapshot: %v", err)
		}
		for _, tc := range []struct {
			purpose, location string
			until             int64
		}{{"other", "device-a", 1900000500}, {"assist", "cloud", 1900000500}, {"assist", "device-a", 1900002000}, {"assist", "device-a", 1900000000}} {
			if err := service.ValidateMissing(ctx, b, missing, tc.purpose, "store", tc.location, tc.until, adapter); err != memory.Denied {
				t.Fatalf("missing metadata restriction: %v", err)
			}
		}
	})
	spec.Kind = "preference"
	spec.About = "alice"
	spec.Conditions = "when answering"
	spec.RecordedAt = 1900000000
	spec.Confidence = &wire.MemoryConfidence{Assessment: "explicit", Basis: "user input", Method: "declaration"}
	spec.Sources[0].Method = "explicit"
	spec.Content.SchemaId = "urn:memory:text"
	spec.Content.SchemaVersion = "1"
	spec.Content.Json = []byte(`{"text":"concise"}`)
	memoryOp, e := auth.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	reader, e := memory.NewReader(service, permits)
	if e != nil {
		t.Fatal(e)
	}
	client := sdk.NewMemoryClient(memorylocal.Bind(service, reader, b), b.Namespace)
	if _, e = client.Exchange(ctx, &wire.MemoryRequest{Method: "PUT", Write: &wire.MemoryWrite{OperationId: memoryOp, Ref: ref, Spec: spec}}); e != nil {
		t.Fatal(e)
	}

	if err := service.ValidateMissing(ctx, b, ref, "assist", "store", "device-a", 1900000500, adapter); err != memory.ContextInvalidated {
		t.Fatalf("existing record declared missing: %v", err)
	}

	if e = auth.UpdateContent(ctx, func(tx authorization.ContentTransaction) error {
		return service.ValidateDerived(ctx, b, ref, 1, "assist", "store", "device-a", 1900000500, adapter.ReadPolicy(tx))
	}); e != nil {
		t.Fatalf("memory source in content transaction %v", e)
	}
	if e = auth.UpdateContent(ctx, func(tx authorization.ContentTransaction) error {
		return service.ValidateDerived(ctx, b, ref, 1, "assist", "store", "device-a", 1900002000, adapter.ReadPolicy(tx))
	}); e != memory.Denied {
		t.Fatalf("derived retention expansion %v", e)
	}
	if e = service.ValidateRetention(ctx, b, ref, 1, "assist", "device-a"); e != nil {
		t.Fatalf("retention %v", e)
	}
	if e = service.ValidateRetention(ctx, b, ref, 1, "assist", "cloud"); e != memory.Denied {
		t.Fatalf("cloud snapshot %v", e)
	}
	readOnly := proto.Clone(scope).(*wire.AuthorizationScope)
	readOnly.Actions = []string{"memory.store_reference", "memory.discover", "memory.process", "memory.disclose", "memory.query", "memory.correct"}
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "memory", Scope: readOnly}}}}})
	if e = service.ValidateCurrent(ctx, b, ref, 1, "assist"); e != nil {
		t.Fatalf("read remains authorized %v", e)
	}
	if e = service.ValidateReferenceStorage(ctx, b, ref, 1, "assist", "device-a"); e != nil {
		t.Fatalf("reference storage %v", e)
	}
	if e = service.ValidateReferenceStorage(ctx, b, ref, 1, "assist", "cloud"); e != memory.Denied {
		t.Fatalf("hidden reference export %v", e)
	}
	if e = service.ValidateRetention(ctx, b, ref, 1, "assist", "device-a"); e != memory.Denied {
		t.Fatalf("revoked storage %v", e)
	}
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "memory", Scope: scope}}}}})
	checkDerivedContent(t, service, adapter, auth, db, b, scope, ref)
	checkContextIntegration(t, service, reader, permits, auth, grants, db, b, scope, ref, peer)
	query := &wire.MemoryQuery{ReadId: readID, Collection: "personal", Purpose: "assist", Text: "concise", MaxResults: 4, MaxBytes: 8000}
	intent, e := memory.DescribeQuery(b, query)
	if e != nil {
		t.Fatal(e)
	}
	op, e := auth.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := db.Load(ctx)
	if e != nil {
		t.Fatal(e)
	}
	grant, e := grants.Mutate(ctx, token, &wire.GrantMutation{OperationId: op, ExpectedRevision: snapshot.State.Revision, Kind: "ISSUE", Spec: &wire.SignedGrantSpec{Subject: "alice", Audience: peer.Audience, Presenter: peer.Presenter, CertificateSha256: peer.CertificateSHA256, Scope: scope, NotBefore: 1900000000, Units: 1, Mode: "single", OperationBinding: readID, SemanticSha256: intent.SemanticSHA256}})
	if e != nil {
		t.Fatal(e)
	}
	permit, e := permits.Reserve(ctx, b, intent, grant.Material)
	if e != nil {
		t.Fatal(e)
	}
	repeated, e := permits.Reserve(ctx, b, intent, grant.Material)
	if e != nil || repeated != permit {
		t.Fatalf("repeat permit %v", e)
	}
	used, e := grants.Get(ctx, token, grant.GrantId)
	if e != nil || used.Allocated != 1 {
		t.Fatalf("allocation %+v %v", used, e)
	}
	if e = permits.Validate(ctx, b, intent, grant.Material, permit); e != nil {
		t.Fatal(e)
	}
	response, e := client.Exchange(ctx, &wire.MemoryRequest{Method: "QUERY", Query: query, GrantMaterial: grant.Material})
	if e != nil || response.Code != "OK" || len(response.GetResult().GetRecords()) != 1 {
		t.Fatalf("SDK query %+v %v", response, e)
	}
	operation, e := client.Exchange(ctx, &wire.MemoryRequest{Method: "LOOKUP", OperationId: memoryOp})
	if e != nil || operation.GetOperation().GetState() != "committed" {
		t.Fatalf("SDK lookup %+v %v", operation, e)
	}
	result, e := reader.Query(ctx, b, query, grant.Material)
	if e != nil || len(result.Records) != 1 || result.Records[0].Revision != 1 {
		t.Fatalf("signed query %+v %v", result, e)
	}
	correctOp, e := auth.NewOperation(ctx, token)
	if e != nil {
		t.Fatal(e)
	}
	changed := proto.Clone(spec).(*wire.MemorySpec)
	changed.Content.Json = []byte(`{"text":"detailed"}`)
	corrected, e := client.Exchange(ctx, &wire.MemoryRequest{Method: "CORRECT", Write: &wire.MemoryWrite{OperationId: correctOp, Ref: ref, ExpectedRevision: 1, Spec: changed}})
	if e != nil || corrected.GetReceipt().GetRevision() != 2 {
		t.Fatalf("SDK correct %+v %v", corrected, e)
	}
	if e = auth.UpdateContent(ctx, func(tx authorization.ContentTransaction) error {
		return service.ValidateDerived(ctx, b, ref, 1, "assist", "store", "device-a", 1900000500, adapter.ReadPolicy(tx))
	}); e != memory.ContextInvalidated {
		t.Fatalf("derived output accepted corrected source: %v", e)
	}
	repeatedWire, e := client.Exchange(ctx, &wire.MemoryRequest{Method: "QUERY", Query: query, GrantMaterial: grant.Material})
	if e != nil || len(repeatedWire.GetResult().GetRecords()) != 1 || repeatedWire.Result.Records[0].Revision != 1 {
		t.Fatalf("SDK original result %+v %v", repeatedWire, e)
	}
	repeatedResult, e := reader.Query(ctx, b, query, grant.Material)
	if e != nil || len(repeatedResult.Records) != 1 {
		t.Fatalf("signed repeat %+v %v", repeatedResult, e)
	}
	used, e = grants.Get(ctx, token, grant.GrantId)
	if e != nil || used.Allocated != 1 {
		t.Fatalf("query allocation %+v %v", used, e)
	}
	altered := intent
	altered.SemanticSHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	if _, e = permits.Reserve(ctx, b, altered, grant.Material); e != memory.Denied {
		t.Fatalf("changed intent %v", e)
	}
	reporter, e := memory.NewDeletionReporter(memoryStore, memoryStore, nil)
	if e != nil {
		t.Fatal(e)
	}
	statusService, e := service.WithDeletionReporter(reporter)
	if e != nil {
		t.Fatal(e)
	}
	checkGovernedDeletion(t, statusService, reader, auth, b, spec, scope, change, reporter)
	checkDeletionConsumer(t, service, memoryStore, adapter, auth, b, spec)
	checkMissingContext(t, service, reader, permits, adapter, auth, grants, db, b, scope, peer, spec)
	change(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "denied", Scope: scope, Deny: true}}}}})
	for _, key := range []string{"preference", "not-recorded"} {
		missing := &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: key}
		if err := service.ValidateMissing(ctx, b, missing, "assist", "discover", "device-a", 1900000500, adapter); err != memory.Denied {
			t.Fatalf("revoked metadata disclosed existence: %v", err)
		}
	}

	if _, e = reader.Query(ctx, b, query, grant.Material); e != memory.Denied {
		t.Fatalf("revoked query %v", e)
	}
	if e = permits.Validate(ctx, b, intent, grant.Material, permit); e != memory.Denied {
		t.Fatalf("revoked disclosure %v", e)
	}

	if e = adapter.Check(ctx, b, ref, spec, "put"); e != memory.Denied {
		t.Fatalf("revoked %v", e)
	}
}
