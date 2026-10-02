package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/contracts"
	"github.com/ruipengliu/lerna/internal/durable"
)

func taskRef(t *TaskState) api.TaskRef {
	return api.TaskRef{TaskID: t.Task.TaskID, OrchestratorID: t.Task.OrchestratorID}
}
func gate(ch *Change, t *TaskState) api.TaskGate {
	v := api.TaskGate{TaskID: t.Task.TaskID, OrchestratorID: t.Task.OrchestratorID, GoalRevision: t.Task.GoalRevision, ControlRevision: t.Task.ControlRevision, Status: t.Task.Status, Control: t.Task.Control}
	for id := t.ParentID; id != ""; {
		p := ch.Tasks[id]
		if p == nil {
			v.Control = "paused"
			break
		}
		if terminal(p) || p.Task.Control == "paused" {
			v.Control = "paused"
		}
		id = p.ParentID
	}
	return v
}
func (c *Coordinator) decide(ctx context.Context, u Unit, f frame) error {
	// A prepared snapshot is resumed before any new continuation is counted.
	var prep SnapshotPreparation
	err := c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		if t.PendingDecision != "" {
			queue(ch, t, "poll", "decision", t.PendingDecision)
			return durable.Done(), nil
		}
		candidate, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "candidate", Current: "candidate", Limit: 2})
		if e != nil {
			return durable.Disposition{}, e
		}
		if len(candidate) > 0 {
			return durable.Done(), nil
		}
		if err := c.canAdvance(ch, t); err != nil {
			queue(ch, t, "verify", "task", t.Task.TaskID)
			return durable.Done(), nil
		}
		if t.NoProgressCount >= t.Policy.MaxNoProgress {
			queue(ch, t, "verify", "task", t.Task.TaskID)
			return durable.Done(), nil
		}
		if e := c.batchBudget(t, preparedProposal{Actions: []preparedAction{{Preparation: api.ActionPreparation{UpperBound: t.Policy.DecisionUpperBound}}}}); e != nil {
			wait(t, "budget", "adjust_original_budget_or_close_task", nil)
			t.Task.Revision++
			queue(ch, t, "verify", "task", t.Task.TaskID)
			return durable.Done(), c.persist(tx, ch, t)
		}
		materials := []api.BrainContextMaterialsItem{}
		rows, e := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "material", Current: "@current", Limit: c.Config.Limits.Facts + 1})
		if e != nil {
			return durable.Disposition{}, e
		}
		if len(rows) > c.Config.Limits.Facts {
			return durable.Disposition{}, failure("precondition_failed", "context projection is incomplete")
		}
		for _, r := range rows {
			v, e := Decode[api.BrainContextMaterialsItem](&r)
			if e != nil {
				return durable.Disposition{}, e
			}
			materials = append(materials, v)
		}
		facts := []api.BrainContextFactsItem{}
		rows, e = c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "context_fact", Current: "@current", Limit: c.Config.Limits.Facts + 1})
		if e != nil {
			return durable.Disposition{}, e
		}
		if len(rows) > c.Config.Limits.Facts {
			return durable.Disposition{}, failure("precondition_failed", "fact context is incomplete")
		}
		for _, r := range rows {
			v, e := Decode[api.BrainContextFactsItem](&r)
			if e != nil {
				return durable.Disposition{}, e
			}
			facts = append(facts, v)
		}
		t.SnapshotRevision++
		t.ContinuationCount++
		id := ID("decision", t.Task.TaskID, strconv.FormatInt(t.SnapshotRevision, 10))
		rid := reserveID("decision", id)
		if e = c.reserve(tx, ch, t, Reservation{ID: rid, TaskID: t.Task.TaskID, ObjectOwnerID: t.Policy.BrainID, ObjectKind: "decision", ObjectID: id, UpperBound: t.Policy.DecisionUpperBound, Usage: zeroUsage(t.Policy.DecisionUpperBound)}); e != nil {
			return durable.Disposition{}, e
		}
		contextValue := api.BrainContext{SchemaVersion: "brain-context/1", TaskRef: taskRef(t), SnapshotRevision: t.SnapshotRevision, GoalRevision: t.Task.GoalRevision, ControlRevision: t.Task.ControlRevision, GoalRef: t.Task.GoalRef, Requirements: t.Task.Requirements, Control: gate(ch, t).Control, Facts: facts, Assumptions: []string{}, UnresolvedEffects: []api.BrainContextUnresolvedEffectsItem{}, Materials: materials, Capabilities: t.Policy.Capabilities, Gaps: []api.BrainContextGapsItem{}, InputManifest: []api.ContentRef{t.Task.GoalRef}}
		for _, r := range facts {
			contextValue.InputManifest = append(contextValue.InputManifest, r.ContentRef)
		}
		for _, m := range materials {
			contextValue.InputManifest = append(contextValue.InputManifest, m.ContentRef)
		}
		if len(contextValue.InputManifest) > 100 {
			return durable.Disposition{}, failure("precondition_failed", "context manifest exceeds fixed scope")
		}
		body := Raw(contextValue) // PlanRef must be explicit null in the frozen context contract.
		var object map[string]json.RawMessage
		_ = json.Unmarshal(body, &object)
		object["plan_ref"] = Raw(nil)
		body = Raw(object)
		if e = contracts.Validate("BrainContext", body); e != nil {
			return durable.Disposition{}, fmt.Errorf("context: %w", e)
		}
		pub, e := publication(ID("publication", id), t.Task.TaskID, json.RawMessage(body))
		if e != nil {
			return durable.Disposition{}, e
		}
		if len(pub.Body) > c.Config.Limits.Bytes {
			return durable.Disposition{}, failure("precondition_failed", "snapshot exceeds byte budget")
		}
		if e = c.Repository.Put(tx, t, rec("publication", pub.ID, t.Task.TaskID, 1, "open", false, pub)); e != nil {
			return durable.Disposition{}, e
		}
		request := api.DecisionRequest{DecisionID: id, TaskID: t.Task.TaskID, OrchestratorID: c.Config.Scope.OwnerID, SnapshotRevision: t.SnapshotRevision, CapabilityRefs: []api.DecisionRequestCapabilityRefsItem{}, ModelProfileRef: t.Policy.ModelProfileRef, Limits: api.DecisionRequestLimits{Deadline: t.Task.Deadline, MaxActions: t.Policy.MaxActions, MaxContextRequests: t.Policy.MaxContextRequests, MaxOutputTokens: t.Policy.MaxOutputTokens, CostReservationRef: api.ObjectRef{OwnerID: c.Config.Scope.OwnerID, ID: rid, Revision: 1}}, UsageAuthorizationRefs: t.Policy.UsageAuthorizationRefs}
		for _, cap := range t.Policy.Capabilities {
			request.CapabilityRefs = append(request.CapabilityRefs, api.DecisionRequestCapabilityRefsItem{CapabilityRef: cap.CapabilityRef, BindingRef: cap.BindingRef})
		}
		prep = SnapshotPreparation{Snapshot: Snapshot{TaskID: t.Task.TaskID, Revision: t.SnapshotRevision, GoalRevision: t.Task.GoalRevision, ControlRevision: t.Task.ControlRevision, InputDigest: pub.Digest, Inputs: contextValue.InputManifest, Request: request, ReservationID: rid}, PublicationID: pub.ID}
		if e = c.Repository.Put(tx, t, rec("snapshot_preparation", id, t.Task.TaskID, 1, "open", true, prep)); e != nil {
			return durable.Disposition{}, e
		}
		t.PendingDecision = id
		t.Task.Revision++
		if e = c.persist(tx, ch, t); e != nil {
			return durable.Disposition{}, e
		}
		queue(ch, t, "poll", "decision", id)
		return durable.Done(), nil
	})
	if err != nil {
		return err
	}
	if prep.Snapshot.TaskID == "" {
		return nil
	}
	// The transaction above finishes this lease. Publication and sending are a
	// separate durable poll responsibility, never an unguarded continuation.
	return nil
}

func zeroUsage(amounts []api.Amount) []api.Amount {
	out := make([]api.Amount, len(amounts))
	for i, a := range amounts {
		out[i] = api.Amount{Unit: a.Unit, Amount: "0"}
	}
	return out
}

// startDecision completes a previously committed snapshot publication and
// stores the exact original command before contacting Brain.
func (c *Coordinator) startDecision(ctx context.Context, u Unit, f frame, id string) error {
	r, err := c.readRecord(ctx, u, "snapshot", id)
	if err != nil {
		return err
	}
	var s Snapshot
	if r == nil {
		p, e := c.readRecord(ctx, u, "snapshot_preparation", id)
		if e != nil {
			return e
		}
		prep, e := Decode[SnapshotPreparation](p)
		if e != nil {
			return e
		}
		ref, e := c.publish(ctx, u, f, prep.PublicationID)
		if e != nil {
			return c.finish(ctx, u, f, "dependency_unavailable", false)
		}
		s = prep.Snapshot
		s.Request.ContextRef = ref
		deadline, _ := time.Parse(time.RFC3339Nano, s.Request.Limits.Deadline)
		s.Call = originalCall(f.Task.Policy.BrainID, "brain.decide", f.Task.Policy.BrainID, ID("command", id, "decide"), s.Request, deadline)
		if e = validateValue("DecisionRequest", s.Request); e != nil {
			return e
		}
		e = c.changeWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
			if e := c.Repository.Put(tx, t, rec("snapshot", id, t.Task.TaskID, 1, "closed", true, s)); e != nil {
				return durable.Disposition{}, e
			}
			queue(ch, t, "poll", "decision", id)
			return durable.Done(), nil
		})
		return e
	}
	s, err = Decode[Snapshot](r)
	if err != nil {
		return err
	}
	allowed, err := c.attempt(ctx, u, f)
	if err != nil || !allowed {
		return err
	}
	var record api.DecisionRecord
	err = c.external(ctx, u, f, "brain.read", id, func() error { var e error; record, e = c.Ports.Brain.Read(ctx, f.Caller, s.Call.OwnerID, id); return e })
	var gateErr error
	live := false
	if notFound(err) {
		live, gateErr, err = c.decisionCanAdvance(ctx, u, f, s)
		if err != nil {
			return err
		}
		if live {
			err = c.external(ctx, u, f, "brain.decide", s.Call.OwnerID, func() error {
				if e := c.Ports.Authority.Current(ctx, f.Caller, api.Access{Method: "brain.decide", TargetID: s.Call.OwnerID, TaskID: s.TaskID, PolicyRef: &f.Task.Task.PolicyRef, ContentRefs: append([]api.ContentRef{s.Request.ContextRef}, s.Inputs...), AuthorizationRefs: s.Request.UsageAuthorizationRefs, Components: []api.ComponentRef{s.Request.ModelProfileRef}}, c.Ports.Clock.Now()); e != nil {
					return e
				}
				var e error
				record, e = c.Ports.Brain.Decide(ctx, f.Caller, s.Call, s.Request)
				return e
			})
		} else {
			if deferredDecisionGate(gateErr) {
				return c.deferDecision(ctx, u, f, "decision_prerequisite_pending")
			}
			call := originalCall(s.Call.OwnerID, "brain.cancel", id, ID("command", id, "cancel"), map[string]string{"decision_id": id}, c.Ports.Clock.Now().Add(c.Config.Limits.TrustedReview))
			err = c.stopDecision(ctx, u, f, s, call, &record)
		}
	}
	if err != nil {
		return c.finish(ctx, u, f, "dependency_unavailable", false)
	}
	if record.Status == "accepted" || record.Status == "running" {
		live, _, err := c.decisionCanAdvance(ctx, u, f, s)
		if err != nil {
			return err
		}
		if !live {
			call := originalCall(s.Call.OwnerID, "brain.cancel", id, ID("command", id, "cancel"), map[string]string{"decision_id": id}, c.Ports.Clock.Now().Add(c.Config.Limits.TrustedReview))
			if err = c.stopDecision(ctx, u, f, s, call, &record); err != nil {
				return c.finish(ctx, u, f, "dependency_unavailable", false)
			}
		}
	}
	return c.mergeDecision(ctx, u, f, s, record)
}

func deferredDecisionGate(err error) bool {
	var f *api.Failure
	if !errors.As(err, &f) {
		return false
	}
	return f.Detail.Code == "effect_unknown" || strings.HasPrefix(f.Detail.Message, "task has unresolved prerequisites")
}

func (c *Coordinator) deferDecision(ctx context.Context, u Unit, f frame, reason string) error {
	delay := max(c.Config.Limits.Backoff, 250*time.Millisecond)
	return u.Step(ctx, func() error {
		return outcome(u.Within(ctx, func(tx *durable.Tx) error {
			ch, _, err := c.load(tx, f.Task.Task.TaskID, false, nil)
			if err != nil {
				return err
			}
			claim := u.Claim()
			return c.flush(tx, ch, &claim, durable.Waiting(ch.Now.Add(delay), reason))
		}))
	})
}

func (c *Coordinator) decisionCanAdvance(ctx context.Context, u Unit, f frame, s Snapshot) (bool, error, error) {
	live := false
	var gateErr error
	err := c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
		before := len(t.Task.WaitReasons)
		responsibility := u.Claim().Key().Responsibility
		clearWait(t, "dependency", responsibility)
		clearWait(t, "authorization", responsibility)
		if len(t.Task.WaitReasons) != before {
			t.Task.Revision++
			if err := c.persist(tx, ch, t); err != nil {
				return err
			}
		}
		gateErr = c.canAdvanceForOriginal(ch, t, s.GoalRevision, s.ControlRevision)
		live = gateErr == nil
		return nil
	})
	return live, gateErr, err
}

func (c *Coordinator) stopDecision(ctx context.Context, u Unit, f frame, s Snapshot, call api.OriginalCall, out *api.DecisionRecord) error {
	r, e := c.readRecord(ctx, u, "decision_cancel", s.Request.DecisionID)
	if e != nil {
		return e
	}
	if r != nil {
		call, e = Decode[api.OriginalCall](r)
		if e != nil {
			return e
		}
	} else {
		e = c.guardWork(ctx, u, f, false, func(tx *durable.Tx, ch *Change, t *TaskState) error {
			return c.Repository.Put(tx, t, rec("decision_cancel", s.Request.DecisionID, t.Task.TaskID, 1, "closed", true, call))
		})
		if e != nil {
			return e
		}
	}
	return c.external(ctx, u, f, "brain.cancel", s.Request.DecisionID, func() error {
		var e error
		*out, e = c.Ports.Brain.Cancel(ctx, f.Caller, call, s.Request.DecisionID)
		return e
	})
}

func (c *Coordinator) canAdvanceForOriginal(ch *Change, t *TaskState, goal, control int64) error {
	if t.Task.GoalRevision != goal || t.Task.ControlRevision != control {
		return failure("revision_conflict", "original snapshot control changed")
	}
	// Counting the already prepared continuation must not consume another slot.
	count := t.ContinuationCount
	t.ContinuationCount--
	e := c.canAdvance(ch, t)
	t.ContinuationCount = count
	return e
}

type preparedAction struct {
	Preparation api.ActionPreparation
	Snapshot    api.ControlSnapshot
}
type preparedProposal struct {
	Actions   []preparedAction
	Delegates []preparedChild
	Materials []api.BrainContextMaterialsItem
}

func (c *Coordinator) prepareProposal(ctx context.Context, u Unit, f frame, p api.Proposal) (preparedProposal, error) {
	v := preparedProposal{}
	if p.PlanDelta != nil {
		return v, failure("unsupported", "finite Plan is not enabled")
	}
	if p.Actions != nil {
		if int64(len(*p.Actions)) > f.Task.Policy.MaxActions {
			return v, failure("quota_exceeded", "action count exceeds policy")
		}
		seen := map[string]bool{}
		for _, raw := range *p.Actions {
			var h struct {
				Type      string
				ActionKey string `json:"action_key"`
			}
			if e := json.Unmarshal(raw, &h); e != nil {
				return v, e
			}
			if seen[h.ActionKey] {
				return v, failure("invalid_output", "duplicate proposal action key")
			}
			seen[h.ActionKey] = true
			if h.Type == "delegate" {
				child, e := c.prepareChild(ctx, u, f, raw)
				if e != nil {
					return v, e
				}
				v.Delegates = append(v.Delegates, child)
				continue
			}
			var a api.ActionInvoke
			if e := json.Unmarshal(raw, &a); e != nil {
				return v, e
			}
			if e := contracts.Validate("ActionInvoke", raw); e != nil {
				return v, failure("invalid_output", "action contract mismatch")
			}
			var prep api.ActionPreparation
			var proof api.ControlSnapshot
			e := c.external(ctx, u, f, "capability.prepare", a.CapabilityRef.ID, func() error { var e error; prep, e = c.Ports.Actions.Prepare(ctx, f.Caller, f.Task.Task, a); return e })
			if e != nil {
				return v, e
			}
			if Hash(prep.Action) != Hash(a) || validateValue("Id", prep.ExecutorID) != nil || amountsValid(prep.UpperBound) != nil {
				return v, failure("invalid_output", "prepared action changed original identity or lacks a trusted upper bound")
			}
			known := false
			for _, cap := range f.Task.Policy.Capabilities {
				if Hash(cap.CapabilityRef) == Hash(a.CapabilityRef) && Hash(cap.BindingRef) == Hash(a.BindingRef) {
					known = true
					if prep.EffectClass == "read_only" && (cap.EffectClass == nil || *cap.EffectClass != "read_only") {
						return v, failure("invalid_output", "read-only effect lacks fixed declaration")
					}
				}
			}
			if !known {
				return v, failure("forbidden", "action is outside fixed capability set")
			}
			e = c.external(ctx, u, f, "control.proof", prep.ExecutorID, func() error {
				var e error
				proof, e = c.Ports.Authority.ControlProof(ctx, f.Caller, api.TaskGate{TaskID: f.Task.Task.TaskID, OrchestratorID: c.Config.Scope.OwnerID, GoalRevision: f.Task.Task.GoalRevision, ControlRevision: f.Task.Task.ControlRevision, Status: f.Task.Task.Status, Control: f.Task.Task.Control}, prep.ExecutorID, c.Ports.Clock.Now())
				return e
			})
			if e != nil {
				return v, e
			}
			v.Actions = append(v.Actions, preparedAction{prep, proof})
		}
	}
	if p.Requests != nil {
		if c.Ports.Context == nil {
			return v, failure("dependency_unavailable", "context source is unavailable")
		}
		if int64(len(*p.Requests)) > f.Task.Policy.MaxContextRequests {
			return v, failure("quota_exceeded", "context request limit reached")
		}
		e := c.external(ctx, u, f, "context.prepare", f.Task.Task.TaskID, func() error {
			var e error
			v.Materials, e = c.Ports.Context.Prepare(ctx, f.Caller, f.Task.Task, *p.Requests)
			return e
		})
		if e != nil {
			return v, e
		}
		if len(v.Materials) > c.Config.Limits.Facts {
			return v, failure("quota_exceeded", "context result exceeds finite scope")
		}
	}
	refs := append([]api.ContentRef{}, p.EvidenceRefs...)
	if p.ArtifactRef != nil {
		refs = append(refs, *p.ArtifactRef)
	}
	for _, a := range v.Actions {
		refs = append(refs, a.Preparation.Action.EvidenceRefs...)
	}
	for _, m := range v.Materials {
		refs = append(refs, m.ContentRef)
	}
	bytes := int64(0)
	for _, ref := range refs {
		bytes += ref.ByteLength
		if bytes > int64(c.Config.Limits.Bytes) {
			return v, failure("quota_exceeded", "proposal input bytes exceed finite scope")
		}
		if e := u.Step(ctx, func() error { _, e := c.content(ctx, f.Caller, ref); return e }); e != nil {
			return v, e
		}
	}
	return v, nil
}

func (c *Coordinator) mergeDecision(ctx context.Context, u Unit, f frame, s Snapshot, d api.DecisionRecord) error {
	if d.DecisionID != s.Request.DecisionID || d.SnapshotRevision != s.Revision || validateValue("DecisionRecord", d) != nil {
		return c.finish(ctx, u, f, "invalid_original_fact", false)
	}
	var prep preparedProposal
	var prepareErr error
	if d.Status == "completed" && s.GoalRevision == f.Task.Task.GoalRevision && s.ControlRevision == f.Task.Task.ControlRevision && f.Task.Task.Status == "active" && f.Task.Task.Control == "running" {
		p := d.Proposal
		if p.RequirementsProposal == nil || p.RequirementsProposal.BaseGoalRevision == f.Task.Task.GoalRevision && Hash(p.RequirementsProposal.Requirements) == Hash(f.Task.Task.Requirements) {
			prep, prepareErr = c.prepareProposal(ctx, u, f, *p)
		}
	}
	extras, e := c.fixChildren(f.Task, d.DecisionID, prep.Delegates)
	if e != nil {
		return e
	}
	return c.changeWorkWith(ctx, u, f, true, extras, func(tx *durable.Tx) error { return c.reserveChildren(tx, prep.Delegates) }, func(tx *durable.Tx, ch *Change, t *TaskState) (durable.Disposition, error) {
		fresh, e := c.fact(tx, t, Fact{OwnerID: s.Call.OwnerID, ObjectKind: "decision", ObjectID: d.DecisionID, Revision: d.Revision, Digest: Hash(d), Value: Raw(d)})
		if e != nil {
			return durable.Disposition{}, e
		}
		queue(ch, t, "settle", "decision", d.DecisionID)
		if e = c.Repository.Put(tx, t, rec("decision", d.DecisionID, t.Task.TaskID, d.Revision, d.Status, false, d)); e != nil {
			return durable.Disposition{}, e
		}
		if d.Status == "accepted" || d.Status == "running" {
			return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "original_decision_pending"), nil
		}
		consumed, e := c.Repository.Get(tx, "consumption", d.DecisionID)
		if e != nil {
			return durable.Disposition{}, e
		}
		if consumed != nil {
			return durable.Done(), c.rejectChildren(tx, prep.Delegates)
		}
		if prepareErr != nil && admissionWait(prepareErr) != "" {
			kind := admissionWait(prepareErr)
			wait(t, kind, "requalify_original_proposal", &api.ObjectRef{OwnerID: s.Call.OwnerID, ID: d.DecisionID, Revision: d.Revision})
			t.Task.Revision++
			if e = c.persist(tx, ch, t); e != nil {
				return durable.Disposition{}, e
			}
			if e = c.rejectChildren(tx, prep.Delegates); e != nil {
				return durable.Disposition{}, e
			}
			return durable.Waiting(ch.Now.Add(c.Config.Limits.Backoff), "original_proposal_dependencies"), nil
		}
		if prepareErr == nil {
			clearWait(t, "dependency", d.DecisionID)
			clearWait(t, "authorization", d.DecisionID)
		}
		conclusion := "stale"
		current := c.canAdvanceForOriginal(ch, t, s.GoalRevision, s.ControlRevision) == nil && t.PendingDecision == d.DecisionID
		if t.PendingDecision == d.DecisionID {
			t.PendingDecision = ""
		}
		if current {
			if d.Status == "completed" {
				p := *d.Proposal
				conclusion = p.Kind
				if p.RequirementsProposal != nil && p.RequirementsProposal.BaseGoalRevision == t.Task.GoalRevision && Hash(p.RequirementsProposal.Requirements) != Hash(t.Task.Requirements) {
					if !validRequirements(p.RequirementsProposal.Requirements) {
						return durable.Disposition{}, failure("invalid_output", "ambiguous requirement identities")
					}
					t.Task.Requirements = p.RequirementsProposal.Requirements
					if e = c.invalidateGoal(tx, ch, t, "requirements_changed"); e != nil {
						return durable.Disposition{}, e
					}
					conclusion = "requirements_changed"
				} else if prepareErr != nil {
					conclusion = "admission_failed"
					t.NoProgressCount++
					t.RepairCount++
					queue(ch, t, "verify", "task", t.Task.TaskID)
				} else {
					switch p.Kind {
					case "act":
						e = c.batchBudget(t, prep)
						if e == nil {
							e = c.preflightChildren(tx, ch, t, prep.Delegates)
						}
						if e == nil {
							e = c.preflightActions(tx, ch, t, prep.Actions)
						}
						if e != nil {
							conclusion = "admission_failed"
							t.NoProgressCount++
							t.RepairCount++
							kind := admissionWait(e)
							if kind == "" {
								kind = "budget"
							}
							wait(t, kind, "requalify_original_task_or_close", nil)
							queue(ch, t, "verify", "task", t.Task.TaskID)
							break
						}
						if e = c.admitActions(tx, ch, t, "decision", d.DecisionID, prep.Actions); e != nil {
							return durable.Disposition{}, e
						}
						if e = c.admitChildren(tx, ch, t, d.DecisionID, prep.Delegates); e != nil {
							return durable.Disposition{}, e
						}
						if len(prep.Actions)+len(prep.Delegates) == 0 {
							t.NoProgressCount++
							t.RepairCount++
						}
					case "complete":
						if p.ArtifactRef == nil {
							return durable.Disposition{}, durable.ErrInvariant
						}
						requirements := []string{}
						if p.RequirementRefs != nil {
							_ = json.Unmarshal(*p.RequirementRefs, &requirements)
						}
						v := Candidate{Artifacts: []api.ContentRef{*p.ArtifactRef}, GoalRevision: t.Task.GoalRevision, Requirements: requirements}
						r := rec("candidate", ID("candidate", d.DecisionID), t.Task.TaskID, 1, "closed", true, v)
						r.CurrentKey = "candidate"
						if e = c.Repository.Deselect(tx, t, "candidate", "candidate"); e != nil {
							return durable.Disposition{}, e
						}
						if e = c.Repository.Put(tx, t, r); e != nil {
							return durable.Disposition{}, e
						}
						queue(ch, t, "verify", "task", t.Task.TaskID)
					case "request_input":
						if e = c.prepareInput(tx, ch, t, d.DecisionID, p); e != nil {
							return durable.Disposition{}, e
						}
					case "need_context":
						t.NoProgressCount++
						for _, m := range prep.Materials {
							r := rec("material", ID("material", d.DecisionID, m.ContentRef.Hash), t.Task.TaskID, 1, "closed", true, m)
							r.CurrentKey = m.ContentRef.ContentID
							if e = c.Repository.Deselect(tx, t, "material", r.CurrentKey); e != nil {
								return durable.Disposition{}, e
							}
							if e = c.Repository.Put(tx, t, r); e != nil {
								return durable.Disposition{}, e
							}
						}
						queue(ch, t, "decide", "task", t.Task.TaskID)
					case "fail":
						if e = c.failTask(tx, ch, t, *p.Reason); e != nil {
							return durable.Disposition{}, e
						}
					default:
						return durable.Disposition{}, durable.ErrUnsupported
					}
				}
			} else {
				conclusion = d.Status
				t.NoProgressCount++
				t.RepairCount++
				queue(ch, t, "verify", "task", t.Task.TaskID)
			}
		}
		if conclusion != "act" {
			if e = c.rejectChildren(tx, prep.Delegates); e != nil {
				return durable.Disposition{}, e
			}
		}
		if e = c.Repository.Put(tx, t, rec("consumption", d.DecisionID, t.Task.TaskID, 1, "closed", true, Consumption{DecisionID: d.DecisionID, Digest: Hash(d), Conclusion: conclusion, GoalRevision: s.GoalRevision, SnapshotRevision: s.Revision})); e != nil {
			return durable.Disposition{}, e
		}
		if fresh {
			t.Task.Revision++
			if e = c.persist(tx, ch, t); e != nil {
				return durable.Disposition{}, e
			}
		}
		return durable.Done(), nil
	})
}
func admissionWait(e error) string {
	var f *api.Failure
	if !errors.As(e, &f) {
		return "dependency"
	}
	switch f.Detail.Code {
	case "dependency_unavailable", "overloaded":
		return "dependency"
	case "forbidden", "authorization", "permission_denied":
		return "authorization"
	}
	return ""
}

func (c *Coordinator) batchBudget(t *TaskState, p preparedProposal) error {
	values := balanceMap(t.Task.Budget)
	all := [][]api.Amount{}
	for _, a := range p.Actions {
		all = append(all, a.Preparation.UpperBound)
	}
	for _, d := range p.Delegates {
		amounts := []api.Amount{}
		for _, l := range d.Preparation.Submit.Budget {
			amounts = append(amounts, api.Amount{Unit: l.Unit, Amount: l.Limit})
		}
		all = append(all, amounts)
	}
	for _, amounts := range all {
		for _, a := range amounts {
			b, ok := values[a.Unit]
			if !ok {
				return failure("precondition_failed", "batch budget unit is not declared")
			}
			reserved, e := add(b.Reserved.Amount, a.Amount)
			if e != nil {
				return e
			}
			total, e := add(b.Spent.Amount, reserved)
			if e != nil {
				return e
			}
			cmp, e := compare(total, b.Limit.Amount)
			if e != nil {
				return e
			}
			if cmp > 0 {
				return failure("quota_exceeded", "complete candidate batch exceeds strict budget")
			}
			b.Reserved.Amount = reserved
			values[a.Unit] = b
		}
	}
	return nil
}

func validRequirements(rs []api.Requirement) bool {
	seen := map[string]bool{}
	for _, r := range rs {
		if seen[r.RequirementID] || validateValue("Requirement", r) != nil {
			return false
		}
		seen[r.RequirementID] = true
	}
	return len(rs) <= 100
}
func (c *Coordinator) fact(tx *durable.Tx, t *TaskState, f Fact) (bool, error) {
	id := ID("fact", f.OwnerID, f.ObjectKind, f.ObjectID, strconv.FormatInt(f.Revision, 10))
	r, e := c.Repository.Get(tx, "fact", id)
	if e != nil {
		return false, e
	}
	if r != nil {
		old, e := Decode[Fact](r)
		if e != nil {
			return false, e
		}
		if old.Digest != f.Digest {
			return false, failure("revision_conflict", "original source revision changed content")
		}
		return false, nil
	}
	return true, c.Repository.Put(tx, t, rec("fact", id, t.Task.TaskID, f.Revision, "closed", true, f))
}
func (c *Coordinator) failTask(tx *durable.Tx, ch *Change, t *TaskState, reason string) error {
	if terminal(t) {
		return nil
	}
	if _, e := c.control(tx, ch, t, "task.cancel", reason); e != nil {
		return e
	}
	t.Task.Status = "failed"
	t.Task.Revision++
	return c.persist(tx, ch, t)
}
