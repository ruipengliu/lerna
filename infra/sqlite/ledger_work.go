package sqlite

import (
	"context"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// LedgerWork 复用同一持久工作算法，但所有工作与提交时钟属于执行管理事务域。
type LedgerWork struct{ *Store }

func (s *Store) LedgerWork() *LedgerWork { return &LedgerWork{s} }
func (w *LedgerWork) Transaction(ctx context.Context, point string, fn func(context.Context) error) error {
	return w.ledgerWorkTransaction(ctx, point, fn)
}
func (w *LedgerWork) BusinessScope(ctx context.Context, fn func(context.Context) error) error {
	return w.businessScope(ctx, "ledger", fn)
}
func (w *LedgerWork) Position(ctx context.Context) (uint64, int64, error) {
	return w.LedgerPosition(ctx)
}
func (w *LedgerWork) SaveReceipt(ctx context.Context, r *v1.CommandReceipt) error {
	return w.SaveLedgerReceipt(ctx, r)
}
func (w *LedgerWork) SaveJob(ctx context.Context, j *v1.Job) error { return w.SaveLedgerJob(ctx, j) }
func (w *LedgerWork) LoadJob(ctx context.Context, n *v1.GlobalName) (*v1.Job, error) {
	j := new(v1.Job)
	ok, e := w.load(ctx, j, "SELECT record FROM ledger_jobs WHERE user_id=? AND domain_id=? AND id=?", n.UserId, n.AuthorityDomainId, n.LocalId)
	if !ok {
		return nil, e
	}
	return j, e
}
func (w *LedgerWork) allJobs(ctx context.Context, user string) ([]*v1.Job, error) {
	var jobs []*v1.Job
	e := w.read(ctx, func(q querier) error {
		rows, e := q.QueryContext(ctx, "SELECT record FROM ledger_jobs WHERE user_id=? AND domain_id=? ORDER BY id", user, w.domain+"/ledger")
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
func (w *LedgerWork) PendingJobs(ctx context.Context, user string) ([]*v1.Job, error) {
	all, e := w.allJobs(ctx, user)
	var jobs []*v1.Job
	for _, j := range all {
		if j.State == "READY" || j.State == "CLAIMED" || j.State == "WAITING" {
			jobs = append(jobs, j)
		}
	}
	return jobs, e
}
func (w *LedgerWork) FindJobPurpose(ctx context.Context, user, purpose string) (*v1.Job, error) {
	all, e := w.allJobs(ctx, user)
	for _, j := range all {
		if j.PurposeKey == purpose {
			return j, e
		}
	}
	return nil, e
}
