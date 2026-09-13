package sqliteextraction

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"google.golang.org/protobuf/proto"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

func (s *Store) ListRetiredSaves(ctx context.Context, namespace, subject, after string, limit int) ([]string, error) {
	if !name(namespace) || !name(subject) || len(after) > 256 || limit < 1 || limit > 16 {
		return nil, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	rows, err := s.db.QueryContext(ctx, `SELECT candidate FROM candidate_saves WHERE namespace=? AND subject=? AND state='retired' AND candidate>? ORDER BY candidate LIMIT ?`, namespace, subject, after, limit)
	if err != nil {
		return nil, memory.Unavailable
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil || !name(id) {
			return nil, memory.Unavailable
		}
		out = append(out, id)
	}
	if rows.Err() != nil {
		return nil, memory.Unavailable
	}
	return out, nil
}

func (s *Store) ReserveCleanup(ctx context.Context, namespace, subject, candidate string, in memory.DeleteRequest) error {
	if !name(namespace) || !name(subject) || !name(candidate) || !name(in.OperationID) || !name(in.Purpose) || in.Ref == nil || in.Ref.Namespace != namespace || !name(in.Ref.Collection) || !name(in.Ref.Key) || in.ExpectedRevision == 0 || in.ExpectedRevision >= 1<<32 {
		return memory.Invalid
	}
	in.Ref = proto.Clone(in.Ref).(*wire.MemoryRef)
	raw, err := json.Marshal(in)
	if err != nil || len(raw) > 4096 {
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
	var state, owner string
	var identity []byte
	err = tx.QueryRowContext(ctx, `SELECT subject,state,identity FROM candidate_saves WHERE namespace=? AND candidate=?`, namespace, candidate).Scan(&owner, &state, &identity)
	if errors.Is(err, sql.ErrNoRows) {
		return memory.Missing
	}
	if err != nil {
		return memory.Unavailable
	}
	if owner != subject || state != "retired" {
		return memory.Denied
	}
	write := new(wire.MemoryWrite)
	if proto.Unmarshal(identity, write) != nil || !proto.Equal(write.Ref, in.Ref) {
		return memory.IdentityConflict
	}
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT document FROM candidate_cleanup WHERE namespace=? AND candidate=?`, namespace, candidate).Scan(&old)
	if err == nil {
		if string(old) != string(raw) {
			return memory.IdentityConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return memory.Unavailable
	}
	var count int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM candidate_cleanup WHERE namespace=? AND operation=?`, namespace, in.OperationID).Scan(&count); err != nil {
		return memory.Unavailable
	}
	if count != 0 {
		return memory.IdentityConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO candidate_cleanup(namespace,candidate,subject,operation,document) VALUES(?,?,?,?,?)`, namespace, candidate, subject, in.OperationID, raw); err != nil {
		return memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}
func (s *Store) LookupCleanup(ctx context.Context, namespace, subject, candidate string) (memory.DeleteRequest, error) {
	if !name(namespace) || !name(subject) || !name(candidate) {
		return memory.DeleteRequest{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var raw []byte
	var owner, operation string
	err := s.db.QueryRowContext(ctx, `SELECT subject,operation,document FROM candidate_cleanup WHERE namespace=? AND candidate=?`, namespace, candidate).Scan(&owner, &operation, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return memory.DeleteRequest{}, memory.Missing
	}
	if err != nil || len(raw) > 4096 {
		return memory.DeleteRequest{}, memory.Unavailable
	}
	if owner != subject {
		return memory.DeleteRequest{}, memory.Denied
	}
	var in memory.DeleteRequest
	if json.Unmarshal(raw, &in) != nil || in.OperationID != operation || in.Ref == nil || in.Ref.Namespace != namespace {
		return memory.DeleteRequest{}, memory.Unavailable
	}
	return in, nil
}
