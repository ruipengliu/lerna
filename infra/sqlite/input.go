package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Store) SaveTaskInputs(ctx context.Context, h *v1.TaskInputHistory) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO task_inputs VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", h, h.TaskId.UserId, h.TaskId.AuthorityDomainId, h.TaskId.LocalId)
}
func (s *Store) LoadTaskInputs(ctx context.Context, id *v1.GlobalName) (*v1.TaskInputHistory, error) {
	h := new(v1.TaskInputHistory)
	found, e := s.load(ctx, h, "SELECT record FROM task_inputs WHERE user_id=? AND domain_id=? AND id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !found {
		return &v1.TaskInputHistory{TaskId: id}, e
	}
	return h, e
}

func (s *Store) SaveQuestion(ctx context.Context, q *v1.Question) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO questions VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", q, q.Ref.Name.UserId, q.Ref.Name.AuthorityDomainId, q.Ref.Name.LocalId)
}
func (s *Store) LoadQuestion(ctx context.Context, r *v1.Ref) (*v1.Question, error) {
	q := new(v1.Question)
	found, e := s.load(ctx, q, "SELECT record FROM questions WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !found {
		return nil, e
	}
	return q, e
}

func (s *Store) SaveInputDelivery(ctx context.Context, d *v1.InputDelivery) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO input_deliveries VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", d, d.Ref.Name.UserId, d.Ref.Name.AuthorityDomainId, d.Ref.Name.LocalId)
}
func (s *Store) LoadInputDelivery(ctx context.Context, r *v1.Ref) (*v1.InputDelivery, error) {
	d := new(v1.InputDelivery)
	found, e := s.load(ctx, d, "SELECT record FROM input_deliveries WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !found {
		return nil, e
	}
	return d, e
}
