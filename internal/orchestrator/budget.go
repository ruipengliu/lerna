package orchestrator

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

func balance(unit, limit, spent, reserved string) api.BudgetBalance {
	return api.BudgetBalance{Unit: unit, Limit: api.Amount{Unit: unit, Amount: limit}, Spent: api.Amount{Unit: unit, Amount: spent}, Reserved: api.Amount{Unit: unit, Amount: reserved}}
}
func amountMap(values []api.Amount) map[string]string {
	m := map[string]string{}
	for _, v := range values {
		m[v.Unit] = v.Amount
	}
	return m
}
func balanceMap(values []api.BudgetBalance) map[string]api.BudgetBalance {
	m := map[string]api.BudgetBalance{}
	for _, v := range values {
		m[v.Unit] = v
	}
	return m
}
func (c *Coordinator) saveBalances(tx *durable.Tx, t *TaskState, values map[string]api.BudgetBalance) error {
	keys := []string{}
	for unit := range values {
		keys = append(keys, unit)
	}
	sort.Strings(keys)
	t.Task.Budget = []api.BudgetBalance{}
	for _, unit := range keys {
		t.Task.Budget = append(t.Task.Budget, values[unit])
	}
	return c.Repository.SaveBalances(tx, t.Task.TaskID, t.Task.Budget)
}
func (c *Coordinator) reserve(tx *durable.Tx, ch *Change, t *TaskState, r Reservation) error {
	if err := amountsValid(r.UpperBound); err != nil {
		return err
	}
	if r.TaskID != t.Task.TaskID || r.ID == "" {
		return durable.ErrInvariant
	}
	if existing, err := c.Repository.Get(tx, "reservation", r.ID); err != nil {
		return err
	} else if existing != nil {
		old, err := Decode[Reservation](existing)
		if err != nil {
			return err
		}
		if old.ObjectID != r.ObjectID || old.ObjectKind != r.ObjectKind || Hash(old.UpperBound) != Hash(r.UpperBound) {
			return failure("idempotency_conflict", "reservation is bound to another immutable call")
		}
		return nil
	}
	if t.AllocationID != "" {
		if _, err := c.Repository.Gate(tx, "receiver-"+t.AllocationID, false, false); err != nil {
			return err
		}
		receiver, err := c.Repository.Receiver(tx, t.AllocationID)
		if err != nil {
			return err
		}
		if receiver == nil || receiver.Receiver.State != "open" {
			return failure("precondition_failed", "allocated receiver has closed new spending")
		}
	}
	values := balanceMap(t.Task.Budget)
	for _, a := range r.UpperBound {
		b, ok := values[a.Unit]
		if !ok {
			return failure("precondition_failed", "trusted billing unit is outside this task budget")
		}
		used, err := add(b.Spent.Amount, b.Reserved.Amount)
		if err != nil {
			return err
		}
		used, err = add(used, a.Amount)
		if err != nil {
			return err
		}
		cmp, err := compare(used, b.Limit.Amount)
		if err != nil {
			return err
		}
		if cmp > 0 {
			return failure("quota_exceeded", "strict upper bound exceeds remaining budget")
		}
		b.Reserved.Amount, err = add(b.Reserved.Amount, a.Amount)
		if err != nil {
			return err
		}
		values[a.Unit] = b
	}
	ch.Changed = true
	if err := c.saveBalances(tx, t, values); err != nil {
		return err
	}
	r.Usage = []api.Amount{}
	return c.Repository.Put(tx, t, rec("reservation", r.ID, t.Task.TaskID, 1, "open", false, r))
}
func (c *Coordinator) adjust(tx *durable.Tx, ch *Change, t *TaskState, limits []api.BudgetLimit) error {
	values := balanceMap(t.Task.Budget)
	if len(limits) != len(values) {
		return failure("precondition_failed", "adjust_budget replaces the complete existing unit set")
	}
	for _, a := range limits {
		b, ok := values[a.Unit]
		if !ok {
			return failure("precondition_failed", "budget unit set is immutable")
		}
		used, err := add(b.Spent.Amount, b.Reserved.Amount)
		if err != nil {
			return err
		}
		cmp, err := compare(a.Limit, used)
		if err != nil {
			return err
		}
		if cmp < 0 {
			return failure("precondition_failed", "new limit is below already spent and outstanding obligations")
		}
		if t.AllocationID != "" {
			receiver, err := c.Repository.Receiver(tx, t.AllocationID)
			if err != nil {
				return err
			}
			if receiver == nil {
				return durable.ErrInvariant
			}
			for _, limit := range receiver.Limits {
				if limit.Unit == a.Unit {
					cmp, err := compare(a.Limit, limit.Limit)
					if err != nil {
						return err
					}
					if cmp > 0 {
						return failure("precondition_failed", "receiver cannot exceed its original allocation")
					}
				}
			}
		}
		b.Limit.Amount = a.Limit
		values[a.Unit] = b
	}
	ch.Changed = true
	if err := c.saveBalances(tx, t, values); err != nil {
		return err
	}
	clearWait(t, "budget", "")
	t.Task.Revision++
	queue(ch, t, "decide", "task", t.Task.TaskID)
	return c.persist(tx, ch, t)
}
func (c *Coordinator) allocate(tx *durable.Tx, ch *Change, t *TaskState, p Prepared, in api.BudgetAllocateInput) (any, error) {
	if in.ParentTaskID != t.Task.TaskID || p.Intent.TargetID() != t.Task.TaskID {
		return nil, failure("invalid_argument", "allocation must bind its original parent")
	}
	if err := c.canAdvance(ch, t); err != nil {
		return nil, err
	}
	expiry, _ := time.Parse(time.RFC3339Nano, in.ExpiresAt)
	deadline, _ := time.Parse(time.RFC3339Nano, t.Task.Deadline)
	if !ch.Now.Before(expiry) || expiry.After(deadline) {
		return nil, failure("expired", "allocation requires a bounded deadline within its parent")
	}
	if old, err := c.Repository.Get(tx, "allocation", in.AllocationID); err != nil {
		return nil, err
	} else if old != nil {
		v, err := Decode[AllocationState](old)
		if err != nil {
			return nil, err
		}
		if old.TaskID != t.Task.TaskID || v.Allocation.ReceiverID != in.ReceiverID || Hash(v.Allocation.Limits) != Hash(in.Limits) || v.Allocation.ExpiresAt != in.ExpiresAt {
			return nil, failure("idempotency_conflict", "allocation identity has an immutable original binding")
		}
		return v.Allocation, nil
	}
	amounts := []api.Amount{}
	for _, a := range in.Limits {
		amounts = append(amounts, api.Amount{Unit: a.Unit, Amount: a.Limit})
	}
	reservation := Reservation{ID: reserveID("allocation", in.AllocationID), TaskID: t.Task.TaskID, ObjectOwnerID: in.ReceiverID, ObjectKind: "budget_allocation", ObjectID: in.AllocationID, UpperBound: amounts, Allocation: true}
	if err := c.reserve(tx, ch, t, reservation); err != nil {
		return nil, err
	}
	v := AllocationState{Allocation: api.RuntimeBudgetAllocation{AllocationID: in.AllocationID, ParentTaskID: t.Task.TaskID, OwnerID: c.Config.Scope.OwnerID, ReceiverID: in.ReceiverID, Revision: 1, Limits: in.Limits, ExpiresAt: in.ExpiresAt, State: "allocated", FinalUsage: []api.Amount{}}, CommandID: p.Intent.CommandID(), ReservationID: reservation.ID}
	if err := c.Repository.Put(tx, t, rec("allocation", in.AllocationID, t.Task.TaskID, 1, "open", false, v)); err != nil {
		return nil, err
	}
	queue(ch, t, "settle", "budget_allocation", in.AllocationID)
	t.Task.Revision++
	if err := c.persist(tx, ch, t); err != nil {
		return nil, err
	}
	return v.Allocation, nil
}
func (c *Coordinator) closeBudget(tx *durable.Tx, p Prepared) (durable.Decision, error) {
	if err := c.Repository.Read(tx); err != nil {
		return durable.Decision{}, err
	}
	var in api.BudgetCloseInput
	_ = json.Unmarshal(p.Intent.Value().Payload, &in)
	id := in.AllocationRef.ID
	if p.Intent.TargetID() != id || in.AllocationRef.OwnerID != in.SenderOrchestratorID || p.Caller.SourceOwnerID != in.SenderOrchestratorID && in.SenderOrchestratorID != c.Config.Scope.OwnerID {
		return rejected(p.Intent, failure("forbidden", "budget close requires the original authenticated sender and allocation")), nil
	}
	if _, err := c.Repository.Gate(tx, "receiver-"+id, false, true); err != nil {
		return durable.Decision{}, err
	}
	v, err := c.Repository.Receiver(tx, id)
	if err != nil {
		return durable.Decision{}, err
	}
	if v != nil && (v.Receiver.ParentOwnerID != in.SenderOrchestratorID || v.Receiver.ParentDelegationID != in.ParentDelegationID || v.AllocationCommandID != in.AllocationCommandID) {
		return rejected(p.Intent, failure("idempotency_conflict", "receiver close cannot change original parent bindings")), nil
	}
	if v == nil {
		v = &ReceiverState{Receiver: api.RuntimeBudgetReceiver{AllocationID: id, ParentOwnerID: in.SenderOrchestratorID, ReceiverID: c.Config.Scope.OwnerID, ParentDelegationID: in.ParentDelegationID, Revision: 1, State: "closing", FinalUsage: []api.Amount{}}, AllocationCommandID: in.AllocationCommandID}
	}
	if v.Receiver.State == "open" {
		v.Receiver.State = "closing"
		v.Receiver.Revision++
	}
	if err = c.Repository.SaveReceiver(tx, v); err != nil {
		return durable.Decision{}, err
	}
	now, err := tx.Now()
	if err != nil {
		return durable.Decision{}, err
	}
	source := id
	key := workKey(WorkRef{Kind: "settle", ObjectKind: "receiver", ObjectID: id})
	if _, err = tx.Raise(key, source, now); err != nil {
		return durable.Decision{}, err
	}
	d := applied(p.Intent, nil, v.Receiver)
	d.Receipt.ResourceID = id
	rev := v.Receiver.Revision
	d.Receipt.Revision = &rev
	return d, nil
}

type billingHint struct {
	Input   api.TaskBillingReconcileInput
	OwnerID string
	Ack     api.JobAck
}

func (c *Coordinator) billingHint(tx *durable.Tx, ch *Change, t *TaskState, p Prepared) (any, error) {
	var in api.TaskBillingReconcileInput
	_ = json.Unmarshal(p.Intent.Value().Payload, &in)
	kind := ""
	switch in.SourceKind {
	case "brain_decision":
		kind = "decision"
	case "execution_operation":
		kind = "operation"
	case "budget_allocation":
		kind = "allocation"
	case "grant_use":
		kind = "grant_use"
	}
	reservationID := reserveID(kind, in.SourceID)
	r, err := c.Repository.Get(tx, "reservation", reservationID)
	if err != nil {
		return nil, err
	}
	if r == nil && kind == "grant_use" {
		alias, err := c.Repository.Get(tx, "billing_alias", ID("alias", p.Caller.SourceOwnerID, in.SourceKind, in.SourceID))
		if err != nil {
			return nil, err
		}
		if alias != nil {
			var id string
			_ = json.Unmarshal(alias.Data, &id)
			r, err = c.Repository.Get(tx, "reservation", id)
			if err != nil {
				return nil, err
			}
		}
	}
	if r == nil || r.TaskID != t.Task.TaskID {
		return nil, failure("not_found", "notification has no original task billing binding")
	}
	reservation, err := Decode[Reservation](r)
	if err != nil {
		return nil, err
	}
	if p.Caller.SourceOwnerID == "" || p.Caller.SourceOwnerID != reservation.ObjectOwnerID && (reservation.Source == nil || p.Caller.SourceOwnerID != reservation.Source.OwnerID) && kind != "grant_use" {
		return nil, failure("forbidden", "notification is not from the original authenticated billing owner")
	}
	key := ID("billing_hint", p.Caller.SourceOwnerID, in.SourceKind, in.SourceID, Hash(in.UsageRevision))
	old, err := c.Repository.Get(tx, "billing_hint", key)
	if err != nil {
		return nil, err
	}
	if old != nil {
		v, err := Decode[billingHint](old)
		if err != nil {
			return nil, err
		}
		if v.Input.UsageDigest != in.UsageDigest {
			return nil, failure("precondition_failed", "same source usage revision has conflicting digest")
		}
		return v.Ack, nil
	}
	ch.Changed = true
	queue(ch, t, "settle", in.SourceKind, in.SourceID)
	if err = c.Repository.Put(tx, t, rec("billing_hint", key, t.Task.TaskID, 1, "closed", false, billingHint{Input: in, OwnerID: p.Caller.SourceOwnerID, Ack: api.JobAck{ResourceID: t.Task.TaskID}})); err != nil {
		return nil, err
	}
	return api.JobAck{ResourceID: t.Task.TaskID}, nil
}
func (c *Coordinator) finishHint(tx *durable.Tx, t *TaskState, p Prepared, ack api.JobAck) error {
	var in api.TaskBillingReconcileInput
	_ = json.Unmarshal(p.Intent.Value().Payload, &in)
	key := ID("billing_hint", p.Caller.SourceOwnerID, in.SourceKind, in.SourceID, Hash(in.UsageRevision))
	r, err := c.Repository.Get(tx, "billing_hint", key)
	if err != nil {
		return err
	}
	v, err := Decode[billingHint](r)
	if err != nil {
		return err
	}
	if v.Ack.JobID != "" {
		return nil
	}
	v.Ack = ack
	return c.Repository.Put(tx, t, rec("billing_hint", key, t.Task.TaskID, 1, "closed", false, v))
}
func (c *Coordinator) applyBilling(tx *durable.Tx, ch *Change, t *TaskState, r Reservation, bill api.Billing) (bool, error) {
	if bill.Revision < 1 || bill.Source.ID == "" || bill.Source.OwnerID == "" || bill.Digest == "" || bill.ProofRef.TenantID != t.Task.TenantID {
		return false, failure("precondition_failed", "trusted original cumulative billing proof required")
	}
	if err := amountsValid(bill.Usage); err != nil {
		return false, err
	}
	if r.Source != nil && Hash(*r.Source) != Hash(bill.Source) {
		return false, failure("precondition_failed", "physical billing source cannot change")
	}
	if bill.Revision < r.UsageRevision {
		return false, nil
	}
	if r.Final && !bill.Final {
		return false, failure("precondition_failed", "a final physical bill cannot reopen its consumption")
	}
	if bill.Revision == r.UsageRevision {
		if r.UsageDigest != bill.Digest || Hash(r.Usage) != Hash(bill.Usage) || r.Final != bill.Final {
			return false, failure("precondition_failed", "same usage revision has conflicting contents")
		}
		return false, nil
	}
	upper, old, current := amountMap(r.UpperBound), amountMap(r.Usage), amountMap(bill.Usage)
	if len(upper) != len(current) {
		return false, failure("precondition_failed", "bill must cover the complete reserved unit set")
	}
	balances := balanceMap(t.Task.Budget)
	breach := false
	for unit, cap := range upper {
		newValue, ok := current[unit]
		if !ok {
			return false, failure("precondition_failed", "bill unit does not match the original reservation")
		}
		prior := old[unit]
		if prior == "" {
			prior = "0"
		}
		cmp, err := compare(newValue, prior)
		if err != nil || cmp < 0 {
			return false, failure("precondition_failed", "refunds need their independent reconciliation contract")
		}
		delta, err := sub(newValue, prior)
		if err != nil {
			return false, err
		}
		b, ok := balances[unit]
		if !ok {
			return false, durable.ErrInvariant
		}
		b.Spent.Amount, err = add(b.Spent.Amount, delta)
		if err != nil {
			return false, err
		}
		before := "0"
		if !r.Final {
			cmp, err = compare(cap, prior)
			if err != nil {
				return false, err
			}
			if cmp > 0 {
				before, err = sub(cap, prior)
				if err != nil {
					return false, err
				}
			}
		}
		after := "0"
		if !bill.Final {
			cmp, err = compare(cap, newValue)
			if err != nil {
				return false, err
			}
			if cmp > 0 {
				after, err = sub(cap, newValue)
				if err != nil {
					return false, err
				}
			}
		}
		release, err := sub(before, after)
		if err != nil {
			return false, err
		}
		b.Reserved.Amount, err = sub(b.Reserved.Amount, release)
		if err != nil {
			return false, err
		}
		balances[unit] = b
		cmp, err = compare(newValue, cap)
		if err != nil {
			return false, err
		}
		breach = breach || cmp > 0
	}
	physical := ID("billing_source", bill.Source.OwnerID, bill.Source.Kind, bill.Source.ID)
	binding, err := c.Repository.Get(tx, "billing_source", physical)
	if err != nil {
		return false, err
	}
	if binding != nil {
		var bound string
		_ = json.Unmarshal(binding.Data, &bound)
		if binding.TaskID != t.Task.TaskID || bound != r.ID {
			return false, failure("precondition_failed", "one physical bill cannot be charged to two reservations")
		}
	}
	ch.Changed = true
	if err = c.saveBalances(tx, t, balances); err != nil {
		return false, err
	}
	if binding == nil {
		if err = c.Repository.Put(tx, t, rec("billing_source", physical, t.Task.TaskID, 1, "closed", true, r.ID)); err != nil {
			return false, err
		}
	}
	r.Source = &bill.Source
	r.Usage = bill.Usage
	r.UsageRevision = bill.Revision
	r.UsageDigest = bill.Digest
	r.Final = bill.Final
	state := "open"
	if r.Final {
		state = "closed"
	}
	if err = c.Repository.Put(tx, t, rec("reservation", r.ID, t.Task.TaskID, bill.Revision, state, false, r)); err != nil {
		return false, err
	}
	if breach {
		cause := "provider_bound_breach"
		if r.Allocation {
			cause = "receiver_allocation_breach"
		}
		if err = c.Repository.Put(tx, t, rec("incident", ID("incident", r.ID, Hash(bill.Revision)), t.Task.TaskID, bill.Revision, "open", true, struct {
			Cause       string
			Reservation Reservation
			Bill        api.Billing
		}{cause, r, bill})); err != nil {
			return false, err
		}
		wait(t, "budget", "trusted upper bound was breached; reconcile original debt", nil)
	}
	t.Task.Revision++
	queue(ch, t, "verify", "task", t.Task.TaskID)
	if !r.Final {
		queue(ch, t, "settle", r.ObjectKind, r.ObjectID)
	}
	if err = c.persist(tx, ch, t); err != nil {
		return false, err
	}
	return true, nil
}
func (c *Coordinator) settleAllocation(tx *durable.Tx, ch *Change, t *TaskState, p Prepared, a AllocationState, in api.BudgetSettleInput) (any, error) {
	if in.AllocationID != p.Intent.TargetID() || a.Allocation.AllocationID != in.Closure.AllocationID || a.Allocation.ReceiverID != in.Closure.ReceiverID || !in.Closure.SpendingClosed {
		return nil, failure("precondition_failed", "closure must bind this exact original receiver")
	}
	if in.Closure.UsageRevision < 1 {
		return nil, failure("precondition_failed", "closure usage revision is required")
	}
	if a.Allocation.Closure != nil && in.Closure.UsageRevision == a.Allocation.Closure.UsageRevision {
		if Hash(in.Closure) != Hash(*a.Allocation.Closure) {
			return nil, failure("precondition_failed", "same closure revision conflicts")
		}
		return a.Allocation, nil
	}
	if a.Allocation.Closure != nil && in.Closure.UsageRevision < a.Allocation.Closure.UsageRevision {
		return nil, failure("revision_conflict", "older closure cannot overwrite current settlement")
	}
	r, err := c.Repository.Get(tx, "reservation", a.ReservationID)
	if err != nil {
		return nil, err
	}
	reservation, err := Decode[Reservation](r)
	if err != nil {
		return nil, err
	}
	bill := api.Billing{Source: api.SourceKey{OwnerID: a.Allocation.ReceiverID, Kind: "budget_allocation", ID: a.Allocation.AllocationID}, Revision: in.Closure.UsageRevision, Digest: Hash(in.Closure), Usage: in.Closure.FinalUsage, Final: true, ProofRef: in.Closure.ProofRef}
	if _, err = c.applyBilling(tx, ch, t, reservation, bill); err != nil {
		return nil, err
	}
	a.Allocation.State = "settled"
	a.Allocation.Revision++
	a.Allocation.FinalUsage = in.Closure.FinalUsage
	a.Allocation.Closure = &in.Closure
	a.UsageDigest = bill.Digest
	a.Settlement = nil
	causes := []string{}
	for _, v := range in.Closure.FinalUsage {
		for _, limit := range a.Allocation.Limits {
			if v.Unit == limit.Unit {
				cmp, _ := compare(v.Amount, limit.Limit)
				if cmp > 0 {
					causes = []string{"receiver_allocation_breach"}
				}
			}
		}
	}
	if len(causes) > 0 {
		a.Allocation.IncidentCauses = &causes
		pending := true
		a.Allocation.IncidentPending = &pending
	}
	if err = c.Repository.Put(tx, t, rec("allocation", a.Allocation.AllocationID, t.Task.TaskID, a.Allocation.Revision, "closed", false, a)); err != nil {
		return nil, err
	}
	return a.Allocation, nil
}
