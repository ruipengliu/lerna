package providers_test

import (
	"context"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 夹具只替换外部授权端口；存储、上传票据、对象介质与 HTTP 均为真实实现。
type informationFixture struct {
	ctx     context.Context
	store   runtime.Store
	scope   runtime.Scope
	auth    runtime.Auth
	content *informationMemory
	gate    *informationGate
	cfg     providers.InformationConfig
	reopen  func() (runtime.Store, error)
}
type informationMemory struct {
	service *memory.Service
	store   runtime.Store
	policy  memory.Policy
}
type informationPlan struct {
	Request     memory.PublicationRequest
	Publication providers.ReceivedPublication
}

func (m *informationMemory) ReadBytes(ctx context.Context, s runtime.Scope, a runtime.Auth, r api.ContentRef, p, l string) ([]byte, error) {
	return m.service.ReadBytes(ctx, s, a, r, p, l)
}
func (m *informationMemory) PublishReceived(ctx context.Context, s runtime.Scope, a runtime.Auth, p providers.ReceivedPublication, b []byte) (api.ContentRef, error) {
	var plan informationPlan
	status, err := m.store.Within(ctx, s, []string{"providers", "content", "memory"}, func(tx runtime.Tx) error {
		_, err := tx.Get(ctx, "providers.test_publications", p.ContentRef.ContentID, &plan)
		if err == nil {
			if !api.Equal(plan.Publication, p) {
				return api.E("idempotency_conflict", "test_publication_changed")
			}
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		retain := now.Add(30 * time.Minute)
		for _, ref := range p.ProcessedSources {
			source, err := m.service.CheckContentTx(ctx, tx, a, ref, "content.write", "local", true)
			if err != nil {
				return err
			}
			until, err := api.ParseTime(source.RetentionUntil)
			if err != nil {
				return err
			}
			if until.Before(retain) {
				retain = until
			}
		}
		plan = informationPlan{Publication: p, Request: memory.PublicationRequest{ContentRef: p.ContentRef, TransferID: api.NewID("transfer"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: m.policy.PolicyRef, ProcessedSources: p.ProcessedSources, DisclosedSources: p.DisclosedSources, RetentionUntil: api.Time(retain), TransferDeadline: api.Time(now.Add(time.Minute))}}
		return tx.Create(ctx, "providers.test_publications", p.ContentRef.ContentID, a.SubjectID, plan)
	})
	if status == runtime.CommitUnknown {
		return api.ContentRef{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return api.ContentRef{}, err
	}
	return m.service.Upload(ctx, s, a, plan.Request, b)
}
func (m *informationMemory) RecoverReceived(ctx context.Context, s runtime.Scope, a runtime.Auth, p providers.ReceivedPublication) (api.ContentRef, error) {
	var plan informationPlan
	_, err := m.store.Read(ctx, s, "providers.test_publications", p.ContentRef.ContentID, 0, &plan)
	if err != nil {
		return api.ContentRef{}, err
	}
	if !api.Equal(plan.Publication, p) {
		return api.ContentRef{}, api.E("idempotency_conflict", "test_publication_changed")
	}
	return m.service.RecoverUpload(ctx, s, a, plan.Request)
}

type informationGate struct {
	store  runtime.Store
	denied atomic.Bool
	checks atomic.Int64
}

func (g *informationGate) Check(ctx context.Context, r execution.AttemptRequest, out providers.HTTPOutRequest) (providers.InformationPermit, error) {
	g.checks.Add(1)
	if g.denied.Load() {
		return providers.InformationPermit{}, api.E("forbidden", "source_revoked")
	}
	if len(out.ActualIPs) != 1 || out.ActualIPs[0] != "127.0.0.1" || out.Receiver != "reference-source" {
		return providers.InformationPermit{}, api.E("forbidden", "actual_outlet_mismatch")
	}
	var permit providers.InformationPermit
	status, err := g.store.Within(ctx, r.Scope, []string{"providers"}, func(tx runtime.Tx) error {
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		permit = providers.InformationPermit{StartBefore: api.Time(now.Add(time.Minute)), PolicyRevision: 1, RequestDigest: out.RequestDigest}
		return nil
	})
	if status == runtime.CommitUnknown {
		return permit, runtime.ErrCommitUnknown
	}
	return permit, err
}
func newInformationFixture(t *testing.T, origin string, faults ...func(string) error) informationFixture {
	t.Helper()
	ctx := context.Background()
	var store runtime.Store
	var err error
	var reopen func() (runtime.Store, error)
	fault := func(phase string) error {
		for _, f := range faults {
			if err := f(phase); err != nil {
				return err
			}
		}
		return nil
	}
	if os.Getenv("HARNESS_INFORMATION_TEST_DRIVER") == "postgres" {
		dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Fatal("explicit PostgreSQL source validation requires configured DSN")
		}
		pg, e := postgres.Open(ctx, dsn, postgres.WithMaxConnections(8), postgres.WithCommitFault(func(phase postgres.CommitPhase) error { return fault(string(phase)) }))
		err = e
		store = pg
		if err == nil {
			err = pg.Migrate(ctx)
		}
		reopen = func() (runtime.Store, error) { return postgres.Open(ctx, dsn, postgres.WithMaxConnections(8)) }
	} else {
		path := filepath.Join(t.TempDir(), "information.sqlite")
		sq, e := sqlite.Open(path, sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error { return fault(string(phase)) }))
		err = e
		store = sq
		if err == nil {
			err = sq.Migrate(ctx)
		}
		reopen = func() (runtime.Store, error) { return sqlite.Open(path) }
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1, Roles: []string{"content_admin"}}
	objects, err := objectstore.OpenLocal(t.TempDir(), memory.MaxContentBytes)
	if err != nil {
		t.Fatal(err)
	}
	svc := memory.New(store, objects)
	values := memory.PolicyValues{Subjects: []string{auth.SubjectID}, Purposes: []string{"content.write", "information.search", "information.body", "information.source", "task.goal"}, Locations: []string{"local"}, RetainUntil: api.Time(time.Now().Add(time.Hour)), Continuous: true}
	digest, _ := api.Digest(values)
	policy, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = svc.InstallPolicy(ctx, scope, auth, policy); err != nil {
		t.Fatal(err)
	}
	receiver := &informationMemory{service: svc, store: store, policy: policy}
	gate := &informationGate{store: store}
	cfg := providers.InformationConfig{Store: store, Scope: scope, Content: receiver, Egress: gate, AllowHTTPForLoopback: true, Source: providers.InformationSourceDescriptor{SourceRef: api.ComponentRef{ComponentID: api.NewID("source"), Version: "1"}, Origin: origin, SearchPath: "/search", FetchPathPrefixes: []string{"/articles/"}, AllowedCIDRs: []string{"127.0.0.1/32"}, Receiver: "reference-source", Location: "local", PublicUnbilled: true, MaxQueryBytes: 4096, MaxItems: 20, MaxResponseBytes: 1 << 20, TimeoutMillis: 5000, MaxConcurrent: 2}}
	return informationFixture{ctx: ctx, store: store, scope: scope, auth: auth, content: receiver, gate: gate, cfg: cfg, reopen: reopen}
}

func (f *informationFixture) reopenDatabase(t *testing.T) {
	t.Helper()
	if err := f.store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err := f.reopen()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if store.ID() != f.scope.DatabaseID {
		t.Fatal("reopened database identity changed")
	}
	objects := f.content.service.Objects
	f.store = store
	f.content.store = store
	f.content.service = memory.New(store, objects)
	f.gate.store = store
	f.cfg.Store = store
}
func (f informationFixture) request(t *testing.T, d execution.Driver, args any, extra ...api.ContentRef) execution.AttemptRequest {
	t.Helper()
	b := api.Raw(args)
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(b), MediaType: "application/json", ByteLength: uint64(len(b))}
	if _, err := f.content.PublishReceived(f.ctx, f.scope, f.auth, providers.ReceivedPublication{ContentRef: ref, ObtainedAt: api.Time(time.Now()), ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}}, b); err != nil {
		t.Fatal(err)
	}
	intent := execution.ExecutionIntent{OperationID: api.NewID("operation"), TaskRef: f.scope.Ref(api.NewID("task"), 1), CapabilityRef: d.Capability().Ref, ArgumentsRef: ref, GoalRevision: 1, ControlRevision: 1, Deadline: api.Time(time.Now().Add(time.Minute)), TaskDeadline: api.Time(time.Now().Add(time.Hour)), ProcessedSourceRefs: []api.ContentRef{ref}, DisclosedSourceRefs: []api.ContentRef{ref}}
	intent.ProcessedSourceRefs = append(intent.ProcessedSourceRefs, extra...)
	intent.DisclosedSourceRefs = append(intent.DisclosedSourceRefs, extra...)
	invoke := execution.InvokeInput{OperationID: intent.OperationID, TaskRef: intent.TaskRef, CapabilityRef: intent.CapabilityRef, GoalRevision: 1, ControlRevision: 1, Deadline: intent.Deadline}
	p, err := d.Prepare(f.ctx, f.scope, f.auth, invoke, intent, b)
	if err != nil {
		t.Fatal(err)
	}
	return execution.AttemptRequest{Scope: f.scope, Auth: f.auth, Invoke: invoke, Intent: intent, Attempt: execution.Attempt{AttemptID: api.NewID("attempt"), OperationID: intent.OperationID, AttemptNo: 1, Prepared: p}}
}

func TestInformationSearchUsesExactDeclaredQueryAndPreservesFiniteCoverage(t *testing.T) {
	var sends atomic.Int64
	var request providers.SearchRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		body, _ := io.ReadAll(r.Body)
		if err := api.Decode(body, &request); err != nil || request.Query != "版本 7 的公开证据" || request.Limit != 2 {
			t.Errorf("actual declared query: %+v %v", request, err)
		}
		w.Header().Set("Content-Type", "application/json")
		next := "original-page-2"
		_, _ = w.Write(api.Raw(providers.SearchResponse{Protocol: "harness.information/1", RequestID: request.RequestID, Items: []providers.SearchItem{{URL: "https://public.example/articles/version", Title: "来源标题", Snippet: "版本信息来自该来源"}}, Coverage: "registered_source_index", Exhausted: false, Cursor: &next}))
	}))
	defer server.Close()
	f := newInformationFixture(t, server.URL)
	query := []byte("版本 7 的公开证据")
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(query), ByteLength: uint64(len(query)), MediaType: "text/plain"}
	if _, err := f.content.PublishReceived(f.ctx, f.scope, f.auth, providers.ReceivedPublication{ContentRef: ref, ObtainedAt: api.Time(time.Now()), ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}}, query); err != nil {
		t.Fatal(err)
	}
	source, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	r := f.request(t, source.SearchDriver(), providers.SearchArguments{QueryRef: ref, Limit: 2}, ref)
	out, err := source.SearchDriver().Start(f.ctx, r, func(context.Context) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	var observation providers.InformationObservation
	if err = api.Decode(out.Output, &observation); err != nil {
		t.Fatal(err)
	}
	if len(observation.Items) != 1 || observation.Items[0].Title != "来源标题" || observation.Coverage != "registered_source_index" || observation.Exhausted || observation.Cursor == nil || *observation.Cursor != "original-page-2" || request.RequestID != r.Intent.OperationID || observation.BodyRef == nil {
		t.Fatalf("finite actual search: %+v", observation)
	}
	if sends.Load() != 1 {
		t.Fatalf("physical search calls=%d", sends.Load())
	}
	bad := r
	bad.Intent.DisclosedSourceRefs = []api.ContentRef{r.Intent.ArgumentsRef}
	if _, err = source.SearchDriver().Prepare(f.ctx, f.scope, f.auth, bad.Invoke, bad.Intent, api.Raw(providers.SearchArguments{QueryRef: ref, Limit: 2})); !api.IsCode(err, "forbidden") {
		t.Fatalf("undeclared query disclosed: %v", err)
	}
	if sends.Load() != 1 {
		t.Fatal("Prepare rejection made HTTP request")
	}
}

func TestInformationLostReplyKeepsOriginalUnknownAndCannotClaimRemoteStop(t *testing.T) {
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	}))
	defer server.Close()
	f := newInformationFixture(t, server.URL)
	source, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	r := f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/lost"})
	out, err := source.BodyDriver().Start(f.ctx, r, func(context.Context) error { return nil })
	if err != nil || out.Effect != "unknown" || out.MayApplyLater != "unknown" {
		t.Fatalf("lost original response: %+v %v", out, err)
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopenDatabase(t)
	restored, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	for range 2 {
		got, err := restored.BodyDriver().Reconcile(f.ctx, r)
		if err != nil || !api.Equal(got, out) {
			t.Fatalf("original unknown: %+v %v", got, err)
		}
	}
	stopped, err := restored.BodyDriver().Stop(f.ctx, r)
	if err != nil || stopped.ActuallyStopped || stopped.MayApplyLater != "unknown" {
		t.Fatalf("unobserved remote stop claimed: %+v %v", stopped, err)
	}
	if sends.Load() != 1 {
		t.Fatalf("lost response retransmitted: %d", sends.Load())
	}
}

func TestReferenceAnswerCitesActualJSONAndKeepsSourceConflictAndAge(t *testing.T) {
	observed := api.Time(time.Now().Add(-time.Minute))
	recent := `{"facts":{"version":"7"},"observed_at":"` + observed + `"}`
	// 测试日期从实际取得的来源冻结，时效上限使用受信 DB 时间，不假装下载即更新事实。
	var sends atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "application/json")
		body := recent
		if r.URL.Path == "/articles/conflict" {
			body = `{"facts":{"version":"8"},"observed_at":"` + observed + `"}`
		} else if r.URL.Path == "/articles/stale" {
			body = `{"facts":{"version":"7"},"observed_at":"2025-01-01T00:00:00.000Z"}`
		}
		_, _ = io.WriteString(w, body)
	}))
	defer server.Close()
	f := newInformationFixture(t, server.URL)
	source, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	refs := []providers.InformationEvidenceRef{}
	for _, path := range []string{"recent", "conflict", "stale"} {
		r := f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/" + path})
		if _, err = source.BodyDriver().Start(f.ctx, r, func(context.Context) error { return nil }); err != nil {
			t.Fatal(err)
		}
		refs = append(refs, providers.InformationEvidenceRef{SourceRef: source.Descriptor().SourceRef, AttemptID: r.Attempt.AttemptID})
	}
	evaluator, err := providers.NewReferenceEvaluator(providers.ReferenceConfig{Store: f.store, Scope: f.scope, Reader: source})
	if err != nil {
		t.Fatal(err)
	}
	expected := "7"
	q := providers.ReferenceQuestion{Claims: []providers.ReferenceClaim{{Key: "version", JSONPointer: "/facts/version", ExpectedValue: &expected}}, Evidence: refs[:1], ObservedAtPointer: "/observed_at", MaxAgeSeconds: 7 * 24 * 3600}
	got, err := evaluator.Assess(f.ctx, f.scope, f.auth, q)
	if err != nil || got.Status != "supported" || len(got.Answers) != 1 || got.Answers[0].Value != "7" || len(got.Answers[0].Citations) != 1 || got.Answers[0].Citations[0].Quote != "7" || got.Answers[0].Citations[0].URL != server.URL+"/articles/recent" {
		t.Fatalf("actual cited answer: %+v %v", got, err)
	}
	q.Claims[0].ExpectedValue = nil
	got, err = evaluator.Assess(f.ctx, f.scope, f.auth, q)
	if err != nil || got.Status != "supported" || len(got.Answers) != 1 || got.Answers[0].Value != "7" {
		t.Fatalf("question need not invent the source answer: %+v %v", got, err)
	}
	q.Evidence = refs[:2]
	got, err = evaluator.Assess(f.ctx, f.scope, f.auth, q)
	if err != nil || got.Status != "conflict" || len(got.Answers) != 0 {
		t.Fatalf("conflicting source facts: %+v %v", got, err)
	}
	q.Evidence = refs[2:]
	got, err = evaluator.Assess(f.ctx, f.scope, f.auth, q)
	if err != nil || got.Status != "stale" || len(got.Answers) != 0 {
		t.Fatalf("retrieval cannot refresh source fact: %+v %v", got, err)
	}
	q.Evidence = refs[:1]
	q.Claims[0].JSONPointer = "/facts/unknown"
	got, err = evaluator.Assess(f.ctx, f.scope, f.auth, q)
	if err != nil || got.Status != "insufficient" || len(got.Answers) != 0 {
		t.Fatalf("unsupported answer: %+v %v", got, err)
	}
	if sends.Load() != 3 {
		t.Fatalf("assessment issued new requests: %d", sends.Load())
	}
}

func TestInformationCurrentAuthorityAndOriginalDeadlinesPreventPhysicalHTTP(t *testing.T) {
	var sends, barriers atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1) }))
	defer server.Close()
	f := newInformationFixture(t, server.URL)
	source, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	r := f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/denied"})
	f.gate.denied.Store(true)
	barrier := func(context.Context) error { barriers.Add(1); return nil }
	if _, err = source.BodyDriver().Start(f.ctx, r, barrier); !api.IsCode(err, "forbidden") {
		t.Fatalf("revoked source: %v", err)
	}
	f.gate.denied.Store(false)
	r = f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/expired"})
	r.Intent.Deadline = api.Time(time.Now().Add(-time.Second))
	if _, err = source.BodyDriver().Start(f.ctx, r, barrier); !api.IsCode(err, "expired") {
		t.Fatalf("original expired request: %v", err)
	}
	r = f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/unknown-barrier"})
	if _, err = source.BodyDriver().Start(f.ctx, r, func(context.Context) error { return runtime.ErrCommitUnknown }); err != runtime.ErrCommitUnknown {
		t.Fatalf("unknown barrier: %v", err)
	}
	if sends.Load() != 0 || barriers.Load() != 0 {
		t.Fatalf("unpermitted physical IO=%d barriers=%d", sends.Load(), barriers.Load())
	}
	if _, err = source.BodyDriver().Prepare(f.ctx, f.scope, f.auth, r.Invoke, r.Intent, api.Raw(map[string]any{"url": server.URL + "/articles/x", "grant": true})); err == nil {
		t.Fatal("undeclared body argument accepted")
	}
}

func TestInformationUnknownSendMarkerCommitStopsIOAndReopensOriginalIdentity(t *testing.T) {
	var sends atomic.Int64
	var armed atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { sends.Add(1) }))
	defer server.Close()
	f := newInformationFixture(t, server.URL, func(phase string) error {
		if phase == "after_commit" && armed.Swap(false) {
			return errors.New("injected actual send marker reply loss")
		}
		return nil
	})
	source, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/marker"})
	_, err = source.BodyDriver().Start(f.ctx, r, func(context.Context) error { armed.Store(true); return nil })
	if !errors.Is(err, runtime.ErrCommitUnknown) || sends.Load() != 0 {
		t.Fatalf("unknown marker advanced physical request: %v sends=%d", err, sends.Load())
	}
	if err = source.Close(); err != nil {
		t.Fatal(err)
	}
	f.reopenDatabase(t)
	restored, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := restored.BodyDriver().Reconcile(f.ctx, r)
	if err != nil || got.Effect != "unknown" || got.MayApplyLater != "unknown" {
		t.Fatalf("original marker recovery: %+v %v", got, err)
	}
	if _, err = restored.BodyDriver().Start(f.ctx, r, func(context.Context) error { t.Fatal("old send barrier repeated"); return nil }); err != nil {
		t.Fatal(err)
	}
	if sends.Load() != 0 {
		t.Fatal("unknown commit recovery sent a new HTTP request")
	}
}

func TestInformationTLSAndMetadataAddressGateUseActualApprovedOutlet(t *testing.T) {
	var sends, barriers atomic.Int64
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		w.Header().Set("Content-Type", "text/plain")
		_, _ = io.WriteString(w, "trusted TLS source")
	}))
	defer server.Close()
	f := newInformationFixture(t, server.URL)
	f.cfg.TLSRootPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: server.Certificate().Raw})
	source, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	r := f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/tls"})
	out, err := source.BodyDriver().Start(f.ctx, r, func(context.Context) error { barriers.Add(1); return nil })
	if err != nil || out.Effect != "applied" {
		t.Fatalf("actual verified TLS: %+v %v", out, err)
	}
	f.cfg.Source.Origin = "https://169.254.169.254"
	f.cfg.Source.AllowedCIDRs = []string{"169.254.169.254/32"}
	metadata, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer metadata.Close()
	r = f.request(t, metadata.BodyDriver(), providers.BodyArguments{URL: "https://169.254.169.254/articles/metadata"})
	if _, err = metadata.BodyDriver().Start(f.ctx, r, func(context.Context) error { barriers.Add(1); return nil }); !api.IsCode(err, "forbidden") {
		t.Fatalf("metadata address bypassed fixed CIDR gate: %v", err)
	}
	if sends.Load() != 1 || barriers.Load() != 1 {
		t.Fatalf("unapproved outlet sends=%d barriers=%d", sends.Load(), barriers.Load())
	}
}

func TestInformationRedirectAndBoundedPartialBytesCannotSupportAnswer(t *testing.T) {
	var sends, target atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		switch r.URL.Path {
		case "/articles/redirect":
			w.Header().Set("Location", "/outside")
			w.WriteHeader(http.StatusFound)
		case "/outside":
			target.Add(1)
		case "/articles/large":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"facts":{"version":"7"},"observed_at":"2026-10-03T00:00:00.000Z"}`)
		default:
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, "permission denied")
		}
	}))
	defer server.Close()
	f := newInformationFixture(t, server.URL)
	f.cfg.Source.MaxResponseBytes = 16
	source, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	refs := []providers.InformationEvidenceRef{}
	for _, path := range []string{"redirect", "large", "denied"} {
		r := f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/" + path})
		out, err := source.BodyDriver().Start(f.ctx, r, func(context.Context) error { return nil })
		if err != nil {
			t.Fatal(err)
		}
		var observation providers.InformationObservation
		if err = api.Decode(out.Output, &observation); err != nil {
			t.Fatal(err)
		}
		if observation.BodyRef == nil || observation.BodyRef.ByteLength > 16 || len(observation.Gaps) == 0 {
			t.Fatalf("bounded/failure source facts: %+v", observation)
		}
		if path == "large" && (!observation.Truncated || observation.Exhausted) {
			t.Fatalf("truncated source called complete: %+v", observation)
		}
		refs = append(refs, providers.InformationEvidenceRef{SourceRef: source.Descriptor().SourceRef, AttemptID: r.Attempt.AttemptID})
	}
	evaluator, err := providers.NewReferenceEvaluator(providers.ReferenceConfig{Store: f.store, Scope: f.scope, Reader: source})
	if err != nil {
		t.Fatal(err)
	}
	q := providers.ReferenceQuestion{Claims: []providers.ReferenceClaim{{Key: "version", JSONPointer: "/facts/version"}}, Evidence: refs, MaxAgeSeconds: 3600, ObservedAtPointer: "/observed_at"}
	got, err := evaluator.Assess(f.ctx, f.scope, f.auth, q)
	if err != nil || got.Status != "retrieval_failed" || len(got.Answers) != 0 {
		t.Fatalf("failed/partial retrieval supported: %+v %v", got, err)
	}
	if sends.Load() != 3 || target.Load() != 0 {
		t.Fatalf("physical=%d redirected=%d", sends.Load(), target.Load())
	}
	registry := runtime.NewRegistry()
	f.content.service.Register(registry)
	dispatcher := runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: registry}
	obs, _, err := source.ReadEvidence(f.ctx, f.scope, f.auth, refs[1])
	if err != nil {
		t.Fatal(err)
	}
	one := uint64(1)
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), TargetID: obs.BodyRef.ContentID, Method: "content.close", ExpectedRevision: &one, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: *obs.BodyRef, Reason: "close actual acquired bytes"})}
	if receipt, err := dispatcher.Command(f.ctx, f.auth, api.Raw(c)); err != nil || receipt.Stage != "applied" {
		t.Fatalf("close current source: %+v %v", receipt, err)
	}
	q.Evidence = refs[1:2]
	if _, err = evaluator.Assess(f.ctx, f.scope, f.auth, q); !api.IsCode(err, "forbidden") {
		t.Fatalf("closed bytes still cited: %v", err)
	}
}

func TestInformationBodyPublishesActualBytesAndRecoversOriginalAttempt(t *testing.T) {
	const literal = "公开信息：版本 7，于来源实际取得。\n"
	var sends, barriers atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		if r.Method != "GET" || r.Header.Get("X-Harness-Attempt-ID") == "" {
			t.Error("original request identity missing")
		}
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = io.WriteString(w, literal)
	}))
	defer server.Close()
	f := newInformationFixture(t, server.URL)
	source, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer source.Close()
	r := f.request(t, source.BodyDriver(), providers.BodyArguments{URL: server.URL + "/articles/version"})
	if sends.Load() != 0 {
		t.Fatal("constructor/Prepare made HTTP request")
	}
	barrier := func(ctx context.Context) error {
		barriers.Add(1)
		status, err := f.store.Within(ctx, f.scope, []string{"providers"}, func(tx runtime.Tx) error {
			return tx.Create(ctx, "providers.test_barriers", r.Attempt.AttemptID, r.Intent.OperationID, struct {
				Confirmed bool `json:"confirmed"`
			}{true})
		})
		if status == runtime.CommitUnknown {
			return runtime.ErrCommitUnknown
		}
		return err
	}
	out, err := source.BodyDriver().Start(f.ctx, r, barrier)
	if err != nil {
		t.Fatal(err)
	}
	var observation providers.InformationObservation
	if err = api.Decode(out.Output, &observation); err != nil {
		t.Fatal(err)
	}
	if out.Effect != "applied" || out.MayApplyLater != false || !out.UsageFinal || observation.BodyRef == nil || observation.ObtainedAt == "" || observation.URL != server.URL+"/articles/version" || len(observation.Gaps) != 0 {
		t.Fatalf("actual source: %+v %+v", out, observation)
	}
	bytes, err := f.content.service.Read(f.ctx, f.scope, f.auth, *observation.BodyRef, "task.goal")
	if err != nil || string(bytes) != literal {
		t.Fatalf("actual original bytes: %q %v", bytes, err)
	}
	restored, err := providers.NewHTTPInformation(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Close()
	got, err := restored.BodyDriver().Reconcile(f.ctx, r)
	if err != nil || !api.Equal(out, got) {
		t.Fatalf("original recovery: %+v %v", got, err)
	}
	got, err = restored.BodyDriver().Start(f.ctx, r, barrier)
	if err != nil || !api.Equal(out, got) {
		t.Fatalf("duplicate original attempt: %+v %v", got, err)
	}
	if sends.Load() != 1 || barriers.Load() != 1 {
		t.Fatalf("physical=%d barrier=%d", sends.Load(), barriers.Load())
	}
	changed := r
	changed.Attempt.AttemptID = api.NewID("attempt")
	if _, err = restored.BodyDriver().Start(f.ctx, changed, barrier); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("one operation acquired a second physical attempt: %v", err)
	}
	if sends.Load() != 1 {
		t.Fatal("duplicate operation replayed with new AttemptID")
	}
}
