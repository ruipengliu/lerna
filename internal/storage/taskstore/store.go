// Package taskstore implements Orchestrator repositories with generated,
// independent PostgreSQL/SQLite SQL inside the original restricted transaction.
package taskstore

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
	"github.com/ruipengliu/lerna/internal/storage/sqlstore"
)

type Queries struct {
	GetMany, PutMany, DeselectKeys, Routes                                                  string
	Task, LockTask, SaveTask, Tree, Chain, List, Recovery                                   string
	Balances, LockBalances, SaveBalance                                                     string
	Record, Records, OpenRecords, SaveRecord, Deselect                                      string
	GateCreate, LockGate, ReadGate, GateAdvance                                             string
	CapacityCreate, LockCapacity, CapacityChange                                            string
	Receiver, SaveReceiver                                                                  string
	SaveRoute, ScheduleCreate, ScheduleLock, ScheduleAdvance, ScheduleTurns, FairCandidates string
	BatchLockTasks, BatchLockBalances, CurrentRecords, OpenTaskRecords, SaveBalances        string
}
type Store struct {
	p durable.Participant
	q Queries
}

func New(p durable.Participant, q Queries) *Store { return &Store{p: p, q: q} }
func (r *Store) Participant() durable.Participant { return r.p }

func (r *Store) Route(tx *durable.Tx, t *o.TaskState, w o.WorkRef) error {
	return use(tx, r, taskKey(t), func(db sqlstore.DBTX) error {
		key := o.WorkKey(w)
		_, e := db.ExecContext(tx.Context(), r.q.SaveRoute, tx.Scope().TenantID, tx.Scope().OwnerID, w.Kind, key.Responsibility, w.SubjectID, w.TaskID, w.ProviderID, w.ResourceID)
		return e
	})
}
func (r *Store) Read(tx *durable.Tx) error {
	return tx.Lock(r.p, "00-read", func(any) error { return nil })
}
func taskKey(t *o.TaskState) string { return fmt.Sprintf("10-task-%02d-%s", t.Depth, t.Task.TaskID) }
func budgetKey(id string) string    { return "30-budget-" + id }
func gateKey(id string) string      { return "40-gate-" + id }
func use(tx *durable.Tx, r *Store, key string, fn func(sqlstore.DBTX) error) error {
	return tx.Use(r.p, key, func(h any) error {
		db, ok := h.(sqlstore.DBTX)
		if !ok {
			return durable.ErrTx
		}
		return fn(db)
	})
}
func (r *Store) Task(tx *durable.Tx, id string) (*o.TaskState, error) {
	var out *o.TaskState
	err := use(tx, r, "00-read", func(db sqlstore.DBTX) error {
		var data string
		err := db.QueryRowContext(tx.Context(), r.q.Task, tx.Scope().TenantID, tx.Scope().OwnerID, id).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out = &o.TaskState{}
		return json.Unmarshal([]byte(data), out)
	})
	return out, err
}
func (r *Store) tasks(tx *durable.Tx, q string, args ...any) ([]*o.TaskState, error) {
	out := []*o.TaskState{}
	err := use(tx, r, "00-read", func(db sqlstore.DBTX) error {
		rows, err := db.QueryContext(tx.Context(), q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var data string
			if err = rows.Scan(&data); err != nil {
				return err
			}
			var t o.TaskState
			if err = json.Unmarshal([]byte(data), &t); err != nil {
				return err
			}
			out = append(out, &t)
		}
		return rows.Err()
	})
	return out, err
}
func (r *Store) Chain(tx *durable.Tx, id string) ([]*o.TaskState, error) {
	return r.tasks(tx, r.q.Chain, tx.Scope().TenantID, tx.Scope().OwnerID, id)
}
func (r *Store) Tree(tx *durable.Tx, id string, limit int) ([]*o.TaskState, error) {
	if limit < 1 || limit > 101 {
		return nil, durable.ErrPrecondition
	}
	return r.tasks(tx, r.q.Tree, tx.Scope().TenantID, tx.Scope().OwnerID, id, limit)
}
func (r *Store) List(tx *durable.Tx, upper, last int64, id string, limit int) ([]*o.TaskState, error) {
	if limit < 1 || limit > 101 {
		return nil, durable.ErrPrecondition
	}
	return r.tasks(tx, r.q.List, tx.Scope().TenantID, tx.Scope().OwnerID, upper, last, id, limit)
}
func (r *Store) Recovery(tx *durable.Tx, after string, limit int) ([]*o.TaskState, error) {
	if limit < 1 || limit > 101 {
		return nil, durable.ErrPrecondition
	}
	return r.tasks(tx, r.q.Recovery, tx.Scope().TenantID, tx.Scope().OwnerID, after, limit)
}
func (r *Store) LockTasks(tx *durable.Tx, tasks []*o.TaskState) ([]*o.TaskState, error) {
	values := append([]*o.TaskState(nil), tasks...)
	sort.Slice(values, func(i, j int) bool { return taskKey(values[i]) < taskKey(values[j]) })
	unique := []*o.TaskState{}
	ids := []string{}
	seen := map[string]bool{}
	for _, t := range values {
		if !seen[t.Task.TaskID] {
			seen[t.Task.TaskID] = true
			unique = append(unique, t)
			ids = append(ids, t.Task.TaskID)
		}
	}
	if len(unique) == 0 || len(unique) > 200 {
		return nil, durable.ErrPrecondition
	}
	payload, _ := json.Marshal(ids)
	current := map[string]*o.TaskState{}
	read := false
	query := func(db sqlstore.DBTX) error {
		read = true
		rows, e := db.QueryContext(tx.Context(), r.q.BatchLockTasks, tx.Scope().TenantID, tx.Scope().OwnerID, string(payload))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var data string
			if e = rows.Scan(&data); e != nil {
				return e
			}
			var t o.TaskState
			if e = json.Unmarshal([]byte(data), &t); e != nil {
				return e
			}
			current[t.Task.TaskID] = &t
		}
		return rows.Err()
	}
	for i, t := range unique {
		if e := tx.Lock(r.p, taskKey(t), func(h any) error {
			if i == 0 {
				return query(h.(sqlstore.DBTX))
			}
			return nil
		}); e != nil {
			return nil, e
		}
	}
	if !read {
		if e := use(tx, r, taskKey(unique[0]), query); e != nil {
			return nil, e
		}
	}
	out := []*o.TaskState{}
	for _, t := range unique {
		v := current[t.Task.TaskID]
		if v == nil {
			v = t
		} else if v.Depth != t.Depth || v.ParentID != t.ParentID {
			return nil, durable.ErrInvariant
		}
		out = append(out, v)
	}
	return out, nil
}
func (r *Store) SaveTask(tx *durable.Tx, t *o.TaskState) error {
	if t.Task.TenantID != tx.Scope().TenantID || t.Task.OrchestratorID != tx.Scope().OwnerID {
		return durable.ErrScope
	}
	return use(tx, r, taskKey(t), func(db sqlstore.DBTX) error {
		b, err := json.Marshal(t)
		if err != nil || len(b) > 1<<20 {
			return durable.ErrPrecondition
		}
		result, err := db.ExecContext(tx.Context(), r.q.SaveTask, tx.Scope().TenantID, tx.Scope().OwnerID, t.Task.TaskID, t.SubjectID, t.ParentID, t.Depth, t.CreatedAt, t.Task.Revision, t.Task.Status, string(b))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return durable.ErrInvariant
		}
		return nil
	})
}
func (r *Store) LockCapacity(tx *durable.Tx, subjects []string) error {
	x := append([]string(nil), subjects...)
	sort.Strings(x)
	for _, id := range x {
		if err := tx.Lock(r.p, "20-capacity-"+id, func(h any) error {
			db := h.(sqlstore.DBTX)
			if _, err := db.ExecContext(tx.Context(), r.q.CapacityCreate, tx.Scope().TenantID, tx.Scope().OwnerID, id); err != nil {
				return err
			}
			var count int64
			return db.QueryRowContext(tx.Context(), r.q.LockCapacity, tx.Scope().TenantID, tx.Scope().OwnerID, id).Scan(&count)
		}); err != nil {
			return err
		}
	}
	return nil
}
func (r *Store) Capacity(tx *durable.Tx, id string, delta int64, limit int) error {
	var changed bool
	err := use(tx, r, "20-capacity-"+id, func(db sqlstore.DBTX) error {
		result, err := db.ExecContext(tx.Context(), r.q.CapacityChange, tx.Scope().TenantID, tx.Scope().OwnerID, id, delta, limit)
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		changed = n == 1
		return err
	})
	if err != nil {
		return err
	}
	if !changed {
		return &api.Failure{Detail: api.Error{Code: "overloaded", Message: "active task capacity exceeded", Retry: "same_command"}}
	}
	return nil
}
func (r *Store) LockBalances(tx *durable.Tx, ids []string) (map[string][]api.BudgetBalance, error) {
	keys := append([]string(nil), ids...)
	sort.Strings(keys)
	if len(keys) == 0 || len(keys) > 200 {
		return nil, durable.ErrPrecondition
	}
	payload, _ := json.Marshal(keys)
	out := map[string][]api.BudgetBalance{}
	read := false
	query := func(db sqlstore.DBTX) error {
		read = true
		rows, e := db.QueryContext(tx.Context(), r.q.BatchLockBalances, tx.Scope().TenantID, tx.Scope().OwnerID, string(payload))
		if e != nil {
			return e
		}
		defer rows.Close()
		for rows.Next() {
			var id, unit, limit, spent, reserved string
			if e = rows.Scan(&id, &unit, &limit, &spent, &reserved); e != nil {
				return e
			}
			if len(out[id]) >= 100 {
				return durable.ErrPrecondition
			}
			out[id] = append(out[id], api.BudgetBalance{Unit: unit, Limit: api.Amount{Unit: unit, Amount: limit}, Spent: api.Amount{Unit: unit, Amount: spent}, Reserved: api.Amount{Unit: unit, Amount: reserved}})
		}
		return rows.Err()
	}
	for i, id := range keys {
		if e := tx.Lock(r.p, budgetKey(id), func(h any) error {
			if i == 0 {
				return query(h.(sqlstore.DBTX))
			}
			return nil
		}); e != nil {
			return nil, e
		}
	}
	if !read {
		if e := use(tx, r, budgetKey(keys[0]), query); e != nil {
			return nil, e
		}
	}
	return out, nil
}
func (r *Store) SaveBalances(tx *durable.Tx, id string, balances []api.BudgetBalance) error {
	if len(balances) == 0 || len(balances) > 100 {
		return durable.ErrPrecondition
	}
	body, e := json.Marshal(balances)
	if e != nil {
		return e
	}
	return use(tx, r, budgetKey(id), func(db sqlstore.DBTX) error {
		_, e := db.ExecContext(tx.Context(), r.q.SaveBalances, tx.Scope().TenantID, tx.Scope().OwnerID, id, string(body))
		return e
	})
}
func (r *Store) Balances(tx *durable.Tx, id string) ([]api.BudgetBalance, error) {
	out := []api.BudgetBalance{}
	err := use(tx, r, "00-read", func(db sqlstore.DBTX) error {
		rows, err := db.QueryContext(tx.Context(), r.q.Balances, tx.Scope().TenantID, tx.Scope().OwnerID, id)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var unit, limit, spent, reserved string
			if err = rows.Scan(&unit, &limit, &spent, &reserved); err != nil {
				return err
			}
			b := api.BudgetBalance{Unit: unit, Limit: api.Amount{Unit: unit, Amount: limit}, Spent: api.Amount{Unit: unit, Amount: spent}, Reserved: api.Amount{Unit: unit, Amount: reserved}}
			out = append(out, b)
		}
		return rows.Err()
	})
	return out, err
}
func (r *Store) SaveBalance(tx *durable.Tx, id string, b api.BudgetBalance) error {
	return use(tx, r, budgetKey(id), func(db sqlstore.DBTX) error {
		_, err := db.ExecContext(tx.Context(), r.q.SaveBalance, tx.Scope().TenantID, tx.Scope().OwnerID, id, b.Unit, b.Limit.Amount, b.Spent.Amount, b.Reserved.Amount)
		return err
	})
}

type scanner interface{ Scan(...any) error }

func scanRecord(s scanner) (o.Record, error) {
	var v o.Record
	var data string
	err := s.Scan(&v.Kind, &v.ID, &v.TaskID, &v.Revision, &v.State, &v.CurrentKey, &v.Immutable, &data)
	v.Data = json.RawMessage(data)
	return v, err
}
func (r *Store) Get(tx *durable.Tx, kind, id string) (*o.Record, error) {
	var out *o.Record
	err := use(tx, r, "00-read", func(db sqlstore.DBTX) error {
		v, err := scanRecord(db.QueryRowContext(tx.Context(), r.q.Record, tx.Scope().TenantID, tx.Scope().OwnerID, kind, id))
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out = &v
		return nil
	})
	return out, err
}
func (r *Store) records(tx *durable.Tx, q string, args ...any) ([]o.Record, error) {
	out := []o.Record{}
	err := use(tx, r, "00-read", func(db sqlstore.DBTX) error {
		rows, err := db.QueryContext(tx.Context(), q, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			v, err := scanRecord(rows)
			if err != nil {
				return err
			}
			out = append(out, v)
		}
		return rows.Err()
	})
	return out, err
}
func (r *Store) Records(tx *durable.Tx, f o.Filter) ([]o.Record, error) {
	if f.Limit < 1 || f.Limit > 101 {
		return nil, durable.ErrPrecondition
	}
	if f.After == "" && f.State == "" && f.Current == "@current" {
		return r.records(tx, r.q.CurrentRecords, tx.Scope().TenantID, tx.Scope().OwnerID, f.TaskID, f.Kind, f.Limit)
	}
	if f.After == "" && f.Current == "" && f.State == "open" {
		return r.records(tx, r.q.OpenTaskRecords, tx.Scope().TenantID, tx.Scope().OwnerID, f.TaskID, f.Kind, f.Limit)
	}
	return r.records(tx, r.q.Records, tx.Scope().TenantID, tx.Scope().OwnerID, f.TaskID, f.Kind, f.After, f.State, f.Current, f.Limit)
}
func (r *Store) OpenRecords(tx *durable.Tx, kind, after string, limit int) ([]o.Record, error) {
	if limit < 1 || limit > 101 {
		return nil, durable.ErrPrecondition
	}
	return r.records(tx, r.q.OpenRecords, tx.Scope().TenantID, tx.Scope().OwnerID, kind, after, limit)
}
func (r *Store) put(tx *durable.Tx, key string, v o.Record) error {
	return use(tx, r, key, func(db sqlstore.DBTX) error {
		if len(v.Data) > 1<<20 || v.Kind == "" || v.ID == "" || v.Revision < 1 {
			return durable.ErrPrecondition
		}
		result, err := db.ExecContext(tx.Context(), r.q.SaveRecord, tx.Scope().TenantID, tx.Scope().OwnerID, v.Kind, v.ID, v.TaskID, v.Revision, v.State, v.CurrentKey, v.Immutable, string(v.Data))
		if err != nil {
			return err
		}
		n, err := result.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return durable.ErrInvariant
		}
		return nil
	})
}
func (r *Store) Put(tx *durable.Tx, t *o.TaskState, v o.Record) error {
	if t == nil || t.Task.TaskID != v.TaskID {
		return durable.ErrPrecondition
	}
	return r.put(tx, taskKey(t), v)
}
func (r *Store) Deselect(tx *durable.Tx, t *o.TaskState, kind, current string) error {
	if t == nil {
		return durable.ErrPrecondition
	}
	return use(tx, r, taskKey(t), func(db sqlstore.DBTX) error {
		_, err := db.ExecContext(tx.Context(), r.q.Deselect, tx.Scope().TenantID, tx.Scope().OwnerID, t.Task.TaskID, kind, current)
		return err
	})
}
func (r *Store) Gate(tx *durable.Tx, id string, shared, create bool) (int64, error) {
	var revision int64
	read := false
	err := tx.Lock(r.p, gateKey(id), func(h any) error {
		read = true
		db := h.(sqlstore.DBTX)
		if create {
			if _, err := db.ExecContext(tx.Context(), r.q.GateCreate, tx.Scope().TenantID, tx.Scope().OwnerID, id); err != nil {
				return err
			}
		}
		q := r.q.LockGate
		if shared {
			q = r.q.ReadGate
		}
		err := db.QueryRowContext(tx.Context(), q, tx.Scope().TenantID, tx.Scope().OwnerID, id).Scan(&revision)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return err
	})
	if err == nil && !read {
		err = use(tx, r, gateKey(id), func(db sqlstore.DBTX) error {
			e := db.QueryRowContext(tx.Context(), r.q.ReadGate, tx.Scope().TenantID, tx.Scope().OwnerID, id).Scan(&revision)
			if errors.Is(e, sql.ErrNoRows) {
				return nil
			}
			return e
		})
	}
	return revision, err
}
func (r *Store) AdvanceGate(tx *durable.Tx, id string) error {
	return use(tx, r, gateKey(id), func(db sqlstore.DBTX) error {
		_, err := db.ExecContext(tx.Context(), r.q.GateAdvance, tx.Scope().TenantID, tx.Scope().OwnerID, id)
		return err
	})
}
func (r *Store) PutGlobal(tx *durable.Tx, id string, v o.Record) error {
	return r.put(tx, gateKey(id), v)
}
func (r *Store) Receiver(tx *durable.Tx, id string) (*o.ReceiverState, error) {
	var out *o.ReceiverState
	err := use(tx, r, "00-read", func(db sqlstore.DBTX) error {
		var data string
		err := db.QueryRowContext(tx.Context(), r.q.Receiver, tx.Scope().TenantID, tx.Scope().OwnerID, id).Scan(&data)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		out = &o.ReceiverState{}
		return json.Unmarshal([]byte(data), out)
	})
	return out, err
}
func (r *Store) SaveReceiver(tx *durable.Tx, v *o.ReceiverState) error {
	err := use(tx, r, gateKey("receiver-"+v.Receiver.AllocationID), func(db sqlstore.DBTX) error {
		data, err := json.Marshal(v)
		if err != nil {
			return err
		}
		_, err = db.ExecContext(tx.Context(), r.q.SaveReceiver, tx.Scope().TenantID, tx.Scope().OwnerID, v.Receiver.AllocationID, string(data))
		return err
	})
	if err != nil {
		return err
	}
	state := "closed"
	if v.Receiver.State == "closing" || v.Outbox != nil {
		state = "open"
	}
	task := ""
	if v.Receiver.TaskID != nil {
		task = *v.Receiver.TaskID
	}
	return r.put(tx, gateKey("receiver-"+v.Receiver.AllocationID), o.Record{Kind: "receiver_work", ID: v.Receiver.AllocationID, TaskID: task, Revision: v.Receiver.Revision, State: state, Data: o.Raw(o.WorkRef{Kind: "settle", ObjectKind: "receiver", ObjectID: v.Receiver.AllocationID})})
}

func (r *Store) GetMany(tx *durable.Tx, kind string, ids []string) ([]o.Record, error) {
	if len(ids) == 0 {
		return []o.Record{}, nil
	}
	if len(ids) > 100 {
		return nil, durable.ErrPrecondition
	}
	b, e := json.Marshal(ids)
	if e != nil {
		return nil, e
	}
	return r.records(tx, r.q.GetMany, tx.Scope().TenantID, tx.Scope().OwnerID, kind, string(b))
}
func (r *Store) PutMany(tx *durable.Tx, t *o.TaskState, rows []o.Record) error {
	if len(rows) == 0 {
		return nil
	}
	if t == nil || len(rows) > 2048 {
		return durable.ErrPrecondition
	}
	seen := map[string]bool{}
	for _, row := range rows {
		key := row.Kind + "/" + row.ID
		if row.TaskID != t.Task.TaskID || row.Revision < 1 || len(row.Data) > 1<<20 || seen[key] {
			return durable.ErrPrecondition
		}
		seen[key] = true
	}
	b, e := json.Marshal(rows)
	if e != nil {
		return e
	}
	if len(b) > 8<<20 {
		return durable.ErrPrecondition
	}
	return use(tx, r, taskKey(t), func(db sqlstore.DBTX) error {
		v, e := db.ExecContext(tx.Context(), r.q.PutMany, tx.Scope().TenantID, tx.Scope().OwnerID, string(b))
		if e != nil {
			return e
		}
		n, e := v.RowsAffected()
		if e != nil {
			return e
		}
		if int(n) != len(rows) {
			return durable.ErrInvariant
		}
		return nil
	})
}
func (r *Store) DeselectKeys(tx *durable.Tx, t *o.TaskState, kind string, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	if t == nil || len(keys) > 100 {
		return durable.ErrPrecondition
	}
	b, e := json.Marshal(keys)
	if e != nil {
		return e
	}
	return use(tx, r, taskKey(t), func(db sqlstore.DBTX) error {
		_, e := db.ExecContext(tx.Context(), r.q.DeselectKeys, tx.Scope().TenantID, tx.Scope().OwnerID, t.Task.TaskID, kind, string(b))
		return e
	})
}
func (r *Store) Routes(tx *durable.Tx, t *o.TaskState, works []o.WorkRef) error {
	if len(works) == 0 {
		return nil
	}
	if len(works) > 2048 {
		return durable.ErrPrecondition
	}
	type route struct{ Kind, Responsibility, SubjectID, TaskID, ProviderID, ResourceID string }
	rows := []route{}
	for _, w := range works {
		rows = append(rows, route{w.Kind, w.TaskID + "/" + w.Kind + "/" + w.ObjectKind + "/" + w.ObjectID, w.SubjectID, w.TaskID, w.ProviderID, w.ResourceID})
	}
	b, e := json.Marshal(rows)
	if e != nil {
		return e
	}
	// Every route is protected by its Task lock; Read is used only for the batch
	// statement after these capabilities have been verified individually.
	for _, w := range works {
		if t == nil || w.TaskID != t.Task.TaskID {
			return durable.ErrPrecondition
		}
	}
	return use(tx, r, taskKey(t), func(db sqlstore.DBTX) error {
		v, e := db.ExecContext(tx.Context(), r.q.Routes, tx.Scope().TenantID, tx.Scope().OwnerID, string(b))
		if e != nil {
			return e
		}
		n, e := v.RowsAffected()
		if e == nil && int(n) != len(rows) {
			return durable.ErrInvariant
		}
		return e
	})
}
