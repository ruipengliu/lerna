package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

func (s *Store) SaveReconciliation(ctx context.Context, p *v1.Reconciliation) error {
	if e := s.saveExecutionVersion(ctx, p.Ref, p); e != nil {
		return e
	}
	return s.saveRecord(ctx, "ledger", "INSERT INTO reconciliations VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,operation_id) DO UPDATE SET record=excluded.record", p, p.OperationId.UserId, p.OperationId.AuthorityDomainId, p.OperationId.LocalId)
}
func (s *Store) LoadReconciliation(ctx context.Context, id *v1.GlobalName) (*v1.Reconciliation, error) {
	p := new(v1.Reconciliation)
	ok, e := s.load(ctx, p, "SELECT record FROM reconciliations WHERE user_id=? AND domain_id=? AND operation_id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !ok {
		return nil, e
	}
	return p, e
}
func (s *Store) SaveReconciliationQuery(ctx context.Context, q *v1.ReconciliationQuery) error {
	if e := s.saveExecutionVersion(ctx, q.Ref, q); e != nil {
		return e
	}
	return s.saveRecord(ctx, "ledger", "INSERT INTO reconciliation_queries VALUES(?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", q, q.Ref.Name.UserId, q.Ref.Name.AuthorityDomainId, q.Ref.Name.LocalId, q.Work.Ref.Name.LocalId)
}
func (s *Store) LoadReconciliationQuery(ctx context.Context, r *v1.Ref) (*v1.ReconciliationQuery, error) {
	q := new(v1.ReconciliationQuery)
	ok, e := s.loadExecutionVersion(ctx, r, q)
	if !ok {
		return nil, e
	}
	return q, e
}
func (s *Store) LoadClosureQuery(ctx context.Context, r *v1.Ref) (*v1.ReconciliationQuery, error) {
	q := new(v1.ReconciliationQuery)
	ok, e := s.load(ctx, q, "SELECT record FROM reconciliation_queries WHERE user_id=? AND domain_id=? AND work_id=?", r.Name.UserId, r.Name.AuthorityDomainId, r.Name.LocalId)
	if !ok {
		return nil, e
	}
	return q, e
}
func (s *Store) SaveReconciliationFinding(ctx context.Context, f *v1.ReconciliationFinding) error {
	return s.saveExecutionVersion(ctx, f.Ref, f)
}
func (s *Store) LoadReconciliationFinding(ctx context.Context, r *v1.Ref) (*v1.ReconciliationFinding, error) {
	f := new(v1.ReconciliationFinding)
	ok, e := s.loadExecutionVersion(ctx, r, f)
	if !ok {
		return nil, e
	}
	return f, e
}
