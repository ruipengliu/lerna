package orchestrator

import (
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

// Automatic query budgets never prevent reception of original trusted debt.
func (c *Coordinator) BillingBinding(tx *durable.Tx, caller api.Caller, q api.BillingQuery) (Reservation, error) {
	if caller.TenantID != c.Config.Scope.TenantID || q.TaskRef.OrchestratorID != c.Config.Scope.OwnerID || caller.SourceOwnerID == "" {
		return Reservation{}, durable.ErrScope
	}
	if e := c.Repository.Read(tx); e != nil {
		return Reservation{}, e
	}
	row, e := c.Repository.Get(tx, "reservation", reserveID(q.ObjectKind, q.ObjectID))
	if e != nil {
		return Reservation{}, e
	}
	r, e := Decode[Reservation](row)
	if e != nil {
		return r, e
	}
	if r.TaskID != q.TaskRef.TaskID || r.ObjectOwnerID != q.ObjectOwnerID || caller.SourceOwnerID != r.ObjectOwnerID && (r.Source == nil || caller.SourceOwnerID != r.Source.OwnerID) {
		return r, failure("forbidden", "late bill must bind the original authenticated source")
	}
	return r, nil
}
func (c *Coordinator) ReconcileBilling(tx *durable.Tx, caller api.Caller, q api.BillingQuery, bill api.Billing) error {
	r, e := c.BillingBinding(tx, caller, q)
	if e != nil {
		return e
	}
	ch, t, e := c.load(tx, r.TaskID, false, nil)
	if e != nil {
		return e
	}
	if e = c.Ports.Authority.Current(tx.Context(), caller, api.Access{Method: "billing.fact.receive", TargetID: r.ObjectID, TaskID: r.TaskID, ContentRefs: []api.ContentRef{bill.ProofRef}}, ch.Now); e != nil {
		return e
	}
	if _, e = c.applyBilling(tx, ch, t, r, bill); e != nil {
		return e
	}
	if t.AllocationID != "" {
		queue(ch, t, "settle", "receiver", t.AllocationID)
	}
	return c.flush(tx, ch, nil, durable.Done())
}
