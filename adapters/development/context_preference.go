package development

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
)

func preferenceFormatAllowed(preference brain.ReportPreference, format string) bool {
	if format != "plain" && format != "bullet" {
		return false
	}
	for _, allowed := range preference.AllowedFormats {
		if format == allowed {
			return true
		}
	}
	return false
}

func (a *App) selectReportPreference(ctx context.Context, facts task.ContextFacts, preference brain.ReportPreference, snapshot *api.Snapshot) (brain.PreferenceResolution, error) {
	out := brain.PreferenceResolution{}
	if !preferenceFormatAllowed(preference, preference.DefaultFormat) {
		return out, api.E("invalid_request", "report_preference_default_not_allowed")
	}
	declared := func(ref api.ContentRef) bool {
		if snapshot == nil {
			return true
		}
		for _, original := range snapshot.MaterialRefs {
			if api.Equal(ref, original) {
				return true
			}
		}
		return false
	}
	for _, material := range facts.ContextMaterials {
		if material.Kind != "memory_query" || material.ContentRef.MediaType != "application/vnd.harness.memory-query-report+json" || material.QueryRef == nil {
			continue
		}
		if !declared(material.ContentRef) || !declared(*material.QueryRef) {
			return out, api.E("forbidden", "ordinary_preference_not_declared")
		}
		body, err := a.Memory.Read(ctx, a.Scope, a.ServiceAuth, material.ContentRef, "task.context")
		if err != nil {
			return out, err
		}
		var report memoryLookupReport
		if err = api.Decode(body, &report); err != nil {
			return out, err
		}
		if !api.Equal(report.Input.QueryRef, preference.QueryRef) || !api.Equal(report.Input.ScopeRef, preference.ScopeRef) {
			continue
		}
		if report.LookupID != material.LookupRef.ObjectID || report.Page.Partial || !report.Page.Exhausted || len(report.Page.Gaps) != 0 || report.Page.NextCursor != "" {
			return out, api.E("dependency_unavailable", "ordinary_preference_query_incomplete")
		}
		selection := brain.PreferenceSelection{QueryRef: preference.QueryRef, ScopeRef: preference.ScopeRef, ReportRef: material.ContentRef, Format: preference.DefaultFormat}
		if len(report.Page.Items) > 0 {
			match := report.Page.Items[0] // 只按原获准查询的固定排名选第一项。
			matched := false
			for _, source := range facts.ContextMaterials {
				matched = matched || source.MemoryRef != nil && api.Equal(*source.MemoryRef, match.MemoryRef) && api.Equal(source.ContentRef, match.ContentRef) && source.QueryRef != nil && api.Equal(*source.QueryRef, *material.QueryRef)
			}
			if !matched || !declared(match.ContentRef) {
				return out, api.E("forbidden", "ordinary_preference_original_memory_missing")
			}
			bytes, err := a.Memory.Read(ctx, a.Scope, a.ServiceAuth, match.ContentRef, "task.context")
			if err != nil {
				return out, err
			}
			var value struct {
				Kind   string `json:"kind"`
				Format string `json:"format"`
			}
			v, err := api.NewValidator(api.Object(map[string]any{"kind": api.Schema{"const": "report.preference"}, "format": api.Enum("plain", "bullet")}, "kind", "format"))
			if err != nil {
				return out, err
			}
			if err = v.Validate(bytes); err != nil {
				return out, err
			}
			if err = api.Decode(bytes, &value); err != nil {
				return out, err
			}
			if !preferenceFormatAllowed(preference, value.Format) {
				return out, api.E("forbidden", "ordinary_preference_format_not_allowed")
			}
			selection.Format, selection.MemoryRef = value.Format, &match.MemoryRef
		}
		return brain.PreferenceResolution{Ready: true, Selection: selection}, nil
	}
	if snapshot == nil {
		return out, api.E("dependency_unavailable", "ordinary_preference_not_resolved")
	}
	var original struct {
		Deadline     string `json:"deadline"`
		TaskDeadline string `json:"task_deadline"`
	}
	if _, err := a.Store.Read(ctx, a.Scope, "platform.context_lookup_deadlines", snapshot.SnapshotID, 1, &original); err != nil {
		return out, err
	}
	out.Query = api.Raw(memory.QueryInput{QueryRef: preference.QueryRef, ScopeRef: preference.ScopeRef, Purposes: []string{"task.context"}, Limits: memory.QueryLimits{MaxCandidates: 20, MaxReadBytes: 4096, MaxPermissionChecks: 500, Deadline: original.Deadline}, Limit: 5})
	return out, nil
}

func (f factSource) Preference(ctx context.Context, snapshot api.Snapshot, preference brain.ReportPreference) (brain.PreferenceResolution, error) {
	facts, err := f.a.Task.ContextFacts(ctx, f.a.Store, f.a.Scope, f.a.ServiceAuth, snapshot.TaskRef.ObjectID)
	if err != nil {
		return brain.PreferenceResolution{}, err
	}
	if facts.Task.GoalRevision != snapshot.GoalRevision || facts.Task.ControlRevision != snapshot.ControlRevision {
		return brain.PreferenceResolution{}, api.E("revision_conflict", "ordinary_preference_snapshot_stale")
	}
	return f.a.selectReportPreference(ctx, facts, preference, &snapshot)
}

// 独立条件负责方复核原允许模板与准确普通选择，不能以模型参数自报判pass。
func (a *App) validatePreferenceParameters(ctx context.Context, facts task.ContextFacts, goal brain.GoalSpec, params brain.RuleParameters) ([]byte, error) {
	format := "plain"
	if goal.Preference != nil {
		selection, err := a.selectReportPreference(ctx, facts, *goal.Preference, nil)
		if err != nil {
			return nil, err
		}
		if !selection.Ready || params.Preference == nil || !api.Equal(*params.Preference, selection.Selection) {
			return nil, api.E("forbidden", "requirement_changes_original_preference")
		}
		format = selection.Selection.Format
	} else if params.Preference != nil {
		return nil, api.E("forbidden", "requirement_adds_unrequested_preference")
	}
	return brain.ReportBytesForFormat(goal, format), nil
}
