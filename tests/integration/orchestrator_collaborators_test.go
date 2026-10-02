package integration_test

import (
	"context"
	"github.com/ruipengliu/lerna/api"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
)

type taskCollaboration struct{ f *taskFixture }

func (v taskCollaboration) Prepare(_ context.Context, _ api.Caller, t api.Task, a api.ActionDelegate) (api.DelegationPreparation, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	return api.DelegationPreparation{Action: a, ReceiverID: taskScope.OwnerID, Internal: true, Submit: api.TaskSubmitInput{OrchestratorID: taskScope.OwnerID, GoalRef: a.Delegation.GoalRef, Constraints: a.Delegation.Constraints, PolicyRef: v.f.policy.Ref, Deadline: a.Delegation.Deadline, Budget: []api.BudgetLimit{{Unit: "usd", Limit: "2"}}}}, nil
}
func (v taskCollaboration) Submit(context.Context, api.Caller, api.OriginalCall, api.DelegationRequest) (api.DelegationObservation, error) {
	panic("local child must not cross external submit")
}
func (v taskCollaboration) Read(context.Context, api.Caller, string, string) (api.DelegationObservation, error) {
	panic("local child must read its original local task")
}
func (v taskCollaboration) Control(context.Context, api.Caller, api.OriginalCall, string, api.TaskGate) (api.DelegationObservation, error) {
	panic("local child control belongs to the bounded local tree")
}

type taskContext struct{ f *taskFixture }

func (v taskContext) Prepare(context.Context, api.Caller, api.Task, []api.ProposalRequestsItem) ([]api.BrainContextMaterialsItem, error) {
	return []api.BrainContextMaterialsItem{{ContentRef: v.f.artifact, Role: "context", SourceRefs: []api.ContentRef{v.f.goal}}}, nil
}

type taskExtraction struct{ f *taskFixture }

func (v taskExtraction) Submit(_ context.Context, _ api.Caller, call api.OriginalCall, _ api.ContentRef) (string, error) {
	v.f.mu.Lock()
	defer v.f.mu.Unlock()
	v.f.extractionCalls = append(v.f.extractionCalls, call)
	return o.ID("extraction", "ack"), nil
}
