package governance_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	adapter "github.com/ruipengliu/lerna/adapters/governance"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type countedDisposalHost struct {
	*adapter.BuiltinHost
	calls int
	lock  api.ComponentRef
}

func (h *countedDisposalHost) Dispose(ctx context.Context, in governance.Installation) (governance.DisposalEvidence, error) {
	h.calls++
	h.lock = in.InstallLockRef
	return h.BuiltinHost.Dispose(ctx, in)
}

// Store 合同的扫描计数边界不改记录或查询结果；用于观察每次原 Job 的工作上限。
type disposalScanStore struct {
	runtime.Store
	rows []string
}

func (s *disposalScanStore) List(ctx context.Context, scope runtime.Scope, ns, parent, after string, limit int) ([]runtime.Record, error) {
	rows, err := s.Store.List(ctx, scope, ns, parent, after, limit)
	if ns == "governance/install_holders" {
		for _, row := range rows {
			s.rows = append(s.rows, row.ID)
		}
	}
	return rows, err
}

func TestOriginalDisposalBoundsHolderWorkAndResumesAfterDatabaseReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	path := filepath.Join(t.TempDir(), "dispose.sqlite")
	open := func() runtime.Store {
		var store runtime.Store
		if dsn := os.Getenv("HARNESS_GOVERNANCE_POSTGRES_DSN"); dsn != "" {
			s, err := postgres.Open(ctx, dsn)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			store = s
		} else {
			s, err := sqlite.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			if err = s.Migrate(ctx); err != nil {
				t.Fatal(err)
			}
			store = s
		}
		return store
	}
	f := &fixture{ctx: ctx, store: open(), registry: runtime.NewRegistry()}
	f.scope = runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: f.store.ID()}
	f.auth = runtime.Auth{TenantID: f.scope.TenantID, SubjectID: api.NewID("user"), CredentialGeneration: 1, Roles: []string{"maintainer"}}
	defer func() {
		if err := f.store.Close(); err != nil {
			t.Error(err)
		}
	}()
	content := contentFiles{root: t.TempDir()}
	install := governance.Installation{InstallLockRef: component("installation"), ConfigRef: component("config"), PlatformRef: component("platform"), Artifacts: []api.ContentRef{putContent(t, f, content, "artifact", []byte("exact bounded disposal artifact"))}, DependencyRefs: []api.ComponentRef{}, ABI: "go-static-v1", Profile: api.Profile, ReadFormats: []string{"v1"}, WriteFormats: []string{"v1"}, TrustedBuiltin: true, IsolationRefs: []api.ContentRef{}}
	host, err := adapter.NewBuiltinHost(adapter.BuiltinHostConfig{Root: t.TempDir(), Scope: f.scope, Content: referenceContent{content, f.scope}, Clock: time.Now, Installations: []governance.Installation{install}, ReadinessTTL: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	h := &countedDisposalHost{BuiltinHost: host}
	defer func() {
		if err := h.Close(); err != nil {
			t.Error(err)
		}
	}()
	bind := func() {
		f.registry = runtime.NewRegistry()
		f.svc = governance.New(f.store, governance.Options{Lifecycle: h, Content: content, PreviewGate: previewGate{}})
		if err := f.svc.Register(f.registry); err != nil {
			t.Fatal(err)
		}
		f.dispatcher = &runtime.Dispatcher{Store: f.store, OwnerID: f.scope.OwnerID, Registry: f.registry}
	}
	bind()
	target := api.NewID("target")
	_, registered := command(t, f, "extensions.target.register", target, governance.TargetRegister{TargetID: target, DataFormat: "v1"}, nil)
	if registered.Stage != "applied" {
		t.Fatal(registered)
	}
	prepare, prepared := command(t, f, "extensions.prepare", target, governance.PrepareRequest{Installation: install, TargetRef: f.scope.Ref(target, 1)}, nil)
	if prepared.Stage != "accepted" {
		t.Fatal(prepared)
	}
	drain(t, f, "governance.prepare")
	if r, err := f.dispatcher.Lookup(ctx, f.auth, prepare.CommandID); err != nil || r.Stage != "applied" {
		t.Fatalf("actual prepared installation %+v %v", r, err)
	}
	for range 101 {
		holder := governance.InstallHolder{HolderID: api.NewID("holder"), Revision: 1, InstallLockRef: install.InstallLockRef, ConsumerRef: f.scope.Ref(api.NewID("consumer"), 1), State: "registered"}
		status, err := f.store.Within(ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
			if err := f.svc.RegisterInstallHolderTx(ctx, tx, holder); err != nil {
				return err
			}
			return f.svc.ReleaseInstallHolderTx(ctx, tx, f.scope.Ref(holder.HolderID, 1), true)
		})
		if err != nil || status != runtime.Committed {
			t.Fatalf("actual holder lifecycle %s %v", status, err)
		}
	}
	revision := uint64(2)
	original, disposing := command(t, f, "extensions.dispose", install.InstallLockRef.ComponentID, governance.DisposeRequest{InstallLockRef: install.InstallLockRef, ExpectedRevision: revision}, &revision)
	if disposing.Stage != "accepted" {
		t.Fatal(disposing)
	}
	claim := func() runtime.Work {
		work, status, err := f.store.Claim(ctx, f.scope, api.NewID("worker"), []string{"governance.dispose"}, 1, time.Minute)
		if err != nil || status != runtime.Committed || len(work) != 1 {
			t.Fatalf("original dispose work %s %v", status, err)
		}
		return work[0]
	}
	work := claim()
	handler, _ := f.registry.Job("governance.dispose")
	scan := &disposalScanStore{Store: f.store}
	if err = handler(ctx, scan, f.scope, work); err != nil {
		t.Fatal(err)
	}
	if len(scan.rows) > 100 || h.calls != 0 {
		t.Fatalf("one job scanned %d holders and disposed %d times before durable history closure", len(scan.rows), h.calls)
	}
	if r, err := f.dispatcher.Lookup(ctx, f.auth, original.CommandID); err != nil || r.Stage != "accepted" {
		t.Fatalf("first page closed original command %+v %v", r, err)
	}
	if err = f.store.Close(); err != nil {
		t.Fatal(err)
	}
	f.store = open()
	bind()
	second := claim()
	if second.Job.JobID != work.Job.JobID {
		t.Fatal("database reopen replaced disposal responsibility")
	}
	scan = &disposalScanStore{Store: f.store}
	handler, _ = f.registry.Job("governance.dispose")
	if err = handler(ctx, scan, f.scope, second); err != nil {
		t.Fatal(err)
	}
	if len(scan.rows) != 1 || h.calls != 0 {
		t.Fatalf("recovery rescanned original page or disposed before its closure: rows=%d calls=%d", len(scan.rows), h.calls)
	}
	last := claim()
	if err = handler(ctx, f.store, f.scope, last); err != nil {
		t.Fatal(err)
	}
	if h.calls != 1 || !api.Equal(h.lock, install.InstallLockRef) {
		t.Fatal("final disposal did not use the original frozen installation")
	}
	r, err := f.dispatcher.Lookup(ctx, f.auth, original.CommandID)
	if err != nil || r.Stage != "applied" {
		t.Fatalf("original disposal not decided %+v %v", r, err)
	}
	replayed, err := f.dispatcher.Command(ctx, f.auth, api.Raw(original))
	if err != nil || !api.Equal(replayed, r) || h.calls != 1 {
		t.Fatal("replayed original disposal changed receipt or repeated native cleanup")
	}
}
