package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 规则：G3
func TestOpenAppliesAndVerifiesLocalProfile(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "d.db"), "adjudication", sqlite.LocalProfile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	var mode string
	var syncLevel int
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("PRAGMA synchronous").Scan(&syncLevel); err != nil {
		t.Fatal(err)
	}
	if mode != "wal" || syncLevel != 2 {
		t.Fatalf("local profile needs WAL + synchronous=FULL, got %s/%d", mode, syncLevel)
	}
}

// 规则：G3
func TestVerifyRejectsWeakerSettings(t *testing.T) {
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "d.db"), "adjudication", sqlite.LocalProfile)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec("PRAGMA synchronous=NORMAL"); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.Verify(db, sqlite.LocalProfile); err == nil {
		t.Fatal("synchronous=NORMAL may lose committed transactions on power loss and must be refused")
	}
}

// 规则：G3
func TestMigrationsAreIdempotentAcrossReopen(t *testing.T) {
	file := filepath.Join(t.TempDir(), "d.db")
	for i := 0; i < 2; i++ {
		db, err := sqlite.Open(file, "adjudication", sqlite.LocalProfile)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		_ = db.Close()
	}
}
