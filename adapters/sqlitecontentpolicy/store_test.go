package sqlitecontentpolicy_test

import (
	"context"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/sqlitecontentpolicy"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"testing"
	"time"
)

func TestSourcePolicyUpdatesReachOtherOpenConsumersAndSurviveReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "policy.db")
	store, err := sqlitecontentpolicy.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	now := time.Now().Unix()
	rules := []contentpolicy.Rule{{Kind: "web", Key: "original", Revision: 1, Actions: []string{"process"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: now + 60}}
	first, err := contentpolicy.NewPersistent(ctx, store, rules)
	if err != nil {
		t.Fatal(err)
	}
	otherStore, err := sqlitecontentpolicy.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer otherStore.Close()
	// A second startup's defaults cannot replace the existing trusted rules.
	second, err := contentpolicy.NewPersistent(ctx, otherStore, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := &wire.ContentSource{Kind: "web", Key: "original", Revision: 1}
	if err = second.Check(ctx, source, "process", "task", "local", now); err != nil {
		t.Fatal("second initialization replaced the saved policy")
	}
	before, err := first.Revision(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	if err = second.Replace(nil); err != nil {
		t.Fatal(err)
	}
	if err = first.Check(ctx, source, "process", "task", "local", now); err == nil {
		t.Fatal("open consumer retained stale source permissions")
	}
	revoked, err := first.Revision(ctx, now)
	if err != nil || revoked == before {
		t.Fatal("revocation did not advance the durable policy revision")
	}
	store.Close()
	restoredStore, err := sqlitecontentpolicy.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer restoredStore.Close()
	restored, err := contentpolicy.OpenPersistent(ctx, restoredStore)
	if err != nil {
		t.Fatal(err)
	}
	after, err := restored.Revision(ctx, now)
	if err != nil || after != revoked {
		t.Fatal("reopen changed the current revoked policy")
	}
	if err = restored.Check(ctx, source, "process", "task", "local", now); err == nil {
		t.Fatal("reopen restored a revoked source")
	}
}
