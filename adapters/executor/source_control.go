package executor

import (
	"context"
	"encoding/base64"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

const sourceCloseJob = "executor.source.close"
const sourceCopyExpireJob = "executor.copy.expire"

type sourceClosureCursor struct {
	After string `json:"after"`
}

// ClosePublishedContent 是设备源 owner 的管理端口；有限传输peer没有此管理权。
// 关闭先与原holder影响Job同Tx保存；它没有声称已删除任何云端副本或本机证据。
func (h *Host) ClosePublishedContent(ctx context.Context, ref api.ContentRef, reason string) error {
	if reason == "" || len(reason) > 512 {
		return api.E("invalid_request", "source_closure_reason_required")
	}
	status, err := h.Store.Within(ctx, h.Scope, []string{Namespace}, func(tx runtime.Tx) error {
		rec, err := h.sourceContentTx(ctx, tx, ref)
		if err != nil {
			return err
		}
		if rec.SourceState != "published" {
			return nil
		}
		rev, err := tx.Get(ctx, Namespace+".contents", contentKey(ref), &rec)
		if err != nil {
			return err
		}
		rec.SourceState = "closing"
		rec.ClosureKind = reason
		rec.ControlRevision++
		rec.Revision = rev + 1
		if err = tx.Put(ctx, Namespace+".contents", contentKey(ref), rev, rec); err != nil {
			return err
		}
		if err = tx.Create(ctx, Namespace+".source_closures", contentKey(ref), ref.ContentID, sourceClosureCursor{}); err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		_, err = tx.Raise(ctx, sourceCloseJob, contentKey(ref), tx.Scope().Ref(ref.ContentID, rec.ControlRevision), now)
		return err
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
func (h *Host) closeSourceCopies(ctx context.Context, st runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var cursor sourceClosureCursor
	key := work.Job.ResponsibilityKey
	if _, err := st.Read(ctx, scope, Namespace+".source_closures", key, 0, &cursor); err != nil {
		return err
	}
	rows, err := st.List(ctx, scope, Namespace+".source_copies", key, cursor.After, 100)
	if err != nil {
		return err
	}
	disposition := runtime.Done()
	if len(rows) == 100 {
		disposition = runtime.Ready(time.Now())
	}
	return runtime.Finish(ctx, st, scope, []string{Namespace}, work, disposition, func(tx runtime.Tx) error {
		var current sourceClosureCursor
		rev, err := tx.Get(ctx, Namespace+".source_closures", key, &current)
		if err != nil {
			return err
		}
		if current.After != cursor.After {
			return runtime.ErrConflict
		}
		for _, row := range rows {
			var copy memory.CopyHolder
			copyRev, err := tx.Get(ctx, Namespace+".source_copies", row.ID, &copy)
			if err != nil {
				return err
			}
			if copy.UseState == "allowed" {
				copy.UseState = "closing"
				copy.Revision = copyRev + 1
				if err = tx.Put(ctx, Namespace+".source_copies", row.ID, copyRev, copy); err != nil {
					return err
				}
			}
			current.After = row.ID
		}
		return tx.Put(ctx, Namespace+".source_closures", key, rev, current)
	})
}
func (h *Host) expireSourceCopy(ctx context.Context, st runtime.Store, scope runtime.Scope, work runtime.Work) error {
	return runtime.Finish(ctx, st, scope, []string{Namespace}, work, runtime.Done(), func(tx runtime.Tx) error {
		var copy memory.CopyHolder
		rev, err := tx.Get(ctx, Namespace+".source_copies", work.Job.ResponsibilityKey, &copy)
		if err != nil {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		until, err := api.ParseTime(copy.RetainUntil)
		if err != nil {
			return err
		}
		if now.Before(until) {
			return api.E("invalid_state", "original_copy_not_expired")
		}
		if copy.UseState != "allowed" {
			return nil
		}
		copy.UseState = "closing"
		copy.Revision = rev + 1
		return tx.Put(ctx, Namespace+".source_copies", copy.CopyID, rev, copy)
	})
}
func (h *Host) releaseSourceCopy(ctx context.Context, tx runtime.Tx, a runtime.Auth, command api.Command, in memory.ReleaseCopyInput) (memory.CopyOutput, error) {
	if err := h.peer(a); err != nil {
		return memory.CopyOutput{}, err
	}
	if command.TargetID != in.ContentRef.ContentID {
		return memory.CopyOutput{}, api.E("invalid_request", "target_mismatch")
	}
	var copy memory.CopyHolder
	rev, err := tx.Get(ctx, Namespace+".source_copies", in.CopyID, &copy)
	if err != nil {
		return memory.CopyOutput{}, err
	}
	if !api.Equal(copy.ContentRef, in.ContentRef) || copy.HolderRef.OwnerID != h.Config.Authority.OwnerID {
		return memory.CopyOutput{}, api.E("forbidden", "original_copy_holder_mismatch")
	}
	rec, err := h.sourceContentTx(ctx, tx, in.ContentRef)
	if err != nil {
		return memory.CopyOutput{}, err
	}
	if in.ControlRevision != rec.ControlRevision {
		return memory.CopyOutput{}, runtime.ErrConflict
	}
	if !has([]string{"pending", "complete", "residual", "unknown"}, in.CleanupState) || in.CleanupState == "complete" && (!in.UseStopped || len(in.EvidenceRefs) == 0) || in.CleanupState == "residual" && in.ResidualReason == "" {
		return memory.CopyOutput{}, api.E("invalid_request", "invalid_cleanup_report")
	}
	if copy.UseState == "use_stopped" && !in.UseStopped {
		return memory.CopyOutput{}, api.E("invalid_state", "copy_cannot_resume")
	}
	if copy.CleanupState == "complete" && in.CleanupState != "complete" {
		return memory.CopyOutput{}, api.E("invalid_state", "cleanup_cannot_regress")
	}
	// 完成只能引用源本机已实际缓存/发布的准确证据。普通foreign Ref不自动成为来源真值。
	for _, evidence := range in.EvidenceRefs {
		if api.ValidateRecord("ContentRef", evidence) != nil || evidence.TenantID != h.Scope.TenantID {
			return memory.CopyOutput{}, api.E("forbidden", "cleanup_evidence_scope_mismatch")
		}
		var actual contentRecord
		if _, err = tx.Get(ctx, Namespace+".contents", contentKey(evidence), &actual); err != nil {
			return memory.CopyOutput{}, api.E("dependency_unavailable", "original_cleanup_evidence_not_cached")
		}
		if !actual.Complete || !api.Equal(actual.Permission.ContentRef, evidence) {
			return memory.CopyOutput{}, api.E("forbidden", "original_cleanup_evidence_changed")
		}
	}
	copy.Revision = rev + 1
	copy.ControlRevision = rec.ControlRevision
	copy.CleanupState = in.CleanupState
	copy.EvidenceRefs = append([]api.ContentRef{}, in.EvidenceRefs...)
	copy.ResidualReason = in.ResidualReason
	if in.UseStopped {
		copy.UseState = "use_stopped"
	}
	if err = tx.Put(ctx, Namespace+".source_copies", copy.CopyID, rev, copy); err != nil {
		return memory.CopyOutput{}, err
	}
	return sourceCopyOutput(copy), nil
}
func (h *Host) sourceGet(ctx context.Context, st runtime.Store, scope runtime.Scope, a runtime.Auth, q api.Query, in memory.GetContentInput) (memory.GetContentOutput, error) {
	if q.TargetID != in.ContentRef.ContentID || in.CopyID == "" || !has([]string{"control", "bytes"}, in.Mode) {
		return memory.GetContentOutput{}, api.E("invalid_request", "original_copy_mode_required")
	}
	var copy memory.CopyHolder
	if _, err := st.Read(ctx, scope, Namespace+".source_copies", in.CopyID, 0, &copy); err != nil {
		return memory.GetContentOutput{}, err
	}
	if !api.Equal(copy.ContentRef, in.ContentRef) {
		return memory.GetContentOutput{}, api.E("forbidden", "copy_holder_mismatch")
	}
	r := memory.ForeignReference{ContentRef: copy.ContentRef, CopyID: copy.CopyID, HolderRef: copy.HolderRef, ReferenceIntentRef: copy.ReferenceIntentRef, Purpose: copy.Purpose, Location: copy.Location, RetainUntil: copy.RetainUntil}
	if in.Mode == "bytes" && (in.Purpose != r.Purpose || in.Location != r.Location) {
		return memory.GetContentOutput{}, api.E("forbidden", "original_copy_purpose_mismatch")
	}
	p, err := h.sourceCurrent(ctx, st, scope, a, q, SourceCurrent{Reference: r, Control: in.Mode == "control"})
	if err != nil {
		return memory.GetContentOutput{}, err
	}
	out := memory.GetContentOutput{ContentRef: in.ContentRef, Mode: in.Mode, ControlRevision: p.ControlRevision, UseState: p.UseState, CleanupState: p.CleanupState}
	if in.Mode == "control" {
		return out, nil
	}
	if in.ContentRef.ByteLength > 2048 {
		return out, api.E("unsupported", "use_bounded_byte_transport")
	}
	chunk, err := h.contentGet(ctx, st, scope, a, q, ContentGet{ContentRef: in.ContentRef, Reference: &r})
	if err != nil {
		return out, err
	}
	if _, err = base64.StdEncoding.Strict().DecodeString(chunk.DataBase64); err != nil {
		return out, err
	}
	out.BytesBase64 = chunk.DataBase64
	return out, nil
}
