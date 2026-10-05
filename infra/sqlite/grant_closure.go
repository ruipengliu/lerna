package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Store) SaveGrantExitClosure(ctx context.Context, c *v1.GrantExitClosure) error {
	return s.saveRecord(ctx, "ledger", "INSERT INTO grant_exit_closures VALUES(?,?,?,?)", c, c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId)
}
func (s *Store) LoadGrantExitClosure(ctx context.Context, r *v1.Ref) (*v1.GrantExitClosure, error) {
	c := new(v1.GrantExitClosure)
	ok, e := s.load(ctx, c, "SELECT record FROM grant_exit_closures WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}
