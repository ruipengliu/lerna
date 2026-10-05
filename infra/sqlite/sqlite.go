// Package sqlite 实现固定本地档的受信存储；不拥有业务裁决权。
package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	_ "github.com/mattn/go-sqlite3"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

//go:embed migrations/001_submission.sql
var migration string

//go:embed migrations/002_admission.sql
var admissionMigration string

//go:embed migrations/003_session_input.sql
var sessionInputMigration string

//go:embed migrations/007_completion.sql
var completionMigration string

//go:embed migrations/006_egress.sql
var egressMigration string

//go:embed migrations/004_grants_confirmation.sql
var grantsMigration string

type Settings struct {
	Platform                 string
	SQLiteVersion            string
	SQLiteSourceID           string
	SQLiteCompileOptionsHash string
	JournalMode              string
	Synchronous              int
	FullFSync                int
	DurabilityProfile        string
	PowerLossQualified       bool
}
type Store struct {
	db           *sql.DB
	conn         *sql.Conn
	mu           sync.Mutex
	user, domain string
	settings     Settings
}
type txKey struct{}
type transaction struct {
	store  *Store
	tx     *sql.Tx
	domain string
}
type querier interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// Open 固定一条经过核验的连接，不允许池中新建未配置的写连接。
func Open(path, user, domain string) (*Store, error) {
	if err := ensureBarrier(); err != nil {
		return nil, err
	}
	if path == "" || path == ":memory:" || user == "" || domain == "" {
		return nil, command.Fail("INVALID_INPUT")
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: path}).String() + "?_busy_timeout=5000&_txlock=immediate&_foreign_keys=on"
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, conn: conn, user: user, domain: domain}
	s.settings.Platform = observedPlatform(path)
	if err := s.configure(context.Background()); err != nil {
		s.Close()
		return nil, err
	}

	return s, nil
}
func (s *Store) configure(ctx context.Context) error {
	for _, pragma := range []string{"PRAGMA fullfsync=ON", "PRAGMA synchronous=FULL", "PRAGMA journal_mode=WAL"} {
		if _, err := s.conn.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	for _, entry := range []struct {
		query string
		dest  any
	}{{"SELECT sqlite_version()", &s.settings.SQLiteVersion}, {"SELECT sqlite_source_id()", &s.settings.SQLiteSourceID}, {"PRAGMA journal_mode", &s.settings.JournalMode}, {"PRAGMA synchronous", &s.settings.Synchronous}, {"PRAGMA fullfsync", &s.settings.FullFSync}} {
		if err := s.conn.QueryRowContext(ctx, entry.query).Scan(entry.dest); err != nil {
			return err
		}
	}
	rows, err := s.conn.QueryContext(ctx, "SELECT compile_options FROM pragma_compile_options ORDER BY compile_options")
	if err != nil {
		return err
	}
	var options []string
	for rows.Next() {
		var option string
		if err := rows.Scan(&option); err != nil {
			rows.Close()
			return err
		}
		options = append(options, option)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	s.settings.SQLiteCompileOptionsHash = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(options, "\n"))))
	parts := strings.Split(s.settings.SQLiteVersion, ".")
	version := 0
	for _, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return err
		}
		version = version*1000 + n
	}
	if version < 3051003 || s.settings.JournalMode != "wal" || s.settings.Synchronous != 2 || (runtime.GOOS == "darwin" && s.settings.FullFSync != 1) {
		return fmt.Errorf("unsupported SQLite local profile: %+v", s.settings)
	}
	s.settings.PowerLossQualified = LocalProfileSupported(s.settings)
	if !s.settings.PowerLossQualified {
		return fmt.Errorf("unqualified local durability platform: %+v", s.settings)
	}
	if _, err := s.conn.ExecContext(ctx, migration+admissionMigration+sessionInputMigration+grantsMigration+egressMigration+completionMigration); err != nil {
		return err
	}
	if _, err := s.conn.ExecContext(ctx, "INSERT OR IGNORE INTO domain_config VALUES(1,?,?,?)", s.user, s.domain, "LOCAL"); err != nil {
		return err
	}
	var user, domain, profile string
	if err := s.conn.QueryRowContext(ctx, "SELECT user_id,domain_id,durability_profile FROM domain_config WHERE singleton=1").Scan(&user, &domain, &profile); err != nil {
		return err
	}
	if user != s.user || domain != s.domain || profile != "LOCAL" {
		return command.Fail("PERMISSION_DENIED")
	}
	s.settings.DurabilityProfile = profile
	return nil
}
func (s *Store) Settings() Settings { return s.settings }
func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return errors.Join(s.conn.Close(), s.db.Close())
}
func (s *Store) Transaction(ctx context.Context, point string, fn func(context.Context) error) error {
	return s.transact(ctx, "adjudication", point, fn)
}
func (s *Store) transact(ctx context.Context, domain, point string, fn func(context.Context) error) error {
	if ctx.Value(txKey{}) != nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.conn.BeginTx(ctx, nil)
	if err != nil {
		return storageError(err, false)
	}
	defer tx.Rollback()
	txctx := context.WithValue(ctx, txKey{}, &transaction{s, tx, domain})
	if err := fn(txctx); err != nil {
		return err
	}
	if err := persistenceBoundary(ctx, point, false); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return storageError(err, true)
	}
	return persistenceBoundary(ctx, point, true)
}
func storageError(err error, commit bool) error {
	if err == nil {
		return nil
	}
	code := "DEPENDENCY_UNAVAILABLE"
	category := v1.ErrorCategory_ERROR_CATEGORY_TRANSIENT
	acceptance := v1.CommandAcceptance_COMMAND_ACCEPTANCE_NOT_SUBMITTED
	if commit {
		code = "TRANSPORT_LOST"
		category = v1.ErrorCategory_ERROR_CATEGORY_INDETERMINATE
		acceptance = v1.CommandAcceptance_COMMAND_ACCEPTANCE_UNKNOWN
	}
	return &command.Failure{Detail: &v1.ContractError{Code: code, Category: category, CommandAcceptance: acceptance, RecoveryAction: "QUERY_OR_RETRY_ORIGINAL"}}
}
func (s *Store) read(ctx context.Context, fn func(querier) error) error {
	if t, ok := ctx.Value(txKey{}).(*transaction); ok {
		if t.store != s {
			return command.Fail("INVARIANT_VIOLATION")
		}
		return fn(t.tx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return fn(s.conn)
}
func (s *Store) writer(ctx context.Context, domain string) (*sql.Tx, error) {
	t, ok := ctx.Value(txKey{}).(*transaction)
	if !ok || t.store != s || t.domain != domain {
		return nil, command.Fail("INVARIANT_VIOLATION")
	}
	return t.tx, nil
}
func (s *Store) Position(ctx context.Context) (uint64, int64, error) {
	tx, err := s.writer(ctx, "adjudication")
	if err != nil {
		return 0, 0, err
	}
	var position uint64
	var now int64
	err = tx.QueryRowContext(ctx, "UPDATE commit_clock SET position=position+1 WHERE singleton=1 RETURNING position, CAST(unixepoch('subsec')*1000 AS INTEGER)").Scan(&position, &now)
	return position, now, storageError(err, false)
}
func (s *Store) LoadReceipt(ctx context.Context, id *v1.CommandIdentity) (*v1.CommandReceipt, error) {
	r := new(v1.CommandReceipt)
	found, err := s.load(ctx, r, "SELECT record FROM command_receipts WHERE user_id=? AND issuer_id=? AND domain_id=? AND command_id=?", id.UserId, id.IssuerId, id.TargetDomainId, id.CommandId)
	if !found {
		return nil, err
	}
	return r, err
}
func (s *Store) SaveReceipt(ctx context.Context, r *v1.CommandReceipt) error {
	tx, err := s.writer(ctx, "adjudication")
	if err != nil {
		return err
	}
	b, err := proto.Marshal(r)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO command_receipts VALUES(?,?,?,?,?) ON CONFLICT(user_id,issuer_id,domain_id,command_id) DO UPDATE SET record=excluded.record", r.Identity.UserId, r.Identity.IssuerId, r.Identity.TargetDomainId, r.Identity.CommandId, b)
	return storageError(err, false)
}
func (s *Store) SaveJob(ctx context.Context, j *v1.Job) error {
	tx, err := s.writer(ctx, "adjudication")
	if err != nil {
		return err
	}
	b, err := proto.Marshal(j)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO jobs VALUES(?,?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET state=excluded.state, record=excluded.record", j.Ref.Name.UserId, j.Ref.Name.AuthorityDomainId, j.Ref.Name.LocalId, j.PurposeKey, j.State, b)
	return storageError(err, false)
}
func (s *Store) PendingJobs(ctx context.Context, user string) ([]*v1.Job, error) {
	var jobs []*v1.Job
	err := s.read(ctx, func(q querier) error {
		rows, err := q.QueryContext(ctx, "SELECT record FROM jobs WHERE user_id=? AND domain_id=? AND state IN ('READY','CLAIMED','WAITING') ORDER BY id", user, s.domain)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if err := rows.Scan(&b); err != nil {
				return err
			}
			j := new(v1.Job)
			if err := proto.Unmarshal(b, j); err != nil {
				return err
			}
			jobs = append(jobs, j)
		}
		return rows.Err()
	})
	return jobs, storageError(err, false)
}
func (s *Store) load(ctx context.Context, message proto.Message, query string, args ...any) (bool, error) {
	found := true
	err := s.read(ctx, func(q querier) error {
		var b []byte
		err := q.QueryRowContext(ctx, query, args...).Scan(&b)
		if errors.Is(err, sql.ErrNoRows) {
			found = false
			return nil
		}
		if err != nil {
			return err
		}
		return proto.Unmarshal(b, message)
	})
	return found, storageError(err, false)
}
func (s *Store) SaveTask(ctx context.Context, t *v1.Task) error {
	return s.saveObject(ctx, "tasks", t.TaskId, t)
}
func (s *Store) SaveSession(ctx context.Context, t *v1.Session) error {
	return s.saveObject(ctx, "sessions", t.SessionId, t)
}
func (s *Store) saveObject(ctx context.Context, table string, n *v1.GlobalName, m proto.Message) error {
	tx, err := s.writer(ctx, "adjudication")
	if err != nil {
		return err
	}
	b, err := proto.Marshal(m)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO "+table+" VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", n.UserId, n.AuthorityDomainId, n.LocalId, b)
	return storageError(err, false)
}
func (s *Store) LoadTask(ctx context.Context, n *v1.GlobalName) (*v1.Task, error) {
	t := new(v1.Task)
	found, err := s.load(ctx, t, "SELECT record FROM tasks WHERE user_id=? AND domain_id=? AND id=?", n.UserId, n.AuthorityDomainId, n.LocalId)
	if !found {
		return nil, err
	}
	return t, err
}
func (s *Store) LoadSession(ctx context.Context, n *v1.GlobalName) (*v1.Session, error) {
	t := new(v1.Session)
	found, err := s.load(ctx, t, "SELECT record FROM sessions WHERE user_id=? AND domain_id=? AND id=?", n.UserId, n.AuthorityDomainId, n.LocalId)
	if !found {
		return nil, err
	}
	return t, err
}

// StageContent 使用独立内容事务，不能参加裁决事务。
func (s *Store) StageContent(ctx context.Context, c *v1.Content, fingerprint string) (*v1.Ref, error) {
	var ref *v1.Ref
	err := s.transact(ctx, "content", "content.stage", func(ctx context.Context) error {
		old := new(v1.Content)
		found, err := s.load(ctx, old, "SELECT record FROM content WHERE user_id=? AND domain_id=? AND source_issuer=? AND source_command=? AND fingerprint=?", c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Source.IssuerId, c.Source.CommandId, fingerprint)
		if err != nil {
			return err
		}
		if found {
			ref = old.Ref
			return nil
		}
		tx, err := s.writer(ctx, "content")
		if err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, "SELECT CAST(unixepoch('subsec')*1000 AS INTEGER)").Scan(&c.AcquiredAtUnixMs); err != nil {
			return err
		}
		b, err := proto.Marshal(c)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, "INSERT INTO content VALUES(?,?,?,?,?,?,?)", c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId, c.Source.IssuerId, c.Source.CommandId, fingerprint, b)
		ref = c.Ref
		return storageError(err, false)
	})
	if err != nil {
		return nil, err
	}
	return ref, nil
}
func (s *Store) ReadContent(ctx context.Context, r *v1.Ref) (*v1.Content, error) {
	if e := contentReadBoundary(ctx); e != nil {
		return nil, e
	}
	c := new(v1.Content)
	found, err := s.load(ctx, c, "SELECT record FROM content WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !found {
		return nil, err
	}
	return c, err
}

func (s *Store) LoadJob(ctx context.Context, n *v1.GlobalName) (*v1.Job, error) {
	j := new(v1.Job)
	found, err := s.load(ctx, j, "SELECT record FROM jobs WHERE user_id=? AND domain_id=? AND id=?", n.UserId, n.AuthorityDomainId, n.LocalId)
	if !found {
		return nil, err
	}
	return j, err
}

func (s *Store) FindJobPurpose(ctx context.Context, user, purpose string) (*v1.Job, error) {
	j := new(v1.Job)
	found, err := s.load(ctx, j, "SELECT record FROM jobs WHERE user_id=? AND domain_id=? AND purpose_key=?", user, s.domain, purpose)
	if !found {
		return nil, err
	}
	return j, err
}

func (s *Store) ledgerWorkTransaction(ctx context.Context, point string, fn func(context.Context) error) error {
	return s.transact(ctx, "ledger", point, fn)
}

func (s *Store) contentWorkTransaction(ctx context.Context, point string, fn func(context.Context) error) error {
	return s.transact(ctx, "content", point, fn)
}
func (s *Store) traceWorkTransaction(ctx context.Context, point string, fn func(context.Context) error) error {
	return s.transact(ctx, "trace", point, fn)
}
