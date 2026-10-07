package ledger

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type executionFollowupStore interface {
	SaveExecutionFollowup(context.Context, *v1.ExecutionFollowup) error
	LoadExecutionFollowup(context.Context, *v1.Ref) (*v1.ExecutionFollowup, error)
	AllExecutionFollowups(context.Context) ([]*v1.ExecutionFollowup, error)
}

func (s *Service) retainExecutionFollowup(ctx context.Context, a *v1.Admission, op *v1.Operation, closing *v1.Ref, id *v1.CommandIdentity) (*v1.ExecutionFollowup, error) {
	f := &v1.ExecutionFollowup{Ref: command.NewRef(s.user, s.domain, "execution-followup", "lerna.v1.ExecutionFollowup"), TaskId: a.TaskId, OperationId: op.Ref.Name, AdmissionRef: a.Ref, TaskClosingRef: closing, OperationRef: proto.Clone(op.Ref).(*v1.Ref), ExecutorEndpointId: op.ExecutorEndpointId, JobRef: command.NewRef(s.user, s.domain, "job", "lerna.v1.Job"), WaitingReason: "ORIGINAL_EVIDENCE_REQUIRED"}
	if op.Execution != nil {
		f.AttemptRef = proto.Clone(op.Execution.Attempt.Ref).(*v1.Ref)
		for _, send := range append([]*v1.PhysicalSend{op.Execution.Send}, op.Execution.PreviousSends...) {
			f.SendRefs = append(f.SendRefs, proto.Clone(send.Ref).(*v1.Ref))
		}
		if !op.Execution.Attempt.Capabilities.Queryable {
			f.WaitingReason = "MANUAL_REVIEW_REQUIRED"
		} else {
			f.WaitingReason = "ORIGINAL_QUERY_AUTHORITY_REQUIRED"
		}
	}
	j := &v1.Job{Ref: f.JobRef, Module: "ledger", JobType: "RECONCILE_UNRESOLVED_OPERATION", ContractVersion: 1, Responsibility: id, State: "WAITING", WaitingReason: f.WaitingReason, PurposeKey: "execution-followup:" + f.Ref.Name.LocalId, SpecificationRef: op.Ref, ExecutorEndpointId: op.ExecutorEndpointId, LedgerDomainId: s.domain}
	if e := s.saveExecutionFollowupJob(ctx, f, j); e != nil {
		return nil, e
	}
	return f, s.saveExecutionFollowup(ctx, f)
}

// QueryExecutionFollowup 返回原负责方保存的责任范围；查询计划仍用原 Reconciliation 身份。
func (s *Service) QueryExecutionFollowup(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.ExecutionFollowup, error) {
	if e := s.checkHistory(c, r, "execution-followup", "lerna.v1.ExecutionFollowup"); e != nil {
		return nil, e
	}
	f, e := s.store.LoadExecutionFollowup(ctx, r)
	if e == nil && f != nil && !proto.Equal(f.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e != nil || f == nil {
		return f, e
	}
	if _, e = s.executionFollowupJob(ctx, c, f); e != nil {
		return nil, e
	}
	return f, nil
}

func (s *Service) executionFollowupJob(ctx context.Context, c *v1.Caller, f *v1.ExecutionFollowup) (*v1.Job, error) {
	original, e := s.QueryOperationVersion(ctx, c, f.OperationRef)
	if e != nil {
		return nil, e
	}
	seal, e := s.store.TaskClosureSealForOperation(ctx, f.OperationId)
	if e != nil {
		return nil, e
	}
	if original == nil || seal == nil || !proto.Equal(original.Ref.Name, f.OperationId) || !proto.Equal(original.AdmissionRef, f.AdmissionRef) || original.ExecutorEndpointId != f.ExecutorEndpointId || original.Dispatch != "SEALED" || !proto.Equal(seal.OperationRef, f.OperationRef) || !proto.Equal(seal.ExecutionFollowupRef, f.Ref) || !proto.Equal(seal.TaskClosingRef, f.TaskClosingRef) {
		return nil, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
	}
	a, e := s.starts.QueryAdmission(ctx, c, f.AdmissionRef)
	if e != nil {
		return nil, e
	}
	if a == nil || !proto.Equal(a.TaskId, f.TaskId) || !proto.Equal(a.OperationId, f.OperationId) || a.ExecutorEndpointId != f.ExecutorEndpointId {
		return nil, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
	}
	if original.Execution == nil {
		if f.AttemptRef != nil || len(f.SendRefs) != 0 {
			return nil, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
		}
	} else {
		sends := append([]*v1.PhysicalSend{original.Execution.Send}, original.Execution.PreviousSends...)
		if !proto.Equal(f.AttemptRef, original.Execution.Attempt.Ref) || len(sends) != len(f.SendRefs) {
			return nil, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
		}
		for i, send := range sends {
			if !proto.Equal(send.Ref, f.SendRefs[i]) || !proto.Equal(send.AttemptId, f.AttemptRef.Name) {
				return nil, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
			}
		}
	}
	jobs, e := s.store.LedgerJobs(ctx, f.OperationId)
	if e != nil {
		return nil, e
	}
	expected := &v1.CommandIdentity{UserId: s.user, IssuerId: "tasks-closing", TargetDomainId: s.domain, CommandId: "seal:" + seal.IntentRef.Name.LocalId}
	for _, job := range jobs {
		if proto.Equal(job.Ref.Name, f.JobRef.Name) && job.Module == "ledger" && job.JobType == "RECONCILE_UNRESOLVED_OPERATION" && job.ContractVersion == 1 && len(job.ProtoReflect().GetUnknown()) == 0 && proto.Equal(job.SpecificationRef, f.OperationRef) && proto.Equal(job.Responsibility, expected) && job.ExecutorEndpointId == f.ExecutorEndpointId && job.LedgerDomainId == s.domain && (job.State == "WAITING" || job.State == "COMPLETED") {
			return job, nil
		}
	}
	return nil, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
}

// ProcessExecutionFollowups 只按原动作的当前权威事实完成等待，不发出新的目标请求。
func (s *Service) ProcessExecutionFollowups(ctx context.Context, c *v1.Caller) error {
	if e := command.CheckCaller(c, s.user); e != nil {
		return e
	}
	all, e := s.store.AllExecutionFollowups(ctx)
	if e != nil {
		return e
	}
	for _, f := range all {
		observed, e := s.QueryOperation(ctx, c, f.OperationId)
		if e != nil {
			return e
		}
		if observed == nil {
			return command.Fail("INVARIANT_VIOLATION")
		}
		actor := &v1.Caller{UserId: s.user, IssuerId: "ledger-followup"}
		header := reconcileHeader(s.user, actor.IssuerId, s.domain, "followup:"+f.Ref.Name.LocalId+":"+command.SemanticFingerprint("followup-version", observed.Ref))
		r, e := s.work.Execute(ctx, actor, header, command.SemanticFingerprint("execution-followup", f.Ref, observed.Ref), "ledger.followup_completion", func(tx context.Context) (*v1.Ref, error) {
			op, e := s.QueryOperation(tx, c, f.OperationId)
			if e != nil {
				return nil, e
			}
			if op == nil || !proto.Equal(op.AdmissionRef, f.AdmissionRef) || op.ExecutorEndpointId != f.ExecutorEndpointId {
				return nil, command.Fail("INVARIANT_VIOLATION")
			}
			job, e := s.executionFollowupJob(tx, c, f)
			if e != nil {
				return nil, e
			}
			state, reason := "COMPLETED", ""
			if op.Lifecycle != "SETTLED" || op.Dispatch != "SEALED" || op.Effect == nil || op.Effect.Outcome == "UNKNOWN" || op.Effect.LateEffect != "RULED_OUT" || op.Effect.EvidenceConflict {
				state, reason = "WAITING", f.WaitingReason
				if op.GetEffect().GetEvidenceConflict() {
					reason = "EVIDENCE_CONFLICT"
				}
			}
			if job.State == state && job.WaitingReason == reason {
				return job.Ref, nil
			}
			job.Ref.Revision++
			job.State = state
			job.WaitingReason = reason
			return job.Ref, s.saveExecutionFollowupJob(tx, f, job)
		})
		if e != nil {
			return e
		}
		if r.Error != nil {
			return &command.Failure{Detail: r.Error}
		}
	}
	return nil
}
