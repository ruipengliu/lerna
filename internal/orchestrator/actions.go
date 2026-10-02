package orchestrator

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

func (c *Coordinator) admitActions(tx *durable.Tx, ch *Change, t *TaskState, sourceKind, source string, actions []preparedAction) error {
	if len(actions) == 0 {
		return nil
	}
	if sourceKind != "decision" && sourceKind != "check" {
		return durable.ErrInvariant
	}
	if e := c.canAdvanceForOriginal(ch, t, t.Task.GoalRevision, t.Task.ControlRevision); e != nil {
		return e
	}
	if e := c.preflightActions(tx, ch, t, actions); e != nil {
		return e
	}
	var e error
	for _, a := range actions {
		p := a.Preparation
		id := ID("operation", t.Task.TaskID, sourceKind, source, p.Action.ActionKey)
		rid := reserveID("operation", id)
		if e = c.reserve(tx, ch, t, Reservation{ID: rid, TaskID: t.Task.TaskID, ObjectOwnerID: p.ExecutorID, ObjectKind: "operation", ObjectID: id, UpperBound: p.UpperBound}); e != nil {
			return e
		}
		invoke := api.Invoke{OperationID: id, TaskID: t.Task.TaskID, OrchestratorID: c.Config.Scope.OwnerID, GoalRevision: t.Task.GoalRevision, ControlSnapshot: a.Snapshot, CapabilityRef: p.Action.CapabilityRef, BindingRef: p.Action.BindingRef, Arguments: p.Action.Arguments, AuthorizationRefs: p.AuthorizationRefs, ReservationRef: api.ObjectRef{OwnerID: c.Config.Scope.OwnerID, ID: rid, Revision: 1}, Deadline: t.Task.Deadline}
		invoke.IntentHash = Hash(struct {
			Tenant, Owner, Task, Operation string
			Goal                           int64
			Capability                     api.ComponentRef
			Binding                        api.BindingRef
			Arguments                      map[string]json.RawMessage
		}{c.Config.Scope.TenantID, c.Config.Scope.OwnerID, t.Task.TaskID, id, t.Task.GoalRevision, invoke.CapabilityRef, invoke.BindingRef, invoke.Arguments})
		if e = validateValue("Invoke", invoke); e != nil {
			return e
		}
		deadline, _ := time.Parse(time.RFC3339Nano, t.Task.Deadline)
		intent := Intent{Invoke: invoke, Call: originalCall(p.ExecutorID, "execution.invoke", p.ExecutorID, ID("command", id, "invoke"), invoke, deadline), SourceKind: sourceKind, SourceID: source, ReservationID: rid, Preparation: p, RequirementIDs: p.Action.RequirementRefs}
		if e = c.Repository.Put(tx, t, rec("intent", id, t.Task.TaskID, 1, "open", true, intent)); e != nil {
			return e
		}
		if p.EffectClass != "read_only" {
			initial := api.Operation{OperationID: id, Revision: 1, ExecutionState: "accepted", Effect: "not_started", MayApplyLater: Raw("unknown"), Attempts: []api.Attempt{}, EvidenceRefs: []api.ContentRef{}, Usage: []api.Amount{}, UsageFinal: false, NextAction: "query_original"}
			if e = c.Repository.Put(tx, t, rec("effect", id, t.Task.TaskID, 1, "open", false, EffectState{OwnerID: p.ExecutorID, Operation: initial, GoalRevision: t.Task.GoalRevision})); e != nil {
				return e
			}
			wait(t, "effect", "query_original_operation", &api.ObjectRef{OwnerID: p.ExecutorID, ID: id, Revision: 1})
		}
		if e = c.Repository.Put(tx, t, rec("executor", p.ExecutorID, t.Task.TaskID, 1, "closed", true, p.ExecutorID)); e != nil {
			return e
		}
		queueAction(ch, t, "dispatch", id, p)
		queue(ch, t, "settle", "operation", id)
	}
	return nil
}

func (c *Coordinator) preflightActions(tx *durable.Tx, ch *Change, t *TaskState, actions []preparedAction) error {
	if len(actions) == 0 {
		return nil
	}
	bindings, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "executor", Limit: c.Config.Limits.Executors + 1})
	if e != nil {
		return e
	}
	known := map[string]bool{}
	for _, b := range bindings {
		known[b.ID] = true
	}
	effects, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "effect", State: "open", Limit: c.Config.Limits.Effects + 1})
	if e != nil {
		return e
	}
	count := len(effects)
	// Validate the whole bounded batch before changing any balance.
	values := balanceMap(t.Task.Budget)
	for _, a := range actions {
		p := a.Preparation
		proof := a.Snapshot
		if Hash(proof.Gate) != Hash(gate(ch, t)) || proof.ExecutorID != p.ExecutorID || validateValue("ControlSnapshot", proof) != nil {
			return failure("revision_conflict", "new action proof does not cover current effective ancestor control")
		}
		exp, _ := time.Parse(time.RFC3339Nano, proof.StartBefore)
		issued, _ := time.Parse(time.RFC3339Nano, proof.IssuedAt)
		deadline, _ := time.Parse(time.RFC3339Nano, t.Task.Deadline)
		if !ch.Now.Before(exp) || issued.After(ch.Now.Add(time.Second)) || exp.After(deadline) {
			return failure("expired", "action start qualification is outside deadline")
		}
		if !known[p.ExecutorID] {
			known[p.ExecutorID] = true
		}
		if len(known) > c.Config.Limits.Executors {
			return failure("quota_exceeded", "complete executor control set is full")
		}
		if p.EffectClass != "read_only" {
			count++
		}
		if count > c.Config.Limits.Effects {
			return failure("quota_exceeded", "complete effects set is full")
		}
		if e = c.Ports.Authority.Current(tx.Context(), c.ownerCaller(t), api.Access{Method: "execution.invoke", TargetID: p.ExecutorID, TaskID: t.Task.TaskID, PolicyRef: &t.Task.PolicyRef, ContentRefs: p.Action.EvidenceRefs, AuthorizationRefs: p.AuthorizationRefs, Gate: &proof.Gate}, ch.Now); e != nil {
			return e
		}
		for _, upper := range p.UpperBound {
			b, ok := values[upper.Unit]
			if !ok {
				return failure("precondition_failed", "action unit is outside budget")
			}
			n, e := add(b.Reserved.Amount, upper.Amount)
			if e != nil {
				return e
			}
			total, e := add(b.Spent.Amount, n)
			if e != nil {
				return e
			}
			cmp, e := compare(total, b.Limit.Amount)
			if e != nil {
				return e
			}
			if cmp > 0 {
				return failure("quota_exceeded", "action batch exceeds strict remaining budget")
			}
			b.Reserved.Amount = n
			values[upper.Unit] = b
		}
	}
	return nil
}

func (c *Coordinator) dispatch(ctx context.Context, u Unit, f frame) error {
	if f.Ref.ObjectKind == "delegation" {
		return c.dispatchChild(ctx, u, f)
	}
	r, e := c.readRecord(ctx, u, "intent", f.Ref.ObjectID)
	if e != nil {
		return e
	}
	v, e := Decode[Intent](r)
	if e != nil {
		return e
	}
	var op api.Operation
	e = c.external(ctx, u, f, "execution.read", v.Invoke.OperationID, func() error {
		var e error
		op, e = c.Ports.Executor.Read(ctx, f.Caller, v.Call.OwnerID, v.Invoke.OperationID)
		return e
	})
	if notFound(e) {
		live := false
		e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
			live = t.Task.GoalRevision == v.Invoke.GoalRevision && t.Task.ControlRevision == v.Invoke.ControlSnapshot.Gate.ControlRevision && !terminal(t) && gate(ch, t).Control == "running" && ch.Now.Before(parseTime(v.Invoke.ControlSnapshot.StartBefore))
			return nil
		})
		if e != nil {
			return e
		}
		if live {
			e = c.external(ctx, u, f, "execution.invoke", v.Call.OwnerID, func() error { var e error; op, e = c.Ports.Executor.Invoke(ctx, f.Caller, v.Call, v.Invoke); return e })
		} else {
			op, e = c.stopOperation(ctx, u, f, v)
		}
	}
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	return c.mergeOperation(ctx, u, f, v, op)
}
func parseTime(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }

func (c *Coordinator) stopOperation(ctx context.Context, u Unit, f frame, v Intent) (api.Operation, error) {
	id := v.Invoke.OperationID
	r, e := c.readRecord(ctx, u, "operation_cancel", id)
	if e != nil {
		return api.Operation{}, e
	}
	var call api.OriginalCall
	if r != nil {
		call, e = Decode[api.OriginalCall](r)
		if e != nil {
			return api.Operation{}, e
		}
	} else {
		call = originalCall(v.Call.OwnerID, "execution.cancel", id, ID("command", id, "cancel"), map[string]string{"operation_id": id}, parseTime(v.Invoke.Deadline))
		e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
			return c.Repository.Put(tx, t, rec("operation_cancel", id, t.Task.TaskID, 1, "closed", true, call))
		})
		if e != nil {
			return api.Operation{}, e
		}
	}
	var op api.Operation
	e = c.external(ctx, u, f, "execution.cancel", id, func() error { var e error; op, e = c.Ports.Executor.Cancel(ctx, f.Caller, call, id); return e })
	return op, e
}

func (c *Coordinator) poll(ctx context.Context, u Unit, f frame) error {
	switch f.Ref.ObjectKind {
	case "input":
		return c.openInput(ctx, u, f)
	case "decision":
		return c.startDecision(ctx, u, f, f.Ref.ObjectID)
	case "delegation":
		return c.pollChild(ctx, u, f)
	case "operation":
		allowed, e := c.attempt(ctx, u, f)
		if e != nil || !allowed {
			return e
		}
		r, e := c.readRecord(ctx, u, "intent", f.Ref.ObjectID)
		if e != nil {
			return e
		}
		v, e := Decode[Intent](r)
		if e != nil {
			return e
		}
		// Evidence delivery has its own original immutable command and stays
		// pending until its owner confirms it. A read never manufactures effects.
		var attachments []Record
		e = u.Step(ctx, func() error {
			return outcome(u.Within(ctx, func(tx *durable.Tx) error {
				if e := c.Repository.Read(tx); e != nil {
					return e
				}
				var e error
				attachments, e = c.Repository.Records(tx, Filter{TaskID: f.Task.Task.TaskID, Kind: "evidence", Limit: c.Config.Limits.Facts + 1})
				return e
			}))
		})
		if e != nil {
			return e
		}
		if len(attachments) > c.Config.Limits.Facts {
			return c.finish(ctx, u, f, "evidence_projection_incomplete", false)
		}
		for _, row := range attachments {
			a, e := Decode[Attachment](&row)
			if e != nil {
				return e
			}
			if a.OperationID != v.Invoke.OperationID {
				continue
			}
			ack, e := c.readRecord(ctx, u, "evidence_ack", row.ID)
			if e != nil {
				return e
			}
			if ack != nil {
				continue
			}
			call := originalCall(v.Call.OwnerID, "execution.attach_evidence", v.Invoke.OperationID, ID("command", a.Call.Command.CommandID, "attach"), map[string]any{"operation_id": v.Invoke.OperationID, "evidence_refs": a.Refs}, parseTime(a.Call.Command.ExpiresAt))
			if e = c.external(ctx, u, f, "execution.attach_evidence", v.Invoke.OperationID, func() error {
				return c.Ports.Executor.AttachEvidence(ctx, f.Caller, call, v.Invoke.OperationID, a.Refs)
			}); e != nil {
				return c.finish(ctx, u, f, "dependency_unavailable", false)
			}
			if e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
				return c.Repository.Put(tx, t, rec("evidence_ack", row.ID, t.Task.TaskID, 1, "closed", true, call))
			}); e != nil {
				return e
			}
		}
		var op api.Operation
		e = c.external(ctx, u, f, "execution.read", v.Invoke.OperationID, func() error {
			var e error
			op, e = c.Ports.Executor.Read(ctx, f.Caller, v.Call.OwnerID, v.Invoke.OperationID)
			return e
		})
		if notFound(e) {
			return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
				queueAction(ch, t, "dispatch", v.Invoke.OperationID, v.Preparation)
				return durable.Done(), nil
			})
		}
		if e != nil {
			return c.finish(ctx, u, f, "dependency_unavailable", false)
		}
		return c.mergeOperation(ctx, u, f, v, op)
	default:
		return durable.ErrInvariant
	}
}

func effectClosed(op api.Operation) bool {
	return op.ExecutionState == "closed" && string(op.MayApplyLater) == "false" && op.Effect != "unknown"
}
func (c *Coordinator) mergeOperation(ctx context.Context, u Unit, f frame, v Intent, op api.Operation) error {
	if op.OperationID != v.Invoke.OperationID || validateValue("Operation", op) != nil {
		return c.finish(ctx, u, f, "invalid_original_fact", false)
	}
	for _, ref := range op.EvidenceRefs {
		if e := u.Step(ctx, func() error { _, e := c.content(ctx, f.Caller, ref); return e }); e != nil {
			return c.finish(ctx, u, f, "dependency_unavailable", false)
		}
	}
	if op.ResultRef != nil {
		if e := u.Step(ctx, func() error { _, e := c.content(ctx, f.Caller, *op.ResultRef); return e }); e != nil {
			return c.finish(ctx, u, f, "dependency_unavailable", false)
		}
	}
	return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		fresh, e := c.fact(tx, t, Fact{OwnerID: v.Call.OwnerID, ObjectKind: "operation", ObjectID: op.OperationID, Revision: op.Revision, Digest: Hash(op), Value: Raw(op)})
		if e != nil {
			return durable.Disposition{}, e
		}
		old, e := c.Repository.Get(tx, "operation", op.OperationID)
		if e != nil {
			return durable.Disposition{}, e
		}
		if old != nil && old.Revision > op.Revision {
			return durable.Done(), nil
		}
		closed := effectClosed(op) || v.Preparation.EffectClass == "read_only" && op.ExecutionState == "closed" && op.NextAction == "none"
		state := "open"
		if closed {
			state = "closed"
		}
		row := rec("operation", op.OperationID, t.Task.TaskID, op.Revision, state, false, op)
		if v.Invoke.GoalRevision == t.Task.GoalRevision {
			row.CurrentKey = op.OperationID
		}
		if e = c.Repository.Put(tx, t, row); e != nil {
			return durable.Disposition{}, e
		}
		if closed {
			if e = c.Repository.Put(tx, t, rec("intent", op.OperationID, t.Task.TaskID, 1, "closed", true, v)); e != nil {
				return durable.Disposition{}, e
			}
		}
		if v.Preparation.EffectClass != "read_only" {
			state := "open"
			if effectClosed(op) {
				state = "closed"
				clearWait(t, "effect", op.OperationID)
			}
			if e = c.Repository.Put(tx, t, rec("effect", op.OperationID, t.Task.TaskID, op.Revision, state, false, EffectState{OwnerID: v.Call.OwnerID, Operation: op, GoalRevision: v.Invoke.GoalRevision})); e != nil {
				return durable.Disposition{}, e
			}
		}
		if fresh {
			if v.Invoke.GoalRevision == t.Task.GoalRevision && (effectClosed(op) || op.ResultRef != nil) {
				t.NoProgressCount = 0
			}
			if op.ResultRef != nil && v.Invoke.GoalRevision == t.Task.GoalRevision {
				r := rec("context_fact", ID("context_fact", op.OperationID, strconv.FormatInt(op.Revision, 10)), t.Task.TaskID, op.Revision, "closed", true, api.BrainContextFactsItem{Kind: "operation", ObjectRef: api.ObjectRef{OwnerID: v.Call.OwnerID, ID: op.OperationID, Revision: op.Revision}, ContentRef: *op.ResultRef})
				r.CurrentKey = op.OperationID
				if e = c.Repository.Deselect(tx, t, r.Kind, r.CurrentKey); e != nil {
					return durable.Disposition{}, e
				}
				if e = c.Repository.Put(tx, t, r); e != nil {
					return durable.Disposition{}, e
				}
			}
			t.Task.Revision++
			queue(ch, t, "verify", "task", t.Task.TaskID)
			queue(ch, t, "settle", "operation", op.OperationID)
			if closed {
				queue(ch, t, "decide", "task", t.Task.TaskID)
			}
			if e = c.persist(tx, ch, t); e != nil {
				return durable.Disposition{}, e
			}
		}
		if !closed {
			if f.Ref.Kind == "dispatch" {
				queue(ch, t, "poll", "operation", op.OperationID)
				return durable.Done(), nil
			}
			return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "original_effect_pending"), nil
		}
		return durable.Done(), nil
	})
}

func (c *Coordinator) prepareInput(tx *durable.Tx, ch *Change, t *TaskState, source string, p api.Proposal) error {
	id := ID("request", t.Task.TaskID, source)
	pub, e := publication(ID("publication", id), t.Task.TaskID, map[string]any{"question": *p.Question, "options": *p.Options})
	if e != nil {
		return e
	}
	if e = c.Repository.Put(tx, t, rec("publication", pub.ID, t.Task.TaskID, 1, "open", false, pub)); e != nil {
		return e
	}
	deadline := ch.Now.Add(t.Policy.InputTimeout)
	if d := parseTime(t.Task.Deadline); deadline.After(d) {
		deadline = d
	}
	v := InputPreparation{SourceID: source, GoalRevision: t.Task.GoalRevision, RequestID: id, Proposal: p, PublicationID: pub.ID, Deadline: stamp(deadline)}
	if e = c.Repository.Put(tx, t, rec("input_preparation", id, t.Task.TaskID, 1, "open", true, v)); e != nil {
		return e
	}
	wait(t, "input", "publish_original_request", &api.ObjectRef{OwnerID: c.Config.Scope.OwnerID, ID: id, Revision: 1})
	queue(ch, t, "poll", "input", id)
	return nil
}
func (c *Coordinator) openInput(ctx context.Context, u Unit, f frame) error {
	r, e := c.readRecord(ctx, u, "input_preparation", f.Ref.ObjectID)
	if e != nil {
		return e
	}
	p, e := Decode[InputPreparation](r)
	if e != nil {
		return e
	}
	acceptance, e := c.readRecord(ctx, u, "acceptance_preparation", f.Ref.ObjectID)
	if e != nil {
		return e
	}
	if acceptance != nil {
		p, e = Decode[InputPreparation](acceptance)
		if e != nil {
			return e
		}
	}
	ref, e := c.publish(ctx, u, f, p.PublicationID)
	if e != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	return c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		if p.GoalRevision != t.Task.GoalRevision || terminal(t) {
			clearWait(t, "input", p.RequestID)
			t.Task.Revision++
			return durable.Done(), c.persist(tx, ch, t)
		}
		old, e := c.Repository.Get(tx, "input", p.RequestID)
		if e != nil {
			return durable.Disposition{}, e
		}
		if old != nil {
			return durable.Done(), nil
		}
		if e = c.Repository.Put(tx, t, rec("input_preparation", p.RequestID, t.Task.TaskID, 1, "closed", true, func() InputPreparation { v, _ := Decode[InputPreparation](r); return v }())); e != nil {
			return durable.Disposition{}, e
		}
		max := int64(4096)
		field := api.SurfaceField{Name: "answer", Label: "回答", Type: "text", Required: true, MaxLength: &max}
		if p.Proposal.Options != nil && len(*p.Proposal.Options) > 0 {
			options := []api.SurfaceFieldOptionsItem{}
			for i, label := range *p.Proposal.Options {
				if len([]rune(label)) > 256 {
					return durable.Disposition{}, failure("invalid_output", "input option label exceeds surface scope")
				}
				options = append(options, api.SurfaceFieldOptionsItem{ID: strconv.Itoa(i + 1), Label: label})
			}
			field = api.SurfaceField{Name: "answer", Label: "回答", Type: "choice", Required: true, Options: &options}
		}
		v := InputState{GoalRevision: p.GoalRevision, View: api.InputRequestView{RequestID: p.RequestID, OwnerID: c.Config.Scope.OwnerID, Revision: 1, Kind: "clarification", TaskRef: ptr(taskRef(t)), Schema: api.SurfaceInputSchema{Fields: []api.SurfaceField{field}}, QuestionRef: ref, Deadline: p.Deadline, RequiredContentRefs: *p.Proposal.RequiredMaterialRefs, AllowedActions: []string{"answer"}, State: "open"}}
		if p.Candidate != nil {
			options := []api.SurfaceFieldOptionsItem{{ID: "accept", Label: "接受"}}
			v.AcceptanceRequirements = p.AcceptanceRequirements
			v.View.Kind = "acceptance"
			v.View.GoalRevision = &p.GoalRevision
			v.View.CandidateRef = p.Candidate
			v.View.CandidateHash = &p.Candidate.Hash
			v.View.Schema.Fields = []api.SurfaceField{{Name: "decision", Label: "决定", Type: "choice", Required: true, Options: &options}}
			v.View.AllowedActions = []string{"accept"}
		}
		if !ch.Now.Before(parseTime(p.Deadline)) {
			v.View.State = "expired"
			clearWait(t, "input", p.RequestID)
		} else {
			wait(t, "input", "consume_exact_request_revision", &api.ObjectRef{OwnerID: c.Config.Scope.OwnerID, ID: p.RequestID, Revision: 1})
		}
		if e = validateValue("InputRequestView", v.View); e != nil {
			return durable.Disposition{}, e
		}
		row := rec("input", p.RequestID, t.Task.TaskID, 1, v.View.State, false, v)
		row.CurrentKey = p.RequestID
		if e = c.Repository.Put(tx, t, row); e != nil {
			return durable.Disposition{}, e
		}
		t.Task.Revision++
		queue(ch, t, "verify", "task", t.Task.TaskID)
		return durable.Done(), c.persist(tx, ch, t)
	})
}
func ptr[T any](v T) *T { return &v }
