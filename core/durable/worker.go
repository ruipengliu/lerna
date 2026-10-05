package durable

import (
	"context"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// JobHandler 推进一项已领取的工作。处理函数自己调用 Advance 写入进展和工作去向；
// 外部调用只能在事务之外进行。
type JobHandler func(ctx context.Context, c *Claim) error

// HandleJob 登记本域的工作类型。工作类型来自核心固定的集合，不注册任意处理函数。
func (d *Domain) HandleJob(kind string, h JobHandler) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.jobs[kind] = h
}

// Worker 是一个进程实例在一个事务域上的工作者。进程重启后使用新的实例身份。
type Worker struct {
	d        *Domain
	instance string
	n        int
}

// NewWorker 创建工作者。
func NewWorker(d *Domain, instance string) *Worker { return &Worker{d: d, instance: instance} }

// Domain 返回工作者所在的事务域。
func (w *Worker) Domain() *Domain { return w.d }

// RunOne 领取并推进一项工作；没有可领取的工作时返回 false。
func (w *Worker) RunOne(ctx context.Context) (bool, error) {
	w.n++
	claimID := fmt.Sprintf("%s/%d", w.instance, w.n)
	claims, err := w.d.ClaimJobs(ctx, claimID, w.instance, 1)
	if err != nil {
		return false, err
	}
	if len(claims) == 0 {
		return false, nil
	}
	c := claims[0]
	w.d.mu.RLock()
	h := w.d.jobs[c.Kind]
	w.d.mu.RUnlock()
	if herr := h(ctx, &c); herr != nil {
		if errs.Is(herr, lernav1.ErrorCode_ERROR_CODE_STALE_CLAIM) { // 旧领取不得覆盖状态。
			return true, nil
		}
		wait := Backoff(c.JobID, c.ClaimCount-1, 100*time.Millisecond, time.Minute, time.Millisecond)
		aerr := w.d.Advance(ctx, &c, "durable:job_error", func(tx *Tx) (Transition, error) {
			return WaitUntil(tx.Now().Add(wait), herr.Error()), nil
		})
		if aerr != nil && !errs.IsPermanent(aerr) {
			return true, fmt.Errorf("job %s (%s): %v; recording error: %w", c.JobID, c.Kind, herr, aerr)
		}
	}
	return true, nil
}

// NextDue 返回最早可领取的等待中工作的时间；没有时返回零值。
func (d *Domain) NextDue(ctx context.Context) (time.Time, error) {
	var ms int64
	err := d.Read(ctx, func(tx *Tx) error {
		return tx.QueryRow(`SELECT COALESCE(MIN(CASE WHEN state = ? THEN lease_until ELSE not_before END), 0) FROM jobs
			WHERE (state IN (?, ?) AND not_before < ?) OR state = ?`,
			string(JobClaimed), string(JobReady), string(JobWaiting), never, string(JobClaimed)).Scan(&ms)
	})
	if err != nil || ms == 0 {
		return time.Time{}, err
	}
	return time.UnixMilli(ms), nil
}
