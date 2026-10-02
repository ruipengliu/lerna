package taskstore

import (
	"context"

	"github.com/ruipengliu/lerna/internal/durable"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
)

// Scheduler only selects locked candidates. The common engine owns lease
// epochs, database time, unknown-commit confirmation and guarded completion.
// Capacity selection is serialized across kinds; fairness counters remain per
// kind. The lock ends with the claim transaction, never with the handler.
func Scheduler(q Queries, providerLimit, resourceLimit int) sqlstore.CandidateSelector {
	return func(ctx context.Context, db sqlstore.DBTX, s durable.Scope, kind string, limit int) ([]durable.Job, error) {
		if limit != 1 || providerLimit < 1 || resourceLimit < 1 {
			return nil, durable.ErrPrecondition
		}
		if _, e := db.ExecContext(ctx, q.ScheduleCreate, s.TenantID, s.OwnerID, "capacity"); e != nil {
			return nil, e
		}
		var capacityTurn int64
		if e := db.QueryRowContext(ctx, q.ScheduleLock, s.TenantID, s.OwnerID, "capacity").Scan(&capacityTurn); e != nil {
			return nil, e
		}
		if _, e := db.ExecContext(ctx, q.ScheduleCreate, s.TenantID, s.OwnerID, kind); e != nil {
			return nil, e
		}
		var turn int64
		if e := db.QueryRowContext(ctx, q.ScheduleLock, s.TenantID, s.OwnerID, kind).Scan(&turn); e != nil {
			return nil, e
		}
		rows, e := db.QueryContext(ctx, q.FairCandidates, s.TenantID, s.OwnerID, kind, limit, providerLimit, resourceLimit)
		if e != nil {
			return nil, e
		}
		jobs := []durable.Job{}
		subjects := []string{}
		tasks := []string{}
		for rows.Next() {
			var j durable.Job
			var subject, task string
			e = rows.Scan(&j.Scope.TenantID, &j.Scope.OwnerID, &j.Key.Kind, &j.Key.Responsibility, &j.ID, &j.SourceRef, &j.State, &j.DueAt, &j.WorkRevision, &j.LeaseEpoch, &j.LeaseUntil, &j.HolderID, &j.WaitReason, &subject, &task)
			if e != nil {
				break
			}
			jobs = append(jobs, j)
			subjects = append(subjects, subject)
			tasks = append(tasks, task)
		}
		rowErr := rows.Err()
		_ = rows.Close()
		if e != nil {
			return nil, e
		}
		if rowErr != nil {
			return nil, rowErr
		}
		if len(jobs) == 0 {
			return jobs, nil
		}
		if _, e = db.ExecContext(ctx, q.ScheduleAdvance, s.TenantID, s.OwnerID, kind); e != nil {
			return nil, e
		}
		for i := range jobs {
			if _, e = db.ExecContext(ctx, q.ScheduleTurns, s.TenantID, s.OwnerID, kind, subjects[i], tasks[i], turn+1); e != nil {
				return nil, e
			}
		}
		return jobs, nil
	}
}
