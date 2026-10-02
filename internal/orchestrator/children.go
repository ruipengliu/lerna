package orchestrator

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/contracts"
	"github.com/ruipengliu/lerna/internal/durable"
)

type preparedChild struct {
	Action                           api.ActionDelegate
	Preparation                      api.DelegationPreparation
	Policy                           api.TaskPolicy
	Task                             *TaskState
	AllocationIntent, SubmitIntent   durable.FixedIntent
	AllocationCreated, SubmitCreated bool
}

func (c *Coordinator) prepareChild(ctx context.Context, u Unit, f frame, raw json.RawMessage) (preparedChild, error) {
	v := preparedChild{}
	if c.Ports.Collaboration == nil {
		return v, failure("dependency_unavailable", "delegation owner is unavailable")
	}
	if contracts.Validate("ActionDelegate", raw) != nil {
		return v, failure("invalid_output", "delegation contract mismatch")
	}
	_ = json.Unmarshal(raw, &v.Action)
	e := c.external(ctx, u, f, "collaboration.prepare", f.Task.Task.TaskID, func() error {
		var e error
		v.Preparation, e = c.Ports.Collaboration.Prepare(ctx, f.Caller, f.Task.Task, v.Action)
		return e
	})
	if e != nil {
		return v, e
	}
	if Hash(v.Preparation.Action) != Hash(v.Action) || validateValue("TaskSubmitInput", v.Preparation.Submit) != nil || v.Preparation.ReceiverID != v.Preparation.Submit.OrchestratorID || v.Preparation.Internal != (v.Preparation.ReceiverID == c.Config.Scope.OwnerID) {
		return v, failure("invalid_output", "delegation binding changed its original action")
	}
	if v.Action.Delegation.ParentTaskID != f.Task.Task.TaskID || !parseTime(v.Action.Delegation.Deadline).Equal(parseTime(v.Preparation.Submit.Deadline)) || parseTime(v.Preparation.Submit.Deadline).After(parseTime(f.Task.Task.Deadline)) {
		return v, failure("invalid_output", "child deadline or parent is outside the original task")
	}
	if e = limitsValid(v.Preparation.Submit.Budget); e != nil {
		return v, e
	}
	if _, e = c.content(ctx, f.Caller, v.Preparation.Submit.GoalRef); e != nil {
		return v, e
	}
	if v.Preparation.Internal {
		v.Policy, e = c.Ports.Policies.Resolve(ctx, f.Caller, v.Preparation.Submit.PolicyRef, v.Preparation.Submit.GoalRef, v.Preparation.Submit.Constraints)
		if e != nil {
			return v, e
		}
		if e = policyValid(v.Policy, v.Preparation.Submit.PolicyRef); e != nil {
			return v, e
		}
	}
	return v, nil
}
func (c *Coordinator) fixChildren(parent *TaskState, source string, children []preparedChild) ([]*TaskState, error) {
	extras := []*TaskState{}
	for i := range children {
		v := &children[i]
		request := v.Action.Delegation
		request.DelegationID = ID("delegation", parent.Task.TaskID, source, v.Action.ActionKey)
		request.AllocationID = ID("allocation", request.DelegationID)
		request.AncestorIDs = []string{}
		v.Action.Delegation = request
		v.Preparation.Action = v.Action
		allocate := api.BudgetAllocateInput{AllocationID: request.AllocationID, ParentTaskID: parent.Task.TaskID, ReceiverID: v.Preparation.ReceiverID, Limits: v.Preparation.Submit.Budget, ExpiresAt: v.Preparation.Submit.Deadline}
		ac := originalCall(c.Config.Scope.OwnerID, "budget.allocate", parent.Task.TaskID, ID("command", request.DelegationID, "allocate"), allocate, parseTime(parent.Task.Deadline))
		fixed, e := fixedCall(ac)
		if e != nil {
			return nil, e
		}
		v.AllocationIntent = fixed
		context := api.RuntimeDelegationContext{SenderOrchestratorID: c.Config.Scope.OwnerID, ParentDelegationID: request.DelegationID, AllocationRef: api.ObjectRef{OwnerID: c.Config.Scope.OwnerID, ID: request.AllocationID, Revision: 1}, AllocationCommandID: ac.Command.CommandID, PermissionRefs: request.PermissionRefs, AncestorIDs: []string{parent.Task.TaskID}}
		v.Preparation.Submit.DelegationContext = &context
		sc := originalCall(v.Preparation.ReceiverID, "task.submit", v.Preparation.ReceiverID, ID("command", request.DelegationID, "submit"), v.Preparation.Submit, parseTime(parent.Task.Deadline))
		fixed, e = fixedCall(sc)
		if e != nil {
			return nil, e
		}
		v.SubmitIntent = fixed
		if v.Preparation.Internal {
			v.Task = &TaskState{Task: api.Task{TenantID: c.Config.Scope.TenantID, TaskID: ID("task", sc.Command.CommandID), OrchestratorID: c.Config.Scope.OwnerID, SubmitCommandID: sc.Command.CommandID, GoalRef: v.Preparation.Submit.GoalRef, GoalRevision: 1, ControlRevision: 1, Revision: 1, Requirements: append([]api.Requirement{}, v.Policy.Requirements...), PolicyRef: v.Policy.Ref, Status: "active", Control: "running", Deadline: v.Preparation.Submit.Deadline, WaitReasons: []api.WaitReason{}, Budget: []api.BudgetBalance{}, OpenEffects: []string{}}, SubjectID: parent.SubjectID, ParentID: parent.Task.TaskID, Depth: parent.Depth + 1, Policy: v.Policy, Constraints: v.Preparation.Submit.Constraints, AllocationID: request.AllocationID, ProjectionComplete: true}
			extras = append(extras, v.Task)
		}
	}
	return extras, nil
}
func (c *Coordinator) reserveChildren(tx *durable.Tx, children []preparedChild) error {
	type item struct {
		i      int
		submit bool
		intent durable.FixedIntent
	}
	all := []item{}
	for i, v := range children {
		all = append(all, item{i, false, v.AllocationIntent})
		if v.Preparation.Internal {
			all = append(all, item{i, true, v.SubmitIntent})
		}
	}
	sort.Slice(all, func(i, j int) bool { return all[i].intent.CommandID() < all[j].intent.CommandID() })
	for _, x := range all {
		_, created, e := tx.ReserveLocal(c.Config.ServiceID, x.intent)
		if e != nil {
			return e
		}
		if x.submit {
			children[x.i].SubmitCreated = created
		} else {
			children[x.i].AllocationCreated = created
		}
	}
	return nil
}
func (c *Coordinator) rejectChildren(tx *durable.Tx, children []preparedChild) error {
	for _, v := range children {
		for _, i := range []durable.FixedIntent{v.AllocationIntent, v.SubmitIntent} {
			created := v.AllocationCreated
			if i.CommandID() == v.SubmitIntent.CommandID() {
				created = v.SubmitCreated
			}
			if !created {
				continue
			}
			d := rejected(i, failure("precondition_failed", "parent proposal did not admit this child"))
			if e := tx.Decide(durable.CommandKey{ServiceID: c.Config.ServiceID, CommandID: i.CommandID()}, *d.Receipt); e != nil {
				return e
			}
		}
	}
	return nil
}
func (c *Coordinator) admitChildren(tx *durable.Tx, ch *Change, parent *TaskState, source string, children []preparedChild) error {
	if len(children) == 0 {
		return nil
	}
	if e := c.canAdvanceForOriginal(ch, parent, parent.Task.GoalRevision, parent.Task.ControlRevision); e != nil {
		return e
	}
	if e := c.preflightChildren(tx, ch, parent, children); e != nil {
		return e
	}
	var e error
	for _, v := range children {
		r := v.Action.Delegation
		limits := v.Preparation.Submit.Budget
		upper := []api.Amount{}
		for _, l := range limits {
			upper = append(upper, api.Amount{Unit: l.Unit, Amount: l.Limit})
		}
		rid := reserveID("allocation", r.AllocationID)
		if e = c.reserve(tx, ch, parent, Reservation{ID: rid, TaskID: parent.Task.TaskID, ObjectOwnerID: v.Preparation.ReceiverID, ObjectKind: "allocation", ObjectID: r.AllocationID, UpperBound: upper, Allocation: true}); e != nil {
			return e
		}
		a := AllocationState{Allocation: api.RuntimeBudgetAllocation{AllocationID: r.AllocationID, ParentTaskID: parent.Task.TaskID, OwnerID: c.Config.Scope.OwnerID, ReceiverID: v.Preparation.ReceiverID, Revision: 1, Limits: limits, ExpiresAt: v.Preparation.Submit.Deadline, State: "allocated", FinalUsage: []api.Amount{}}, CommandID: v.AllocationIntent.CommandID(), ReservationID: rid, DelegationID: r.DelegationID}
		if e = c.Repository.Put(tx, parent, rec("allocation", r.AllocationID, parent.Task.TaskID, 1, "open", false, a)); e != nil {
			return e
		}
		d := applied(v.AllocationIntent, parent, a.Allocation)
		if e = tx.Decide(durable.CommandKey{ServiceID: c.Config.ServiceID, CommandID: v.AllocationIntent.CommandID()}, *d.Receipt); e != nil {
			return e
		}
		request := r
		request.AncestorIDs = []string{}
		for p := parent; p != nil; {
			request.AncestorIDs = append(request.AncestorIDs, p.Task.TaskID)
			p = ch.Tasks[p.ParentID]
		}
		delegation := Delegation{ID: r.DelegationID, TaskID: parent.Task.TaskID, ReceiverID: v.Preparation.ReceiverID, AllocationID: r.AllocationID, Request: request, Call: original(v.SubmitIntent, v.Preparation.ReceiverID), Internal: v.Preparation.Internal}
		if v.Task != nil {
			t := ch.Tasks[v.Task.Task.TaskID]
			if t == nil {
				return durable.ErrInvariant
			}
			t.CreatedAt = ch.Now.UnixMilli()
			if e = c.Repository.Capacity(tx, t.SubjectID, 1, c.Config.Limits.ActivePerUser); e != nil {
				return e
			}
			if e = c.Repository.SaveTask(tx, t); e != nil {
				return e
			}
			values := map[string]api.BudgetBalance{}
			for _, l := range limits {
				values[l.Unit] = balance(l.Unit, l.Limit, "0", "0")
			}
			if e = c.saveBalances(tx, t, values); e != nil {
				return e
			}
			receiver := &ReceiverState{Receiver: api.RuntimeBudgetReceiver{AllocationID: r.AllocationID, ParentOwnerID: c.Config.Scope.OwnerID, ReceiverID: c.Config.Scope.OwnerID, ParentDelegationID: r.DelegationID, Revision: 1, State: "open", TaskID: &t.Task.TaskID, FinalUsage: []api.Amount{}}, AllocationCommandID: a.CommandID, ParentTaskID: parent.Task.TaskID, Limits: limits}
			existing, e := c.Repository.Receiver(tx, r.AllocationID)
			if e != nil {
				return e
			}
			if existing != nil {
				return failure("precondition_failed", "receiver was already closed or accepted")
			}
			if e = c.Repository.SaveReceiver(tx, receiver); e != nil {
				return e
			}
			t.Task.Revision++
			if e = c.persist(tx, ch, t); e != nil {
				return e
			}
			receipt := applied(v.SubmitIntent, t, t.Task)
			if e = tx.Decide(durable.CommandKey{ServiceID: c.Config.ServiceID, CommandID: v.SubmitIntent.CommandID()}, *receipt.Receipt); e != nil {
				return e
			}
			delegation.ChildID = t.Task.TaskID
			queue(ch, t, "decide", "task", t.Task.TaskID)
		} else {
			queue(ch, parent, "dispatch", "delegation", delegation.ID)
		}
		if e = c.Repository.Put(tx, parent, rec("delegation", delegation.ID, parent.Task.TaskID, 1, "open", false, delegation)); e != nil {
			return e
		}
		queue(ch, parent, "poll", "delegation", delegation.ID)
		queue(ch, parent, "settle", "allocation", a.Allocation.AllocationID)
	}
	return nil
}
func (c *Coordinator) preflightChildren(tx *durable.Tx, ch *Change, parent *TaskState, children []preparedChild) error {
	if len(children) == 0 {
		return nil
	}
	active, e := c.Repository.Records(tx, Filter{TaskID: parent.Task.TaskID, Kind: "delegation", State: "open", Limit: c.Config.Limits.Children + 1})
	if e != nil {
		return e
	}
	if len(active)+len(children) > c.Config.Limits.Children {
		return failure("quota_exceeded", "active child set is full")
	}
	for _, v := range children {
		if v.Task != nil && v.Task.Depth > c.Config.Limits.Depth {
			return failure("quota_exceeded", "child depth is full")
		}
	}
	// Prelock all receiver gates in the global order before reservation writes.
	gateIDs := []string{}
	for _, v := range children {
		if v.Task != nil {
			gateIDs = append(gateIDs, "receiver-"+v.Action.Delegation.AllocationID)
		}
	}
	if parent.AllocationID != "" {
		gateIDs = append(gateIDs, "receiver-"+parent.AllocationID)
	}
	sort.Strings(gateIDs)
	for _, id := range gateIDs {
		if _, e = c.Repository.Gate(tx, id, false, true); e != nil {
			return e
		}
	}
	internal := 0
	for _, child := range children {
		if child.Task != nil {
			internal++
		}
	}
	if e = c.Repository.Capacity(tx, parent.SubjectID, 0, c.Config.Limits.ActivePerUser-internal); e != nil {
		return e
	}
	for _, child := range children {
		if child.Task != nil {
			r, e := c.Repository.Receiver(tx, child.Action.Delegation.AllocationID)
			if e != nil {
				return e
			}
			if r != nil {
				return failure("precondition_failed", "child receiver was already consumed or closed")
			}
		}
	}
	return nil
}

func (c *Coordinator) dispatchChild(ctx context.Context, u Unit, f frame) error {
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
	var obs api.DelegationObservation
	e = c.external(ctx, u, f, "collaboration.read", d.ID, func() error {
		var e error
		obs, e = c.Ports.Collaboration.Read(ctx, f.Caller, d.ReceiverID, d.ID)
		return e
	})
	if notFound(e) {
		live := false
		e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
			live = !terminal(t) && gate(ch, t).Control == "running" && ch.Now.Before(parseTime(d.Request.Deadline))
			return nil
		})
		if e != nil {
			return e
		}
		if !live {
			return c.finish(ctx, u, f, "control_required", false)
		}
		e = c.external(ctx, u, f, "collaboration.submit", d.ReceiverID, func() error {
			var e error
			obs, e = c.Ports.Collaboration.Submit(ctx, f.Caller, d.Call, d.Request)
			return e
		})
	}
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	return c.mergeChild(ctx, u, f, d, obs)
}
func (c *Coordinator) pollChild(ctx context.Context, u Unit, f frame) error {
	r, e := c.readRecord(ctx, u, "delegation", f.Ref.ObjectID)
	if e != nil {
		return e
	}
	d, e := Decode[Delegation](r)
	if e != nil {
		return e
	}
	if d.Internal {
		return c.changeWork(ctx, u, f, true, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
			child := ch.Tasks[d.ChildID]
			if child == nil {
				child, e = c.Repository.Task(tx, d.ChildID)
				if e != nil {
					return durable.Disposition{}, e
				}
			}
			if child == nil {
				return durable.Disposition{}, durable.ErrInvariant
			}
			if !terminal(child) || len(child.Task.OpenEffects) > 0 {
				return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "original_child_pending"), nil
			}
			d.Observation = &api.DelegationObservation{OwnerID: c.Config.Scope.OwnerID, DelegationID: d.ID, ChildTaskID: d.ChildID, Revision: child.Task.Revision, Status: child.Task.Status, GoalClosed: true, EffectsClosed: true}
			if e = c.Repository.Put(tx, t, rec("delegation", d.ID, t.Task.TaskID, child.Task.Revision, "closed", false, d)); e != nil {
				return durable.Disposition{}, e
			}
			t.NoProgressCount = 0
			t.Task.Revision++
			queue(ch, t, "verify", "task", t.Task.TaskID)
			queue(ch, t, "decide", "task", t.Task.TaskID)
			return durable.Done(), c.persist(tx, ch, t)
		})
	}
	allowed, e := c.attempt(ctx, u, f)
	if e != nil || !allowed {
		return e
	}
	if c.Ports.Collaboration == nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	var obs api.DelegationObservation
	e = c.external(ctx, u, f, "collaboration.read", d.ID, func() error {
		var e error
		obs, e = c.Ports.Collaboration.Read(ctx, f.Caller, d.ReceiverID, d.ID)
		return e
	})
	if notFound(e) {
		return c.dispatchChild(ctx, u, f)
	}
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	return c.mergeChild(ctx, u, f, d, obs)
}
func (c *Coordinator) mergeChild(ctx context.Context, u Unit, f frame, d Delegation, obs api.DelegationObservation) error {
	if obs.OwnerID != d.ReceiverID || obs.DelegationID != d.ID || obs.ChildTaskID == "" || obs.Revision < 1 || validateValue("ContentRef", obs.ProofRef) != nil {
		return c.finish(ctx, u, f, "invalid_original_fact", false)
	}
	if _, e := c.content(ctx, f.Caller, obs.ProofRef); e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		fresh, e := c.fact(tx, t, Fact{OwnerID: obs.OwnerID, ObjectKind: "delegation", ObjectID: d.ID, Revision: obs.Revision, Digest: Hash(obs), Value: Raw(obs)})
		if e != nil {
			return durable.Disposition{}, e
		}
		if d.Observation != nil && d.Observation.Revision > obs.Revision {
			return durable.Done(), nil
		}
		d.Observation = &obs
		d.ChildID = obs.ChildTaskID
		state := "open"
		if obs.GoalClosed && obs.EffectsClosed {
			state = "closed"
		}
		if e = c.Repository.Put(tx, t, rec("delegation", d.ID, t.Task.TaskID, obs.Revision, state, false, d)); e != nil {
			return durable.Disposition{}, e
		}
		if fresh {
			t.Task.Revision++
			queue(ch, t, "verify", "task", t.Task.TaskID)
			if state == "closed" {
				t.NoProgressCount = 0
				queue(ch, t, "decide", "task", t.Task.TaskID)
			}
			if e = c.persist(tx, ch, t); e != nil {
				return durable.Disposition{}, e
			}
		}
		if state == "open" {
			if f.Ref.Kind == "dispatch" {
				queue(ch, t, "poll", "delegation", d.ID)
				return durable.Done(), nil
			}
			return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "original_child_pending"), nil
		}
		return durable.Done(), nil
	})
}

func fixedCall(c api.OriginalCall) (durable.FixedIntent, error) {
	v := c.Command
	return durable.FixIntent(durable.Intent{CommandID: v.CommandID, Method: v.Method, TargetID: v.TargetID, ExpiresAt: v.ExpiresAt, ExpectedRevision: v.ExpectedRevision, Payload: v.Payload})
}
