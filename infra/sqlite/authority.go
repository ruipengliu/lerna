package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveGrant(ctx context.Context, g *v1.Grant) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO grants VALUES(?,?,?,?)", g, g.Ref.Name.UserId, g.Ref.Name.AuthorityDomainId, g.Ref.Name.LocalId)
}
func (s *Store) LoadGrant(ctx context.Context, r *v1.Ref) (*v1.Grant, error) {
	g := new(v1.Grant)
	ok, e := s.load(ctx, g, "SELECT record FROM grants WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return g, e
}
func (s *Store) SaveGrantUse(ctx context.Context, g *v1.GrantUse) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO grant_uses VALUES(?,?,?,?,?,?)", g, g.Ref.Name.UserId, g.Ref.Name.AuthorityDomainId, g.Ref.Name.LocalId, g.UsePoolId, g.OperationId.LocalId)
}
func (s *Store) LoadGrantUse(ctx context.Context, id *v1.GlobalName) (*v1.GrantUse, error) {
	g := new(v1.GrantUse)
	ok, e := s.load(ctx, g, "SELECT record FROM grant_uses WHERE user_id=? AND operation_id=?", id.UserId, id.LocalId)
	if !ok {
		return nil, e
	}
	return g, e
}
func budgetScope(id *v1.GlobalName) string {
	if id == nil {
		return "USER"
	}
	return "TASK:" + id.LocalId
}
func (s *Store) SaveBudget(ctx context.Context, b *v1.Budget) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO budgets VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,scope_id) DO UPDATE SET record=excluded.record", b, b.Ref.Name.UserId, b.Ref.Name.AuthorityDomainId, budgetScope(b.TaskId))
}
func (s *Store) LoadBudget(ctx context.Context, id *v1.GlobalName) (*v1.Budget, error) {
	b := new(v1.Budget)
	ok, e := s.load(ctx, b, "SELECT record FROM budgets WHERE user_id=? AND domain_id=? AND scope_id=?", s.user, s.domain, budgetScope(id))
	if !ok {
		return nil, e
	}
	return b, e
}
func (s *Store) SaveReservation(ctx context.Context, r *v1.Reservation) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO reservations VALUES(?,?,?,?,?)", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId, r.OperationId.LocalId)
}
func (s *Store) LoadReservation(ctx context.Context, r *v1.Ref) (*v1.Reservation, error) {
	v := new(v1.Reservation)
	ok, e := s.load(ctx, v, "SELECT record FROM reservations WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}

// saveRecord 只封装本适配器的绑定与序列化，表与事务域由每个有类型方法固定。
func (s *Store) saveRecord(ctx context.Context, domain, query string, m proto.Message, args ...any) error {
	tx, e := s.writer(ctx, domain)
	if e != nil {
		return e
	}
	b, e := proto.Marshal(m)
	if e != nil {
		return e
	}
	args = append(args, b)
	_, e = tx.ExecContext(ctx, query, args...)
	return storageError(e, false)
}
func (s *Store) GrantUses(ctx context.Context, id *v1.GlobalName) ([]*v1.GrantUse, error) {
	var result []*v1.GrantUse
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM grant_uses WHERE user_id=? AND domain_id=?", id.UserId, id.AuthorityDomainId)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.GrantUse)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			if proto.Equal(v.TaskId, id) {
				result = append(result, v)
			}
		}
		return rows.Err()
	})
	return result, e
}
func (s *Store) Reservations(ctx context.Context, id *v1.GlobalName) ([]*v1.Reservation, error) {
	var result []*v1.Reservation
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM reservations WHERE user_id=? AND domain_id=?", id.UserId, id.AuthorityDomainId)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.Reservation)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			if proto.Equal(v.TaskId, id) {
				result = append(result, v)
			}
		}
		return rows.Err()
	})
	return result, e
}
