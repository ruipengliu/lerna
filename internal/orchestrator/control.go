package orchestrator

import (
	"context"
	"errors"
	"strconv"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

func (c *Coordinator) propagate(ctx context.Context, u Unit, f frame) error {
	if f.Ref.ObjectKind == "delegation" {
		return c.propagateChild(ctx, u, f)
	}
	id := ID("control", f.Task.Task.TaskID, f.Ref.ObjectID)
	r, e := c.readRecord(ctx, u, "control", id)
	if e != nil {
		return e
	}
	v, e := Decode[ControlState](r)
	if e != nil {
		return e
	}
	if v.EnforcedGoal == v.GoalRevision && v.EnforcedControl == v.ControlRevision {
		return c.finish(ctx, u, f, "", true)
	}
	if v.Attempts >= f.Task.Policy.MaxPolls {
		return c.finish(ctx, u, f, "automatic_queries_exhausted", false)
	}
	if v.Call == nil {
		var current api.TaskGate
		e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error { current = gate(ch, t); return nil })
		if e != nil {
			return e
		}
		var proof api.ControlSnapshot
		e = c.external(ctx, u, f, "control.proof", v.ExecutorID, func() error {
			var e error
			proof, e = c.Ports.Authority.ControlProof(ctx, f.Caller, current, v.ExecutorID, c.Ports.Clock.Now())
			return e
		})
		if e != nil {
			return c.finish(ctx, u, f, "authorization", false)
		}
		if Hash(proof.Gate) != Hash(current) || proof.ExecutorID != v.ExecutorID || validateValue("ControlSnapshot", proof) != nil {
			return c.finish(ctx, u, f, "invalid_control_proof", false)
		}
		call := originalCall(v.ExecutorID, "execution.task_control", v.ExecutorID, ID("command", id, strconv.FormatInt(current.ControlRevision, 10), strconv.FormatInt(v.Attempts, 10)), proof, c.Ports.Clock.Now().Add(c.Config.Limits.TrustedReview))
		e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
			row, e := c.Repository.Get(tx, "control", id)
			if e != nil {
				return e
			}
			state, e := Decode[ControlState](row)
			if e != nil {
				return e
			}
			if Hash(gate(ch, t)) != Hash(current) {
				return failure("revision_conflict", "control changed before proof commit")
			}
			state.Call = &call
			state.Snapshot = &proof
			state.Attempts++
			v = state
			if e = c.Repository.Put(tx, t, rec("control_intent", call.Command.CommandID, t.Task.TaskID, 1, "closed", true, call)); e != nil {
				return e
			}
			return c.Repository.Put(tx, t, rec("control", id, t.Task.TaskID, t.Task.ControlRevision, "open", false, state))
		})
		if e != nil {
			return e
		}
	}
	var obs api.ControlObservation
	e = c.external(ctx, u, f, "execution.task_control", v.ExecutorID, func() error {
		var e error
		obs, e = c.Ports.Executor.Control(ctx, f.Caller, *v.Call, *v.Snapshot)
		return e
	})
	if e != nil {
		var fail *api.Failure
		if errors.As(e, &fail) && (fail.Detail.Code == "expired" || fail.Detail.Code == "rejected_expired") {
			return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
				row, e := c.Repository.Get(tx, "control", id)
				if e != nil {
					return durable.Disposition{}, e
				}
				state, e := Decode[ControlState](row)
				if e != nil {
					return durable.Disposition{}, e
				}
				if state.Call != nil && state.Call.Command.CommandID == v.Call.Command.CommandID {
					state.Call = nil
					state.Snapshot = nil
					if e = c.Repository.Put(tx, t, rec("control", id, t.Task.TaskID, t.Task.ControlRevision, "open", false, state)); e != nil {
						return durable.Disposition{}, e
					}
				}
				return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "original_control_expired_unapplied"), nil
			})
		}
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	if obs.ExecutorID != v.ExecutorID || obs.TaskID != v.TaskID || obs.GoalRevision != v.GoalRevision || obs.ControlRevision != v.ControlRevision || !obs.Enforced {
		return c.finish(ctx, u, f, "control_not_enforced", false)
	}
	return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		row, e := c.Repository.Get(tx, "control", id)
		if e != nil {
			return durable.Disposition{}, e
		}
		state, e := Decode[ControlState](row)
		if e != nil {
			return durable.Disposition{}, e
		}
		if state.GoalRevision != obs.GoalRevision || state.ControlRevision != obs.ControlRevision {
			return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "newer_control_pending"), nil
		}
		state.EnforcedGoal = obs.GoalRevision
		state.EnforcedControl = obs.ControlRevision
		if e = c.Repository.Put(tx, t, rec("control", id, t.Task.TaskID, t.Task.ControlRevision, "closed", false, state)); e != nil {
			return durable.Disposition{}, e
		}
		queue(ch, t, "verify", "task", t.Task.TaskID)
		return durable.Done(), nil
	})
}
func (c *Coordinator) propagateChild(ctx context.Context, u Unit, f frame) error {
	r, e := c.readRecord(ctx, u, "delegation", f.Ref.ObjectID)
	if e != nil {
		return e
	}
	d, e := Decode[Delegation](r)
	if e != nil {
		return e
	}
	if d.Internal {
		return c.finish(ctx, u, f, "", true)
	}
	if c.Ports.Collaboration == nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	var g api.TaskGate
	var call api.OriginalCall
	e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
		g = gate(ch, t)
		id := ID("command", d.ID, "control", strconv.FormatInt(g.ControlRevision, 10))
		row, e := c.Repository.Get(tx, "delegation_control", id)
		if e != nil {
			return e
		}
		if row != nil {
			call, e = Decode[api.OriginalCall](row)
			return e
		}
		call = originalCall(d.ReceiverID, "collaboration.control", d.ID, id, g, ch.Now.Add(c.Config.Limits.TrustedReview))
		return c.Repository.Put(tx, t, rec("delegation_control", id, t.Task.TaskID, 1, "closed", true, call))
	})
	if e != nil {
		return e
	}
	var obs api.DelegationObservation
	e = c.external(ctx, u, f, "collaboration.control", d.ID, func() error {
		var e error
		obs, e = c.Ports.Collaboration.Control(ctx, f.Caller, call, d.ID, g)
		return e
	})
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	return c.mergeChild(ctx, u, f, d, obs)
}
