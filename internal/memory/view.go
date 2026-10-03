package memory

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

type OpenViewInput struct {
	ViewID        string         `json:"view_id"`
	ScopeRef      api.ContentRef `json:"scope_ref"`
	Purposes      []string       `json:"purposes"`
	HolderRef     api.ObjectRef  `json:"holder_ref"`
	Location      string         `json:"location"`
	MaxCandidates uint64         `json:"max_candidates"`
}
type ViewOutput struct {
	ViewRef      api.ObjectRef `json:"view_ref"`
	SnapshotHead uint64        `json:"snapshot_head"`
	Cursor       string        `json:"cursor"`
	ExpiresAt    string        `json:"expires_at"`
	Partial      bool          `json:"partial"`
	Gaps         []string      `json:"gaps"`
}
type PullViewInput struct {
	ViewID string `json:"view_id"`
	Cursor string `json:"cursor,omitempty"`
	Limit  uint64 `json:"limit,omitempty"`
}
type AckViewInput struct {
	ViewID     string        `json:"view_id"`
	Cursor     string        `json:"cursor"`
	ReceiptRef api.ObjectRef `json:"receipt_ref"`
}
type AckViewOutput struct {
	ViewID string `json:"view_id"`
	Cursor string `json:"cursor"`
}

func viewCursor(view View, phase string, position uint64) string {
	return view.ViewID + ":" + strings.TrimPrefix(view.VisibilityToken, "sha256:") + ":" + phase + strconv.FormatUint(position, 10)
}
func viewPosition(view View, cursor string) (string, uint64, error) {
	parts := strings.Split(cursor, ":")
	if len(parts) != 3 || parts[0] != view.ViewID || parts[1] != strings.TrimPrefix(view.VisibilityToken, "sha256:") || len(parts[2]) < 2 {
		return "", 0, api.E("invalid_request", "invalid_view_cursor")
	}
	phase := parts[2][:1]
	position, err := strconv.ParseUint(parts[2][1:], 10, 64)
	if err != nil || (phase != "s" && phase != "c") {
		return "", 0, api.E("invalid_request", "invalid_view_cursor")
	}
	return phase, position, nil
}

func (s *Service) openView(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in OpenViewInput) (ViewOutput, error) {
	if c.TargetID != in.ViewID || !api.ValidID(in.ViewID) || in.MaxCandidates == 0 || in.MaxCandidates > 200 || len(in.Purposes) == 0 || len(in.Purposes) > 10 || in.HolderRef.ObjectID != auth.SubjectID || in.HolderRef.Revision != auth.CredentialGeneration {
		return ViewOutput{}, api.E("invalid_request", "invalid_view_scope")
	}
	if err := runtime.CheckRef(tx.Scope(), in.HolderRef); err != nil {
		return ViewOutput{}, err
	}
	if _, err := s.CheckContentTx(ctx, tx, auth, in.ScopeRef, "memory.sync", in.Location, true); err != nil {
		return ViewOutput{}, err
	}
	if in.Location != s.Location {
		if _, err := s.CheckContentTx(ctx, tx, auth, in.ScopeRef, "memory.sync", s.Location, true); err != nil {
			return ViewOutput{}, err
		}
	}
	head, err := loadHead(ctx, tx)
	if err != nil {
		return ViewOutput{}, err
	}
	token, err := s.visibility(ctx, tx, auth)
	if err != nil {
		return ViewOutput{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return ViewOutput{}, err
	}
	view := View{ViewID: in.ViewID, Revision: 1, PrincipalID: auth.SubjectID, VisibilityToken: token, ExpiresAt: api.Time(now.Add(5 * time.Minute)), SnapshotHead: head.ChangeHead, Snapshot: []api.ObjectRef{}, Gaps: []string{}, Purposes: in.Purposes, ScopeRef: in.ScopeRef, HolderRef: in.HolderRef, Location: in.Location}
	rows, err := tx.List(ctx, "memory.records", "", "", int(in.MaxCandidates)+1)
	if err != nil {
		return ViewOutput{}, err
	}
	if len(rows) > int(in.MaxCandidates) {
		view.Partial = true
		view.Gaps = append(view.Gaps, "candidate_limit")
		rows = rows[:in.MaxCandidates]
	}
	for _, row := range rows {
		var record MemoryRecord
		if err = row.Decode(&record); err != nil {
			return ViewOutput{}, err
		}
		allowed := true
		for _, purpose := range append([]string{"memory.sync"}, in.Purposes...) {
			if err = s.viewAllowed(ctx, tx, auth, view, record, purpose); err != nil {
				allowed = false
				if isUnavailable(err) {
					view.Partial = true
					view.Gaps = unique(append(view.Gaps, "permission_authority_unavailable"))
				}
				if !isUnavailable(err) && !api.IsCode(err, "forbidden") && !api.IsCode(err, "gone") {
					return ViewOutput{}, err
				}
				break
			}
		}
		if !allowed {
			continue
		}
		view.Snapshot = append(view.Snapshot, tx.Scope().Ref(record.MemoryID, record.Revision))
		if err = s.registerViewCopy(ctx, tx, auth, view, record); err != nil {
			return ViewOutput{}, err
		}
	}
	view.PullCursor = viewCursor(view, "s", 0)
	if err = tx.Create(ctx, "memory.views", in.ViewID, auth.SubjectID, view); err != nil {
		return ViewOutput{}, err
	}
	return ViewOutput{ViewRef: tx.Scope().Ref(in.ViewID, 1), SnapshotHead: view.SnapshotHead, Cursor: view.PullCursor, ExpiresAt: view.ExpiresAt, Partial: view.Partial, Gaps: view.Gaps}, nil
}

func (s *Service) viewAllowed(ctx context.Context, tx runtime.Tx, auth runtime.Auth, view View, record MemoryRecord, purpose string) error {
	if err := s.memoryAllowed(ctx, tx, auth, record, purpose, true); err != nil {
		return err
	}
	if view.Location != s.Location {
		return s.memoryAllowedAt(ctx, tx, auth, record, purpose, view.Location, true)
	}
	return nil
}

func (s *Service) registerViewCopy(ctx context.Context, tx runtime.Tx, auth runtime.Auth, view View, record MemoryRecord) error {
	var content ContentVersion
	if _, err := tx.Get(ctx, "content.versions", contentKey(record.Values.ContentRef), &content); err != nil {
		return err
	}
	retention, _ := api.ParseTime(content.RetentionUntil)
	until, _ := api.ParseTime(view.ExpiresAt)
	if until.After(retention) {
		until = retention
	}
	copyID := semanticID("copy", view.ViewID+":"+record.MemoryID+":"+fmt.Sprint(record.Revision))
	_, err := s.RegisterCopyTx(ctx, tx, auth, RegisterCopyInput{CopyID: copyID, ContentRef: record.Values.ContentRef, HolderRef: view.HolderRef, Purpose: "memory.sync", Location: view.Location, RetainUntil: api.Time(until), ReferenceIntentRef: tx.Scope().Ref(view.ViewID, 1)})
	return err
}

func (s *Service) PullView(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in PullViewInput) (ViewPage, error) {
	if in.Limit == 0 {
		in.Limit = 20
	}
	if in.Limit > 20 {
		return ViewPage{}, api.E("invalid_request", "invalid_page_limit")
	}
	var out ViewPage
	err := s.within(ctx, scope, func(tx runtime.Tx) error {
		if err := checkAuth(scope, auth); err != nil {
			return err
		}
		var view View
		rev, err := tx.Get(ctx, "memory.views", in.ViewID, &view)
		if err != nil {
			return err
		}
		if view.PrincipalID != auth.SubjectID {
			return api.E("forbidden", "view_principal_mismatch")
		}
		if _, err = future(ctx, tx, view.ExpiresAt); err != nil {
			return api.E("cursor_expired", "view_expired")
		}
		token, err := s.visibility(ctx, tx, auth)
		if err != nil {
			return err
		}
		if token != view.VisibilityToken {
			return api.E("snapshot_required", "query_scope_changed")
		}
		if _, err = s.CheckContentTx(ctx, tx, auth, view.ScopeRef, "memory.sync", view.Location, true); err != nil {
			return err
		}
		if view.Location != s.Location {
			if _, err = s.CheckContentTx(ctx, tx, auth, view.ScopeRef, "memory.sync", s.Location, true); err != nil {
				return err
			}
		}
		requested := in.Cursor
		if requested == "" {
			requested = viewCursor(view, "s", 0)
		}
		if view.IssuedPage != nil && requested == view.IssuedFromCursor {
			// 已发页仍逐项核当前权限，失联不借缓存披露正文或无权ID。
			for _, record := range view.IssuedPage.Records {
				var current MemoryRecord
				if _, err = tx.Get(ctx, "memory.records", record.MemoryID, &current); err != nil {
					return err
				}
				if current.Revision != record.Revision {
					return api.E("snapshot_required", "view_revision_changed")
				}
				for _, purpose := range append([]string{"memory.sync"}, view.Purposes...) {
					if err = s.viewAllowed(ctx, tx, auth, view, current, purpose); err != nil {
						return err
					}
				}
			}
			out = *view.IssuedPage
			return nil
		}
		if requested != view.PullCursor || view.IssuedPage != nil && !view.IssuedAcknowledged {
			return api.E("invalid_state", "view_page_ack_required")
		}
		phase, position, err := viewPosition(view, requested)
		if err != nil {
			return err
		}
		out = ViewPage{ViewID: view.ViewID, Records: []MemoryRecord{}, Changes: []MemoryChange{}, ChangeHead: view.SnapshotHead, Partial: view.Partial, Gaps: append([]string{}, view.Gaps...)}
		if phase == "s" {
			if position > uint64(len(view.Snapshot)) {
				return api.E("invalid_request", "invalid_view_cursor")
			}
			end := position + in.Limit
			if end > uint64(len(view.Snapshot)) {
				end = uint64(len(view.Snapshot))
			}
			for _, ref := range view.Snapshot[position:end] {
				var record MemoryRecord
				_, err = tx.Get(ctx, "memory.records", ref.ObjectID, &record)
				if err != nil {
					return err
				}
				if record.Revision != ref.Revision {
					return api.E("snapshot_required", "view_revision_changed")
				}
				for _, purpose := range append([]string{"memory.sync"}, view.Purposes...) {
					if err = s.viewAllowed(ctx, tx, auth, view, record, purpose); err != nil {
						return err
					}
				}
				out.Records = append(out.Records, record)
			}
			out.SnapshotComplete = end == uint64(len(view.Snapshot))
			if out.SnapshotComplete {
				out.Cursor = viewCursor(view, "c", view.SnapshotHead)
			} else {
				out.Cursor = viewCursor(view, "s", end)
			}
		} else {
			head, err := loadHead(ctx, tx)
			if err != nil {
				return err
			}
			if position < view.SnapshotHead || position > head.ChangeHead {
				return api.E("snapshot_required", "view_log_gap")
			}
			out.SnapshotComplete = true
			out.ChangeHead = head.ChangeHead
			rows, err := tx.List(ctx, "memory.changes", "", fmt.Sprintf("%016d", position), int(in.Limit))
			if err != nil {
				return err
			}
			for _, row := range rows {
				var change MemoryChange
				if err = row.Decode(&change); err != nil {
					return err
				}
				if change.ChangeSeq != position+1 {
					return api.E("snapshot_required", "view_log_gap")
				}
				position = change.ChangeSeq
				var record MemoryRecord
				if err = tx.GetVersion(ctx, "memory.records", change.MemoryID, change.Revision, &record); err != nil {
					return err
				}
				allowed := true
				for _, purpose := range append([]string{"memory.sync"}, view.Purposes...) {
					if err = s.viewAllowed(ctx, tx, auth, view, record, purpose); err != nil {
						allowed = false
						if isUnavailable(err) {
							out.Partial = true
							out.Gaps = unique(append(out.Gaps, "permission_authority_unavailable"))
						}
						if !isUnavailable(err) && !api.IsCode(err, "forbidden") && !api.IsCode(err, "gone") {
							return err
						}
						break
					}
				}
				if allowed {
					var current MemoryRecord
					if _, err = tx.Get(ctx, "memory.records", record.MemoryID, &current); err != nil {
						return err
					}
					if current.Revision != record.Revision {
						return api.E("snapshot_required", "view_revision_changed")
					}
					if err = s.registerViewCopy(ctx, tx, auth, view, record); err != nil {
						return err
					}
					out.Changes = append(out.Changes, change)
					out.Records = append(out.Records, record)
				}
			}
			if len(rows) == 0 && position < head.ChangeHead {
				return api.E("snapshot_required", "view_log_gap")
			}
			out.Cursor = viewCursor(view, "c", position)
		}
		view.IssuedFromCursor = requested
		view.IssuedCursor = out.Cursor
		view.IssuedPage = &out
		view.IssuedAcknowledged = false
		view.Revision = rev + 1
		return tx.Put(ctx, "memory.views", view.ViewID, rev, view)
	})
	return out, err
}

func (s *Service) ackView(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AckViewInput) (AckViewOutput, error) {
	var view View
	rev, err := tx.Get(ctx, "memory.views", in.ViewID, &view)
	if err != nil {
		return AckViewOutput{}, err
	}
	if c.TargetID != in.ViewID || view.PrincipalID != auth.SubjectID {
		return AckViewOutput{}, api.E("forbidden", "view_principal_mismatch")
	}
	if err = runtime.CheckRef(tx.Scope(), in.ReceiptRef); err != nil {
		return AckViewOutput{}, err
	}
	if in.Cursor == view.AckCursor {
		return AckViewOutput{in.ViewID, in.Cursor}, nil
	}
	if view.IssuedPage == nil || in.Cursor != view.IssuedCursor {
		return AckViewOutput{}, api.E("invalid_state", "view_cursor_not_issued")
	}
	view.AckCursor = in.Cursor
	view.PullCursor = in.Cursor
	view.LastReceiptRef = &in.ReceiptRef
	view.IssuedAcknowledged = true
	view.Revision = rev + 1
	if err = tx.Put(ctx, "memory.views", in.ViewID, rev, view); err != nil {
		return AckViewOutput{}, err
	}
	return AckViewOutput{in.ViewID, in.Cursor}, nil
}

func (s *Service) registerViews(registry *runtime.Registry) {
	command(s, registry, "memory.view.open", "memory", false, s.openView)
	command(s, registry, "memory.view.ack", "memory", false, s.ackView)
	query(s, registry, "memory.view.pull", "memory", func(ctx context.Context, scope runtime.Scope, auth runtime.Auth, q api.Query, in PullViewInput) (ViewPage, error) {
		if q.TargetID != in.ViewID {
			return ViewPage{}, api.E("invalid_request", "target_mismatch")
		}
		return s.PullView(ctx, scope, auth, in)
	})
}
