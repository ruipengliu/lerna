package sqlite_test

import (
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 规则：G3
func TestLocalDurabilityConnectionSettings(t *testing.T) {
	path := filepath.Join(t.TempDir(), "local.db")
	s, err := sqlite.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings := s.Settings()
	t.Logf("SQLite profile: %+v", settings)
	if settings.JournalMode != "wal" || settings.Synchronous != 2 || settings.FullFSync != 1 || settings.DurabilityProfile != "LOCAL" || settings.PowerLossQualified {
		t.Fatalf("invalid/unsubstantiated local profile: %+v", settings)
	}
	if other, err := sqlite.Open(path, "mallory", "local"); err == nil {
		other.Close()
		t.Fatal("existing file accepted a different user")
	}
}
