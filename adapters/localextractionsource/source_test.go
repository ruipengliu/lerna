package localextractionsource_test

import (
	"context"
	"crypto/sha256"
	"fmt"
	"lerna/adapters/localextractionsource"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{}

func (clock) Now() (time.Time, error) { return time.Unix(1900000000, 0), nil }
func TestCurrentFileAndRestrictionsGovernSourceRead(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "note.txt")
	body := []byte("回答时，我偏好中文。")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	ref := &wire.ContentSource{Kind: "note", Key: "one", Revision: 1}
	bounds := extraction.Restrictions{Storage: []string{"device-a"}, Processing: []string{"device-a"}, Recipients: []string{"device-a"}, Purposes: []string{"assist"}, RetainUntil: 1900001000}
	entry := localextractionsource.Entry{Ref: ref, Path: path, SHA256: fmt.Sprintf("%x", sha256.Sum256(body)), Speaker: "alice", Method: "authenticated-note", Fragment: "paragraph:1", Restrictions: bounds}
	source, err := localextractionsource.New([]localextractionsource.Entry{entry}, clock{})
	if err != nil {
		t.Fatal(err)
	}
	materials, limits, err := source.Read(ctx, []*wire.ContentSource{ref}, "device-a", "assist")
	if err != nil || len(materials) != 1 || materials[0].Text != string(body) || limits[0].RetainUntil != 1900001000 {
		t.Fatalf("read: %+v %v", materials, err)
	}
	if _, _, err = source.Read(ctx, []*wire.ContentSource{ref}, "cloud", "assist"); err != memory.Denied {
		t.Fatalf("cloud: %v", err)
	}
	if err = source.Validate(ctx, []*wire.ContentSource{ref}, bounds); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	if err = source.Validate(ctx, []*wire.ContentSource{ref}, bounds); err != memory.Denied {
		t.Fatalf("changed file: %v", err)
	}
	if err = os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	entry.Restrictions.RetainUntil = 1900000500
	if err = source.Replace([]localextractionsource.Entry{entry}); err != nil {
		t.Fatal(err)
	}
	if err = source.Validate(ctx, []*wire.ContentSource{ref}, bounds); err != memory.Denied {
		t.Fatalf("tightened retention: %v", err)
	}
	if err = source.Replace(nil); err != nil {
		t.Fatal(err)
	}
	if _, _, err = source.Read(ctx, []*wire.ContentSource{ref}, "device-a", "assist"); err != memory.Denied {
		t.Fatalf("removed source: %v", err)
	}
}
