package governance

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/runtime"
)

// 分页工作本轮保存进度后明确提交下一 disposition。Runtime Hint 只是
// 提前唤醒，不能把正在领取的分页 Job 的 done 变成新的责任。
type workTx struct {
	runtime.Tx
	jobID       string
	disposition runtime.Disposition
}

func (tx *workTx) Hint(ctx context.Context, id string, at time.Time) error {
	if id != tx.jobID {
		return tx.Tx.Hint(ctx, id, at)
	}
	now, err := tx.Tx.Now(ctx)
	if err != nil {
		return err
	}
	tx.disposition = runtime.Ready(at)
	if at.After(now) {
		tx.disposition = runtime.Waiting(at)
	}
	return nil
}
func finish(ctx context.Context, store runtime.Store, scope runtime.Scope, participants []string, work runtime.Work, disposition runtime.Disposition, fn func(runtime.Tx) error) error {
	status, err := store.Within(ctx, scope, participants, func(tx runtime.Tx) error {
		wrapped := &workTx{Tx: tx, jobID: work.Job.JobID, disposition: disposition}
		if fn != nil {
			if err := fn(wrapped); err != nil {
				return err
			}
		}
		if err := tx.Guard(ctx, work.Claim); err != nil {
			return err
		}
		return tx.Finish(ctx, work.Claim, wrapped.disposition)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
