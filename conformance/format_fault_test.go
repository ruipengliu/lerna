//go:build fault

package conformance_test

import (
	"bytes"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
)

// 规则：G3、G11、V4
func TestForeignWALAndHotJournalRemainUntouchedOnPublicOpenRefusal(t *testing.T) {
	for _, mode := range []string{"wal", "hot-journal"} {
		t.Run(mode, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "foreign.db")
			child := exec.Command(os.Args[0], "-test.run=^TestForeignUncleanProducerChild$")
			child.Env = append(os.Environ(), "LERNA_FOREIGN_PATH="+path, "LERNA_FOREIGN_MODE="+mode)
			output, e := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(e, &exited) || exited.ExitCode() != 86 {
				t.Fatalf("actual abrupt foreign producer: %v %s", e, output)
			}
			before := foreignFileSet(t, path)
			required := "-wal"
			if mode == "hot-journal" {
				required = "-journal"
			}
			if len(before[required]) == 0 {
				t.Fatalf("producer did not retain actual %s", required)
			}
			h, e := assembly.Open(path, "alice", "local")
			if h != nil {
				if closeErr := h.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if e == nil {
				t.Error("foreign unclean format accepted")
			}
			after := foreignFileSet(t, path)
			for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
				old, oldPresent := before[suffix]
				current, currentPresent := after[suffix]
				if oldPresent != currentPresent || !bytes.Equal(old, current) {
					t.Errorf("qualification changed original %q presence %v→%v size %d→%d", suffix, oldPresent, currentPresent, len(old), len(current))
				}
			}
		})
	}
}

func foreignFileSet(t *testing.T, path string) map[string][]byte {
	t.Helper()
	result := map[string][]byte{}
	for _, suffix := range []string{"", "-wal", "-shm", "-journal"} {
		b, e := os.ReadFile(path + suffix)
		if errors.Is(e, os.ErrNotExist) {
			continue
		}
		if e != nil {
			t.Fatal(e)
		}
		result[suffix] = b
	}
	return result
}

// 规则：G3、G11、V4
func TestOrphanSidecarsAreNeverInitializedAsANewDatabase(t *testing.T) {
	for _, suffix := range []string{"-wal", "-shm", "-journal"} {
		t.Run(suffix, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "orphan.db")
			// 仅构造物理文件资格边界，不伪造任何业务记录。
			if e := os.WriteFile(path+suffix, []byte("retained orphan format evidence"), 0600); e != nil {
				t.Fatal(e)
			}
			before := foreignFileSet(t, path)
			h, e := assembly.Open(path, "alice", "local")
			if h != nil {
				if closeErr := h.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if e == nil {
				t.Error("orphan file group accepted as a new database")
			}
			after := foreignFileSet(t, path)
			for _, item := range []string{"", "-wal", "-shm", "-journal"} {
				old, oldPresent := before[item]
				current, currentPresent := after[item]
				if oldPresent != currentPresent || !bytes.Equal(old, current) {
					t.Errorf("orphan refusal changed %q presence %v→%v bytes %d→%d", item, oldPresent, currentPresent, len(old), len(current))
				}
			}
		})
	}
}

// 规则：G3、V4
func TestForeignUncleanProducerChild(t *testing.T) {
	path := os.Getenv("LERNA_FOREIGN_PATH")
	if path == "" {
		t.Skip("isolated foreign-format child only")
	}
	db, e := sql.Open("sqlite3", path)
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	statement := "PRAGMA application_id=123456; PRAGMA journal_mode=WAL; PRAGMA wal_autocheckpoint=0; CREATE TABLE unrelated_format(payload BLOB); INSERT INTO unrelated_format VALUES(zeroblob(32768));"
	if os.Getenv("LERNA_FOREIGN_MODE") == "hot-journal" {
		statement = "PRAGMA application_id=123456; PRAGMA journal_mode=DELETE; PRAGMA cache_size=1; CREATE TABLE unrelated_format(payload BLOB); INSERT INTO unrelated_format VALUES(zeroblob(32768)); BEGIN IMMEDIATE; UPDATE unrelated_format SET payload=zeroblob(65536);"
	}
	if _, e = db.Exec(statement); e != nil {
		t.Fatal(e)
	}
	// 无 Close 或 defer：保留真实未检查点 WAL／热主日志。
	os.Exit(86)
}
