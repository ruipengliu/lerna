package development

import (
	"context"
	"encoding/json"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/interaction"
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
	histories := []interaction.HistoryView{}
	requiredHistory := ""
	original, e := c.a.Store.LookupCommand(ctx, scope, t.SubmitCommandID)
	if e != nil {
		return task.PreparedDecision{}, e
	}
	var submitted task.SubmitInput
	if original.Command.Method == "task.submit" {
		if e = api.Decode(original.Command.Payload, &submitted); e != nil {
			return task.PreparedDecision{}, e
		}
		if submitted.SourceSubmissionRef != nil {
			requiredHistory = submitted.SourceSubmissionRef.ObjectID
		}
	}
	seenHistory := map[string]bool{}
	for _, source := range facts.SourceRefs {
		processed = append(processed, source.ContentRef)
		if source.SubmissionRef == nil || source.SubmissionRef.ObjectID == t.SubmitCommandID || seenHistory[source.SubmissionRef.ObjectID] {
			continue
		}
		seenHistory[source.SubmissionRef.ObjectID] = true
		history, err := c.a.Interaction.History(ctx, c.a.Store, scope, auth, *source.SubmissionRef)
		if api.IsCode(err, "not_found") && source.SubmissionRef.ObjectID != requiredHistory {
			continue // 非Session的原输入/修订依据由Task自己的源事实核验。
		}
		if err != nil {
			return task.PreparedDecision{}, err
		}
		if !history.Complete {
			return task.PreparedDecision{}, api.E("dependency_unavailable", "history_incomplete")
		}
		histories = append(histories, history)
		processed = append(processed, history.Submission.Submission.ContentRef)
		processed = append(processed, history.Submission.AttachmentRefs...)
		for _, message := range history.Messages {
			processed = append(processed, message.ContentRef)
		}
	}
	for _, op := range facts.Operations {
		processed = append(processed, op.Intent.ProcessedSourceRefs...)
		if c.a.Model != nil {
			operation, err := (executionBridge{c.a}).Read(ctx, scope, op.Fact.Ref)
			if err != nil {
				return task.PreparedDecision{}, err
			}
			if operation.ResultRef != nil {
				processed = append(processed, *operation.ResultRef)
			}
		}
	}
	processed = append(processed, facts.Artifacts...)
	information, informationSources, e := c.a.informationContext(ctx, scope, t, facts)
	if e != nil {
		return task.PreparedDecision{}, e
	}
	processed = append(processed, informationSources...)
	for _, check := range facts.Checks {
		processed = append(processed, check.ArtifactRef, check.ScopeRef)
		processed = append(processed, check.EvidenceRefs...)
	}
	processed = uniqueSources(processed)
	for _, r := range processed {
		if _, e = c.a.ReadContent(ctx, scope, auth, r, "task.context"); e != nil {
			return task.PreparedDecision{}, e
		}
	}
	purpose := "decide"
	if t.RequirementsState == "collecting" || t.RequirementsState == "awaiting_input" {
		purpose = "interpret_requirements"
	}
	key := t.TaskID + "/" + string(api.Raw([]uint64{t.GoalRevision, t.ControlRevision, t.Revision}))
	snapshotID := stableID("snapshot", key)
	registered, e := c.a.prepareActionSnapshot(ctx, snapshotID, t.Deadline)
	if e != nil {
		return task.PreparedDecision{}, e
	}
	caps := []api.ComponentRef{}
	bindings := []api.ObjectRef{}
	for _, d := range registered.Entries {
		caps = append(caps, d.Capability.Ref)
		bindings = append(bindings, d.BindingRef)
	}
	var knowledge *governance.KnowledgeBundle
	var knowledgePacket api.ContentRef
	installLock := registered.InstallLockRef
	if c.a.Knowledge.Enabled() {
		bundle, packet, err := c.a.Knowledge.Load(ctx, scope, auth, t, snapshotID, registered.InstallLockRef, caps)
		if err != nil {
			return task.PreparedDecision{}, err
		}
		knowledge, knowledgePacket = &bundle, packet
		installLock = bundle.Selection.InstallLockRef
		caps, bindings = []api.ComponentRef{}, []api.ObjectRef{}
		entries := []actionDescriptor{}
		for _, entry := range registered.Entries {
			allowed := false
			for _, cap := range bundle.Selection.EffectiveCapabilityRefs {
				allowed = allowed || api.Equal(cap, entry.Capability.Ref)
			}
			if allowed {
				caps = append(caps, entry.Capability.Ref)
				bindings = append(bindings, entry.BindingRef)
				entries = append(entries, entry)
			}
		}
		registered.Entries = entries
		processed = uniqueSources(append(append(processed, bundle.Selection.SourceRefs...), packet))
	}
	if c.a.Model != nil {
		packet, err := c.a.Publish(ctx, scope, c.a.ServiceAuth, stableID("content", "context-facts/"+key), "application/vnd.harness.context+json", api.Raw(struct {
			Facts                   task.ContextFacts                  `json:"facts"`
			History                 []interaction.HistoryView          `json:"history"`
			ArtifactRule            api.ComponentRef                   `json:"artifact_rule"`
			SavedRule               api.ComponentRef                   `json:"saved_rule"`
			AnswerSchema            api.ComponentRef                   `json:"answer_schema_ref"`
			GoalSchema              api.Schema                         `json:"goal_schema"`
			Actions                 []actionDeclaration                `json:"actions"`
			InformationQuestion     *InformationQuestion               `json:"information_question,omitempty"`
			InformationRule         *api.ComponentRef                  `json:"information_rule,omitempty"`
			InformationObservations []providers.InformationObservation `json:"information_observations,omitempty"`
		}{facts, histories, c.a.ArtifactRule, c.a.SavedRule, c.a.AnswerSchema, brain.GoalSchema(), registered.declarations(), information.Question, information.Rule, information.Observations}), processed, []api.ContentRef{})
		if err != nil {
			return task.PreparedDecision{}, err
		}
		processed = append(processed, packet)
	}
	selection, e := c.a.Publish(ctx, scope, c.a.ServiceAuth, stableID("content", "selection/"+key), "application/json", api.Raw(struct {
		TaskRef         api.ObjectRef    `json:"task_ref"`
		RequiredSources []api.ContentRef `json:"required_sources"`
		FactRefs        []api.ObjectRef  `json:"fact_refs"`
		Clipped         bool             `json:"clipped"`
	}{scope.Ref(t.TaskID, t.Revision), processed, facts.FactRefs, false}), processed, []api.ContentRef{})
	if e != nil {
		return task.PreparedDecision{}, e
	}
	reservedOutput := uint64(4096)
	if reservedOutput > c.a.Profile.MaxOutputTokens {
		reservedOutput = c.a.Profile.MaxOutputTokens
	}
	if knowledge != nil && reservedOutput > knowledge.Selection.EffectiveControls.MaxOutputTokens {
		reservedOutput = knowledge.Selection.EffectiveControls.MaxOutputTokens
	}
	snap := api.Snapshot{SnapshotID: snapshotID, Revision: 1, TaskRef: scope.Ref(t.TaskID, t.Revision), GoalRevision: t.GoalRevision, ControlRevision: t.ControlRevision, GoalRef: t.GoalRef, Requirements: t.Requirements, RequirementsDigest: t.RequirementsDigest, CoverageRef: t.CurrentCoverageRef, RequirementsState: t.RequirementsState, Purpose: purpose, FactRefs: facts.FactRefs, UnresolvedCollections: facts.UnresolvedCollections, PolicyRef: t.PolicyRef, InstallLockRef: installLock, ModelProfileRef: c.a.Profile.Ref, CapabilityRefs: caps, BindingRefs: bindings, MaterialRefs: processed, SelectionReportRef: selection, ProcessedSources: processed, ReservedOutputTokens: reservedOutput, SafetyMarginTokens: c.a.Profile.SafetyMargin, CountMode: "upper_bound", TokenizerRef: c.a.TokenizerRef}
	goal, e := c.a.ReadContent(ctx, scope, auth, t.GoalRef, "brain.input")
	if e != nil {
		return task.PreparedDecision{}, e
	}
	encoding, e := c.a.Engine.Encode(ctx, snap, goal, c.a.Profile)
	if e != nil {
		return task.PreparedDecision{}, e
	}
	snap.InputTokens = encoding.InputTokens
	snap.EncodedDigest = encoding.Digest
	snap.CountMode = encoding.CountMode
	bound, e := c.a.decisionCost(snap)
	if e != nil {
		return task.PreparedDecision{}, e
	}
	if knowledge != nil {
		if e = c.a.Knowledge.CheckEncoding(*knowledge, snap, encoding, bound); e != nil {
			return task.PreparedDecision{}, e
		}
	}
	ref, e := c.a.Publish(ctx, scope, c.a.ServiceAuth, stableID("content", "snapshot/"+key), "application/vnd.harness.snapshot+json", api.Raw(snap), processed, []api.ContentRef{})
	if e != nil {
		return task.PreparedDecision{}, e
	}
	prepared := task.PreparedDecision{Snapshot: snap, SnapshotRef: ref, BrainOwnerID: scope.OwnerID, CostBound: bound, DecisionID: stableID("decision", key), CommandID: stableID("command", "decision/"+key)}
	if knowledge != nil {
		if e = c.a.Knowledge.Freeze(ctx, scope, auth, *knowledge, knowledgePacket, prepared); e != nil {
			return task.PreparedDecision{}, e
		}
	}
	if _, e = c.a.prepareForeignSources(ctx, scope, auth, processed, "task.snapshot", "cloud"); e != nil {
		return task.PreparedDecision{}, e
	}
	if _, e = c.a.prepareForeignSources(ctx, scope, c.a.ServiceAuth, processed, "task.snapshot", "cloud"); e != nil {
		return task.PreparedDecision{}, e
	}
	return prepared, nil
}

func (c contextCompiler) CommitTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, prepared task.PreparedDecision) error {
	return c.a.Knowledge.CommitTx(ctx, tx, auth, prepared)
}
func stableID(prefix, key string) string { return prefix + "_" + api.Hash([]byte(key))[7:39] }

// 完整GoalDocument保持来源；仅完整准确GoalSpec替换可走已登记规则，其余请求补充。
func (a *App) goalBytes(ctx context.Context, s runtime.Scope, auth runtime.Auth, ref api.ContentRef) ([]byte, error) {
	raw, e := a.ReadContent(ctx, s, auth, ref, "brain.input")
	if e != nil {
		return nil, e
	}
	var doc api.GoalDocument
	if api.Decode(raw, &doc) == nil && doc.FormatVersion == 1 {
		last := doc.InitialGoalRef
		if len(doc.AmendmentRefs) > 0 {
			last = doc.AmendmentRefs[len(doc.AmendmentRefs)-1]
		}
		return a.ReadContent(ctx, s, auth, last, "brain.input")
	}
	return raw, nil
}

// GoalDocument 只是原 owner 的确定性包装；它不能自报新的本人来源。
// 按原组件顺序返回 Task 已保存的完整来源依据，绝不从派生引用猜 SubmissionRef。
func (a *App) goalSourceEvidence(ctx context.Context, scope runtime.Scope, goalRef api.ContentRef, originals []api.SourceEvidence, snapshot *api.Snapshot) ([]api.SourceEvidence, error) {
	declared := func(ref api.ContentRef) bool {
		if snapshot == nil {
			return true
		}
		material, processed := false, false
		for _, r := range snapshot.MaterialRefs {
			material = material || api.Equal(ref, r)
		}
		for _, r := range snapshot.ProcessedSources {
			processed = processed || api.Equal(ref, r)
		}
		return material && processed
	}
	if !declared(goalRef) {
		return nil, api.E("forbidden", "goal_source_not_declared")
	}
	raw, err := a.ReadContent(ctx, scope, a.ServiceAuth, goalRef, "task.context")
	if err != nil {
		return nil, err
	}
	refs := []api.ContentRef{goalRef}
	var document api.GoalDocument
	if api.Decode(raw, &document) == nil && document.FormatVersion == 1 {
		if err = api.ValidateRecord("GoalDocument", document); err != nil {
			return nil, err
		}
		refs = append([]api.ContentRef{document.InitialGoalRef}, document.AmendmentRefs...)
	}
	if len(refs) > 100 {
		return nil, api.E("invalid_request", "requirement_source_limit")
	}
	out := make([]api.SourceEvidence, 0, len(refs))
	used := map[int]bool{}
	for _, ref := range refs {
		if !declared(ref) {
			return nil, api.E("forbidden", "goal_source_not_declared")
		}
		matched := false
		for i, original := range originals {
			if !used[i] && api.Equal(ref, original.ContentRef) {
				if err = api.ValidateRecord("SourceEvidence", original); err != nil {
					return nil, err
				}
				out = append(out, original)
				used[i], matched = true, true
				break
			}
		}
		if !matched {
			return nil, api.E("forbidden", "goal_source_not_original")
		}
	}
	return out, nil
}

type factSource struct{ a *App }

func (f factSource) ResolveGoal(ctx context.Context, snap api.Snapshot, original json.RawMessage) (json.RawMessage, error) {
	if api.Hash(original) != snap.GoalRef.Hash {
		return nil, api.E("invalid_request", "goal_digest_mismatch")
	}
	var doc api.GoalDocument
	if api.Decode(original, &doc) != nil || doc.FormatVersion != 1 {
		return original, nil
	}
	facts, err := f.a.Task.ContextFacts(ctx, f.a.Store, f.a.Scope, f.a.ServiceAuth, snap.TaskRef.ObjectID)
	if err != nil {
		return nil, err
	}
	if _, err = f.a.goalSourceEvidence(ctx, f.a.Scope, snap.GoalRef, facts.SourceRefs, &snap); err != nil {
		return nil, err
	}
	ref := doc.InitialGoalRef
	if len(doc.AmendmentRefs) > 0 {
		ref = doc.AmendmentRefs[len(doc.AmendmentRefs)-1]
	}
	return f.a.ReadContent(ctx, f.a.Scope, f.a.ServiceAuth, ref, "brain.input")
}

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
			bytes, e = f.a.ReadContent(ctx, f.a.Scope, f.a.ServiceAuth, *operation.ResultRef, "brain.input")
			if e != nil {
				return nil, e
			}
		}
		out = append(out, brain.ActionFact{Ref: op.Fact.Ref, CapabilityRef: op.Intent.CapabilityRef, LogicalStepKey: op.Intent.LogicalStepKey, Operation: operation, ResultBytes: bytes})
	}
	return out, nil
}
