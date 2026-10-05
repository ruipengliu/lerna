package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Store) SaveExitCredential(ctx context.Context, c *v1.ExitCredential) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO exit_credentials VALUES(?,?,?,?)", c, c.Ref.Name.UserId, c.Ref.Name.AuthorityDomainId, c.Ref.Name.LocalId)
}
func (s *Store) LoadExitCredential(ctx context.Context, r *v1.Ref) (*v1.ExitCredential, error) {
	c := new(v1.ExitCredential)
	ok, e := s.load(ctx, c, "SELECT record FROM exit_credentials WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}

func (s *Store) SaveExitCredentialUse(ctx context.Context, u *v1.ExitCredentialUse) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO exit_credential_uses VALUES(?,?,?,?)", u, u.CredentialRef.Name.UserId, u.CredentialRef.Name.AuthorityDomainId, u.CredentialRef.Name.LocalId)
}
func (s *Store) LoadExitCredentialUse(ctx context.Context, r *v1.Ref) (*v1.ExitCredentialUse, error) {
	u := new(v1.ExitCredentialUse)
	ok, e := s.load(ctx, u, "SELECT record FROM exit_credential_uses WHERE user_id=? AND domain_id=? AND credential_id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return u, e
}
