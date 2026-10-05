package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveOperationProgressHandoff(ctx context.Context, h *v1.OperationProgressHandoff) error {
	n := h.Notice.Ref.Name
	return s.saveRecord(ctx, "ledger", "INSERT INTO operation_progress_handoffs VALUES(?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", h, n.UserId, n.AuthorityDomainId, n.LocalId, h.Notice.OperationRef.Name.LocalId)
}
func (s *Store) LoadOperationProgressHandoff(ctx context.Context, r *v1.Ref) (*v1.OperationProgressHandoff, error) {
	h := new(v1.OperationProgressHandoff)
	ok, e := s.load(ctx, h, "SELECT record FROM operation_progress_handoffs WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return h, e
}
func (s *Store) OperationProgressHandoffs(ctx context.Context, id *v1.GlobalName) ([]*v1.OperationProgressHandoff, error) {
	var all []*v1.OperationProgressHandoff
	e := s.read(ctx, func(q querier) error {
		query := "SELECT record FROM operation_progress_handoffs WHERE user_id=? AND domain_id=?"
		args := []any{s.user, s.domain + "/ledger"}
		if id != nil {
			query += " AND operation_id=?"
			args = append(args, id.LocalId)
		}
		query += " ORDER BY rowid"
		rows, e := q.QueryContext(ctx, query, args...)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			h := new(v1.OperationProgressHandoff)
			if e = proto.Unmarshal(b, h); e != nil {
				return e
			}
			all = append(all, h)
		}
		return rows.Err()
	})
	return all, e
}
func (s *Store) SaveTaskOperationProgress(ctx context.Context, n *v1.OperationProgressNotice) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO task_operation_progress VALUES(?,?,?,?)", n, n.Ref.Name.UserId, s.domain, n.Ref.Name.LocalId)
}
func (s *Store) LoadTaskOperationProgress(ctx context.Context, r *v1.Ref) (*v1.OperationProgressNotice, error) {
	n := new(v1.OperationProgressNotice)
	ok, e := s.load(ctx, n, "SELECT record FROM task_operation_progress WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, s.domain, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return n, e
}
