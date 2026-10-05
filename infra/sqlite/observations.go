package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ContentWork 只为内容命令提供独立的事务域和提交时钟。
type ContentWork struct{ *Store }

func (s *Store) ContentWork() *ContentWork { return &ContentWork{s} }
func (w *ContentWork) Transaction(ctx context.Context, point string, fn func(context.Context) error) error {
	return w.contentWorkTransaction(ctx, point, fn)
}
func (w *ContentWork) BusinessScope(ctx context.Context, fn func(context.Context) error) error {
	return w.businessScope(ctx, "content", fn)
}
func (w *ContentWork) Position(ctx context.Context) (uint64, int64, error) {
	tx, e := w.writer(ctx, "content")
	if e != nil {
		return 0, 0, e
	}
	var pos uint64
	var now int64
	e = tx.QueryRowContext(ctx, "UPDATE content_commit_clock SET position=position+1 WHERE singleton=1 RETURNING position, CAST(unixepoch('subsec')*1000 AS INTEGER)").Scan(&pos, &now)
	return pos, now, storageError(e, false)
}
func (w *ContentWork) SaveReceipt(ctx context.Context, r *v1.CommandReceipt) error {
	return w.saveRecord(ctx, "content", "INSERT INTO command_receipts VALUES(?,?,?,?,?)", r, r.Identity.UserId, r.Identity.IssuerId, r.Identity.TargetDomainId, r.Identity.CommandId)
}
func (s *Store) SaveObservationContent(ctx context.Context, c *v1.Content) error {
	return s.saveRecord(ctx, "content", "INSERT INTO content VALUES(?,?,?,?,?,?,?)", c, c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId, c.Source.IssuerId, c.Source.CommandId, "")
}
func (s *Store) SaveObservationHandoff(ctx context.Context, h *v1.ObservationHandoff) error {
	r := h.Observation.Ref
	return s.saveRecord(ctx, "content", "INSERT INTO content_observations VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", h, r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
}
func (s *Store) LoadObservationHandoff(ctx context.Context, r *v1.Ref) (*v1.ObservationHandoff, error) {
	h := new(v1.ObservationHandoff)
	ok, e := s.load(ctx, h, "SELECT record FROM content_observations WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return h, e
}
func (s *Store) PendingObservations(ctx context.Context) ([]*v1.ObservationHandoff, error) {
	var all []*v1.ObservationHandoff
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM content_observations WHERE user_id=?", s.user)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			h := new(v1.ObservationHandoff)
			if e = proto.Unmarshal(b, h); e != nil {
				return e
			}
			if h.RecipientReceipt == nil {
				all = append(all, h)
			}
		}
		return rows.Err()
	})
	return all, e
}
func (s *Store) SaveLedgerObservation(ctx context.Context, r *v1.RawObservation) error {
	return s.saveRecord(ctx, "ledger", "INSERT INTO ledger_observations VALUES(?,?,?,?)", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId)
}
func (s *Store) LoadLedgerObservation(ctx context.Context, r *v1.Ref) (*v1.RawObservation, error) {
	o := new(v1.RawObservation)
	ok, e := s.load(ctx, o, "SELECT record FROM ledger_observations WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return o, e
}
