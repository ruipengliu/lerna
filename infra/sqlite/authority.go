package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveGrant(ctx context.Context, g *v1.Grant) error {
	if e := s.saveRecord(ctx, "adjudication", "INSERT INTO grants VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", g, g.Ref.Name.UserId, g.Ref.Name.AuthorityDomainId, g.Ref.Name.LocalId); e != nil {
		return e
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO grant_versions VALUES(?,?,?,?,?)", g, g.Ref.Name.UserId, g.Ref.Name.AuthorityDomainId, g.Ref.Name.LocalId, g.Ref.Revision)
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
	old, e := s.LoadBudget(ctx, b.TaskId)
	if e != nil {
		return e
	}
	if old != nil {
		if e = s.saveBudgetVersion(ctx, old); e != nil {
			return e
		}
	}
	if e = s.saveBudgetVersion(ctx, b); e != nil {
		return e
	}
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
	old, e := s.LoadCurrentReservation(ctx, r.Ref)
	if e != nil {
		return e
	}
	if old != nil {
		if e = s.saveReservationVersion(ctx, old); e != nil {
			return e
		}
	}
	if e = s.saveReservationVersion(ctx, r); e != nil {
		return e
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO reservations VALUES(?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId, r.OperationId.LocalId)
}
func (s *Store) LoadReservation(ctx context.Context, r *v1.Ref) (*v1.Reservation, error) {
	v, e := s.LoadReservationVersion(ctx, r)
	if e != nil || v != nil {
		return v, e
	}
	return s.LoadCurrentReservation(ctx, r)
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

func (s *Store) GrantUseCount(ctx context.Context, pool string) (uint64, error) {
	var count uint64
	e := s.read(ctx, func(q querier) error {
		return q.QueryRowContext(ctx, "SELECT COUNT(*) FROM grant_uses WHERE user_id=? AND pool_id=?", s.user, pool).Scan(&count)
	})
	return count, e
}

func (s *Store) ReadAuthorityTime(ctx context.Context) (int64, error) {
	var now int64
	e := s.read(ctx, func(q querier) error {
		return q.QueryRowContext(ctx, "SELECT CAST(unixepoch('subsec')*1000 AS INTEGER)").Scan(&now)
	})
	return now, e
}
