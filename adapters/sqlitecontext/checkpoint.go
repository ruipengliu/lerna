package sqlitecontext

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"

	"lerna/contextassembly"
)

func (s *Store) BindCheckpoint(ctx context.Context, key contextassembly.Key, expected [32]byte) error {
	if !validKey(key) {
		return contextassembly.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return contextassembly.Unavailable
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `UPDATE context_lock SET version=version WHERE id=1`); err != nil {
		return contextassembly.Unavailable
	}
	snapshot, err := read(ctx, tx, key)
	if err != nil {
		return err
	}
	if sha256.Sum256(snapshot.Document) != expected {
		return contextassembly.IdentityConflict
	}
	var digest []byte
	err = tx.QueryRowContext(ctx, `SELECT digest FROM context_checkpoints WHERE namespace=? AND task_id=? AND decision=?`, key.Namespace, key.TaskID, key.Decision).Scan(&digest)
	if err == nil {
		if !bytes.Equal(digest, expected[:]) {
			return contextassembly.IdentityConflict
		}
		return nil
	}
	if err != sql.ErrNoRows {
		return contextassembly.Unavailable
	}
	// Every entry requires a live snapshot. Snapshot capacity also bounds pins.
	if _, err = tx.ExecContext(ctx, `INSERT INTO context_checkpoints VALUES(?,?,?,?)`, key.Namespace, key.TaskID, key.Decision, expected[:]); err != nil {
		return contextassembly.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return contextassembly.Unavailable
	}
	return nil
}

func (s *Store) VerifyCheckpoint(ctx context.Context, key contextassembly.Key) error {
	if !validKey(key) {
		return contextassembly.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return contextassembly.Unavailable
	}
	defer tx.Rollback()
	snapshot, state := read(ctx, tx, key)
	if state != nil && state != contextassembly.Invalidated {
		return state
	}
	var digest []byte
	err = tx.QueryRowContext(ctx, `SELECT digest FROM context_checkpoints WHERE namespace=? AND task_id=? AND decision=?`, key.Namespace, key.TaskID, key.Decision).Scan(&digest)
	if err == sql.ErrNoRows {
		if state == contextassembly.Invalidated {
			return state
		}
		return contextassembly.Missing
	}
	if err != nil || len(digest) != 32 || state == contextassembly.Invalidated {
		return contextassembly.Unavailable
	}
	actual := sha256.Sum256(snapshot.Document)
	if !bytes.Equal(actual[:], digest) {
		return contextassembly.IdentityConflict
	}
	return nil
}

var _ contextassembly.CheckpointStore = (*Store)(nil)
