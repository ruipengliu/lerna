package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Store) SaveConfirmation(ctx context.Context, c *v1.Confirmation) error {
	n := c.Ref.Name
	if e := s.saveRecord(ctx, "adjudication", "INSERT INTO confirmations VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", c, n.UserId, n.AuthorityDomainId, n.LocalId); e != nil {
		return e
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO confirmation_versions VALUES(?,?,?,?,?)", c, n.UserId, n.AuthorityDomainId, n.LocalId, c.Ref.Revision)
}
func (s *Store) LoadConfirmation(ctx context.Context, r *v1.Ref) (*v1.Confirmation, error) {
	c := new(v1.Confirmation)
	n := r.Name
	ok, e := s.load(ctx, c, "SELECT record FROM confirmation_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", n.UserId, n.AuthorityDomainId, n.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return c, e
}
func (s *Store) LoadCurrentConfirmation(ctx context.Context, n *v1.GlobalName) (*v1.Confirmation, error) {
	c := new(v1.Confirmation)
	ok, e := s.load(ctx, c, "SELECT record FROM confirmations WHERE user_id=? AND domain_id=? AND id=?", n.UserId, n.AuthorityDomainId, n.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}
