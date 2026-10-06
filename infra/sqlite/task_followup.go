package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveExecutionFollowup(ctx context.Context, f *v1.ExecutionFollowup) error {
	return s.saveRecord(ctx, "ledger", "INSERT INTO execution_followups VALUES(?,?,?,?,?)", f, f.Ref.Name.UserId, f.Ref.Name.AuthorityDomainId, f.Ref.Name.LocalId, f.OperationId.LocalId)
}
func (s *Store) LoadExecutionFollowup(ctx context.Context, r *v1.Ref) (*v1.ExecutionFollowup, error) {
	v := new(v1.ExecutionFollowup)
	ok, e := s.load(ctx, v, "SELECT record FROM execution_followups WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) SaveSettlementFollowup(ctx context.Context, f *v1.SettlementFollowup) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO settlement_followups VALUES(?,?,?,?)", f, f.Ref.Name.UserId, f.Ref.Name.AuthorityDomainId, f.Ref.Name.LocalId)
}
func (s *Store) LoadSettlementFollowup(ctx context.Context, r *v1.Ref) (*v1.SettlementFollowup, error) {
	v := new(v1.SettlementFollowup)
	ok, e := s.load(ctx, v, "SELECT record FROM settlement_followups WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) BillingSourcesForOperation(ctx context.Context, id *v1.GlobalName) ([]*v1.BillingSource, error) {
	var all []*v1.BillingSource
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM billing_sources WHERE user_id=? AND domain_id=? ORDER BY send_id", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.BillingSource)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			if proto.Equal(v.OperationId, id) {
				all = append(all, v)
			}
		}
		return rows.Err()
	})
	return all, e
}

func (s *Store) AllExecutionFollowups(ctx context.Context) ([]*v1.ExecutionFollowup, error) {
	var all []*v1.ExecutionFollowup
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM execution_followups WHERE user_id=? AND domain_id=? ORDER BY id", s.user, s.domain+"/ledger")
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.ExecutionFollowup)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			all = append(all, v)
		}
		return rows.Err()
	})
	return all, e
}
func (s *Store) AllSettlementFollowups(ctx context.Context) ([]*v1.SettlementFollowup, error) {
	var all []*v1.SettlementFollowup
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM settlement_followups WHERE user_id=? AND domain_id=? ORDER BY id", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.SettlementFollowup)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			all = append(all, v)
		}
		return rows.Err()
	})
	return all, e
}
