package content

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"google.golang.org/protobuf/proto"
	sqliteauth "lerna/adapters/authorization/sqlite"
	filecontent "lerna/adapters/content/file"
	contentlocal "lerna/adapters/content/local"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/artifacts"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type clock struct {
	mu sync.Mutex
	at time.Time
}

func (c *clock) Now() (time.Time, error) { c.mu.Lock(); defer c.mu.Unlock(); return c.at, nil }
func (c *clock) advance(d time.Duration) { c.mu.Lock(); c.at = c.at.Add(d); c.mu.Unlock() }

type harness struct {
	root, token string
	store       *sqliteauth.Store
	auth        *authorization.Service
	blob        *filecontent.Store
	service     *artifacts.Service
	client      *sdk.ContentClient
	policy      *contentpolicy.Policy
	clock       *clock
	config      artifacts.Config
	binding     artifacts.Binding
	rule        contentpolicy.Rule
}

func config() artifacts.Config {
	return artifacts.Config{Inline: 16, MaxObject: 1 << 20, MaxTotal: 2 << 20, MaxRecords: 32, MaxChunk: 65536, MaxFiles: 64, CleanupBatch: 16, Timeout: time.Second, Retention: time.Hour}
}
func authConfig() authorization.Config {
	return authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second}
}
func open(root, token string) (*harness, error) {
	h := &harness{root: root, token: token, clock: &clock{at: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}, config: config()}
	var err error
	h.store, err = sqliteauth.Open(filepath.Join(root, "auth.db"))
	if err != nil {
		return nil, err
	}
	h.auth, err = authorization.New(h.store, h.clock, authConfig())
	if err != nil {
		h.store.Close()
		return nil, err
	}
	h.blob, err = filecontent.Open(filepath.Join(root, "content"))
	if err != nil {
		h.store.Close()
		return nil, err
	}
	now, _ := h.clock.Now()
	h.rule = contentpolicy.Rule{Kind: "input", Key: "source", Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"research"}, Locations: []string{"device"}, RetainUntil: now.Add(time.Hour).Unix()}
	h.policy, err = contentpolicy.New([]contentpolicy.Rule{h.rule})
	if err != nil {
		h.close()
		return nil, err
	}
	h.binding = artifacts.Binding{Token: token, Namespace: "local", Location: "device", Recipient: "device"}
	if err = h.bind(h.auth, h.blob); err != nil {
		h.close()
		return nil, err
	}
	return h, nil
}
func (h *harness) bind(a artifacts.Authority, b artifacts.Blobs) error {
	var err error
	h.service, err = artifacts.New(a, b, h.policy, h.config)
	h.client = sdk.NewContentClient(contentlocal.Bind(h.service, h.binding), "local")
	return err
}
func fresh(ctx context.Context) (*harness, error) {
	root, err := os.MkdirTemp("", "content-profile-")
	if err != nil {
		return nil, err
	}
	h, err := open(root, "")
	if err != nil {
		os.RemoveAll(root)
		return nil, err
	}
	h.token, err = h.auth.Bootstrap(ctx, "local", "admin")
	if err != nil {
		h.close()
		os.RemoveAll(root)
		return nil, err
	}
	h.binding.Token = h.token
	h.bind(h.auth, h.blob)
	now, _ := h.clock.Now()
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"content.store", "content.process", "content.retain", "content.discover", "content.disclose", "content.delete"}, Purposes: []string{"research", "other"}, Locations: []string{"device", "cloud"}, ExpiresUnix: now.Add(time.Hour).Unix()}
	cmds := []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "content", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "content", Subject: "admin", Mode: "continuous", Scope: scope}}}}
	for i, c := range cmds {
		c.ExpectedRevision = uint64(i)
		if err = h.mutate(ctx, c); err != nil {
			h.destroy()
			return nil, err
		}
	}
	return h, nil
}
func (h *harness) mutate(ctx context.Context, c *wire.AuthorizationCommand) error {
	op, err := h.auth.NewOperation(ctx, h.token)
	if err != nil {
		return err
	}
	_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: c})
	return err
}
func (h *harness) close()   { h.store.Close(); h.blob.Close() }
func (h *harness) destroy() { h.close(); os.RemoveAll(h.root) }
func (h *harness) request(ctx context.Context, data []byte) (*wire.ContentRequest, error) {
	op, err := h.auth.NewOperation(ctx, h.token)
	if err != nil {
		return nil, err
	}
	now, _ := h.clock.Now()
	sum := sha256.Sum256(data)
	return &wire.ContentRequest{Method: "PUT", OperationId: op, Spec: &wire.ContentSpec{Kind: "evidence", Resource: "root", Purpose: "research", Sources: []*wire.ContentSource{{Kind: "input", Key: "source", Revision: 1}}, AcquiredAt: now.Unix(), MediaType: "text/plain", Size: uint64(len(data)), Sha256: hex.EncodeToString(sum[:]), RetainUntil: now.Add(time.Minute * 5).Unix()}, Data: data}, nil
}
func (h *harness) put(ctx context.Context, data []byte) (*wire.ContentResponse, *wire.ContentRequest, error) {
	in, err := h.request(ctx, data)
	if err != nil {
		return nil, nil, err
	}
	out, err := h.client.Call(ctx, in)
	return out, in, err
}
func (h *harness) read(ctx context.Context, r *wire.ContentRef, n uint32) (*wire.ContentResponse, error) {
	return h.client.Call(ctx, &wire.ContentRequest{Method: "READ", Ref: r, Purpose: "research", Limit: n})
}
func (h *harness) delete(ctx context.Context, r *wire.ContentRef) (*wire.ContentRequest, error) {
	op, err := h.auth.NewOperation(ctx, h.token)
	if err != nil {
		return nil, err
	}
	in := &wire.ContentRequest{Method: "DELETE", OperationId: op, Ref: proto.Clone(r).(*wire.ContentRef), ExpectedRevision: 1, Purpose: "research"}
	_, err = h.client.Call(ctx, in)
	return in, err
}
func demand(ok bool) error {
	if !ok {
		return artifacts.Error("CHECK_FAILED")
	}
	return nil
}
