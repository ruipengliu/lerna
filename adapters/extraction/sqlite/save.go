package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"google.golang.org/protobuf/proto"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
)

var _ extraction.SaveJournal = (*Store)(nil)

func (s *Store) ReserveSave(ctx context.Context, namespace, subject, candidate string, request *wire.MemoryWrite) error {
	if !name(namespace) || !name(subject) || !name(candidate) || request == nil || request.Ref == nil || request.Spec == nil || !name(request.OperationId) || request.Ref.Namespace != namespace || !name(request.Ref.Collection) || !name(request.Ref.Key) || request.ExpectedRevision != 0 || proto.Size(request) > maxDocument {
		return memory.Invalid
	}
	request = proto.Clone(request).(*wire.MemoryWrite)
	raw, err := (proto.MarshalOptions{Deterministic: true}).Marshal(request)
	if err != nil {
		return memory.Invalid
	}
	identity, err := proto.Marshal(&wire.MemoryWrite{OperationId: request.OperationId, Ref: request.Ref})
	if err != nil {
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
	var oldSubject, state string
	err = tx.QueryRowContext(ctx, `SELECT subject,'retired' FROM retired_candidates WHERE namespace=? AND operation=? UNION ALL SELECT json_extract(document,'$.Subject'),'retained' FROM candidates WHERE namespace=? AND operation=?`, namespace, candidate, namespace, candidate).Scan(&oldSubject, &state)
	if errors.Is(err, sql.ErrNoRows) {
		return memory.Missing
	}
	if err != nil {
		return memory.Unavailable
	}
	if oldSubject != subject {
		return memory.Denied
	}
	if state == "retired" {
		return memory.ReplayUnavailable
	}
	var old []byte
	err = tx.QueryRowContext(ctx, `SELECT document FROM candidate_saves WHERE namespace=? AND candidate=?`, namespace, candidate).Scan(&old)
	if err == nil {
		if string(old) != string(raw) {
			return memory.IdentityConflict
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return memory.Unavailable
	}
	var reused int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM candidate_saves WHERE namespace=? AND operation=?`, namespace, request.OperationId).Scan(&reused); err != nil {
		return memory.Unavailable
	}
	if reused != 0 {
		return memory.IdentityConflict
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO candidate_saves(namespace,candidate,subject,operation,state,identity,document) VALUES(?,?,?,?,'pending',?,?)`, namespace, candidate, subject, request.OperationId, identity, raw); err != nil {
		return memory.Unavailable
	}
	if err = tx.Commit(); err != nil {
		return memory.Unavailable
	}
	return nil
}

func (s *Store) LookupSave(ctx context.Context, namespace, subject, candidate string) (extraction.SaveIntent, error) {
	if !name(namespace) || !name(subject) || !name(candidate) {
		return extraction.SaveIntent{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var out extraction.SaveIntent
	var raw []byte
	var owner, operation string
	err := s.db.QueryRowContext(ctx, `SELECT subject,operation,state,document FROM candidate_saves WHERE namespace=? AND candidate=?`, namespace, candidate).Scan(&owner, &operation, &out.State, &raw)
	if errors.Is(err, sql.ErrNoRows) {
		return extraction.SaveIntent{}, memory.Missing
	}
	if err != nil || len(raw) > maxDocument {
		return extraction.SaveIntent{}, memory.Unavailable
	}
	if owner != subject {
		return extraction.SaveIntent{}, memory.Denied
	}
	out.Request = new(wire.MemoryWrite)
	if proto.Unmarshal(raw, out.Request) != nil || out.Request.OperationId != operation || out.Request.GetRef().GetNamespace() != namespace || (out.State != "pending" && out.State != "retired") || (out.State == "retired" && out.Request.Spec != nil) || (out.State == "pending" && out.Request.Spec == nil) {
		return extraction.SaveIntent{}, memory.Unavailable
	}
	return out, nil
}
