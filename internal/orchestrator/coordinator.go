package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/contracts"
	"github.com/ruipengliu/lerna/internal/durable"
)

type Coordinator struct {
	Repository Repository
	Ports      api.OrchestratorPorts
	Config     Config
}
type Prepared struct {
	Caller       api.Caller
	Intent       durable.FixedIntent
	Policy       *api.TaskPolicy
	Allocation   *api.RuntimeBudgetAllocation
	Confirmation *api.ConfirmationRecord
	Answer       json.RawMessage
}
type Change struct {
	Changed bool
	Tasks   map[string]*TaskState
	Works   map[durable.JobKey]WorkRef
	Now     time.Time
	Jobs    map[durable.JobKey]durable.Job
	Repair  bool
}

func (c *Coordinator) Ready() bool {
	p := c.Ports
	return p.Authority != nil && p.Authority.AdmissionReady() && p.Clock != nil && p.Clock.Ready() && p.Content != nil && p.Policies != nil && p.Brain != nil && p.Actions != nil && p.Executor != nil && p.Billing != nil && p.Verification != nil
}
func (c *Coordinator) Metadata(caller api.Caller, method, target string, raw json.RawMessage, command bool) error {
	if caller.TenantID != c.Config.Scope.TenantID || len(caller.ActorID) > 200 || validateValue("Id", caller.ActorID) != nil {
		return failure("unauthenticated", "authenticated tenant/actor required")
	}
	m, ok := contracts.Method(method)
	if !ok || (command && m.Kind != "command") || (!command && m.Kind != "query") || !(len(method) > 5 && method[:5] == "task." || len(method) > 7 && method[:7] == "budget." || method == "interaction.request_read") {
		return failure("unsupported", "method is not implemented by this owner")
	}
	if len(target) > 200 || validateValue("Id", target) != nil {
		return failure("invalid_argument", "invalid target identity")
	}
	if _, err := durable.CanonicalJSON(raw); err != nil {
		return failure("invalid_argument", "invalid original JSON")
	}
	if err := contracts.Validate(m.Input, raw); err != nil {
		return failure("invalid_argument", "method input does not match the fixed contract")
	}
	return nil
}
func (c *Coordinator) Check(ctx context.Context, caller api.Caller, intent durable.FixedIntent) error {
	v := intent.Value()
	if err := c.Metadata(caller, v.Method, v.TargetID, v.Payload, true); err != nil {
		return err
	}
	if c.Ports.Authority == nil {
		return failure("dependency_unavailable", "current authority is unavailable")
	}
	return c.Ports.Authority.Current(ctx, caller, api.Access{Method: v.Method, TargetID: v.TargetID}, c.Ports.Clock.Now())
}
func (c *Coordinator) Disclose(ctx context.Context, caller api.Caller, r durable.CommandRecord) (durable.CommandRecord, error) {
	target := r.TargetID
	if r.Receipt != nil && r.Receipt.ResourceID != "" {
		target = r.Receipt.ResourceID
	}
	err := c.Ports.Authority.Current(ctx, caller, api.Access{Method: "command.get", TargetID: target}, c.Ports.Clock.Now())
	return r, err
}
func (c *Coordinator) content(ctx context.Context, caller api.Caller, ref api.ContentRef) ([]byte, error) {
	if ref.TenantID != caller.TenantID || ref.ByteLength < 0 || ref.ByteLength > int64(c.Config.Limits.Bytes) || validateValue("ContentRef", ref) != nil {
		return nil, failure("invalid_argument", "content reference is outside the finite input scope")
	}
	if c.Ports.Content == nil {
		return nil, failure("dependency_unavailable", "content owner is unavailable")
	}
	if err := c.Ports.Authority.Current(ctx, caller, api.Access{Method: "content.use", TargetID: ref.ContentID, ContentRefs: []api.ContentRef{ref}}, c.Ports.Clock.Now()); err != nil {
		return nil, err
	}
	data, err := c.Ports.Content.Read(ctx, caller, ref)
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(data)
	if int64(len(data)) != ref.ByteLength || "sha256:"+hex.EncodeToString(h[:]) != ref.Hash {
		return nil, failure("precondition_failed", "content bytes do not match their exact reference")
	}
	return data, nil
}
func policyValid(p api.TaskPolicy, ref api.ComponentRef) error {
	if e := validateValue("", p); e != nil {
		return e
	}
	if validateValue("ComponentRef", p.CoverageBinding.RuleRef) != nil || validateValue("ComponentRef", p.CoverageBinding.EvaluatorRef) != nil || len(p.VerificationBindings) > 100 {
		return failure("precondition_failed", "fixed verification configuration is incomplete")
	}
	bindings := map[string]bool{}
	for _, b := range p.VerificationBindings {
		key := Hash(b.RuleRef)
		if bindings[key] || validateValue("ComponentRef", b.RuleRef) != nil || validateValue("ComponentRef", b.EvaluatorRef) != nil || b.Basis != "verified" && b.Basis != "assessed" {
			return failure("precondition_failed", "ambiguous or invalid fixed verifier binding")
		}
		bindings[key] = true
	}
	for _, r := range p.Requirements {
		if r.Required && !bindings[Hash(r.RuleRef)] {
			return failure("precondition_failed", "required rule has no fixed verifier")
		}
	}
	if Hash(p.Ref) != Hash(ref) || p.BudgetMode != "strict" {
		return failure("unsupported", "this assembly requires the fixed strict/ReAct policy")
	}
	if p.MaxContinuations < 1 || p.MaxContinuations > 10000 || p.MaxRepairs < 0 || p.MaxRepairs > 1000 || p.MaxNoProgress < 1 || p.MaxNoProgress > 1000 || p.MaxActions < 1 || p.MaxActions > 32 || p.MaxContextRequests < 0 || p.MaxContextRequests > 32 || p.MaxPolls < 1 || p.MaxPolls > 1000 || p.InputTimeout < time.Second || p.InputTimeout > 24*time.Hour || len(p.Requirements) > 100 || len(p.Capabilities) > 100 || len(p.DecisionUpperBound) == 0 {
		return failure("invalid_argument", "policy limits or trusted decision upper bound are missing")
	}
	seen := map[string]bool{}
	for _, r := range p.Requirements {
		if validateValue("Requirement", r) != nil || seen[r.RequirementID] {
			return failure("invalid_argument", "requirements must have unique fixed identities")
		}
		seen[r.RequirementID] = true
	}
	return amountsValid(p.DecisionUpperBound)
}
func amountsValid(amounts []api.Amount) error {
	if len(amounts) < 1 || len(amounts) > 100 {
		return failure("invalid_argument", "finite amounts required")
	}
	seen := map[string]bool{}
	for _, v := range amounts {
		if v.Unit == "" || len(v.Unit) > 4096 || seen[v.Unit] {
			return failure("invalid_argument", "amount units must be unique")
		}
		if _, err := decimal(v.Amount); err != nil {
			return err
		}
		seen[v.Unit] = true
	}
	return nil
}
func limitsValid(limits []api.BudgetLimit) error {
	a := make([]api.Amount, len(limits))
	for i, v := range limits {
		a[i] = api.Amount{Unit: v.Unit, Amount: v.Limit}
	}
	return amountsValid(a)
}
func (c *Coordinator) Prepare(ctx context.Context, caller api.Caller, i durable.FixedIntent) (Prepared, error) {
	p := Prepared{Caller: caller, Intent: i}
	body := i.Value().Payload
	switch i.Method() {
	case "task.submit":
		var in api.TaskSubmitInput
		_ = json.Unmarshal(body, &in)
		if in.OrchestratorID != c.Config.Scope.OwnerID || i.TargetID() != in.OrchestratorID {
			return p, failure("invalid_argument", "task owner must match the original route")
		}
		if _, err := c.content(ctx, caller, in.GoalRef); err != nil {
			return p, err
		}
		if err := limitsValid(in.Budget); err != nil {
			return p, err
		}
		policy, err := c.Ports.Policies.Resolve(ctx, caller, in.PolicyRef, in.GoalRef, in.Constraints)
		if err != nil {
			return p, err
		}
		if err = policyValid(policy, in.PolicyRef); err != nil {
			return p, err
		}
		p.Policy = &policy
		if in.DelegationContext != nil {
			d := in.DelegationContext
			if caller.SourceOwnerID != d.SenderOrchestratorID || d.AllocationRef.OwnerID != d.SenderOrchestratorID {
				return p, failure("forbidden", "delegation requires the authenticated original sender")
			}
			allocation, err := c.Ports.Billing.Allocation(ctx, caller, d.SenderOrchestratorID, d.AllocationRef.ID)
			if err != nil {
				return p, err
			}
			p.Allocation = &allocation
		}
	case "task.revise":
		var in api.TaskReviseInput
		_ = json.Unmarshal(body, &in)
		if _, err := c.content(ctx, caller, in.GoalRef); err != nil {
			return p, err
		}
	case "task.input":
		var in api.TaskInputInput
		_ = json.Unmarshal(body, &in)
		var ref api.ContentRef
		if err := json.Unmarshal(in.AnswerRef, &ref); err != nil {
			return p, failure("invalid_argument", "answer content reference required")
		}
		data, err := c.content(ctx, caller, ref)
		if err != nil {
			return p, err
		}
		if _, err = durable.CanonicalJSON(data); err != nil {
			return p, failure("invalid_argument", "answer bytes are invalid")
		}
		p.Answer = data
		for _, r := range in.PreviewRefs {
			if _, err = c.content(ctx, caller, r); err != nil {
				return p, err
			}
		}
	case "task.accept_result":
		var in api.TaskAcceptResultInput
		_ = json.Unmarshal(body, &in)
		if c.Ports.Confirmation == nil {
			return p, failure("dependency_unavailable", "trusted user confirmation is unavailable")
		}
		v := i.Value()
		record, err := c.Ports.Confirmation.Read(ctx, caller, api.Command{CommandID: v.CommandID, Method: v.Method, TargetID: v.TargetID, ExpiresAt: v.ExpiresAt, ExpectedRevision: v.ExpectedRevision, Payload: v.Payload}, in.ConfirmationRef)
		if err != nil {
			return p, err
		}
		p.Confirmation = &record
	case "task.attach_evidence":
		var in api.TaskAttachEvidenceInput
		_ = json.Unmarshal(body, &in)
		for _, ref := range in.EvidenceRefs {
			if _, err := c.content(ctx, caller, ref); err != nil {
				return p, err
			}
		}
	case "task.adjust_budget":
		var in api.TaskAdjustBudgetInput
		_ = json.Unmarshal(body, &in)
		if err := limitsValid(in.Limits); err != nil {
			return p, err
		}
	case "budget.allocate":
		var in api.BudgetAllocateInput
		_ = json.Unmarshal(body, &in)
		if err := limitsValid(in.Limits); err != nil {
			return p, err
		}
	case "budget.settle":
		var in api.BudgetSettleInput
		_ = json.Unmarshal(body, &in)
		actual, err := c.Ports.Billing.Closure(ctx, caller, in.Closure.ReceiverID, in.AllocationID)
		if err != nil {
			return p, err
		}
		if Hash(actual) != Hash(in.Closure) {
			return p, failure("precondition_failed", "closure must match the original receiver's current proof")
		}
		if _, err = c.content(ctx, caller, actual.ProofRef); err != nil {
			return p, err
		}
	}
	return p, nil
}
func (c *Coordinator) load(tx *durable.Tx, id string, subtree bool, extras []*TaskState) (*Change, *TaskState, error) {
	if err := c.Repository.Read(tx); err != nil {
		return nil, nil, err
	}
	now, err := tx.Now()
	if err != nil {
		return nil, nil, err
	}
	chain, err := c.Repository.Chain(tx, id)
	if err != nil {
		return nil, nil, err
	}
	if len(chain) == 0 && len(extras) == 0 {
		return nil, nil, failure("not_found", "original task is absent")
	}
	if len(chain) > c.Config.Limits.Depth+1 {
		return nil, nil, failure("precondition_failed", "ancestor chain exceeds its finite bound")
	}
	all := append([]*TaskState(nil), chain...)
	if subtree && len(chain) > 0 {
		tree, err := c.Repository.Tree(tx, id, c.Config.Limits.Tree+1)
		if err != nil {
			return nil, nil, err
		}
		if len(tree) > c.Config.Limits.Tree {
			return nil, nil, failure("precondition_failed", "control subtree cannot be read completely")
		}
		all = append(all, tree...)
	}
	all = append(all, extras...)
	locked, err := c.Repository.LockTasks(tx, all)
	if err != nil {
		return nil, nil, err
	}
	change := &Change{Tasks: map[string]*TaskState{}, Works: map[durable.JobKey]WorkRef{}, Now: now}
	subjects := []string{}
	ids := []string{}
	for _, t := range locked {
		change.Tasks[t.Task.TaskID] = t
		subjects = append(subjects, t.SubjectID)
		ids = append(ids, t.Task.TaskID)
	}
	if err = c.Repository.LockCapacity(tx, subjects); err != nil {
		return nil, nil, err
	}
	balances, err := c.Repository.LockBalances(tx, ids)
	if err != nil {
		return nil, nil, err
	}
	for _, t := range locked {
		if len(balances[t.Task.TaskID]) > 0 {
			t.Task.Budget = balances[t.Task.TaskID]
		}
	}
	return change, change.Tasks[id], nil
}
func (c *Coordinator) access(tx *durable.Tx, p Prepared, t *TaskState) error {
	n, e := tx.Now()
	if e != nil {
		return e
	}
	return c.Ports.Authority.Current(tx.Context(), p.Caller, api.Access{Method: p.Intent.Method(), TargetID: p.Intent.TargetID(), TaskID: t.Task.TaskID, PolicyRef: &t.Task.PolicyRef}, n)
}
func (c *Coordinator) canAdvance(ch *Change, t *TaskState) error {
	if !c.Ready() {
		return failure("dependency_unavailable", "original assembly recovery or dependencies are not ready")
	}
	if terminal(t) || t.Task.Control != "running" {
		return failure("precondition_failed", "task does not allow new goal work")
	}
	deadline, err := time.Parse(time.RFC3339Nano, t.Task.Deadline)
	if err != nil || !ch.Now.Before(deadline) {
		return failure("deadline_exceeded", "task deadline exceeded")
	}
	for p := t.ParentID; p != ""; {
		ancestor := ch.Tasks[p]
		if ancestor == nil {
			return failure("precondition_failed", "complete ancestor gates are required")
		}
		if terminal(ancestor) || ancestor.Task.Control != "running" {
			return failure("precondition_failed", "ancestor has closed new goal work")
		}
		p = ancestor.ParentID
	}
	for _, wait := range t.Task.WaitReasons {
		if wait.Kind != "effect" {
			return failure("precondition_failed", "task has unresolved prerequisites")
		}
	}
	if len(t.Task.OpenEffects) > 0 {
		return failure("effect_unknown", "original effects must be reconciled before a conflicting action")
	}
	if t.ContinuationCount >= t.Policy.MaxContinuations {
		return failure("quota_exceeded", "cumulative continuation limit reached")
	}
	return nil
}
func queue(ch *Change, t *TaskState, kind, objectKind, id string) {
	w := WorkRef{TaskID: t.Task.TaskID, SubjectID: t.SubjectID, Kind: kind, ObjectKind: objectKind, ObjectID: id}
	if kind == "poll" && objectKind == "decision" {
		w.ProviderID, w.ResourceID = t.Policy.BrainProviderID, t.Policy.BrainID
		if w.ProviderID == "" {
			w.ProviderID = t.Policy.BrainID
		}
	}
	ch.Works[workKey(w)] = w
}
func queueAction(ch *Change, t *TaskState, kind, id string, p api.ActionPreparation) {
	w := WorkRef{TaskID: t.Task.TaskID, SubjectID: t.SubjectID, Kind: kind, ObjectKind: "operation", ObjectID: id, ProviderID: p.ProviderID, ResourceID: p.ResourceID}
	ch.Works[workKey(w)] = w
}
func (c *Coordinator) persist(tx *durable.Tx, ch *Change, t *TaskState) error {
	if err := c.refresh(tx, t); err != nil {
		return err
	}
	return c.Repository.SaveTask(tx, t)
}
func (c *Coordinator) refresh(tx *durable.Tx, t *TaskState) error {
	effects, err := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "effect", State: "open", Limit: c.Config.Limits.Effects + 1})
	if err != nil {
		return err
	}
	if len(effects) > c.Config.Limits.Effects {
		t.ProjectionComplete = false
		return failure("precondition_failed", "effects exceed the complete bounded projection")
	}
	t.Task.OpenEffects = []string{}
	for _, r := range effects {
		t.Task.OpenEffects = append(t.Task.OpenEffects, r.ID)
	}
	reservations, err := c.Repository.Records(tx, Filter{TaskID: t.Task.TaskID, Kind: "reservation", State: "open", Limit: 1})
	if err != nil {
		return err
	}
	t.Task.AccountingOpen = len(reservations) > 0
	if t.Task.WaitReasons == nil {
		t.Task.WaitReasons = []api.WaitReason{}
	}
	if t.Task.Requirements == nil {
		t.Task.Requirements = []api.Requirement{}
	}
	return nil
}
func (c *Coordinator) flush(tx *durable.Tx, ch *Change, claim *durable.Claim, d durable.Disposition) error {
	return c.jobCommit(tx, ch, claim, &d)
}
func (c *Coordinator) jobCommit(tx *durable.Tx, ch *Change, claim *durable.Claim, d *durable.Disposition) error {
	keys := []durable.JobKey{}
	seen := map[durable.JobKey]bool{}
	groups := map[string][]WorkRef{}
	for key, w := range ch.Works {
		keys = append(keys, key)
		seen[key] = true
		if ch.Tasks[w.TaskID] != nil {
			groups[w.TaskID] = append(groups[w.TaskID], w)
		}
	}
	for _, t := range sortedTasks(taskValues(ch.Tasks)) {
		works := groups[t.Task.TaskID]
		if len(works) == 0 {
			continue
		}
		sort.Slice(works, func(i, j int) bool { return workKey(works[i]).Responsibility < workKey(works[j]).Responsibility })
		rows := []Record{}
		for _, w := range works {
			rows = append(rows, rec("work", workKey(w).Responsibility, w.TaskID, 1, "open", true, w))
		}
		if e := c.Repository.PutMany(tx, t, rows); e != nil {
			return fmt.Errorf("persist work records for %s: %w", t.Task.TaskID, e)
		}
		if e := c.Repository.Routes(tx, t, works); e != nil {
			return fmt.Errorf("route work for %s: %w", t.Task.TaskID, e)
		}
	}
	if claim != nil && !seen[claim.Key()] {
		keys = append(keys, claim.Key())
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].Kind != keys[j].Kind {
			return keys[i].Kind < keys[j].Kind
		}
		return keys[i].Responsibility < keys[j].Responsibility
	})
	if ch.Repair && claim == nil {
		items := []durable.JobRepair{}
		for _, key := range keys {
			w := ch.Works[key]
			source := w.TaskID
			if source == "" {
				source = w.ObjectID
			}
			items = append(items, durable.JobRepair{Kind: key.Kind, Responsibility: key.Responsibility, Source: source})
		}
		jobs, e := tx.RepairMany(items, ch.Now)
		if e != nil {
			return e
		}
		ch.Jobs = map[durable.JobKey]durable.Job{}
		for _, j := range jobs {
			ch.Jobs[j.Key] = j
		}
		return nil
	}
	for _, key := range keys {
		if w, ok := ch.Works[key]; ok {
			source := w.TaskID
			if source == "" {
				source = w.ObjectID
			}
			var job durable.Job
			var err error
			if ch.Repair {
				job, err = tx.Repair(key, source, ch.Now)
			} else {
				job, err = tx.Raise(key, source, ch.Now)
			}
			if err != nil {
				return fmt.Errorf("raise %s/%s: %w", key.Kind, key.Responsibility, err)
			}
			if ch.Jobs == nil {
				ch.Jobs = map[durable.JobKey]durable.Job{}
			}
			ch.Jobs[key] = job
		}
		if claim != nil && key == claim.Key() {
			if d == nil {
				if err := tx.Guard(*claim); err != nil {
					return fmt.Errorf("guard %s/%s: %w", key.Kind, key.Responsibility, err)
				}
				continue
			}
			if _, err := tx.Finish(*claim, *d); err != nil {
				return fmt.Errorf("finish %s/%s: %w", key.Kind, key.Responsibility, err)
			}
		}
	}
	return nil
}
func wait(t *TaskState, kind, condition string, object *api.ObjectRef) {
	for i := range t.Task.WaitReasons {
		if t.Task.WaitReasons[i].Kind == kind && ((object == nil && t.Task.WaitReasons[i].ObjectRef == nil) || (object != nil && t.Task.WaitReasons[i].ObjectRef != nil && t.Task.WaitReasons[i].ObjectRef.ID == object.ID)) {
			t.Task.WaitReasons[i] = api.WaitReason{Kind: kind, ResumeCondition: condition, ObjectRef: object}
			return
		}
	}
	t.Task.WaitReasons = append(t.Task.WaitReasons, api.WaitReason{Kind: kind, ResumeCondition: condition, ObjectRef: object})
}
func clearWait(t *TaskState, kind, id string) {
	out := []api.WaitReason{}
	for _, w := range t.Task.WaitReasons {
		if w.Kind == kind && (id == "" || w.ObjectRef != nil && w.ObjectRef.ID == id) {
			continue
		}
		out = append(out, w)
	}
	t.Task.WaitReasons = out
}
func rejected(i durable.FixedIntent, err error) durable.Decision {
	var f *api.Failure
	if !errors.As(err, &f) {
		f = failure("internal_error", "local decision could not be completed")
	}
	return durable.Decision{Receipt: &durable.Receipt{CommandID: i.CommandID(), Stage: "rejected", Error: Raw(f.Detail)}}
}
func applied(i durable.FixedIntent, t *TaskState, value any) durable.Decision {
	receipt := &durable.Receipt{CommandID: i.CommandID(), Stage: "applied", Output: Raw(value)}
	if t != nil {
		receipt.ResourceID = t.Task.TaskID
		v := t.Task.Revision
		receipt.Revision = &v
	}
	return durable.Decision{Receipt: receipt}
}
func original(i durable.FixedIntent, owner string) api.OriginalCall {
	v := i.Value()
	return api.OriginalCall{OwnerID: owner, Command: api.Command{CommandID: v.CommandID, Method: v.Method, TargetID: v.TargetID, ExpiresAt: v.ExpiresAt, ExpectedRevision: v.ExpectedRevision, Payload: v.Payload}}
}
func (c *Coordinator) sameScope(tx *durable.Tx) error {
	if tx.Scope() != c.Config.Scope {
		return durable.ErrScope
	}
	return nil
}
func reserveID(kind, id string) string { return ID("reservation", kind, id) }
func (c *Coordinator) ReceiptValid(i durable.FixedIntent, d durable.Decision) error {
	if d.Receipt == nil {
		return durable.ErrInvariant
	}
	if d.Receipt.Stage == "applied" {
		method, _ := contracts.Method(i.Method())
		if err := contracts.Validate(method.Output, d.Receipt.Output); err != nil {
			return fmt.Errorf("%w: method output", durable.ErrInvariant)
		}
	}
	return nil
}

// ContentMatches validates bounded exact bytes returned by a trusted adapter.
func ContentMatches(ref api.ContentRef, body []byte) error {
	if validateValue("ContentRef", ref) != nil || len(body) > 1<<20 || int64(len(body)) != ref.ByteLength {
		return failure("precondition_failed", "content is outside its exact bounded reference")
	}
	sum := sha256.Sum256(body)
	if ref.Hash != "sha256:"+hex.EncodeToString(sum[:]) {
		return failure("precondition_failed", "content digest does not match")
	}
	return nil
}
