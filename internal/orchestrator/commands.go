package orchestrator

import (
	"bytes"
	"encoding/json"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
)

// Apply is local only. Validation rejections occur before changing facts; an
// adapter/invariant failure returns an error and rolls the entire attempt back.
func (c *Coordinator) Apply(tx *durable.Tx, p Prepared) (durable.Decision, error) {
	if err := c.sameScope(tx); err != nil {
		return durable.Decision{}, err
	}
	i := p.Intent
	if i.Method() == "task.submit" {
		return c.submit(tx, p)
	}
	if i.Method() == "budget.close" {
		return c.closeBudget(tx, p)
	}
	target := i.TargetID()
	var allocation *AllocationState
	if i.Method() == "budget.settle" {
		if err := c.Repository.Read(tx); err != nil {
			return durable.Decision{}, err
		}
		r, err := c.Repository.Get(tx, "allocation", target)
		if err != nil {
			return durable.Decision{}, err
		}
		if r == nil {
			return rejected(i, failure("not_found", "original allocation is absent")), nil
		}
		a, err := Decode[AllocationState](r)
		if err != nil {
			return durable.Decision{}, err
		}
		allocation = &a
		target = r.TaskID
	}
	subtree := i.Method() == "task.pause" || i.Method() == "task.resume" || i.Method() == "task.cancel" || i.Method() == "task.revise"
	ch, t, err := c.load(tx, target, subtree, nil)
	if err != nil {
		var f *api.Failure
		if errors.As(err, &f) {
			return rejected(i, err), nil
		}
		return durable.Decision{}, err
	}
	if err = c.access(tx, p, t); err != nil {
		return durable.Decision{}, err
	}
	if expected := i.Value().ExpectedRevision; expected != nil {
		current := t.Task.Revision
		if allocation != nil {
			current = allocation.Allocation.Revision
		}
		if current != *expected {
			return rejected(i, failure("revision_conflict", "original expected revision does not match")), nil
		}
	}
	if terminal(t) && i.Method() != "task.attach_evidence" && i.Method() != "task.billing_reconcile" && i.Method() != "budget.settle" {
		return rejected(i, failure("precondition_failed", "task has an immutable terminal decision")), nil
	}
	var output any
	switch i.Method() {
	case "task.pause", "task.resume", "task.cancel":
		var input struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(i.Value().Payload, &input)
		output, err = c.control(tx, ch, t, i.Method(), input.Reason)
	case "task.revise":
		var in api.TaskReviseInput
		_ = json.Unmarshal(i.Value().Payload, &in)
		if Hash(in.GoalRef) == Hash(t.Task.GoalRef) && Hash(in.Constraints) == Hash(t.Constraints) {
			output = t.Task
			break
		}
		t.Task.GoalRef = in.GoalRef
		t.Constraints = in.Constraints
		t.Task.Requirements = []api.Requirement{}
		if err = c.invalidateGoal(tx, ch, t, "user_revised_goal"); err != nil {
			break
		}
		output = t.Task
	case "task.adjust_budget":
		var in api.TaskAdjustBudgetInput
		_ = json.Unmarshal(i.Value().Payload, &in)
		err = c.adjust(tx, ch, t, in.Limits)
		output = t.Task
	case "task.input", "task.accept_result":
		output, err = c.consumeInput(tx, ch, t, p)
	case "task.attach_evidence":
		var in api.TaskAttachEvidenceInput
		_ = json.Unmarshal(i.Value().Payload, &in)
		r, e := c.Repository.Get(tx, "intent", in.OperationID)
		if e != nil {
			return durable.Decision{}, e
		}
		if r == nil || r.TaskID != t.Task.TaskID {
			return rejected(i, failure("not_found", "evidence must reference an original admitted operation")), nil
		}
		ch.Changed = true
		id := ID("evidence", i.CommandID())
		if err = c.Repository.Put(tx, t, rec("evidence", id, t.Task.TaskID, 1, "open", true, struct {
			OperationID string
			Refs        []api.ContentRef
			Call        api.OriginalCall
		}{in.OperationID, in.EvidenceRefs, original(i, c.Config.Scope.OwnerID)})); err != nil {
			break
		}
		queue(ch, t, "poll", "operation", in.OperationID)
		queue(ch, t, "verify", "task", t.Task.TaskID)
		t.Task.Revision++
		if err = c.persist(tx, ch, t); err != nil {
			return durable.Decision{}, err
		}
		output = api.JobAck{ResourceID: t.Task.TaskID}
	case "task.billing_reconcile":
		output, err = c.billingHint(tx, ch, t, p)
	case "budget.allocate":
		var in api.BudgetAllocateInput
		_ = json.Unmarshal(i.Value().Payload, &in)
		output, err = c.allocate(tx, ch, t, p, in)
	case "budget.settle":
		var in api.BudgetSettleInput
		_ = json.Unmarshal(i.Value().Payload, &in)
		output, err = c.settleAllocation(tx, ch, t, p, *allocation, in)
	default:
		return rejected(i, failure("unsupported", "method is not available")), nil
	}
	if err != nil {
		var f *api.Failure
		if errors.As(err, &f) && !ch.Changed {
			return rejected(i, err), nil
		}
		return durable.Decision{}, err
	}
	if err = c.flush(tx, ch, nil, durable.Done()); err != nil {
		return durable.Decision{}, err
	}
	if ack, ok := output.(api.JobAck); ok {
		kind := "verify"
		objKind, objID := "task", t.Task.TaskID
		if i.Method() == "task.billing_reconcile" {
			var in api.TaskBillingReconcileInput
			_ = json.Unmarshal(i.Value().Payload, &in)
			kind = "settle"
			objKind, objID = in.SourceKind, in.SourceID
		}
		key := workKey(WorkRef{TaskID: t.Task.TaskID, Kind: kind, ObjectKind: objKind, ObjectID: objID})
		if j, ok := ch.Jobs[key]; ok {
			ack.JobID = j.ID
		}
		output = ack
		if i.Method() == "task.billing_reconcile" {
			if err = c.finishHint(tx, t, p, ack); err != nil {
				return durable.Decision{}, err
			}
		}
	}
	return applied(i, t, output), nil
}
func (c *Coordinator) submit(tx *durable.Tx, p Prepared) (durable.Decision, error) {
	var in api.TaskSubmitInput
	_ = json.Unmarshal(p.Intent.Value().Payload, &in)
	if p.Policy == nil {
		return rejected(p.Intent, failure("dependency_unavailable", "fixed policy was not prepared")), nil
	}
	now, err := tx.Now()
	if err != nil {
		return durable.Decision{}, err
	}
	deadline, _ := time.Parse(time.RFC3339Nano, in.Deadline)
	if !now.Before(deadline) {
		return rejected(p.Intent, failure("deadline_exceeded", "task requires a future finite deadline")), nil
	}
	t := &TaskState{Task: api.Task{TenantID: c.Config.Scope.TenantID, OrchestratorID: c.Config.Scope.OwnerID, TaskID: ID("task", p.Intent.CommandID()), SubmitCommandID: p.Intent.CommandID(), GoalRef: in.GoalRef, GoalRevision: 1, Requirements: append([]api.Requirement{}, p.Policy.Requirements...), PolicyRef: in.PolicyRef, Revision: 1, ControlRevision: 1, Status: "active", Control: "running", WaitReasons: []api.WaitReason{}, Budget: []api.BudgetBalance{}, OpenEffects: []string{}, Deadline: in.Deadline}, SubjectID: p.Caller.ActorID, Policy: *p.Policy, Constraints: in.Constraints, CreatedAt: now.UnixMilli(), ProjectionComplete: true}
	ch, _, err := c.load(tx, t.Task.TaskID, false, []*TaskState{t})
	if err != nil {
		return durable.Decision{}, err
	}
	if existing := ch.Tasks[t.Task.TaskID]; existing.Task.SubmitCommandID != p.Intent.CommandID() {
		return rejected(p.Intent, failure("idempotency_conflict", "task identity already belongs to another submission")), nil
	}
	if in.DelegationContext != nil {
		d := in.DelegationContext
		a := p.Allocation
		if a == nil || a.AllocationID != d.AllocationRef.ID || a.OwnerID != d.SenderOrchestratorID || a.ReceiverID != c.Config.Scope.OwnerID || a.ParentTaskID == "" || a.State != "allocated" || a.Revision != d.AllocationRef.Revision || Hash(a.Limits) != Hash(in.Budget) {
			return rejected(p.Intent, failure("precondition_failed", "original allocation binding and complete limits required")), nil
		}
		expiry, _ := time.Parse(time.RFC3339Nano, a.ExpiresAt)
		if deadline.After(expiry) || !now.Before(expiry) {
			return rejected(p.Intent, failure("expired", "allocation deadline does not cover the child")), nil
		}
		if _, err = c.Repository.Gate(tx, "receiver-"+a.AllocationID, false, true); err != nil {
			return durable.Decision{}, err
		}
		receiver, err := c.Repository.Receiver(tx, a.AllocationID)
		if err != nil {
			return durable.Decision{}, err
		}
		if receiver != nil {
			return rejected(p.Intent, failure("precondition_failed", "allocation already accepted or its spending gate is closed")), nil
		}
		t.AllocationID = a.AllocationID
		r := &ReceiverState{Receiver: api.RuntimeBudgetReceiver{AllocationID: a.AllocationID, ParentOwnerID: a.OwnerID, ReceiverID: c.Config.Scope.OwnerID, ParentDelegationID: d.ParentDelegationID, Revision: 1, State: "open", TaskID: &t.Task.TaskID, FinalUsage: []api.Amount{}}, AllocationCommandID: d.AllocationCommandID, ParentTaskID: a.ParentTaskID, Limits: a.Limits}
		if err = c.Repository.SaveReceiver(tx, r); err != nil {
			return durable.Decision{}, err
		}
	}
	if err = c.Repository.Capacity(tx, t.SubjectID, 1, c.Config.Limits.ActivePerUser); err != nil {
		return durable.Decision{}, err
	}
	if err = c.Ports.Authority.Current(tx.Context(), p.Caller, api.Access{Method: "task.submit", TargetID: c.Config.Scope.OwnerID, TaskID: t.Task.TaskID, PolicyRef: &t.Task.PolicyRef, ContentRefs: []api.ContentRef{in.GoalRef}}, now); err != nil {
		return durable.Decision{}, err
	}
	for _, b := range in.Budget {
		t.Task.Budget = append(t.Task.Budget, balance(b.Unit, b.Limit, "0", "0"))
	}
	if err = c.Repository.SaveTask(tx, t); err != nil {
		return durable.Decision{}, err
	}
	if err = c.Repository.SaveBalances(tx, t.Task.TaskID, t.Task.Budget); err != nil {
		return durable.Decision{}, err
	}
	if err = c.Repository.Put(tx, t, rec("goal", ID("goal", t.Task.TaskID, "1"), t.Task.TaskID, 1, "closed", true, struct {
		Goal         api.ContentRef
		Constraints  []string
		Requirements []api.Requirement
	}{t.Task.GoalRef, t.Constraints, t.Task.Requirements})); err != nil {
		return durable.Decision{}, err
	}
	queue(ch, t, "decide", "task", t.Task.TaskID)
	if err = c.flush(tx, ch, nil, durable.Done()); err != nil {
		return durable.Decision{}, err
	}
	return applied(p.Intent, t, t.Task), nil
}
func (c *Coordinator) controls(tx *durable.Tx, ch *Change, t *TaskState) error {
	if t.PendingDecision != "" {
		queue(ch, t, "poll", "decision", t.PendingDecision)
	}
	delegations, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "delegation", State: "open", Limit: c.Config.Limits.Children + 1})
	if e != nil {
		return e
	}
	if len(delegations) > c.Config.Limits.Children {
		return durable.ErrInvariant
	}
	for _, r := range delegations {
		d, e := Decode[Delegation](&r)
		if e != nil {
			return e
		}
		if !d.Internal {
			queue(ch, t, "control", "delegation", d.ID)
		}
	}
	bindings, err := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "executor", Limit: c.Config.Limits.Executors + 1})
	if err != nil {
		return err
	}
	if len(bindings) > c.Config.Limits.Executors {
		return durable.ErrInvariant
	}
	for _, binding := range bindings {
		r, err := c.Repository.Get(tx, "control", ID("control", t.Task.TaskID, binding.ID))
		if err != nil {
			return err
		}
		v := ControlState{TaskID: t.Task.TaskID, ExecutorID: binding.ID}
		if r != nil {
			v, err = Decode[ControlState](r)
			if err != nil {
				return err
			}
		}
		v.GoalRevision = t.Task.GoalRevision
		v.ControlRevision = t.Task.ControlRevision
		v.Call = nil
		v.Snapshot = nil
		v.Attempts = 0
		if err = c.Repository.Put(tx, t, rec("control", ID("control", t.Task.TaskID, binding.ID), t.Task.TaskID, t.Task.ControlRevision, "open", false, v)); err != nil {
			return err
		}
		queue(ch, t, "control", "executor", binding.ID)
	}
	return nil
}
func (c *Coordinator) control(tx *durable.Tx, ch *Change, root *TaskState, method, reason string) (api.ControlAck, error) {
	ch.Changed = true
	ack := api.ControlAck{TaskID: root.Task.TaskID, PendingExecutorIDs: []string{}, AffectedChildIDs: []string{}}
	for _, t := range sortedTasks(taskValues(ch.Tasks)) {
		if t.Task.TaskID != root.Task.TaskID && !descendant(ch, t, root.Task.TaskID) {
			continue
		}
		if terminal(t) {
			continue
		}
		if t.Task.TaskID == root.Task.TaskID {
			if method == "task.pause" {
				t.Task.Control = "paused"
			}
			if method == "task.resume" {
				t.Task.Control = "running"
			}
		}
		if method == "task.cancel" {
			t.Task.Status = "cancelled"
			t.FailureReason = reason
			if err := c.Repository.Capacity(tx, t.SubjectID, -1, c.Config.Limits.ActivePerUser); err != nil {
				return ack, err
			}
			if err := c.closeInputs(tx, t, "superseded"); err != nil {
				return ack, err
			}
		}
		t.Task.ControlRevision++
		t.Task.Revision++
		if err := c.controls(tx, ch, t); err != nil {
			return ack, err
		}
		if method == "task.resume" && !terminal(t) {
			queue(ch, t, "decide", "task", t.Task.TaskID)
		}
		queue(ch, t, "verify", "task", t.Task.TaskID)
		if err := c.persist(tx, ch, t); err != nil {
			return ack, err
		}
		if t.Task.TaskID != root.Task.TaskID {
			ack.AffectedChildIDs = append(ack.AffectedChildIDs, t.Task.TaskID)
		}
	}
	rows, err := c.Repository.Records(tx, Filter{TaskID: root.Task.TaskID, Kind: "control", State: "open", Limit: c.Config.Limits.Executors + 1})
	if err != nil {
		return ack, err
	}
	for _, r := range rows {
		v, e := Decode[ControlState](&r)
		if e != nil {
			return ack, e
		}
		ack.PendingExecutorIDs = append(ack.PendingExecutorIDs, v.ExecutorID)
	}
	ack.Revision = root.Task.Revision
	ack.ControlRevision = root.Task.ControlRevision
	ack.Control = root.Task.Control
	ack.Status = root.Task.Status
	return ack, nil
}
func taskValues(m map[string]*TaskState) []*TaskState {
	out := []*TaskState{}
	for _, t := range m {
		out = append(out, t)
	}
	return out
}
func descendant(ch *Change, t *TaskState, root string) bool {
	for id := t.ParentID; id != ""; {
		if id == root {
			return true
		}
		p := ch.Tasks[id]
		if p == nil {
			return false
		}
		id = p.ParentID
	}
	return false
}
func (c *Coordinator) invalidateGoal(tx *durable.Tx, ch *Change, t *TaskState, reason string) error {
	ch.Changed = true
	t.Task.GoalRevision++
	t.Task.ControlRevision++
	t.Task.Revision++
	t.PendingDecision = ""
	t.LastInputDigest = ""
	t.NoProgressCount = 0
	if err := c.closeInputs(tx, t, "superseded"); err != nil {
		return err
	}
	clearWait(t, "input", "")
	clearWait(t, "dependency", "")
	for _, kind := range []string{"check", "coverage", "candidate", "acceptance", "operation"} {
		rows, err := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: kind, Current: "@current", Limit: c.Config.Limits.Checks + 1})
		if err != nil {
			return err
		}
		if len(rows) > c.Config.Limits.Checks {
			return durable.ErrInvariant
		}
		for _, r := range rows {
			if err = c.Repository.Deselect(tx, t, kind, r.CurrentKey); err != nil {
				return err
			}
		}
	}
	if err := c.Repository.Put(tx, t, rec("goal", ID("goal", t.Task.TaskID, stamp(ch.Now), Hash(t.Task.Requirements)), t.Task.TaskID, t.Task.GoalRevision, "closed", true, struct {
		Goal         api.ContentRef
		Constraints  []string
		Requirements []api.Requirement
	}{t.Task.GoalRef, t.Constraints, t.Task.Requirements})); err != nil {
		return err
	}
	if err := c.controls(tx, ch, t); err != nil {
		return err
	}
	queue(ch, t, "decide", "task", t.Task.TaskID)
	queue(ch, t, "verify", "task", t.Task.TaskID)
	for _, child := range sortedTasks(taskValues(ch.Tasks)) {
		if !descendant(ch, child, t.Task.TaskID) || terminal(child) {
			continue
		}
		child.Task.Status = "cancelled"
		child.FailureReason = reason
		child.Task.ControlRevision++
		child.Task.Revision++
		if err := c.Repository.Capacity(tx, child.SubjectID, -1, c.Config.Limits.ActivePerUser); err != nil {
			return err
		}
		if err := c.closeInputs(tx, child, "superseded"); err != nil {
			return err
		}
		if err := c.controls(tx, ch, child); err != nil {
			return err
		}
		if err := c.persist(tx, ch, child); err != nil {
			return err
		}
	}
	return c.persist(tx, ch, t)
}
func (c *Coordinator) closeInputs(tx *durable.Tx, t *TaskState, state string) error {
	rows, err := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "input", State: "open", Limit: 101})
	if err != nil {
		return err
	}
	if len(rows) > 100 {
		return durable.ErrInvariant
	}
	for _, r := range rows {
		v, err := Decode[InputState](&r)
		if err != nil {
			return err
		}
		v.View.State = state
		v.View.Revision++
		if err = c.Repository.Put(tx, t, rec("input", r.ID, t.Task.TaskID, v.View.Revision, "closed", false, v)); err != nil {
			return err
		}
		if err = c.Repository.Put(tx, t, rec("input_revision", ID("input_revision", r.ID, Hash(v.View.Revision)), t.Task.TaskID, v.View.Revision, "closed", true, v)); err != nil {
			return err
		}
	}
	clearWait(t, "input", "")
	return nil
}
func (c *Coordinator) consumeInput(tx *durable.Tx, ch *Change, t *TaskState, p Prepared) (any, error) {
	var id string
	var rev int64
	if p.Intent.Method() == "task.input" {
		var in api.TaskInputInput
		_ = json.Unmarshal(p.Intent.Value().Payload, &in)
		id, rev = in.RequestID, in.RequestRevision
	} else {
		var in api.TaskAcceptResultInput
		_ = json.Unmarshal(p.Intent.Value().Payload, &in)
		id, rev = in.RequestID, in.RequestRevision
	}
	r, err := c.Repository.Get(tx, "input", id)
	if err != nil {
		return nil, err
	}
	if r == nil || r.TaskID != t.Task.TaskID {
		return nil, failure("not_found", "input request is not owned by the original task")
	}
	v, err := Decode[InputState](r)
	if err != nil {
		return nil, err
	}
	expires, _ := time.Parse(time.RFC3339Nano, v.View.Deadline)
	if v.View.State != "open" || v.View.Revision != rev || v.GoalRevision != t.Task.GoalRevision || !ch.Now.Before(expires) {
		return nil, failure("revision_conflict", "input request is consumed, superseded, expired or no longer applicable")
	}
	if p.Intent.Method() == "task.input" {
		if v.View.Kind != "clarification" {
			return nil, failure("precondition_failed", "plain input cannot consume an acceptance request")
		}
		if err = answerValid(p.Answer, v.View); err != nil {
			return nil, err
		}
		var in api.TaskInputInput
		_ = json.Unmarshal(p.Intent.Value().Payload, &in)
		if Hash(in.PreviewRefs) != Hash(v.View.RequiredContentRefs) {
			return nil, failure("precondition_failed", "exact required preview references are missing")
		}
	} else {
		var in api.TaskAcceptResultInput
		_ = json.Unmarshal(p.Intent.Value().Payload, &in)
		if v.View.Kind != "acceptance" || v.View.CandidateHash == nil || *v.View.CandidateHash != in.CandidateHash || in.GoalRevision != t.Task.GoalRevision {
			return nil, failure("precondition_failed", "acceptance must bind the exact current candidate")
		}
		proof := p.Confirmation
		if proof == nil || in.ConfirmationRef.OwnerID != c.Config.Scope.OwnerID || proof.OwnerID != c.Config.Scope.OwnerID || proof.ConfirmationID != in.ConfirmationRef.ID || proof.Revision != in.ConfirmationRef.Revision || proof.State != "approved" || proof.ConsumerCommandID != p.Intent.CommandID() || proof.ConsumerMethod != p.Intent.Method() || proof.ConsumerTargetID != t.Task.TaskID || proof.TrustedUserSessionRef == nil || proof.DecidedBy == nil || *proof.DecidedBy != t.SubjectID || p.Caller.ActorID != t.SubjectID || validateValue("ConfirmationRecord", proof) != nil {
			return nil, failure("confirmation_required", "original trusted confirmation is not approved")
		}
		if Hash(proof.ConsumerCommand) != Hash(Raw(p.Intent.Value())) {
			return nil, failure("precondition_failed", "confirmation binds another complete command")
		}
		if proof.IntentHash != Hash(map[string]any{"owner_id": c.Config.Scope.OwnerID, "consumer_command": proof.ConsumerCommand}) {
			return nil, failure("precondition_failed", "confirmation has a different canonical owner intent")
		}
		expiry, _ := time.Parse(time.RFC3339Nano, proof.ExpiresAt)
		if !ch.Now.Before(expiry) {
			return nil, failure("expired", "trusted confirmation expired")
		}
		used, err := c.Repository.Get(tx, "confirmation", proof.ConfirmationID)
		if err != nil {
			return nil, err
		}
		if used != nil {
			return nil, failure("precondition_failed", "confirmation was already consumed")
		}
		consumed := *proof
		consumed.State = "consumed"
		consumed.Revision++
		consumed.ConsumedBy = ptr(p.Intent.CommandID())
		consumed.ConsumedAt = ptr(stamp(ch.Now))
		ch.Changed = true
		if err = c.Repository.Put(tx, t, rec("confirmation", proof.ConfirmationID, t.Task.TaskID, proof.Revision, "closed", true, struct {
			Record    api.ConfirmationRecord
			CommandID string
		}{consumed, p.Intent.CommandID()})); err != nil {
			return nil, err
		}
	}
	ch.Changed = true
	v.View.State = "consumed"
	v.View.ConsumedBy = func() *string { s := p.Intent.CommandID(); return &s }()
	v.View.Revision++
	if p.Intent.Method() == "task.accept_result" {
		row := rec("acceptance", id, t.Task.TaskID, v.View.Revision, "closed", true, v)
		row.CurrentKey = id
		if err = c.Repository.Put(tx, t, row); err != nil {
			return nil, err
		}
	}
	if err = c.Repository.Put(tx, t, rec("input", id, t.Task.TaskID, v.View.Revision, "closed", false, v)); err != nil {
		return nil, err
	}
	if err = c.Repository.Put(tx, t, rec("input_revision", ID("input_revision", id, Hash(v.View.Revision)), t.Task.TaskID, v.View.Revision, "closed", true, v)); err != nil {
		return nil, err
	}
	if err = c.Repository.Put(tx, t, rec("input_consumption", id, t.Task.TaskID, 1, "closed", true, struct {
		CommandID string
		Answer    json.RawMessage
		Request   InputState
	}{p.Intent.CommandID(), p.Answer, v})); err != nil {
		return nil, err
	}
	clearWait(t, "input", id)
	t.NoProgressCount = 0
	t.LastInputDigest = Hash(struct {
		Goal        int64
		ID, Command string
	}{t.Task.GoalRevision, id, p.Intent.CommandID()})
	t.Task.Revision++
	if p.Intent.Method() == "task.input" {
		queue(ch, t, "decide", "task", t.Task.TaskID)
	}
	queue(ch, t, "verify", "task", t.Task.TaskID)
	if err = c.persist(tx, ch, t); err != nil {
		return nil, err
	}
	return api.InputConsumption{RequestID: id, RequestRevision: rev, ConsumedBy: p.Intent.CommandID(), TaskID: t.Task.TaskID}, nil
}
func answerValid(raw []byte, request api.InputRequestView) error {
	canonical, err := durable.CanonicalJSON(raw)
	if err != nil || !bytes.Equal(raw, canonical) {
		return failure("invalid_argument", "input-answer requires exact JCS bytes")
	}
	var body map[string]json.RawMessage
	if json.Unmarshal(raw, &body) != nil || len(body) != 3 {
		return failure("invalid_argument", "plain answer has exactly format/action_id/fields")
	}
	var format, action string
	_ = json.Unmarshal(body["format"], &format)
	_ = json.Unmarshal(body["action_id"], &action)
	if format != "input-answer/1" {
		return failure("invalid_argument", "unknown answer format")
	}
	allowed := false
	for _, a := range request.AllowedActions {
		allowed = allowed || a == action
	}
	if !allowed {
		return failure("invalid_argument", "answer action is not declared")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(body["fields"], &fields) != nil || fields == nil {
		return failure("invalid_argument", "answer fields must be an object")
	}
	known := map[string]bool{}
	for _, f := range request.Schema.Fields {
		known[f.Name] = true
		value, ok := fields[f.Name]
		if !ok {
			if f.Required {
				return failure("invalid_argument", "required answer field is missing")
			}
			continue
		}
		if string(value) == "null" {
			return failure("invalid_argument", "null is not an omitted answer")
		}
		switch f.Type {
		case "text":
			var s string
			if json.Unmarshal(value, &s) != nil || f.MaxLength != nil && int64(utf8.RuneCountInString(s)) > *f.MaxLength {
				return failure("invalid_argument", "text answer does not match its field")
			}
		case "boolean":
			var b bool
			if json.Unmarshal(value, &b) != nil {
				return failure("invalid_argument", "boolean answer required")
			}
		case "integer":
			var n int64
			if json.Unmarshal(value, &n) != nil || n < -9007199254740991 || n > 9007199254740991 || f.Minimum != nil && n < *f.Minimum || f.Maximum != nil && n > *f.Maximum {
				return failure("invalid_argument", "integer answer is outside its field range")
			}
		case "choice", "choices":
			options := map[string]int{}
			if f.Options != nil {
				for i, o := range *f.Options {
					options[o.ID] = i
				}
			}
			values := []string{}
			if f.Type == "choice" {
				var s string
				if json.Unmarshal(value, &s) != nil {
					return failure("invalid_argument", "choice ID required")
				}
				values = []string{s}
			} else {
				if json.Unmarshal(value, &values) != nil || values == nil {
					return failure("invalid_argument", "ordered choices required")
				}
				if f.MaxChoices != nil && int64(len(values)) > *f.MaxChoices {
					return failure("invalid_argument", "too many choices")
				}
			}
			last := -1
			for _, id := range values {
				i, ok := options[id]
				if !ok || i <= last {
					return failure("invalid_argument", "choices must be unique declared IDs in declaration order")
				}
				last = i
			}
		default:
			return failure("unsupported", "request field type is unsupported")
		}
	}
	for name := range fields {
		if !known[name] {
			return failure("invalid_argument", "answer contains an undeclared field")
		}
	}
	return nil
}
