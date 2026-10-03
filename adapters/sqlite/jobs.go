package sqlite

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/adapters/internal/durable"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const jobColumns = "job_id,kind,responsibility_key,source_ref,state,due_at,work_revision,lease_epoch,holder_id,lease_until,observed_work_revision"

type storedJob struct {
	job        api.Job
	due, until int64
	observed   uint64
}
type scanner interface{ Scan(...any) error }

func scanJob(row scanner, scope runtime.Scope) (storedJob, error) {
	var result storedJob
	var source []byte
	var revision, epoch, observed int64
	err := row.Scan(&result.job.JobID, &result.job.Kind, &result.job.ResponsibilityKey, &source, &result.job.State, &result.due, &revision, &epoch, &result.job.HolderID, &result.until, &observed)
	if err != nil {
		return result, recordError(err)
	}
	if err = json.Unmarshal(source, &result.job.SourceRef); err != nil {
		return result, err
	}
	result.job.TenantID = scope.TenantID
	result.job.OwnerID = scope.OwnerID
	result.job.WorkRevision = uint64(revision)
	result.job.LeaseEpoch = uint64(epoch)
	result.observed = uint64(observed)
	result.job.DueAt = api.Time(time.Unix(0, result.due))
	if result.until != 0 {
		result.job.LeaseUntil = api.Time(time.Unix(0, result.until))
	}
	return result, nil
}
func (tx *transaction) job(ctx context.Context, id string) (storedJob, error) {
	if err := tx.check(); err != nil {
		return storedJob{}, err
	}
	return scanJob(tx.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM runtime_jobs WHERE tenant_id=? AND owner_id=? AND job_id=?", tx.scope.TenantID, tx.scope.OwnerID, id), tx.scope)
}
func (tx *transaction) Raise(ctx context.Context, kind, key string, source api.ObjectRef, due time.Time) (api.Job, error) {
	if err := tx.namespace(kind); err != nil {
		return api.Job{}, err
	}
	if err := durable.Text(key); err != nil {
		return api.Job{}, err
	}
	if err := runtime.CheckRef(tx.scope, source); err != nil {
		return api.Job{}, err
	}
	if source.Revision > api.MaxSafeInteger {
		return api.Job{}, api.E("invalid_request", "invalid_revision")
	}
	at, err := durable.Time(due)
	if err != nil {
		return api.Job{}, err
	}
	old, err := scanJob(tx.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM runtime_jobs WHERE tenant_id=? AND owner_id=? AND kind=? AND responsibility_key=?", tx.scope.TenantID, tx.scope.OwnerID, kind, key), tx.scope)
	if err != nil && !errors.Is(err, runtime.ErrNotFound) {
		return api.Job{}, err
	}
	if err == nil {
		if old.job.SourceRef == source {
			return old.job, nil
		}
		if old.job.WorkRevision >= api.MaxSafeInteger {
			return api.Job{}, api.E("invalid_state", "work_revision_exhausted")
		}
		if old.job.SourceRef.OwnerID == source.OwnerID && old.job.SourceRef.ObjectID == source.ObjectID && source.Revision < old.job.SourceRef.Revision {
			return api.Job{}, api.E("revision_conflict", "job_source_revision_regressed")
		}
		if at < old.due {
			old.due = at
		}
		old.job.SourceRef = source
		old.job.WorkRevision++
		old.job.DueAt = api.Time(time.Unix(0, old.due))
		if old.job.State != "leased" {
			old.job.State = "ready"
			old.job.HolderID = ""
			old.job.LeaseUntil = ""
			old.until = 0
			old.observed = 0
		}
		_, err = tx.db.ExecContext(ctx, "UPDATE runtime_jobs SET source_ref=?,state=?,due_at=?,work_revision=?,holder_id=?,lease_until=?,observed_work_revision=? WHERE tenant_id=? AND owner_id=? AND job_id=?", api.Raw(source), old.job.State, old.due, int64(old.job.WorkRevision), old.job.HolderID, old.until, int64(old.observed), tx.scope.TenantID, tx.scope.OwnerID, old.job.JobID)
		return old.job, err
	}
	job := api.Job{JobID: api.NewID("job"), TenantID: tx.scope.TenantID, OwnerID: tx.scope.OwnerID, Kind: kind, ResponsibilityKey: key, SourceRef: source, State: "ready", DueAt: api.Time(due), WorkRevision: 1}
	_, err = tx.db.ExecContext(ctx, "INSERT INTO runtime_jobs(tenant_id,owner_id,job_id,kind,responsibility_key,source_ref,state,due_at,work_revision,lease_epoch) VALUES(?,?,?,?,?,?,'ready',?,1,0)", tx.scope.TenantID, tx.scope.OwnerID, job.JobID, kind, key, api.Raw(source), at)
	return job, recordError(err)
}
func (tx *transaction) Hint(ctx context.Context, id string, due time.Time) error {
	if err := durable.Identity(id); err != nil {
		return err
	}
	job, err := tx.job(ctx, id)
	if err != nil {
		return err
	}
	if err = tx.namespace(job.job.Kind); err != nil {
		return err
	}
	at, err := durable.Time(due)
	if err != nil {
		return err
	}
	if job.job.State == "done" || at >= job.due {
		return nil
	}
	_, err = tx.db.ExecContext(ctx, "UPDATE runtime_jobs SET due_at=? WHERE tenant_id=? AND owner_id=? AND job_id=?", at, tx.scope.TenantID, tx.scope.OwnerID, id)
	return err
}
func kinds(kinds []string) ([]string, error) {
	if len(kinds) == 0 || len(kinds) > 64 {
		return nil, api.E("invalid_request", "invalid_job_kinds")
	}
	roots := []string{}
	seen := map[string]bool{}
	for _, kind := range kinds {
		if err := durable.Namespace(kind); err != nil {
			return nil, err
		}
		root := strings.FieldsFunc(kind, func(r rune) bool { return r == '.' || r == '/' })[0]
		if !seen[root] {
			roots = append(roots, root)
			seen[root] = true
		}
	}
	return roots, nil
}
func (s *Store) Claim(ctx context.Context, scope runtime.Scope, holder string, allowed []string, limit int, lease time.Duration) ([]runtime.Work, runtime.CommitStatus, error) {
	if err := durable.Identity(holder); err != nil {
		return nil, runtime.RolledBack, err
	}
	if err := durable.Limits(limit); err != nil {
		return nil, runtime.RolledBack, err
	}
	if err := durable.Lease(lease); err != nil {
		return nil, runtime.RolledBack, err
	}
	roots, err := kinds(allowed)
	if err != nil {
		return nil, runtime.RolledBack, err
	}
	works := []runtime.Work{}
	status, err := s.Within(ctx, scope, roots, func(base runtime.Tx) error {
		tx := base.(*transaction)
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		rows, err := tx.db.QueryContext(ctx, "SELECT "+jobColumns+" FROM runtime_jobs WHERE tenant_id=? AND owner_id=? AND kind IN (SELECT value FROM json_each(?)) AND due_at<=? AND (state IN ('ready','waiting') OR (state='leased' AND lease_until<=?)) ORDER BY due_at,job_id LIMIT ?", scope.TenantID, scope.OwnerID, string(api.Raw(allowed)), now.UnixNano(), now.UnixNano(), limit)
		if err != nil {
			return err
		}
		pending := []storedJob{}
		for rows.Next() {
			job, err := scanJob(rows, scope)
			if err != nil {
				rows.Close()
				return err
			}
			pending = append(pending, job)
		}
		if err = rows.Err(); err != nil {
			rows.Close()
			return err
		}
		if err = rows.Close(); err != nil {
			return err
		}
		for _, stored := range pending {
			if stored.job.LeaseEpoch >= api.MaxSafeInteger {
				return api.E("invalid_state", "lease_epoch_exhausted")
			}
			until := now.Add(lease)
			stored.job.LeaseEpoch++
			stored.job.State = "leased"
			stored.job.HolderID = holder
			stored.job.LeaseUntil = api.Time(until)
			_, err = tx.db.ExecContext(ctx, "UPDATE runtime_jobs SET state='leased',holder_id=?,lease_epoch=?,lease_until=?,observed_work_revision=? WHERE tenant_id=? AND owner_id=? AND job_id=?", holder, int64(stored.job.LeaseEpoch), until.UnixNano(), int64(stored.job.WorkRevision), scope.TenantID, scope.OwnerID, stored.job.JobID)
			if err != nil {
				return err
			}
			claim := api.Claim{JobID: stored.job.JobID, TenantID: scope.TenantID, OwnerID: scope.OwnerID, HolderID: holder, LeaseEpoch: stored.job.LeaseEpoch, LeaseUntil: stored.job.LeaseUntil, ObservedWorkRevision: stored.job.WorkRevision}
			works = append(works, runtime.Work{Job: stored.job, Claim: claim})
		}
		return nil
	})
	if status == runtime.RolledBack {
		return nil, status, err
	}
	return works, status, err
}

func (tx *transaction) guarded(ctx context.Context, claim api.Claim) (storedJob, error) {
	return tx.checkGuard(ctx, claim, true)
}
func (tx *transaction) checkGuard(ctx context.Context, claim api.Claim, lock bool) (storedJob, error) {
	if err := durable.Claim(tx.scope, claim); err != nil {
		return storedJob{}, err
	}
	var job storedJob
	var err error
	if lock {
		job, err = tx.job(ctx, claim.JobID)
	} else {
		if err = tx.check(); err != nil {
			return storedJob{}, err
		}
		job, err = scanJob(tx.db.QueryRowContext(ctx, "SELECT "+jobColumns+" FROM runtime_jobs WHERE tenant_id=? AND owner_id=? AND job_id=?", tx.scope.TenantID, tx.scope.OwnerID, claim.JobID), tx.scope)
	}
	if errors.Is(err, runtime.ErrNotFound) {
		return job, runtime.ErrClaimLost
	}
	if err != nil {
		return job, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return job, err
	}
	confirmed, _ := api.ParseTime(claim.LeaseUntil)
	if job.job.State != "leased" || job.job.HolderID != claim.HolderID || job.job.LeaseEpoch != claim.LeaseEpoch || job.observed != claim.ObservedWorkRevision || !now.Before(confirmed) || now.UnixNano() >= job.until || confirmed.UnixNano() > job.until {
		return job, runtime.ErrClaimLost
	}
	if job.job.WorkRevision < claim.ObservedWorkRevision {
		return job, fmt.Errorf("job work revision regressed")
	}
	if tx.guards == nil {
		tx.guards = make(map[string]api.Claim)
	}
	tx.guards[claim.JobID] = claim
	return job, nil
}
func (tx *transaction) Guard(ctx context.Context, claim api.Claim) error {
	_, err := tx.checkGuard(ctx, claim, false)
	return err
}
func (tx *transaction) Finish(ctx context.Context, claim api.Claim, disposition runtime.Disposition) error {
	job, err := tx.guarded(ctx, claim)
	if err != nil {
		return err
	}
	if err = tx.namespace(job.job.Kind); err != nil {
		return err
	}
	state, due := disposition.State, job.due
	if state != "done" && state != "ready" && state != "waiting" {
		return api.E("invalid_request", "invalid_job_disposition")
	}
	if job.job.WorkRevision > claim.ObservedWorkRevision {
		state = "ready"
	} else if state != "done" {
		due, err = durable.Time(disposition.DueAt)
		if err != nil {
			return err
		}
	}
	_, err = tx.db.ExecContext(ctx, "UPDATE runtime_jobs SET state=?,due_at=?,holder_id='',lease_until=0,observed_work_revision=0 WHERE tenant_id=? AND owner_id=? AND job_id=?", state, due, tx.scope.TenantID, tx.scope.OwnerID, claim.JobID)
	if err == nil {
		if tx.finished == nil {
			tx.finished = make(map[string]bool)
		}
		tx.finished[claim.JobID] = true
	}
	return err
}
func (s *Store) Renew(ctx context.Context, scope runtime.Scope, claim api.Claim, lease time.Duration) (api.Claim, runtime.CommitStatus, error) {
	if err := durable.Lease(lease); err != nil {
		return api.Claim{}, runtime.RolledBack, err
	}
	var renewed api.Claim
	status, err := s.Within(ctx, scope, nil, func(base runtime.Tx) error {
		tx := base.(*transaction)
		job, err := tx.guarded(ctx, claim)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		until := now.Add(lease).UnixNano()
		if until < job.until {
			until = job.until
		}
		_, err = tx.db.ExecContext(ctx, "UPDATE runtime_jobs SET lease_until=? WHERE tenant_id=? AND owner_id=? AND job_id=?", until, scope.TenantID, scope.OwnerID, claim.JobID)
		if err != nil {
			return err
		}
		renewed = claim
		renewed.LeaseUntil = api.Time(time.Unix(0, until))
		return nil
	})
	if status == runtime.RolledBack {
		return api.Claim{}, status, err
	}
	return renewed, status, err
}
func (s *Store) CheckClaim(ctx context.Context, scope runtime.Scope, claim api.Claim) error {
	if err := durable.Scope(scope, s.ID()); err != nil {
		return err
	}
	if err := s.acquire(ctx); err != nil {
		return err
	}
	defer s.release()
	dbtx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer dbtx.Rollback()
	tx := &transaction{db: dbtx, scope: scope, active: true}
	defer func() { tx.active = false }()
	return tx.Guard(ctx, claim)
}
