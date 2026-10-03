package memory_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

type queryRecheckAuthority struct {
	checks  uint64
	failure error
}

func (a *queryRecheckAuthority) Check(context.Context, runtime.Tx, runtime.Auth, api.ComponentRef, string, string, bool) (uint64, error) {
	a.checks++
	return 1, a.failure
}

func (*queryRecheckAuthority) Visibility(context.Context, runtime.Tx, runtime.Auth) (string, error) {
	return "original-current-authority", nil
}

func TestFrozenInputRecheckPreservesTraversalAndUnderlyingErrors(t *testing.T) {
	testFrozenInputRecheckErrors(t, newFixture)
}

func testFrozenInputRecheckErrors(t *testing.T, createFixture func(*testing.T) fixture) {
	t.Helper()
	for _, kind := range []string{"permission_budget", "context_cancelled", "sql_deadlock"} {
		t.Run(kind, func(t *testing.T) {
			f := createFixture(t)
			for i := 0; i < 2; i++ {
				id := api.NewID("memory")
				if receipt := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: f.values(t, "checked query")}); receipt.Stage != "applied" {
					t.Fatalf("create: %+v", receipt)
				}
			}
			text := f.upload(t, "checked query")
			query := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: text, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
			in := memory.QueryInput{QueryRef: query, ScopeRef: f.upload(t, "原当前查询范围"), Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 10000, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 1}
			authority := &queryRecheckAuthority{}
			f.service.Authorization = authority
			page, err := f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in)
			if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
				t.Fatalf("original page: %+v %v", page, err)
			}
			if kind == "permission_budget" {
				// 许可端口实际计数给出首轮消费上限；新查询只预留这一轮检查。
				in.Limits.MaxPermissionChecks = authority.checks
				page, err = f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in)
				if err != nil || len(page.Items) != 1 || page.NextCursor == "" {
					t.Fatalf("bounded first page: %+v %v", page, err)
				}
			} else if kind == "context_cancelled" {
				authority.failure = context.Canceled
			} else {
				authority.failure = &pgconn.PgError{Code: "40P01", Message: "injected current authority deadlock"}
			}
			in.Cursor = page.NextCursor
			next, err := f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in)
			if kind == "permission_budget" {
				if err != nil || len(next.Items) != 0 || next.Exhausted || !next.Partial || next.NextCursor != in.Cursor || len(next.Gaps) == 0 {
					t.Fatalf("unchecked candidates falsely exhausted: %+v %v", next, err)
				}
			} else if !errors.Is(err, authority.failure) {
				t.Fatalf("underlying current source failure replaced: %v", err)
			}
		})
	}
}

func TestMemoryPagesUseOriginalQueryBindingDeadline(t *testing.T) {
	for _, kind := range []string{"query", "list"} {
		t.Run(kind, func(t *testing.T) {
			f := newFixture(t)
			for i := 0; i < 2; i++ {
				id := api.NewID("memory")
				if r := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: f.values(t, "query binding deadline")}); r.Stage != "applied" {
					t.Fatalf("create: %+v", r)
				}
			}
			text := f.upload(t, "query binding")
			spec := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: text, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
			scope := f.upload(t, "准确范围")
			queryID := api.NewID("query")
			binder := f.service.Store.(runtime.QueryBindingStore)
			binding, status, err := binder.BindQuery(f.ctx, f.scope, runtime.QueryBindingInput{QueryID: queryID, PrincipalID: f.auth.SubjectID, CredentialGeneration: f.auth.CredentialGeneration, RolesDigest: api.Hash(api.Raw(f.auth.Roles)), QueryDigest: api.Hash([]byte("original " + kind)), TTL: time.Minute})
			if err != nil || status != runtime.Committed {
				t.Fatalf("actual original binding: %+v %s %v", binding, status, err)
			}
			expires, _ := api.ParseTime(binding.ExpiresAt)
			now := expires.Add(-time.Minute)
			f.service.Store = clockStore{Store: f.service.Store, now: &now}
			ctx := runtime.WithQueryBinding(f.ctx, binding)
			var cursor string
			in := memory.QueryInput{QueryRef: spec, ScopeRef: scope, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 10000, Deadline: api.Time(now.Add(4 * time.Minute))}, Limit: 1}
			if kind == "query" {
				page, e := f.service.QueryMemory(ctx, f.scope, f.auth, queryID, in)
				err = e
				cursor = page.NextCursor
			} else {
				page, e := f.service.ListMemory(ctx, f.scope, f.auth, memory.ListMemoryInput{Purpose: "memory.read", Limit: 1})
				err = e
				cursor = page.NextCursor
			}
			if err != nil || cursor == "" {
				t.Fatalf("first bounded page: %s %v", cursor, err)
			}
			// 时间端口前进，原查询期限不因更晚的绑定或新分页请求延长。
			now = expires.Add(time.Second)
			later := binding
			later.ExpiresAt = api.Time(now.Add(5 * time.Minute))
			ctx = runtime.WithQueryBinding(f.ctx, later)
			if kind == "query" {
				in.Cursor = cursor
				_, err = f.service.QueryMemory(ctx, f.scope, f.auth, api.NewID("query"), in)
			} else {
				_, err = f.service.ListMemory(ctx, f.scope, f.auth, memory.ListMemoryInput{Purpose: "memory.read", Limit: 1, Cursor: cursor})
			}
			if !api.IsCode(err, "cursor_expired") {
				t.Fatalf("old page outlived original query binding: %v", err)
			}
		})
	}
}

func TestFrozenQueryPageRechecksActualQuerySourceRetentionWithoutTimerProjection(t *testing.T) {
	testFrozenQueryPageSourceRetention(t, newFixture)
}

func testFrozenQueryPageSourceRetention(t *testing.T, createFixture func(*testing.T) fixture) {
	t.Helper()
	for _, source := range []string{"text", "query", "scope"} {
		t.Run(source, func(t *testing.T) {
			f := createFixture(t)
			for i := 0; i < 2; i++ {
				id := api.NewID("memory")
				if receipt := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: f.values(t, "retention query")}); receipt.Stage != "applied" {
					t.Fatalf("create: %+v", receipt)
				}
			}
			expires := time.Now().Add(2 * time.Minute)
			upload := func(kind, body string) api.ContentRef {
				if source == kind {
					return f.uploadUntil(t, body, expires, expires)
				}
				return f.upload(t, body)
			}
			text := upload("text", "retention query")
			spec := upload("query", string(api.Raw(memory.MemoryQuerySpec{TextRef: text, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
			scope := upload("scope", "当前查询使用范围")
			now := time.Now()
			f.service.Store = clockStore{Store: f.service.Store, now: &now}
			input := memory.QueryInput{QueryRef: spec, ScopeRef: scope, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 10000, Deadline: api.Time(now.Add(4 * time.Minute))}, Limit: 1}
			page, err := f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), input)
			if err != nil || page.NextCursor == "" {
				t.Fatalf("original page: %+v %v", page, err)
			}
			// 到期 Job 没有运行；当前权限必须直接查来源保留期，不能借旧水位披露。
			now = expires.Add(time.Second)
			input.Cursor = page.NextCursor
			if _, err = f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), input); !api.IsCode(err, "snapshot_required") {
				t.Fatalf("expired %s input still used by frozen ranking: %v", source, err)
			}
		})
	}
}
