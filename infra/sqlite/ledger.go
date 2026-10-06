package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

func (s *Store) LedgerTransaction(ctx context.Context, fn func(context.Context) error) error {
	return s.transact(ctx, "ledger", "ledger.accept", fn)
}
func (s *Store) LedgerPosition(ctx context.Context) (uint64, int64, error) {
	tx, e := s.writer(ctx, "ledger")
	if e != nil {
		return 0, 0, e
	}
	var pos uint64
	var now int64
	e = tx.QueryRowContext(ctx, "UPDATE ledger_commit_clock SET position=position+1 WHERE singleton=1 RETURNING position, CAST(unixepoch('subsec')*1000 AS INTEGER)").Scan(&pos, &now)
	return pos, now, storageError(e, false)
}
func (s *Store) SaveLedgerReceipt(ctx context.Context, r *v1.CommandReceipt) error {
	return s.saveRecord(ctx, "ledger", "INSERT INTO command_receipts VALUES(?,?,?,?,?)", r, r.Identity.UserId, r.Identity.IssuerId, r.Identity.TargetDomainId, r.Identity.CommandId)
}
func (s *Store) SaveOperation(ctx context.Context, o *v1.Operation) error {
	old, e := s.LoadOperation(ctx, o.Ref.Name)
	if e != nil {
		return e
	}
	if old != nil {
		if e = s.saveExecutionVersions(ctx, old); e != nil {
			return e
		}
	}
	if e = s.saveExecutionVersions(ctx, o); e != nil {
		return e
	}
	return s.saveRecord(ctx, "ledger", "INSERT INTO operations VALUES(?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", o, o.Ref.Name.UserId, o.Ref.Name.AuthorityDomainId, o.Ref.Name.LocalId)
}
func (s *Store) LoadOperation(ctx context.Context, id *v1.GlobalName) (*v1.Operation, error) {
	o := new(v1.Operation)
	ok, e := s.load(ctx, o, "SELECT record FROM operations WHERE user_id=? AND domain_id=? AND id=?", id.UserId, id.AuthorityDomainId, id.LocalId)
	if !ok {
		return nil, e
	}
	return o, e
}
func (s *Store) SaveLedgerJob(ctx context.Context, j *v1.Job) error {
	if j.JobType == "RECONCILE_UNRESOLVED_OPERATION" {
		return s.saveRecord(ctx, "ledger", "INSERT INTO execution_followup_jobs VALUES(?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", j, j.Ref.Name.UserId, j.Ref.Name.AuthorityDomainId, j.Ref.Name.LocalId, j.SpecificationRef.Name.LocalId)
	}
	if j.JobType == "RECONCILE_OPERATION" {
		return s.saveRecord(ctx, "ledger", "INSERT INTO reconciliation_jobs VALUES(?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", j, j.Ref.Name.UserId, j.Ref.Name.AuthorityDomainId, j.Ref.Name.LocalId, j.SpecificationRef.Name.LocalId)
	}
	return s.saveRecord(ctx, "ledger", "INSERT INTO ledger_jobs VALUES(?,?,?,?,?) ON CONFLICT(user_id,domain_id,id) DO UPDATE SET record=excluded.record", j, j.Ref.Name.UserId, j.Ref.Name.AuthorityDomainId, j.Ref.Name.LocalId, j.SpecificationRef.Name.LocalId)
}
func (s *Store) LedgerJobs(ctx context.Context, id *v1.GlobalName) ([]*v1.Job, error) {
	var jobs []*v1.Job
	e := s.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM ledger_jobs WHERE user_id=? AND domain_id=? AND operation_id=? UNION ALL SELECT record FROM reconciliation_jobs WHERE user_id=? AND domain_id=? AND operation_id=? UNION ALL SELECT record FROM execution_followup_jobs WHERE user_id=? AND domain_id=? AND operation_id=?", id.UserId, id.AuthorityDomainId, id.LocalId, id.UserId, id.AuthorityDomainId, id.LocalId, id.UserId, id.AuthorityDomainId, id.LocalId)
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var b []byte
			if e = rows.Scan(&b); e != nil {
				return e
			}
			j := new(v1.Job)
			if e = proto.Unmarshal(b, j); e != nil {
				return e
			}
			jobs = append(jobs, j)
		}
		return rows.Err()
	})
	return jobs, e
}
