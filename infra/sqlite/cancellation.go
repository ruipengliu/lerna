package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveCancellation(ctx context.Context, c *v1.Cancellation) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO cancellations VALUES(?,?,?,?)", c, c.TaskId.UserId, c.TaskId.AuthorityDomainId, c.TaskId.LocalId)
}
func (s *Store) LoadCancellation(ctx context.Context, id *v1.GlobalName) (*v1.Cancellation, error) {
	c := new(v1.Cancellation)
	ok, e := s.load(ctx, c, "SELECT record FROM cancellations WHERE user_id=? AND domain_id=? AND task_id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}
func (s *Store) AllCancellations(ctx context.Context) ([]*v1.Cancellation, error) {
	var result []*v1.Cancellation
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM cancellations WHERE user_id=? AND domain_id=? ORDER BY task_id", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			c := new(v1.Cancellation)
			if e = proto.Unmarshal(b, c); e != nil {
				return e
			}
			result = append(result, c)
		}
		return rows.Err()
	})
	return result, e
}
func (s *Store) SaveCancellationIntent(ctx context.Context, c *v1.CancellationClosureIntent) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO cancellation_intents VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", c, c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId)
}
func (s *Store) LoadCancellationIntent(ctx context.Context, r *v1.Ref) (*v1.CancellationClosureIntent, error) {
	c := new(v1.CancellationClosureIntent)
	ok, e := s.load(ctx, c, "SELECT record FROM cancellation_intents WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}

func (s *Store) SaveCancellationSeal(ctx context.Context, v *v1.CancellationSeal) error {
	if e := s.saveRecord(ctx, "ledger", "INSERT INTO cancellation_seals VALUES(?,?,?,?)", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId); e != nil {
		return e
	}
	return s.saveRecord(ctx, "ledger", "INSERT INTO cancellation_sealed_operations VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO NOTHING", v, v.OperationId.UserId, v.OperationId.AuthorityDomainId, v.OperationId.LocalId)
}
func (s *Store) LoadCancellationSeal(ctx context.Context, r *v1.Ref) (*v1.CancellationSeal, error) {
	v := new(v1.CancellationSeal)
	ok, e := s.load(ctx, v, "SELECT record FROM cancellation_seals WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) CancellationSealForOperation(ctx context.Context, id *v1.GlobalName) (*v1.CancellationSeal, error) {
	v := new(v1.CancellationSeal)
	ok, e := s.load(ctx, v, "SELECT record FROM cancellation_sealed_operations WHERE user_id=? AND domain_id=? AND id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
