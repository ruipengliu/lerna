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

type brainBridge struct{ a *App }

func (b brainBridge) Dispatch(ctx context.Context, s runtime.Scope, i api.DecisionDispatchIntent, snap api.Snapshot) error {
	t, e := b.a.Task.Read(ctx, b.a.Store, s, b.a.ServiceAuth, i.TaskRef.ObjectID)
	if e != nil {
		return e
	}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: i.BrainOwnerID, CommandID: i.CommandID, TargetID: i.DecisionID, Method: "brain.decide", ExpiresAt: t.Deadline, Payload: api.Raw(brain.DecideInput{DecisionID: i.DecisionID, TaskRef: i.TaskRef, SnapshotRef: i.SnapshotRef, SnapshotRevision: i.SnapshotRevision, ModelProfileRef: i.ModelProfileRef, UseRefs: []api.ObjectRef{}, Limits: []api.Amount{{Unit: "USD", Value: "0"}}, Deadline: t.Deadline})}
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
	raw, e := b.a.Memory.Read(ctx, s, b.a.ServiceAuth, *view.Decision.ProposalRef, "brain.output")
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
	var original task.Proposal
	_, e = b.a.Store.Read(ctx, s, "platform.prepared_proposals", i.DecisionID, 0, &original)
	if e == nil {
		return original, nil
	}
	if !api.IsCode(e, "not_found") {
		return original, e
	}
	snapshotBytes, e := b.a.Memory.Read(ctx, s, b.a.ServiceAuth, i.SnapshotRef, "brain.input")
	if e != nil {
		return original, e
	}
	var snap api.Snapshot
	if e = api.Decode(snapshotBytes, &snap); e != nil {
		return original, e
	}
	original = task.Proposal{DecisionID: i.DecisionID, Kind: p.Kind, ReasonRef: p.ReasonRef, RequirementDelta: p.RequirementDelta, ArtifactRefs: p.ArtifactRefs, Limitations: []string{}}
	if p.Kind == "act" {
		for _, candidate := range p.Actions {
			if !api.Equal(candidate.BindingRef, b.a.ReadBinding) && !api.Equal(candidate.BindingRef, b.a.WriteBinding) {
				return original, api.E("forbidden", "binding_not_registered")
			}
			if !api.Equal(candidate.CapabilityRef, filedriver.FileReadCapability().Ref) && !api.Equal(candidate.CapabilityRef, filedriver.FileWriteCapability().Ref) {
				return original, api.E("unsupported", "capability_not_configured")
			}
			args, e := b.a.Memory.Read(ctx, s, b.a.ServiceAuth, candidate.ArgumentsRef, "execution.arguments")
			if e != nil {
				return original, e
			}
			var path string
			if api.Equal(candidate.CapabilityRef, filedriver.FileReadCapability().Ref) {
				var in filedriver.FileReadArguments
				if e = api.Decode(args, &in); e != nil {
					return original, e
				}
				path = in.Path
			} else {
				var in filedriver.FileWriteArguments
				if e = api.Decode(args, &in); e != nil {
					return original, e
				}
				path = in.Path
			}
			resources, e := b.a.Publish(ctx, s, b.a.ServiceAuth, stableID("content", "resources/"+i.DecisionID+"/"+candidate.LocalKey), "application/json", api.Raw([]api.ObjectRef{}), candidate.ProcessedSourceRefs, []api.ContentRef{})
			if e != nil {
				return original, e
			}
			reqs := []api.RequirementRef{}
			for _, r := range snap.Requirements {
				reqs = append(reqs, api.RequirementRef{RequirementID: r.RequirementID, Revision: r.Revision})
			}
			original.Actions = append(original.Actions, task.PreparedAction{OperationID: stableID("operation", i.DecisionID+"/"+candidate.LocalKey), ExecutorID: s.OwnerID, CapabilityRef: candidate.CapabilityRef, BindingRef: candidate.BindingRef, InstallLockRef: b.a.InstallLock, ArgumentsRef: candidate.ArgumentsRef, ResourcesRef: resources, RequirementRefs: reqs, UseIntentRefs: []api.ObjectRef{s.Ref(stableID("use", i.DecisionID+"/"+candidate.LocalKey), 1)}, CostBound: []api.Amount{{Unit: "USD", Value: "0"}}, LogicalStepKey: snap.TaskRef.ObjectID + "/" + candidate.LocalKey, ProcessedSourceRefs: candidate.ProcessedSourceRefs, DisclosedSourceRefs: candidate.DisclosedSourceRefs, ResourceKeys: []string{"file:" + path}, Independent: true, SafeRequirementCheck: false, CommandID: stableID("command", "invoke/"+i.DecisionID+"/"+candidate.LocalKey)})
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
		return tx.Create(ctx, "platform.prepared_proposals", i.DecisionID, i.TaskRef.ObjectID, original)
	})
	if status == runtime.CommitUnknown {
		return original, runtime.ErrCommitUnknown
	}
	return original, e
}
func (b brainBridge) Usage(ctx context.Context, s runtime.Scope, r api.ObjectRef) (api.UsageSnapshot, error) {
	return b.a.Brain.Usage(ctx, b.a.Store, s, r)
}
