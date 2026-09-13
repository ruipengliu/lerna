package sqlitememory

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"lerna/memory"
)

func validBinding(b memory.ReadBinding) bool {
	hash, e := hex.DecodeString(b.SemanticSHA256)
	if !label(b.Namespace) || !label(b.ID) || !label(b.Subject) || !label(b.PermitID) || e != nil || len(hash) != 32 || hex.EncodeToString(hash) != b.SemanticSHA256 || len(b.Results) > 32 {
		return false
	}
	switch b.Coverage {
	case "complete", "budget_exhausted", "partial_unavailable", "partial_and_budget_exhausted":
	default:
		return false
	}
	witnesses, err := b.CoverageReferences()
	if err != nil {
		return false
	}
	for _, r := range witnesses {
		if !validRef(r.Ref) || r.Ref.Namespace != b.Namespace || r.Revision == 0 || r.Revision > 1<<32 {
			return false
		}
	}
	seen := map[memory.VersionRef]bool{}
	for _, r := range b.Results {
		if !validRef(r.Ref) || r.Ref.Namespace != b.Namespace || r.Revision == 0 || r.Revision > 1<<32 || seen[r] {
			return false
		}
		seen[r] = true
	}
	return true
}
func lookupRead(ctx context.Context, q querier, namespace, id string) (memory.ReadBinding, error) {
	var raw []byte
	e := q.QueryRowContext(ctx, `SELECT document FROM memory_disclosures WHERE namespace=? AND read_id=?`, namespace, id).Scan(&raw)
	if e == sql.ErrNoRows {
		return memory.ReadBinding{}, memory.Missing
	}
	if e != nil {
		return memory.ReadBinding{}, memory.Unavailable
	}
	var out memory.ReadBinding
	if len(raw) > 16384 || json.Unmarshal(raw, &out) != nil || !validBinding(out) || out.Namespace != namespace || out.ID != id {
		return memory.ReadBinding{}, memory.Unavailable
	}
	return out, nil
}
func (s *Store) LookupRead(ctx context.Context, namespace, id string) (memory.ReadBinding, error) {
	if !label(namespace) || !label(id) {
		return memory.ReadBinding{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	return lookupRead(ctx, s.db, namespace, id)
}
func (s *Store) BindRead(ctx context.Context, in memory.ReadBinding) (memory.ReadBinding, error) {
	if !validBinding(in) {
		return memory.ReadBinding{}, memory.Invalid
	}
	raw, e := json.Marshal(in)
	if e != nil || len(raw) > 16384 {
		return memory.ReadBinding{}, memory.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return memory.ReadBinding{}, memory.Unavailable
	}
	defer tx.Rollback()
	if _, e = tx.ExecContext(ctx, `UPDATE memory_lock SET version=version WHERE id=1`); e != nil {
		return memory.ReadBinding{}, memory.Unavailable
	}
	old, e := lookupRead(ctx, tx, in.Namespace, in.ID)
	if e == nil {
		if old.Subject != in.Subject || old.SemanticSHA256 != in.SemanticSHA256 || old.PermitID != in.PermitID {
			return memory.ReadBinding{}, memory.IdentityConflict
		}
		return old, nil
	}
	if e != memory.Missing {
		return memory.ReadBinding{}, e
	}
	var count int
	if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_disclosures`).Scan(&count); e != nil {
		return memory.ReadBinding{}, memory.Unavailable
	}
	if count >= 512 {
		return memory.ReadBinding{}, memory.Capacity
	}
	witnesses, _ := in.CoverageReferences()
	for _, r := range append(witnesses, in.Results...) {
		if e = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM memory_revisions WHERE namespace=? AND collection=? AND record_key=? AND revision=?`, r.Ref.Namespace, r.Ref.Collection, r.Ref.Key, r.Revision).Scan(&count); e != nil {
			return memory.ReadBinding{}, memory.Unavailable
		}
		if count != 1 {
			return memory.ReadBinding{}, memory.Missing
		}
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO memory_disclosures VALUES(?,?,?)`, in.Namespace, in.ID, raw); e != nil {
		return memory.ReadBinding{}, memory.Unavailable
	}
	if e = tx.Commit(); e != nil {
		return memory.ReadBinding{}, memory.Unavailable
	}
	// Decode the durable representation so caller-owned result slices cannot
	// mutate the returned binding or become retained by the adapter.
	var out memory.ReadBinding
	if json.Unmarshal(raw, &out) != nil {
		return memory.ReadBinding{}, memory.Unavailable
	}
	return out, nil
}
