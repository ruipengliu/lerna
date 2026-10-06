package budget

import (
	"context"

	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type settlementFollowupStore interface {
	SaveSettlementFollowup(context.Context, *v1.SettlementFollowup) error
	LoadSettlementFollowup(context.Context, *v1.Ref) (*v1.SettlementFollowup, error)
	SaveJob(context.Context, *v1.Job) error
	BillingSourcesForOperation(context.Context, *v1.GlobalName) ([]*v1.BillingSource, error)
	AllSettlementFollowups(context.Context) ([]*v1.SettlementFollowup, error)
	LoadJob(context.Context, *v1.GlobalName) (*v1.Job, error)
	Transaction(context.Context, string, func(context.Context) error) error
}

// RetainSettlementInTransaction 在原关闭裁决中保留预算自己的全部逐发送责任。
func (s *Service) RetainSettlementInTransaction(ctx context.Context, c *v1.Caller, closing *v1.TaskClosing, a *v1.Admission) (*v1.SettlementFollowup, error) {
	if !s.trustedCaller(c) && c.GetIssuerId() != "host-recovery" {
		return nil, command.Fail("PERMISSION_DENIED")
	}
	if closing == nil || a == nil || !proto.Equal(closing.TaskId, a.TaskId) {
		return nil, command.Fail("INVALID_CLOSE_BASIS")
	}
	all, e := s.QueryReservations(ctx, c, a.TaskId)
	if e != nil {
		return nil, e
	}
	f := &v1.SettlementFollowup{Ref: command.NewRef(s.user, s.domain, "settlement-followup", "lerna.v1.SettlementFollowup"), TaskId: a.TaskId, OperationId: a.OperationId, AdmissionRef: a.Ref, TaskClosingRef: closing.Ref, JobRef: command.NewRef(s.user, s.domain, "job", "lerna.v1.Job")}
	for _, r := range all {
		if !proto.Equal(r.OperationId, a.OperationId) {
			continue
		}
		if !proto.Equal(r.AdmissionRef, a.Ref) {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		f.ReservationRefs = append(f.ReservationRefs, r.Ref)
	}
	if len(f.ReservationRefs) == 0 {
		return nil, command.Fail("INVARIANT_VIOLATION")
	}
	store := s.store.(settlementFollowupStore)
	sources, e := store.BillingSourcesForOperation(ctx, a.OperationId)
	if e != nil {
		return nil, e
	}
	for _, source := range sources {
		if !proto.Equal(source.TaskId, a.TaskId) || !proto.Equal(source.AdmissionRef, a.Ref) {
			return nil, command.Fail("INVARIANT_VIOLATION")
		}
		f.BillingSourceRefs = append(f.BillingSourceRefs, source.Ref)
	}
	j := &v1.Job{Ref: f.JobRef, Module: "budget", JobType: "SETTLE_CLOSED_TASK", ContractVersion: 1, Responsibility: closing.RequestedBy, SpecificationRef: f.Ref, PurposeKey: "settlement-followup:" + f.Ref.Name.LocalId, State: "WAITING", WaitingReason: "ORIGINAL_BILLING_EVIDENCE_REQUIRED"}
	if e = s.saveSettlementFollowupJob(ctx, f, j); e != nil {
		return nil, e
	}
	return f, s.saveSettlementFollowup(ctx, f)
}

func (s *Service) QuerySettlementFollowup(ctx context.Context, c *v1.Caller, r *v1.Ref) (*v1.SettlementFollowup, error) {
	if r == nil || r.Revision != 1 || r.SchemaId != "lerna.v1.SettlementFollowup" {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e := command.CheckName(c, r.Name, s.user, s.domain, "settlement-followup"); e != nil {
		return nil, e
	}
	f, e := s.store.(settlementFollowupStore).LoadSettlementFollowup(ctx, r)
	if e == nil && f != nil && !proto.Equal(f.Ref, r) {
		return nil, command.Fail("INVALID_REFERENCE")
	}
	if e != nil || f == nil {
		return f, e
	}
	if _, _, e = s.settlementFollowupScope(ctx, c, f); e != nil {
		return nil, e
	}
	return f, nil
}

func (s *Service) settlementFollowupScope(ctx context.Context, c *v1.Caller, f *v1.SettlementFollowup) (*v1.Job, bool, error) {
	closing, e := s.taskClosingAuthority.QueryTaskClosing(ctx, c, f.TaskClosingRef)
	if e != nil {
		return nil, false, e
	}
	a, e := s.taskClosingAuthority.QueryAdmission(ctx, c, f.AdmissionRef)
	if e != nil {
		return nil, false, e
	}
	listed := false
	for _, ref := range closing.GetSettlementFollowupRefs() {
		if proto.Equal(ref, f.Ref) {
			listed = true
		}
	}
	if !listed || a == nil || !proto.Equal(a.TaskId, f.TaskId) || !proto.Equal(closing.TaskId, f.TaskId) || !proto.Equal(a.OperationId, f.OperationId) {
		return nil, false, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
	}
	job, e := s.store.(settlementFollowupStore).LoadJob(ctx, f.JobRef.Name)
	if e != nil {
		return nil, false, e
	}
	if job == nil || job.Module != "budget" || job.JobType != "SETTLE_CLOSED_TASK" || job.ContractVersion != 1 || len(job.ProtoReflect().GetUnknown()) != 0 || !proto.Equal(job.SpecificationRef, f.Ref) || !proto.Equal(job.Responsibility, closing.RequestedBy) || (job.State != "WAITING" && job.State != "COMPLETED") {
		return nil, false, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
	}
	all, e := s.QueryReservations(ctx, c, f.TaskId)
	if e != nil {
		return nil, false, e
	}
	reservations := map[string]*v1.Reservation{}
	ready := true
	for _, r := range all {
		if !proto.Equal(r.OperationId, f.OperationId) {
			continue
		}
		if !proto.Equal(r.AdmissionRef, f.AdmissionRef) || !proto.Equal(r.TaskId, f.TaskId) || r.Unit != a.BudgetBasis.Unit || r.SendCeiling != 1 || r.ConsumedSends > 1 {
			return nil, false, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
		}
		reservations[r.Ref.Name.LocalId] = r
		if r.Status != "SETTLED" && r.Status != "UNUSED_CLOSED" {
			ready = false
		}
	}
	if len(reservations) != len(f.ReservationRefs) || len(reservations) == 0 {
		return nil, false, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
	}
	seen := map[string]bool{}
	for _, ref := range f.ReservationRefs {
		original, e := s.QueryReservation(ctx, c, ref)
		if e != nil {
			return nil, false, e
		}
		r := reservations[ref.GetName().GetLocalId()]
		if original == nil || r == nil || seen[ref.Name.LocalId] || !proto.Equal(original.Ref.Name, r.Ref.Name) || !proto.Equal(original.OperationId, f.OperationId) || !proto.Equal(original.AdmissionRef, f.AdmissionRef) || !proto.Equal(original.TaskId, f.TaskId) {
			return nil, false, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
		}
		seen[ref.Name.LocalId] = true
	}
	sources, e := s.store.(settlementFollowupStore).BillingSourcesForOperation(ctx, f.OperationId)
	if e != nil {
		return nil, false, e
	}
	if len(sources) != len(f.BillingSourceRefs) {
		return nil, false, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
	}
	counts := map[string]uint32{}
	seen = map[string]bool{}
	for _, source := range sources {
		r := reservations[source.ReservationRef.GetName().GetLocalId()]
		listed = false
		for _, ref := range f.BillingSourceRefs {
			if !proto.Equal(ref.Name, source.Ref.Name) {
				continue
			}
			original, e := s.QueryBillingSourceVersion(ctx, c, ref)
			if e != nil {
				return nil, false, e
			}
			listed = original != nil && proto.Equal(original.SendRef, source.SendRef) && proto.Equal(original.ReservationRef, source.ReservationRef) && proto.Equal(original.AdmissionRef, f.AdmissionRef) && proto.Equal(original.TaskId, f.TaskId) && proto.Equal(original.OperationId, f.OperationId)
		}
		if !listed || r == nil || seen[source.Ref.Name.LocalId] || !proto.Equal(source.OperationId, f.OperationId) || !proto.Equal(source.AdmissionRef, f.AdmissionRef) || !proto.Equal(source.TaskId, f.TaskId) || source.Unit != r.Unit {
			return nil, false, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
		}
		seen[source.Ref.Name.LocalId] = true
		counts[r.Ref.Name.LocalId]++
		if source.Status != "SETTLED" && source.Status != "UNUSED_CLOSED" {
			ready = false
		}
	}
	for id, r := range reservations {
		if counts[id] != r.ConsumedSends {
			return nil, false, command.Fail("FOLLOWUP_RESPONSIBILITY_MISSING")
		}
	}
	return job, ready, nil
}

// ProcessSettlementFollowups 由预算自己的原预留与计费来源裁决责任是否已结清。
func (s *Service) ProcessSettlementFollowups(ctx context.Context, c *v1.Caller) error {
	if e := command.CheckCaller(c, s.user); e != nil {
		return e
	}
	store := s.store.(settlementFollowupStore)
	all, e := store.AllSettlementFollowups(ctx)
	if e != nil {
		return e
	}
	for _, f := range all {
		if e = store.Transaction(ctx, "budget.followup_completion", func(tx context.Context) error {
			job, ready, e := s.settlementFollowupScope(tx, c, f)
			if e != nil {
				return e
			}
			state, reason := "COMPLETED", ""
			if !ready {
				state, reason = "WAITING", "ORIGINAL_BILLING_EVIDENCE_REQUIRED"
				sources, e := store.BillingSourcesForOperation(tx, f.OperationId)
				if e != nil {
					return e
				}
				for _, source := range sources {
					if source.Status == "CONFLICT" {
						reason = "BILLING_CONFLICT"
					}
				}
			}
			if job.State == state && job.WaitingReason == reason {
				return nil
			}
			job.Ref.Revision++
			job.State = state
			job.WaitingReason = reason
			return s.saveSettlementFollowupJob(tx, f, job)
		}); e != nil {
			return e
		}
	}
	return nil
}
