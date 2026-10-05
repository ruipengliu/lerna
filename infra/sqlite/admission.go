package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) BusinessScope(ctx context.Context, fn func(context.Context) error) error {
	return s.businessScope(ctx, "adjudication", fn)
}
func (s *Store) businessScope(ctx context.Context, domain string, fn func(context.Context) error) error {
	tx, err := s.writer(ctx, domain)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, "SAVEPOINT business"); err != nil {
		return err
	}
	if err = fn(ctx); err != nil {
		if _, rollbackErr := tx.ExecContext(ctx, "ROLLBACK TO business"); rollbackErr != nil {
			return rollbackErr
		}
		if _, releaseErr := tx.ExecContext(ctx, "RELEASE business"); releaseErr != nil {
			return releaseErr
		}
		return err
	}
	_, err = tx.ExecContext(ctx, "RELEASE business")
	return err
}
func (s *Store) SavePlanning(ctx context.Context, p *v1.PlanningState) error {
	tx, err := s.writer(ctx, "adjudication")
	if err != nil {
		return err
	}
	b, err := proto.Marshal(p)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, "INSERT INTO planning VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", p.TaskId.UserId, p.TaskId.AuthorityDomainId, p.TaskId.LocalId, b)
	return storageError(err, false)
}
func (s *Store) LoadPlanning(ctx context.Context, id *v1.GlobalName) (*v1.PlanningState, error) {
	p := new(v1.PlanningState)
	found, err := s.load(ctx, p, "SELECT record FROM planning WHERE user_id=? AND domain_id=? AND id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !found {
		return &v1.PlanningState{TaskId: id}, err
	}
	return p, err
}
func (s *Store) SaveSnapshot(ctx context.Context, p *v1.ContextSnapshot) error {
	tx, e := s.writer(ctx, "adjudication")
	if e != nil {
		return e
	}
	b, e := proto.Marshal(p)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, "INSERT INTO snapshots VALUES(?,?,?,?)", p.Ref.Name.UserId, p.Ref.Name.AuthorityDomainId, p.Ref.Name.LocalId, b)
	return storageError(e, false)
}
func (s *Store) LoadSnapshot(ctx context.Context, r *v1.Ref) (*v1.ContextSnapshot, error) {
	p := new(v1.ContextSnapshot)
	found, e := s.load(ctx, p, "SELECT record FROM snapshots WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !found {
		return nil, e
	}
	return p, e
}
func (s *Store) SaveCapability(ctx context.Context, c *v1.Capability) error {
	if e := s.saveRecord(ctx, "adjudication", "INSERT INTO capability_versions VALUES(?,?,?,?,?)", c, c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId, c.Ref.Revision); e != nil {
		return e
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO capabilities VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", c, c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId)
}
func (s *Store) LoadCapability(ctx context.Context, r *v1.Ref) (*v1.Capability, error) {
	c := new(v1.Capability)
	ok, e := s.load(ctx, c, "SELECT record FROM capabilities WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}
func (s *Store) CapabilityRefs(ctx context.Context) ([]*v1.Ref, error) {
	var refs []*v1.Ref
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM capabilities WHERE user_id=? AND domain_id=? ORDER BY id", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			c := new(v1.Capability)
			if e = proto.Unmarshal(b, c); e != nil {
				return e
			}
			refs = append(refs, c.Ref)
		}
		return rows.Err()
	})
	return refs, e
}
func (s *Store) SaveAdmission(ctx context.Context, a *v1.Admission) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO admissions VALUES(?,?,?,?)", a, a.Ref.Name.UserId, a.Ref.Name.AuthorityDomainId, a.Ref.Name.LocalId)
}
func (s *Store) LoadAdmission(ctx context.Context, r *v1.Ref) (*v1.Admission, error) {
	a := new(v1.Admission)
	ok, e := s.load(ctx, a, "SELECT record FROM admissions WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return a, e
}
func (s *Store) SaveHandoff(ctx context.Context, h *v1.Handoff) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO handoffs VALUES(?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", h, h.Ref.Name.UserId, h.Ref.Name.AuthorityDomainId, h.Ref.Name.LocalId, h.AdmissionRef.Name.LocalId)
}
func (s *Store) LoadHandoff(ctx context.Context, r *v1.Ref) (*v1.Handoff, error) {
	h := new(v1.Handoff)
	ok, e := s.load(ctx, h, "SELECT record FROM handoffs WHERE user_id=? AND domain_id=? AND admission_id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return h, e
}
func (s *Store) SaveRequirements(ctx context.Context, r *v1.Requirements) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO requirements VALUES(?,?,?,?)", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId)
}
func (s *Store) LoadRequirements(ctx context.Context, r *v1.Ref) (*v1.Requirements, error) {
	v := new(v1.Requirements)
	ok, e := s.load(ctx, v, "SELECT record FROM requirements WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) SaveProposal(ctx context.Context, p *v1.Proposal) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO proposals VALUES(?,?,?,?)", p, p.Ref.Name.UserId, p.Ref.Name.AuthorityDomainId, p.Ref.Name.LocalId)
}
func (s *Store) LoadProposal(ctx context.Context, r *v1.Ref) (*v1.Proposal, error) {
	p := new(v1.Proposal)
	ok, e := s.load(ctx, p, "SELECT record FROM proposals WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return p, e
}

func (s *Store) LoadCapabilityVersion(ctx context.Context, r *v1.Ref) (*v1.Capability, error) {
	c := new(v1.Capability)
	ok, e := s.load(ctx, c, "SELECT record FROM capability_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return c, e
}
