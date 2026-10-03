package memory_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
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
