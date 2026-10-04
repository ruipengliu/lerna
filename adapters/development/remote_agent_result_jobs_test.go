package development

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 只记录实际已提交 Claim；不制造 Job，不从请求 Done 推测持久终态。
type configuredReportJobInventory struct {
	mu       sync.Mutex
	jobs     map[string]api.Job
	overflow bool
}

type configuredReportInventoryStore struct {
	runtime.Store
	inventory *configuredReportJobInventory
}

func (s *configuredReportInventoryStore) Claim(ctx context.Context, scope runtime.Scope, holder string, kinds []string, max int, lease time.Duration) ([]runtime.Work, runtime.CommitStatus, error) {
	works, status, err := s.Store.Claim(ctx, scope, holder, kinds, max, lease)
	if status == runtime.Committed && err == nil {
		s.inventory.mu.Lock()
		defer s.inventory.mu.Unlock()
		for _, work := range works {
			if !configuredReportRequiredJob(work.Job.Kind) {
				continue
			}
			if _, known := s.inventory.jobs[work.Job.JobID]; !known && len(s.inventory.jobs) == 1024 {
				s.inventory.overflow = true
				continue
			}
			s.inventory.jobs[work.Job.JobID] = work.Job
		}
	}
	return works, status, err
}

func configuredReportRequiredJob(kind string) bool {
	switch kind {
	case task.JobBilling, task.JobDelegation, task.JobAllocation, task.JobPublishResult, collaboration.JobRemoteProof, proofJob:
		return true
	default:
		return false
	}
}

// 验收专用只读观察口：准确 Scope 和实际 ClaimID 的当前数据库真值。
// SQLite mode=ro/query_only；PG事务 ReadOnly。没有迁移、Raise、Claim 或写入。
func configuredReportJobsDone(ctx context.Context, e *configuredAgentEndpoint, inventory *configuredReportJobInventory) (done bool, observed []api.Job, retErr error) {
	inventory.mu.Lock()
	if inventory.overflow {
		inventory.mu.Unlock()
		return false, nil, api.E("overloaded", "report_job_observation_limit")
	}
	known := make(map[string]api.Job, len(inventory.jobs))
	ids := make([]string, 0, len(inventory.jobs))
	for id, job := range inventory.jobs {
		known[id] = job
		ids = append(ids, id)
	}
	inventory.mu.Unlock()
	if len(ids) == 0 {
		return false, nil, api.E("invalid_state", "report_original_jobs_unobserved")
	}
	sort.Strings(ids)
	driver, dsn := "sqlite3", ""
	if e.config.Driver == "sqlite" {
		u := url.URL{Scheme: "file", Path: e.config.DatabasePath}
		q := u.Query()
		q.Set("mode", "ro")
		q.Set("_query_only", "1")
		q.Set("_busy_timeout", "5000")
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else if e.config.Driver == "postgres" {
		driver = "pgx"
		var err error
		dsn, err = DSN(e.config)
		if err != nil {
			return false, nil, err
		}
	} else {
		return false, nil, api.E("unsupported", "report_job_database_driver")
	}
	db, err := sql.Open(driver, dsn)
	if err != nil {
		return false, nil, err
	}
	db.SetMaxOpenConns(1)
	defer func() { retErr = errors.Join(retErr, db.Close()) }()
	tx, err := db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return false, nil, err
	}
	defer func() { retErr = errors.Join(retErr, tx.Rollback()) }()
	var actualDatabase string
	if err = tx.QueryRowContext(ctx, "SELECT database_id FROM harness_store_metadata WHERE singleton=1").Scan(&actualDatabase); err != nil {
		return false, nil, err
	}
	if actualDatabase != e.app.Scope.DatabaseID {
		return false, nil, api.E("forbidden", "report_original_database_changed")
	}
	args := []any{e.app.Scope.TenantID, e.app.Scope.OwnerID}
	bind := func(index int) string {
		if driver == "pgx" {
			return fmt.Sprintf("$%d", index)
		}
		return "?"
	}
	placeholders := make([]string, len(ids))
	for i, id := range ids {
		args = append(args, id)
		placeholders[i] = bind(i + 3)
	}
	query := "SELECT job_id,kind,responsibility_key,source_ref,state,work_revision,lease_epoch,holder_id FROM runtime_jobs WHERE tenant_id=" + bind(1) + " AND owner_id=" + bind(2) + " AND job_id IN (" + strings.Join(placeholders, ",") + ") ORDER BY job_id"
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return false, nil, err
	}
	defer func() { retErr = errors.Join(retErr, rows.Close()) }()
	done = true
	for rows.Next() {
		var current api.Job
		var source []byte
		if err = rows.Scan(&current.JobID, &current.Kind, &current.ResponsibilityKey, &source, &current.State, &current.WorkRevision, &current.LeaseEpoch, &current.HolderID); err != nil {
			return false, observed, err
		}
		if err = json.Unmarshal(source, &current.SourceRef); err != nil {
			return false, observed, err
		}
		current.TenantID, current.OwnerID = e.app.Scope.TenantID, e.app.Scope.OwnerID
		claim, ok := known[current.JobID]
		if !ok || current.Kind != claim.Kind || current.ResponsibilityKey != claim.ResponsibilityKey || current.SourceRef.TenantID != claim.SourceRef.TenantID || current.SourceRef.OwnerID != claim.SourceRef.OwnerID || current.SourceRef.ObjectID != claim.SourceRef.ObjectID || current.SourceRef.Revision < claim.SourceRef.Revision || current.WorkRevision < claim.WorkRevision || current.LeaseEpoch < claim.LeaseEpoch {
			return false, observed, api.E("idempotency_conflict", "report_original_job_identity_changed")
		}
		done = done && current.State == "done" && current.HolderID == ""
		observed = append(observed, current)
	}
	if err = rows.Err(); err != nil {
		return false, observed, err
	}
	if len(observed) != len(ids) {
		return false, observed, api.E("not_found", "report_original_job_missing")
	}
	return done, observed, nil
}

func configuredReportHasJob(jobs []api.Job, kind, key string) bool {
	for _, job := range jobs {
		if job.Kind == kind && job.ResponsibilityKey == key {
			return true
		}
	}
	return false
}

func configuredReportHasCorrection(jobs []api.Job, allocationID string) bool {
	for _, job := range jobs {
		if job.Kind == task.JobAllocation && strings.HasPrefix(job.ResponsibilityKey, "correction/") && job.SourceRef.ObjectID == allocationID {
			return true
		}
	}
	return false
}

func configuredReportProofsPublished(ctx context.Context, t *testing.T, a, b *configuredAgentEndpoint, refs []api.ContentRef) bool {
	t.Helper()
	if len(refs) == 0 || len(refs) > 64 {
		t.Fatal("original Closure proof set not bounded")
	}
	seen := make(map[api.ContentRef]bool, len(refs))
	for _, ref := range refs {
		if seen[ref] {
			continue
		}
		seen[ref] = true
		e := a
		if ref.OwnerID == b.app.Scope.OwnerID {
			e = b
		} else if ref.OwnerID != a.app.Scope.OwnerID {
			t.Fatal("original Closure proof owner changed")
		}
		body, err := e.app.Memory.ReadBytes(ctx, e.app.Scope, e.app.ServiceAuth, ref, "content.read", "cloud")
		if api.IsCode(err, "not_found") {
			return false
		}
		if err != nil || uint64(len(body)) != ref.ByteLength || api.Hash(body) != ref.Hash {
			t.Fatalf("original Closure proof publication/current permission differs: ref=%s %v", ref.ContentID, err)
		}
	}
	return true
}
