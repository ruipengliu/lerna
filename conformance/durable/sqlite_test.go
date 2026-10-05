package durable_test

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/ruipengliu/lerna/conformance/durable"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

func sqliteBackend(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "d.db"), "test", sqlite.LocalProfile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// 规则：G3、G11
func TestDurableSemanticsOnSQLite(t *testing.T) {
	durable.Run(t, sqliteBackend)
}
