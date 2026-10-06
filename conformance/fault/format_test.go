//go:build fault

package fault_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 规则：G3、G11、V4
func TestInitialFormatBootstrapIsAtomicAtThreePersistenceModes(t *testing.T) {
	for _, mode := range []sqlite.FaultMode{sqlite.CrashBeforeCommit, sqlite.CrashAfterCommit, sqlite.LoseReceipt} {
		t.Run(string(mode), func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "bootstrap.db")
			child := exec.Command(os.Args[0], "-test.run=^TestFormatBootstrapChild$")
			child.Env = append(os.Environ(), "LERNA_FORMAT_DB="+path, "LERNA_FORMAT_MODE="+string(mode))
			output, e := child.CombinedOutput()
			if mode == sqlite.LoseReceipt {
				if e != nil {
					t.Fatalf("lost receipt child: %v %s", e, output)
				}
			} else {
				var exited *exec.ExitError
				if !errors.As(e, &exited) || exited.ExitCode() != sqlite.CrashExitCode {
					t.Fatalf("crash boundary not reached: %v %s", e, output)
				}
			}
			db, e := sql.Open("sqlite3", path)
			if e != nil {
				t.Fatal(e)
			}
			var objects, appID, userVersion int
			if e = db.QueryRow("SELECT count(*) FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%'").Scan(&objects); e != nil {
				t.Fatal(e)
			}
			if e = db.QueryRow("PRAGMA application_id").Scan(&appID); e != nil {
				t.Fatal(e)
			}
			if e = db.QueryRow("PRAGMA user_version").Scan(&userVersion); e != nil {
				t.Fatal(e)
			}
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			if mode == sqlite.CrashBeforeCommit && (objects != 0 || appID != 0 || userVersion != 0) {
				t.Fatalf("partial format survived: objects=%d appid=%d version=%d", objects, appID, userVersion)
			}
			if mode != sqlite.CrashBeforeCommit && (objects == 0 || appID != 0x4c524e41 || userVersion != 1) {
				t.Fatalf("committed format lost: objects=%d appid=%d version=%d", objects, appID, userVersion)
			}
			h, e := assembly.Open(path, "alice", "local")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			settings := h.StorageSettings()
			if settings.FormatVersion != 1 || settings.ContractVersion != 1 || len(settings.FormatDigest) != 64 || settings.ImplementationProfile != "lerna-m1-v1" {
				t.Fatalf("format settings: %+v", settings)
			}
			caller, c := goal()
			if _, e = h.Sessions.SubmitGoal(context.Background(), caller, c); e != nil {
				t.Fatal(e)
			}
			if e = h.Sessions.ProcessPending(context.Background(), caller); e != nil {
				t.Fatal(e)
			}
			assertOneTask(t, h, caller, c)
		})
	}
}

// 规则：G3、V4
func TestFormatBootstrapChild(t *testing.T) {
	path := os.Getenv("LERNA_FORMAT_DB")
	if path == "" {
		t.Skip("subprocess only")
	}
	mode := sqlite.FaultMode(os.Getenv("LERNA_FORMAT_MODE"))
	ctx, e := sqlite.WithFault(context.Background(), "storage.bootstrap", mode)
	if e != nil {
		t.Fatal(e)
	}
	store, e := sqlite.OpenContext(ctx, path, "alice", "local")
	if store != nil {
		store.Close()
	}
	if mode != sqlite.LoseReceipt {
		t.Fatal("crash not reached")
	}
	if e == nil {
		t.Fatal("bootstrap returned success despite lost commit receipt")
	}
}

// 规则：G3、G11、V4
func TestUnsupportedFormatNeverAdvancesExistingOwnerResponsibility(t *testing.T) {
	for _, statement := range []string{
		"UPDATE database_format SET format_version=2", "UPDATE database_format SET contract_version=2",
		"UPDATE database_format SET implementation_profile='future'", "UPDATE database_format SET compiled_digest='future'",
		"UPDATE database_format SET actual_schema_digest='future'", "UPDATE database_format SET user_id='other'",
		"PRAGMA user_version=2", "PRAGMA application_id=123456", "DELETE FROM database_format", "DROP TABLE database_format",
		"CREATE TABLE foreign_extra(value INTEGER)",
	} {
		t.Run(statement, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incompatible.db")
			h, e := assembly.Open(path, "alice", "local")
			if e != nil {
				t.Fatal(e)
			}
			defer h.Close()
			caller, c := goal()
			original, e := h.Sessions.SubmitGoal(context.Background(), caller, c)
			if e != nil {
				t.Fatal(e)
			}
			if e = h.StorageFaultSQL(statement); e != nil {
				t.Fatal(e)
			}
			candidate, e := assembly.Open(path, "alice", "local")
			if candidate != nil {
				candidate.Close()
			}
			if e == nil {
				t.Error("unsupported original format accepted")
			}
			after, e := h.Durable.QueryReceipt(context.Background(), caller, c.Identity)
			if e != nil || !proto.Equal(after.Receipt, original) {
				t.Fatalf("incompatible startup advanced original receipt: %v %v", after, e)
			}
			job, e := h.Durable.QueryJob(context.Background(), caller, original.JobRef.Name)
			if e != nil || job.State != "READY" || job.ClaimEpoch != 0 {
				t.Fatalf("incompatible startup advanced job: %v %v", job, e)
			}
		})
	}
}
