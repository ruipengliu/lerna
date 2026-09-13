package simresource

import (
	"context"
	"fmt"
	"lerna/execution"
)

const authority = "local-resource-authority"

func (d *Driver) ref() execution.ResourceRef {
	return execution.ResourceRef{Namespace: "local", Kind: "counter", Key: d.key}
}
func (d *Driver) ObserveResource(ctx context.Context, ref execution.ResourceRef) (execution.ResourceObservation, error) {
	o := execution.ResourceObservation{Resource: d.ref(), Authority: authority}
	if ref != d.ref() {
		return o, fmt.Errorf("resource")
	}
	var running int
	e := d.db.QueryRowContext(ctx, `SELECT control_version,version,intent,(SELECT count(*) FROM jobs WHERE state='running') FROM resource WHERE id=1`).Scan(&o.ControlVersion, &o.ResourceVersion, &o.Intent, &running)
	o.Quiescent = running == 0
	return o, e
}

// ApplyResourceControl serializes control and business changes in the target database.
// Its generation fence covers delayed requests even when no job was created yet.
func (d *Driver) ApplyResourceControl(ctx context.Context, c execution.ResourceCommand) (execution.ResourceObservation, error) {
	if c.Resource != d.ref() || c.Authority != authority || c.OperationID == "" || len(c.OperationID) > 512 || c.Version < 2 || (c.Intent != "TAKEOVER" && c.Intent != "RESUME") {
		return execution.ResourceObservation{}, fmt.Errorf("control")
	}
	tx, e := d.db.BeginTx(ctx, nil)
	if e != nil {
		return execution.ResourceObservation{}, e
	}
	defer tx.Rollback()
	var version, business, expected uint64
	var intent, op string
	if e = tx.QueryRowContext(ctx, `SELECT control_version,version,intent,control_op,control_expected FROM resource WHERE id=1`).Scan(&version, &business, &intent, &op, &expected); e != nil {
		return execution.ResourceObservation{}, e
	}
	if c.Version == version {
		if c.Intent != intent || c.OperationID != op || c.ExpectedResourceVersion != expected {
			return execution.ResourceObservation{}, fmt.Errorf("identity conflict")
		}
	} else {
		if c.Version < version {
			return execution.ResourceObservation{}, fmt.Errorf("stale control")
		}
		if c.Intent == "RESUME" && (intent != "TAKEOVER" || business != c.ExpectedResourceVersion) {
			return execution.ResourceObservation{}, fmt.Errorf("stale observation")
		}
		if c.Intent == "TAKEOVER" {
			now, err := d.clock.Now()
			if err != nil {
				return execution.ResourceObservation{}, err
			}
			if _, e = tx.ExecContext(ctx, `UPDATE jobs SET state='cancelled',revision=revision+1,completed_at=?,value=(SELECT value FROM resource WHERE id=1),version=(SELECT version FROM resource WHERE id=1) WHERE state='running'`, now.UnixNano()); e != nil {
				return execution.ResourceObservation{}, e
			}
		}
		if _, e = tx.ExecContext(ctx, `UPDATE resource SET control_version=?,intent=?,control_op=?,control_expected=? WHERE id=1`, c.Version, c.Intent, c.OperationID, c.ExpectedResourceVersion); e != nil {
			return execution.ResourceObservation{}, e
		}
	}
	if e = tx.Commit(); e != nil {
		return execution.ResourceObservation{}, e
	}
	return d.ObserveResource(ctx, c.Resource)
}

// UserChange represents a human modifying the independent target during takeover.
func (d *Driver) UserChange(ctx context.Context, value int64) error {
	if value < 0 || value > 1000000 {
		return fmt.Errorf("value")
	}
	r, e := d.db.ExecContext(ctx, `UPDATE resource SET value=?,version=version+1 WHERE id=1 AND intent='TAKEOVER'`, value)
	if e != nil {
		return e
	}
	n, e := r.RowsAffected()
	if e == nil && n != 1 {
		return fmt.Errorf("not taken over")
	}
	return e
}
