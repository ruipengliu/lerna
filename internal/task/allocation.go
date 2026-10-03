package task

import (
	"context"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) AllocateTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AllocateInput) (AllocationOutput, error) {
	if c.TargetID != in.AllocationID || !api.ValidID(in.AllocationID) || !api.ValidID(in.ReceiverID) {
		return AllocationOutput{}, invalid("invalid_allocation_identity")
	}
	if in.ParentTaskRef.OwnerID != tx.Scope().OwnerID || in.ParentTaskRef.TenantID != tx.Scope().TenantID {
		return AllocationOutput{}, api.E("forbidden", "parent_owner_mismatch")
	}
	t, e := getTask(ctx, tx, in.ParentTaskRef.ObjectID)
	if e != nil {
		return AllocationOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return AllocationOutput{}, e
	}
	if in.ParentTaskRef.Revision != t.Task.Revision {
		return AllocationOutput{}, api.E("revision_conflict", "parent_revision_changed")
	}
	if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
		return AllocationOutput{}, e
	}
	deadline, e := api.ParseTime(in.Deadline)
	if e != nil {
		return AllocationOutput{}, invalid("invalid_deadline")
	}
	parentDeadline, e := api.ParseTime(t.Task.Deadline)
	if e != nil {
		return AllocationOutput{}, e
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return AllocationOutput{}, e
	}
	if !now.Before(deadline) || deadline.After(parentDeadline) {
		return AllocationOutput{}, invalid("delegation_deadline_exceeded")
	}
	a := Allocation{AllocationID: in.AllocationID, Revision: 1, ParentTaskRef: in.ParentTaskRef, ReceiverID: in.ReceiverID, Limits: in.Limits, Deadline: in.Deadline, CommandRef: tx.Scope().Ref(c.CommandID, 1), State: "preparing"}
	source := api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: in.ReceiverID, ObjectID: in.AllocationID, Revision: 1}
	if _, e = s.reserveTx(ctx, tx, &t, "budget_allocation", source, in.Limits); e != nil {
		return AllocationOutput{}, e
	}
	if e = tx.Create(ctx, allocations, in.AllocationID, t.Task.TaskID, a); e != nil {
		return AllocationOutput{}, e
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return AllocationOutput{}, e
	}
	return AllocationOutput{AllocationRef: tx.Scope().Ref(in.AllocationID, 1), State: a.State}, nil
}

// ReceiveAllocation 从原父主动读取后接纳；读在Tx外，拒绝以通知中的金额代替父事实。
func (s *Service) ReceiveAllocation(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, ref api.ObjectRef) (IncomingAllocation, error) {
	if s.ports.Collaboration == nil {
		return IncomingAllocation{}, api.E("dependency_unavailable", "allocation_source_unavailable")
	}
	if ref.TenantID != scope.TenantID {
		return IncomingAllocation{}, api.E("forbidden", "cross_tenant_allocation")
	}
	a, e := s.ports.Collaboration.ReadAllocation(ctx, scope, ref)
	if e != nil {
		return IncomingAllocation{}, e
	}
	var out IncomingAllocation
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		var e error
		out, e = s.ReceiveAllocationTx(ctx, tx, auth, ref, a)
		return e
	})
	return out, err
}
func (s *Service) ReceiveAllocationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, ref api.ObjectRef, a Allocation) (IncomingAllocation, error) {
	if !auth.HasRole("service") && auth.SubjectID != ref.OwnerID {
		return IncomingAllocation{}, api.E("forbidden", "parent_identity_required")
	}
	if ref.TenantID != tx.Scope().TenantID || a.ParentTaskRef.TenantID != tx.Scope().TenantID || ref.OwnerID != a.ParentTaskRef.OwnerID || ref.ObjectID != a.AllocationID || a.ReceiverID != tx.Scope().OwnerID || ref.Revision > a.Revision {
		return IncomingAllocation{}, api.E("forbidden", "allocation_scope_mismatch")
	}
	if a.State != "preparing" && a.State != "open" {
		return IncomingAllocation{}, api.E("invalid_state", "allocation_closed")
	}
	if e := api.ValidateAmounts(a.Limits); e != nil {
		return IncomingAllocation{}, e
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return IncomingAllocation{}, e
	}
	deadline, e := api.ParseTime(a.Deadline)
	if e != nil || !now.Before(deadline) {
		return IncomingAllocation{}, api.E("expired", "allocation_expired")
	}
	id := incomingID(ref)
	var old IncomingAllocation
	_, e = tx.Get(ctx, incoming, id, &old)
	if e == nil {
		if old.Gate != "open" {
			return old, api.E("invalid_state", "allocation_closed")
		}
		if !api.Equal(old.Limits, a.Limits) || old.ParentTaskRef.ObjectID != a.ParentTaskRef.ObjectID {
			return old, api.E("idempotency_conflict", "digest_conflict")
		}
		return old, nil
	}
	if !api.IsCode(e, "not_found") {
		return old, e
	}
	old = IncomingAllocation{AllocationID: a.AllocationID, Revision: 1, ParentOwner: ref.OwnerID, ReceiverID: tx.Scope().OwnerID, ParentTaskRef: a.ParentTaskRef, Limits: a.Limits, Gate: "open", UsageRevision: 1, Cumulative: []api.Amount{}}
	for _, limit := range a.Limits {
		old.Cumulative = append(old.Cumulative, api.Amount{Unit: limit.Unit, Value: "0"})
	}
	if e = tx.Create(ctx, incoming, id, ref.OwnerID, old); e != nil {
		return old, e
	}
	return old, nil
}
func (s *Service) CloseAllocationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AllocationCloseInput) (AllocationOutput, error) {
	if in.AllocationRef.TenantID != tx.Scope().TenantID || in.ParentTaskRef.TenantID != tx.Scope().TenantID || in.ParentTaskRef.OwnerID != in.AllocationRef.OwnerID || c.TargetID != in.AllocationRef.ObjectID {
		return AllocationOutput{}, api.E("forbidden", "allocation_scope_mismatch")
	}
	if !auth.HasRole("service") && auth.SubjectID != in.AllocationRef.OwnerID {
		return AllocationOutput{}, api.E("forbidden", "parent_identity_required")
	}
	id := incomingID(in.AllocationRef)
	a, e := incomingForTaskTx(ctx, tx, id)
	rev := a.Revision
	if confirmedNotFound(e) {
		a = IncomingAllocation{AllocationID: in.AllocationRef.ObjectID, Revision: 1, ParentOwner: in.AllocationRef.OwnerID, ReceiverID: tx.Scope().OwnerID, ParentTaskRef: in.ParentTaskRef, Limits: []api.Amount{}, Gate: "closed", UsageRevision: 1, Cumulative: []api.Amount{}}
		now, e := tx.Now(ctx)
		if e != nil {
			return AllocationOutput{}, e
		}
		a.ClosedAt = api.Time(now)
		if e = tx.Create(ctx, incoming, id, in.AllocationRef.OwnerID, a); e != nil {
			return AllocationOutput{}, e
		}
		return AllocationOutput{AllocationRef: tx.Scope().Ref(in.AllocationRef.ObjectID, 1), State: "closed"}, nil
	}
	if e != nil {
		return AllocationOutput{}, e
	}
	if a.ParentTaskRef.ObjectID != in.ParentTaskRef.ObjectID || a.ParentOwner != in.ParentTaskRef.OwnerID || a.ReceiverID != tx.Scope().OwnerID {
		return AllocationOutput{}, api.E("forbidden", "allocation_scope_mismatch")
	}
	if a.Gate == "closed" {
		return AllocationOutput{AllocationRef: tx.Scope().Ref(a.AllocationID, a.Revision), State: a.Gate}, nil
	}
	a.Gate = "closing"
	a.Revision++
	if e = tx.Put(ctx, incoming, id, rev, a); e != nil {
		return AllocationOutput{}, e
	}
	if a.TaskRef != nil {
		t, e := getTask(ctx, tx, a.TaskRef.ObjectID)
		if e != nil {
			return AllocationOutput{}, e
		}
		if e = s.closeUnsent(ctx, tx, &t); e != nil {
			return AllocationOutput{}, e
		}
		if e = s.saveTask(ctx, tx, &t); e != nil {
			return AllocationOutput{}, e
		}
		if e = s.refreshIncomingTx(ctx, tx, t); e != nil {
			return AllocationOutput{}, e
		}
	}
	if _, e = raise(ctx, tx, JobAllocation, "incoming/"+id, tx.Scope().Ref(a.AllocationID, a.Revision)); e != nil {
		return AllocationOutput{}, e
	}
	return AllocationOutput{AllocationRef: tx.Scope().Ref(a.AllocationID, a.Revision), State: "closing"}, nil
}

// CloseIncoming 完整核验当前source集合；未知费用或效果使原额度持续closing。
func (s *Service) refreshIncomingTx(ctx context.Context, tx runtime.Tx, t taskState) error {
	if t.IncomingAllocationID == "" {
		return nil
	}
	var a IncomingAllocation
	if _, e := tx.Get(ctx, incoming, t.IncomingAllocationID, &a); e != nil {
		return e
	}
	usage := []api.Amount{}
	for _, b := range t.Task.Budget {
		usage = append(usage, api.Amount{Unit: b.Unit, Value: b.Spent})
	}
	changed := !api.Equal(a.Cumulative, usage)
	a.Cumulative = usage
	if changed {
		a.UsageRevision++
		if a.Gate == "closed" {
			a.ClosureRef = nil
			a.ClosurePending = false
		}
	}
	if a.Gate == "closing" && !t.Task.AccountingOpen {
		rows, e := s.fullRelations(ctx, tx, t.Task.TaskID)
		if e != nil {
			return e
		}
		canClose := true
		for _, r := range rows {
			if r.Kind == "operation" && (!r.Closed || r.MayApplyLater || r.Effect == "unknown") {
				canClose = false
			}
			if r.Kind == "delegation" {
				var d Delegation
				if _, e = tx.Get(ctx, delegations, r.Ref.ObjectID, &d); e != nil {
					return e
				}
				if d.ClosureRef == nil {
					canClose = false
				}
			}
		}
		if canClose {
			a.Gate = "closed"
			if a.ClosedAt == "" {
				now, e := tx.Now(ctx)
				if e != nil {
					return e
				}
				a.ClosedAt = api.Time(now)
			}
			changed = true
		}
	}
	if a.Gate == "closed" && a.TaskRef != nil {
		closure := api.AllocationClosure{AllocationID: a.AllocationID, ParentOwnerID: a.ParentOwner, ReceiverID: a.ReceiverID, SpendingClosed: true, ClosedAt: a.ClosedAt, UsageRevision: a.UsageRevision, FinalUsage: a.Cumulative}
		semantic := fmt.Sprintf("%s/%020d", t.IncomingAllocationID, a.UsageRevision)
		binding, e := tx.LookupKey(ctx, closures, semantic)
		var ref api.ObjectRef
		if confirmedNotFound(e) {
			sealer, ok := s.ports.ClosureProof.(AllocationProofPort)
			if !ok {
				if !a.ClosurePending {
					if _, e = raise(ctx, tx, JobAllocation, "incoming/"+t.IncomingAllocationID, tx.Scope().Ref(a.AllocationID, a.UsageRevision)); e != nil {
						return e
					}
				}
				a.ClosurePending = true
				a.Revision++
				return tx.Put(ctx, incoming, t.IncomingAllocationID, a.Revision-1, a)
			}
			closure.ProofRef, e = sealer.SealAllocationClosureTx(ctx, tx, closure)
			if e != nil {
				return e
			}
			if e = s.checkSourceProof(tx.Scope(), closure.ProofRef); e != nil {
				return e
			}
			if e = api.ValidateRecord("AllocationClosure", closure); e != nil {
				return e
			}
			closureID := api.NewID("closure")
			if e = tx.Create(ctx, closures, closureID, t.Task.TaskID, closure); e != nil {
				return e
			}
			digest, e := api.Digest(closure)
			if e != nil {
				return e
			}
			if e = tx.Bind(ctx, closures, semantic, closureID, digest); e != nil {
				return e
			}
			ref = tx.Scope().Ref(closureID, 1)
		} else if e != nil {
			return e
		} else {
			var previous api.AllocationClosure
			if _, e = tx.Get(ctx, closures, binding.ObjectID, &previous); e != nil {
				return e
			}
			originalProof := previous.ProofRef
			previous.ProofRef = api.ContentRef{}
			if !api.Equal(previous, closure) {
				return api.E("idempotency_conflict", "digest_conflict")
			}
			if e = s.checkSourceProof(tx.Scope(), originalProof); e != nil {
				return e
			}
			ref = tx.Scope().Ref(binding.ObjectID, 1)
		}
		a.ClosureRef = &ref
		a.ClosurePending = false

		if changed {
			if _, e = raise(ctx, tx, JobAllocation, "correction/"+t.IncomingAllocationID, tx.Scope().Ref(a.AllocationID, a.UsageRevision)); e != nil {
				return e
			}
		}
	}
	a.Revision++
	return tx.Put(ctx, incoming, t.IncomingAllocationID, a.Revision-1, a)
}
func (s *Service) SettleTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SettleInput) (AllocationOutput, error) {
	if in.AllocationRef.OwnerID != tx.Scope().OwnerID || in.AllocationRef.TenantID != tx.Scope().TenantID || c.TargetID != in.AllocationRef.ObjectID {
		return AllocationOutput{}, api.E("forbidden", "allocation_scope_mismatch")
	}
	t, a, e := allocationForTaskTx(ctx, tx, in.AllocationRef.ObjectID)
	if e != nil {
		return AllocationOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return AllocationOutput{}, e
	}
	if in.ClosureRef.TenantID != tx.Scope().TenantID || in.ClosureRef.OwnerID != a.ReceiverID {
		return AllocationOutput{}, api.E("forbidden", "closure_scope_mismatch")
	}
	a.ClosureRef = &in.ClosureRef
	a.Revision++
	if e = tx.Put(ctx, allocations, a.AllocationID, a.Revision-1, a); e != nil {
		return AllocationOutput{}, e
	}
	if _, e = raise(ctx, tx, JobAllocation, "settle/"+a.AllocationID, tx.Scope().Ref(a.AllocationID, a.Revision)); e != nil {
		return AllocationOutput{}, e
	}
	return AllocationOutput{AllocationRef: tx.Scope().Ref(a.AllocationID, a.Revision), State: a.State}, nil
}
func (s *Service) ReconcileClosure(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, allocationID string, closure api.AllocationClosure, sourceRefs ...api.ObjectRef) error {
	return s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		return s.ReconcileClosureTx(ctx, tx, auth, allocationID, closure, sourceRefs...)
	})
}
func (s *Service) ReconcileClosureTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, allocationID string, closure api.AllocationClosure, sourceRefs ...api.ObjectRef) error {

	if !auth.HasRole("service") && auth.SubjectID != closure.ReceiverID {
		return api.E("forbidden", "receiver_identity_required")
	}
	_, a, e := allocationForTaskTx(ctx, tx, allocationID)
	if e != nil {
		return e
	}
	if len(sourceRefs) > 1 {
		return invalid("one_closure_reference_required")
	}
	if len(sourceRefs) == 1 {
		ref := sourceRefs[0]
		if err := runtime.CheckRef(tx.Scope(), ref); err != nil {
			return err
		}
		if ref.OwnerID != a.ReceiverID {
			return api.E("forbidden", "closure_scope_mismatch")
		}
		if ref.OwnerID == tx.Scope().OwnerID {
			var original api.AllocationClosure
			if err := tx.GetVersion(ctx, closures, ref.ObjectID, ref.Revision, &original); err != nil {
				return err
			}
			if !api.Equal(original, closure) {
				return api.E("idempotency_conflict", "closure_reference_mismatch")
			}
		}
		a.ClosureRef = &ref
	}
	if a.ClosureRef == nil {
		return invalid("closure_reference_required")
	}
	if closure.AllocationID != allocationID || closure.ParentOwnerID != tx.Scope().OwnerID || closure.ReceiverID != a.ReceiverID || !closure.SpendingClosed {
		return api.E("invalid_request", "closure_unverified")
	}
	if e := api.ValidateRecord("AllocationClosure", closure); e != nil {
		return e
	}
	source := api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: closure.ReceiverID, ObjectID: allocationID, Revision: closure.UsageRevision}
	usage := api.UsageSnapshot{SourceRef: source, UsageRevision: closure.UsageRevision, Cumulative: closure.FinalUsage, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{closure.ProofRef}}
	usage.UsageDigest, e = UsageDigest(usage)
	if e != nil {
		return e
	}
	if _, e = s.ReconcileUsageTx(ctx, tx, auth, "budget_allocation", usage); e != nil {
		return e
	}
	a.State = "settled"
	a.Revision++
	return tx.Put(ctx, allocations, a.AllocationID, a.Revision-1, a)
}
func (s *Service) AllocationRead(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string) (Allocation, error) {
	var a Allocation
	if _, e := store.Read(ctx, scope, allocations, id, 0, &a); e != nil {
		return a, e
	}
	t, e := s.readState(ctx, store, scope, auth, a.ParentTaskRef.ObjectID, 0)
	_ = t
	return a, e
}
func (s *Service) IncomingRead(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, ref api.ObjectRef) (IncomingAllocation, error) {
	if !auth.HasRole("service") && auth.SubjectID != ref.OwnerID {
		return IncomingAllocation{}, api.E("forbidden", "parent_identity_required")
	}
	var a IncomingAllocation
	_, e := store.Read(ctx, scope, incoming, incomingID(ref), 0, &a)
	return a, e
}
func (s *Service) AllocationClosureRead(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, ref api.ObjectRef) (api.AllocationClosure, error) {
	if ref.TenantID != scope.TenantID || ref.OwnerID != scope.OwnerID {
		return api.AllocationClosure{}, api.E("forbidden", "closure_scope_mismatch")
	}
	var c api.AllocationClosure
	_, e := store.Read(ctx, scope, closures, ref.ObjectID, ref.Revision, &c)
	if e != nil {
		return c, e
	}
	if !auth.HasRole("service") && auth.SubjectID != c.ParentOwnerID {
		return c, api.E("forbidden", "parent_identity_required")
	}
	return c, nil
}
func allocationRetry(now time.Time) runtime.Disposition { return runtime.Waiting(now.Add(time.Second)) }
