package governance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type fixture struct {
	ctx        context.Context
	store      runtime.Store
	scope      runtime.Scope
	auth       runtime.Auth
	svc        *governance.Service
	registry   *runtime.Registry
	dispatcher *runtime.Dispatcher
}

// 预览来源门禁的数据库边界夹具；由测试受信预置，公开 API 无写权。
type previewControl struct {
	Ref        api.ContentRef `json:"ref"`
	SubjectID  string         `json:"subject_id"`
	Generation uint64         `json:"generation"`
}
type previewGate struct{}

func (previewGate) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ContentRef) error {
	for _, ref := range refs {
		var p previewControl
		if _, err := tx.Get(ctx, "governance/fixture_preview", ref.ContentID, &p); err != nil {
			return err
		}
		if p.SubjectID != auth.SubjectID || p.Generation != auth.CredentialGeneration || !api.Equal(p.Ref, ref) {
			return api.E("forbidden", "preview_revoked")
		}
	}
	return nil
}

func environment(t *testing.T, options governance.Options) *fixture {
	t.Helper()
	ctx := context.Background()
	var store runtime.Store
	var err error
	if dsn := os.Getenv("HARNESS_GOVERNANCE_POSTGRES_DSN"); dsn != "" {
		pg, e := postgres.Open(ctx, dsn)
		if e != nil {
			t.Fatal(e)
		}
		if e = pg.Migrate(ctx); e != nil {
			t.Fatal(e)
		}
		store = pg
	} else {
		local, e := sqlite.Open(filepath.Join(t.TempDir(), "governance.db"))
		if e != nil {
			t.Fatal(e)
		}
		if e = local.Migrate(ctx); e != nil {
			t.Fatal(e)
		}
		store = local
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	f := &fixture{ctx: ctx, store: store, scope: runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}, registry: runtime.NewRegistry()}
	f.auth = runtime.Auth{TenantID: f.scope.TenantID, SubjectID: api.NewID("user"), CredentialGeneration: 1, Roles: []string{"trusted_renderer", "grant_authority", "maintainer", "release_approver", "evaluation_authority"}}
	if options.PreviewGate == nil {
		options.PreviewGate = previewGate{}
	}
	f.svc = governance.New(store, options)
	if err = f.svc.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher = &runtime.Dispatcher{Store: store, OwnerID: f.scope.OwnerID, Registry: f.registry}
	return f
}
func command(t *testing.T, f *fixture, method, target string, in any, revision *uint64) (api.Command, api.Receipt) {
	t.Helper()
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: target, ExpiresAt: api.Time(time.Now().Add(time.Hour)), ExpectedRevision: revision, Payload: api.Raw(in)}
	r, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(c))
	if err != nil {
		t.Fatal(err)
	}
	return c, r
}
func query[T any](t *testing.T, f *fixture, method string, in any) T {
	t.Helper()
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: api.NewID("query"), Method: method, TargetID: f.scope.OwnerID, Payload: api.Raw(in)}
	b, err := f.dispatcher.Query(f.ctx, f.auth, api.Raw(q))
	if err != nil {
		t.Fatal(err)
	}
	var out T
	if err = api.Decode(b, &out); err != nil {
		t.Fatal(err)
	}
	return out
}
func ref(t *testing.T, f *fixture, prefix string) api.ContentRef {
	t.Helper()
	r := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID(prefix), Version: 1, Hash: api.Hash([]byte(prefix)), MediaType: "text/plain", ByteLength: uint64(len(prefix))}
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		return tx.Create(f.ctx, "governance/fixture_preview", r.ContentID, f.auth.SubjectID, previewControl{Ref: r, SubjectID: f.auth.SubjectID, Generation: f.auth.CredentialGeneration})
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("preview fixture: %s %v", status, err)
	}
	return r
}
func TestOrdinarySubjectCannotIssuePermission(t *testing.T) {
	f := environment(t, governance.Options{})
	f.auth.Roles = nil
	g := api.Grant{GrantID: api.NewID("grant"), OwnerID: f.scope.OwnerID, Revision: 1, SubjectRef: f.auth.Ref(f.scope.OwnerID), Resources: []string{"document"}, Actions: []string{"read"}, Purposes: []string{"task_input"}, Recipients: []string{"brain"}, Locations: []string{"cloud"}, Mode: "once", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: api.Time(time.Now().Add(time.Hour)), Limits: []api.Amount{}}
	_, r := command(t, f, "grant.issue", g.GrantID, governance.GrantIssue{Grant: g, PreviewRefs: []api.ContentRef{ref(t, f, "preview")}, ConfirmationExpiresAt: g.ExpiresAt, ParentGrantRefs: []api.ObjectRef{}}, nil)
	if r.Stage != "rejected" || r.Error == nil || r.Error.Code != "forbidden" {
		t.Fatalf("untrusted issue = %+v", r)
	}
}

func drain(t *testing.T, f *fixture, kind string) {
	t.Helper()
	for pass := 0; pass < 100; pass++ {
		jobs, status, err := f.store.Claim(f.ctx, f.scope, api.NewID("worker"), []string{kind}, 100, time.Minute)
		if err != nil || status != runtime.Committed {
			t.Fatalf("claim: %s %v", status, err)
		}
		if len(jobs) == 0 {
			return
		}
		h, ok := f.registry.Job(kind)
		if !ok {
			t.Fatal("handler missing")
		}
		for _, job := range jobs {
			if err = h(f.ctx, f.store, f.scope, job); err != nil {
				t.Fatal(err)
			}
		}
	}
	t.Fatal("job did not settle within bounded drain")
}

type usageEvidence struct{}

func (usageEvidence) Verify(_ context.Context, _ runtime.Scope, _ api.ObjectRef, u api.UsageSnapshot) error {
	if len(u.ProofRefs) == 0 {
		return api.E("forbidden", "no_independent_source")
	}
	return nil
}
func issue(t *testing.T, f *fixture, mode string) api.Grant {
	t.Helper()
	g := api.Grant{GrantID: api.NewID("grant"), OwnerID: f.scope.OwnerID, Revision: 1, SubjectRef: f.auth.Ref(f.scope.OwnerID), Resources: []string{"document"}, Actions: []string{"write"}, Purposes: []string{"save"}, Recipients: []string{"executor"}, Locations: []string{"device"}, Mode: mode, State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: api.Time(time.Now().Add(time.Hour)), Limits: []api.Amount{{Unit: "USD", Value: "10"}}}
	c, r := command(t, f, "grant.issue", g.GrantID, governance.GrantIssue{Grant: g, PreviewRefs: []api.ContentRef{ref(t, f, "preview")}, ConfirmationExpiresAt: g.ExpiresAt, ParentGrantRefs: []api.ObjectRef{}}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("issue: %+v", r)
	}
	var accepted governance.ConfirmedOutput
	if err := api.Decode(r.Output, &accepted); err != nil {
		t.Fatal(err)
	}
	confirm := query[governance.ConfirmationView](t, f, "confirmation.read", governance.IDInput{ID: accepted.ConfirmationRef.ObjectID})
	if confirm.State != "pending" {
		t.Fatalf("confirmation = %+v", confirm)
	}
	_, decided := command(t, f, "confirmation.decide", confirm.RequestID, governance.ConfirmationDecision{RequestID: confirm.RequestID, RequestRevision: confirm.Revision, Decision: "approved", Challenge: confirm.Challenge, PreviewRefs: confirm.PreviewRefs}, nil)
	if decided.Stage != "applied" {
		t.Fatalf("decide: %+v", decided)
	}
	before, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
	if err != nil || before.Stage != "accepted" {
		t.Fatalf("decision must not imply original applied: %+v %v", before, err)
	}
	drain(t, f, "governance.confirmation")
	after, err := f.dispatcher.Lookup(f.ctx, f.auth, c.CommandID)
	if err != nil || after.Stage != "applied" {
		t.Fatalf("original not applied: %+v %v", after, err)
	}
	confirm = query[governance.ConfirmationView](t, f, "confirmation.read", governance.IDInput{ID: confirm.RequestID})
	if confirm.State != "consumed" || confirm.ConsumedBy != c.CommandID {
		t.Fatalf("consumption = %+v", confirm)
	}
	again, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(c))
	if err != nil || !api.Equal(again, after) {
		t.Fatalf("retry changed original: %+v %v", again, err)
	}
	return g
}
func useRequest(f *fixture, g api.Grant) governance.UseRequest {
	return governance.UseRequest{UseID: api.NewID("use"), SubjectRef: f.auth.Ref(f.scope.OwnerID), TargetRef: f.scope.Ref(api.NewID("operation"), 1), TargetKind: "operation", IntentHash: api.Hash([]byte("save precise version")), GrantRefs: []api.ObjectRef{f.scope.Ref(g.GrantID, g.Revision)}, RequestedUnits: []api.Amount{{Unit: "USD", Value: "2"}}, Resources: []string{"document"}, Actions: []string{"write"}, Recipient: "executor", Location: "device", Purposes: []string{"save"}, StartBefore: api.Time(time.Now().Add(time.Minute))}
}
func TestOncePermissionStaysConsumedAfterVerifiedZeroUsage(t *testing.T) {
	f := environment(t, governance.Options{UsageVerifier: usageEvidence{}})
	g := issue(t, f, "once")
	in := useRequest(f, g)
	_, r := command(t, f, "grant.use", in.UseID, in, nil)
	if r.Stage != "applied" {
		t.Fatalf("use: %+v", r)
	}
	var receipt governance.UseReceipt
	if err := api.Decode(r.Output, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Decision != "allowed" {
		t.Fatalf("denied: %+v", receipt)
	}
	f.auth.Roles = append(f.auth.Roles, "usage_reporter")
	_, sr := command(t, f, "grant.use.settle", in.UseID, governance.SettleRequest{UseID: in.UseID, Usage: api.UsageSnapshot{SourceRef: in.TargetRef, UsageRevision: 1, UsageDigest: api.Hash([]byte("closed and never sent")), Cumulative: []api.Amount{{Unit: "USD", Value: "0"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{ref(t, f, "proof")}}}, nil)
	if sr.Stage != "accepted" {
		t.Fatalf("settle: %+v", sr)
	}
	drain(t, f, "governance.settle")
	settled := query[governance.UseSettlement](t, f, "grant.settlement.read", governance.IDInput{ID: in.UseID})
	if !settled.UsageFinal || !settled.SpendingClosed || len(settled.RemainingReserved) != 0 {
		t.Fatalf("settlement: %+v", settled)
	}
	current := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: g.GrantID})
	if !current.OnceConsumed {
		t.Fatal("once resurrected after zero usage")
	}
	in.UseID = api.NewID("use")
	in.TargetRef = f.scope.Ref(api.NewID("operation"), 1)
	_, denied := command(t, f, "grant.use", in.UseID, in, nil)
	var deny governance.UseReceipt
	if err := api.Decode(denied.Output, &deny); err != nil {
		t.Fatal(err)
	}
	if denied.Stage != "applied" || deny.Decision != "denied" || deny.Reason != "once_consumed" {
		t.Fatalf("second use: %+v %+v", denied, deny)
	}
}

func component(name string) api.ComponentRef {
	return api.ComponentRef{ComponentID: api.NewID(name), Version: "1.0.0", Digest: api.Hash([]byte(name))}
}
func makeCheck(t *testing.T, f *fixture, taskID string, dependencies []api.ObjectRef) governance.ConditionCheck {
	t.Helper()
	rule := governance.RuleDefinition{ComponentRef: component("rule"), Kind: "effect", Predicate: "current_state", AllowedBasis: []string{"verified"}, RiskClass: "ordinary", MaxObservationAgeSeconds: 600}
	check := governance.ConditionCheck{CheckID: api.NewID("check"), Revision: 1, AuthorityID: f.scope.OwnerID, TaskID: taskID, GoalRevision: 1, RequirementID: api.NewID("requirement"), RequirementRevision: 1, ArtifactRef: ref(t, f, "artifact"), ScopeRef: ref(t, f, "scope"), ObservedAt: api.Time(time.Now().Add(-time.Second)), CheckedAt: api.Time(time.Now()), RuleRef: rule.ComponentRef, EvaluatorRef: component("evaluator"), ConfigRef: component("config"), InstallLockRef: component("install"), DependentCheckRefs: dependencies, Verdict: "pass", Basis: "verified", ReportRef: ref(t, f, "report"), EvidenceRefs: []api.ContentRef{ref(t, f, "evidence")}}
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		if _, err := f.svc.RegisterRuleTx(f.ctx, tx, rule); err != nil {
			return err
		}
		_, err := f.svc.RegisterCheckTx(f.ctx, tx, check)
		return err
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("register check: %s %v", status, err)
	}
	return check
}
func TestDefectInFullDependencyBlocksCompletionAndNoticesExistingResult(t *testing.T) {
	f := environment(t, governance.Options{})
	taskID := api.NewID("task")
	leaf := makeCheck(t, f, taskID, []api.ObjectRef{})
	root := makeCheck(t, f, taskID, []api.ObjectRef{f.scope.Ref(leaf.CheckID, 1)})
	resultRef := f.scope.Ref(api.NewID("result"), 1)
	completion := governance.EvidenceCompletion{ConsumerTaskRef: f.scope.Ref(taskID, 1), Checks: []governance.CheckReference{{CheckRef: f.scope.Ref(root.CheckID, 1)}}, ResultRef: &resultRef}
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		d, e := f.svc.CheckEvidenceTx(f.ctx, tx, completion)
		if e == nil && (!d.Eligible || len(d.GateRefs) != 4 || len(d.HolderRefs) != 1) {
			t.Fatalf("decision: %+v", d)
		}
		return e
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("completion: %s %v", status, err)
	}
	d := governance.DefectRegister{DefectID: api.NewID("defect"), RuleRef: leaf.RuleRef, EvaluatorRef: leaf.EvaluatorRef, ScopeRef: leaf.ScopeRef, EvidenceRef: ref(t, f, "defect_evidence")}
	_, registered := command(t, f, "evidence.defect.register", d.DefectID, d, nil)
	if registered.Stage != "applied" {
		t.Fatalf("defect: %+v", registered)
	}
	status, err = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error { _, e := f.svc.CheckEvidenceTx(f.ctx, tx, completion); return e })
	if status != runtime.RolledBack || err == nil || err.Error() != "invalid_state: evidence_defective" {
		t.Fatalf("defect did not block composite pass: %s %v", status, err)
	}
	drain(t, f, "governance.defect")
	f.auth.Roles = append(f.auth.Roles, "evidence_consumer")
	notices := query[api.Page[governance.ResultNotice]](t, f, "evidence.notice.list", api.ListInput{Limit: 10})
	if len(notices.Items) != 1 || !api.Equal(notices.Items[0].ResultRef, resultRef) {
		t.Fatalf("notice lost original result: %+v", notices)
	}
}
