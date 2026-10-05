// Package sqlite 是 M1 唯一的事务后端（受信实现）：本地档的 SQLite。
//
// 每个事务域使用一个独立的数据库文件，不跨域共用事务（分层与模块 R7）。
// 持久设置只在本文件声明一处（ADR 0001、数据与存储 5.4），每条写连接打开时核验实际设置。
package sqlite

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"runtime"
	"sort"
	"strings"

	_ "modernc.org/sqlite" // SQLite 驱动只出现在 infra（R6）。
)

// Profile 是本地档要求的 SQLite 设置。
type Profile struct {
	Name        string
	JournalMode string
	// Synchronous 取 SQLite 的数值：2 为 FULL。NORMAL 在掉电时可能丢失已提交事务。
	Synchronous int
	// FullFsync 在 macOS 默认 VFS 上补齐设备缓存刷新。
	FullFsync bool
}

// LocalProfile 是本地档（ADR 0001）：WAL + synchronous=FULL，macOS 另加 fullfsync。
var LocalProfile = Profile{
	Name:        "local",
	JournalMode: "wal",
	Synchronous: 2,
	FullFsync:   runtime.GOOS == "darwin",
}

//go:embed migrations
var migrations embed.FS

// Open 打开一个事务域的数据库文件，按 p 配置并核验实际设置，然后执行迁移。
// kind 是事务域的种类（例如 adjudication），决定执行哪组领域迁移；持久工作的表总会建立。
func Open(file string, kind string, p Profile) (*sql.DB, error) {
	q := url.Values{}
	q.Add("_pragma", "journal_mode("+p.JournalMode+")")
	q.Add("_pragma", fmt.Sprintf("synchronous(%d)", p.Synchronous))
	q.Add("_pragma", "busy_timeout(10000)")
	q.Add("_pragma", "foreign_keys(1)")
	if p.FullFsync {
		q.Add("_pragma", "fullfsync(1)")
	}
	q.Set("_txlock", "immediate")
	db, err := sql.Open("sqlite", "file:"+file+"?"+q.Encode())
	if err != nil {
		return nil, err
	}
	// 单进程单写者：一条连接，所有事务串行；核验也只需对这条连接做一次。
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)
	if err := Verify(db, p); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := migrate(db, kind); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

// Verify 核验连接的实际持久设置与档位一致；不一致时不开放依赖掉电保证的能力。
func Verify(db *sql.DB, p Profile) error {
	var mode string
	if err := db.QueryRow("PRAGMA journal_mode").Scan(&mode); err != nil {
		return err
	}
	if !strings.EqualFold(mode, p.JournalMode) {
		return fmt.Errorf("sqlite: journal_mode is %q, profile %s requires %q", mode, p.Name, p.JournalMode)
	}
	var syncLevel int
	if err := db.QueryRow("PRAGMA synchronous").Scan(&syncLevel); err != nil {
		return err
	}
	if syncLevel != p.Synchronous {
		return fmt.Errorf("sqlite: synchronous is %d, profile %s requires %d", syncLevel, p.Name, p.Synchronous)
	}
	if p.FullFsync {
		var ff int
		if err := db.QueryRow("PRAGMA fullfsync").Scan(&ff); err != nil {
			return err
		}
		if ff != 1 {
			return fmt.Errorf("sqlite: fullfsync is off, profile %s requires it", p.Name)
		}
	}
	return nil
}

func migrate(db *sql.DB, kind string) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (name TEXT PRIMARY KEY, applied_at TEXT NOT NULL)`); err != nil {
		return err
	}
	var files []string
	for _, dir := range []string{"durable", kind} {
		entries, err := fs.ReadDir(migrations, path.Join("migrations", dir))
		if err != nil {
			if dir == kind {
				continue
			}
			return err
		}
		var names []string
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".sql") {
				names = append(names, path.Join("migrations", dir, e.Name()))
			}
		}
		sort.Strings(names)
		files = append(files, names...)
	}
	for _, f := range files {
		var done int
		if err := db.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, f).Scan(&done); err != nil {
			return err
		}
		if done > 0 {
			continue
		}
		body, err := migrations.ReadFile(f)
		if err != nil {
			return err
		}
		tx, err := db.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(string(body)); err != nil {
			_ = tx.Rollback()
			return fmt.Errorf("sqlite: migration %s: %w", f, err)
		}
		if _, err := tx.Exec(`INSERT INTO schema_migrations (name, applied_at) VALUES (?, datetime('now'))`, f); err != nil {
			_ = tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}
