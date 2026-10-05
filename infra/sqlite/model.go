package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Store) SaveProposalRequest(ctx context.Context, r *v1.ProposalRequest) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO proposal_requests VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", r, r.Ref.Name.UserId, r.Ref.Name.AuthorityDomainId, r.Ref.Name.LocalId)
}
func (s *Store) LoadProposalRequest(ctx context.Context, r *v1.Ref) (*v1.ProposalRequest, error) {
	p := new(v1.ProposalRequest)
	ok, e := s.load(ctx, p, "SELECT record FROM proposal_requests WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return p, e
}

func (s *Store) SaveModelCall(ctx context.Context, c *v1.ModelCall) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO model_calls VALUES(?,?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", c, c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId, c.RequestRef.Name.LocalId, c.Position)
}
func (s *Store) LoadModelCall(ctx context.Context, r *v1.Ref, position uint32) (*v1.ModelCall, error) {
	c := new(v1.ModelCall)
	ok, e := s.load(ctx, c, "SELECT record FROM model_calls WHERE user_id=? AND domain_id=? AND request_id=? AND position=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId, position)
	if !ok {
		return nil, e
	}
	return c, e
}

func (s *Store) LoadModelCallRef(ctx context.Context, r *v1.Ref) (*v1.ModelCall, error) {
	c := new(v1.ModelCall)
	ok, e := s.load(ctx, c, "SELECT record FROM model_calls WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}

func (s *Store) SaveProposalOutcome(ctx context.Context, o *v1.ProposalOutcome) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO proposal_outcomes VALUES(?,?,?,?)", o, o.Ref.Name.UserId, o.Ref.Name.AuthorityDomainId, o.Ref.Name.LocalId)
}
func (s *Store) LoadProposalOutcome(ctx context.Context, r *v1.Ref) (*v1.ProposalOutcome, error) {
	o := new(v1.ProposalOutcome)
	ok, e := s.load(ctx, o, "SELECT record FROM proposal_outcomes WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return o, e
}
