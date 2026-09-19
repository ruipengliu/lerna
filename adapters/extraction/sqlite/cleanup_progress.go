package sqlite

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"lerna/extraction"
	"lerna/memory"
)

func validCleanupBinding(b extraction.CleanupBinding) bool {
	raw, err := hex.DecodeString(b.ConfigSHA256)
	return name(b.Namespace) && name(b.Subject) && name(b.Consumer) && err == nil && len(raw) == 32 && hex.EncodeToString(raw) == b.ConfigSHA256
}
func (s *Store) LoadCleanupProgress(ctx context.Context, b extraction.CleanupBinding) (extraction.CleanupProgress, error) {
	if !validCleanupBinding(b) {
		return extraction.CleanupProgress{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var p extraction.CleanupProgress
	var config string
	err := s.db.QueryRowContext(ctx, `SELECT config,version,after_candidate FROM cleanup_progress WHERE namespace=? AND subject=? AND consumer=?`, b.Namespace, b.Subject, b.Consumer).Scan(&config, &p.Version, &p.After)
	if errors.Is(err, sql.ErrNoRows) {
		return p, memory.Missing
	}
	if err != nil {
		return p, memory.Unavailable
	}
	if config != b.ConfigSHA256 {
		return extraction.CleanupProgress{}, memory.IdentityConflict
	}
	return p, nil
}
func (s *Store) AdvanceCleanupProgress(ctx context.Context, b extraction.CleanupBinding, expected uint64, after string) error {
	if !validCleanupBinding(b) || expected >= 1<<32-1 || len(after) > 256 {
		return memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return memory.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE candidate_lock SET version=version WHERE id=1`); err != nil {
		return memory.Unavailable
	}
	var config string
	var version uint64
	err = tx.QueryRowContext(ctx, `SELECT config,version FROM cleanup_progress WHERE namespace=? AND subject=? AND consumer=?`, b.Namespace, b.Subject, b.Consumer).Scan(&config, &version)
	if errors.Is(err, sql.ErrNoRows) {
		if expected != 0 {
			return memory.Conflict
		}
		var count int
		if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM cleanup_progress`).Scan(&count); err != nil {
			return memory.Unavailable
		}
		if count >= 64 {
			return memory.Capacity
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO cleanup_progress(namespace,subject,consumer,config,version,after_candidate) VALUES(?,?,?,?,1,?)`, b.Namespace, b.Subject, b.Consumer, b.ConfigSHA256, after)
	} else {
		if err != nil {
			return memory.Unavailable
		}
		if config != b.ConfigSHA256 {
			return memory.IdentityConflict
		}
		if version != expected {
			return memory.Conflict
		}
		_, err = tx.ExecContext(ctx, `UPDATE cleanup_progress SET version=?,after_candidate=? WHERE namespace=? AND subject=? AND consumer=?`, version+1, after, b.Namespace, b.Subject, b.Consumer)
	}
	if err != nil {
		return memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}
