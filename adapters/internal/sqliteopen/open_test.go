package sqliteopen_test

import (
	"io"
	"os"
	"path/filepath"
	"testing"

	sqlitecontentpolicy "lerna/adapters/content/sqlitepolicy"
	sqliteextraction "lerna/adapters/extraction/sqlite"
	sqlitefetch "lerna/adapters/research/sqlite"
	"lerna/artifacts"
	"lerna/fetch"
	"lerna/memory"
)

// Exercise each public store: a helper-only test would miss error mapping and
// schema initialization failures after the shared connection has been opened.
func TestStoreOpenContract(t *testing.T) {
	stores := []struct {
		name                 string
		open                 func(string) (io.Closer, error)
		invalid, unavailable error
	}{
		{"fetch", func(p string) (io.Closer, error) { return sqlitefetch.Open(p) }, fetch.Invalid, fetch.Unavailable},
		{"extraction", func(p string) (io.Closer, error) { return sqliteextraction.Open(p) }, memory.Invalid, memory.Unavailable},
		{"contentpolicy", func(p string) (io.Closer, error) { return sqlitecontentpolicy.Open(p) }, artifacts.Error("INVALID_ARGUMENT"), artifacts.Error("UNAVAILABLE")},
	}
	for _, store := range stores {
		t.Run(store.name, func(t *testing.T) {
			for _, kind := range []string{"new", "empty", "memory", "permissions", "symlink", "hardlink", "directory", "corrupt"} {
				t.Run(kind, func(t *testing.T) {
					path := filepath.Join(t.TempDir(), "store.db")
					var want error
					switch kind {
					case "empty":
						path, want = "", store.invalid
					case "memory":
						path, want = ":memory:", store.invalid
					case "permissions":
						if err := os.WriteFile(path, nil, 0600); err != nil {
							t.Fatal(err)
						}
						if err := os.Chmod(path, 0644); err != nil {
							t.Fatal(err)
						}
						want = store.invalid
					case "symlink", "hardlink":
						target := path + ".target"
						if err := os.WriteFile(target, nil, 0600); err != nil {
							t.Fatal(err)
						}
						link := os.Link
						want = store.invalid
						if kind == "symlink" {
							link, want = os.Symlink, store.unavailable
						}
						if err := link(target, path); err != nil {
							t.Fatal(err)
						}
					case "directory":
						if err := os.Mkdir(path, 0700); err != nil {
							t.Fatal(err)
						}
						want = store.unavailable
					case "corrupt":
						if err := os.WriteFile(path, []byte("not a SQLite database"), 0600); err != nil {
							t.Fatal(err)
						}
						want = store.unavailable
					}
					db, err := store.open(path)
					if err != want {
						if err == nil {
							db.Close()
						}
						t.Fatalf("Open error = %v, want %v", err, want)
					}
					if err == nil {
						if err := db.Close(); err != nil {
							t.Fatal(err)
						}
						info, err := os.Stat(path)
						if err != nil {
							t.Fatal(err)
						}
						if info.Mode().Perm() != 0600 {
							t.Fatalf("permissions = %o", info.Mode().Perm())
						}
					}
					if kind == "corrupt" {
						// Repair the same file after initialization failed, then open again.
						if err := os.WriteFile(path, nil, 0600); err != nil {
							t.Fatal(err)
						}
					}
					if kind == "new" || kind == "corrupt" {
						reopened, err := store.open(path)
						if err != nil {
							t.Fatalf("reopen: %v", err)
						}
						if err := reopened.Close(); err != nil {
							t.Fatal(err)
						}
					}
				})
			}
		})
	}
}
