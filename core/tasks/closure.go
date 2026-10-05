package tasks

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

// ClosureSource 从负责方的已保存意图核验用途，不采信调用者自报分类。
type ClosureSource interface {
	ValidateClosureWork(context.Context, *v1.Caller, *v1.Ref) (*v1.ClosureWorkRequest, error)
	QueryClosureWork(context.Context, *v1.Caller, *v1.Ref) (*v1.ClosureWorkRequest, error)
}

func (s *Service) WithClosureSource(source ClosureSource) *Service {
	s.closureSource = source
	return s
}

func (s *Service) AdmitClosure(ctx context.Context, caller *v1.Caller, c *v1.AdmitClosureCommand) (*v1.CommandReceipt, error) {
	if e := command.ValidateHeader(c.GetHeader(), c); e != nil {
		return nil, e
	}
	return s.decisions.Execute(ctx, caller, c.Header, command.SemanticFingerprint("admit-closure", c.WorkRef, c.ConfirmationRef), "tasks.closure_admit", func(tx context.Context) (*v1.Ref, error) {
		if s.closureSource == nil {
			return nil, command.Fail("DEPENDENCY_UNAVAILABLE")
		}
		w, e := s.closureSource.ValidateClosureWork(tx, caller, c.WorkRef)
		if e != nil {
			return nil, e
		}
		if w == nil || !proto.Equal(w.AdmissionIdentity, c.Header.Identity) || w.Purpose != "RECONCILE" || w.QuerySubject == nil {
			return nil, command.Fail("INVALID_CLOSURE_ORIGIN")
		}
		task, e := s.QueryTask(tx, caller, w.TaskId)
		if e != nil {
			return nil, e
		}
		if task == nil {
			return nil, command.Fail("NOT_FOUND")
		}
		if task.ParentTaskRef != nil {
			return nil, command.Fail("UNSUPPORTED_FEATURE")
		}
		p, e := s.store.LoadPlanning(tx, task.TaskId)
		if e != nil {
			return nil, e
		}
		if p == nil {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		cap, e := s.QueryCapability(tx, caller, w.CapabilityRef)
		if e != nil {
			return nil, e
		}
		if cap == nil || cap.Action != "QUERY" || cap.UseRight != "READ" || cap.Resource != w.QuerySubject.TargetScope || cap.ExecutorEndpointId != w.QuerySubject.ExecutorEndpointId {
			return nil, command.Fail("CAPABILITY_INVALID")
		}
		ref := command.NewRef(s.user, s.domain, "admission", "lerna.v1.Admission")
		op := command.NewRef(s.user, s.domain+"/ledger", "operation", "lerna.v1.Operation")
		a := &v1.Admission{Ref: ref, Origin: w.Ref, StepId: w.Ref.Name.LocalId, TaskId: task.TaskId, RequirementsVersion: task.RequirementsVersion, InputVersion: task.InputVersion, ControlGeneration: task.ControlGeneration, OperationId: op.Name, LedgerDomainId: s.domain + "/ledger", ExecutorEndpointId: cap.ExecutorEndpointId, ParametersRef: w.ParametersRef, CapabilityRef: cap.Ref, CapabilitySnapshot: cap, WorkCategory: "CLOSURE", QuerySubject: w.QuerySubject, HandoffIdentity: &v1.CommandIdentity{UserId: s.user, IssuerId: "tasks-handoff", TargetDomainId: s.domain + "/ledger", CommandId: op.Name.LocalId}}
		use, required, e := s.grants.OccupyInTransaction(tx, w.GrantRef, task.TaskId, op.Name, ref, cap, w.ParametersRef)
		if e != nil {
			return nil, e
		}
		a.GrantRefs = []*v1.Ref{use.GrantRef}
		a.GrantUseRef = use.Ref
		a.BudgetBasis, e = s.budget.ReserveInTransaction(tx, task.TaskId, op.Name, ref, cap)
		if e != nil {
			return nil, e
		}
		a.ConfirmationRef = c.ConfirmationRef
		if e = s.confirmations.ConsumeAdmissionConfirmation(tx, c.ConfirmationRef, a, required); e != nil {
			return nil, e
		}
		for _, r := range []*v1.Ref{w.ParametersRef, cap.RateBasisRef} {
			if e = s.content.CheckUsable(tx, caller, r); e != nil {
				return nil, e
			}
		}
		if e = s.store.SaveAdmission(tx, a); e != nil {
			return nil, e
		}
		job, e := s.scheduling.EnqueueHandoffInTransaction(tx, a)
		if e != nil {
			return nil, e
		}
		h := &v1.Handoff{Ref: command.NewRef(s.user, s.domain, "handoff", "lerna.v1.Handoff"), AdmissionRef: ref, Identity: a.HandoffIdentity, JobRef: job, State: "PENDING"}
		if e = s.store.SaveHandoff(tx, h); e != nil {
			return nil, e
		}
		p.AdmissionRefs = append(p.AdmissionRefs, ref)
		return ref, s.store.SavePlanning(tx, p)
	})
}
func (s *Service) validateClosureAdmission(ctx context.Context, caller *v1.Caller, a *v1.Admission) error {
	if s.closureSource == nil {
		return command.Fail("DEPENDENCY_UNAVAILABLE")
	}
	w, e := s.closureSource.ValidateClosureWork(ctx, caller, a.Origin)
	if e != nil {
		return e
	}
	if w == nil || w.Purpose != "RECONCILE" || !proto.Equal(w.TaskId, a.TaskId) || !proto.Equal(w.CapabilityRef, a.CapabilityRef) || !proto.Equal(w.ParametersRef, a.ParametersRef) || !proto.Equal(w.QuerySubject, a.QuerySubject) || len(a.GrantRefs) != 1 || !proto.Equal(w.GrantRef, a.GrantRefs[0]) || a.CapabilitySnapshot.Action != "QUERY" || a.CapabilitySnapshot.UseRight != "READ" {
		return command.Fail("INVALID_CLOSURE_ORIGIN")
	}
	return nil
}

func (s *Service) QueryClosureReceipt(ctx context.Context, c *v1.Caller, id *v1.CommandIdentity) (*v1.ReceiptQuery, error) {
	return s.QueryStartReceipt(ctx, c, id)
}
