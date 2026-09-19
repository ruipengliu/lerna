package simworkflow

import (
	"context"
	"fmt"
	"lerna/execution"
)

const Authority = "local-business-authority"

func (d *Driver) Resource() execution.ResourceRef {
	return execution.ResourceRef{Namespace: d.def.Kind, Kind: "business", Key: "ledger"}
}
func (d *Driver) ObserveResource(ctx context.Context, ref execution.ResourceRef) (execution.ResourceObservation, error) {
	o := execution.ResourceObservation{Resource: d.Resource(), Authority: Authority, Quiescent: true}
	if ref != d.Resource() {
		return o, fmt.Errorf("resource binding")
	}
	e := d.db.QueryRowContext(ctx, `SELECT control_version,business_version,intent FROM fence WHERE id=1`).Scan(&o.ControlVersion, &o.ResourceVersion, &o.Intent)
	return o, e
}
func (d *Driver) ApplyResourceControl(ctx context.Context, c execution.ResourceCommand) (execution.ResourceObservation, error) {
	if c.Resource != d.Resource() || c.Authority != Authority || c.OperationID == "" || len(c.OperationID) > 512 || c.Version < 2 || (c.Intent != "TAKEOVER" && c.Intent != "RESUME") {
		return execution.ResourceObservation{}, fmt.Errorf("invalid control")
	}
	tx, e := d.db.BeginTx(ctx, nil)
	if e != nil {
		return execution.ResourceObservation{}, e
	}
	defer tx.Rollback()
	var version, business, expected uint64
	var intent, op string
	if e = tx.QueryRowContext(ctx, `SELECT control_version,business_version,intent,operation,expected FROM fence WHERE id=1`).Scan(&version, &business, &intent, &op, &expected); e != nil {
		return execution.ResourceObservation{}, e
	}
	if version == c.Version {
		if intent != c.Intent || op != c.OperationID || expected != c.ExpectedResourceVersion {
			return execution.ResourceObservation{}, fmt.Errorf("identity conflict")
		}
	} else {
		if version > c.Version || c.Intent == "RESUME" && (intent != "TAKEOVER" || business != c.ExpectedResourceVersion) {
			return execution.ResourceObservation{}, fmt.Errorf("stale control")
		}
		if _, e = tx.ExecContext(ctx, `UPDATE fence SET control_version=?,intent=?,operation=?,expected=? WHERE id=1`, c.Version, c.Intent, c.OperationID, c.ExpectedResourceVersion); e != nil {
			return execution.ResourceObservation{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return execution.ResourceObservation{}, e
	}
	return d.ObserveResource(ctx, c.Resource)
}
