package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
)

// TraceSource 只参加本模块已有的权威事务；不能向运行记录域直接写入。
type TraceSource interface {
	SaveTraceSource(context.Context, string, *v1.TraceEvent) error
}

func (s *Service) saveTask(ctx context.Context, task *v1.Task) error {
	if err := s.store.SaveTask(ctx, task); err != nil {
		return err
	}
	source := s.store
	kind := "TASK_CHANGED"
	if task.Control == v1.TaskControl_TASK_CONTROL_CANCELLING {
		kind = "TASK_CANCEL_REQUESTED"
	}
	if task.Control == v1.TaskControl_TASK_CONTROL_PAUSED {
		kind = "TASK_PAUSED"
	}
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: kind, TaskId: task.TaskId, SourceRecordRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, RequirementsVersion: task.RequirementsVersion, RelatedRefs: []*v1.Ref{task.GoalRef, task.ResultRef}})
}

func (s *Service) saveRequirements(ctx context.Context, v *v1.Requirements) error {
	if err := s.store.SaveRequirements(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "REQUIREMENTS_ACCEPTED", SourceRecordRef: v.Ref, TaskId: v.TaskId, RequirementsVersion: v.RequirementsVersion, RelatedRefs: append([]*v1.Ref{v.SourceInputRef}, conditionTraceRefs(v.Conditions)...), OriginCommand: v.AcceptedBy})
}

func (s *Service) saveProposal(ctx context.Context, v *v1.Proposal) error {
	if err := s.store.SaveProposal(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "PROPOSAL_RECEIVED", SourceRecordRef: v.Ref, TaskId: v.TaskId, RequirementsVersion: v.RequirementsVersion, BodyRef: v.BodyContentRef, RelatedRefs: proposalTraceRefs(v)})
}

func (s *Service) saveSnapshot(ctx context.Context, v *v1.ContextSnapshot) error {
	if err := s.store.SaveSnapshot(ctx, v); err != nil {
		return err
	}
	source := s.store
	refs := append([]*v1.Ref{v.TaskRef, v.RequestRef, v.RequirementsRef}, v.ContentRefs...)
	refs = append(refs, v.ProgressWatermarks...)
	for _, confirmation := range v.Confirmations {
		refs = append(refs, confirmation.Ref, confirmation.RequirementsRef, confirmation.ProposalRef)
		refs = append(refs, confirmation.EvidenceRefs...)
	}
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "PROPOSAL_REQUESTED", SourceRecordRef: v.Ref, TaskId: v.TaskRef.Name, RequirementsVersion: v.RequirementsVersion, RelatedRefs: refs})
}

func (s *Service) saveAdmission(ctx context.Context, v *v1.Admission) error {
	if err := s.store.SaveAdmission(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "ADMISSION_ACCEPTED", OriginCommand: v.HandoffIdentity, SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.OperationId, RequirementsVersion: v.RequirementsVersion, RelatedRefs: append([]*v1.Ref{v.Origin, v.GrantUseRef, v.ConfirmationRef, v.BudgetBasis.ReservationRef}, v.GrantRefs...)})
}

func (s *Service) saveVerification(ctx context.Context, v *v1.Verification) error {
	if err := s.store.(completionStore).SaveVerification(ctx, v); err != nil {
		return err
	}
	source := s.store
	refs := append([]*v1.Ref{v.RequirementsRef, v.ProposalRef, v.ContinuationRequestRef}, v.AdmissionRefs...)
	for _, candidate := range v.Candidates {
		refs = append(refs, candidate.ConfirmationRef)
	}
	refs = append(refs, findingTraceRefs(v.Conditions)...)
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "VERIFICATION_CHANGED", SourceRecordRef: v.Ref, TaskId: v.TaskId, RelatedRefs: refs})
}

func (s *Service) saveResult(ctx context.Context, v *v1.Result) error {
	if err := s.store.(completionStore).SaveResult(ctx, v); err != nil {
		return err
	}
	source := s.store
	refs := append([]*v1.Ref{v.VerificationRef, v.RequirementsRef, v.TaskClosingRef}, v.OperationRefs...)
	refs = append(refs, v.ExecutionFollowupRefs...)
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "RESULT_FIXED", SourceRecordRef: v.Ref, TaskId: v.TaskId, RelatedRefs: append(refs, findingTraceRefs(v.Conditions)...)})
}

type observedDecisions interface {
	ExecuteObserved(context.Context, *v1.Caller, *v1.CommandHeader, string, string, func(context.Context) (*v1.Ref, error), func(context.Context, *v1.CommandReceipt) error) (*v1.CommandReceipt, error)
}
type traceDecisions struct {
	service *Service
	task    *v1.GlobalName
	request *v1.Ref
}

func (s *Service) traceDecisions(task *v1.GlobalName) Decisions {
	return &traceDecisions{service: s, task: task}
}
func (s *Service) traceModelDecisions(request *v1.Ref) Decisions {
	return &traceDecisions{service: s, request: request}
}
func (d *traceDecisions) Execute(ctx context.Context, caller *v1.Caller, h *v1.CommandHeader, fingerprint, point string, fn func(context.Context) (*v1.Ref, error)) (*v1.CommandReceipt, error) {
	observed, ok := d.service.decisions.(observedDecisions)
	if !ok {
		return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	return observed.ExecuteObserved(ctx, caller, h, fingerprint, point, fn, func(tx context.Context, r *v1.CommandReceipt) error {
		if r.Decision != v1.Decision_DECISION_REJECTED {
			return nil
		}
		source := d.service.store
		event := &v1.TraceEvent{EventType: "DECISION_REJECTED", OriginCommand: r.Identity, SourceRecordRef: r.DecisionRef, ReasonCode: r.GetError().GetCode()}

		taskID := d.task
		if d.request != nil && command.CheckName(caller, d.request.Name, d.service.user, d.service.domain, "proposal-request") == nil {
			request, err := d.service.store.(modelStore).LoadProposalRequest(tx, d.request)
			if err != nil {
				return err
			}
			if request != nil {
				taskID = request.TaskId
				event.RelatedRefs = []*v1.Ref{request.Ref, request.SnapshotRef}
			}
		}
		if command.CheckName(caller, taskID, d.service.user, d.service.domain, "task") == nil {
			task, err := d.service.store.LoadTask(tx, taskID)
			if err != nil {
				return err
			}
			if task != nil {
				event.TaskId = task.TaskId
				event.RequirementsVersion = task.RequirementsVersion
			}
		}
		return source.SaveTraceSource(tx, "tasks", event)
	})
}

func (s *Service) saveHandoff(ctx context.Context, v *v1.Handoff) error {
	if err := s.store.SaveHandoff(ctx, v); err != nil {
		return err
	}
	source := s.store
	a, err := s.store.LoadAdmission(ctx, v.AdmissionRef)
	if err != nil {
		return err
	}
	refs := []*v1.Ref{v.AdmissionRef, v.JobRef}
	if v.RecipientReceipt != nil {
		refs = append(refs, v.RecipientReceipt.DecisionRef)
	}
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "HANDOFF_CHANGED", SourceRecordRef: v.Ref, TaskId: a.TaskId, OperationId: a.OperationId, OriginCommand: v.Identity, RelatedRefs: refs})
}
func (s *Service) saveStart(ctx context.Context, v *v1.StartRecord) error {
	if err := s.store.(startStore).SaveStart(ctx, v); err != nil {
		return err
	}
	source := s.store
	return source.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "START_ACCEPTED", SourceRecordRef: v.Ref, TaskId: v.Binding.TaskId, OperationId: v.Binding.OperationId, AttemptId: v.Binding.AttemptId, SendRef: v.SendRef, OriginCommand: v.Identity, RequirementsVersion: v.Binding.RequirementsVersion, RelatedRefs: []*v1.Ref{v.Binding.AdmissionRef, v.CredentialRef, v.Binding.GrantUseRef, v.Binding.BudgetReservationRef}})
}

func (s *Service) saveCompletionIntent(ctx context.Context, v *v1.CompletionClosureIntent) error {
	if err := s.store.(completionIntentStore).SaveCompletionIntent(ctx, v); err != nil {
		return err
	}
	refs := []*v1.Ref{v.Command.VerificationRef, v.Command.AdmissionRef, v.JobRef}
	if v.RecipientReceipt != nil {
		refs = append(refs, v.RecipientReceipt.DecisionRef)
	}
	return s.store.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "COMPLETION_HANDOFF_CHANGED", SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.Command.OperationId, OriginCommand: v.Command.Header.Identity, RelatedRefs: refs})
}

func (s *Service) saveCancellationIntent(ctx context.Context, v *v1.CancellationClosureIntent) error {
	if err := s.store.(cancellationStore).SaveCancellationIntent(ctx, v); err != nil {
		return err
	}
	kind := "CANCELLATION_CLOSURE_REQUESTED"
	refs := []*v1.Ref{v.Command.CancellationRef, v.Command.AdmissionRef, v.JobRef}
	if v.RecipientReceipt != nil {
		kind = "CANCELLATION_CLOSURE_ACKNOWLEDGED"
		refs = append(refs, v.RecipientReceipt.DecisionRef, v.RecipientReceipt.ResultRef)
	}
	return s.store.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: kind, SourceRecordRef: v.Ref, TaskId: v.TaskId, OperationId: v.Command.OperationId, OriginCommand: v.Command.Header.Identity, RelatedRefs: refs})
}

func (s *Service) saveTaskOperationProgress(ctx context.Context, n *v1.OperationProgressNotice) error {
	if err := s.store.(progressStore).SaveTaskOperationProgress(ctx, n); err != nil {
		return err
	}
	return s.store.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "PROGRESS_ACCEPTED", SourceRecordRef: n.Ref, TaskId: n.TaskId, OperationId: n.OperationRef.Name, OriginCommand: n.Identity, ObservationRef: n.LastObservationRef, ReasonCode: n.PauseReason, RelatedRefs: []*v1.Ref{n.OperationRef, n.ReconciliationRef, n.EffectRef}})
}

// saveModelRequest 将模型请求状态与原责任引用写入同一源事务。
func (s *Service) saveModelRequest(ctx context.Context, r *v1.ProposalRequest) error {
	if err := s.store.(modelStore).SaveProposalRequest(ctx, r); err != nil {
		return err
	}
	refs := append([]*v1.Ref{r.SnapshotRef, r.JobRef}, r.ModelOperationRefs...)
	refs = append(refs, r.OutcomeRefs...)
	return s.store.SaveTraceSource(ctx, "tasks", &v1.TraceEvent{EventType: "PROPOSAL_REQUEST_" + r.State, SourceRecordRef: r.Ref, TaskId: r.TaskId, OriginCommand: r.OutcomeIdentity, RelatedRefs: refs})
}

func (s *Service) saveModelCall(ctx context.Context, call *v1.ModelCall) error {
	if err := s.store.(modelCallStore).SaveModelCall(ctx, call); err != nil {
		return err
	}
	request, err := s.store.(modelStore).LoadProposalRequest(ctx, call.RequestRef)
	if err != nil {
		return err
	}
	if request == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	refs := []*v1.Ref{call.RequestRef, request.SnapshotRef, call.DerivationRef, call.InputRef, call.AdmissionRef, call.CapabilityRef}
	refs = append(refs, call.InputRefs...)
	ev := &v1.TraceEvent{EventType: "MODEL_CALL_" + call.State, SourceRecordRef: call.Ref, TaskId: request.TaskId, RelatedRefs: refs}
	if call.AdmissionRef != nil {
		a, e := s.store.LoadAdmission(ctx, call.AdmissionRef)
		if e != nil {
			return e
		}
		if a == nil {
			return command.Fail("INVARIANT_VIOLATION")
		}
		ev.OperationId = a.OperationId
		ev.RequirementsVersion = a.RequirementsVersion
	}
	if result := call.Result; result != nil {
		ev.EventType = "MODEL_RESULT_" + result.Status
		ev.ObservationRef = result.ObservationRef
		ev.OperationId = result.OperationId
		ev.RelatedRefs = append(ev.RelatedRefs, result.OutputRef, result.OutputDerivationRef, result.UsageRef, result.InputRef)
	}
	return s.store.SaveTraceSource(ctx, "tasks", ev)
}

func (s *Service) saveModelOutcome(ctx context.Context, outcome *v1.ProposalOutcome) error {
	if err := s.store.(outcomeStore).SaveProposalOutcome(ctx, outcome); err != nil {
		return err
	}
	request, err := s.store.(modelStore).LoadProposalRequest(ctx, outcome.RequestRef)
	if err != nil {
		return err
	}
	if request == nil {
		return command.Fail("INVARIANT_VIOLATION")
	}
	kind := "PROPOSAL_OUTCOME_RETAINED"
	if outcome.AcceptedForProgress {
		kind = "PROPOSAL_OUTCOME_ACCEPTED"
	}
	reason := outcome.ReasonCode
	if reason == "" {
		reason = outcome.ErrorCode
	}
	refs := []*v1.Ref{outcome.RequestRef, request.SnapshotRef, outcome.ModelCallRef, outcome.OutputRef, outcome.UsageRef, outcome.ProposalRef, outcome.Claim.GetRef()}
	if outcome.Proposal != nil {
		refs = append(refs, proposalTraceRefs(outcome.Proposal)...)
	}
	ev := &v1.TraceEvent{EventType: kind, SourceRecordRef: outcome.Ref, TaskId: request.TaskId, OriginCommand: outcome.Identity, ReasonCode: reason, BodyRef: outcome.GetProposal().GetBodyContentRef(), RelatedRefs: refs}
	if outcome.ModelCallRef != nil {
		call, e := s.store.(modelCallStore).LoadModelCallRef(ctx, outcome.ModelCallRef)
		if e != nil {
			return e
		}
		if call == nil {
			return command.Fail("INVARIANT_VIOLATION")
		}
		ev.OperationId = call.GetResult().GetOperationId()
	}
	return s.store.SaveTraceSource(ctx, "tasks", ev)
}

func conditionTraceRefs(conditions []*v1.Requirement) []*v1.Ref {
	var refs []*v1.Ref
	for _, condition := range conditions {
		refs = append(refs, condition.DescriptionRef)
		if condition.TargetRecord != nil {
			refs = append(refs, condition.TargetRecord.CapabilityRef, condition.TargetRecord.ParametersRef)
		}
	}
	return refs
}

func findingTraceRefs(findings []*v1.ConditionFinding) []*v1.Ref {
	var refs []*v1.Ref
	for _, finding := range findings {
		refs = append(refs, finding.SourceInputRef, finding.OperationRef)
		if finding.Condition != nil {
			refs = append(refs, conditionTraceRefs([]*v1.Requirement{finding.Condition})...)
		}
		refs = append(refs, finding.EvidenceRefs...)
	}
	return refs
}

// proposalTraceRefs 只投影提议原载荷中的结构引用，不读取规范正文。
func proposalTraceRefs(p *v1.Proposal) []*v1.Ref {
	refs := append([]*v1.Ref{p.ContextSnapshotRef, p.RequestRef, p.ReasonerRef, p.BodyContentRef}, p.BasisRefs...)
	if p.Step != nil {
		refs = append(refs, p.Step.CapabilityRef, p.Step.ParametersRef)
		refs = append(refs, p.Step.ContentRefs...)
		refs = append(refs, p.Step.Dependencies...)
	}
	if p.RequirementsChange != nil {
		refs = append(refs, conditionTraceRefs(p.RequirementsChange.Conditions)...)
	}
	for _, verdict := range p.Verdicts {
		refs = append(refs, verdict.ConfirmationRef)
		refs = append(refs, verdict.EvidenceRefs...)
	}
	for _, evidence := range p.CompletionEvidence {
		refs = append(refs, evidence.ConfirmationRef)
	}
	return refs
}
