package catalogcheck

import (
	"context"
	"fmt"
	"lerna/adapters/contentpolicy"
	"lerna/authorization"
	"lerna/catalog"
	"time"
)

var dataPolicyNames = []string{"query-policy-denied", "source-policy-before-ranking", "source-policy-before-schema", "source-policy-cursor", "source-policy-revoked-during-query", "source-policy-revoked-during-describe", "source-policy-expiry", "source-policy-aba"}

func (f *fixture) sourceRules(query, source bool, until int64) []contentpolicy.Rule {
	rules := []contentpolicy.Rule{}
	if query {
		rules = append(rules, contentpolicy.Rule{Kind: "query", Key: "host-session", Revision: 1, Actions: []string{"process"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: until})
	}
	if source {
		rules = append(rules, contentpolicy.Rule{Kind: "catalog", Key: "business-manifest", Revision: 1, Actions: []string{"process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: until})
	}
	return rules
}

type schemaHook struct {
	catalog.Store
	describe func(context.Context, catalog.Ref) (catalog.Entry, uint64, error)
}

func (s schemaHook) Describe(ctx context.Context, r catalog.Ref) (catalog.Entry, uint64, error) {
	return s.describe(ctx, r)
}
func dataPolicyCheck(ctx context.Context, name string) error {
	f, e := newFixture(ctx)
	if e != nil {
		return e
	}
	defer f.close()
	until := f.host.now().Add(time.Hour).Unix()
	q := query()
	switch name {
	case "query-policy-denied":
		if e = f.dataPolicy.Replace(f.sourceRules(false, true, until)); e != nil {
			return e
		}
		p, e := f.client.Search(ctx, q)
		return must(e != nil && len(p.Items) == 0, "query processed without source permission")
	case "source-policy-before-ranking":
		if e = f.dataPolicy.Replace(f.sourceRules(true, false, until)); e != nil {
			return e
		}
		var read int
		s, e := f.service(hookedStore{Store: f.store, index: func(c context.Context, r []catalog.Ref, v uint64) (catalog.IndexResult, error) {
			read += len(r)
			return f.store.Index(c, r, v)
		}}, "operator")
		if e != nil {
			return e
		}
		p, e := s.Search(ctx, q)
		return must(e == nil && len(p.Items) == 0 && p.Scanned == 0 && read == 0, "prohibited source reached candidates/index")
	case "source-policy-before-schema":
		if e = f.dataPolicy.Replace(f.sourceRules(true, false, until)); e != nil {
			return e
		}
		loads := 0
		s, e := f.service(schemaHook{Store: f.store, describe: func(c context.Context, r catalog.Ref) (catalog.Entry, uint64, error) {
			loads++
			return f.store.Describe(c, r)
		}}, "operator")
		if e != nil {
			return e
		}
		_, e = s.Describe(ctx, f.entries[0].Ref)
		return must(authorization.Is(e, authorization.Denied) && loads == 0, "prohibited schema read before policy")
	case "source-policy-cursor", "source-policy-expiry", "source-policy-aba":
		if name == "source-policy-expiry" {
			if e = f.dataPolicy.Replace(f.sourceRules(true, true, f.host.now().Add(time.Second).Unix())); e != nil {
				return e
			}
		}
		p, e := f.client.List(ctx, q)
		if e != nil {
			return e
		}
		q.Cursor = p.Cursor
		if name == "source-policy-expiry" {
			f.host.clock.advance(2 * time.Second)
		} else {
			if e = f.dataPolicy.Replace(f.sourceRules(true, false, until)); e != nil {
				return e
			}
			if name == "source-policy-aba" {
				if e = f.dataPolicy.Replace(f.sourceRules(true, true, until)); e != nil {
					return e
				}
			}
		}
		_, e = f.client.List(ctx, q)
		return must(e != nil, "data policy stale cursor accepted")
	case "source-policy-revoked-during-query":
		s, e := f.service(hookedStore{Store: f.store, index: func(c context.Context, r []catalog.Ref, v uint64) (catalog.IndexResult, error) {
			out, e := f.store.Index(c, r, v)
			if e != nil {
				return out, e
			}
			return out, f.dataPolicy.Replace(f.sourceRules(true, false, until))
		}}, "operator")
		if e != nil {
			return e
		}
		p, e := s.Search(ctx, q)
		return must(e != nil && len(p.Items) == 0, "revoked source candidates disclosed")
	case "source-policy-revoked-during-describe":
		s, e := f.service(schemaHook{Store: f.store, describe: func(c context.Context, r catalog.Ref) (catalog.Entry, uint64, error) {
			entry, revision, e := f.store.Describe(c, r)
			if e != nil {
				return entry, revision, e
			}
			return entry, revision, f.dataPolicy.Replace(f.sourceRules(true, false, until))
		}}, "operator")
		if e != nil {
			return e
		}
		entry, e := s.Describe(ctx, f.entries[0].Ref)
		return must(e != nil && entry.Ref.Name == "", "revoked source schema disclosed")
	}
	return fmt.Errorf("unknown policy check")
}
