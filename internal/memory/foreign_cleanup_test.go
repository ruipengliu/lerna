package memory_test

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

type delayedMirror struct {
	memory.ObjectStore
	entered chan struct{}
	resume  chan struct{}
	writes  atomic.Int32
}

func (s *delayedMirror) Write(ctx context.Context, ref api.ContentRef, body io.Reader) (memory.ObjectLocation, error) {
	s.writes.Add(1)
	close(s.entered)
	select {
	case <-s.resume:
	case <-ctx.Done():
		return memory.ObjectLocation{}, ctx.Err()
	}
	return s.ObjectStore.Write(ctx, ref, body)
}

type cleanupSource struct {
	*foreignAuthority
	releases    atomic.Int32
	failCurrent atomic.Bool
}

type completedMirror struct {
	memory.ObjectStore
	source *cleanupSource
	writes atomic.Int32
}

type armObservationFailure struct {
	memory.ObjectStore
	armed *atomic.Bool
}

func (s armObservationFailure) Write(ctx context.Context, ref api.ContentRef, body io.Reader) (memory.ObjectLocation, error) {
	loc, err := s.ObjectStore.Write(ctx, ref, body)
	if err == nil {
		s.armed.Store(true)
	}
	return loc, err
}

func TestForeignUnobservedWriteAfterReopenKeepsUnknownDespiteExactLocalDelete(t *testing.T) {
	_, local, source, in := setupCleanupMirror(t)
	path := filepath.Join(t.TempDir(), "unobserved-writer.sqlite")
	var armed atomic.Bool
	fault := errors.New("native mirror write returned but observation Tx definitely rolled back")
	store, err := sqlite.Open(path, sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
		if phase == sqlite.BeforeCommit && armed.CompareAndSwap(true, false) {
			return fault
		}
		return nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(local.ctx); err != nil {
		t.Fatal(err)
	}
	local.scope.DatabaseID = store.ID()
	native := local.service.Objects
	local.service = memory.New(store, armObservationFailure{ObjectStore: native, armed: &armed})
	local.service.Foreign = source
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in); !errors.Is(err, fault) {
		t.Fatalf("actual observation rollback cause lost: %v", err)
	}
	if _, err = native.Locate(local.ctx, in.ContentRef); err != nil {
		t.Fatalf("native write was not actually complete: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(path, sqlite.WithExpectedDatabaseID(local.scope.DatabaseID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	local.service = memory.New(reopened, native)
	local.service.Foreign = source
	if err = local.service.StopForeignCopy(local.ctx, local.scope, local.auth, in.CopyID); !api.IsCode(err, "effect_unknown") || source.releases.Load() != 0 {
		t.Fatalf("reopen inferred writer exit from file/lease instead of exact evidence: %v releases%d", err, source.releases.Load())
	}
	if _, err = native.Locate(local.ctx, in.ContentRef); !api.IsCode(err, "gone") {
		t.Fatalf("recovery did not delete exactly located original bytes: %v", err)
	}
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in); !api.IsCode(err, "forbidden") {
		t.Fatalf("unobserved close allowed source reread: %v", err)
	}
}

func (s *completedMirror) Write(ctx context.Context, ref api.ContentRef, body io.Reader) (memory.ObjectLocation, error) {
	s.writes.Add(1)
	location, err := s.ObjectStore.Write(ctx, ref, body)
	if err == nil {
		s.source.failCurrent.Store(true)
	}
	return location, err
}

func TestForeignPostWriteCurrentFailureReopensOriginalPhysicalCleanup(t *testing.T) {
	_, local, source, in := setupCleanupMirror(t)
	// 独立原库可关闭重开，不替换任何源身份/原命令或 native 镜像。
	path := filepath.Join(t.TempDir(), "original-cleanup.sqlite")
	store, err := sqlite.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Migrate(local.ctx); err != nil {
		t.Fatal(err)
	}
	local.scope.DatabaseID = store.ID()
	local.service = memory.New(store, local.service.Objects)
	local.service.Foreign = source
	native := local.service.Objects
	objects := &completedMirror{ObjectStore: native, source: source}
	local.service.Objects = objects
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("post native write source failure was hidden %v", err)
	}
	location, err := native.Locate(local.ctx, in.ContentRef)
	if err != nil {
		t.Fatalf("native mirror did not actually exist: %v", err)
	}
	if body, err := native.Read(local.ctx, location, in.ContentRef, memory.MaxContentBytes); err != nil || api.Hash(body) != in.ContentRef.Hash {
		t.Fatalf("exact real mirror bytes %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlite.Open(path, sqlite.WithExpectedDatabaseID(local.scope.DatabaseID))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	local.service = memory.New(reopened, native)
	local.service.Foreign = source
	if err = local.service.StopForeignCopy(local.ctx, local.scope, local.auth, in.CopyID); err != nil {
		t.Fatal(err)
	}
	if _, err = native.Locate(local.ctx, in.ContentRef); !api.IsCode(err, "gone") {
		t.Fatalf("successful original write lost before proof lookup was incorrectly declared erased: %v", err)
	}
	if objects.writes.Load() != 1 || source.releases.Load() != 1 || source.registerCalls != 1 {
		t.Fatal("recovery reread/rewrote bytes or replaced original commands")
	}
	if _, err = local.service.PrepareForeignUse(local.ctx, local.scope, local.auth, in); !api.IsCode(err, "forbidden") {
		t.Fatalf("reopen restored stopped use %v", err)
	}
}

func (s *cleanupSource) Current(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in memory.ForeignReference) (memory.ForeignProof, error) {
	if s.failCurrent.CompareAndSwap(true, false) {
		return memory.ForeignProof{}, api.E("dependency_unavailable", "original_post_write_current_unavailable")
	}
	return s.foreignAuthority.Current(ctx, scope, auth, in)
}
func (s *cleanupSource) Release(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in memory.ForeignReference, report memory.ReleaseCopyInput) (memory.ForeignProof, error) {
	s.releases.Add(1)
	return s.foreignAuthority.Release(ctx, scope, auth, in, report)
}
func setupCleanupMirror(t *testing.T) (fixture, fixture, *cleanupSource, memory.ForeignReference) {
	t.Helper()
	source, local := newForeignFixture(t), newForeignFixture(t)
	local.scope.TenantID, local.auth = source.scope.TenantID, source.auth
	keys, err := platform.NewDevelopmentKey(source.scope.TenantID, source.scope.OwnerID, []string{"executor_content"})
	if err != nil {
		t.Fatal(err)
	}
	port := &cleanupSource{foreignAuthority: &foreignAuthority{source: source, consumer: local.scope, keys: keys}}
	local.service.Foreign = port
	ref := source.upload(t, "real original foreign bytes remain a physical cleanup responsibility")
	in := memory.ForeignReference{ContentRef: ref, CopyID: api.NewID("copy"), RegisterCommandID: api.NewID("command"), ReleaseCommandID: api.NewID("command"), ReferenceIntentRef: local.scope.Ref(api.NewID("intent"), 1), HolderRef: local.auth.Ref(local.scope.OwnerID), Purpose: "task.goal", Location: "local", RetainUntil: api.Time(time.Now().Add(20 * time.Minute))}
	return source, local, port, in
}

func TestForeignStopCannotCompleteWhileOriginalMirrorWriteIsInFlight(t *testing.T) {
	_, local, source, in := setupCleanupMirror(t)
	native := local.service.Objects
	blocked := &delayedMirror{ObjectStore: native, entered: make(chan struct{}), resume: make(chan struct{})}
	local.service.Objects = blocked
	exit := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	go func() { _, err := local.service.PrepareForeignUse(ctx, local.scope, local.auth, in); exit <- err }()
	select {
	case <-blocked.entered:
	case <-ctx.Done():
		t.Fatal("original actual mirror write did not enter")
	}
	err := local.service.StopForeignCopy(ctx, local.scope, local.auth, in.CopyID)
	// 先放行并观察真正退出，再报告反例；不把测试清理误当 writer 已退出。
	close(blocked.resume)
	var writeErr error
	select {
	case writeErr = <-exit:
	case <-ctx.Done():
		t.Fatal("original writer did not actually exit")
	}
	if !api.IsCode(err, "effect_unknown") || source.releases.Load() != 0 {
		t.Fatalf("empty old location was falsely treated as actual writer join: stop=%v releases=%d", err, source.releases.Load())
	}
	if writeErr == nil {
		t.Fatal("late mirror completion reopened original stopped use")
	}
	location, err := native.Locate(context.Background(), in.ContentRef)
	if err != nil {
		t.Fatalf("actual late native bytes were lost from the observation: %v", err)
	}
	if body, err := native.Read(context.Background(), location, in.ContentRef, memory.MaxContentBytes); err != nil || api.Hash(body) != in.ContentRef.Hash {
		t.Fatalf("late actual bytes differ %v", err)
	}
	if _, err = local.service.PrepareForeignUse(context.Background(), local.scope, local.auth, in); !api.IsCode(err, "forbidden") || blocked.writes.Load() != 1 {
		t.Fatalf("stopped copy resumed physical writing: %v writes%d", err, blocked.writes.Load())
	}
	if err = local.service.StopForeignCopy(context.Background(), local.scope, local.auth, in.CopyID); err != nil {
		t.Fatal(err)
	}
	if _, err = native.Locate(context.Background(), in.ContentRef); !api.IsCode(err, "gone") {
		t.Fatalf("late exact mirror was left on disk after original cleanup: %v", err)
	}
	if source.releases.Load() != 1 {
		t.Fatal("cleanup changed or repeated original release responsibility")
	}
	if err = local.service.StopForeignCopy(context.Background(), local.scope, local.auth, in.CopyID); err != nil || source.releases.Load() != 1 {
		t.Fatalf("stopped original replay changed responsibility %v", err)
	}
}
