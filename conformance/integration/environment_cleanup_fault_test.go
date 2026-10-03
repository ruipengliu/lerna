package integration_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

type failedEnvironmentPreparation struct{ domain.EnvironmentAdmission }

func (failedEnvironmentPreparation) Prepare(context.Context, rt.Scope, rt.Auth, domain.Environment) error {
	return api.E("unsupported", "original_runtime_preparation_unavailable")
}

func TestFailedEnvironmentPreparationCleanupSQLFaultRollsBackOriginalReceiptAndRecoversSameClaim(t *testing.T) {
	f, host := newWASIFixture(t)
	if f.postgresDSN != "" {
		t.Skip("native SQLite statement fault boundary; PG lifecycle covered separately")
	}
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{host}, EnvironmentAdmission: failedEnvironmentPreparation{host.Admission()}})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = rt.NewRegistry()
	if err = service.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Registry = f.registry
	id := api.NewID("environment")
	accepted := f.command(t, "environment.create", id, domain.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: host.EnvironmentConfigRef(), InstallLockRef: host.InstallLockRef(), Limits: wasi.DefaultLimits(), ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}}, nil)
	if accepted.Stage != "accepted" {
		t.Fatalf("original preparation not accepted %+v", accepted)
	}
	var before domain.Environment
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &before)
	// 原隔离 SQLite 文件的真实 INSERT 语句失败；所有判断仍由公开 Query/Lookup 验证。
	db, err := sql.Open("sqlite3", f.databasePath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := db.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err = db.Exec("CREATE TRIGGER reject_environment_cleanup BEFORE INSERT ON runtime_jobs WHEN NEW.kind = 'execution.environment.cleanup' BEGIN SELECT RAISE(ABORT, 'injected_environment_cleanup_insert'); END"); err != nil {
		t.Fatal(err)
	}
	claims, status, err := f.st.Claim(context.Background(), f.sc, api.NewID("worker"), []string{domain.EnvironmentPrepareJob}, 1, time.Minute)
	if err != nil || status != rt.Committed || len(claims) != 1 {
		t.Fatalf("original preparation claim %s %v", status, err)
	}
	prepare, ok := f.registry.Job(domain.EnvironmentPrepareJob)
	if !ok {
		t.Fatal("preparation handler missing")
	}
	err = prepare(context.Background(), f.st, f.sc, claims[0])
	var originalSQL sqlite3.Error
	if !errors.As(err, &originalSQL) || originalSQL.ExtendedCode != sqlite3.ErrConstraintTrigger {
		t.Fatalf("cleanup INSERT failure was swallowed or reclassified: %v", err)
	}
	current, err := f.dispatcher.Lookup(context.Background(), f.auth, accepted.CommandID)
	if err != nil || current.Stage != "accepted" {
		t.Fatalf("SQL failure decided original receipt without cleanup %+v %v", current, err)
	}
	var unchanged domain.Environment
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &unchanged)
	if !api.Equal(unchanged, before) {
		t.Fatalf("SQL failure committed environment without its cleanup %+v %+v", unchanged, before)
	}
	if _, err = db.Exec("DROP TRIGGER reject_environment_cleanup"); err != nil {
		t.Fatal(err)
	}
	if err = prepare(context.Background(), f.st, f.sc, claims[0]); err != nil {
		t.Fatal(err)
	}
	rejected, err := f.dispatcher.Lookup(context.Background(), f.auth, accepted.CommandID)
	if err != nil || rejected.Stage != "rejected" || !api.IsCode(rejected.Error, "unsupported") {
		t.Fatalf("same original preparation did not recover accurate refusal %+v %v", rejected, err)
	}
	f.drain(t)
	var closed domain.Environment
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &closed)
	if closed.Phase != "closed" || !closed.ActuallyExited || closed.ReadyForCell || closed.Generation != before.Generation+1 {
		t.Fatalf("required cleanup did not close the original failed environment %+v", closed)
	}
}
