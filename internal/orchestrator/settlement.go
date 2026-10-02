package orchestrator

import (
	"context"
	"errors"
	"strconv"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

func (c *Coordinator) settle(ctx context.Context, u Unit, f frame) error {
	if f.Ref.ObjectKind == "receiver" {
		return c.settleReceiver(ctx, u, f)
	}
	kind := f.Ref.ObjectKind
	switch kind {
	case "brain_decision":
		kind = "decision"
	case "execution_operation":
		kind = "operation"
	case "budget_allocation":
		kind = "allocation"
	}
	if kind == "allocation" {
		return c.settleParent(ctx, u, f)
	}
	r, e := c.readRecord(ctx, u, "reservation", reserveID(kind, f.Ref.ObjectID))
	if e != nil {
		return e
	}
	if r == nil && kind == "grant_use" {
		alias, e := c.readRecord(ctx, u, "billing_alias", f.Ref.ObjectID)
		if e != nil {
			return e
		}
		id, e := Decode[string](alias)
		if e != nil {
			return e
		}
		r, e = c.readRecord(ctx, u, "reservation", id)
		if e != nil {
			return e
		}
	}
	reservation, e := Decode[Reservation](r)
	if e != nil {
		return e
	}
	allowed, e := c.attempt(ctx, u, f)
	if e != nil || !allowed {
		return e
	}
	var bill api.Billing
	e = c.external(ctx, u, f, "billing.read", reservation.ObjectID, func() error {
		var e error
		bill, e = c.Ports.Billing.Read(ctx, f.Caller, api.BillingQuery{TaskRef: taskRef(f.Task), ObjectOwnerID: reservation.ObjectOwnerID, ObjectKind: reservation.ObjectKind, ObjectID: reservation.ObjectID, Source: reservation.Source})
		return e
	})
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	if _, e = c.content(ctx, f.Caller, bill.ProofRef); e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		r, e := c.Repository.Get(tx, "reservation", reservation.ID)
		if e != nil {
			return durable.Disposition{}, e
		}
		current, e := Decode[Reservation](r)
		if e != nil {
			return durable.Disposition{}, e
		}
		changed, e := c.applyBilling(tx, ch, t, current, bill)
		if e != nil {
			return durable.Disposition{}, e
		}
		if changed && bill.Source.Kind == "grant_use" {
			alias := ID("alias", bill.Source.OwnerID, "grant_use", bill.Source.ID)
			if e = c.Repository.Put(tx, t, rec("billing_alias", alias, t.Task.TaskID, 1, "closed", true, current.ID)); e != nil {
				return durable.Disposition{}, e
			}
			if e = c.Repository.Put(tx, t, rec("billing_alias", bill.Source.ID, t.Task.TaskID, 1, "closed", true, current.ID)); e != nil {
				return durable.Disposition{}, e
			}
		}
		if t.AllocationID != "" {
			queue(ch, t, "settle", "receiver", t.AllocationID)
		}
		if bill.Final {
			return durable.Done(), nil
		}
		return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "original_bill_not_final"), nil
	})
}

func (c *Coordinator) settleParent(ctx context.Context, u Unit, f frame) error {
	r, e := c.readRecord(ctx, u, "allocation", f.Ref.ObjectID)
	if e != nil {
		return e
	}
	a, e := Decode[AllocationState](r)
	if e != nil {
		return e
	}
	allowed, e := c.attempt(ctx, u, f)
	if e != nil || !allowed {
		return e
	}
	var closure api.RuntimeBudgetClosure
	if a.Allocation.ReceiverID == c.Config.Scope.OwnerID {
		e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
			r, e := c.Repository.Receiver(tx, a.Allocation.AllocationID)
			if e != nil {
				return e
			}
			if r == nil || r.Receiver.Closure == nil {
				return failure("not_found", "original receiver closure pending")
			}
			closure = *r.Receiver.Closure
			return nil
		})
	} else {
		e = c.external(ctx, u, f, "budget.read", a.Allocation.ReceiverID, func() error {
			var e error
			closure, e = c.Ports.Billing.Closure(ctx, f.Caller, a.Allocation.ReceiverID, a.Allocation.AllocationID)
			return e
		})
	}
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	if validateValue("RuntimeBudgetClosure", closure) != nil || !closure.SpendingClosed || closure.AllocationID != a.Allocation.AllocationID || closure.ReceiverID != a.Allocation.ReceiverID {
		return c.finish(ctx, u, f, "receiver_closure_pending", false)
	}
	if _, e = c.content(ctx, f.Caller, closure.ProofRef); e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		r, e := c.Repository.Get(tx, "allocation", a.Allocation.AllocationID)
		if e != nil {
			return durable.Disposition{}, e
		}
		current, e := Decode[AllocationState](r)
		if e != nil {
			return durable.Disposition{}, e
		}
		input := api.BudgetSettleInput{AllocationID: a.Allocation.AllocationID, Closure: closure}
		call := originalCall(c.Config.Scope.OwnerID, "budget.settle", a.Allocation.AllocationID, ID("command", a.Allocation.AllocationID, "settle", strconv.FormatInt(closure.UsageRevision, 10)), input, ch.Now.Add(c.Config.Limits.TrustedReview))
		fixed, e := fixedCall(call)
		if e != nil {
			return durable.Disposition{}, e
		}
		_, e = c.settleAllocation(tx, ch, t, Prepared{Caller: f.Caller, Intent: fixed}, current, input)
		if e != nil {
			return durable.Disposition{}, e
		}
		return durable.Done(), nil
	})
}

func (c *Coordinator) receiverTransaction(ctx context.Context, u Unit, f frame, finish *durable.Disposition, fn func(*durable.Tx, *Change, *TaskState, *ReceiverState) error) error {
	return u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			var ch *Change
			var task *TaskState
			var e error
			if f.Task != nil {
				ch, task, e = c.load(tx, f.Task.Task.TaskID, false, nil)
				if e != nil {
					return e
				}
			} else {
				if e = c.Repository.Read(tx); e != nil {
					return e
				}
				now, e := tx.Now()
				if e != nil {
					return e
				}
				ch = &Change{Tasks: map[string]*TaskState{}, Works: map[durable.JobKey]WorkRef{}, Now: now}
			}
			if _, e = c.Repository.Gate(tx, "receiver-"+f.Ref.ObjectID, false, false); e != nil {
				return e
			}
			receiver, e := c.Repository.Receiver(tx, f.Ref.ObjectID)
			if e != nil {
				return e
			}
			if receiver == nil {
				return durable.ErrInvariant
			}
			if e = fn(tx, ch, task, receiver); e != nil {
				return e
			}
			if e = c.Repository.SaveReceiver(tx, receiver); e != nil {
				return e
			}
			claim := u.Claim()
			return c.jobCommit(tx, ch, &claim, finish)
		}))
	})
}
func (c *Coordinator) settleReceiver(ctx context.Context, u Unit, f frame) error {
	var receiver *ReceiverState
	e := c.receiverTransaction(ctx, u, f, nil, func(tx *durable.Tx, ch *Change, t *TaskState, r *ReceiverState) error {
		copy := *r
		receiver = &copy
		return nil
	})
	if e != nil {
		return e
	}
	if receiver.Receiver.State == "open" && (f.Task == nil || !terminal(f.Task)) {
		return c.finish(ctx, u, f, "", true)
	}
	if len(receiver.Limits) == 0 || receiver.ParentTaskID == "" {
		var allocation api.RuntimeBudgetAllocation
		e = c.external(ctx, u, f, "budget.read", receiver.Receiver.ParentOwnerID, func() error {
			var e error
			allocation, e = c.Ports.Billing.Allocation(ctx, f.Caller, receiver.Receiver.ParentOwnerID, receiver.Receiver.AllocationID)
			return e
		})
		if e != nil {
			return c.finish(ctx, u, f, "dependency_unavailable", false)
		}
		if allocation.OwnerID != receiver.Receiver.ParentOwnerID || allocation.AllocationID != receiver.Receiver.AllocationID || allocation.ReceiverID != c.Config.Scope.OwnerID || validateValue("RuntimeBudgetAllocation", allocation) != nil {
			return c.finish(ctx, u, f, "invalid_original_allocation", false)
		}
		receiver.Limits = allocation.Limits
		receiver.ParentTaskID = allocation.ParentTaskID
	}
	final := false
	e = c.receiverTransaction(ctx, u, f, nil, func(tx *durable.Tx, ch *Change, t *TaskState, r *ReceiverState) error {
		if len(r.Limits) == 0 {
			r.Limits = receiver.Limits
		}
		if r.ParentTaskID == "" {
			r.ParentTaskID = receiver.ParentTaskID
		}
		if r.Receiver.State == "open" {
			r.Receiver.State = "closing"
			r.Receiver.Revision++
		}
		if t != nil {
			open, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "reservation", State: "open", Limit: 1})
			if e != nil {
				return e
			}
			if len(open) > 0 {
				return nil
			}
		}
		usage := []api.Amount{}
		for _, l := range r.Limits {
			amount := "0"
			if t != nil {
				found := false
				for _, b := range t.Task.Budget {
					if b.Unit == l.Unit {
						amount = b.Spent.Amount
						found = true
					}
				}
				if !found {
					return failure("precondition_failed", "closure unit set is incomplete")
				}
			}
			usage = append(usage, api.Amount{Unit: l.Unit, Amount: amount})
		}
		if r.Receiver.State == "closed" && r.PendingClosure == nil && Hash(r.Receiver.FinalUsage) == Hash(usage) {
			final = true
			*receiver = *r
			return nil
		}
		if r.Publication == nil || r.PendingClosure == nil && Hash(r.Receiver.FinalUsage) != Hash(usage) || r.PendingClosure != nil && Hash(r.PendingClosure.FinalUsage) != Hash(usage) {
			r.ClosedUsageRevision++
			r.Receiver.Revision++
			closure := api.RuntimeBudgetClosure{AllocationID: r.Receiver.AllocationID, ReceiverID: c.Config.Scope.OwnerID, UsageRevision: r.ClosedUsageRevision, FinalUsage: usage, SpendingClosed: true, ClosedAt: stamp(ch.Now)}
			pub, e := publication(ID("publication", r.Receiver.AllocationID, strconv.FormatInt(r.ClosedUsageRevision, 10)), func() string {
				if t != nil {
					return t.Task.TaskID
				}
				return ""
			}(), closure)
			if e != nil {
				return e
			}
			r.Publication = &pub
			r.PendingClosure = &closure
		}
		*receiver = *r
		return nil
	})
	if e != nil {
		return e
	}
	if receiver.Publication == nil {
		return c.finish(ctx, u, f, "receiver_original_bills_pending", false)
	}
	if !final && receiver.Publication.Ref == nil {
		p := receiver.Publication
		var ref api.ContentRef
		e = c.external(ctx, u, f, "content.write", p.ID, func() error {
			var e error
			ref, e = c.Ports.Content.Write(ctx, f.Caller, p.ID, p.MediaType, p.Body)
			return e
		})
		if e != nil {
			return c.finish(ctx, u, f, "dependency_unavailable", false)
		}
		if ref.Hash != p.Digest || ref.ByteLength != int64(len(p.Body)) || ref.TenantID != c.Config.Scope.TenantID || validateValue("ContentRef", ref) != nil {
			return c.finish(ctx, u, f, "invalid_closure_publication", false)
		}
		e = c.receiverTransaction(ctx, u, f, nil, func(tx *durable.Tx, ch *Change, t *TaskState, r *ReceiverState) error {
			if r.Publication == nil || r.Publication.Digest != p.Digest {
				return failure("revision_conflict", "cumulative usage changed during publication")
			}
			r.Publication.Ref = &ref
			r.Publication.Body = nil
			r.PendingClosure.ProofRef = ref
			r.Receiver.Closure = r.PendingClosure
			r.Receiver.FinalUsage = r.PendingClosure.FinalUsage
			r.PendingClosure = nil
			r.Receiver.State = "closed"
			r.Receiver.Revision++
			digest := Hash(*r.Receiver.Closure)
			input := api.TaskBillingReconcileInput{SourceKind: "budget_allocation", SourceID: r.Receiver.AllocationID, UsageRevision: r.ClosedUsageRevision, UsageDigest: digest}
			call := originalCall(r.Receiver.ParentOwnerID, "task.billing_reconcile", r.ParentTaskID, ID("command", r.Receiver.AllocationID, "reconcile", strconv.FormatInt(r.ClosedUsageRevision, 10)), input, ch.Now.Add(c.Config.Limits.TrustedReview))
			if r.Outbox != nil {
				if e := c.Repository.PutGlobal(tx, "receiver-"+r.Receiver.AllocationID, rec("receiver_outbox", r.Outbox.Call.Command.CommandID, "receiver-"+r.Receiver.AllocationID, 1, "closed", true, *r.Outbox)); e != nil {
					return e
				}
			}
			r.Outbox = &Outbox{TaskID: func() string {
				if t != nil {
					return t.Task.TaskID
				}
				return ""
			}(), ReceiverID: r.Receiver.ParentOwnerID, AllocationID: r.Receiver.AllocationID, Digest: digest, UsageRevision: r.ClosedUsageRevision, Call: call}
			*receiver = *r
			return nil
		})
		if e != nil {
			return e
		}
	}
	if receiver.Outbox != nil {
		ack := api.JobAck{}
		if receiver.Receiver.ParentOwnerID == c.Config.Scope.OwnerID {
			e = c.receiverTransaction(ctx, u, f, nil, func(tx *durable.Tx, ch *Change, t *TaskState, r *ReceiverState) error {
				parent := ch.Tasks[r.ParentTaskID]
				if parent == nil {
					return durable.ErrInvariant
				}
				queue(ch, parent, "settle", "allocation", r.Receiver.AllocationID)
				return nil
			})
			ack.JobID = ID("job", receiver.Receiver.AllocationID, "handoff")
		} else {
			e = c.external(ctx, u, f, "task.billing_reconcile", receiver.Outbox.ReceiverID, func() error {
				var e error
				ack, e = c.Ports.Billing.Reconcile(ctx, f.Caller, receiver.Outbox.Call)
				return e
			})
		}
		if e != nil {
			var known *api.Failure
			if errors.As(e, &known) && known.Detail.Code == "expired" && known.Detail.Retry == "none" && len(receiver.Outbox.Previous) < 8 {
				d := durable.Waiting(c.Ports.Clock.Now().Add(c.Config.Limits.Backoff), "known_unapplied_handoff_expired")
				return c.receiverTransaction(ctx, u, f, &d, func(tx *durable.Tx, ch *Change, t *TaskState, r *ReceiverState) error {
					if r.Outbox == nil || r.Outbox.Call.Command.CommandID != receiver.Outbox.Call.Command.CommandID {
						return durable.ErrConflict
					}
					old := r.Outbox.Call
					r.Outbox.Previous = append(r.Outbox.Previous, old)
					r.Outbox.Call.Command.CommandID = ID("command", old.Command.CommandID, "successor")
					r.Outbox.Call.Command.ExpiresAt = stamp(ch.Now.Add(c.Config.Limits.TrustedReview))
					return nil
				})
			}
			return c.finish(ctx, u, f, "dependency_unavailable", false)
		}
		if ack.JobID == "" {
			return c.finish(ctx, u, f, "billing_handoff_pending", false)
		}
	}
	d := durable.Done()
	return c.receiverTransaction(ctx, u, f, &d, func(tx *durable.Tx, ch *Change, t *TaskState, r *ReceiverState) error {
		if r.ClosedUsageRevision != receiver.ClosedUsageRevision {
			return failure("revision_conflict", "newer correction remains pending")
		}
		r.Outbox = nil
		return nil
	})
}

func (c *Coordinator) prepareExtraction(tx *durable.Tx, ch *Change, t *TaskState, result api.Result) error {
	if t.Policy.ExtractionOwnerID == "" {
		return nil
	}
	id := ID("extraction", t.Task.TaskID, strconv.FormatInt(t.Task.GoalRevision, 10))
	pub, e := publication(ID("publication", id), t.Task.TaskID, result)
	if e != nil {
		return e
	}
	if e = c.Repository.Put(tx, t, rec("publication", pub.ID, t.Task.TaskID, 1, "open", false, pub)); e != nil {
		return e
	}
	state := ExtractionState{Call: originalCall(t.Policy.ExtractionOwnerID, "memory.extract", t.Policy.ExtractionOwnerID, ID("command", id), map[string]any{"request_id": id, "source_publication_id": pub.ID, "authorization_refs": t.Policy.ExtractionAuthorizationRefs}, ch.Now.Add(c.Config.Limits.TrustedReview))}
	if e = c.Repository.Put(tx, t, rec("extraction", id, t.Task.TaskID, 1, "open", false, state)); e != nil {
		return e
	}
	queue(ch, t, "extract", "extraction", id)
	return nil
}
func (c *Coordinator) extract(ctx context.Context, u Unit, f frame) error {
	if c.Ports.Extraction == nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	r, e := c.readRecord(ctx, u, "extraction", f.Ref.ObjectID)
	if e != nil {
		return e
	}
	state, e := Decode[ExtractionState](r)
	if e != nil {
		return e
	}
	if state.Ack != "" {
		return c.finish(ctx, u, f, "", true)
	}
	ref, e := c.publish(ctx, u, f, ID("publication", f.Ref.ObjectID))
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	if state.Source.ContentID == "" {
		state.Source = ref
		state.Call.Command.Payload = Raw(map[string]any{"request_id": f.Ref.ObjectID, "source_refs": []api.ContentRef{ref}, "authorization_refs": f.Task.Policy.ExtractionAuthorizationRefs})
		e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
			return c.Repository.Put(tx, t, rec("extraction", f.Ref.ObjectID, t.Task.TaskID, 2, "open", false, state))
		})
		if e != nil {
			return e
		}
	}
	var ack string
	e = c.external(ctx, u, f, "memory.extract", state.Call.OwnerID, func() error {
		var e error
		ack, e = c.Ports.Extraction.Submit(ctx, f.Caller, state.Call, state.Source)
		return e
	})
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	if ack == "" {
		return c.finish(ctx, u, f, "extraction_handoff_pending", false)
	}
	state.Ack = ack
	return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		return durable.Done(), c.Repository.Put(tx, t, rec("extraction", f.Ref.ObjectID, t.Task.TaskID, 3, "closed", false, state))
	})
}
