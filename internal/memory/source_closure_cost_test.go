package memory_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 只观测真实Runtime强读：每次Get原样交给SQLite/PG，不替换锁、裁决时间或提交。
type closureReadStore struct {
	runtime.Store
	contentReads atomic.Uint64
	policyReads  atomic.Uint64
}
type closureReadTx struct {
	runtime.Tx
	store *closureReadStore
}

func (s *closureReadStore) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error { return fn(closureReadTx{Tx: tx, store: s}) })
}
func (t closureReadTx) Get(ctx context.Context, namespace, id string, value any) (uint64, error) {
	if namespace == "content.versions" {
		t.store.contentReads.Add(1)
	}
	if namespace == "content.policies" {
		t.store.policyReads.Add(1)
	}
	return t.Tx.Get(ctx, namespace, id, value)
}

type closureAuthorization struct {
	checks  atomic.Uint64
	purpose string
	check   func(api.ComponentRef, uint64) error
}

func (a *closureAuthorization) Check(_ context.Context, _ runtime.Tx, _ runtime.Auth, policy api.ComponentRef, purpose, _ string, _ bool) (uint64, error) {
	if purpose == "task.goal" || a.purpose != "" && purpose == a.purpose {
		count := a.checks.Add(1)
		if a.check != nil {
			if err := a.check(policy, count); err != nil {
				return 0, err
			}
		}
	}
	return 1, nil
}

func TestSharedSourceClosureQueryBudgetStillPaysForEveryPermissionPath(t *testing.T) {
	for _, database := range []string{"sqlite", "postgres"} {
		t.Run(database, func(t *testing.T) {
			f := closureFixture(t, database)
			shared := f.upload(t, "bounded query")
			left := f.upload(t, "query source branch one", shared)
			right := f.upload(t, "query source branch two", shared)
			query := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: shared, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})), left, right, shared)
			authority := &closureAuthorization{purpose: "memory.query"}
			f.service.Authorization = authority
			in := memory.QueryInput{QueryRef: query, ScopeRef: shared, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 10, MaxReadBytes: 1024, MaxPermissionChecks: 5, Deadline: api.Time(time.Now().Add(time.Minute))}}
			// 4个节点和5条边需要6次当前许可，元数据复用不能让只有5次的预算通过。
			_, err := f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in)
			var failure *api.Error
			if !errors.As(err, &failure) || failure.Code != "overloaded" || failure.Reason != "query_permission_budget" {
				t.Fatalf("metadata reuse reduced the original permission budget: %v", err)
			}
			if authority.checks.Load() != 5 {
				t.Fatalf("permission budget consumption changed: %d", authority.checks.Load())
			}
		})
	}
}

// 真实读取准确原字节后插入一个公开控制动作，复核仍使用真实独立 Tx。
type closureAfterRead struct {
	memory.ObjectStore
	after func()
	reads atomic.Uint64
}

func (o *closureAfterRead) Read(ctx context.Context, loc memory.ObjectLocation, ref api.ContentRef, max uint64) ([]byte, error) {
	body, err := o.ObjectStore.Read(ctx, loc, ref, max)
	if err == nil && o.reads.Add(1) == 1 {
		o.after()
	}
	return body, err
}

func TestSharedSourceClosureIsRecheckedAfterActualBodyReadAndOnTheNextEntry(t *testing.T) {
	for _, database := range []string{"sqlite", "postgres"} {
		t.Run(database, func(t *testing.T) {
			f := closureFixture(t, database)
			shared := f.upload(t, "original source to withdraw")
			left := f.upload(t, "left exact derivation", shared)
			right := f.upload(t, "right exact derivation", shared)
			root := f.upload(t, "exact bytes that cannot escape withdrawal", left, right, shared)
			objects := &closureAfterRead{ObjectStore: f.service.Objects, after: func() {
				one := uint64(1)
				r := f.command(t, "content.close", shared.ContentID, &one, memory.CloseInput{ContentRef: shared, Reason: "withdraw the original source after actual read"})
				if r.Stage != "applied" {
					t.Fatalf("original withdrawal: %+v", r)
				}
			}}
			f.service.Objects = objects
			if body, err := f.service.Read(f.ctx, f.scope, f.auth, root, "task.goal"); len(body) != 0 || !api.IsCode(err, "forbidden") {
				t.Fatalf("post-read current closure leaked bytes: len=%d err=%v", len(body), err)
			}
			if body, err := f.service.Read(f.ctx, f.scope, f.auth, root, "task.goal"); len(body) != 0 || !api.IsCode(err, "forbidden") {
				t.Fatalf("new entry reused the pre-withdrawal allowance: len=%d err=%v", len(body), err)
			}
			if objects.reads.Load() != 1 {
				t.Fatalf("withdrawn source reached another physical body read: %d", objects.reads.Load())
			}
			t.Logf("original tenant=%s owner=%s source=%s root=%s actual_body_reads=1 returned_bytes=0", f.scope.TenantID, f.scope.OwnerID, shared.ContentID, root.ContentID)
		})
	}
}

func TestSharedSourceClosureStillChecksCurrentAuthorityOnRepeatedPaths(t *testing.T) {
	for _, database := range []string{"sqlite", "postgres"} {
		t.Run(database, func(t *testing.T) {
			f := closureFixture(t, database)
			shared := f.upload(t, "shared original with current authority")
			left := f.upload(t, "first path", shared)
			right := f.upload(t, "second path", shared)
			root := f.upload(t, "must check second original-source path", left, right)
			failure := api.E("dependency_unavailable", "current_source_authority_unavailable")
			authority := &closureAuthorization{check: func(_ api.ComponentRef, count uint64) error {
				if count == 5 { // 第二条 shared 路径，已经读过并锁定该准确元数据。
					return failure
				}
				return nil
			}}
			f.service.Authorization = authority
			if body, err := f.service.Read(f.ctx, f.scope, f.auth, root, "task.goal"); len(body) != 0 || !errors.Is(err, failure) {
				t.Fatalf("repeated path skipped current authority or replaced its failure: len=%d err=%v", len(body), err)
			}
			if authority.checks.Load() != 5 {
				t.Fatalf("changed permission path consumption: %d", authority.checks.Load())
			}
		})
	}
}

func TestSharedSourceClosureRejectsChangedAccurateReferenceOnAnotherEntryInTheSameTx(t *testing.T) {
	for _, database := range []string{"sqlite", "postgres"} {
		t.Run(database, func(t *testing.T) {
			f := closureFixture(t, database)
			shared := f.upload(t, "exact original reference")
			root := f.upload(t, "exact root reference", shared)
			status, err := f.service.Store.Within(f.ctx, f.scope, memory.Participants, func(tx runtime.Tx) error {
				if _, err := f.service.CheckContentTx(f.ctx, tx, f.auth, root, "task.goal", "local", false); err != nil {
					return err
				}
				changed := root
				changed.Hash = api.Hash([]byte("different bytes under the same original identity"))
				_, err := f.service.CheckContentTx(f.ctx, tx, f.auth, changed, "task.goal", "local", false)
				return err
			})
			if !api.IsCode(err, "idempotency_conflict") || status != runtime.RolledBack {
				t.Fatalf("same-Tx new entry borrowed an allowance for changed exact bytes: %s %v", status, err)
			}
		})
	}
}

// 只替换已批准的可信时间端口；所有 Get、来源门禁及提交仍交给原数据库。
type closureMovingClock struct {
	runtime.Store
	nanos atomic.Int64
}
type closureMovingClockTx struct {
	runtime.Tx
	clock *closureMovingClock
}

func (s *closureMovingClock) Within(ctx context.Context, scope runtime.Scope, participants []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, participants, func(tx runtime.Tx) error { return fn(closureMovingClockTx{Tx: tx, clock: s}) })
}
func (t closureMovingClockTx) Now(context.Context) (time.Time, error) {
	return time.Unix(0, t.clock.nanos.Load()).UTC(), nil
}

func TestSharedSourceClosureRetainsPathSpecificHistoricalAndCurrentTimeChecks(t *testing.T) {
	for _, database := range []string{"sqlite", "postgres"} {
		for _, order := range []string{"historical_then_current", "current_then_historical"} {
			t.Run(database+"/"+order, func(t *testing.T) {
				f := closureFixture(t, database)
				ordinary := f.policy
				values := ordinary.Values
				values.IndependentDerived = true
				digest, err := api.Digest(values)
				if err != nil {
					t.Fatal(err)
				}
				independent, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
				if err != nil {
					t.Fatal(err)
				}
				if err = f.service.InstallPolicy(f.ctx, f.scope, f.auth, independent); err != nil {
					t.Fatal(err)
				}
				f.policy = independent
				now := time.Now()
				expires := now.Add(2 * time.Minute)
				shared := f.uploadUntil(t, "literal original with finite retention", expires, now.Add(time.Minute))
				left := f.uploadUntil(t, "independently retained derivation", expires.Add(time.Minute), now.Add(time.Minute), shared)
				f.policy = ordinary
				sources := []api.ContentRef{left, shared}
				if order == "current_then_historical" {
					sources = []api.ContentRef{shared, left}
				}
				root := f.uploadUntil(t, "ordinary finite root", expires, now.Add(time.Minute), sources...)
				clock := &closureMovingClock{Store: f.service.Store}
				clock.nanos.Store(now.UnixNano())
				f.service.Store = clock
				firstShared := uint64(3)
				if order == "current_then_historical" {
					firstShared = 2
				}
				f.service.Authorization = &closureAuthorization{check: func(policy api.ComponentRef, count uint64) error {
					if policy == independent.PolicyRef && count == firstShared {
						clock.nanos.Store(expires.Add(time.Second).UnixNano())
					}
					return nil
				}}
				status, err := clock.Within(f.ctx, f.scope, memory.Participants, func(tx runtime.Tx) error {
					v, err := f.service.CheckContentTx(f.ctx, tx, f.auth, root, "task.goal", "local", false)
					if err == nil && v.ContentRef != root {
						t.Fatal("changed original accurate reference")
					}
					return err
				})
				if order == "historical_then_current" {
					if !api.IsCode(err, "gone") || status != runtime.RolledBack {
						t.Fatalf("historical visit bypassed later ordinary-source expiry: %s %v", status, err)
					}
				} else if err != nil || status != runtime.Committed {
					t.Fatalf("current visit suppressed independent historical path: %s %v", status, err)
				}
				// 下一入口不可使用本次遍历起始时尚有效的 root 时间事实。
				if body, err := f.service.Read(f.ctx, f.scope, f.auth, root, "task.goal"); len(body) != 0 || !api.IsCode(err, "gone") {
					t.Fatalf("new entry retained old time permission: len=%d err=%v", len(body), err)
				}
				t.Logf("original tenant=%s owner=%s root=%s shared=%s original_retention=%s order=%s", f.scope.TenantID, f.scope.OwnerID, root.ContentID, shared.ContentID, api.Time(expires), order)
			})
		}
	}
}
func (*closureAuthorization) Visibility(context.Context, runtime.Tx, runtime.Auth) (string, error) {
	return "explicit-local-test-authority-generation-1", nil
}

func closureFixture(t *testing.T, database string) fixture {
	t.Helper()
	ctx := context.Background()
	var store runtime.Store
	var migrate func(context.Context) error
	if database == "postgres" {
		dsn := os.Getenv("HARNESS_TEST_POSTGRES_DSN")
		if dsn == "" {
			t.Skip("HARNESS_TEST_POSTGRES_DSN required for actual PostgreSQL evidence")
		}
		pg, err := postgres.Open(ctx, dsn, postgres.WithMaxConnections(8))
		if err != nil {
			t.Fatal(err)
		}
		store, migrate = pg, pg.Migrate
	} else {
		local, err := sqlite.Open(filepath.Join(t.TempDir(), "source-closure.sqlite"))
		if err != nil {
			t.Fatal(err)
		}
		store, migrate = local, local.Migrate
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := migrate(ctx); err != nil {
		t.Fatal(err)
	}
	return newFixtureWithStore(t, store)
}

func TestSharedSourceClosureHasBoundedStrongReadsAndStillChecksEveryPermissionPath(t *testing.T) {
	for _, database := range []string{"sqlite", "postgres"} {
		t.Run(database, func(t *testing.T) {
			f := closureFixture(t, database)
			shared := f.upload(t, "exact shared original source")
			left := f.upload(t, "left derivation", shared)
			right := f.upload(t, "right derivation", shared)
			root := f.upload(t, "literal result from both derivations", left, right, shared)
			store := &closureReadStore{Store: f.service.Store}
			authority := &closureAuthorization{}
			f.service.Store, f.service.Authorization = store, authority
			body, err := f.service.Read(f.ctx, f.scope, f.auth, root, "task.goal")
			if err != nil || string(body) != "literal result from both derivations" {
				t.Fatalf("accurate publicly read root: %q %v", body, err)
			}
			// 两个读取门禁各有4个准确节点和1份政策。其5条来源边仍带来
			// 每入口6次当前许可核验，不能把重复数据库读取消除变成权限肯定缓存。
			contentReads, policyReads := store.contentReads.Load(), store.policyReads.Load()
			t.Logf("original tenant=%s owner=%s root=%s shared=%s strong_content_reads=%d strong_policy_reads=%d current_permission_checks=%d", f.scope.TenantID, f.scope.OwnerID, root.ContentID, shared.ContentID, contentReads, policyReads, authority.checks.Load())
			if contentReads+policyReads > 10 {
				t.Fatalf("shared paths repeated locked metadata reads beyond the two-entry node bound: content=%d policy=%d", contentReads, policyReads)
			}
			if authority.checks.Load() != 12 {
				t.Fatalf("current permission paths were skipped: %d", authority.checks.Load())
			}
		})
	}
}
