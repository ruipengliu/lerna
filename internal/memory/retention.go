package memory

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) expireJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return finishWork(s, ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
		head, err := loadHead(ctx, tx)
		if err != nil {
			return runtime.Disposition{}, err
		}
		var content ContentVersion
		rev, err := tx.Get(ctx, "content.versions", work.Job.ResponsibilityKey, &content)
		if err != nil {
			return runtime.Disposition{}, err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return runtime.Disposition{}, err
		}
		deadline, err := api.ParseTime(content.RetentionUntil)
		if err != nil {
			return runtime.Disposition{}, err
		}
		if now.Before(deadline) {
			return runtime.Waiting(deadline), nil
		}
		if content.State == "published" {
			content.State = "closed"
			content.ClosureKind = "retention"
			content.ControlRevision++
			if err = tx.Put(ctx, "content.versions", work.Job.ResponsibilityKey, rev, content); err != nil {
				return runtime.Disposition{}, err
			}
			head.VisibilityRevision++
			if err = saveHead(ctx, tx, head); err != nil {
				return runtime.Disposition{}, err
			}
		}
		if _, err = tx.Raise(ctx, "content.cleanup", work.Job.ResponsibilityKey, work.Job.SourceRef, now); err != nil {
			return runtime.Disposition{}, err
		}
		return runtime.Done(), nil
	})
}
