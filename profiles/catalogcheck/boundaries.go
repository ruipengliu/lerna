package catalogcheck

import (
	"context"
	"database/sql"
	"fmt"

	sqlitecatalog "lerna/adapters/catalog/sqlite"
	"lerna/adapters/execution/simworkflow"
	"lerna/authorization"
	"lerna/catalog"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"path/filepath"
	"strings"
	"time"
)

var boundaryNames = []string{"publication-admission-order", "cross-api-takeover", "cross-api-unknown", "capacity", "index-repair", "pagination", "cursor-tamper", "cursor-query", "cursor-subject", "cursor-revision", "cursor-revocation", "describe-denied", "source-unavailable", "index-missing", "index-stale", "index-rebuilding", "index-corrupt", "index-unreachable", "budget", "no-match", "cancelled", "invalid-query", "exact-version", "immutable-version", "admitted-withdrawal", "revocation-during-search", "slow-index", "aliases"}

func must(ok bool, msg string) error {
	if !ok {
		return fmt.Errorf("%s", msg)
	}
	return nil
}
func query() catalog.Query {
	return catalog.Query{Purpose: "task", Location: "local", Limit: 32, Budget: 2048}
}
func (f *fixture) sql(ctx context.Context, q string, args ...any) error {
	db, e := sql.Open("sqlite", filepath.Join(f.root, "catalog.db"))
	if e != nil {
		return e
	}
	defer db.Close()
	_, e = db.ExecContext(ctx, q, args...)
	return e
}
func (f *fixture) revoke(ctx context.Context) error {
	op, e := f.host.operation(ctx)
	if e != nil {
		return e
	}
	snap, e := f.host.db.Load(ctx)
	if e != nil {
		return e
	}
	_, e = f.host.auth.Execute(ctx, f.host.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{ExpectedRevision: snap.State.Revision, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}}})
	return e
}

type hookedStore struct {
	catalog.Store
	index func(context.Context, []catalog.Ref, uint64) (catalog.IndexResult, error)
}

func (s hookedStore) Index(c context.Context, r []catalog.Ref, v uint64) (catalog.IndexResult, error) {
	return s.index(c, r, v)
}
func (f *fixture) service(st catalog.Store, subject string) (*catalog.Service, error) {
	return catalog.New(st, f.discoveryAuth(), catalog.Config{MaxScan: 2048, MaxPage: 64, MaxCandidates: 32, Timeout: time.Second}, catalog.QueryContext{Namespace: "local", Subject: subject, Token: f.host.token, Purpose: "task", Location: "local"})
}
func boundary(ctx context.Context, name string) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	q := query()
	switch name {
	case "publication-admission-order":
		return f.gateRace(ctx)
	case "cross-api-takeover":
		return f.sharedControl(ctx, false)
	case "cross-api-unknown":
		return f.sharedControl(ctx, true)
	case "capacity":
		oversized := make([]catalog.Entry, 2049)
		if _, e = f.store.Replace(ctx, 1, oversized); e == nil {
			return fmt.Errorf("over capacity import accepted")
		}
		p, e := f.client.Search(ctx, q)
		return must(e == nil && p.Revision == 1 && p.Scanned == 1008, "capacity partial import")
	case "index-repair":
		if e = f.sql(ctx, `DROP TABLE catalog_index`); e != nil {
			return e
		}
		for n := 0; n < 18; n++ {
			done, e := f.store.Rebuild(ctx, 64)
			if e != nil {
				return e
			}
			if done {
				p, e := f.client.Search(ctx, q)
				return must(e == nil && len(p.Limitations) == 0 && p.IndexRevision == 1, "index repair")
			}
		}
		return fmt.Errorf("repair exceeded batches")
	case "pagination":
		seen := map[string]bool{}
		for pages := 0; pages < 40; pages++ {
			p, e := f.client.List(ctx, q)
			if e != nil {
				return e
			}
			if p.SchemaLoads != 0 || len(p.Items) > 32 || p.Scanned > 2048 {
				return fmt.Errorf("unbounded page")
			}
			for _, item := range p.Items {
				key := catalog.Key(item.Ref)
				if seen[key] || item.Guarantees == "" || item.Effects == "" {
					return fmt.Errorf("duplicate/incomplete summary")
				}
				seen[key] = true
			}
			if p.Cursor == "" {
				return must(len(seen) == 1008, "pagination coverage")
			}
			q.Cursor = p.Cursor
		}
		return fmt.Errorf("pagination did not end")
	case "cursor-tamper", "cursor-query", "cursor-subject", "cursor-revision", "cursor-revocation":
		p, e := f.client.List(ctx, q)
		if e != nil {
			return e
		}
		q.Cursor = p.Cursor
		switch name {
		case "cursor-tamper":
			q.Cursor = "x" + q.Cursor[1:]
		case "cursor-query":
			q.Category = "procurement"
		case "cursor-subject":
			s, e := f.service(f.store, "other")
			if e != nil {
				return e
			}
			_, e = s.List(ctx, q)
			return must(e != nil, "cross subject cursor accepted")
		case "cursor-revision":
			_, e = f.store.Replace(ctx, 1, f.entries)
		case "cursor-revocation":
			e = f.revoke(ctx)
		}
		if e != nil {
			return e
		}
		_, e = f.client.List(ctx, q)
		return must(e != nil, "stale cursor accepted")
	case "describe-denied":
		if e = f.revoke(ctx); e != nil {
			return e
		}
		_, e = f.client.Describe(ctx, f.entries[0].Ref)
		return must(authorization.Is(e, authorization.Denied), "unauthorized schema")
	case "invalid-query":
		for _, bad := range []catalog.Query{{Purpose: "other", Location: "local", Limit: 1, Budget: 1}, {Purpose: "task", Location: "cloud", Limit: 1, Budget: 1}, {Purpose: "task", Location: "local", Limit: 65, Budget: 1}, {Purpose: "task", Location: "local", Limit: 1, Budget: 4097}, {Purpose: "task", Location: "local", Limit: 1, Budget: 1, Text: strings.Repeat("x", 131073)}} {
			if _, e = f.client.Search(ctx, bad); e == nil {
				return fmt.Errorf("invalid query accepted")
			}
		}
		_, e = sdk.NewCapabilityClient(f.transport, "wrong").List(ctx, q)
		return must(e != nil, "wrong discovery namespace")
	case "exact-version":
		for _, which := range []string{"version", "digest", "implementation"} {
			r := f.entries[0].Ref
			switch which {
			case "version":
				r.Version = "absent"
			case "digest":
				r.Digest = strings.Repeat("0", 64)
			case "implementation":
				r.Implementation = "absent"
			}
			if _, e = f.client.Describe(ctx, r); e == nil {
				return fmt.Errorf("exact ref substituted")
			}
		}
		return nil
	case "immutable-version":
		changed := append([]catalog.Entry(nil), f.entries...)
		changed[0].Effects = "different meaning"
		_, e = f.store.Replace(ctx, 1, changed)
		if !authorization.Is(e, authorization.IdentityConflict) {
			return fmt.Errorf("immutable declaration accepted: %v", e)
		}
		p, e := f.client.Search(ctx, q)
		return must(e == nil && p.Revision == 1 && p.Scanned == 1008, "partial import published")
	case "admitted-withdrawal":
		return f.withdrawal(ctx)
	case "revocation-during-search", "slow-index":
		s, e := f.service(hookedStore{Store: f.store, index: func(c context.Context, r []catalog.Ref, v uint64) (catalog.IndexResult, error) {
			if name == "slow-index" {
				<-c.Done()
				return catalog.IndexResult{}, c.Err()
			}
			if e := f.revoke(c); e != nil {
				return catalog.IndexResult{}, e
			}
			return f.store.Index(c, r, v)
		}}, "operator")
		if e != nil {
			return e
		}
		p, e := s.Search(ctx, q)
		return must(e != nil && len(p.Items) == 0, "released expired authorization or deadline")
	case "cancelled":
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		p, e := f.client.Search(cancelled, q)
		return must(e != nil && len(p.Items) == 0, "cancelled query disclosed results")
	case "aliases":
		q.Text = "purchase order draft"
		q.Required = []string{"create draft"}
		q.Category = "procurement"
		p, e := f.client.Search(ctx, q)
		return must(e == nil && len(p.Items) > 0 && p.Items[0].Ref == f.entries[0].Ref, "alias/category/required search")
	}
	expected := ""
	q.Text = "zzzz-no-business-match"
	switch name {
	case "source-unavailable":
		e = f.store.SetAvailable(ctx, f.entries[0].Ref, false)
		expected = "SOURCE_UNAVAILABLE"
	case "index-missing":
		e = f.store.InvalidateIndex(ctx)
		expected = "INDEX_MISSING"
	case "index-stale":
		_, e = f.store.Replace(ctx, 1, f.entries)
		expected = "INDEX_STALE"
	case "index-rebuilding":
		e = f.store.InvalidateIndex(ctx)
		if e == nil {
			_, e = f.store.Rebuild(ctx, 16)
		}
		expected = "INDEX_REBUILDING"
	case "index-corrupt":
		e = f.sql(ctx, `UPDATE catalog_index SET terms='corrupted' WHERE ref=?`, catalog.Key(f.entries[0].Ref))
		expected = "INDEX_CORRUPT"
	case "index-unreachable":
		e = f.sql(ctx, `DROP TABLE catalog_index`)
		expected = "INDEX_UNAVAILABLE"
	case "budget":
		q.Budget = 1
		expected = "SCAN_BUDGET"
	case "no-match":
	default:
		return fmt.Errorf("unknown boundary")
	}
	if e != nil {
		return e
	}
	p, e := f.client.Search(ctx, q)
	if e != nil {
		return e
	}
	if len(p.Items) != 0 {
		return fmt.Errorf("false match")
	}
	if expected == "" {
		return must(p.Coverage == "COMPLETE" && len(p.Limitations) == 0 && p.Scanned == 1008, "false complete no-match")
	}
	if !strings.Contains(strings.Join(p.Limitations, ","), expected) {
		return fmt.Errorf("missing limitation %s: %+v", expected, p)
	}
	if name == "budget" || name == "source-unavailable" {
		return must(p.Coverage == "PARTIAL", "incomplete source reported complete")
	}
	return nil
}
func (f *fixture) withdrawal(ctx context.Context) error {
	entry := f.entries[0]
	if _, e := f.manager.Resolve(ctx, entry.Ref.Namespace); e != nil {
		return e
	}
	h := f.manager.current
	h.selectEntry(entry)
	r, m, e := h.request(ctx, []byte(`{"record":"old","ordered_units":3,"approved_units":10,"vendor_verified":true}`), 1)
	if e != nil {
		return e
	}
	client := sdk.NewCapabilityClient(f.transport, entry.Ref.Namespace)
	receipt, e := client.Invoke(ctx, r, m)
	if e != nil {
		return e
	}
	if _, e = f.store.Replace(ctx, 1, f.entries[1:]); e != nil {
		return e
	}
	if _, e = f.client.Describe(ctx, entry.Ref); e == nil {
		return fmt.Errorf("withdrawn describe")
	}
	out, e := h.exec.Run(ctx, r.OperationID)
	if e != nil || out.Effect != "CONFIRMED" {
		return fmt.Errorf("old admission lost: %v", e)
	}
	if e = h.exec.Drain(ctx, 16); e != nil {
		return e
	}
	replay, e := client.Invoke(ctx, r, "")
	if e != nil || replay != receipt {
		return fmt.Errorf("old replay rejected %v", e)
	}
	next, m, e := h.request(ctx, []byte(`{"record":"new","ordered_units":3,"approved_units":10,"vendor_verified":true}`), 1)
	if e != nil {
		return e
	}
	if _, e = client.Invoke(ctx, next, m); e == nil {
		return fmt.Errorf("withdrawn new admission")
	}
	if _, e = h.target.Snapshot(ctx, "new"); e == nil {
		return fmt.Errorf("withdrawn business effect")
	}
	return nil
}

func (f *fixture) gateRace(ctx context.Context) error {
	check, e := simworkflow.ImplementationCheck()
	if e != nil {
		return e
	}
	other, e := sqlitecatalog.Open(filepath.Join(f.root, "catalog.db"), check)
	if e != nil {
		return e
	}
	defer other.Close()
	entered := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- f.store.WithVersion(ctx, f.entries[0].Ref, func(catalog.Entry) error { close(entered); <-release; return nil })
	}()
	<-entered
	bounded, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	_, updateErr := other.Replace(bounded, 1, f.entries[1:2])
	cancel()
	close(release)
	if e = <-done; e != nil {
		return e
	}
	if updateErr == nil {
		return fmt.Errorf("publication crossed held admission gate")
	}
	if _, e = other.Replace(ctx, 1, f.entries[1:2]); e != nil {
		return e
	}
	called := false
	e = f.store.WithVersion(ctx, f.entries[0].Ref, func(catalog.Entry) error { called = true; return nil })
	return must(e != nil && !called, "withdrawn gate called executor")
}
func (f *fixture) sharedControl(ctx context.Context, unknown bool) error {
	if _, e := f.manager.Resolve(ctx, f.entries[0].Ref.Namespace); e != nil {
		return e
	}
	h := f.manager.current
	entry := f.entries[0]
	h.selectEntry(entry)
	if unknown {
		r, m, e := h.request(ctx, []byte(`{"record":"uncertain","ordered_units":3,"approved_units":10,"vendor_verified":true}`), 1)
		if e != nil {
			return e
		}
		if _, e = h.client.Invoke(ctx, r, m); e != nil {
			return e
		}
		// A real durable start whose target evidence cannot currently be read.
		failing := uncertainDriver{Driver: h.target}
		svc, e := execution.New(h.grants, h.work, h.access, failing, h.binding, h.cap, config(), h.operation)
		if e != nil {
			return e
		}
		svc, e = svc.WithResourceControl(h.scope(), h.target)
		if e != nil {
			return e
		}
		out, e := svc.Run(ctx, r.OperationID)
		if e != nil || out.Effect != "UNKNOWN" {
			return fmt.Errorf("unknown setup: %v", e)
		}
	} else {
		op, e := h.operation(ctx)
		if e != nil {
			return e
		}
		if _, e = h.client.RequestResourceControl(ctx, execution.ResourceControlRequest{OperationID: op, Resource: h.target.Resource(), Intent: "TAKEOVER", ExpectedVersion: 1}); e != nil {
			return e
		}
	}
	h.selectEntry(f.entries[1])
	r, m, e := h.request(ctx, []byte(`{"record":"uncertain"}`), 2)
	if e != nil {
		return e
	}
	client := sdk.NewCapabilityClient(f.transport, h.namespace)
	_, e = client.Invoke(ctx, r, m)
	return must(e != nil, "cross-capability resource control bypass")
}

type uncertainDriver struct{ execution.Driver }

func (d uncertainDriver) Inspect(context.Context, execution.Call) (execution.Observation, error) {
	return execution.Observation{Phase: "UNKNOWN", Result: "UNKNOWN", Effect: "UNKNOWN"}, nil
}
