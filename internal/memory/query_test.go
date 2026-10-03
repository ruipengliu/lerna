package memory_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

func TestFrozenLexicalQueryExplainsChineseAndEnglishAndRejectsPermissionChange(t *testing.T) {
	f := newFixture(t)
	ids := []string{api.NewID("memory"), api.NewID("memory")}
	for i, text := range []string{"北京 weather tomorrow", "北京 weather today"} {
		r := f.command(t, "memory.create", ids[i], nil, memory.CreateInput{MemoryID: ids[i], Values: f.values(t, text)})
		if r.Stage != "applied" {
			t.Fatalf("create: %+v", r)
		}
	}
	textRef := f.upload(t, "北京 weather")
	specRef := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: textRef, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
	scopeRef := f.upload(t, "本人授权查询范围")
	in := memory.QueryInput{QueryRef: specRef, ScopeRef: scopeRef, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 1000, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 1}
	page, err := f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.NextCursor == "" || page.Exhausted || len(page.Items[0].Explanation) != 2 {
		t.Fatalf("frozen lexical first page: %+v", page)
	}
	one := uint64(1)
	r := f.command(t, "memory.delete", ids[0], &one, memory.DeleteInput{MemoryID: ids[0], Reason: "删除导致披露范围变化"})
	if r.Stage != "applied" {
		t.Fatalf("delete: %+v", r)
	}
	in.Cursor = page.NextCursor
	if _, err = f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in); !api.IsCode(err, "snapshot_required") {
		t.Fatalf("old query revived after visibility change: %v", err)
	}
}

func TestLexicalQueryRejectsUnboundedExplanationTerms(t *testing.T) {
	f := newFixture(t)
	words := make([]string, 101)
	for i := range words {
		words[i] = fmt.Sprintf("word%03d", i)
	}
	text := f.upload(t, strings.Join(words, " "))
	spec := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: text, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
	in := memory.QueryInput{QueryRef: spec, ScopeRef: f.upload(t, "有限查询用途"), Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 1000, Deadline: api.Time(time.Now().Add(time.Minute))}}
	if _, err := f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in); !api.IsCode(err, "invalid_request") {
		t.Fatalf("more explanation terms than the closed output permits accepted: %v", err)
	}
}

func TestFrozenQueryPaginatesMaximumCandidatesWithLongExplanations(t *testing.T) {
	testFrozenQueryPaginatesMaximumCandidatesWithLongExplanations(t, newFixture)
}

func testFrozenQueryPaginatesMaximumCandidatesWithLongExplanations(t *testing.T, createFixture func(*testing.T) fixture) {
	f := createFixture(t)
	text := strings.Repeat("a", 4096-len("literal:"))
	values := f.values(t, text)
	for range 200 {
		id := api.NewID("memory")
		if receipt := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values}); receipt.Stage != "applied" {
			t.Fatalf("create: %+v", receipt)
		}
	}
	for _, profile := range []api.ComponentRef{memory.LexicalProfile(), memory.LiteralProfile()} {
		t.Run(profile.Version, func(t *testing.T) {
			textRef := f.upload(t, text)
			specRef := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: textRef, TypeFilter: []string{}, RankingProfileRef: profile})))
			in := memory.QueryInput{QueryRef: specRef, ScopeRef: values.ScopeRef, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 100000, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 20}
			queryID := api.NewID("query")
			seen := map[string]bool{}
			for pageNumber := 0; pageNumber < 10; pageNumber++ {
				wireQueryID := queryID
				if pageNumber != 0 {
					wireQueryID = api.NewID("query")
				}
				page := f.queryMemoryPage(t, wireQueryID, in)
				if len(page.Items) != 20 || page.Partial || page.Exhausted != (pageNumber == 9) {
					t.Fatalf("page %d: count=%d partial=%t exhausted=%t", pageNumber, len(page.Items), page.Partial, page.Exhausted)
				}
				if _, err := api.Canonical(api.Raw(page)); err != nil {
					t.Fatalf("public page exceeds its JSON bound: %v", err)
				}
				if pageNumber == 0 && !api.Equal(page, f.queryMemoryPage(t, wireQueryID, in)) {
					t.Fatal("original query retry changed the sealed result")
				}
				for _, match := range page.Items {
					if seen[match.MemoryRef.ObjectID] || len(match.Explanation) != 1 || !strings.HasSuffix(match.Explanation[0], text) {
						t.Fatalf("duplicate match or explanation changed for %s", match.MemoryRef.ObjectID)
					}
					seen[match.MemoryRef.ObjectID] = true
				}
				in.Cursor = page.NextCursor
			}
			if len(seen) != 200 || in.Cursor != "" {
				t.Fatalf("frozen candidates not traversed: %d cursor=%q", len(seen), in.Cursor)
			}
		})
	}
}

func TestFrozenLiteralQueryKeepsPublicPagesWithinJSONBound(t *testing.T) {
	testFrozenLiteralQueryKeepsPublicPagesWithinJSONBound(t, newFixture)
}

func testFrozenLiteralQueryKeepsPublicPagesWithinJSONBound(t *testing.T, createFixture func(*testing.T) fixture) {
	f := createFixture(t)
	text := strings.Repeat("\x01", 4096-len("literal:"))
	values := f.values(t, text)
	for range 20 {
		id := api.NewID("memory")
		if receipt := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values}); receipt.Stage != "applied" {
			t.Fatalf("create: %+v", receipt)
		}
	}
	textRef := f.upload(t, text)
	specRef := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: textRef, TypeFilter: []string{}, RankingProfileRef: memory.LiteralProfile()})))
	in := memory.QueryInput{QueryRef: specRef, ScopeRef: values.ScopeRef, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 100000, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 20}
	seen := map[string]bool{}
	for pageNumber := 0; pageNumber < 20; pageNumber++ {
		page := f.queryMemoryPage(t, api.NewID("query"), in)
		if len(page.Items) == 0 || page.Partial || pageNumber == 0 && (len(page.Items) >= 20 || page.Exhausted) {
			t.Fatalf("page %d: count=%d partial=%t exhausted=%t", pageNumber, len(page.Items), page.Partial, page.Exhausted)
		}
		if _, err := api.Canonical(api.Raw(page)); err != nil {
			t.Fatalf("public page exceeds its JSON bound: %v", err)
		}
		for _, match := range page.Items {
			if seen[match.MemoryRef.ObjectID] || len(match.Explanation) != 1 || match.Explanation[0] != "literal:"+text {
				t.Fatalf("duplicate match or explanation changed for %s", match.MemoryRef.ObjectID)
			}
			seen[match.MemoryRef.ObjectID] = true
		}
		if page.Exhausted {
			if len(seen) != 20 || page.NextCursor != "" {
				t.Fatalf("frozen candidates not traversed: %d cursor=%q", len(seen), page.NextCursor)
			}
			return
		}
		in.Cursor = page.NextCursor
	}
	t.Fatal("bounded query did not finish traversing its frozen candidates")
}

func (f fixture) queryMemoryPage(t *testing.T, queryID string, in memory.QueryInput) api.Page[memory.Match] {
	t.Helper()
	query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: queryID, Method: "memory.query", TargetID: f.scope.OwnerID, Payload: api.Raw(in)}
	raw, err := f.dispatcher.Query(f.ctx, f.auth, api.Raw(query))
	if err != nil {
		t.Fatal(err)
	}
	var page api.Page[memory.Match]
	if err = api.Decode(raw, &page); err != nil {
		t.Fatal(err)
	}
	return page
}

func TestFrozenQueryRejectsSnapshotPartDigestChanges(t *testing.T) {
	testFrozenQueryRejectsSnapshotPartDigestChanges(t, newFixture)
}

func testFrozenQueryRejectsSnapshotPartDigestChanges(t *testing.T, createFixture func(*testing.T) fixture) {
	for _, kind := range []string{"source", "match"} {
		t.Run(kind, func(t *testing.T) {
			f := createFixture(t)
			values := f.values(t, "frozen accurate term")
			for range 2 {
				id := api.NewID("memory")
				if receipt := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values}); receipt.Stage != "applied" {
					t.Fatalf("create: %+v", receipt)
				}
			}
			text := f.upload(t, "frozen accurate")
			spec := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: text, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
			in := memory.QueryInput{QueryRef: spec, ScopeRef: values.ScopeRef, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 100000, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 1}
			queryID := api.NewID("query")
			page := f.queryMemoryPage(t, queryID, in)
			if len(page.Items) != 1 || page.NextCursor == "" {
				t.Fatal("no original first page")
			}
			// 持久快照清单损坏的恢复反例，不通过业务入口修改原正文或许可。
			status, err := f.service.Store.Within(f.ctx, f.scope, []string{"memory"}, func(tx runtime.Tx) error {
				var view memory.QueryView
				revision, err := tx.Get(f.ctx, "memory.queries", queryID, &view)
				if err != nil {
					return err
				}
				if kind == "source" {
					view.SourceParts[0].Digest = api.Hash([]byte("different reference"))
				} else {
					view.MatchParts[1].Digest = api.Hash([]byte("different frozen match"))
				}
				return tx.Put(f.ctx, "memory.queries", queryID, revision, view)
			})
			if err != nil || status != runtime.Committed {
				t.Fatalf("persist actual damaged snapshot manifest: %s %v", status, err)
			}
			in.Cursor = page.NextCursor
			if _, err = f.service.QueryMemory(f.ctx, f.scope, f.auth, api.NewID("query"), in); !api.IsCode(err, "invalid_state") {
				t.Fatalf("different snapshot piece digest accepted: %v", err)
			}
		})
	}
}

func TestFrozenQueryReservesRoomForLaterPermissionGaps(t *testing.T) {
	testFrozenQueryReservesRoomForLaterPermissionGaps(t, newFixture)
}

func testFrozenQueryReservesRoomForLaterPermissionGaps(t *testing.T, createFixture func(*testing.T) fixture) {
	f := createFixture(t)
	text := strings.Repeat("a", 4096-len("literal:"))
	queryID := api.NewID("query")
	ids := []string{api.NewID("memory"), api.NewID("memory"), api.NewID("memory"), api.NewID("memory"), api.NewID("memory"), api.NewID("memory")}
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte(text)), ByteLength: uint64(len(text)), MediaType: "text/a"}
	probe := api.Page[memory.Match]{Items: []memory.Match{}, CollectionRevision: 6, Gaps: []string{}, NextCursor: queryID + ":" + strings.Repeat("a", 64) + ":3"}
	for _, id := range ids[:3] {
		probe.Items = append(probe.Items, memory.Match{MemoryRef: f.scope.Ref(id, 1), ContentRef: ref, Score: 1, Explanation: []string{"term:" + text}})
	}
	// 合法 ContentRef 原字段使三项回复落在 256 KiB 边缘，留下不足一个 gap 的空间。
	additional := (api.MaxJSONBytes - len(api.Raw(probe)) - 1) / 3
	ref.MediaType = "text/" + strings.Repeat("a", 1+additional)
	policyDeadline, _ := api.ParseTime(f.policy.Values.RetainUntil)
	_, err := f.service.Upload(f.ctx, f.scope, f.auth, memory.PublicationRequest{ContentRef: ref, TransferID: api.NewID("upload"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: f.policy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(policyDeadline.Add(-20 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(10 * time.Minute))}, []byte(text))
	if err != nil {
		t.Fatal(err)
	}
	values := memory.MemoryValues{Type: "fact", ContentRef: ref, Sources: []api.SourceEvidence{}, ScopeRef: f.upload(t, "原有限查询范围"), PolicyRef: f.policy.PolicyRef, ObservedAt: api.Time(time.Now().Add(-time.Hour))}
	for _, id := range ids {
		if receipt := f.command(t, "memory.create", id, nil, memory.CreateInput{MemoryID: id, Values: values}); receipt.Stage != "applied" {
			t.Fatalf("create: %+v", receipt)
		}
	}
	textRef := f.upload(t, text)
	specRef := f.upload(t, string(api.Raw(memory.MemoryQuerySpec{TextRef: textRef, TypeFilter: []string{}, RankingProfileRef: memory.LexicalProfile()})))
	in := memory.QueryInput{QueryRef: specRef, ScopeRef: values.ScopeRef, Purposes: []string{"memory.query"}, Limits: memory.QueryLimits{MaxCandidates: 200, MaxReadBytes: 1 << 20, MaxPermissionChecks: 100000, Deadline: api.Time(time.Now().Add(4 * time.Minute))}, Limit: 5}
	authority := &queryRecheckAuthority{}
	f.service.Authorization = authority
	page := f.queryMemoryPage(t, queryID, in)
	if len(page.Items) != 3 || page.Exhausted || page.NextCursor == "" {
		t.Fatal("no original bounded three-match page")
	}
	var original memory.QueryView
	if _, err = f.service.Store.Read(f.ctx, f.scope, "memory.queries", queryID, 1, &original); err != nil {
		t.Fatal(err)
	}
	before := authority.checks
	if _, err = f.service.ReadMemory(f.ctx, f.scope, f.auth, memory.ReadMemoryInput{MemoryID: ids[0]}); err != nil {
		t.Fatal(err)
	}
	checksPerMemory := authority.checks - before
	// 原 SQL 快照给出冻结前真实消费；另预留准确三个来源和三项当前许可，再多一个检查。
	// 第四项发生累计预算不足时，已发三项的 JSON 也必须仍然合法。
	in.Limits.MaxPermissionChecks = 100000 - original.RemainingPermissionChecks + uint64(len(original.SourceParts)) + 3*uint64(1+len(in.Purposes))*checksPerMemory + 1
	page = f.queryMemoryPage(t, api.NewID("query"), in)
	if len(page.Items) != 3 || page.Exhausted || page.NextCursor == "" {
		t.Fatalf("permission tail changed first page: items=%d exhausted=%t partial=%t gaps=%v bytes=%d budget=%d perMemory=%d frozenSpent=%d", len(page.Items), page.Exhausted, page.Partial, page.Gaps, len(api.Raw(page)), in.Limits.MaxPermissionChecks, checksPerMemory, 100000-original.RemainingPermissionChecks)
	}
	if _, err = api.Canonical(api.Raw(page)); err != nil {
		t.Fatalf("later permission metadata exceeded the public byte bound: %v", err)
	}
	in.Cursor = page.NextCursor
	next := f.queryMemoryPage(t, api.NewID("query"), in)
	if len(next.Items) != 0 || !next.Partial || next.Exhausted || next.NextCursor != in.Cursor || len(next.Gaps) != 1 || next.Gaps[0] != "permission_budget" {
		t.Fatalf("actual remaining budget did not preserve the unvisited cursor: count=%d partial=%t exhausted=%t gaps=%v", len(next.Items), next.Partial, next.Exhausted, next.Gaps)
	}
}
