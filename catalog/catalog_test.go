package catalog_test

import (
	"context"
	"lerna/adapters/sqlitecatalog"
	"lerna/catalog"
	"lerna/execution"
	"lerna/schema"
	"path/filepath"
	"testing"
	"time"
)

type access struct{}

func (access) View(_ context.Context, _ catalog.QueryContext, policies []catalog.Policy) (catalog.Access, error) {
	allowed := make([]bool, len(policies))
	for i, p := range policies {
		allowed[i] = p.Resource != "private"
	}
	return catalog.Access{Namespace: "local", Subject: "operator", Revision: "view1", Allowed: allowed}, nil
}
func example() catalog.Entry {
	return catalog.Entry{Source: catalog.Source{Kind: "catalog", Key: "example", Revision: 1}, Ref: catalog.Ref{Namespace: "orders", Name: "order.submit", Version: "1", Implementation: "sim-order", ImplementationVersion: "1"}, Title: "Submit a purchase order", Category: "procurement", Aliases: []string{"send purchase request"}, Purpose: "task", Location: "local", Resource: "root", ResourceType: "purchase-order", Preconditions: "A draft order with a positive approved budget", Effects: "The order is submitted for review", Unsupported: "Already submitted orders", Guarantees: "Original operation lookup; no hidden retries", Available: true, Capability: execution.Capability{Name: "order.submit", Version: "1", Implementation: "sim-order", ImplementationVersion: "1", Resource: "root", Purpose: "task", Location: "local", Exclusive: true, Synchronous: true, Input: schema.Resource{Type: "order.input", ID: "urn:order:input", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:order:input","type":"object"}`)}, Output: schema.Resource{Type: "order.output", ID: "urn:order:output", Version: "1", Document: []byte(`{"$schema":"https://json-schema.org/draft/2020-12/schema","$id":"urn:order:output","type":"object"}`)}}}
}
func TestListThenDescribeExactVersion(t *testing.T) {
	ctx := context.Background()
	store, e := sqlitecatalog.Open(filepath.Join(t.TempDir(), "catalog.db"), func(catalog.Entry) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	defer store.Close()
	entry := example()
	entry.Ref.Digest = entry.Capability.Digest()
	if _, e = store.Replace(ctx, 0, []catalog.Entry{entry}); e != nil {
		t.Fatal(e)
	}
	service, e := catalog.New(store, access{}, catalog.Config{MaxScan: 2048, MaxPage: 32, MaxCandidates: 16, Timeout: time.Second}, catalog.QueryContext{Namespace: "local", Subject: "operator", Token: "trusted", Purpose: "task", Location: "local"})
	if e != nil {
		t.Fatal(e)
	}
	page, e := service.List(ctx, catalog.Query{Limit: 4, Budget: 32, Purpose: "task", Location: "local"})
	if e != nil || len(page.Items) != 1 {
		t.Fatal(page, e)
	}
	full, e := service.Describe(ctx, page.Items[0].Ref)
	if e != nil || full.Capability.Digest() != entry.Ref.Digest {
		t.Fatal(full, e)
	}
	wrong := entry.Ref
	wrong.Version = "2"
	if _, e = service.Describe(ctx, wrong); e == nil {
		t.Fatal("silently replaced requested version")
	}
}

func TestHiddenEntriesDoNotConsumeCandidateBudget(t *testing.T) {
	ctx := context.Background()
	st, e := sqlitecatalog.Open(filepath.Join(t.TempDir(), "catalog.db"), func(catalog.Entry) error { return nil })
	if e != nil {
		t.Fatal(e)
	}
	defer st.Close()
	public := example()
	public.Ref.Digest = public.Capability.Digest()
	hidden := example()
	hidden.Ref.Namespace = "aaa-private"
	hidden.DiscoveryResource = "private"
	hidden.Title = "Secret project"
	hidden.Ref.Digest = hidden.Capability.Digest()
	if _, e = st.Replace(ctx, 0, []catalog.Entry{public, hidden}); e != nil {
		t.Fatal(e)
	}
	svc, e := catalog.New(st, access{}, catalog.Config{MaxScan: 2048, MaxPage: 32, MaxCandidates: 16, Timeout: time.Second}, catalog.QueryContext{Namespace: "local", Subject: "operator", Token: "trusted", Purpose: "task", Location: "local"})
	if e != nil {
		t.Fatal(e)
	}
	p, e := svc.Search(ctx, catalog.Query{Text: "purchase", Limit: 1, Budget: 1, Purpose: "task", Location: "local"})
	if e != nil || len(p.Items) != 1 || p.Coverage != "COMPLETE" || p.Scanned != 1 {
		t.Fatal(p, e)
	}
}
