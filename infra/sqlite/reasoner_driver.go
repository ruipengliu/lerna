package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveReasonerDriver(ctx context.Context, d *v1.ReasonerDriver) error {
	n := d.TaskId
	if e := s.saveRecord(ctx, "adjudication", "INSERT INTO reasoner_drivers VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,task_id) DO UPDATE SET record=excluded.record", d, n.UserId, n.AuthorityDomainId, n.LocalId); e != nil {
		return e
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO reasoner_driver_versions VALUES(?,?,?,?,?)", d, d.Ref.Name.UserId, d.Ref.Name.AuthorityDomainId, d.Ref.Name.LocalId, d.Ref.Revision)
}
func (s *Store) LoadReasonerDriver(ctx context.Context, n *v1.GlobalName) (*v1.ReasonerDriver, error) {
	d := new(v1.ReasonerDriver)
	ok, e := s.load(ctx, d, "SELECT record FROM reasoner_drivers WHERE user_id=? AND domain_id=? AND task_id=?", n.UserId, n.AuthorityDomainId, n.LocalId)
	if !ok {
		return nil, e
	}
	return d, e
}
func (s *Store) LoadReasonerDriverVersion(ctx context.Context, r *v1.Ref) (*v1.ReasonerDriver, error) {
	d := new(v1.ReasonerDriver)
	n := r.Name
	ok, e := s.load(ctx, d, "SELECT record FROM reasoner_driver_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", n.UserId, n.AuthorityDomainId, n.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return d, e
}
func (s *Store) AllReasonerDrivers(ctx context.Context) ([]*v1.ReasonerDriver, error) {
	var result []*v1.ReasonerDriver
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM reasoner_drivers WHERE user_id=? AND domain_id=? ORDER BY task_id", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var body []byte
			if e = rows.Scan(&body); e != nil {
				return e
			}
			d := new(v1.ReasonerDriver)
			if e = proto.Unmarshal(body, d); e != nil {
				return e
			}
			result = append(result, d)
		}
		return rows.Err()
	})
	return result, e
}
