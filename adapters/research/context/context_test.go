package context_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	researchcontext "lerna/adapters/research/context"
	"lerna/answers"
	"lerna/brain"
	"lerna/contextassembly"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"lerna/websearch"
	"testing"
)

type pageReader func(context.Context, string) (fetch.Result, error)

func (f pageReader) Read(ctx context.Context, ref string) (fetch.Result, error) { return f(ctx, ref) }

type searchReader func(context.Context, string) (websearch.Result, error)

func (f searchReader) Read(ctx context.Context, ref string) (websearch.Result, error) {
	return f(ctx, ref)
}

func TestAssemblyPreservesObservationBudgetAndRechecksEarlierSources(t *testing.T) {
	ref := func(key string) string {
		return answers.Reference(&wire.ContentRef{Namespace: "local", Key: fmt.Sprintf("%x", sha256.Sum256([]byte(key))), Revision: 1})
	}
	p1, p2, s1, s2 := ref("page1"), ref("page2"), ref("search1"), ref("search2")
	task := tasks.Task{Ref: tasks.Ref{Namespace: "local", TaskID: "task"}, Subject: "operator", Goal: "question"}
	for _, tc := range []struct {
		name          string
		refs          researchcontext.References
		first, last   string
		reads, blocks int
	}{
		{"one_page", researchcontext.References{Pages: []string{p1}}, p1, p1, 1, 1},
		{"two_pages", researchcontext.References{Pages: []string{p1, p2}}, p1, p2, 4, 2},
		{"one_search", researchcontext.References{Search: []string{s1}}, s1, s1, 1, 1},
		{"search_and_page", researchcontext.References{Search: []string{s1}, Pages: []string{p1}}, s1, p1, 4, 2},
		{"two_searches_and_page", researchcontext.References{Search: []string{s1, s2}, Pages: []string{p1}}, s1, p1, 8, 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			reads, revoke, revoked := 0, false, false
			observe := func(ref string) error {
				reads++
				if revoked && ref == tc.first {
					return fetch.Denied
				}
				if revoke && ref == tc.last {
					revoked = true
				}
				return nil
			}
			pages := pageReader(func(_ context.Context, ref string) (fetch.Result, error) {
				return fetch.Result{Body: []byte("page evidence")}, observe(ref)
			})
			search := searchReader(func(_ context.Context, ref string) (websearch.Result, error) {
				return websearch.Result{}, observe(ref)
			})
			c, err := researchcontext.New(pages, search, nil, task, "local", tc.refs)
			if err != nil {
				t.Fatal(err)
			}
			input, err := c.Assemble(ctx, task, "local", brain.MaxInputBytes)
			if err != nil || reads != tc.reads || len(input.Blocks) != tc.blocks {
				t.Fatalf("observation budget changed: reads=%d blocks=%d err=%v", reads, len(input.Blocks), err)
			}
			changed := task
			changed.Goal = "replacement question"
			if _, err = c.Assemble(ctx, changed, "local", brain.MaxInputBytes); err != contextassembly.Invalidated || reads != tc.reads {
				t.Fatal("changed task facts reached evidence I/O")
			}
			if tc.blocks > 1 {
				revoke = true
				input, err = c.Assemble(ctx, task, "local", brain.MaxInputBytes)
				if err != fetch.Denied || len(input.Blocks) != 0 {
					t.Fatal("revocation during assembly leaked earlier evidence")
				}
			}
		})
	}
}
