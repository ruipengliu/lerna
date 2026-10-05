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
	if settings.JournalMode != "wal" || settings.Synchronous != 2 || settings.FullFSync != 1 || settings.DurabilityProfile != "LOCAL" || !settings.PowerLossQualified {
		t.Fatalf("invalid/unsubstantiated local profile: %+v", settings)
	}
	if other, err := sqlite.Open(path, "mallory", "local"); err == nil {
		other.Close()
		t.Fatal("existing file accepted a different user")
	}
}

// 规则：G3
func TestUnqualifiedCombinationsAreRejected(t *testing.T) {
	admitted := sqlite.Settings{Platform: "27.0.1/26A434/27.0.0/arm64/apfs", SQLiteVersion: "3.53.4", SQLiteSourceID: "2026-07-24 19:02:57 bf7c7f30031888f4e796e429ab3978879485813aaca6f641c7b33e4e09459bcc", SQLiteCompileOptionsHash: "a2f6947c17af9ef76e5b18f8ade825805502af9d65a6f1c7a4c547a74ae4c610", JournalMode: "wal", Synchronous: 2, FullFSync: 1}
	if !sqlite.LocalProfileSupported(admitted) {
		t.Fatal("admitted model combination rejected")
	}
	for _, change := range []func(*sqlite.Settings){
		func(s *sqlite.Settings) { s.Platform = "27.0.1/arm64/apfs" },
		func(s *sqlite.Settings) { s.Platform = "27.0.0/arm64/exfat" },
		func(s *sqlite.Settings) { s.SQLiteSourceID = "unknown" },
		func(s *sqlite.Settings) { s.SQLiteCompileOptionsHash = "unknown" },
		func(s *sqlite.Settings) { s.Synchronous = 1 },
		func(s *sqlite.Settings) { s.FullFSync = 0 },
		func(s *sqlite.Settings) { s.JournalMode = "delete" },
	} {
		candidate := admitted
		change(&candidate)
		if sqlite.LocalProfileSupported(candidate) {
			t.Fatalf("unsupported combination admitted: %+v", candidate)
		}
	}
}
