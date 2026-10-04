package decision_engine

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Store) ReadStop(ctx context.Context, token runtime.Tx, ref v.DecisionRef) (*decision.DecisionStop, error) {
	tx, err := s.core.SQL(ctx, token, owner(ref))
	if err != nil {
		return nil, err
	}
	var data []byte
	err = tx.QueryRowContext(ctx, `SELECT body FROM `+s.table("decision_stops")+` WHERE tenant_id=$1 AND owner_id=$2 AND decision_id=$3`, ref.TenantID, ref.OwnerID, ref.ID).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var stop decision.DecisionStop
	if err = json.Unmarshal(data, &stop); err != nil {
		return nil, err
	}
	if stop.Ref != ref {
		return nil, runtime.ErrScope
	}
	if err = stop.Validate(); err != nil {
		return nil, err
	}
	return &stop, nil
}
func (s *Store) SaveStop(ctx context.Context, token runtime.Tx, stop decision.DecisionStop) error {
	tx, err := s.core.SQL(ctx, token, owner(stop.Ref))
	if err != nil {
		return err
	}
	if err = stop.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(stop)
	if err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO `+s.table("decision_stops")+`(tenant_id,owner_id,decision_id,control_revision,body)VALUES($1,$2,$3,$4,$5)ON CONFLICT(tenant_id,owner_id,decision_id)DO UPDATE SET control_revision=excluded.control_revision,body=excluded.body WHERE decision_stops.control_revision<excluded.control_revision`, stop.Ref.TenantID, stop.Ref.OwnerID, stop.Ref.ID, stop.ControlBasis.ControlRevision, data)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err == nil && count != 1 {
		return runtime.ErrClaim
	}
	return err
}
func (s *Store) LockDecisionJob(ctx context.Context, token runtime.Tx, ref v.DecisionRef) (*runtime.Job, error) {
	tx, err := s.core.SQL(ctx, token, owner(ref))
	if err != nil {
		return nil, err
	}
	job := runtime.Job{Object: contract.ObjectRef{TenantID: contract.ID(ref.TenantID), OwnerID: contract.ID(ref.OwnerID), Kind: "decision", ID: contract.ID(ref.ID)}}
	err = tx.QueryRowContext(ctx, `SELECT job_id,phase,work_revision,completed_revision,state,due_at FROM `+s.table("jobs")+` WHERE tenant_id=$1 AND owner_id=$2 AND object_kind='decision' AND object_id=$3 AND phase='decide' FOR UPDATE`, ref.TenantID, ref.OwnerID, ref.ID).Scan(&job.ID, &job.Phase, &job.WorkRevision, &job.CompletedRevision, &job.State, &job.DueAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return &job, err
}
