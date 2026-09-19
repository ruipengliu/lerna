package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"lerna/credentials"
)

type rowQuerier interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadLifecycle(ctx context.Context, q rowQuerier) (credentials.LifecycleState, error) {
	var s credentials.LifecycleState
	var revision uint64
	var raw []byte
	if e := q.QueryRowContext(ctx, "SELECT revision,document FROM credential_lifecycle WHERE id=1").Scan(&revision, &raw); e != nil {
		return s, credentials.Unavailable
	}
	if len(raw) > 524288 || json.Unmarshal(raw, &s) != nil || s.Revision != revision || !credentials.ValidLifecycleState(s) {
		return credentials.LifecycleState{}, credentials.Unavailable
	}
	return s, nil
}
func (s *Store) LoadLifecycle(ctx context.Context) (credentials.LifecycleState, error) {
	return loadLifecycle(ctx, s.db)
}
func writeAllowed(s credentials.LifecycleState, r credentials.Record) bool {
	for _, retired := range s.Retirements {
		if retired.KeyVersion == r.KeyVersion {
			return false
		}
	}

	for _, rotation := range s.Rotations {
		if rotation.Phase == "active" || rotation.Phase == "completed" {
			for _, old := range rotation.SourceKeys {
				if r.KeyVersion == old {
					return false
				}
			}
		}

		if rotation.Phase == "active" && rotation.Binding == r.Binding && r.KeyVersion != rotation.KeyVersion {
			return false
		}
	}
	return true
}
func swapRecord(ctx context.Context, tx *sql.Tx, expected uint64, r credentials.Record, raw []byte) error {
	var result sql.Result
	var e error
	if expected == 0 {
		result, e = tx.ExecContext(ctx, `INSERT OR IGNORE INTO credential_ciphertext(ref,revision,document) SELECT ?,?,? WHERE (SELECT count(*) FROM credential_ciphertext)<64`, r.Ref, r.Revision, raw)
	} else {
		result, e = tx.ExecContext(ctx, `UPDATE credential_ciphertext SET revision=?,document=? WHERE ref=? AND revision=?`, r.Revision, raw, r.Ref, expected)
	}
	if e != nil {
		return credentials.Unavailable
	}
	n, e := result.RowsAffected()
	if e != nil {
		return credentials.Unavailable
	}
	if n != 1 {
		return credentials.Conflict
	}
	return nil
}
func (s *Store) CommitLifecycle(ctx context.Context, expected uint64, change credentials.LifecycleChange) error {
	if expected >= 1<<32-1 || change.State.Revision != expected+1 || !credentials.ValidLifecycleState(change.State) {
		return credentials.Invalid
	}
	raw, e := json.Marshal(change.State)
	if e != nil || len(raw) > 524288 {
		return credentials.Invalid
	}
	var recordRaw []byte
	if change.Record != nil {
		r := *change.Record
		if !credentials.ValidRecord(r) || change.ExpectedRecord >= 1<<32-1 || r.Revision != change.ExpectedRecord+1 || !writeAllowed(change.State, r) {
			return credentials.Invalid
		}
		recordRaw, e = json.Marshal(r)
		if e != nil || len(recordRaw) > maxDocument {
			return credentials.Invalid
		}
	} else if change.ExpectedRecord != 0 {
		return credentials.Invalid
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return credentials.Unavailable
	}
	defer tx.Rollback()
	current, e := loadLifecycle(ctx, tx)
	if e != nil {
		return e
	}
	if current.Revision != expected {
		return credentials.Conflict
	}
	for _, old := range current.Retirements {
		retained := false
		for _, next := range change.State.Retirements {
			if next.KeyVersion == old.KeyVersion && next.Binding == old.Binding {
				retained = true
			}
		}
		if !retained {
			return credentials.Invalid
		}
	}
	// A retirement fence and the absence of live ciphertext must be checked
	// under the same transaction as the journal update.
	if len(change.State.Retirements) > 0 {
		rows, err := tx.QueryContext(ctx, "SELECT document FROM credential_ciphertext")
		if err != nil {
			return credentials.Unavailable
		}
		for rows.Next() {
			var raw []byte
			if err = rows.Scan(&raw); err != nil {
				rows.Close()
				return credentials.Unavailable
			}
			record, err := decode(raw)
			if err != nil {
				rows.Close()
				return err
			}
			for _, retired := range change.State.Retirements {
				if record.KeyVersion == retired.KeyVersion {
					rows.Close()
					return credentials.Conflict
				}
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return credentials.Unavailable
		}
	}
	result, e := tx.ExecContext(ctx, "UPDATE credential_lifecycle SET revision=?,document=? WHERE id=1 AND revision=?", change.State.Revision, raw, expected)
	if e != nil {
		return credentials.Unavailable
	}
	n, e := result.RowsAffected()
	if e != nil {
		return credentials.Unavailable
	}
	if n != 1 {
		return credentials.Conflict
	}
	if change.Record != nil {
		if e = swapRecord(ctx, tx, change.ExpectedRecord, *change.Record, recordRaw); e != nil {
			return e
		}
	}
	if e = tx.Commit(); e != nil {
		return credentials.Unavailable
	}
	return nil
}
