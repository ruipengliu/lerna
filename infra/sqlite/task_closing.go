package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveTaskClosing(ctx context.Context, v *v1.TaskClosing) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO task_closings VALUES(?,?,?,?,?)", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId, v.TaskId.LocalId)
}
func (s *Store) LoadTaskClosing(ctx context.Context, r *v1.Ref) (*v1.TaskClosing, error) {
	v := new(v1.TaskClosing)
	ok, e := s.load(ctx, v, "SELECT record FROM task_closings WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) TaskClosingForTask(ctx context.Context, n *v1.GlobalName) (*v1.TaskClosing, error) {
	v := new(v1.TaskClosing)
	ok, e := s.load(ctx, v, "SELECT record FROM task_closings WHERE user_id=? AND domain_id=? AND task_id=?", n.UserId, n.AuthorityDomainId, n.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) AllTaskClosings(ctx context.Context) ([]*v1.TaskClosing, error) {
	var all []*v1.TaskClosing
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM task_closings WHERE user_id=? AND domain_id=? ORDER BY id", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.TaskClosing)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			all = append(all, v)
		}
		return rows.Err()
	})
	return all, e
}

func (s *Store) SaveTaskClosureIntent(ctx context.Context, v *v1.TaskClosureIntent) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO task_closure_intents VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId)
}
func (s *Store) LoadTaskClosureIntent(ctx context.Context, r *v1.Ref) (*v1.TaskClosureIntent, error) {
	v := new(v1.TaskClosureIntent)
	ok, e := s.load(ctx, v, "SELECT record FROM task_closure_intents WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) SaveTaskClosureSeal(ctx context.Context, v *v1.TaskClosureSeal) error {
	if e := s.saveRecord(ctx, "ledger", "INSERT INTO task_closure_seals VALUES(?,?,?,?)", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId); e != nil {
		return e
	}
	return s.saveRecord(ctx, "ledger", "INSERT INTO task_sealed_operations VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO NOTHING", v, v.OperationId.UserId, v.OperationId.AuthorityDomainId, v.OperationId.LocalId)
}
func (s *Store) LoadTaskClosureSeal(ctx context.Context, r *v1.Ref) (*v1.TaskClosureSeal, error) {
	v := new(v1.TaskClosureSeal)
	ok, e := s.load(ctx, v, "SELECT record FROM task_closure_seals WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) TaskClosureSealForOperation(ctx context.Context, id *v1.GlobalName) (*v1.TaskClosureSeal, error) {
	v := new(v1.TaskClosureSeal)
	ok, e := s.load(ctx, v, "SELECT record FROM task_sealed_operations WHERE user_id=? AND domain_id=? AND id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
