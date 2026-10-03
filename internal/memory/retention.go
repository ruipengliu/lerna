package memory

import (
	"context"
	"time"

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
		notice, err := contentControlNotice(ctx, tx, content)
		if err != nil {
			return runtime.Disposition{}, err
		}
		if _, err = tx.Raise(ctx, "content.cleanup", work.Job.ResponsibilityKey, notice, now); err != nil {
			return runtime.Disposition{}, err
		}
		return runtime.Done(), nil
	})
}

type controlNotice struct {
	ContentRef      api.ContentRef `json:"content_ref"`
	ControlRevision uint64         `json:"control_revision"`
	ClosureKind     string         `json:"closure_kind"`
}

// contentControlNotice 指向已共同保存的控制变化；不能用相同准确版本冒充新 Job 触发。
func contentControlNotice(ctx context.Context, tx runtime.Tx, version ContentVersion) (api.ObjectRef, error) {
	value := controlNotice{version.ContentRef, version.ControlRevision, version.ClosureKind}
	digest, err := api.Digest(value)
	if err != nil {
		return api.ObjectRef{}, err
	}
	id := semanticID("cnotice", digest)
	var old controlNotice
	_, err = tx.Get(ctx, "content.control_notices", id, &old)
	if api.IsCode(err, "not_found") {
		err = tx.Create(ctx, "content.control_notices", id, contentKey(version.ContentRef), value)
	} else if err == nil && !api.Equal(value, old) {
		err = api.E("idempotency_conflict", "control_notice_changed")
	}
	return tx.Scope().Ref(id, 1), err
}

// copyExpireJob 只关闭该副本的使用门禁；持有者未报告停止前不伪报物理清理。
func (s *Service) copyExpireJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return finishWork(s, ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
		var holder CopyHolder
		rev, err := tx.Get(ctx, "content.holders", work.Job.SourceRef.ObjectID, &holder)
		if err != nil {
			return runtime.Disposition{}, err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return runtime.Disposition{}, err
		}
		deadline, err := api.ParseTime(holder.RetainUntil)
		if err != nil {
			return runtime.Disposition{}, err
		}
		if now.Before(deadline) {
			return runtime.Waiting(deadline), nil
		}
		if holder.Kind == "metadata_reference" {
			holder.UseState = "use_stopped"
			holder.CleanupState = "complete"
		} else if holder.UseState == "allowed" {
			holder.UseState = "closing"
		}
		holder.Revision = rev + 1
		if err = tx.Put(ctx, "content.holders", holder.CopyID, rev, holder); err != nil {
			return runtime.Disposition{}, err
		}
		if holder.UseState == "use_stopped" && holder.CleanupState == "complete" {
			return runtime.Done(), nil
		}
		return runtime.Waiting(now.Add(time.Second)), nil
	})
}
