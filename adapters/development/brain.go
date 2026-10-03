package development

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type brainBridge struct{ a *App }

func (b brainBridge) Dispatch(ctx context.Context, s runtime.Scope, i api.DecisionDispatchIntent, snap api.Snapshot) error {
	ctx, e := b.a.prepareForeignSources(ctx, s, b.a.ServiceAuth, append(append([]api.ContentRef{}, snap.ProcessedSources...), i.SnapshotRef), "brain.input", "cloud")
	if e != nil {
		return e
	}
	t, e := b.a.Task.Read(ctx, b.a.Store, s, b.a.ServiceAuth, i.TaskRef.ObjectID)
	if e != nil {
		return e
	}
	limits, e := b.a.decisionCost(snap)
	if e != nil {
		return e
	}
	uses := []api.ObjectRef{}
	if b.a.Model != nil {
		uses = append(uses, s.Ref(modelUseID(i.DecisionID), 1))
	}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: i.BrainOwnerID, CommandID: i.CommandID, TargetID: i.DecisionID, Method: "brain.decide", ExpiresAt: t.Deadline, Payload: api.Raw(brain.DecideInput{DecisionID: i.DecisionID, TaskRef: i.TaskRef, SnapshotRef: i.SnapshotRef, SnapshotRevision: i.SnapshotRevision, ModelProfileRef: i.ModelProfileRef, UseRefs: uses, Limits: limits, Deadline: t.Deadline})}
	r, e := b.a.Dispatcher.Command(ctx, b.a.ServiceAuth, api.Raw(c))
	if e != nil {
		return e
	}
	if r.Error != nil {
		return r.Error
	}
	return nil
}
func (b brainBridge) ReadProposal(ctx context.Context, s runtime.Scope, i api.DecisionDispatchIntent) (task.Proposal, error) {
	view, e := b.a.Brain.Get(ctx, b.a.Store, s, b.a.ServiceAuth, i.DecisionID)
	if e != nil {
		return task.Proposal{}, e
	}
	if view.Decision.Status == "provider_result_unknown" {
		return task.Proposal{}, api.E("accounting_unknown", "provider_result_unknown")
	}
	if view.Decision.Status != "completed" || view.Decision.ProposalRef == nil {
		return task.Proposal{}, api.E("dependency_unavailable", "proposal_not_ready")
	}
	raw, e := b.a.ReadContent(ctx, s, b.a.ServiceAuth, *view.Decision.ProposalRef, "brain.output")
	if e != nil {
		return task.Proposal{}, e
	}
	var p brain.Proposal
	if e = api.Decode(raw, &p); e != nil {
		return task.Proposal{}, e
	}
	v, _ := api.NewValidator(brain.ProposalSchema())
	if e = v.Validate(raw); e != nil {
		return task.Proposal{}, e
	}
	knowledge, e := b.a.Knowledge.ProposalLimits(ctx, s, b.a.ServiceAuth, i, p)
	if e != nil {
		return task.Proposal{}, e
	}
	var original task.Proposal
	_, e = b.a.Store.Read(ctx, s, "platform.prepared_proposals", i.DecisionID, 0, &original)
	if e == nil {
		return original, b.a.prepareProposalSources(ctx, s, original)
	}
	if !api.IsCode(e, "not_found") {
		return original, e
	}
	snapshotBytes, e := b.a.ReadContent(ctx, s, b.a.ServiceAuth, i.SnapshotRef, "brain.input")
	if e != nil {
		return original, e
	}
	var snap api.Snapshot
	if e = api.Decode(snapshotBytes, &snap); e != nil {
		return original, e
	}
	original = task.Proposal{DecisionID: i.DecisionID, Kind: p.Kind, ReasonRef: p.ReasonRef, RequirementDelta: p.RequirementDelta, ArtifactRefs: p.ArtifactRefs, Limitations: []string{}}
	if original.RequirementDelta != nil {
		facts, err := b.a.Task.ContextFacts(ctx, b.a.Store, s, b.a.ServiceAuth, snap.TaskRef.ObjectID)
		if err != nil {
			return original, err
		}
		sources, err := b.a.goalSourceEvidence(ctx, s, snap.GoalRef, facts.SourceRefs, &snap)
		if err != nil {
			return original, err
		}
		for n := range original.RequirementDelta.Candidates {
			candidate := &original.RequirementDelta.Candidates[n]
			mapped := []api.SourceEvidence{}
			for _, source := range candidate.SourceRefs {
				// Brain materialize 的包装别名不是本人证明；只展开无自报依据的准确 GoalRef。
				if api.Equal(source.ContentRef, snap.GoalRef) && source.SourceKind == "user_input" && source.SubmissionRef == nil && source.Locator == "" {
					mapped = append(mapped, sources...)
					continue
				}
				matched := false
				for _, original := range sources {
					matched = matched || api.Equal(source, original)
				}
				if !matched {
					return original, api.E("forbidden", "requirement_source_not_original")
				}
				mapped = append(mapped, source)
			}
			candidate.SourceRefs = mapped
		}
	}
	for _, suggestion := range p.CheckSuggestions {
		if len(suggestion.EvidenceRefs) == 0 {
			return original, api.E("invalid_request", "check_artifact_required")
		}
		original.CheckRequests = append(original.CheckRequests, task.AttachInput{TaskID: snap.TaskRef.ObjectID, GoalRevision: snap.GoalRevision, RequirementRef: suggestion.RequirementRef, ArtifactRef: suggestion.EvidenceRefs[0], EvidenceRefs: suggestion.EvidenceRefs})
	}
	admissions := []actionAdmission{}
	if p.Kind == "act" {
		for _, candidate := range p.Actions {
			reqs := []api.RequirementRef{}
			for _, r := range snap.Requirements {
				reqs = append(reqs, api.RequirementRef{RequirementID: r.RequirementID, Revision: r.Revision})
			}
			prepared, admission, err := b.a.prepareAction(ctx, s, i, snap, candidate, reqs)
			if err != nil {
				return original, err
			}
			if knowledge != nil {
				prepared.MaxDurationSeconds = knowledge.Selection.EffectiveControls.MaxActionDurationSeconds
				admission.Prepared = prepared
			}
			original.Actions = append(original.Actions, prepared)
			admissions = append(admissions, admission)
		}
	}
	if p.Kind == "request_input" {
		if p.QuestionRef == nil || p.AnswerSchemaRef == nil {
			return original, api.E("invalid_request", "invalid_input_request")
		}
		t, e := b.a.Task.Read(ctx, b.a.Store, s, b.a.ServiceAuth, i.TaskRef.ObjectID)
		if e != nil {
			return original, e
		}
		revision := snap.GoalRevision
		original.InputRequest = &api.InputRequest{RequestID: stableID("request", i.DecisionID), TenantID: s.TenantID, OwnerID: s.OwnerID, Revision: 1, TargetRef: snap.TaskRef, GoalRevision: &revision, Purpose: p.Purpose, QuestionRef: *p.QuestionRef, AnswerSchemaRef: *p.AnswerSchemaRef, PreviewRefs: p.PreviewRefs, ExpiresAt: t.Deadline, State: "pending"}
	}
	if p.Kind == "fail" {
		original.FailureReason = p.ReasonCode
	}
	status, e := b.a.Store.Within(ctx, s, []string{"platform"}, func(tx runtime.Tx) error {
		var existing task.Proposal
		_, e := tx.Get(ctx, "platform.prepared_proposals", i.DecisionID, &existing)
		if e == nil {
			if !api.Equal(existing, original) {
				return api.E("idempotency_conflict", "prepared_proposal_changed")
			}
			original = existing
			return nil
		}
		if !api.IsCode(e, "not_found") {
			return e
		}
		for _, admission := range admissions {
			var fixed actionAdmission
			_, err := tx.Get(ctx, "platform.action_admissions", admission.Prepared.OperationID, &fixed)
			if err == nil {
				if !api.Equal(fixed, admission) {
					return api.E("idempotency_conflict", "action_admission_changed")
				}
				continue
			}
			if !api.IsCode(err, "not_found") {
				return err
			}
			if err = tx.Create(ctx, "platform.action_admissions", admission.Prepared.OperationID, i.TaskRef.ObjectID, admission); err != nil {
				return err
			}
		}
		return tx.Create(ctx, "platform.prepared_proposals", i.DecisionID, i.TaskRef.ObjectID, original)
	})
	if status == runtime.CommitUnknown {
		return original, runtime.ErrCommitUnknown
	}
	if e != nil {
		return original, e
	}
	return original, b.a.prepareProposalSources(ctx, s, original)
}
func (b brainBridge) Usage(ctx context.Context, s runtime.Scope, r api.ObjectRef) (api.UsageSnapshot, error) {
	u, err := b.a.Brain.Usage(ctx, b.a.Store, s, r)
	if err != nil {
		return u, err
	}
	// 已发生费用沿原Decision的Use身份收尾，不依赖当前模型是否仍启用。
	facts, err := b.a.Brain.AccountingFacts(ctx, b.a.Store, s, b.a.ServiceAuth, u.SourceRef)
	if err != nil {
		return u, err
	}
	return u, b.a.settleUses(ctx, s, facts.UseRefs, u)
}
