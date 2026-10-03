//go:build integration

package recovery_test

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/mattn/go-sqlite3"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

type historicalVersion struct {
	Version  int64
	Checksum string
}
type historicalReport struct {
	Schema            string
	MigrationChecksum string
	Migration         historicalVersion
	Observation       demo.Observation
	Outcomes          []contract.TransportOutcome
}
type historicalFixture struct {
	Store    workStore
	Report   historicalReport
	Commands []json.RawMessage
	Migrate  func(context.Context) error
	Versions func(context.Context) ([]historicalVersion, error)
	Reopen   func(*testing.T) workStore
	Fault    func(*testing.T, bool)
	IsFault  func(error) bool
	Expected []historicalVersion
}

var historicalOwner = contract.OwnerRef{TenantID: "fixture-tenant", OwnerID: "fixture-owner"}
var historicalSubject = contract.SubjectBinding{TenantID: "fixture-tenant", SubjectID: "fixture-writer", DelegationChain: []contract.DelegatedSubject{}}

func TestPGHistoricalV1UpgradePreservesOriginalAndRollsBackFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "NormalUpgrade"
		if fail {
			name = "Version2RefusalRollsBackAndRetryCompletesOriginal"
		}
		t.Run(name, func(t *testing.T) { exerciseHistoricalUpgrade(t, restoreHistoricalPG(t), fail) })
	}
}
func TestSQLiteHistoricalV1UpgradePreservesOriginalAndRollsBackFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "NormalUpgrade"
		if fail {
			name = "Version2RefusalRollsBackAndRetryCompletesOriginal"
		}
		t.Run(name, func(t *testing.T) { exerciseHistoricalUpgrade(t, restoreHistoricalSQLite(t), fail) })
	}
}
func historicalHost(store workStore) *durablework.Host {
	return durablework.New(historicalOwner, store, store, store, store, durablework.NewPermissions([]durablework.Permission{{Subject: historicalSubject, Owner: historicalOwner, Record: true, Read: true, Cleanup: true}}))
}
func historicalFound(t *testing.T, h *durablework.Host, ref contract.CommandRef) contract.CommandReceipt {
	t.Helper()
	got, err := contract.GetCommand(contextFor(t), readWire(ref), &historicalSubject, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := got.AsFound()
	if !ok {
		t.Fatalf("original decision not found: %+v", got)
	}
	return found.Receipt
}
func assertHistoricalQueries(t *testing.T, h *durablework.Host, f *historicalFixture) {
	t.Helper()
	for _, out := range f.Report.Outcomes[:2] {
		received, ok := out.AsReceived()
		if !ok {
			t.Fatal("historical writer observation has unconfirmed outcome")
		}
		var ref contract.CommandRef
		if applied, ok := received.Receipt.AsApplied(); ok {
			ref = applied.CommandRef
		} else if rejected, ok := received.Receipt.AsRejected(); ok {
			ref = rejected.CommandRef
		} else {
			t.Fatal("unsupported historical receipt")
		}
		assertReceiptSame(t, received.Receipt, historicalFound(t, h, ref))
	}
}
func assertHistoricalVersions(t *testing.T, f *historicalFixture, expected []historicalVersion) {
	t.Helper()
	got, err := f.Versions(contextFor(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(expected) {
		t.Fatalf("migration versions: %+v want %+v", got, expected)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Fatalf("changed migration identity: %+v want %+v", got, expected)
		}
	}
	t.Logf("actual historical migration identities: %+v", got)
}
func assertHistoricalReplay(t *testing.T, h *durablework.Host, f *historicalFixture) {
	t.Helper()
	for i, data := range f.Commands {
		original, ok := f.Report.Outcomes[i].AsReceived()
		if !ok {
			t.Fatal("original receipt unconfirmed")
		}
		out, err := h.Record(contextFor(t), data, &historicalSubject)
		assertReceiptSame(t, original.Receipt, assertReceived(t, out, err))
	}
}
func exerciseHistoricalUpgrade(t *testing.T, f *historicalFixture, fail bool) {
	t.Helper()
	h := historicalHost(f.Store)
	assertHistoricalVersions(t, f, f.Expected[:1])
	assertHistoricalQueries(t, h, f)
	if fail {
		f.Fault(t, true)
		err := f.Migrate(contextFor(t))
		if err == nil || !f.IsFault(err) {
			t.Fatalf("migration did not reach controlled version2 refusal: %v", err)
		}
		assertHistoricalVersions(t, f, f.Expected[:1])
		// Real v2 DDL must roll back with its version record; query still reads the v1 receipt.
		assertHistoricalQueries(t, h, f)
		f.Fault(t, false)
	}
	if err := f.Migrate(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	assertHistoricalVersions(t, f, f.Expected)
	assertHistoricalQueries(t, h, f)
	assertHistoricalReplay(t, h, f)
	got, err := h.Observe(contextFor(t), "v1-input", &historicalSubject)
	if err != nil || got.Input.Text != f.Report.Observation.Input.Text || got.Input.BodyGone || got.Input.StoredTextBytes != 25 || got.Input.Revision != 1 || got.Job.ID != f.Report.Observation.Job.ID || got.Job.WorkRevision != 1 || got.Job.CompletedRevision != 0 || got.Job.State != "ready" {
		t.Fatalf("upgrade changed original pending responsibility: %+v %v", got, err)
	}
	// Close the actual product connection and continue on a freshly opened Host.
	f.Store = f.Reopen(t)
	h = historicalHost(f.Store)
	assertHistoricalQueries(t, h, f)
	assertHistoricalReplay(t, h, f)
	worker := conformanceWorker(t, historicalOwner, f.Store, f.Store, f.Store, h.Clock)
	batch, err := worker.Claim(contextFor(t), "historical-successor", 1, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Claim.JobID != f.Report.Observation.Job.ID || batch[0].Claim.ClaimedRevision != 1 || batch[0].Input.Text != f.Report.Observation.Input.Text {
		t.Fatalf("successor did not claim original v1 Job: %+v %v", batch, err)
	}
	startWork(t, worker, batch[0])
	projected := durablework.Project(batch[0])
	if projected.TextDigest != "sha256:eeebf3ebdb81d9e669ca989199225a42df8bfe152a9e247c1b5981052f615bbc" {
		t.Fatalf("original input projection hash: %+v", projected)
	}
	if err = worker.Complete(contextFor(t), batch[0].Claim, projected); err != nil {
		t.Fatal(err)
	}
	got, err = h.Observe(contextFor(t), "v1-input", &historicalSubject)
	if err != nil || got.Job.ID != f.Report.Observation.Job.ID || got.Job.State != "done" || got.Job.CompletedRevision != 1 || got.Projection == nil || *got.Projection != projected {
		t.Fatalf("original Job not durably completed: %+v %v", got, err)
	}
	ref := contract.CommandRef{Owner: historicalOwner, CommandID: "applied-original"}
	cleaned, err := h.Cleanup(contextFor(t), ref, "1", &historicalSubject)
	if err != nil || cleaned != demo.Cleaned {
		t.Fatalf("upgraded original cleanup: %s %v", cleaned, err)
	}
	f.Store = f.Reopen(t)
	h = historicalHost(f.Store)
	got, err = h.Observe(contextFor(t), "v1-input", &historicalSubject)
	if err != nil || !got.Input.BodyGone || got.Input.Text != "" || got.Input.StoredTextBytes != 0 || got.Input.Revision != 1 || got.Job.ID != f.Report.Observation.Job.ID || got.Job.State != "done" || got.Projection == nil || *got.Projection != projected {
		t.Fatalf("reopen lost exact cleanup/projection: %+v %v", got, err)
	}
	queried, err := contract.GetCommand(contextFor(t), readWire(ref), &historicalSubject, h.Permissions, h, time.Now)
	gone, ok := queried.AsGone()
	if err != nil || !ok || gone.CommandRef != ref {
		t.Fatalf("upgraded original command not gone: %+v %v", queried, err)
	}
	assertHistoricalReplay(t, h, f)
	// Replaying gone original must retain gone view and empty storage.
	queried, err = contract.GetCommand(contextFor(t), readWire(ref), &historicalSubject, h.Permissions, h, time.Now)
	if gone, ok := queried.AsGone(); err != nil || !ok || gone.CommandRef != ref {
		t.Fatalf("replay resurrected old command: %+v %v", queried, err)
	}
	got, err = h.Observe(contextFor(t), "v1-input", &historicalSubject)
	if err != nil || !got.Input.BodyGone || got.Input.StoredTextBytes != 0 || got.Input.Revision != 1 || got.Job.ID != f.Report.Observation.Job.ID || got.Job.CompletedRevision != 1 {
		t.Fatalf("replay recreated historical work/body: %+v %v", got, err)
	}
	old, _ := f.Report.Outcomes[1].AsReceived()
	assertReceiptSame(t, old.Receipt, historicalFound(t, h, contract.CommandRef{Owner: historicalOwner, CommandID: "expired-original"}))
	worker = conformanceWorker(t, historicalOwner, f.Store, f.Store, f.Store, h.Clock)
	batch, err = worker.Claim(contextFor(t), "no-duplicate", 1, time.Minute)
	if err != nil || len(batch) != 0 {
		t.Fatalf("historical replay duplicated responsibility: %+v %v", batch, err)
	}
	cleaned, err = h.Cleanup(contextFor(t), ref, "1", &historicalSubject)
	if err != nil || cleaned != demo.AlreadyGone {
		t.Fatalf("upgraded cleanup not idempotent: %s %v", cleaned, err)
	}
	assertHistoricalVersions(t, f, f.Expected)
}

// Fixture verification happens before any database is opened for writing.
func loadHistoricalFixture(t *testing.T, adapter, writer string) (string, map[string][]byte, *historicalFixture) {
	t.Helper()
	dir := filepath.Join("..", "fixtures", "durable-work", adapter+"-v1")
	manifest, err := os.Open(filepath.Join(dir, "SHA256SUMS"))
	if err != nil {
		t.Fatal(err)
	}
	defer manifest.Close()
	files := make(map[string][]byte)
	scanner := bufio.NewScanner(manifest)
	for scanner.Scan() {
		parts := strings.Fields(scanner.Text())
		if len(parts) != 2 || len(parts[0]) != 64 || filepath.Base(parts[1]) != parts[1] {
			t.Fatal("invalid immutable fixture hash manifest")
		}
		data, err := os.ReadFile(filepath.Join(dir, parts[1]))
		if err != nil {
			t.Fatal(err)
		}
		hash := sha256.Sum256(data)
		if hex.EncodeToString(hash[:]) != parts[0] {
			t.Fatalf("immutable fixture hash mismatch: %s", parts[1])
		}
		files[parts[1]] = data
	}
	if err := scanner.Err(); err != nil {
		t.Fatal(err)
	}
	if len(files) != 5 || strings.TrimSpace(string(files["writer-revision.txt"])) != writer {
		t.Fatal("unexpected historical fixture provenance")
	}
	f := &historicalFixture{}
	if err := json.Unmarshal(files["writer-observation.json"], &f.Report); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(files["commands.json"], &f.Commands); err != nil {
		t.Fatal(err)
	}
	if len(f.Commands) != 3 || len(f.Report.Outcomes) != 3 {
		t.Fatal("incomplete historical writer corpus/observation")
	}
	t.Logf("verified real historical %s writer %s and all five SHA256SUMS entries", adapter, writer)
	return dir, files, f
}
func restoreHistoricalSQLite(t *testing.T) *historicalFixture {
	t.Helper()
	_, files, f := loadHistoricalFixture(t, "sqlite", "f4fb0576bc0a1e3371fb88ab43f729beb9ddf118")
	path := filepath.Join(t.TempDir(), "original-v1.sqlite")
	if err := os.WriteFile(path, files["database.sqlite"], 0600); err != nil {
		t.Fatal(err)
	}
	// t.TempDir honors the root-controlled TMPDIR overlay; the checked-in file is never opened writable.
	cfg := sqlite.Config{Path: path, TransactionTimeout: 3 * time.Second, BusyTimeout: 100 * time.Millisecond}
	current, err := sqlite.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := current.Close(); err != nil {
			t.Error(err)
		}
	})
	f.Store = current
	f.Migrate = func(ctx context.Context) error { return current.Migrate(ctx) }
	f.Versions = func(ctx context.Context) ([]historicalVersion, error) {
		vs, err := current.MigrationVersions(ctx)
		out := make([]historicalVersion, len(vs))
		for i, v := range vs {
			out[i] = historicalVersion{v.Version, v.Checksum}
		}
		return out, err
	}
	f.Reopen = func(t *testing.T) workStore {
		if err := current.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		current, err = sqlite.Open(contextFor(t), cfg)
		if err != nil {
			t.Fatal(err)
		}
		return current
	}
	f.Fault = func(t *testing.T, install bool) {
		// SQL is a declared test-only database boundary fault, never a business assertion.
		db, err := sql.Open("sqlite3", path+"?_busy_timeout=100&_foreign_keys=on")
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		statement := `DROP TRIGGER lerna_test_refuse_v2`
		if install {
			statement = `CREATE TRIGGER lerna_test_refuse_v2 BEFORE INSERT ON schema_migrations WHEN NEW.version=2 BEGIN SELECT RAISE(ABORT,'lerna_test_refuse_v2'); END`
		}
		if _, err = db.ExecContext(contextFor(t), statement); err != nil {
			t.Fatal(err)
		}
	}
	f.IsFault = func(err error) bool {
		var actual sqlite3.Error
		return errors.As(err, &actual) && actual.ExtendedCode == sqlite3.ErrConstraintTrigger
	}
	f.Expected = []historicalVersion{{1, "sha256:324dd9c72a00438095596b59c80bf21e66a02eb53d7182ddba67e4784e2c0203"}, {2, "sha256:3791b3fc5ca49c18eee04b2afcaa54c2aae9c5afbf3f1e3fd98f5cce8715e01c"}, {3, sqlite.MigrationV3Checksum()}, {4, sqlite.MigrationV4Checksum()}}
	if f.Report.Migration != f.Expected[0] {
		t.Fatal("writer v1 migration identity mismatch")
	}
	if err := current.Within(contextFor(t), historicalOwner, func(ctx context.Context, tx runtime.Tx) error {
		settings, err := current.Settings(ctx, tx)
		t.Logf("actual restored SQLite settings: %+v", settings)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return f
}
func restoreHistoricalPG(t *testing.T) *historicalFixture {
	t.Helper()
	_, files, f := loadHistoricalFixture(t, "pg", "988f8b7ec2a8fd3a28db44b11cf5a863af4593b2")
	psql, err := exec.LookPath("psql")
	if err != nil {
		t.Fatal("psql is required to restore the complete real historical plain dump")
	}
	clientContext, clientCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer clientCancel()
	version, err := runHistoricalPsql(t, clientContext, psql, []string{"--version"}, os.Environ())
	if err != nil {
		t.Fatal("psql version check failed")
	}
	t.Logf("actual historical dump restore client: %s", strings.TrimSpace(string(version)))
	dsn := os.Getenv("LERNA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("LERNA_TEST_POSTGRES_DSN is required")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	cfg := postgres.Config{DSN: dsn, Schema: "lerna_test_" + hex.EncodeToString(nonce[:]), TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
	scope, err := postgres.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = scope.CreateSchema(contextFor(t)); err != nil {
		scope.Close()
		t.Fatal(err)
	}
	// Only this successful CREATE registers schema ownership. Keep its administrative
	// connection separately so closing/reopening product connections does not lose cleanup ownership.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := scope.DropTestSchema(ctx); err != nil {
			t.Error(err)
		}
		if err := scope.Close(); err != nil {
			t.Error(err)
		}
	})
	const fixed = "lerna_test_000000000000000000000001"
	if f.Report.Schema != fixed {
		t.Fatal("unexpected immutable dump schema")
	}
	dump := string(files["database.sql"])
	create := "CREATE SCHEMA " + fixed + ";"
	if strings.Count(dump, create) != 1 || strings.Contains(string(files["commands.json"]), fixed) || strings.Contains(f.Report.Observation.Input.Text, fixed) {
		t.Fatal("fixture outside reviewed schema replacement bounds")
	}
	dump = strings.Replace(dump, create, "", 1)
	dump = strings.ReplaceAll(dump, fixed, cfg.Schema)
	path := filepath.Join(t.TempDir(), "complete-original-v1.sql")
	if err := os.WriteFile(path, []byte(dump), 0600); err != nil {
		t.Fatal(err)
	}
	restoreEnvironmentEntries, err := historicalPsqlEnvironment(dsn)
	if err != nil {
		t.Fatal(err)
	}
	restoreContext, restoreCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer restoreCancel()
	output, err := runHistoricalPsql(t, restoreContext, psql, []string{"-X", "-v", "ON_ERROR_STOP=1", "--single-transaction", "--file", path}, restoreEnvironmentEntries)
	if err != nil {
		t.Fatalf("full historical psql restore failed: %v (output intentionally withheld to protect connection credentials)", err)
	}
	var copied []string
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "COPY ") {
			copied = append(copied, line)
		}
	}
	t.Logf("complete historical dump restored with original psql metacommands; %v", copied)
	current, err := postgres.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := current.Close(); err != nil {
			t.Error(err)
		}
	})
	f.Store = current
	f.Migrate = func(ctx context.Context) error { return current.Migrate(ctx) }
	f.Versions = func(ctx context.Context) ([]historicalVersion, error) {
		vs, err := current.MigrationVersions(ctx)
		out := make([]historicalVersion, len(vs))
		for i, v := range vs {
			out[i] = historicalVersion{v.Version, v.Checksum}
		}
		return out, err
	}
	f.Reopen = func(t *testing.T) workStore {
		if err := current.Close(); err != nil {
			t.Fatal(err)
		}
		var err error
		current, err = postgres.Open(contextFor(t), cfg)
		if err != nil {
			t.Fatal(err)
		}
		return current
	}
	f.Fault = func(t *testing.T, install bool) {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			t.Fatal("fault connection failed")
		}
		defer db.Close()
		scope := `"` + cfg.Schema + `".`
		statements := []string{`DROP TRIGGER lerna_test_refuse_v2 ON ` + scope + `schema_migrations`, `DROP FUNCTION ` + scope + `lerna_test_refuse_v2()`}
		if install {
			statements = []string{`CREATE FUNCTION ` + scope + `lerna_test_refuse_v2() RETURNS trigger LANGUAGE plpgsql AS $fault$ BEGIN IF NEW.version=2 THEN RAISE EXCEPTION USING ERRCODE='P0001', MESSAGE='lerna_test_refuse_v2'; END IF; RETURN NEW; END; $fault$`, `CREATE TRIGGER lerna_test_refuse_v2 BEFORE INSERT ON ` + scope + `schema_migrations FOR EACH ROW EXECUTE FUNCTION ` + scope + `lerna_test_refuse_v2()`}
		}
		tx, err := db.BeginTx(contextFor(t), nil)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		for _, statement := range statements {
			if _, err = tx.ExecContext(contextFor(t), statement); err != nil {
				t.Fatal(err)
			}
		}
		if err = tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	f.IsFault = func(err error) bool {
		var actual *pgconn.PgError
		return errors.As(err, &actual) && actual.Code == "P0001" && actual.Message == "lerna_test_refuse_v2"
	}
	f.Expected = []historicalVersion{{1, "sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e"}, {2, "sha256:cdb7dea9f55ee8ac9201a943cecf8096108b48bc208409e1372bbf17295b2297"}, {3, postgres.MigrationV3Checksum()}, {4, postgres.MigrationV4Checksum()}}
	if f.Report.MigrationChecksum != f.Expected[0].Checksum {
		t.Fatal("writer v1 migration identity mismatch")
	}
	if err := current.Within(contextFor(t), historicalOwner, func(ctx context.Context, tx runtime.Tx) error {
		settings, err := current.Settings(ctx, tx)
		t.Logf("actual restored PostgreSQL settings: %+v", settings)
		return err
	}); err != nil {
		t.Fatal(err)
	}
	return f
}
