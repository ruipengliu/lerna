package memorycheck

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"lerna/adapters/authorization/josegrant"
	sqliteauth "lerna/adapters/authorization/sqlite"
	contentpolicy "lerna/adapters/content/policy"
	memoryauth "lerna/adapters/memory/auth"
	memorylocal "lerna/adapters/memory/local"
	sqlitememory "lerna/adapters/memory/sqlite"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"lerna/sdk"
	"os"
	"path/filepath"
	"time"
)

var document = []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:memory:profile:text","type":"object","properties":{"text":{"type":"string","maxLength":512}},"required":["text"],"additionalProperties":false}`)

type clock struct{}

func (clock) Now() (time.Time, error) { return time.Unix(1900000000, 0), nil }

type probeConfig struct{ Root, Token, WriteID, ReadID, Grant string }
type fixture struct {
	config    probeConfig
	authDB    *sqliteauth.Store
	auth      *authorization.Service
	grants    *authorization.GrantAuthority
	store     *sqlitememory.Store
	authority *memoryauth.Authority
	permits   *memoryauth.ReadPermits
	schemas   *schema.Registry
	binding   memory.Binding
	client    *sdk.MemoryClient
}

func (f *fixture) close() {
	if f.store != nil {
		f.store.Close()
	}
	if f.authDB != nil {
		f.authDB.Close()
	}
}
func authConfig() authorization.Config {
	return authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second}
}
func peer() memoryauth.Peer {
	return memoryauth.Peer{Audience: "memory", Presenter: "device-a", CertificateSHA256: base64.RawURLEncoding.EncodeToString(make([]byte, 32))}
}
func scope() *wire.AuthorizationScope {
	return &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"memory.put", "memory.correct", "memory.delete", "memory.store", "memory.process", "memory.discover", "memory.disclose", "memory.query", "memory.get"}, Purposes: []string{"assist"}, Locations: []string{"device-a"}, ExpiresUnix: 1900003600}
}
func openFixture(ctx context.Context, c probeConfig) (f *fixture, err error) {
	f = &fixture{config: c}
	defer func() {
		if err != nil {
			f.close()
		}
	}()
	f.authDB, err = sqliteauth.Open(filepath.Join(c.Root, "auth.db"))
	if err != nil {
		return
	}
	f.auth, err = authorization.New(f.authDB, clock{}, authConfig())
	if err != nil {
		return
	}
	key, e := josegrant.LoadPrivateKey(filepath.Join(c.Root, "signer.pem"))
	if e != nil {
		return f, e
	}
	crypto, e := josegrant.New("memory-signer", key, map[string]*ecdsa.PublicKey{"memory-signer": &key.PublicKey})
	if e != nil {
		return f, e
	}
	f.grants, err = f.auth.SignedGrants(authorization.GrantConfig{Issuer: "local", MaxTTL: time.Hour, IOTimeout: time.Second, MaxDepth: 4, MaxRecords: 64}, crypto)
	if err != nil {
		return
	}
	sources, e := contentpolicy.New([]contentpolicy.Rule{{Kind: "user", Key: "input-1", Revision: 1, Actions: []string{"store", "process", "discover", "disclose"}, Purposes: []string{"assist"}, Locations: []string{"device-a"}, RetainUntil: 1900003600}})
	if e != nil {
		return f, e
	}
	f.authority, err = memoryauth.New(f.auth, sources, []memoryauth.Collection{{Namespace: "local", Name: "personal", Resource: "root", PolicyRef: "private", Purpose: "assist", Storage: []string{"device-a"}, Processing: []string{"device-a"}, Recipients: []string{"device-a"}, Schemas: map[string]string{"text": schema.Digest(document)}}})
	if err != nil {
		return
	}
	f.permits, err = memoryauth.NewReadPermits(f.authority, f.grants, peer())
	if err != nil {
		return
	}
	f.schemas, err = schema.New([]schema.Resource{{Type: "text", ID: "urn:memory:profile:text", Version: "1", Document: document}})
	if err != nil {
		return
	}
	f.store, err = sqlitememory.Open(filepath.Join(c.Root, "memory.db"))
	if err != nil {
		return
	}
	f.binding = memory.Binding{Token: c.Token, Namespace: "local", Subject: "alice", Location: "device-a", Recipient: "device-a"}
	f.client, err = f.bind(f.store, f.permits)
	return
}
func (f *fixture) bind(store memory.Store, permits memory.ReadPermits) (*sdk.MemoryClient, error) {
	service, e := memory.New(store, f.authority, f.schemas, clock{}, memory.Config{Location: "device-a", Timeout: 5 * time.Second})
	if e != nil {
		return nil, e
	}
	reader, e := memory.NewReader(service, permits)
	if e != nil {
		return nil, e
	}
	return sdk.NewMemoryClient(memorylocal.Bind(service, reader, f.binding), "local"), nil
}
func newFixture(ctx context.Context) (f *fixture, err error) {
	root, e := os.MkdirTemp("", "memory-profile-")
	if e != nil {
		return nil, e
	}
	defer func() {
		if err != nil {
			if f != nil {
				f.close()
			}
			os.RemoveAll(root)
		}
	}()
	key, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		return nil, e
	}
	raw, e := x509.MarshalPKCS8PrivateKey(key)
	if e != nil {
		return nil, e
	}
	if e = os.WriteFile(filepath.Join(root, "signer.pem"), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}), 0600); e != nil {
		return nil, e
	}
	f, err = openFixture(ctx, probeConfig{Root: root})
	if err != nil {
		return
	}
	f.config.Token, err = f.auth.Bootstrap(ctx, "local", "alice")
	if err != nil {
		return
	}
	f.binding.Token = f.config.Token
	apply := func(command *wire.AuthorizationCommand) error {
		view, e := f.auth.GetPolicy(ctx, f.config.Token)
		if e != nil {
			return e
		}
		op, e := f.auth.NewOperation(ctx, f.config.Token)
		if e != nil {
			return e
		}
		command.ExpectedRevision = view.Revision
		_, e = f.auth.Execute(ctx, f.config.Token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command})
		return e
	}
	if err = apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "memory", Scope: scope()}}}}}); err != nil {
		return
	}
	if err = apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "memory", Subject: "alice", Scope: scope(), Mode: "continuous"}}}); err != nil {
		return
	}
	f.config.WriteID, err = f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		return
	}
	f.config.ReadID, err = f.auth.NewOperation(ctx, f.config.Token)
	if err != nil {
		return
	}
	intent, e := memory.DescribeQuery(f.binding, f.query())
	if e != nil {
		return f, e
	}
	op, e := f.auth.NewOperation(ctx, f.config.Token)
	if e != nil {
		return f, e
	}
	view, e := f.auth.GetPolicy(ctx, f.config.Token)
	if e != nil {
		return f, e
	}
	p := peer()
	grant, e := f.grants.Mutate(ctx, f.config.Token, &wire.GrantMutation{OperationId: op, Kind: "ISSUE", ExpectedRevision: view.Revision, Spec: &wire.SignedGrantSpec{Subject: "alice", Audience: p.Audience, Presenter: p.Presenter, CertificateSha256: p.CertificateSHA256, Scope: scope(), NotBefore: 1900000000, Units: 1, Mode: "single", OperationBinding: intent.ID, SemanticSha256: intent.SemanticSHA256}})
	if e != nil {
		return f, e
	}
	f.config.Grant = grant.Material
	f.client, err = f.bind(f.store, f.permits)
	if err != nil {
		return
	}
	raw, err = json.Marshal(f.config)
	if err != nil {
		return
	}
	err = os.WriteFile(filepath.Join(root, "probe.json"), raw, 0600)
	return
}
func (f *fixture) write(id string, expected uint64, text string) *wire.MemoryRequest {
	body, _ := json.Marshal(map[string]string{"text": text})
	return &wire.MemoryRequest{Method: "PUT", Write: &wire.MemoryWrite{OperationId: id, Ref: &wire.MemoryRef{Namespace: "local", Collection: "personal", Key: "format"}, ExpectedRevision: expected, Spec: &wire.MemorySpec{Kind: "preference", Content: &wire.DynamicPayload{TypeName: "text", SchemaId: "urn:memory:profile:text", SchemaVersion: "1", SchemaDigest: schema.Digest(document), Json: body}, Sources: []*wire.MemorySource{{Ref: &wire.ContentSource{Kind: "user", Key: "input-1", Revision: 1}, Method: "explicit"}}, About: "alice", Conditions: "when answering", RecordedAt: 1900000000, Confidence: &wire.MemoryConfidence{Assessment: "explicit", Basis: "user statement", Method: "declaration"}, PolicyRef: "private", Purpose: "assist", RetainUntil: 1900001800}}}
}
func (f *fixture) query() *wire.MemoryQuery {
	return &wire.MemoryQuery{ReadId: f.config.ReadID, Collection: "personal", Purpose: "assist", MaxResults: 4, MaxBytes: 8000}
}
func (f *fixture) put(ctx context.Context) error {
	_, e := f.client.Exchange(ctx, f.write(f.config.WriteID, 0, "concise"))
	return e
}
func (f *fixture) correct(ctx context.Context) error {
	id, e := f.auth.NewOperation(ctx, f.config.Token)
	if e != nil {
		return e
	}
	request := f.write(id, 1, "detailed")
	request.Method = "CORRECT"
	_, e = f.client.Exchange(ctx, request)
	return e
}
func (f *fixture) read(ctx context.Context) (*wire.MemoryReadResult, error) {
	out, e := f.client.Exchange(ctx, &wire.MemoryRequest{Method: "QUERY", Query: f.query(), GrantMaterial: f.config.Grant})
	if e != nil {
		return nil, e
	}
	return out.Result, nil
}
func require(ok bool, message string) error {
	if !ok {
		return fmt.Errorf("%s", message)
	}
	return nil
}
