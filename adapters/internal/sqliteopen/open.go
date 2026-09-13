// Package sqliteopen shares private-file and connection setup for SQLite adapters.
package sqliteopen

import (
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/unix"
	_ "modernc.org/sqlite"
)

// Open leaves schema initialization and connection ownership to the caller.
// Error values and busy timeout retain each adapter's existing contract.
func Open(path string, busy time.Duration, invalid, unavailable error) (*sql.DB, error) {
	if path == "" || path == ":memory:" {
		return nil, invalid
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, invalid
	}
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0600)
	if err != nil {
		return nil, unavailable
	}
	var info unix.Stat_t
	err = unix.Fstat(int(file.Fd()), &info)
	file.Close()
	if err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG || info.Mode&0777 != 0600 || info.Uid != uint32(os.Getuid()) || info.Nlink != 1 {
		return nil, invalid
	}
	u := url.URL{Scheme: "file", Path: path}
	u.RawQuery = url.Values{"_pragma": {fmt.Sprintf("busy_timeout(%d)", busy.Milliseconds()), "journal_mode(WAL)", "synchronous(FULL)"}}.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, unavailable
	}
	db.SetMaxOpenConns(1)
	return db, nil
}
