package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) SaveVerification(ctx context.Context, v *v1.Verification) error {
	if e := s.saveRecord(ctx, "adjudication", "INSERT INTO verification_versions VALUES(?,?,?,?,?)", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId, v.Ref.Revision); e != nil {
		return e
	}
	return s.saveRecord(ctx, "adjudication", "INSERT INTO verifications VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId)
}
func (s *Store) LoadVerification(ctx context.Context, r *v1.Ref) (*v1.Verification, error) {
	v := new(v1.Verification)
	ok, e := s.load(ctx, v, "SELECT record FROM verification_versions WHERE user_id=? AND domain_id=? AND id=? AND revision=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId, r.Revision)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) AllVerifications(ctx context.Context) ([]*v1.Verification, error) {
	var result []*v1.Verification
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM verifications WHERE user_id=? AND domain_id=? ORDER BY id", s.user, s.domain)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			v := new(v1.Verification)
			if e = proto.Unmarshal(b, v); e != nil {
				return e
			}
			result = append(result, v)
		}
		return rows.Err()
	})
	return result, e
}
func (s *Store) SaveResult(ctx context.Context, r *v1.Result) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO results VALUES(?,?,?,?)", r, r.TaskId.UserId, r.TaskId.AuthorityDomainId, r.TaskId.LocalId)
}
func (s *Store) LoadResult(ctx context.Context, id *v1.GlobalName) (*v1.Result, error) {
	r := new(v1.Result)
	ok, e := s.load(ctx, r, "SELECT record FROM results WHERE user_id=? AND domain_id=? AND task_id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !ok {
		return nil, e
	}
	return r, e
}

func (s *Store) SaveCompletionIntent(ctx context.Context, v *v1.CompletionClosureIntent) error {
	return s.saveRecord(ctx, "adjudication", "INSERT INTO completion_intents VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId)
}
func (s *Store) LoadCompletionIntent(ctx context.Context, r *v1.Ref) (*v1.CompletionClosureIntent, error) {
	v := new(v1.CompletionClosureIntent)
	ok, e := s.load(ctx, v, "SELECT record FROM completion_intents WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) SaveCompletionSeal(ctx context.Context, v *v1.CompletionSeal) error {
	if e := s.saveRecord(ctx, "ledger", "INSERT INTO completion_seals VALUES(?,?,?,?)", v, v.Ref.Name.UserId, v.Ref.Name.AuthorityDomainId, v.Ref.Name.LocalId); e != nil {
		return e
	}
	return s.saveRecord(ctx, "ledger", "INSERT INTO completion_sealed_operations VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO NOTHING", v, v.OperationId.UserId, v.OperationId.AuthorityDomainId, v.OperationId.LocalId)
}
func (s *Store) LoadCompletionSeal(ctx context.Context, r *v1.Ref) (*v1.CompletionSeal, error) {
	v := new(v1.CompletionSeal)
	ok, e := s.load(ctx, v, "SELECT record FROM completion_seals WHERE user_id=? AND domain_id=? AND id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
func (s *Store) CompletionSealForOperation(ctx context.Context, id *v1.GlobalName) (*v1.CompletionSeal, error) {
	v := new(v1.CompletionSeal)
	ok, e := s.load(ctx, v, "SELECT record FROM completion_sealed_operations WHERE user_id=? AND domain_id=? AND id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !ok {
		return nil, e
	}
	return v, e
}
