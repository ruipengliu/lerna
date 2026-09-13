package contextassembly_test

import (
	"context"
	"crypto/sha256"
	"lerna/adapters/memorycleanup"
	"lerna/adapters/sqlitecontext"
	"lerna/contextassembly"
	"lerna/memory"
	"path/filepath"
	"testing"
)

func TestExactErasureKeepsOtherRevisionSnapshotsAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "context.db")
	store, err := sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	assembly, err := contextassembly.New(store, facts{}, &sources{text: processBody, retain: true})
	if err != nil {
		t.Fatal(err)
	}
	first := processRequest()
	first.Candidates[0].Reference.Revision = 2
	second := processRequest()
	second.Key.TaskID = "independent-revision"
	second.Candidates[0].Reference.Revision = 3
	earlier := processRequest()
	earlier.Key.TaskID = "earlier-independent"
	for _, request := range []contextassembly.Request{first, second, earlier} {
		if _, err = assembly.Assemble(ctx, request); err != nil {
			t.Fatal(err)
		}
		snapshot, e := store.Read(ctx, request.Key)
		if e != nil {
			t.Fatal(e)
		}
		if e = store.BindCheckpoint(ctx, request.Key, sha256.Sum256(snapshot.Document)); e != nil {
			t.Fatal(e)
		}
	}
	preserved, err := store.Read(ctx, second.Key)
	if err != nil {
		t.Fatal(err)
	}
	sink, err := memorycleanup.NewContexts(store)
	if err != nil {
		t.Fatal(err)
	}
	event := memory.SourceEvent{Ref: memory.Ref{Namespace: "local", Collection: "personal", Key: "style"}, Kind: memory.SourceErased, Revision: 2, Position: 4}
	if complete, e := sink.Apply(ctx, event); e != nil || !complete {
		t.Fatalf("exact context cleanup: %v %v", complete, e)
	}
	if snapshot, e := store.Read(ctx, first.Key); e != contextassembly.Invalidated || len(snapshot.Document) != 0 || snapshot.SemanticSHA256 != "" {
		t.Fatalf("old snapshot retained: %+v %v", snapshot, e)
	}
	if e := store.VerifyCheckpoint(ctx, first.Key); e != contextassembly.Invalidated {
		t.Fatalf("old checkpoint retained: %v", e)
	}
	if snapshot, e := store.Read(ctx, second.Key); e != nil || string(snapshot.Document) != string(preserved.Document) {
		t.Fatalf("independent revision damaged: %v", e)
	}
	if e := store.VerifyCheckpoint(ctx, second.Key); e != nil {
		t.Fatalf("independent checkpoint damaged: %v", e)
	}
	if _, e := store.Read(ctx, earlier.Key); e != nil {
		t.Fatalf("earlier independent revision damaged: %v", e)
	}
	if e := store.VerifyCheckpoint(ctx, earlier.Key); e != nil {
		t.Fatalf("earlier checkpoint damaged: %v", e)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqlitecontext.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	sink, err = memorycleanup.NewContexts(store)
	if err != nil {
		t.Fatal(err)
	}
	if complete, e := sink.Apply(ctx, event); e != nil || !complete {
		t.Fatalf("replay: %v %v", complete, e)
	}
	assembly, err = contextassembly.New(store, facts{}, &sources{text: processBody, retain: true})
	if err != nil {
		t.Fatal(err)
	}
	first.Key.TaskID = "fresh-old-source"
	if _, e := assembly.Assemble(ctx, first); e != contextassembly.Invalidated {
		t.Fatalf("exact fence bypassed by new decision: %v", e)
	}
	second.Key.TaskID = "fresh-independent"
	if _, e := assembly.Assemble(ctx, second); e != nil {
		t.Fatalf("other revision blocked: %v", e)
	}
}
