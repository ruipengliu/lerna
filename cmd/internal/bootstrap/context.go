package bootstrap

import (
	"context"
	filedriver "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type contextCompiler struct{ a *App }

func (c contextCompiler) Prepare(ctx context.Context, scope runtime.Scope, auth runtime.Auth, t api.Task) (task.PreparedDecision, error) {
	facts, e := c.a.Task.ContextFacts(ctx, c.a.Store, scope, auth, t.TaskID)
	if e != nil {
		return task.PreparedDecision{}, e
	}
	if facts.Task.Revision != t.Revision {
		return task.PreparedDecision{}, api.E("dependency_unavailable", "snapshot_changed")
	}
	for _, x := range facts.UnresolvedCollections {
		if !x.Complete || x.UnresolvedCount > 0 {
			return task.PreparedDecision{}, api.E("dependency_unavailable", "original_effect_open")
		}
	}
	processed := []api.ContentRef{t.GoalRef}
	for _, source := range facts.SourceRefs {
		processed = append(processed, source.ContentRef)
	}
	for _, op := range facts.Operations {
		processed = append(processed, op.Intent.ProcessedSourceRefs...)
	}
	processed = uniqueSources(processed)
	for _, r := range processed {
		if _, e = c.a.Memory.Read(ctx, scope, auth, r, "task.context"); e != nil {
			return task.PreparedDecision{}, e
		}
	}
	purpose := "decide"
	if t.RequirementsState == "collecting" || t.RequirementsState == "awaiting_input" {
		purpose = "interpret_requirements"
	}
	key := t.TaskID + "/" + string(api.Raw([]uint64{t.GoalRevision, t.ControlRevision, t.Revision}))
	snapshotID := stableID("snapshot", key)
	selection, e := c.a.Publish(ctx, scope, c.a.ServiceAuth, stableID("content", "selection/"+key), "application/json", api.Raw(struct {
		TaskRef         api.ObjectRef    `json:"task_ref"`
		RequiredSources []api.ContentRef `json:"required_sources"`
		FactRefs        []api.ObjectRef  `json:"fact_refs"`
		Clipped         bool             `json:"clipped"`
	}{scope.Ref(t.TaskID, t.Revision), processed, facts.FactRefs, false}), processed, []api.ContentRef{})
	if e != nil {
		return task.PreparedDecision{}, e
	}
	snap := api.Snapshot{SnapshotID: snapshotID, Revision: 1, TaskRef: scope.Ref(t.TaskID, t.Revision), GoalRevision: t.GoalRevision, ControlRevision: t.ControlRevision, GoalRef: t.GoalRef, Requirements: t.Requirements, RequirementsDigest: t.RequirementsDigest, CoverageRef: t.CurrentCoverageRef, RequirementsState: t.RequirementsState, Purpose: purpose, FactRefs: facts.FactRefs, UnresolvedCollections: facts.UnresolvedCollections, PolicyRef: t.PolicyRef, InstallLockRef: c.a.InstallLock, ModelProfileRef: c.a.Profile.Ref, CapabilityRefs: []api.ComponentRef{filedriver.FileReadCapability().Ref, filedriver.FileWriteCapability().Ref}, BindingRefs: []api.ObjectRef{c.a.ReadBinding, c.a.WriteBinding}, MaterialRefs: processed, SelectionReportRef: selection, ProcessedSources: processed, ReservedOutputTokens: 4096, SafetyMarginTokens: c.a.Profile.SafetyMargin, CountMode: "upper_bound", TokenizerRef: component("rule-byte-count")}
	goal, e := c.a.goalBytes(ctx, scope, auth, t.GoalRef)
	if e != nil {
		return task.PreparedDecision{}, e
	}
	engine := &brain.RuleEngine{}
	encoding, e := engine.Encode(ctx, snap, goal, c.a.Profile)
	if e != nil {
		return task.PreparedDecision{}, e
	}
	snap.InputTokens = encoding.InputTokens
	snap.EncodedDigest = encoding.Digest
	ref, e := c.a.Publish(ctx, scope, c.a.ServiceAuth, stableID("content", "snapshot/"+key), "application/vnd.harness.snapshot+json", api.Raw(snap), processed, []api.ContentRef{})
	if e != nil {
		return task.PreparedDecision{}, e
	}
	return task.PreparedDecision{Snapshot: snap, SnapshotRef: ref, BrainOwnerID: scope.OwnerID, CostBound: []api.Amount{{Unit: "USD", Value: "0"}}, DecisionID: stableID("decision", key), CommandID: stableID("command", "decision/"+key)}, nil
}
func stableID(prefix, key string) string { return prefix + "_" + api.Hash([]byte(key))[7:39] }

// 完整GoalDocument保持来源；仅完整准确GoalSpec替换可走已登记规则，其余请求补充。
func (a *App) goalBytes(ctx context.Context, s runtime.Scope, auth runtime.Auth, ref api.ContentRef) ([]byte, error) {
	raw, e := a.Memory.Read(ctx, s, auth, ref, "brain.input")
	if e != nil {
		return nil, e
	}
	var doc api.GoalDocument
	if api.Decode(raw, &doc) == nil && doc.FormatVersion == 1 {
		last := doc.InitialGoalRef
		if len(doc.AmendmentRefs) > 0 {
			last = doc.AmendmentRefs[len(doc.AmendmentRefs)-1]
		}
		return a.Memory.Read(ctx, s, auth, last, "brain.input")
	}
	return raw, nil
}

type factSource struct{ a *App }

func (f factSource) Operations(ctx context.Context, snap api.Snapshot) ([]brain.ActionFact, error) {
	facts, e := f.a.Task.ContextFacts(ctx, f.a.Store, f.a.Scope, f.a.ServiceAuth, snap.TaskRef.ObjectID)
	if e != nil {
		return nil, e
	}
	if facts.Task.GoalRevision != snap.GoalRevision || facts.Task.ControlRevision != snap.ControlRevision {
		return nil, api.E("revision_conflict", "goal_changed")
	}
	out := []brain.ActionFact{}
	for _, op := range facts.Operations {
		if op.Intent.GoalRevision != snap.GoalRevision {
			continue
		}
		operation, e := (executionBridge{f.a}).Read(ctx, f.a.Scope, op.Fact.Ref)
		if e != nil {
			return nil, e
		}
		var bytes []byte
		if operation.ResultRef != nil {
			bytes, e = f.a.Memory.Read(ctx, f.a.Scope, f.a.ServiceAuth, *operation.ResultRef, "brain.input")
			if e != nil {
				return nil, e
			}
		}
		out = append(out, brain.ActionFact{Ref: op.Fact.Ref, CapabilityRef: op.Intent.CapabilityRef, LogicalStepKey: op.Intent.LogicalStepKey, Operation: operation, ResultBytes: bytes})
	}
	return out, nil
}
