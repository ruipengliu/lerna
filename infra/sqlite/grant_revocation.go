package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveGrantRevocation(ctx context.Context, c *v1.GrantRevocation) error {
	n := c.Ref.Name
	if e := s.saveRecord(ctx, "adjudication", "INSERT INTO grant_revocations VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", c, n.UserId, n.AuthorityDomainId, n.LocalId); e != nil {
		return e
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO grant_revocation_versions VALUES(?,?,?,?,?)", c, n.UserId, n.AuthorityDomainId, n.LocalId, c.Ref.Revision)
}
func (s *Store) LoadGrantRevocation(ctx context.Context, r *v1.Ref) (*v1.GrantRevocation, error) {
	c := new(v1.GrantRevocation)
	n := r.Name
	ok, e := s.load(ctx, c, "SELECT record FROM grant_revocation_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", n.UserId, n.AuthorityDomainId, n.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return c, e
}
func (s *Store) LoadCurrentGrantRevocation(ctx context.Context, n *v1.GlobalName) (*v1.GrantRevocation, error) {
	c := new(v1.GrantRevocation)
	ok, e := s.load(ctx, c, "SELECT record FROM grant_revocations WHERE user_id=? AND domain_id=? AND id=?", n.UserId, n.AuthorityDomainId, n.LocalId)
	if !ok {
		return nil, e
	}
	return c, e
}
func (s *Store) LoadGrantVersion(ctx context.Context, r *v1.Ref) (*v1.Grant, error) {
	c := new(v1.Grant)
	n := r.Name
	ok, e := s.load(ctx, c, "SELECT record FROM grant_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", n.UserId, n.AuthorityDomainId, n.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return c, e
}
func (s *Store) ExitCredentials(ctx context.Context) ([]*v1.ExitCredential, error) {
	var result []*v1.ExitCredential
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM exit_credentials WHERE user_id=? AND domain_id=?", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.ExitCredential)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			result = append(result, v)
		}
		return rows.Err()
	})
	return result, e
}

func (s *Store) PendingGrantRevocations(ctx context.Context) ([]*v1.GrantRevocation, error) {
	var result []*v1.GrantRevocation
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM grant_revocations WHERE user_id=? AND domain_id=?", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.GrantRevocation)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			if v.Status == "PENDING" {
				result = append(result, v)
			}
		}
		return rows.Err()
	})
	return result, e
}
