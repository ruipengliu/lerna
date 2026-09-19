package catalogcheck

import (
	"context"
	"encoding/json"
	"fmt"
	catalogauth "lerna/adapters/catalog/auth"
	cataloglocal "lerna/adapters/catalog/local"
	sqlitecatalog "lerna/adapters/catalog/sqlite"
	contentpolicy "lerna/adapters/content/policy"
	executionrouter "lerna/adapters/execution/router"
	"lerna/adapters/execution/simworkflow"
	"lerna/authorization"
	"lerna/catalog"
	"lerna/sdk"
	"os"
	"path/filepath"
	"time"
)

// runtimes is an authenticated finite host assembly, independent of retrieval.
// At most one business namespace is resident. Eviction only closes handles;
// credentials, tasks, grants, inputs, invocation journals and targets persist.
// The reference profile serializes calls; this manager is intentionally not a
// network multi-tenant host and must not be shared by concurrent callers.
type runtimes struct {
	root    string
	defs    map[string]simworkflow.Definition
	current *harness
}

func (m *runtimes) Resolve(ctx context.Context, ns string) ([]executionrouter.Bound, error) {
	def, ok := m.defs[ns]
	if !ok {
		return nil, &authorization.Error{Code: authorization.Denied}
	}
	if m.current != nil && m.current.namespace == ns {
		return m.current.Resolve(ctx, ns)
	}
	if m.current != nil {
		m.current.close()
		m.current = nil
	}
	root := filepath.Join(m.root, ns)
	if e := os.MkdirAll(root, 0700); e != nil {
		return nil, e
	}
	tokenPath := filepath.Join(root, "session")
	raw, e := os.ReadFile(tokenPath)
	if e != nil && !os.IsNotExist(e) {
		return nil, e
	}
	h, e := open(ctx, root, string(raw), def)
	if e != nil {
		return nil, e
	}
	if len(raw) == 0 {
		if e = os.WriteFile(tokenPath, []byte(h.token), 0600); e != nil {
			h.close()
			return nil, e
		}
	}
	m.current = h
	return h.Resolve(ctx, ns)
}

type fixture struct {
	retain     bool
	dataPolicy *contentpolicy.Policy
	root       string
	store      *sqlitecatalog.Store
	host       *harness
	manager    *runtimes
	entries    []catalog.Entry
	defs       []simworkflow.Definition
	client     *sdk.CapabilityClient
	transport  *cataloglocal.Binding
}

func newFixture(ctx context.Context) (*fixture, error) { return newFixtureAt(ctx, "") }
func newFixtureAt(ctx context.Context, root string) (f *fixture, err error) {
	var e error
	if root == "" {
		root, e = os.MkdirTemp("", "catalog-profile-")
	} else {
		e = os.MkdirAll(root, 0700)
	}
	if e != nil {
		return nil, e
	}
	f = &fixture{root: root}
	cleanup := f
	defer func() {
		if err != nil {
			cleanup.close()
		}
	}()
	f.defs, e = simworkflow.Definitions()
	if e != nil {
		return nil, e
	}
	f.manager = &runtimes{root: root, defs: map[string]simworkflow.Definition{}}
	for _, def := range f.defs {
		f.entries = append(f.entries, def.Entries()...)
		f.manager.defs[def.Kind] = def
	}
	check, e := simworkflow.ImplementationCheck()
	if e != nil {
		return nil, e
	}
	f.store, e = sqlitecatalog.Open(filepath.Join(root, "catalog.db"), check)
	if e != nil {
		return nil, e
	}
	if _, e = f.store.Replace(ctx, 0, f.entries); e != nil {
		return nil, e
	}
	for {
		done, e := f.store.Rebuild(ctx, 64)
		if e != nil {
			return nil, e
		}
		if done {
			break
		}
	}
	def := f.defs[0]
	def.Kind = "local"
	hostRoot := filepath.Join(root, "discovery")
	if e = os.MkdirAll(hostRoot, 0700); e != nil {
		return nil, e
	}
	f.host, e = open(ctx, hostRoot, "", def)
	if e != nil {
		return nil, e
	}
	f.dataPolicy, e = contentpolicy.New([]contentpolicy.Rule{{Kind: "catalog", Key: "business-manifest", Revision: 1, Actions: []string{"process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: f.host.now().Add(time.Hour).Unix()}, {Kind: "query", Key: "host-session", Revision: 1, Actions: []string{"process"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: f.host.now().Add(time.Hour).Unix()}})
	if e != nil {
		return nil, e
	}
	service, e := catalog.New(f.store, f.discoveryAuth(), catalog.Config{MaxScan: 2048, MaxPage: 64, MaxCandidates: 32, Timeout: time.Second}, catalog.QueryContext{Namespace: "local", Subject: "operator", Token: f.host.token, Purpose: "task", Location: "local"})
	if e != nil {
		return nil, e
	}
	router := executionrouter.Router{Resolver: f.manager, Store: f.store}
	f.transport = cataloglocal.Bind(service, "local", router)
	f.client = sdk.NewCapabilityClient(f.transport, "local")
	return f, nil
}
func (f *fixture) close() {
	if f == nil {
		return
	}
	if f.manager != nil && f.manager.current != nil {
		f.manager.current.close()
	}
	if f.host != nil {
		f.host.close()
	}
	if f.store != nil {
		f.store.Close()
	}
	if !f.retain {
		os.RemoveAll(f.root)
	}
}
func (f *fixture) call(ctx context.Context, index int) error {
	entry := f.entries[index]
	def := f.defs[index/6]
	action := simworkflow.Actions[index%6]
	// Public query contains business text and constraints, not an expected ref.
	page, e := f.client.Search(ctx, catalog.Query{Text: action + " " + def.Title, Category: def.Category, ResourceType: def.Kind, Purpose: "task", Location: "local", Limit: 6, Budget: 2048})
	if e != nil {
		return fmt.Errorf("search: %w", e)
	}
	if page.Coverage != "COMPLETE" || page.Scanned != 1008 || page.SchemaLoads != 0 || len(page.Items) != 6 || page.Items[0].Ref != entry.Ref {
		return fmt.Errorf("candidate coverage/order: %+v", page)
	}
	described, e := f.client.Describe(ctx, page.Items[0].Ref)
	if e != nil {
		return e
	}
	if described.Ref != entry.Ref {
		return fmt.Errorf("exact version mismatch")
	}
	if _, e = f.manager.Resolve(ctx, entry.Ref.Namespace); e != nil {
		return e
	}
	h := f.manager.current
	h.selectEntry(described)
	id := "case_" + action
	// Expected state/accounting is authored separately from driver transition code.
	before := []string{"", "draft", "submitted", "submitted", "authorized", "rejected"}[index%6]
	expectedState := []string{"draft", "submitted", "authorized", "rejected", "withdrawn", "archived"}[index%6]
	if before != "" {
		if e = h.target.Seed(ctx, id, before, 3, 10, true); e != nil {
			return e
		}
	}
	input := map[string]any{"record": id}
	if action == "draft" {
		input[def.Quantity] = 3
		input[def.Limit] = 10
		input[def.Verified] = true
	}
	data, _ := json.Marshal(input)
	r, material, e := h.request(ctx, data, 1)
	if e != nil {
		return fmt.Errorf("prepare: %w", e)
	}
	client := sdk.NewCapabilityClient(f.transport, entry.Ref.Namespace)
	receipt, e := client.Invoke(ctx, r, material)
	if e != nil {
		return fmt.Errorf("invoke: %w", e)
	}
	out, e := h.exec.Run(ctx, r.OperationID)
	if e != nil {
		return fmt.Errorf("run: %w", e)
	}
	if out.Result != "SUCCESS" || out.Effect != "CONFIRMED" {
		return fmt.Errorf("unconfirmed result: %+v", out)
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return fmt.Errorf("drain: %w", e)
	}
	snap, e := h.target.Snapshot(ctx, id)
	if e != nil {
		return e
	}
	// Full sequential coverage authorize and withdraw cancel accounting effects;
	// isolated calls use the persisted ledger before this operation.
	expectedLedger := int64(1000)
	if action == "authorize" || action == "reject" {
		if def.Mode == "reserve" {
			expectedLedger = 997
		} else {
			expectedLedger = 1003
		}
	}
	if snap.State != expectedState || snap.Version != 2 || snap.Ledger != expectedLedger {
		return fmt.Errorf("business result: %+v; expected %s ledger %d", snap, expectedState, expectedLedger)
	}
	replay, e := client.Invoke(ctx, r, "")
	if e != nil || replay != receipt {
		return fmt.Errorf("replay: %v", e)
	}
	saved, e := client.GetInvocation(ctx, r.OperationID)
	if e != nil || saved.Request.DescriptorSHA256 != entry.Ref.Digest {
		return fmt.Errorf("routed snapshot: %v", e)
	}
	return nil
}

func (f *fixture) discoveryAuth() catalogauth.Adapter {
	return catalogauth.Adapter{Authority: f.host.auth, Policy: f.dataPolicy, Clock: f.host.clock, QuerySource: catalog.Source{Kind: "query", Key: "host-session", Revision: 1}}
}
