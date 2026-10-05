// Package tasks 是任务事实的唯一写入方。
package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

type Store interface {
	SaveRequirements(context.Context, *v1.Requirements) error
	LoadRequirements(context.Context, *v1.Ref) (*v1.Requirements, error)
	SaveProposal(context.Context, *v1.Proposal) error
	LoadProposal(context.Context, *v1.Ref) (*v1.Proposal, error)

	Transaction(context.Context, string, func(context.Context) error) error
	LoadHandoff(context.Context, *v1.Ref) (*v1.Handoff, error)
	SaveCapability(context.Context, *v1.Capability) error
	LoadCapability(context.Context, *v1.Ref) (*v1.Capability, error)
	CapabilityRefs(context.Context) ([]*v1.Ref, error)
	SaveAdmission(context.Context, *v1.Admission) error
	LoadAdmission(context.Context, *v1.Ref) (*v1.Admission, error)
	SaveHandoff(context.Context, *v1.Handoff) error

	SavePlanning(context.Context, *v1.PlanningState) error
	LoadPlanning(context.Context, *v1.GlobalName) (*v1.PlanningState, error)
	SaveSnapshot(context.Context, *v1.ContextSnapshot) error
	LoadSnapshot(context.Context, *v1.Ref) (*v1.ContextSnapshot, error)
	Position(context.Context) (uint64, int64, error)
	SaveTask(context.Context, *v1.Task) error
	LoadTask(context.Context, *v1.GlobalName) (*v1.Task, error)
}
type Service struct {
	completionJobs        CompletionJobs
	completionCloser      CompletionCloser
	completionFacts       CompletionFacts
	completionBudget      CompletionBudget
	progressSource        OperationProgressSource
	closureSource         ClosureSource
	confirmationPublisher ConfirmationPublisher
	confirmationContent   ConfirmationContent
	startGrants           StartGrants
	startBudget           StartBudget
	startExecution        StartExecutionFacts
	decisions             Decisions
	grants                Grants
	budget                Budget
	content               Content
	confirmations         Confirmations
	scheduling            Scheduling
	execution             ExecutionFacts
	handoffJobs           HandoffJobs
	recipient             Recipient
	store                 Store
	user, domain          string
}

func New(s Store, user, domain string) *Service {
	return &Service{store: s, user: user, domain: domain}
}

// CreateInTransaction 只能由受信裁决事务参与者调用。
func (s *Service) CreateInTransaction(ctx context.Context, goal *v1.Ref) (*v1.Ref, error) {
	ref := command.NewRef(s.user, s.domain, "task", "lerna.v1.Task")
	task := &v1.Task{TaskId: ref.Name, Revision: 1, GoalRef: goal, OwnerDomainId: s.domain, RequirementsVersion: 1, InputVersion: 1, Lifecycle: v1.TaskLifecycle_TASK_LIFECYCLE_OPEN, Control: v1.TaskControl_TASK_CONTROL_ACTIVE, Progress: v1.TaskProgress_TASK_PROGRESS_WAITING, WaitingOn: []string{"REQUIREMENTS"}, RequirementsStatus: v1.RequirementsStatus_REQUIREMENTS_STATUS_DRAFT, ControlGeneration: 1, PlanningGeneration: 1}
	if err := s.store.SaveTask(ctx, task); err != nil {
		return nil, err
	}
	return ref, nil
}
func (s *Service) QueryTask(ctx context.Context, caller *v1.Caller, id *v1.GlobalName) (*v1.Task, error) {
	if err := command.CheckName(caller, id, s.user, s.domain, "task"); err != nil {
		return nil, err
	}
	return s.store.LoadTask(ctx, id)
}
