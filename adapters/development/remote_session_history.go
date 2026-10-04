package development

import (
	"context"
	"fmt"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/runtime"
	"sort"
)

// remoteSessionHistoryMaterials 只生产普通材料；原 Goal 与 SourceEvidence 不变。
func (a *App) remoteSessionHistoryMaterials(ctx context.Context, scope runtime.Scope, _ runtime.Auth, actual api.Task) ([]api.ContentRef, error) {
	if a.RemoteAgent == nil {
		return nil, nil
	}
	meta, err := a.RemoteAgent.ChildSessionContext(ctx, scope, actual)
	if err != nil || meta == nil {
		return nil, err
	}
	prepared, originalAuth, err := a.RemoteAgent.ChildSessionHistoryAuth(ctx, scope, actual)
	if err != nil {
		return nil, err
	}
	ctx = prepared
	// Session owner 重新核原创建主体、当前会话身体状态；引用不是永久读取许可。
	view, err := a.Interaction.ReadSession(ctx, a.Store, scope, originalAuth, meta.SessionRef.ObjectID, interaction.ReadInput{})
	if err != nil {
		return nil, err
	}
	if view.Session.SessionID != meta.SessionRef.ObjectID || view.Session.OwnerID != scope.OwnerID || view.BodyState != "available" || meta.HistoryCutoff > view.Sequence {
		return nil, api.E("revision_conflict", "original_child_history_cutoff_changed")
	}
	selected := []api.Message{}
	sources := []api.ContentRef{meta.Binding.AccessScopeRef}
	cursor := ""
	examined := 0
	var selectedBytes uint64
	for {
		page, err := a.Interaction.ListMessages(ctx, a.Store, scope, originalAuth, meta.SessionRef.ObjectID, api.ListInput{Limit: 100, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		if page.Partial || len(page.Gaps) != 0 {
			return nil, api.E("dependency_unavailable", "child_session_history_incomplete")
		}
		examined += len(page.Items)
		if examined > 1000 {
			return nil, api.E("overloaded", "child_session_history_scan_limit")
		}
		for _, m := range page.Items {
			if m.SessionRef.OwnerID != scope.OwnerID || m.SessionRef.ObjectID != meta.SessionRef.ObjectID {
				return nil, api.E("forbidden", "child_session_message_scope_changed")
			}
			if m.Seq > meta.HistoryCutoff {
				continue
			}
			if len(selected) >= 100 {
				return nil, api.E("overloaded", "child_session_history_budget_exceeded")
			}
			if m.ContentRef.ByteLength > api.MaxJSONBytes-selectedBytes {
				return nil, api.E("overloaded", "child_session_history_bytes_exceeded")
			}
			selectedBytes += m.ContentRef.ByteLength
			if _, err = a.ReadContent(ctx, scope, originalAuth, m.ContentRef, "task.context"); err != nil {
				return nil, err
			}
			selected = append(selected, m)
			sources = append(sources, m.ContentRef)
		}
		if page.Exhausted {
			break
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			return nil, api.E("dependency_unavailable", "child_session_history_incomplete")
		}
		cursor = page.NextCursor
	}
	sort.Slice(selected, func(i, j int) bool { return selected[i].Seq < selected[j].Seq })
	for i, m := range selected {
		if m.Seq != uint64(i+1) {
			return nil, api.E("dependency_unavailable", "child_session_history_incomplete")
		}
	}
	if uint64(len(selected)) != meta.HistoryCutoff {
		return nil, api.E("dependency_unavailable", "child_session_history_incomplete")
	}
	sources = uniqueSources(sources)
	for _, source := range sources {
		if _, err = a.ReadContent(ctx, scope, originalAuth, source, "task.context"); err != nil {
			return nil, err
		}
	}
	// publisher的service证明只能由它自己当前各用途取得；不复用originalAuth的读取证明。
	for _, source := range sources {
		if _, err = a.ReadContent(ctx, scope, a.ServiceAuth, source, "task.context"); err != nil {
			return nil, err
		}
	}
	body := api.Raw(struct {
		Kind          string                             `json:"kind"`
		FormatVersion uint64                             `json:"format_version"`
		Context       collaboration.RemoteSessionContext `json:"context"`
		Messages      []api.Message                      `json:"messages"`
	}{"ordinary_session_history/1", 1, *meta, selected})
	digest, err := api.Digest(meta)
	if err != nil {
		return nil, err
	}
	id := stableID("content", fmt.Sprintf("remote-child-history/%s/%s", actual.TaskID, digest))
	ref, err := a.Publish(ctx, scope, a.ServiceAuth, id, "application/vnd.harness.child-history+json", body, sources, []api.ContentRef{})
	if err != nil {
		return nil, err
	}
	return append(sources, ref), nil
}
