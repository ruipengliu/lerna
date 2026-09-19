package authorization_test

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	contextpolicy "lerna/adapters/context/policy"
	sqlitecontext "lerna/adapters/context/sqlite"
	"lerna/authorization"
	"lerna/contextassembly"
	wire "lerna/gen/harness/v1"
)

type failedRetirement struct {
	contextassembly.RetirementStore
}

func (failedRetirement) Retire(context.Context, contextassembly.Key) error {
	return contextassembly.Unavailable
}

type failedContextConfirmation struct{ contextpolicy.Authority }

func (failedContextConfirmation) ConfirmUseCleaned(context.Context, string, string, string, authorization.UseNotice) error {
	return &authorization.Error{Code: authorization.Unavailable}
}

func checkContextPolicyCleanup(t *testing.T, auth *authorization.Service, token string, scope *wire.AuthorizationScope, policy func(*wire.AuthorizationScope) uint64, now time.Time) {
	t.Helper()
	ctx := context.Background()
	store, err := sqlitecontext.Open(filepath.Join(t.TempDir(), "contexts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	config := contextpolicy.Config{Namespace: "local", Consumer: "contexts", ConfigSHA256: strings.Repeat("c", 64), Batch: 16, Timeout: 5 * time.Second}
	adapter, err := contextpolicy.New(auth, store, config)
	if err != nil {
		t.Fatal(err)
	}
	key := contextassembly.Key{Namespace: "local", TaskID: "context-use", Decision: 1}
	spec := authorization.UseSpec{Until: now.Add(30 * time.Second).Unix(), Actions: []*wire.AuthorizationAction{{Resource: "root", Action: "content.store", Purpose: "assist", Location: "local"}}}
	if err := adapter.Prepare(ctx, token, key, spec); err != nil {
		t.Fatal(err)
	}
	if err := adapter.Validate(ctx, key); err != nil {
		t.Fatal(err)
	}
	snapshot := contextassembly.Snapshot{Key: key, Subject: "admin", SemanticSHA256: strings.Repeat("d", 64), Document: []byte(`{"body":"public retained snapshot"}`)}
	if _, err := store.Bind(ctx, snapshot); err != nil {
		t.Fatal(err)
	}
	narrow := proto.Clone(scope).(*wire.AuthorizationScope)
	narrow.Purposes = []string{"research"}
	policy(narrow)
	policy(scope)
	if err := adapter.Validate(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("restored permission released old context: %v", err)
	}
	faulty, err := contextpolicy.New(auth, failedRetirement{store}, config)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := faulty.Clean(ctx); err != contextassembly.Unavailable {
		t.Fatalf("cleanup failure: %v", err)
	}
	if notices, err := auth.PendingUses(ctx, "local", "contexts", config.ConfigSHA256, 16); err != nil || len(notices) != 1 {
		t.Fatalf("failed cleanup acknowledged notice: %+v %v", notices, err)
	}
	unknown, err := contextpolicy.New(failedContextConfirmation{auth}, store, config)
	if err != nil {
		t.Fatal(err)
	}
	if count, err := unknown.Clean(ctx); err != contextassembly.Unavailable || count != 0 {
		t.Fatalf("unknown confirmation counted complete: %d %v", count, err)
	}
	if _, err := store.Read(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("confirmation attempted before actual retirement: %v", err)
	}
	if notices, err := auth.PendingUses(ctx, "local", "contexts", config.ConfigSHA256, 16); err != nil || len(notices) != 1 {
		t.Fatalf("unknown confirmation lost notice: %+v %v", notices, err)
	}
	if count, err := adapter.Clean(ctx); err != nil || count != 1 {
		t.Fatalf("actual cleanup: %d %v", count, err)
	}
	if _, err := store.Read(ctx, key); err != contextassembly.Invalidated {
		t.Fatalf("snapshot not erased: %v", err)
	}
	if count, err := adapter.Clean(ctx); err != nil || count != 0 {
		t.Fatalf("cleanup replay: %d %v", count, err)
	}
	if err := adapter.Prepare(ctx, token, key, spec); err != contextassembly.Invalidated {
		t.Fatalf("retired use prepared again: %v", err)
	}
	key.Decision = 2
	if err := adapter.Prepare(ctx, token, key, spec); err != nil {
		t.Fatal(err)
	}
	policy(narrow)
	policy(scope)
	if count, err := adapter.Clean(ctx); err != nil || count != 1 {
		t.Fatalf("cleanup before snapshot: %d %v", count, err)
	}
	snapshot.Key = key
	if _, err := store.Bind(ctx, snapshot); err != contextassembly.Invalidated {
		t.Fatalf("late snapshot resurrected: %v", err)
	}
}
