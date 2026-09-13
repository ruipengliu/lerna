package extractioncheck

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"lerna/adapters/localextractionsource"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func TestSourceChangeRetiresCandidateBeforePolicyReplacementAndCannotRestoreIt(t *testing.T) {
	checkSourceChange(t, "")
}
func TestSourceChangeWaitsForKnownInvalidationBeforeActivatingReplacement(t *testing.T) {
	for _, mode := range []string{"before", "after"} {
		t.Run(mode, func(t *testing.T) { checkSourceChange(t, mode) })
	}
}
func checkSourceChange(t *testing.T, failure string) {
	t.Helper()
	ctx := context.Background()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	store, _ := bindProcessSave(t, h, "")
	r, grant, err := h.request(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = h.client.Invoke(ctx, r, grant); err != nil {
		t.Fatal(err)
	}
	if out, e := h.exec.Run(ctx, r.OperationID); e != nil || out.Result != "SUCCESS" {
		t.Fatalf("original extraction: %+v %v", out, e)
	}
	old := localextractionsource.Entry{Ref: &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}, Path: filepath.Join(h.root, "source.txt"), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("回答时，我偏好简洁的说明。"))), Speaker: "operator", Method: "authenticated-note", Fragment: "paragraph:1", Restrictions: extraction.Restrictions{Storage: []string{"local"}, Processing: []string{"local"}, Recipients: []string{"local"}, Purposes: []string{"task"}, RetainUntil: h.now().Add(time.Hour).Unix()}}
	next := old
	next.Ref = &wire.ContentSource{Kind: "note", Key: "one", Revision: 2}
	next.Restrictions.RetainUntil = h.now().Add(5 * time.Minute).Unix()
	event := extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}
	want := 1
	if failure != "" {
		if _, e := h.rawSource.Change(ctx, sourceChangeFailure{ChangeFence: h.candidates, mode: failure}, event, &next); e != memory.Unavailable {
			t.Fatalf("invalidation failure: %v", e)
		}
		current, e := h.rawSource.Current(ctx, extraction.SourceScope{Kind: "note", Key: "one"}, "local", "task")
		if e != nil || current.Revision != 1 {
			t.Fatalf("unknown invalidation activated replacement: %v %v", current, e)
		}
		_, _, e = h.source.Read(ctx, []*wire.ContentSource{old.Ref}, "local", "task")
		if failure == "after" {
			want = 0
			if e != memory.Denied {
				t.Fatalf("committed fence lost with reply: %v", e)
			}
		} else if e != nil {
			t.Fatalf("failed precommit changed old source: %v", e)
		}
	}
	if n, e := h.rawSource.Change(ctx, h.candidates, event, &next); e != nil || n != want {
		t.Fatalf("source change: %d %v", n, e)
	}
	state, err := h.candidates.Inspect(ctx, "local", r.OperationID)
	if err != nil || state.State != "retired" || !state.Committed {
		t.Fatalf("old candidate not retired: %+v %v", state, err)
	}
	intent, err := h.candidates.LookupSave(ctx, "local", "operator", r.OperationID)
	if err != nil || intent.State != "retired" || intent.Request.Spec != nil {
		t.Fatalf("save intent retained old body: %+v %v", intent, err)
	}
	if _, _, err = h.source.Read(ctx, []*wire.ContentSource{next.Ref}, "local", "task"); err != nil {
		t.Fatalf("new revision not enabled: %v", err)
	}
	if n, e := h.rawSource.Change(ctx, h.candidates, event, &next); e != nil || n != 0 {
		t.Fatalf("source change replay: %d %v", n, e)
	}
	if err = h.rawSource.Replace([]localextractionsource.Entry{old}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = h.source.Read(ctx, []*wire.ContentSource{old.Ref}, "local", "task"); err != memory.Denied {
		t.Fatalf("restored old policy revived source: %v", err)
	}
	cleaner := bindProcessCleanup(t, h, store, "", h.candidates)
	out, err := cleaner.Clean(ctx, r.OperationID)
	if err != nil || out.State != "committed" {
		t.Fatalf("source change cleanup: %+v %v", out, err)
	}
	if _, err = store.Read(ctx, memory.Ref{Namespace: "local", Collection: "personal", Key: r.OperationID}, 1); err != memory.Missing {
		t.Fatalf("derived Memory body remains: %v", err)
	}
	event.ThroughRevision = 2
	if _, err = h.rawSource.Change(ctx, h.candidates, event, nil); err != nil {
		t.Fatal(err)
	}
	if _, err = h.rawSource.Current(ctx, extraction.SourceScope{Kind: "note", Key: "one"}, "local", "task"); err != memory.Missing {
		t.Fatalf("deleted source remains registered: %v", err)
	}
}

type sourceChangeFailure struct {
	localextractionsource.ChangeFence
	mode string
}

func (f sourceChangeFailure) InvalidateSource(ctx context.Context, event extraction.SourceInvalidation) (int, error) {
	if f.mode == "after" {
		if _, err := f.ChangeFence.InvalidateSource(ctx, event); err != nil {
			return 0, err
		}
	}
	return 0, memory.Unavailable
}
