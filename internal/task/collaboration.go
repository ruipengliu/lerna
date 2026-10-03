package task

import (
	"context"
	"fmt"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func (s *Service) Delegate(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, c api.Command, in DelegateInput) (DelegateOutput, error) {
	var out DelegateOutput
	err := s.transaction(ctx, store, scope, func(tx runtime.Tx) error { var e error; out, e = s.DelegateTx(ctx, tx, auth, c, in); return e })
	return out, err
}
func (s *Service) DelegateTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in DelegateInput) (DelegateOutput, error) {
	if !api.ValidID(in.DelegationID) || !api.ValidID(in.ReceiverID) || c.TargetID != in.DelegationID {
		return DelegateOutput{}, invalid("invalid_delegation_identity")
	}
	if in.ParentTaskRef.OwnerID != tx.Scope().OwnerID || in.ParentTaskRef.TenantID != tx.Scope().TenantID {
		return DelegateOutput{}, api.E("forbidden", "parent_owner_mismatch")
	}
	t, e := getTask(ctx, tx, in.ParentTaskRef.ObjectID)
	if e != nil {
		return DelegateOutput{}, e
	}
	if e = principal(auth, t); e != nil {
		return DelegateOutput{}, e
	}
	if e = s.CheckCurrent(ctx, tx, t, true); e != nil {
		return DelegateOutput{}, e
	}
	if in.ParentGoalRevision != t.Task.GoalRevision || in.ParentTaskRef.Revision != t.Task.Revision {
		return DelegateOutput{}, api.E("revision_conflict", "parent_goal_changed")
	}
	if t.Task.RequirementsState != "ready" {
		return DelegateOutput{}, api.E("invalid_state", "requirements_not_ready")
	}
	if t.DelegationsCreated >= t.Policy.MaxDelegations {
		return DelegateOutput{}, api.E("invalid_state", "delegation_limit")
	}
	if uint64(len(t.Ancestors)+1) >= t.Policy.MaxDepth {
		return DelegateOutput{}, api.E("invalid_state", "max_depth")
	}
	if e = s.authorize(ctx, tx, auth, "task.delegate", append([]api.ContentRef{in.GoalRef}, in.InputRefs...), append([]api.ObjectRef{in.AgentBindingRef}, in.PermissionRefs...)); e != nil {
		return DelegateOutput{}, e
	}
	rows, e := s.fullRelations(ctx, tx, t.Task.TaskID)
	if e != nil {
		return DelegateOutput{}, e
	}
	active := uint64(0)
	for _, r := range rows {
		if r.Kind == "delegation" && !r.Closed {
			active++
		}
	}
	if active >= s.config.MaxActiveChildren {
		return DelegateOutput{}, api.E("invalid_state", "active_child_limit")
	}
	if in.Internal {
		if in.ReceiverID != tx.Scope().OwnerID {
			return DelegateOutput{}, api.E("forbidden", "internal_owner_mismatch")
		}
		root := t.Task.TaskID
		if len(t.Ancestors) > 0 {
			root = t.Ancestors[0]
		}
		count, e := s.subtreeCount(ctx, tx, root)
		if e != nil {
			return DelegateOutput{}, e
		}
		if count >= s.config.MaxActiveSubtree {
			return DelegateOutput{}, api.E("invalid_state", "active_subtree_limit")
		}
	}
	allocationID := api.NewID("allocation")
	allocationCommand := api.Command{TargetID: allocationID, CommandID: api.NewID("command")}
	allocation, e := s.AllocateTx(ctx, tx, auth, allocationCommand, AllocateInput{AllocationID: allocationID, ParentTaskRef: taskRef(tx, t), ReceiverID: in.ReceiverID, Limits: in.Budget, Deadline: in.Deadline})
	if e != nil {
		return DelegateOutput{}, e
	}
	t, e = getTask(ctx, tx, t.Task.TaskID)
	if e != nil {
		return DelegateOutput{}, e
	}
	ancestorRefs := []api.ObjectRef{}
	for _, id := range t.Ancestors {
		a, e := getTask(ctx, tx, id)
		if e != nil {
			return DelegateOutput{}, e
		}
		ancestorRefs = append(ancestorRefs, taskRef(tx, a))
	}
	ancestorRefs = append(ancestorRefs, taskRef(tx, t))
	d := Delegation{DelegateInput: in, Revision: 1, CreationKey: in.DelegationID, CommandRef: tx.Scope().Ref(c.CommandID, 1), AllocationRef: allocation.AllocationRef, AncestorTaskRefs: ancestorRefs, Phase: "preparing"}
	if in.Internal {
		childID := api.NewID("task")
		var a Allocation
		if _, e = tx.Get(ctx, allocations, allocationID, &a); e != nil {
			return DelegateOutput{}, e
		}
		trusted := auth
		trusted.Roles = append(append([]string{}, auth.Roles...), "service")
		inc, e := s.ReceiveAllocationTx(ctx, tx, trusted, d.AllocationRef, a)
		if e != nil {
			return DelegateOutput{}, e
		}
		childCommand := api.Command{TargetID: childID, CommandID: api.NewID("command")}
		sub, e := s.SubmitTx(ctx, tx, auth, childCommand, SubmitInput{OrchestratorID: tx.Scope().OwnerID, GoalRef: in.GoalRef, PolicyRef: in.PolicyRef, Deadline: in.Deadline, Budget: in.Budget})
		if e != nil {
			return DelegateOutput{}, e
		}
		child, e := getTask(ctx, tx, childID)
		if e != nil {
			return DelegateOutput{}, e
		}
		child.ParentTaskID = t.Task.TaskID
		child.Ancestors = append(append([]string{}, t.Ancestors...), t.Task.TaskID)
		child.IncomingAllocationID = incomingID(d.AllocationRef)
		if e = s.saveTask(ctx, tx, &child); e != nil {
			return DelegateOutput{}, e
		}
		ref := taskRef(tx, child)
		inc.TaskRef = &ref
		inc.Revision++
		if e = tx.Put(ctx, incoming, incomingID(d.AllocationRef), inc.Revision-1, inc); e != nil {
			return DelegateOutput{}, e
		}
		d.ChildTaskRef = &sub.TaskRef
		d.ChildTaskRef.Revision = child.Task.Revision
		d.Phase = "active"
		d.Sent = true
		r := relation{ID: relationID(t.Task.TaskID, "child", tx.Scope().OwnerID, childID), Revision: 1, TaskID: t.Task.TaskID, Kind: "child", Ref: ref, Purpose: "goal_action", MayApplyLater: true, Effect: "unknown"}
		if e = tx.Create(ctx, relations, r.ID, t.Task.TaskID, r); e != nil {
			return DelegateOutput{}, e
		}
	}
	if e = tx.Create(ctx, delegations, d.DelegationID, t.Task.TaskID, d); e != nil {
		return DelegateOutput{}, e
	}
	r := relation{ID: relationID(t.Task.TaskID, "delegation", tx.Scope().OwnerID, d.DelegationID), Revision: 1, TaskID: t.Task.TaskID, Kind: "delegation", Ref: tx.Scope().Ref(d.DelegationID, 1), Purpose: "goal_action", MayApplyLater: true, Effect: "unknown"}
	if e = tx.Create(ctx, relations, r.ID, t.Task.TaskID, r); e != nil {
		return DelegateOutput{}, e
	}
	t.DelegationsCreated++
	if e = s.updateSummary(ctx, tx, &t); e != nil {
		return DelegateOutput{}, e
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return DelegateOutput{}, e
	}
	if _, e = raise(ctx, tx, JobDelegation, "delegation/"+d.DelegationID, tx.Scope().Ref(d.DelegationID, 1)); e != nil {
		return DelegateOutput{}, e
	}
	return DelegateOutput{DelegationRef: tx.Scope().Ref(d.DelegationID, 1), AllocationRef: d.AllocationRef, ChildTaskRef: d.ChildTaskRef}, nil
}
func (s *Service) subtreeCount(ctx context.Context, tx runtime.Tx, root string) (uint64, error) {
	queue := []string{root}
	seen := map[string]bool{}
	count := uint64(0)
	for len(queue) > 0 {
		id := queue[0]
		queue = queue[1:]
		if seen[id] {
			return 0, api.E("dependency_unavailable", "subtree_cycle")
		}
		seen[id] = true
		if uint64(len(seen)) > s.config.MaxActiveSubtree+1 {
			return 0, api.E("dependency_unavailable", "subtree_capacity")
		}
		t, e := getTask(ctx, tx, id)
		if e != nil {
			return 0, e
		}
		if !terminal(t) {
			count++
		}
		rows, e := s.fullRelations(ctx, tx, id)
		if e != nil {
			return 0, e
		}
		for _, r := range rows {
			if r.Kind == "child" {
				queue = append(queue, r.Ref.ObjectID)
			}
		}
	}
	return count, nil
}
func (s *Service) MergeDelegation(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, fact DelegationFact) error {
	return s.transaction(ctx, store, scope, func(tx runtime.Tx) error { return s.MergeDelegationTx(ctx, tx, auth, fact) })
}
func (s *Service) MergeDelegationTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, f DelegationFact) error {
	var d Delegation
	if _, e := tx.Get(ctx, delegations, f.DelegationID, &d); e != nil {
		return e
	}
	if !auth.HasRole("service") && auth.SubjectID != d.ReceiverID {
		return api.E("forbidden", "receiver_identity_required")
	}
	if f.Revision == 0 {
		return invalid("invalid_fact_revision")
	}
	digest, e := api.Digest(f)
	if e != nil {
		return e
	}
	if f.Revision < d.SourceRevision {
		return nil
	}
	if f.Revision == d.SourceRevision {
		if digest != d.SourceDigest {
			return api.E("idempotency_conflict", "digest_conflict")
		}
		return nil
	}
	if f.ChildTaskRef != nil {
		if f.ChildTaskRef.TenantID != tx.Scope().TenantID || f.ChildTaskRef.OwnerID != d.ReceiverID {
			return api.E("forbidden", "child_scope_mismatch")
		}
		if d.ChildTaskRef != nil && refKey(*d.ChildTaskRef) != refKey(*f.ChildTaskRef) {
			return api.E("idempotency_conflict", "child_mapping_changed")
		}
		d.ChildTaskRef = f.ChildTaskRef
	}
	if d.GoalWorkClosed && !f.GoalWorkClosed || d.EffectsClosed && !f.EffectsClosed {
		return api.E("invalid_state", "closed_delegation_reopened")
	}
	d.GoalWorkClosed = f.GoalWorkClosed
	d.EffectsClosed = f.EffectsClosed
	d.SourceRevision = f.Revision
	d.SourceDigest = digest
	d.Phase = "preparing"
	if d.ChildTaskRef != nil {
		d.Phase = "active"
	}
	if len(f.Gaps) > 0 {
		d.Phase = "reconciling"
	}
	if f.ClosureRef != nil {
		if !f.GoalWorkClosed || !f.EffectsClosed || !f.TransfersClosed || !f.UsageFinal || f.ClosureRef.TenantID != tx.Scope().TenantID || f.ClosureRef.OwnerID != d.ReceiverID {
			return api.E("invalid_request", "closure_unverified")
		}
		d.ClosureRef = f.ClosureRef
		d.Phase = "closed"
	}
	d.Revision++
	if e = tx.Put(ctx, delegations, d.DelegationID, d.Revision-1, d); e != nil {
		return e
	}
	t, e := getTask(ctx, tx, d.ParentTaskRef.ObjectID)
	if e != nil {
		return e
	}
	id := relationID(t.Task.TaskID, "delegation", tx.Scope().OwnerID, d.DelegationID)
	var r relation
	if _, e = tx.Get(ctx, relations, id, &r); e != nil {
		return e
	}
	r.Closed = d.GoalWorkClosed && d.EffectsClosed
	r.MayApplyLater = !d.EffectsClosed
	r.SourceRevision = f.Revision
	r.SourceDigest = digest
	r.Ref.Revision = d.Revision
	r.Revision++
	if e = tx.Put(ctx, relations, id, r.Revision-1, r); e != nil {
		return e
	}
	if e = s.updateSummary(ctx, tx, &t); e != nil {
		return e
	}
	if f.GoalWorkClosed && f.EffectsClosed {
		t.NoProgress = 0
	}
	if e = s.saveTask(ctx, tx, &t); e != nil {
		return e
	}
	if _, e = raise(ctx, tx, JobAdvance, "advance/"+t.Task.TaskID, taskRef(tx, t)); e != nil {
		return e
	}
	return nil
}
func (s *Service) DelegationRead(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string) (Delegation, error) {
	var d Delegation
	if _, e := store.Read(ctx, scope, delegations, id, 0, &d); e != nil {
		return d, e
	}
	_, e := s.readState(ctx, store, scope, auth, d.ParentTaskRef.ObjectID, 0)
	return d, e
}
func (s *Service) ChildCreateTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ChildCreateInput) (ChildOutput, error) {
	if c.TargetID != in.ChildID || !api.ValidID(in.ChildID) || !api.ValidID(in.SessionOwnerID) {
		return ChildOutput{}, invalid("invalid_child_identity")
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return ChildOutput{}, e
	}
	deadline, e := api.ParseTime(in.PrepareDeadline)
	if e != nil || !now.Before(deadline) || deadline.Sub(now) > time.Hour {
		return ChildOutput{}, invalid("invalid_prepare_deadline")
	}
	if e = s.authorize(ctx, tx, auth, "child.create", []api.ContentRef{in.AccessScopeRef}, []api.ObjectRef{in.AgentBindingRef}); e != nil {
		return ChildOutput{}, e
	}
	rows, e := tx.List(ctx, children, auth.SubjectID, "", int(s.config.MaxTasksPerSubject)+1)
	if e != nil {
		return ChildOutput{}, e
	}
	if uint64(len(rows)) >= s.config.MaxTasksPerSubject {
		return ChildOutput{}, api.E("overloaded", "child_handle_limit")
	}
	h := ChildHandle{ChildCreateInput: in, Revision: 1, SubjectID: auth.SubjectID, State: "preparing", SessionCommandRef: api.ObjectRef{TenantID: tx.Scope().TenantID, OwnerID: in.SessionOwnerID, ObjectID: api.NewID("command"), Revision: 1}}
	if e = tx.Create(ctx, children, in.ChildID, auth.SubjectID, h); e != nil {
		return ChildOutput{}, e
	}
	if e = tx.Create(ctx, childCommands, in.ChildID, auth.SubjectID, childCommand{CommandID: c.CommandID}); e != nil {
		return ChildOutput{}, e
	}
	if _, e = raise(ctx, tx, JobChildPrepare, "child/"+in.ChildID, tx.Scope().Ref(in.ChildID, 1)); e != nil {
		return ChildOutput{}, e
	}
	return ChildOutput{ChildRef: tx.Scope().Ref(in.ChildID, 1)}, nil
}
func (s *Service) ChildSendTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ChildSendInput) (ChildOutput, error) {
	if e := target(c, in.ChildID); e != nil {
		return ChildOutput{}, e
	}
	var h ChildHandle
	if _, e := tx.Get(ctx, children, in.ChildID, &h); e != nil {
		return ChildOutput{}, e
	}
	if auth.SubjectID != h.SubjectID && !auth.HasRole("service") {
		return ChildOutput{}, api.E("forbidden", "child_access_denied")
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != h.Revision {
		return ChildOutput{}, api.E("revision_conflict", "revision_changed")
	}
	if h.State != "open" {
		return ChildOutput{}, api.E("invalid_state", "child_not_open")
	}
	if !api.Equal(h.ActiveDelegationRef, in.ExpectedActiveDelegationRef) {
		return ChildOutput{}, api.E("revision_conflict", "active_mapping_changed")
	}
	out := ChildOutput{ChildRef: tx.Scope().Ref(h.ChildID, h.Revision), ChildSessionRef: h.ChildSessionRef}
	switch in.Mode {
	case "new_goal":
		if in.Delegation == nil || in.DelegationRef != nil {
			return out, invalid("new_goal_input_required")
		}
		if h.ActiveDelegationRef != nil {
			var old Delegation
			if _, e := tx.Get(ctx, delegations, h.ActiveDelegationRef.ObjectID, &old); e != nil {
				return out, e
			}
			if !old.GoalWorkClosed {
				return out, api.E("invalid_state", "previous_goal_open")
			}
			if !old.EffectsClosed {
				return out, api.E("effect_unknown", "previous_effect_unknown")
			}
		}
		d := *in.Delegation
		if d.ParentTaskRef.OwnerID != tx.Scope().OwnerID {
			return out, api.E("forbidden", "parent_owner_mismatch")
		}
		if !api.Equal(d.AgentBindingRef, h.AgentBindingRef) {
			return out, api.E("forbidden", "delegation_scope_exceeded")
		}
		if e := s.authorize(ctx, tx, auth, "child.new_goal", []api.ContentRef{h.AccessScopeRef}, d.PermissionRefs); e != nil {
			return out, e
		}
		dc := c
		dc.TargetID = d.DelegationID
		accepted, e := s.DelegateTx(ctx, tx, auth, dc, d)
		if e != nil {
			return out, e
		}
		h.ActiveDelegationRef = &accepted.DelegationRef
		out.DelegationRef = &accepted.DelegationRef
	case "continue_existing":
		if in.DelegationRef == nil || h.ActiveDelegationRef == nil || !api.Equal(*in.DelegationRef, *h.ActiveDelegationRef) {
			return out, api.E("revision_conflict", "active_mapping_changed")
		}
		var d Delegation
		if _, e := tx.Get(ctx, delegations, in.DelegationRef.ObjectID, &d); e != nil {
			return out, e
		}
		if d.GoalWorkClosed || d.CloseRequested {
			return out, api.E("invalid_state", "delegation_closed")
		}
		if in.ParentGoalRevision != d.ParentGoalRevision {
			return out, api.E("revision_conflict", "parent_goal_changed")
		}
		content := []api.ContentRef{}
		refs := []api.ObjectRef{}
		switch in.Kind {
		case "answer_request":
			if in.RequestRef == nil || in.AnswerRef == nil {
				return out, invalid("answer_request_required")
			}
			content = append(content, *in.AnswerRef)
			refs = append(refs, *in.RequestRef)
		case "steer":
			if in.ChildGoalRevision == 0 || in.ContentRef == nil {
				return out, invalid("steer_input_required")
			}
			content = append(content, *in.ContentRef)
		default:
			return out, invalid("transfer_kind")
		}
		if e := s.authorize(ctx, tx, auth, "child.continue", content, refs); e != nil {
			return out, e
		}
		tr := Transfer{TransferID: api.NewID("transfer"), Revision: 1, DelegationID: d.DelegationID, Kind: "input", CommandRef: tx.Scope().Ref(api.NewID("command"), 1), State: "queued", Input: in}
		if e := tx.Create(ctx, transfers, tr.TransferID, h.ChildID, tr); e != nil {
			return out, e
		}
		if _, e := raise(ctx, tx, JobChildTransfer, "transfer/"+tr.TransferID, tx.Scope().Ref(tr.TransferID, 1)); e != nil {
			return out, e
		}
		out.DelegationRef = in.DelegationRef
	default:
		return out, invalid("child_send_mode")
	}
	h.Revision++
	if e := tx.Put(ctx, children, h.ChildID, h.Revision-1, h); e != nil {
		return out, e
	}
	out.ChildRef = tx.Scope().Ref(h.ChildID, h.Revision)
	return out, nil
}
func (s *Service) ChildCloseTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in ChildCloseInput) (ChildCloseOutput, error) {
	if e := target(c, in.ChildID); e != nil {
		return ChildCloseOutput{}, e
	}
	var h ChildHandle
	if _, e := tx.Get(ctx, children, in.ChildID, &h); e != nil {
		return ChildCloseOutput{}, e
	}
	if auth.SubjectID != h.SubjectID && !auth.HasRole("service") {
		return ChildCloseOutput{}, api.E("forbidden", "child_access_denied")
	}
	if c.ExpectedRevision == nil || *c.ExpectedRevision != h.Revision {
		return ChildCloseOutput{}, api.E("revision_conflict", "revision_changed")
	}
	out := ChildCloseOutput{ChildRef: tx.Scope().Ref(h.ChildID, h.Revision), Targets: []CloseTarget{}}
	if h.State == "closed" {
		return out, nil
	}
	h.State = "closed"
	h.CloseCancelActive = in.CancelActive
	if h.ActiveDelegationRef != nil {
		var d Delegation
		if _, e := tx.Get(ctx, delegations, h.ActiveDelegationRef.ObjectID, &d); e != nil {
			return out, e
		}
		state := "continuing"
		if d.GoalWorkClosed && d.EffectsClosed {
			state = "closed"
		} else if in.CancelActive {
			d.CloseRequested = true
			d.Revision++
			if e := tx.Put(ctx, delegations, d.DelegationID, d.Revision-1, d); e != nil {
				return out, e
			}
			if _, e := raise(ctx, tx, JobDelegation, "delegation/"+d.DelegationID, tx.Scope().Ref(d.DelegationID, d.Revision)); e != nil {
				return out, e
			}
			state = "cancel_requested"
		}
		out.Targets = append(out.Targets, CloseTarget{DelegationRef: *h.ActiveDelegationRef, State: state})
	}
	trs, e := tx.List(ctx, transfers, h.ChildID, "", int(s.config.MaxRelations)+1)
	if e != nil {
		return out, e
	}
	if uint64(len(trs)) > s.config.MaxRelations {
		return out, api.E("dependency_unavailable", "transfer_index_capacity")
	}
	for _, row := range trs {
		var tr Transfer
		if e = row.Decode(&tr); e != nil {
			return out, e
		}
		if tr.State == "queued" {
			tr.State = "closed"
			tr.Revision++
			if e = tx.Put(ctx, transfers, tr.TransferID, tr.Revision-1, tr); e != nil {
				return out, e
			}
		}
	}
	h.Revision++
	if e = tx.Put(ctx, children, h.ChildID, h.Revision-1, h); e != nil {
		return out, e
	}
	out.ChildRef = tx.Scope().Ref(h.ChildID, h.Revision)
	return out, nil
}
func (s *Service) ChildRead(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string) (ChildHandle, error) {
	var h ChildHandle
	_, e := store.Read(ctx, scope, children, id, 0, &h)
	if e != nil {
		return h, e
	}
	if h.SubjectID != auth.SubjectID && !auth.HasRole("service") {
		return ChildHandle{}, api.E("forbidden", "child_access_denied")
	}
	return h, nil
}
func (s *Service) ChildWait(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, in ChildWaitInput) (ChildWaitOutput, error) {
	if in.TimeoutMS > 5000 || in.WaitFor != "goal_closed" && in.WaitFor != "effects_closed" && in.WaitFor != "closure" {
		return ChildWaitOutput{}, invalid("invalid_wait")
	}
	h, e := s.ChildRead(ctx, store, scope, auth, in.ChildID)
	if e != nil {
		return ChildWaitOutput{}, e
	}
	if in.DelegationRef.OwnerID != scope.OwnerID || in.DelegationRef.TenantID != scope.TenantID {
		return ChildWaitOutput{}, api.E("forbidden", "delegation_scope_mismatch")
	}
	// 固定原delegation，active指针后续改变不会改变本次等待对象。
	var d Delegation
	if _, e = store.Read(ctx, scope, delegations, in.DelegationRef.ObjectID, 0, &d); e != nil {
		return ChildWaitOutput{}, e
	}
	if !api.Equal(d.AgentBindingRef, h.AgentBindingRef) {
		return ChildWaitOutput{}, api.E("forbidden", "delegation_scope_exceeded")
	}
	deadline := time.NewTimer(time.Duration(in.TimeoutMS) * time.Millisecond)
	defer deadline.Stop()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		met := d.GoalWorkClosed
		switch in.WaitFor {
		case "effects_closed":
			met = d.EffectsClosed
		case "closure":
			met = d.ClosureRef != nil
		}
		out := ChildWaitOutput{ObservedRevision: d.Revision, ConditionMet: met, Delegation: d, Gaps: []string{}}
		if !d.GoalWorkClosed {
			out.Gaps = append(out.Gaps, "goal_work_open")
		}
		if !d.EffectsClosed {
			out.Gaps = append(out.Gaps, "effects_open")
		}
		if in.WaitFor == "closure" && d.ClosureRef == nil {
			out.Gaps = append(out.Gaps, "closure_pending")
		}
		if met || in.TimeoutMS == 0 {
			return out, nil
		}
		select {
		case <-ctx.Done():
			return ChildWaitOutput{}, ctx.Err()
		case <-deadline.C:
			return out, nil
		case <-ticker.C:
			if _, e = store.Read(ctx, scope, delegations, in.DelegationRef.ObjectID, 0, &d); e != nil {
				return ChildWaitOutput{}, e
			}
		}
	}
}
func (s *Service) DelegationClosureRead(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string) (DelegationClosure, error) {
	var c DelegationClosure
	_, e := store.Read(ctx, scope, delegationClosures, id, 0, &c)
	if e != nil {
		return c, e
	}
	_, e = s.DelegationRead(ctx, store, scope, auth, c.DelegationID)
	return c, e
}
func childCollectionKey(childID, delegationID string) string {
	return fmt.Sprintf("%s/%s", childID, delegationID)
}
