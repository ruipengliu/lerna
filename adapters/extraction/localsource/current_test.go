package localsource_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"testing"

	localextractionsource "lerna/adapters/extraction/localsource"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func TestCurrentRevisionNeverFallsBackToOlderPermittedMetadata(t *testing.T) {
	ctx := context.Background()
	scope := extraction.SourceScope{Kind: "note", Key: "one"}
	old := localextractionsource.Entry{Ref: &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}, Path: filepath.Join(t.TempDir(), "not-opened.txt"), SHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("private body"))), Speaker: "alice", Method: "authenticated-note", Fragment: "paragraph:1", Restrictions: extraction.Restrictions{Storage: []string{"device-a"}, Processing: []string{"device-a"}, Recipients: []string{"device-a"}, Purposes: []string{"assist"}, RetainUntil: 1900001000}}
	latest := old
	latest.Ref = &wire.ContentSource{Kind: "note", Key: "one", Revision: 2}
	latest.Restrictions.Recipients = []string{"device-b"}
	source, err := localextractionsource.New([]localextractionsource.Entry{old, latest}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = source.Current(ctx, scope, "device-a", "assist"); err != memory.Denied {
		t.Fatalf("older revision escaped latest policy: %v", err)
	}
	latest.Restrictions.Recipients = []string{"device-a"}
	if err = source.Replace([]localextractionsource.Entry{old, latest}); err != nil {
		t.Fatal(err)
	}
	ref, err := source.Current(ctx, scope, "device-a", "assist")
	if err != nil || ref.Revision != 2 {
		t.Fatalf("current metadata: %v %v", ref, err)
	}
	// No file exists: polling is only manifest metadata; it must not read body.
	ref.Revision = 99
	again, err := source.Current(ctx, scope, "device-a", "assist")
	if err != nil || again.Revision != 2 {
		t.Fatalf("returned metadata mutated manifest: %v %v", again, err)
	}
	if _, _, err = source.Read(ctx, []*wire.ContentSource{again}, "device-a", "assist"); err != memory.Denied {
		t.Fatalf("metadata observation admitted absent body: %v", err)
	}
}
