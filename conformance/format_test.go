package conformance_test

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/ruipengliu/lerna/cmd/assembly"
)

// 规则：G3、G11、V4
func TestOpenRefusesUnidentifiedDatabaseBeforeSchemaOrJournalChanges(t *testing.T) {
	path := filepath.Join(t.TempDir(), "foreign.db")
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec("CREATE TABLE unrelated_format(version INTEGER NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if err = db.Close(); err != nil {
		t.Fatal(err)
	}
	before, journal := formatMetadata(t, path)
	h, err := assembly.Open(path, "alice", "local")
	if h != nil {
		if closeErr := h.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	if err == nil {
		t.Error("unidentified nonempty database was accepted")
	}
	after, afterJournal := formatMetadata(t, path)
	if !reflect.DeepEqual(before, after) || journal != afterJournal {
		t.Fatalf("refusal modified format: schema %v -> %v, journal %s -> %s", before, after, journal, afterJournal)
	}
}

// formatMetadata 只读取受信存储格式元数据，不用私有业务表作断言。
func formatMetadata(t *testing.T, path string) ([]string, string) {
	t.Helper()
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT name||':'||coalesce(sql,'') FROM sqlite_schema WHERE name NOT LIKE 'sqlite_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	var schema []string
	for rows.Next() {
		var entry string
		if err = rows.Scan(&entry); err != nil {
			t.Fatal(err)
		}
		schema = append(schema, entry)
	}
	if err = rows.Err(); err != nil {
		t.Fatal(err)
	}
	if err = rows.Close(); err != nil {
		t.Fatal(err)
	}
	var journal string
	if err = db.QueryRow("PRAGMA journal_mode").Scan(&journal); err != nil {
		t.Fatal(err)
	}
	return schema, journal
}

// 规则：G3、G11、V4
func TestOpenRefusesTablelessForeignHeaderAndDeletedSchemaHistory(t *testing.T) {
	for _, statement := range []string{"PRAGMA application_id=123456", "PRAGMA user_version=42", "CREATE TABLE prior_history(id INTEGER); DROP TABLE prior_history"} {
		t.Run(statement, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "header.db")
			db, e := sql.Open("sqlite3", path)
			if e != nil {
				t.Fatal(e)
			}
			if _, e = db.Exec(statement); e != nil {
				t.Fatal(e)
			}
			if e = db.Close(); e != nil {
				t.Fatal(e)
			}
			before, journal := formatMetadata(t, path)
			headers := formatHeaders(t, path)
			bytesBefore, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			h, e := assembly.Open(path, "alice", "local")
			if h != nil {
				if closeErr := h.Close(); closeErr != nil {
					t.Fatal(closeErr)
				}
			}
			if e == nil {
				t.Error("foreign header or erased schema history accepted as new database")
			}
			after, afterJournal := formatMetadata(t, path)
			bytesAfter, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if !reflect.DeepEqual(before, after) || journal != afterJournal || headers != formatHeaders(t, path) || !bytes.Equal(bytesBefore, bytesAfter) {
				t.Fatal("refusal changed foreign format or bytes")
			}
		})
	}
}
func formatHeaders(t *testing.T, path string) [3]int64 {
	t.Helper()
	db, e := sql.Open("sqlite3", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var result [3]int64
	for i, pragma := range []string{"application_id", "user_version", "schema_version"} {
		if e = db.QueryRow("PRAGMA " + pragma).Scan(&result[i]); e != nil {
			t.Fatal(e)
		}
	}
	return result
}
