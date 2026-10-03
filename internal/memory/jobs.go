package memory

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type Progress struct {
	Revision          uint64 `json:"revision"`
	WorkRevision      uint64 `json:"work_revision"`
	LastID            string `json:"last_id"`
	Unresolved        bool   `json:"unresolved"`
	IncompleteCleanup bool   `json:"incomplete_cleanup"`
}

func progress(ctx context.Context, tx runtime.Tx, namespace, key string, work runtime.Work) (Progress, error) {
	var p Progress
	_, err := tx.Get(ctx, namespace, key, &p)
	if api.IsCode(err, "not_found") {
		p = Progress{Revision: 1, WorkRevision: work.Claim.ObservedWorkRevision}
		err = tx.Create(ctx, namespace, key, "", p)
	}
	if err != nil {
		return p, err
	}
	if p.WorkRevision != work.Claim.ObservedWorkRevision {
		p.WorkRevision = work.Claim.ObservedWorkRevision
	}
	return p, nil
}
func saveProgress(ctx context.Context, tx runtime.Tx, namespace, key string, p Progress) error {
	old := p.Revision
	p.Revision++
	return tx.Put(ctx, namespace, key, old, p)
}

type ProjectionState struct {
	Revision            uint64           `json:"revision"`
	ContiguousWatermark uint64           `json:"contiguous_watermark"`
	IndexGeneration     uint64           `json:"index_generation"`
	StrategyRef         api.ComponentRef `json:"strategy_ref"`
	State               string           `json:"state"`
}
type Projection struct {
	SourceRef       api.ObjectRef    `json:"source_ref"`
	StrategyRef     api.ComponentRef `json:"strategy_ref"`
	IndexGeneration uint64           `json:"index_generation"`
	Purpose         string           `json:"purpose"`
	State           string           `json:"state"`
}

func (s *Service) indexJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		head, err := loadHead(ctx, tx)
		if err != nil {
			return err
		}
		var state ProjectionState
		_, err = tx.Get(ctx, "memory.index_heads", scope.OwnerID, &state)
		if api.IsCode(err, "not_found") {
			state = ProjectionState{Revision: 1, IndexGeneration: 1, StrategyRef: LexicalProfile(), State: "building"}
			if err = tx.Create(ctx, "memory.index_heads", scope.OwnerID, "", state); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		rows, err := tx.List(ctx, "memory.changes", "", fmt.Sprintf("%016d", state.ContiguousWatermark), 200)
		if err != nil {
			return err
		}
		for _, row := range rows {
			var change MemoryChange
			if err = row.Decode(&change); err != nil {
				return err
			}
			if change.ChangeSeq != state.ContiguousWatermark+1 {
				state.State = "invalid"
				break
			}
			var record MemoryRecord
			if err = tx.GetVersion(ctx, "memory.records", change.MemoryID, change.Revision, &record); err != nil {
				return err
			}
			projection := Projection{SourceRef: scope.Ref(change.MemoryID, change.Revision), StrategyRef: LexicalProfile(), IndexGeneration: state.IndexGeneration, Purpose: "memory.query", State: record.State}
			id := semanticID("projection", change.MemoryID+":"+fmt.Sprint(change.Revision)+":"+LexicalProfile().Digest)
			if err = tx.Create(ctx, "memory.projections", id, change.MemoryID, projection); err != nil {
				return err
			}
			state.ContiguousWatermark = change.ChangeSeq
		}
		if state.ContiguousWatermark == head.ChangeHead {
			state.State = "ready"
		}
		old := state.Revision
		state.Revision++
		if err = tx.Put(ctx, "memory.index_heads", scope.OwnerID, old, state); err != nil {
			return err
		}
		if state.ContiguousWatermark < head.ChangeHead && state.State != "invalid" {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			_, err = tx.Raise(ctx, "memory.index", work.Job.ResponsibilityKey, work.Job.SourceRef, now)
			return err
		}
		return nil
	})
}

func (s *Service) impactJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := loadHead(ctx, tx); err != nil {
			return err
		}
		key := work.Job.ResponsibilityKey
		p, err := progress(ctx, tx, "memory.impact_progress", work.Job.Kind+":"+key, work)
		if err != nil {
			return err
		}
		rows, err := tx.List(ctx, "memory.source_edges", key, p.LastID, 20)
		if err != nil {
			return err
		}
		for _, row := range rows {
			p.LastID = row.ID
			var edge MemorySourceEdge
			if err = row.Decode(&edge); err != nil {
				return err
			}
			var record MemoryRecord
			rev, err := tx.Get(ctx, "memory.records", edge.MemoryRef.ObjectID, &record)
			if err != nil {
				return err
			}
			if record.State == "deleted" || record.Revision != edge.MemoryRef.Revision {
				continue
			}
			if record.State == "active" {
				if work.Job.Kind != "memory.restrict_impact" {
					record.State = "needs_review"
				}
				record.ReviewReason = work.Job.Kind
				record.Revision = rev + 1
				if err = tx.Put(ctx, "memory.records", record.MemoryID, rev, record); err != nil {
					return err
				}
				head, err := loadHead(ctx, tx)
				if err != nil {
					return err
				}
				if _, err = appendChange(ctx, tx, head, record, "restrict", true); err != nil {
					return err
				}
				now, err := tx.Now(ctx)
				if err != nil {
					return err
				}
				if _, err = tx.Raise(ctx, "memory.cleanup", record.MemoryID, scope.Ref(record.MemoryID, record.Revision), now); err != nil {
					return err
				}
			}
		}
		if len(rows) < 20 {
			p.LastID = ""
		}
		if err = saveProgress(ctx, tx, "memory.impact_progress", work.Job.Kind+":"+key, p); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if len(rows) == 20 {
			_, err = tx.Raise(ctx, work.Job.Kind, key, work.Job.SourceRef, now)
			return err
		}
		// Content DAG 的有界独立责任确保未列入证据引用的 processed 来源也受影响。
		_, err = tx.Raise(ctx, "content.source_impact", work.Job.Kind+":"+key, work.Job.SourceRef, now)
		return err
	})
}

func (s *Service) contentImpactJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		head, err := loadHead(ctx, tx)
		if err != nil {
			return err
		}
		sourceKey := work.Job.SourceRef.ObjectID + ":" + fmt.Sprint(work.Job.SourceRef.Revision)
		p, err := progress(ctx, tx, "content.impact_progress", work.Job.ResponsibilityKey, work)
		if err != nil {
			return err
		}
		rows, err := tx.List(ctx, "content.source_edges", sourceKey, p.LastID, 20)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		closed := false
		var source ContentVersion
		_, sourceErr := tx.Get(ctx, "content.versions", sourceKey, &source)
		if sourceErr != nil {
			return sourceErr
		}
		closed = source.State != "published"
		for _, row := range rows {
			p.LastID = row.ID
			var edge SourceEdge
			if err = row.Decode(&edge); err != nil {
				return err
			}
			var derived ContentVersion
			rev, err := tx.Get(ctx, "content.versions", contentKey(edge.DerivedRef), &derived)
			if err != nil {
				return err
			}
			if closed && derived.State == "published" {
				derived.State = "closed"
				derived.ControlRevision = rev + 1
				if err = tx.Put(ctx, "content.versions", contentKey(edge.DerivedRef), rev, derived); err != nil {
					return err
				}
				head.VisibilityRevision++
				if _, err = tx.Raise(ctx, "content.cleanup", contentKey(edge.DerivedRef), scope.Ref(edge.DerivedRef.ContentID, edge.DerivedRef.Version), now); err != nil {
					return err
				}
			}
			kind := "memory.correction_impact"
			if strings.HasPrefix(work.Job.ResponsibilityKey, "memory.restrict_impact:") {
				kind = "memory.restrict_impact"
			}
			if closed {
				kind = "memory.source_impact"
			}
			if _, err = tx.Raise(ctx, kind, contentKey(edge.DerivedRef), scope.Ref(edge.DerivedRef.ContentID, edge.DerivedRef.Version), now); err != nil {
				return err
			}
		}
		if err = saveHead(ctx, tx, head); err != nil {
			return err
		}
		if len(rows) < 20 {
			p.LastID = ""
		}
		if err = saveProgress(ctx, tx, "content.impact_progress", work.Job.ResponsibilityKey, p); err != nil {
			return err
		}
		if len(rows) == 20 {
			_, err = tx.Raise(ctx, "content.source_impact", work.Job.ResponsibilityKey, work.Job.SourceRef, now)
			return err
		}
		return nil
	})
}

func (s *Service) memoryCleanupJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		if _, err := loadHead(ctx, tx); err != nil {
			return err
		}
		id := work.Job.SourceRef.ObjectID
		var record MemoryRecord
		rev, err := tx.Get(ctx, "memory.records", id, &record)
		if err != nil {
			return err
		}
		p, err := progress(ctx, tx, "memory.cleanup_progress", id, work)
		if err != nil {
			return err
		}
		rows, err := tx.List(ctx, "memory.holder_edges", id, p.LastID, 20)
		if err != nil {
			return err
		}
		for _, row := range rows {
			p.LastID = row.ID
			var edge MemorySourceEdge
			if err = row.Decode(&edge); err != nil {
				return err
			}
			if edge.MemoryRef.Revision == record.Revision && record.State == "active" {
				continue
			}
			var holder CopyHolder
			holderRev, err := tx.Get(ctx, "content.holders", edge.CopyID, &holder)
			if err != nil {
				return err
			}
			if holder.Kind != "metadata_reference" {
				return api.E("invalid_state", "unowned_copy_cleanup")
			}
			holder.UseState = "use_stopped"
			holder.CleanupState = "complete"
			holder.Revision = holderRev + 1
			if err = tx.Put(ctx, "content.holders", holder.CopyID, holderRev, holder); err != nil {
				return err
			}
			var content ContentVersion
			_, err = tx.Get(ctx, "content.versions", contentKey(holder.ContentRef), &content)
			if err != nil {
				return err
			}
			if content.State != "published" {
				now, err := tx.Now(ctx)
				if err != nil {
					return err
				}
				if _, err = tx.Raise(ctx, "content.cleanup", contentKey(holder.ContentRef), scope.Ref(holder.ContentRef.ContentID, holder.ContentRef.Version), now); err != nil {
					return err
				}
			}
		}
		if len(rows) < 20 {
			p.LastID = ""
		}
		if err = saveProgress(ctx, tx, "memory.cleanup_progress", id, p); err != nil {
			return err
		}
		if len(rows) == 20 {
			now, err := tx.Now(ctx)
			if err != nil {
				return err
			}
			_, err = tx.Raise(ctx, "memory.cleanup", id, scope.Ref(id, record.Revision), now)
			return err
		}
		if record.State == "deleted" && record.CleanupState != "complete" {
			record.CleanupState = "complete"
			record.Revision = rev + 1
			if err = tx.Put(ctx, "memory.records", id, rev, record); err != nil {
				return err
			}
			head, err := loadHead(ctx, tx)
			if err != nil {
				return err
			}
			_, err = appendChange(ctx, tx, head, record, "delete", false)
			return err
		}
		return nil
	})
}

func (s *Service) contentCleanupJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var content ContentVersion
	ready := false
	unresolved := false
	more := false
	var due time.Time
	err := s.within(ctx, scope, func(tx runtime.Tx) error {
		if err := tx.Guard(ctx, work.Claim); err != nil {
			return err
		}
		_, err := tx.Get(ctx, "content.versions", work.Job.ResponsibilityKey, &content)
		if err != nil {
			return err
		}
		if content.State == "published" {
			return api.E("invalid_state", "content_still_published")
		}
		p, err := progress(ctx, tx, "content.cleanup_progress", work.Job.ResponsibilityKey, work)
		if err != nil {
			return err
		}
		rows, err := tx.List(ctx, "content.holders", work.Job.ResponsibilityKey, p.LastID, 20)
		if err != nil {
			return err
		}
		for _, row := range rows {
			p.LastID = row.ID
			var holder CopyHolder
			if err = row.Decode(&holder); err != nil {
				return err
			}
			if content.ClosureKind == "retention" && holder.Kind == "metadata_reference" {
				holder.UseState = "use_stopped"
				holder.CleanupState = "complete"
				holder.Revision = row.Revision + 1
				if err = tx.Put(ctx, "content.holders", holder.CopyID, row.Revision, holder); err != nil {
					return err
				}
			}
			if holder.UseState != "use_stopped" {
				holder.UseState = "closing"
				holder.ControlRevision = content.ControlRevision
				holder.Revision = row.Revision + 1
				if err = tx.Put(ctx, "content.holders", holder.CopyID, row.Revision, holder); err != nil {
					return err
				}
				p.Unresolved = true
			}
			if holder.CleanupState != "complete" {
				p.IncompleteCleanup = true
			}
		}
		if len(rows) == 20 {
			more = true
			due, err = tx.Now(ctx)
			if err != nil {
				return err
			}
			if err = saveProgress(ctx, tx, "content.cleanup_progress", work.Job.ResponsibilityKey, p); err != nil {
				return err
			}
			return nil
		}
		ready = !p.Unresolved
		unresolved = p.IncompleteCleanup
		p.LastID = ""
		p.Unresolved = false
		p.IncompleteCleanup = false
		if err = saveProgress(ctx, tx, "content.cleanup_progress", work.Job.ResponsibilityKey, p); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}
	if more {
		return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Ready(due), nil)
	}
	if !ready {
		return finishWork(s, ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
			now, err := tx.Now(ctx)
			return runtime.Waiting(now.Add(time.Second)), err
		})
	}
	if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	if err = s.Objects.Delete(ctx, content.ObjectLocation); err != nil {
		return err
	}
	return finishWork(s, ctx, store, scope, work, func(tx runtime.Tx) (runtime.Disposition, error) {
		var current ContentVersion
		rev, err := tx.Get(ctx, "content.versions", work.Job.ResponsibilityKey, &current)
		if err != nil {
			return runtime.Disposition{}, err
		}
		if current.State == "published" {
			return runtime.Disposition{}, api.E("invalid_state", "content_reopened")
		}
		if current.State != "deleted" {
			current.State = "deleted"
			if err = tx.Put(ctx, "content.versions", work.Job.ResponsibilityKey, rev, current); err != nil {
				return runtime.Disposition{}, err
			}
		}
		if unresolved {
			now, err := tx.Now(ctx)
			if err != nil {
				return runtime.Disposition{}, err
			}
			return runtime.Waiting(now.Add(time.Second)), nil
		}
		return runtime.Done(), nil
	})
}

func (s *Service) transferCleanupJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var transfer Transfer
	done := false
	var waitUntil time.Time
	err := s.within(ctx, scope, func(tx runtime.Tx) error {
		if err := tx.Guard(ctx, work.Claim); err != nil {
			return err
		}
		rev, err := tx.Get(ctx, "content.transfers", work.Job.SourceRef.ObjectID, &transfer)
		if err != nil {
			return err
		}
		var content ContentVersion
		_, err = tx.Get(ctx, "content.versions", contentKey(transfer.ContentRef), &content)
		if err == nil && content.State != "prepared" || transfer.Phase == "published" {
			done = true
			return nil
		}
		if err != nil && !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		deadline, _ := api.ParseTime(transfer.ExpiresAt)
		if now.Before(deadline) {
			waitUntil = deadline
			return nil
		}
		if transfer.Phase != "cleanup" {
			transfer.Phase = "cleanup"
			transfer.Revision = rev + 1
			return tx.Put(ctx, "content.transfers", transfer.TransferID, rev, transfer)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if done {
		return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), nil)
	}
	if !waitUntil.IsZero() {
		return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Waiting(waitUntil), nil)
	}
	if err = store.CheckClaim(ctx, scope, work.Claim); err != nil {
		return err
	}
	loc := transfer.ObjectLocation
	if loc.Key == "" {
		loc, err = s.Objects.Locate(ctx, transfer.ContentRef)
		if api.IsCode(err, "gone") {
			return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), nil)
		}
		if err != nil {
			return err
		}
	}
	if err = s.Objects.Delete(ctx, loc); err != nil {
		return err
	}
	return runtime.Finish(ctx, store, scope, s.participants(), work, runtime.Done(), nil)
}

func finishWork(s *Service, ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work, fn func(runtime.Tx) (runtime.Disposition, error)) error {
	status, err := store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		if _, err := loadHead(ctx, tx); err != nil {
			return err
		}
		if err := tx.Guard(ctx, work.Claim); err != nil {
			return err
		}
		disposition, err := fn(tx)
		if err != nil {
			return err
		}
		return tx.Finish(ctx, work.Claim, disposition)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}

func (s *Service) registerJobs(registry *runtime.Registry) {
	registry.MustRegisterJob("memory.index", s.indexJob)
	registry.MustRegisterJob("memory.extract", s.extractionJob)
	registry.MustRegisterJob("memory.cleanup", s.memoryCleanupJob)
	registry.MustRegisterJob("memory.source_impact", s.impactJob)
	registry.MustRegisterJob("memory.correction_impact", s.impactJob)
	registry.MustRegisterJob("memory.restrict_impact", s.impactJob)
	registry.MustRegisterJob("content.source_impact", s.contentImpactJob)
	registry.MustRegisterJob("content.cleanup", s.contentCleanupJob)
	registry.MustRegisterJob("content.transfer_cleanup", s.transferCleanupJob)
	registry.MustRegisterJob("content.expire", s.expireJob)
}
