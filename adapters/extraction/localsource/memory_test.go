package localsource_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	localextractionsource "lerna/adapters/extraction/localsource"
	memoryauth "lerna/adapters/memory/auth"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"os"
	"path/filepath"
	"testing"
)

func TestMemorySourceChecksCurrentFileAndEachResidencyDimension(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "note.txt")
	body := []byte("回答时，我偏好中文。")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	ref := &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}
	entry := localextractionsource.Entry{Ref: ref, Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Speaker: "alice", Method: "authenticated-note", Fragment: "paragraph:1",
		Restrictions: extraction.Restrictions{Storage: []string{"disk"}, Processing: []string{"cpu"}, Recipients: []string{"viewer"}, Purposes: []string{"assist"}, RetainUntil: 1900001000}}
	source, err := localextractionsource.New([]localextractionsource.Entry{entry}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	var policy memoryauth.Sources = source
	for _, c := range []struct{ action, location string }{{"store", "disk"}, {"store_reference", "disk"}, {"process", "cpu"}, {"discover", "viewer"}, {"disclose", "viewer"}} {
		t.Run(c.action, func(t *testing.T) {
			if err := policy.Check(ctx, ref, c.action, "assist", c.location, 1900000500); err != nil {
				t.Fatal(err)
			}
			for _, bad := range []struct {
				action, purpose, location string
				until                     int64
			}{
				{c.action, "assist", "cloud", 1900000500},
				{c.action, "advertising", c.location, 1900000500},
				{c.action, "assist", c.location, 1900001001},
				{c.action, "assist", c.location, 1900000000},
				{"scan", "assist", c.location, 1900000500},
			} {
				if err := policy.Check(ctx, ref, bad.action, bad.purpose, bad.location, bad.until); err != memory.Denied {
					t.Fatalf("out of scope: %v", err)
				}
			}
		})
	}
	if err := os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := policy.Check(ctx, ref, "store", "assist", "disk", 1900000500); err != memory.Denied {
		t.Fatalf("changed revision accepted: %v", err)
	}
}
