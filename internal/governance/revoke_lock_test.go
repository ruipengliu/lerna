package governance_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// 仅观察真实持久锁边界，拒绝 grant→preview 的反序；不模拟数据库提交或回执。
type previewBeforeGrantStore struct {
	runtime.Store
	runtime.QueryBindingStore
}
type previewBeforeGrantTx struct {
	runtime.Tx
	grantLocked bool
	previews    map[string]bool
}

func (s previewBeforeGrantStore) Within(ctx context.Context, scope runtime.Scope, parts []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, parts, func(tx runtime.Tx) error { return fn(&previewBeforeGrantTx{Tx: tx, previews: map[string]bool{}}) })
}
func (tx *previewBeforeGrantTx) Get(ctx context.Context, ns, id string, out any) (uint64, error) {
	if ns == "governance/fixture_preview" && tx.grantLocked && !tx.previews[id] {
		return 0, fmt.Errorf("original grant locked before its preview source head")
	}
	rev, err := tx.Tx.Get(ctx, ns, id, out)
	if err == nil {
		if ns == "governance/grants" {
			tx.grantLocked = true
		}
		if ns == "governance/fixture_preview" {
			tx.previews[id] = true
		}
	}
	return rev, err
}
func (tx *previewBeforeGrantTx) Savepoint(ctx context.Context, fn func(runtime.Tx) error) error {
	return tx.Tx.Savepoint(ctx, func(inner runtime.Tx) error {
		prior := tx.Tx
		tx.Tx = inner
		defer func() { tx.Tx = prior }()
		return fn(tx)
	})
}

func TestGrantRevokeLocksExactPreviewBeforeCurrentGrantAndRetainsPendingCAS(t *testing.T) {
	f := environment(t, governance.Options{})
	g := issue(t, f, "continuous")
	f.store = previewBeforeGrantStore{Store: f.store, QueryBindingStore: f.store.(runtime.QueryBindingStore)}
	f.dispatcher.Store = f.store
	preview := ref(t, f, "revoke_preview")
	revision := uint64(1)
	c, r := command(t, f, "grant.revoke", g.GrantID, governance.GrantRevoke{GrantRef: f.scope.Ref(g.GrantID, 1), PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: api.Time(time.Now().Add(time.Hour))}, &revision)
	if r.Stage != "accepted" {
		t.Fatalf("legitimate revoke pending %+v", r)
	}
	var pending governance.ConfirmedOutput
	if err := api.Decode(r.Output, &pending); err != nil || pending.ConfirmationRef == nil {
		t.Fatalf("pending confirmation %+v %v", pending, err)
	}
	confirm := query[governance.ConfirmationView](t, f, "confirmation.read", governance.IDInput{ID: pending.ConfirmationRef.ObjectID})
	_, approved := command(t, f, "confirmation.decide", confirm.RequestID, governance.ConfirmationDecision{RequestID: confirm.RequestID, RequestRevision: confirm.Revision, Decision: "approved", Challenge: confirm.Challenge, PreviewRefs: confirm.PreviewRefs}, nil)
	if approved.Stage != "applied" {
		t.Fatalf("original approval %+v", approved)
	}
	drain(t, f, "governance.confirmation")
	closed, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
	if err != nil || closed.Stage != "applied" {
		t.Fatalf("original revoke failed ordered closure %+v %v", closed, err)
	}
	current := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: g.GrantID})
	if current.Grant.State != "revoked" || current.Grant.Revision != 2 {
		t.Fatalf("ordered revoke did not change exactly its original grant %+v", current)
	}
	stale := uint64(1)
	_, rejected := command(t, f, "grant.revoke", g.GrantID, governance.GrantRevoke{GrantRef: f.scope.Ref(g.GrantID, 1), PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: api.Time(time.Now().Add(time.Hour))}, &stale)
	if rejected.Stage != "rejected" || rejected.Error == nil || rejected.Error.Code != "revision_conflict" {
		t.Fatalf("stale initial CAS created a pending confirmation %+v", rejected)
	}
	currentCAS := uint64(2)
	_, rejected = command(t, f, "grant.revoke", g.GrantID, governance.GrantRevoke{GrantRef: f.scope.Ref(g.GrantID, 1), PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: api.Time(time.Now().Add(time.Hour))}, &currentCAS)
	if rejected.Stage != "rejected" || rejected.Error == nil || rejected.Error.Code != "revision_conflict" {
		t.Fatalf("stale original body created a pending confirmation %+v", rejected)
	}
	again, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(c))
	if err != nil || !api.Equal(again, closed) {
		t.Fatalf("original revoke replay changed %+v %v", again, err)
	}
}

// 故障只注入准确预览的纯事务门禁；所有命令、确认、Grant 与 Job 由真实数据库维护。
type falliblePreviewGate struct {
	checks int
	failAt int
	fault  error
}

func (g *falliblePreviewGate) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ContentRef) error {
	g.checks++
	if g.checks == g.failAt {
		return g.fault
	}
	return (previewGate{}).CheckTx(ctx, tx, auth, refs)
}

func TestOriginalRevokeRetainsApprovedConfirmationAcrossPreviewDependencyFailure(t *testing.T) {
	for _, fault := range []error{errors.New("preview original database failed"), context.DeadlineExceeded, api.E("dependency_unavailable", "preview_temporarily_unavailable"), api.E("effect_unknown", "preview_source_unresolved")} {
		t.Run(fault.Error(), func(t *testing.T) {
			gate := &falliblePreviewGate{}
			f := environment(t, governance.Options{PreviewGate: gate})
			g := issue(t, f, "continuous")
			preview := ref(t, f, "revoke_preview")
			revision := uint64(1)
			c, r := command(t, f, "grant.revoke", g.GrantID, governance.GrantRevoke{GrantRef: f.scope.Ref(g.GrantID, 1), PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: api.Time(time.Now().Add(time.Hour))}, &revision)
			if r.Stage != "accepted" {
				t.Fatalf("revoke not accepted %+v", r)
			}
			var pending governance.ConfirmedOutput
			if err := api.Decode(r.Output, &pending); err != nil || pending.ConfirmationRef == nil {
				t.Fatalf("original pending %+v %v", pending, err)
			}
			confirm := query[governance.ConfirmationView](t, f, "confirmation.read", governance.IDInput{ID: pending.ConfirmationRef.ObjectID})
			_, approved := command(t, f, "confirmation.decide", confirm.RequestID, governance.ConfirmationDecision{RequestID: confirm.RequestID, RequestRevision: confirm.Revision, Decision: "approved", Challenge: confirm.Challenge, PreviewRefs: confirm.PreviewRefs}, nil)
			if approved.Stage != "applied" {
				t.Fatalf("approval %+v", approved)
			}
			jobs, status, err := f.store.Claim(f.ctx, f.scope, api.NewID("worker"), []string{"governance.confirmation"}, 1, time.Minute)
			if err != nil || status != runtime.Committed || len(jobs) != 1 {
				t.Fatalf("original claim %s %v %+v", status, err, jobs)
			}
			h, ok := f.registry.Job("governance.confirmation")
			if !ok {
				t.Fatal("confirmation handler missing")
			}
			gate.checks, gate.failAt, gate.fault = 0, 2, fault
			if err = h(f.ctx, f.store, f.scope, jobs[0]); !errors.Is(err, fault) {
				t.Fatalf("original preview cause lost: %v, want %v", err, fault)
			}
			accepted, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
			if err != nil || accepted.Stage != "accepted" {
				t.Fatalf("dependency failure permanently decided original %+v %v", accepted, err)
			}
			before := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: g.GrantID})
			current := query[governance.ConfirmationView](t, f, "confirmation.read", governance.IDInput{ID: confirm.RequestID})
			if before.Grant.Revision != 1 || before.Grant.State != "active" || current.State != "approved" {
				t.Fatalf("rolled back preview altered original grant or confirmation %+v %+v", before, current)
			}
			gate.failAt = 0
			if err = h(f.ctx, f.store, f.scope, jobs[0]); err != nil {
				t.Fatal(err)
			}
			applied, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
			if err != nil || applied.Stage != "applied" {
				t.Fatalf("same original claim did not recover %+v %v", applied, err)
			}
			closed := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: g.GrantID})
			if closed.Grant.Revision != 2 || closed.Grant.State != "revoked" {
				t.Fatalf("original grant not exactly revoked %+v", closed)
			}
			replay, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(c))
			if err != nil || !api.Equal(replay, applied) || !api.Equal(query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: g.GrantID}), closed) {
				t.Fatal("original revoke replay created another revision")
			}
		})
	}
}

func TestRevokedExactPreviewStillRejectsOriginalRevoke(t *testing.T) {
	f := environment(t, governance.Options{})
	g := issue(t, f, "continuous")
	preview := ref(t, f, "revoke_preview")
	revision := uint64(1)
	c, r := command(t, f, "grant.revoke", g.GrantID, governance.GrantRevoke{GrantRef: f.scope.Ref(g.GrantID, 1), PreviewRefs: []api.ContentRef{preview}, ConfirmationExpiresAt: api.Time(time.Now().Add(time.Hour))}, &revision)
	if r.Stage != "accepted" {
		t.Fatalf("revoke not accepted %+v", r)
	}
	var pending governance.ConfirmedOutput
	if err := api.Decode(r.Output, &pending); err != nil || pending.ConfirmationRef == nil {
		t.Fatalf("original pending %+v %v", pending, err)
	}
	confirm := query[governance.ConfirmationView](t, f, "confirmation.read", governance.IDInput{ID: pending.ConfirmationRef.ObjectID})
	_, approved := command(t, f, "confirmation.decide", confirm.RequestID, governance.ConfirmationDecision{RequestID: confirm.RequestID, RequestRevision: confirm.Revision, Decision: "approved", Challenge: confirm.Challenge, PreviewRefs: confirm.PreviewRefs}, nil)
	if approved.Stage != "applied" {
		t.Fatalf("approval %+v", approved)
	}
	// 在准确来源门禁撤回原主体披露；公开 Governance 无写来源的权限。
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		var current previewControl
		rev, err := tx.Get(f.ctx, "governance/fixture_preview", preview.ContentID, &current)
		if err != nil {
			return err
		}
		current.Generation++
		return tx.Put(f.ctx, "governance/fixture_preview", preview.ContentID, rev, current)
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("actual preview revocation %s %v", status, err)
	}
	drain(t, f, "governance.confirmation")
	rejected, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
	if err != nil || rejected.Stage != "rejected" || !api.IsCode(rejected.Error, "forbidden") {
		t.Fatalf("revoked preview not refused %+v %v", rejected, err)
	}
	current := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: g.GrantID})
	if current.Grant.Revision != 1 || current.Grant.State != "active" {
		t.Fatalf("refused preview revoked another authority %+v", current)
	}
	replayed, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(c))
	if err != nil || !api.Equal(replayed, rejected) {
		t.Fatalf("definitive refusal changed on replay %+v %v", replayed, err)
	}
}
