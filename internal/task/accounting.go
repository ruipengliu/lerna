package task

import (
	"context"
	"errors"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func sourceKey(kind string, source api.ObjectRef) string { return kind + "/" + refKey(source) }
func amountMap(amounts []api.Amount) map[string]string {
	m := map[string]string{}
	for _, a := range amounts {
		m[a.Unit] = a.Value
	}
	return m
}
func (s *Service) reserveTx(ctx context.Context, tx runtime.Tx, t *taskState, kind string, source api.ObjectRef, bound []api.Amount) (Reservation, error) {
	if e := api.ValidateAmounts(bound); e != nil {
		return Reservation{}, e
	}
	if len(bound) == 0 {
		return Reservation{}, api.E("forbidden", "cost_bound_required")
	}
	if e := runtime.CheckRef(tx.Scope(), source); e != nil {
		return Reservation{}, e
	}
	key := sourceKey(kind, source)
	existing, e := tx.LookupKey(ctx, reservations, key)
	if e == nil {
		var old Reservation
		_, e = tx.Get(ctx, reservations, existing.ObjectID, &old)
		if e != nil {
			return old, e
		}
		if old.TaskID != t.Task.TaskID {
			return old, api.E("idempotency_conflict", "billing_source_already_bound")
		}
		return old, api.E("idempotency_conflict", "billing_source_already_reserved")
	}
	if !api.IsCode(e, "not_found") {
		return Reservation{}, e
	}
	rows, e := tx.List(ctx, reservations, t.Task.TaskID, "", int(s.config.MaxRelations)+1)
	if e != nil {
		return Reservation{}, e
	}
	if uint64(len(rows)) >= s.config.MaxRelations {
		return Reservation{}, api.E("overloaded", "reservation_capacity")
	}
	r := Reservation{ReservationID: s.config.Identity.NewID("reservation"), Revision: 1, TaskID: t.Task.TaskID, SourceKind: kind, SourceRef: source, BindingState: "bound", State: "open", Units: []ReservationUnit{}, Incidents: []string{}}
	for _, a := range bound {
		index := -1
		for i, b := range t.Task.Budget {
			if b.Unit == a.Unit {
				index = i
				break
			}
		}
		if index < 0 {
			return r, api.E("forbidden", "budget_unit_unavailable")
		}
		b := &t.Task.Budget[index]
		sum, e := api.AddDecimal(b.Spent, b.Reserved)
		if e != nil {
			return r, e
		}
		sum, e = api.AddDecimal(sum, a.Value)
		if e != nil {
			return r, e
		}
		cmp, e := api.CompareDecimal(sum, b.Limit)
		if e != nil {
			return r, e
		}
		if cmp > 0 {
			return r, api.E("invalid_state", "budget_unavailable")
		}
		b.Reserved, e = api.AddDecimal(b.Reserved, a.Value)
		if e != nil {
			return r, e
		}
		r.Units = append(r.Units, ReservationUnit{Unit: a.Unit, OriginalReserved: a.Value, RemainingReserved: a.Value, AppliedCumulative: "0"})
	}
	if e = tx.Create(ctx, reservations, r.ReservationID, t.Task.TaskID, r); e != nil {
		return r, e
	}
	d, e := api.Digest(struct {
		TaskID string
		Kind   string
		Source api.ObjectRef
	}{t.Task.TaskID, kind, source})
	if e != nil {
		return r, e
	}
	if e = tx.Bind(ctx, reservations, key, r.ReservationID, d); e != nil {
		return r, e
	}
	t.Task.AccountingOpen = true
	if e = queueJob(ctx, tx, JobBilling, "billing/"+r.ReservationID, tx.Scope().Ref(r.ReservationID, 1)); e != nil {
		return r, e
	}
	return r, nil
}
func (s *Service) adjustBudgetTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in BudgetInput) (TaskOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return TaskOutput{}, e
	}
	t, e := getTask(ctx, tx, in.TaskID)
	if e != nil {
		return TaskOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return TaskOutput{}, e
	}
	if e = cas(c, t); e != nil {
		return TaskOutput{}, e
	}
	if terminal(t) {
		return TaskOutput{}, api.E("invalid_state", "target_terminal")
	}
	if e = api.ValidateAmounts(in.Limits); e != nil {
		return TaskOutput{}, e
	}
	if e = checkLimits(in.Limits, t.Policy.BudgetLimits); e != nil {
		return TaskOutput{}, e
	}
	if len(in.Limits) != len(t.Task.Budget) {
		return TaskOutput{}, invalid("all_budget_units_required")
	}
	if e = s.authorize(ctx, tx, auth, "task.adjust_budget", []api.ContentRef{in.ReasonRef}, nil); e != nil {
		return TaskOutput{}, e
	}
	limits := amountMap(in.Limits)
	for i := range t.Task.Budget {
		b := &t.Task.Budget[i]
		limit, ok := limits[b.Unit]
		if !ok {
			return TaskOutput{}, invalid("all_budget_units_required")
		}
		committed, e := api.AddDecimal(b.Spent, b.Reserved)
		if e != nil {
			return TaskOutput{}, e
		}
		cmp, e := api.CompareDecimal(limit, committed)
		if e != nil {
			return TaskOutput{}, e
		}
		if cmp < 0 {
			return TaskOutput{}, api.E("invalid_state", "commitment_exceeds_limit")
		}
		b.Limit = limit
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return TaskOutput{}, e
	}
	if e = queueJob(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, t)); e != nil {
		return TaskOutput{}, e
	}
	return output(tx, t), nil
}
func (s *Service) BillingTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in BillingInput) (BillingOutput, error) {
	if e := target(c, in.TaskID); e != nil {
		return BillingOutput{}, e
	}
	if e := runtime.CheckRef(tx.Scope(), in.SourceRef); e != nil {
		return BillingOutput{}, e
	}
	if !auth.HasRole("service") && auth.SubjectID != in.SourceRef.OwnerID {
		return BillingOutput{}, api.E("forbidden", "billing_owner_required")
	}
	key, e := tx.LookupKey(ctx, reservations, sourceKey(in.SourceKind, in.SourceRef))
	if e != nil {
		return BillingOutput{}, api.E("invalid_request", "unknown_billing_binding")
	}
	var r Reservation
	if _, e = tx.Get(ctx, reservations, key.ObjectID, &r); e != nil {
		return BillingOutput{}, e
	}
	if r.TaskID != in.TaskID {
		return BillingOutput{}, api.E("forbidden", "unknown_billing_binding")
	}
	if in.UsageRevision == 0 {
		return BillingOutput{}, invalid("invalid_usage_revision")
	}
	if r.AppliedUsageRevision == in.UsageRevision && r.UsageDigest != "" && r.UsageDigest != in.UsageDigest {
		return BillingOutput{}, api.E("idempotency_conflict", "digest_conflict")
	}
	job, e := raise(ctx, tx, JobBilling, "billing/"+r.ReservationID, tx.Scope().Ref(r.ReservationID, r.Revision))
	if e != nil {
		return BillingOutput{}, e
	}
	return BillingOutput{SourceRef: in.SourceRef, WorkRevision: job.WorkRevision}, nil
}

// UsageDigest 固定完整账单事实；发送方与接收方共用准确累计用量，不截预算。
func UsageDigest(usage api.UsageSnapshot) (string, error) {
	usage.UsageDigest = ""
	return api.Digest(usage)
}
func (s *Service) ReconcileUsage(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, kind string, usage api.UsageSnapshot) (Reservation, error) {
	var out Reservation
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error {
		var e error
		out, e = s.ReconcileUsageTx(ctx, tx, auth, kind, usage)
		return e
	})
	return out, err
}
func (s *Service) reconcileUsageTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, kind string, u api.UsageSnapshot) (Reservation, error) {
	if !auth.HasRole("service") && auth.SubjectID != u.SourceRef.OwnerID {
		return Reservation{}, api.E("forbidden", "billing_owner_required")
	}
	if e := runtime.CheckRef(tx.Scope(), u.SourceRef); e != nil {
		return Reservation{}, e
	}
	if e := api.ValidateRecord("UsageSnapshot", u); e != nil {
		return Reservation{}, e
	}
	if e := api.ValidateAmounts(u.Cumulative); e != nil {
		return Reservation{}, e
	}
	digest, e := UsageDigest(u)
	if e != nil {
		return Reservation{}, e
	}
	if digest != u.UsageDigest {
		return Reservation{}, api.E("idempotency_conflict", "digest_conflict")
	}
	key, e := tx.LookupKey(ctx, reservations, sourceKey(kind, u.SourceRef))
	if e != nil {
		return Reservation{}, api.E("invalid_request", "unknown_billing_binding")
	}
	t, r, e := reservationForTaskTx(ctx, tx, key.ObjectID)
	if e != nil {
		return r, e
	}
	if u.UsageRevision < r.AppliedUsageRevision {
		return r, nil
	}
	if u.UsageRevision == r.AppliedUsageRevision {
		if r.UsageDigest != u.UsageDigest {
			return r, api.E("idempotency_conflict", "digest_conflict")
		}
		return r, nil
	}
	if r.State == "settled" && (!u.SpendingClosed || !u.UsageFinal) {
		return r, api.E("invalid_state", "closed_spending_reopened")
	}
	cumulative := amountMap(u.Cumulative)
	if len(cumulative) != len(r.Units) {
		return r, invalid("all_usage_units_required")
	}
	for i := range r.Units {
		unit := &r.Units[i]
		value, ok := cumulative[unit.Unit]
		if !ok {
			return r, invalid("all_usage_units_required")
		}
		cmp, e := api.CompareDecimal(value, unit.AppliedCumulative)
		if e != nil {
			return r, e
		}
		if cmp < 0 {
			return r, api.E("invalid_request", "cumulative_usage_decreased")
		}
		delta, e := api.SubDecimal(value, unit.AppliedCumulative)
		if e != nil {
			return r, e
		}
		bi := -1
		for j, b := range t.Task.Budget {
			if b.Unit == unit.Unit {
				bi = j
				break
			}
		}
		if bi < 0 {
			return r, api.E("dependency_unavailable", "budget_binding_missing")
		}
		b := &t.Task.Budget[bi]
		b.Spent, e = api.AddDecimal(b.Spent, delta)
		if e != nil {
			return r, e
		}
		remaining := "0"
		if !u.SpendingClosed || !u.UsageFinal {
			cmp, e = api.CompareDecimal(unit.OriginalReserved, value)
			if e != nil {
				return r, e
			}
			if cmp > 0 {
				remaining, e = api.SubDecimal(unit.OriginalReserved, value)
				if e != nil {
					return r, e
				}
			}
		}
		released, e := api.SubDecimal(unit.RemainingReserved, remaining)
		if e != nil {
			return r, api.E("invalid_request", "closed_spending_reopened")
		}
		b.Reserved, e = api.SubDecimal(b.Reserved, released)
		if e != nil {
			return r, e
		}
		unit.RemainingReserved = remaining
		unit.AppliedCumulative = value
		if cmp, e = api.CompareDecimal(value, unit.OriginalReserved); e != nil {
			return r, e
		} else if cmp > 0 {
			incident := "provider_bound_breach"
			if kind == "budget_allocation" {
				incident = "receiver_allocation_breach"
			}
			r.Incidents = appendUnique(r.Incidents, incident)
		}
	}
	r.AppliedUsageRevision = u.UsageRevision
	r.UsageDigest = u.UsageDigest
	if u.SpendingClosed && u.UsageFinal {
		r.State = "settled"
	}
	r.Revision++
	if e = tx.Put(ctx, reservations, r.ReservationID, r.Revision-1, r); e != nil {
		return r, e
	}
	rows, e := tx.List(ctx, reservations, t.Task.TaskID, "", int(s.config.MaxRelations)+1)
	if e != nil {
		return r, e
	}
	if uint64(len(rows)) > s.config.MaxRelations {
		return r, api.E("dependency_unavailable", "reservation_index_capacity")
	}
	open := false
	for _, row := range rows {
		var original Reservation
		if e = row.Decode(&original); e != nil {
			return r, e
		}
		if original.State != "settled" {
			open = true
		}
	}
	t.Task.AccountingOpen = open
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return r, e
	}
	if t.IncomingAllocationID != "" {
		if e = s.refreshIncomingTx(ctx, tx, t); e != nil {
			return r, e
		}
	}
	return r, nil
}
func appendUnique(values []string, value string) []string {
	for _, v := range values {
		if v == value {
			return values
		}
	}
	return append(values, value)
}
func (s *Service) BudgetRead(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, taskID string) (BudgetReadOutput, error) {
	t, e := s.readState(ctx, store, scope, auth, taskID, 0)
	if e != nil {
		return BudgetReadOutput{}, e
	}
	rows, e := store.List(ctx, scope, reservations, taskID, "", int(s.config.MaxRelations)+1)
	if e != nil {
		return BudgetReadOutput{}, e
	}
	if uint64(len(rows)) > s.config.MaxRelations {
		return BudgetReadOutput{}, api.E("dependency_unavailable", "reservation_index_capacity")
	}
	out := BudgetReadOutput{Budget: t.Task.Budget, Reservations: []Reservation{}, AccountingOpen: t.Task.AccountingOpen, GrossSpent: []api.Amount{}, CreditTotal: []api.Amount{}, NetCost: []api.Amount{}}
	for _, row := range rows {
		var r Reservation
		if e = row.Decode(&r); e != nil {
			return out, e
		}
		out.Reservations = append(out.Reservations, r)
	}
	credits := amountMap(t.Credits)
	for _, b := range t.Task.Budget {
		credit := credits[b.Unit]
		if credit == "" {
			credit = "0"
		}
		net, e := api.SubDecimal(b.Spent, credit)
		if e != nil {
			return out, e
		}
		out.GrossSpent = append(out.GrossSpent, api.Amount{Unit: b.Unit, Value: b.Spent})
		out.CreditTotal = append(out.CreditTotal, api.Amount{Unit: b.Unit, Value: credit})
		out.NetCost = append(out.NetCost, api.Amount{Unit: b.Unit, Value: net})
	}
	return out, nil
}
func (s *Service) adjustmentTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in AdjustmentInput) (AdjustmentOutput, error) {
	if c.TargetID != in.AdjustmentID {
		return AdjustmentOutput{}, invalid("target_mismatch")
	}
	if in.Kind != "refund" && in.Kind != "credit" {
		return AdjustmentOutput{}, invalid("adjustment_kind")
	}
	if !auth.HasRole("service") && auth.SubjectID != in.OriginalSourceRef.OwnerID {
		return AdjustmentOutput{}, api.E("forbidden", "billing_owner_required")
	}
	if len(in.EvidenceRefs) == 0 {
		return AdjustmentOutput{}, invalid("adjustment_evidence_required")
	}
	if e := api.ValidateAmounts([]api.Amount{{Unit: in.Unit, Value: in.Amount}}); e != nil {
		return AdjustmentOutput{}, e
	}
	key, e := tx.LookupKey(ctx, reservations, sourceKeyFromAny(ctx, tx, in.OriginalSourceRef))
	if e != nil {
		return AdjustmentOutput{}, api.E("invalid_request", "unknown_billing_binding")
	}
	t, _, e := reservationForTaskTx(ctx, tx, key.ObjectID)
	if e != nil {
		return AdjustmentOutput{}, e
	}
	if e = s.authorize(ctx, tx, auth, "billing.adjustment", in.EvidenceRefs, []api.ObjectRef{in.OriginalSourceRef}); e != nil {
		return AdjustmentOutput{}, e
	}
	semantic := refKey(in.OriginalSourceRef) + "/" + in.ProviderAdjustmentKey
	digest, e := api.Digest(in)
	if e != nil {
		return AdjustmentOutput{}, e
	}
	old, e := tx.LookupKey(ctx, adjustments, semantic)
	if e == nil {
		if old.Digest != digest {
			return AdjustmentOutput{}, api.E("idempotency_conflict", "digest_conflict")
		}
		var a BillingAdjustment
		_, e = tx.Get(ctx, adjustments, old.ObjectID, &a)
		return AdjustmentOutput{AdjustmentRef: tx.Scope().Ref(a.AdjustmentID, a.Revision), State: a.State}, e
	}
	if !api.IsCode(e, "not_found") {
		return AdjustmentOutput{}, e
	}
	a := BillingAdjustment{AdjustmentInput: in, Revision: 1, State: "pending"}
	if e = tx.Create(ctx, adjustments, in.AdjustmentID, t.Task.TaskID, a); e != nil {
		return AdjustmentOutput{}, e
	}
	if e = tx.Bind(ctx, adjustments, semantic, in.AdjustmentID, digest); e != nil {
		return AdjustmentOutput{}, e
	}
	if e = queueJob(ctx, tx, JobAdjustment, "adjustment/"+in.AdjustmentID, tx.Scope().Ref(in.AdjustmentID, 1)); e != nil {
		return AdjustmentOutput{}, e
	}
	return AdjustmentOutput{AdjustmentRef: tx.Scope().Ref(in.AdjustmentID, 1), State: "pending"}, nil
}
func sourceKeyFromAny(ctx context.Context, tx runtime.Tx, ref api.ObjectRef) string {
	for _, kind := range []string{"brain_decision", "execution_operation", "grant_use", "budget_allocation"} {
		if _, e := tx.LookupKey(ctx, reservations, sourceKey(kind, ref)); e == nil {
			return sourceKey(kind, ref)
		}
	}
	return "missing"
}

// VerifyAdjustment 在可信提供方核验完成后调用；pending 永不改 gross_spent 或预算。
func (s *Service) VerifyAdjustment(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string, approved bool) (BillingAdjustment, error) {
	var out BillingAdjustment
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error { return s.verifyAdjustmentTx(ctx, tx, auth, id, approved, &out) })
	return out, err
}
func (s *Service) verifyAdjustmentTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, id string, approved bool, out *BillingAdjustment) error {

	if !auth.HasRole("service") && !auth.HasRole("billing") {
		return api.E("forbidden", "billing_verifier_required")
	}
	var birth BillingAdjustment
	if e := tx.GetVersion(ctx, adjustments, id, 1, &birth); e != nil {
		return e
	}
	key, e := tx.LookupKey(ctx, reservations, sourceKeyFromAny(ctx, tx, birth.OriginalSourceRef))
	if e != nil {
		return e
	}
	t, r, e := reservationForTaskTx(ctx, tx, key.ObjectID)
	if e != nil {
		return e
	}
	var a BillingAdjustment
	if _, e = tx.Get(ctx, adjustments, id, &a); e != nil {
		return e
	}
	if !api.Equal(a.AdjustmentInput, birth.AdjustmentInput) {
		return api.E("idempotency_conflict", "adjustment_source_changed")
	}
	if a.State != "pending" {
		*out = a
		return nil
	}
	a.State = "rejected"
	if approved {
		charge := ""
		for _, u := range r.Units {
			if u.Unit == a.Unit {
				charge = u.AppliedCumulative
			}
		}
		if charge == "" {
			return invalid("adjustment_unknown_unit")
		}
		rows, e := tx.List(ctx, adjustments, t.Task.TaskID, "", int(s.config.MaxRelations)+1)
		if e != nil {
			return e
		}
		if uint64(len(rows)) > s.config.MaxRelations {
			return api.E("dependency_unavailable", "adjustment_index_capacity")
		}
		total := a.Amount
		for _, row := range rows {
			var prior BillingAdjustment
			if e = row.Decode(&prior); e != nil {
				return e
			}
			if prior.State == "applied" && prior.Unit == a.Unit && refKey(prior.OriginalSourceRef) == refKey(a.OriginalSourceRef) {
				total, e = api.AddDecimal(total, prior.Amount)
				if e != nil {
					return e
				}
			}
		}
		cmp, e := api.CompareDecimal(total, charge)
		if e != nil {
			return e
		}
		if cmp > 0 {
			return api.E("invalid_request", "adjustment_exceeds_charge")
		}
		credits := amountMap(t.Credits)
		old := credits[a.Unit]
		if old == "" {
			old = "0"
		}
		credits[a.Unit], e = api.AddDecimal(old, a.Amount)
		if e != nil {
			return e
		}
		t.Credits = []api.Amount{}
		for unit, value := range credits {
			t.Credits = append(t.Credits, api.Amount{Unit: unit, Value: value})
		}
		sort.Slice(t.Credits, func(i, j int) bool { return t.Credits[i].Unit < t.Credits[j].Unit })
		if e = s.saveTask(ctx, tx, &t); e != nil {
			return e
		}
		a.State = "applied"
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return e
	}
	a.VerifiedBy = auth.SubjectID
	a.VerifiedAt = api.Time(now)
	a.Revision++
	if e = tx.Put(ctx, adjustments, id, a.Revision-1, a); e != nil {
		return e
	}
	*out = a
	return nil
}

func confirmedNotFound(err error) bool {
	return errors.Is(err, runtime.ErrNotFound) || api.IsCode(err, "not_found")
}

const JobAdjustment = "task.adjustment"

func (s *Service) adjustmentJob(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var a BillingAdjustment
	if _, e := store.Read(ctx, scope, adjustments, work.Job.SourceRef.ObjectID, 0, &a); e != nil {
		return e
	}
	if a.State != "pending" {
		return s.finish(ctx, store, scope, work, runtime.Done(), nil)
	}
	if s.ports.AdjustmentVerifier == nil {
		return s.wait(ctx, store, scope, work)
	}
	if e := s.preIO(ctx, store, scope, work); e != nil {
		return e
	}
	approved, e := s.ports.AdjustmentVerifier.VerifyAdjustment(ctx, scope, a)
	if e != nil {
		if deferred(e) {
			return s.wait(ctx, store, scope, work)
		}
		return e
	}
	return s.finish(ctx, store, scope, work, runtime.Done(), func(tx runtime.Tx) error {
		var out BillingAdjustment
		return s.verifyAdjustmentTx(ctx, tx, serviceAuth(scope), a.AdjustmentID, approved, &out)
	})
}
