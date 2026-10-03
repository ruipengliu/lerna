package memory_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

type forbiddenReadProbe struct {
	memory.ObjectStore
	forbiddenID string
	reads       atomic.Int64
}

func (p *forbiddenReadProbe) Read(ctx context.Context, loc memory.ObjectLocation, ref api.ContentRef, max uint64) ([]byte, error) {
	if ref.ContentID == p.forbiddenID {
		p.reads.Add(1)
		return nil, api.E("dependency_unavailable", "forbidden_candidate_processed")
	}
	return p.ObjectStore.Read(ctx, loc, ref, max)
}

func TestQueryNeverReadsUnauthorizedContentBeforeRanking(t *testing.T) {
	f := newFixture(t)
	other := f
	other.auth.SubjectID = api.NewID("subject")
	values := f.policy.Values
	values.Subjects = []string{other.auth.SubjectID}
	digest, err := api.Digest(values)
	if err != nil {
		t.Fatal(err)
	}
	other.policy, err = memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.service.InstallPolicy(f.ctx, f.scope, f.auth, other.policy); err != nil {
		t.Fatal(err)
	}
	secretValues := other.values(t, "classified 秘密词")
	id := api.NewID("memory")
	r := other.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: secretValues})
	if r.Stage != "applied" {
		t.Fatalf("private create: %+v", r)
	}
	text := f.upload(t, "classified 秘密词")
	spec := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: text, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
	scope := f.upload(t, "本人查询范围")
	probe := &forbiddenReadProbe{ObjectStore: f.service.Objects, forbiddenID: secretValues.ContentRef.ContentID}
	f.service.Objects = probe
	page, err := f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), memory.QueryInput{QueryRef: spec, ScopeRef: scope, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 1000, Deadline: api.Time(time.Now().Add(time.Minute))}})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 0 || probe.reads.Load() != 0 {
		t.Fatalf("unauthorized content participated: items=%d byte_reads=%d", len(page.Items), probe.reads.Load())
	}
}

func TestContentChecksTenantOwnerSubjectPurposeAndLocation(t *testing.T) {
	f := newFixture(t)
	ref := f.upload(t, "准确授权正文")
	if _, err := f.service.Read(f.ctx, f.scope, f.auth, ref, "task.goal"); err != nil {
		t.Fatal(err)
	}
	otherAuth := f.auth
	otherAuth.SubjectID = api.NewID("subject")
	if _, err := f.service.Read(f.ctx, f.scope, otherAuth, ref, "task.goal"); !api.IsCode(err, "forbidden") {
		t.Fatalf("other subject: %v", err)
	}
	wrongTenant := ref
	wrongTenant.TenantID = api.NewID("tenant")
	if _, err := f.service.Read(f.ctx, f.scope, f.auth, wrongTenant, "task.goal"); !api.IsCode(err, "forbidden") {
		t.Fatalf("wrong tenant: %v", err)
	}
	unreachable := ref
	unreachable.OwnerID = api.NewID("owner")
	if _, err := f.service.Read(f.ctx, f.scope, f.auth, unreachable, "task.goal"); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("unreachable original owner: %v", err)
	}
	if _, err := f.service.Read(f.ctx, f.scope, f.auth, ref, "undeclared.export"); !api.IsCode(err, "forbidden") {
		t.Fatalf("undeclared purpose: %v", err)
	}
	if _, err := f.service.ReadBytes(f.ctx, f.scope, f.auth, ref, "task.goal", "another_device"); !api.IsCode(err, "forbidden") {
		t.Fatalf("undeclared location: %v", err)
	}
	changed := ref
	changed.Hash = api.Hash([]byte("同键换正文"))
	if _, err := f.service.Read(f.ctx, f.scope, f.auth, changed, "task.goal"); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("changed exact reference: %v", err)
	}
}

type authority struct {
	revision    atomic.Uint64
	unavailable atomic.Bool
}

func (a *authority) Check(context.Context, runtime.Tx, runtime.Auth, api.ComponentRef, string, string, bool) (uint64, error) {
	if a.unavailable.Load() {
		return 0, api.E("dependency_unavailable", "grant_authority_unavailable")
	}
	return a.revision.Load(), nil
}
func (a *authority) Visibility(context.Context, runtime.Tx, runtime.Auth) (string, error) {
	if a.unavailable.Load() {
		return "", api.E("dependency_unavailable", "grant_authority_unavailable")
	}
	return fmt.Sprint(a.revision.Load()), nil
}

func TestExternalPermissionExpansionInvalidatesFrozenQueryWithoutMemoryChange(t *testing.T) {
	f := newFixture(t)
	a := &authority{}
	a.revision.Store(1)
	f.service.Authorization = a
	for _, text := range []string{"北京天气之一", "北京天气之二"} {
		id := api.NewID("memory")
		r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: f.values(t, text)})
		if r.Stage != "applied" {
			t.Fatalf("create: %+v", r)
		}
	}
	text := f.upload(t, "北京天气")
	spec := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: text, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
	scope := f.upload(t, "当前许可查询范围")
	in := memory.QueryInput{QueryRef: spec, ScopeRef: scope, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 1000, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 1}
	page, err := f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in)
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == "" {
		t.Fatal("expected another frozen page")
	}
	a.revision.Store(2)
	in.Cursor = page.NextCursor
	if _, err = f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in); !api.IsCode(err, "snapshot_required") {
		t.Fatalf("expanded authority reused old cursor: %v", err)
	}
	a.unavailable.Store(true)
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, text, "memory.query"); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("unreachable current authority served bytes: %v", err)
	}
}

type clockStore struct {
	runtime.Store
	now *time.Time
}
type clockTx struct {
	runtime.Tx
	now time.Time
}

func (t clockTx) Now(context.Context) (time.Time, error) { return t.now, nil }
func (s clockStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error { return fn(clockTx{Tx: tx, now: *s.now}) })
}

func TestReadsDoNotExtendRetentionOrFrozenListTTL(t *testing.T) {
	f := newFixture(t)
	ref := f.upload(t, "不得滑动延长保存期")
	id := api.NewID("memory")
	r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: f.values(t, "固定分页TTL记录")})
	if r.Stage != "applied" {
		t.Fatalf("create: %+v", r)
	}
	second := api.NewID("memory")
	r = f.command(t, "memory.create", second, nil, memory.CreateInput{MemoryID: second, Values: f.values(t, "另一条固定分页记录")})
	if r.Stage != "applied" {
		t.Fatalf("create: %+v", r)
	}
	now := time.Now()
	f.service.Store = clockStore{Store: f.service.Store, now: &now}
	page, err := f.service.ListMemory(f.ctx, f.scope, f.auth, memory.ListMemoryInput{Purpose: "memory.read", Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if page.NextCursor == "" {
		t.Fatal("expected next fixed page")
	}
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, ref, "task.goal"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(6 * time.Minute)
	if _, err = f.service.ListMemory(f.ctx, f.scope, f.auth, memory.ListMemoryInput{Purpose: "memory.read", Limit: 1, Cursor: page.NextCursor}); !api.IsCode(err, "cursor_expired") {
		t.Fatalf("old frozen cursor renewed TTL: %v", err)
	}
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, ref, "task.goal"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(35 * time.Minute)
	if _, err = f.service.Read(f.ctx, f.scope, f.auth, ref, "task.goal"); !api.IsCode(err, "gone") {
		t.Fatalf("read extended retention: %v", err)
	}
}

func TestRequestedReadLocationCannotOverrideProcessingLocation(t *testing.T) {
	f := newFixture(t)
	ref := f.upload(t, "仅许可本地处理的准确字节")
	f.service.Location = "device"
	if _, err := f.service.ReadBytes(f.ctx, f.scope, f.auth, ref, "task.goal", "local"); !api.IsCode(err, "forbidden") {
		t.Fatalf("caller location bypassed current processing location: %v", err)
	}
}

func TestCopyReportsRequireCurrentHolderAndExistingAccurateEvidence(t *testing.T) {
	f := newFixture(t)
	ref := f.upload(t, "本人副本")
	holder := f.auth.Ref(f.scope.OwnerID)
	holder.Revision++
	id := api.NewID("copy")
	in := memory.RegisterCopyInput{CopyID: id, ContentRef: ref, HolderRef: holder, Purpose: "memory.read", Location: "local", RetainUntil: api.Time(time.Now().Add(time.Minute)), ReferenceIntentRef: f.scope.Ref(api.NewID("intent"), 1)}
	r := f.command(t, "content.register_copy", ref.ContentID, nil, in)
	if r.Stage != "rejected" || r.Error == nil || r.Error.Code != "forbidden" {
		t.Fatalf("stale/future holder generation accepted: %+v", r)
	}
	in.HolderRef = f.auth.Ref(f.scope.OwnerID)
	r = f.command(t, "content.register_copy", ref.ContentID, nil, in)
	if r.Stage != "applied" {
		t.Fatalf("register current copy: %+v", r)
	}
	evidence := ref
	evidence.ContentID = api.NewID("content")
	report := memory.ReleaseCopyInput{CopyID: id, ContentRef: ref, ControlRevision: 1, UseStopped: true, CleanupState: "complete", EvidenceRefs: []api.ContentRef{evidence}}
	r = f.command(t, "content.release_copy", ref.ContentID, nil, report)
	if r.Stage != "rejected" {
		t.Fatalf("nonexistent cleanup evidence accepted: %+v", r)
	}
	report.EvidenceRefs = []api.ContentRef{f.upload(t, "持有者清理回执")}
	r = f.command(t, "content.release_copy", ref.ContentID, nil, report)
	if r.Stage != "applied" {
		t.Fatalf("accurate cleanup receipt rejected: %+v", r)
	}
}
