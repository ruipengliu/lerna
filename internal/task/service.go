package task

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const (
	tasks              = "task.tasks"
	goals              = "task.goals"
	requirements       = "task.requirements"
	candidates         = "task.candidates"
	adoptions          = "task.adoptions"
	coverages          = "task.coverages"
	checks             = "task.checks"
	checkRequests      = "task.check_requests"
	inputs             = "task.inputs"
	decisions          = "task.decisions"
	consumptions       = "task.consumptions"
	intents            = "task.intents"
	relations          = "task.relations"
	reservations       = "task.reservations"
	allocations        = "task.allocations"
	incoming           = "task.incoming"
	closures           = "task.allocation_closures"
	adjustments        = "task.adjustments"
	delegations        = "task.delegations"
	delegationClosures = "task.delegation_closures"
	children           = "task.children"
	transfers          = "task.transfers"
	publications       = "task.result_publications"
	results            = "task.results"
	steers             = "task.steers"
	collections        = "task.collections"
	windows            = "task.windows"
)

type Service struct {
	config        Config
	ports         Ports
	policies      map[string]TaskPolicy
	rules         map[string]api.RuleDefinition
	answerSchemas map[string]api.Schema
}

func New(config Config, ports Ports) (*Service, error) {
	if len(config.Policies) == 0 {
		return nil, fmt.Errorf("task policies must be explicitly configured")
	}
	if config.MaxTasksPerSubject == 0 {
		config.MaxTasksPerSubject = 64
	}
	if config.MaxActiveChildren == 0 {
		config.MaxActiveChildren = 8
	}
	if config.MaxActiveSubtree == 0 {
		config.MaxActiveSubtree = 64
	}
	if config.MaxRelations == 0 {
		config.MaxRelations = 512
	}
	if config.MaxTasksPerSubject > 999 || config.MaxActiveChildren > 64 || config.MaxActiveSubtree > 256 || config.MaxRelations > 999 {
		return nil, fmt.Errorf("unbounded task configuration")
	}
	if config.ControlWindow == 0 {
		config.ControlWindow = 2 * time.Second
	}
	if config.ControlWindow < 0 || config.ControlWindow > 5*time.Second {
		return nil, fmt.Errorf("invalid control window")
	}
	if len(config.Participants) == 0 {
		config.Participants = []string{Namespace}
	}
	found := false
	for _, p := range config.Participants {
		if p == Namespace {
			found = true
		}
	}
	if !found {
		config.Participants = append(config.Participants, Namespace)
	}
	s := &Service{config: config, ports: ports, policies: map[string]TaskPolicy{}, rules: map[string]api.RuleDefinition{}, answerSchemas: map[string]api.Schema{}}
	for _, p := range config.Policies {
		if err := api.ValidateRecord("ComponentRef", p.PolicyRef); err != nil {
			return nil, err
		}
		if p.ContinuationLimit == 0 || p.MaxRequirements == 0 || p.MaxRequirements > 100 || p.MaxDelegations == 0 || p.MaxDelegations > 128 || p.MaxDepth == 0 || p.MaxDepth > 4 || p.MaxDurationSeconds == 0 || p.MaxEvidenceStalenessSeconds == 0 || len(p.BudgetLimits) == 0 || p.CostMode != "strict" && p.CostMode != "estimate" {
			return nil, fmt.Errorf("invalid task policy")
		}
		if err := api.ValidateAmounts(p.BudgetLimits); err != nil {
			return nil, err
		}
		k := componentKey(p.PolicyRef)
		if _, ok := s.policies[k]; ok {
			return nil, fmt.Errorf("duplicate task policy")
		}
		s.policies[k] = p
	}
	for _, rule := range config.Rules {
		if err := api.ValidateRecord("RuleDefinition", rule); err != nil {
			return nil, err
		}
		s.rules[componentKey(rule.RuleRef)] = rule
	}
	for _, definition := range config.AnswerSchemas {
		if err := api.ValidateRecord("ComponentRef", definition.Ref); err != nil {
			return nil, err
		}
		if err := closedAnswerSchema(definition.Schema); err != nil {
			return nil, err
		}
		if _, err := api.NewValidator(definition.Schema); err != nil {
			return nil, err
		}
		digest, err := api.Digest(definition.Schema)
		if err != nil {
			return nil, err
		}
		if digest != definition.Ref.Digest {
			return nil, fmt.Errorf("answer schema digest mismatch")
		}
		s.answerSchemas[componentKey(definition.Ref)] = definition.Schema
	}
	return s, nil
}
func componentKey(c api.ComponentRef) string { d, _ := api.Digest(c); return d }
func refKey(r api.ObjectRef) string          { return r.OwnerID + "/" + r.ObjectID }
func taskRef(tx runtime.Tx, t taskState) api.ObjectRef {
	return tx.Scope().Ref(t.Task.TaskID, t.Task.Revision)
}
func output(tx runtime.Tx, t taskState) TaskOutput {
	return TaskOutput{TaskRef: taskRef(tx, t), GoalRevision: t.Task.GoalRevision, ControlRevision: t.Task.ControlRevision, RequirementsState: t.Task.RequirementsState, Status: t.Task.Status, ControlTargets: api.CollectionSummary{CollectionRevision: t.RelationRevision, TotalCount: uint64(len(t.ControlTargets)), UnresolvedCount: uint64(len(t.ControlTargets)), Complete: true}, Budget: t.Task.Budget}
}
func invalid(reason string) error { return api.E("invalid_request", reason) }
func terminal(t taskState) bool   { return t.Task.Status != "active" }
func getTask(ctx context.Context, tx runtime.Tx, id string) (taskState, error) {
	var t taskState
	_, err := tx.Get(ctx, tasks, id, &t)
	return t, err
}
func cas(c api.Command, t taskState) error {
	if c.ExpectedRevision == nil || *c.ExpectedRevision != t.Task.Revision {
		return api.E("revision_conflict", "revision_changed")
	}
	return nil
}
func principal(auth runtime.Auth, t taskState) error {
	if auth.SubjectID != t.SubjectID && !auth.HasRole("task_admin") && !auth.HasRole("service") {
		return api.E("forbidden", "task_access_denied")
	}
	return nil
}
func target(c api.Command, id string) error {
	if c.TargetID != id || !api.ValidID(id) {
		return invalid("target_mismatch")
	}
	return nil
}
func contentScope(scope runtime.Scope, ref api.ContentRef) error {
	if ref.TenantID != scope.TenantID {
		return api.E("forbidden", "cross_tenant_content")
	}
	return api.ValidateRecord("ContentRef", ref)
}
func (s *Service) authorize(ctx context.Context, tx runtime.Tx, auth runtime.Auth, purpose string, content []api.ContentRef, refs []api.ObjectRef) error {
	for _, r := range content {
		if err := contentScope(tx.Scope(), r); err != nil {
			return err
		}
	}
	for _, r := range refs {
		if err := runtime.CheckRef(tx.Scope(), r); err != nil {
			return err
		}
	}
	if s.ports.Gate != nil {
		return s.ports.Gate.Authorize(ctx, tx, auth, purpose, content, refs)
	}
	for _, r := range content {
		if r.OwnerID != tx.Scope().OwnerID {
			return api.E("forbidden", "content_gate_required")
		}
	}
	for _, r := range refs {
		if r.OwnerID != tx.Scope().OwnerID && !auth.HasRole("service") {
			return api.E("forbidden", "reference_gate_required")
		}
	}
	return nil
}
func (s *Service) saveTask(ctx context.Context, tx runtime.Tx, t *taskState) error {
	old := t.Task.Revision
	t.Task.Revision++
	if err := tx.Put(ctx, tasks, t.Task.TaskID, old, *t); err != nil {
		return err
	}
	return bumpCollection(ctx, tx, t.SubjectID)
}

type collection struct {
	Revision uint64 `json:"revision"`
}

func bumpCollection(ctx context.Context, tx runtime.Tx, id string) error {
	var c collection
	rev, e := tx.Get(ctx, collections, id, &c)
	if api.IsCode(e, "not_found") || errors.Is(e, runtime.ErrNotFound) {
		return tx.Create(ctx, collections, id, id, collection{Revision: 1})
	}
	if e != nil {
		return e
	}
	c.Revision++
	return tx.Put(ctx, collections, id, rev, c)
}
func raise(ctx context.Context, tx runtime.Tx, kind, key string, ref api.ObjectRef) (api.Job, error) {
	now, err := tx.Now(ctx)
	if err != nil {
		return api.Job{}, err
	}
	return tx.Raise(ctx, kind, key, ref, now)
}
func (s *Service) transaction(ctx context.Context, store runtime.Store, scope runtime.Scope, fn func(runtime.Tx) error) error {
	status, err := store.Within(ctx, scope, s.config.Participants, fn)
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}
func (s *Service) readState(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string, revision uint64) (taskState, error) {
	var t taskState
	_, err := store.Read(ctx, scope, tasks, id, revision, &t)
	if err != nil {
		return t, err
	}
	return t, principal(auth, t)
}
func (s *Service) Read(ctx context.Context, store runtime.Store, scope runtime.Scope, auth runtime.Auth, id string) (api.Task, error) {
	t, e := s.readState(ctx, store, scope, auth, id, 0)
	return t.Task, e
}
func (s *Service) CheckCurrent(ctx context.Context, tx runtime.Tx, t taskState, requireRunning bool) error {
	if terminal(t) {
		return api.E("invalid_state", "target_terminal")
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return e
	}
	deadline, e := api.ParseTime(t.Task.Deadline)
	if e != nil {
		return e
	}
	if !now.Before(deadline) {
		return api.E("invalid_state", "deadline_exceeded")
	}
	if requireRunning && (t.Task.Control != "running" || t.PendingGoalCommand != "") {
		return api.E("invalid_state", "task_not_running")
	}
	for _, id := range t.Ancestors {
		a, e := getTask(ctx, tx, id)
		if e != nil {
			return e
		}
		if terminal(a) || requireRunning && a.Task.Control != "running" {
			return api.E("invalid_state", "ancestor_control")
		}
	}
	if t.IncomingAllocationID != "" {
		var a IncomingAllocation
		if _, e = tx.Get(ctx, incoming, t.IncomingAllocationID, &a); e != nil {
			return e
		}
		if a.Gate != "open" {
			return api.E("invalid_state", "allocation_closed")
		}
	}
	return nil
}
func requirementsDigest(reqs []api.Requirement) (string, error) {
	sorted := append([]api.Requirement{}, reqs...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].RequirementID < sorted[j].RequirementID })
	return api.Digest(sorted)
}
func refsFor(reqs []api.Requirement) []api.RequirementRef {
	r := make([]api.RequirementRef, 0, len(reqs))
	for _, v := range reqs {
		r = append(r, api.RequirementRef{RequirementID: v.RequirementID, Revision: v.Revision})
	}
	return r
}
func (s *Service) saveGoal(ctx context.Context, tx runtime.Tx, t taskState, kind string, cause api.ObjectRef) error {
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	g := api.GoalRevision{TaskRef: taskRef(tx, t), GoalRevision: t.Task.GoalRevision, GoalRef: t.Task.GoalRef, SourceRefs: t.SourceRefs, Requirements: refsFor(t.Task.Requirements), RequirementsDigest: t.Task.RequirementsDigest, ChangeKind: kind, CauseRef: cause, CreatedAt: api.Time(now)}
	return tx.Create(ctx, goals, fmt.Sprintf("%s/%020d", t.Task.TaskID, t.Task.GoalRevision), t.Task.TaskID, g)
}
func (s *Service) SubmitTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in SubmitInput) (TaskOutput, error) {
	if in.OrchestratorID != tx.Scope().OwnerID {
		return TaskOutput{}, invalid("wrong_orchestrator")
	}
	if !api.ValidID(c.TargetID) {
		return TaskOutput{}, invalid("invalid_task_id")
	}
	p, ok := s.policies[componentKey(in.PolicyRef)]
	if !ok {
		return TaskOutput{}, api.E("unsupported", "unsupported_policy")
	}
	if err := api.ValidateAmounts(in.Budget); err != nil {
		return TaskOutput{}, err
	}
	if len(in.Budget) == 0 {
		return TaskOutput{}, invalid("budget_required")
	}
	if e := checkLimits(in.Budget, p.BudgetLimits); e != nil {
		return TaskOutput{}, e
	}
	now, e := tx.Now(ctx)
	if e != nil {
		return TaskOutput{}, e
	}
	deadline, e := api.ParseTime(in.Deadline)
	if e != nil || !now.Before(deadline) || deadline.Sub(now) > time.Duration(p.MaxDurationSeconds)*time.Second {
		return TaskOutput{}, invalid("invalid_deadline")
	}
	refs := []api.ObjectRef{}
	if in.SourceSubmissionRef != nil {
		refs = append(refs, *in.SourceSubmissionRef)
	}
	if in.AcceptanceRef != nil {
		refs = append(refs, *in.AcceptanceRef)
	}
	if p.CostMode == "estimate" {
		if in.AcceptanceRef == nil || s.ports.Gate == nil {
			return TaskOutput{}, api.E("forbidden", "policy_acceptance_required")
		}
	}
	if e = s.authorize(ctx, tx, auth, "task.submit", []api.ContentRef{in.GoalRef}, refs); e != nil {
		return TaskOutput{}, e
	}
	list, e := tx.List(ctx, tasks, auth.SubjectID, "", int(s.config.MaxTasksPerSubject)+1)
	if e != nil {
		return TaskOutput{}, e
	}
	active := uint64(0)
	for _, r := range list {
		var old taskState
		if e = r.Decode(&old); e != nil {
			return TaskOutput{}, e
		}
		if !terminal(old) {
			active++
		}
	}
	if active >= s.config.MaxTasksPerSubject {
		return TaskOutput{}, api.E("overloaded", "active_task_limit")
	}
	balances := []api.BudgetBalance{}
	for _, a := range in.Budget {
		balances = append(balances, api.BudgetBalance{Unit: a.Unit, Limit: a.Value, Reserved: "0", Spent: "0"})
	}
	digest, e := requirementsDigest([]api.Requirement{})
	if e != nil {
		return TaskOutput{}, e
	}
	t := taskState{Task: api.Task{TenantID: tx.Scope().TenantID, TaskID: c.TargetID, OrchestratorID: tx.Scope().OwnerID, SubmitCommandID: c.CommandID, GoalRef: in.GoalRef, GoalRevision: 1, ControlRevision: 1, Revision: 1, PolicyRef: in.PolicyRef, Requirements: []api.Requirement{}, Deadline: in.Deadline, Status: "active", Control: "running", WaitReasons: []api.WaitReason{}, Budget: balances, OpenEffects: api.CollectionSummary{CollectionRevision: 1, Complete: true}, RequirementsState: "collecting", RequirementsDigest: digest, AcceptanceRef: in.AcceptanceRef}, SubjectID: auth.SubjectID, Policy: p, InitialGoalRef: in.GoalRef, Amendments: []api.ContentRef{}, Ancestors: []string{}, SemanticKeys: []string{}, CurrentArtifactRefs: []api.ContentRef{}, ControlTargets: []string{}, Credits: []api.Amount{}, RelationRevision: 1}
	sourceKind := "user_input"
	source := tx.Scope().Ref(c.CommandID, 1)
	if in.SourceSubmissionRef != nil {
		sourceKind = "user_input"
		source = *in.SourceSubmissionRef
	}
	t.SourceRefs = []api.SourceEvidence{{ContentRef: in.GoalRef, SourceKind: sourceKind, SubmissionRef: &source}}
	if in.DelegationContext != nil {
		d := in.DelegationContext
		if d.ParentTaskRef.TenantID != tx.Scope().TenantID || d.AllocationRef.OwnerID != d.ParentTaskRef.OwnerID {
			return TaskOutput{}, api.E("forbidden", "invalid_delegation_context")
		}
		var a IncomingAllocation
		_, e = tx.Get(ctx, incoming, incomingID(d.AllocationRef), &a)
		if e != nil {
			return TaskOutput{}, api.E("dependency_unavailable", "allocation_unverified")
		}
		if a.Gate != "open" || a.TaskRef != nil {
			return TaskOutput{}, api.E("invalid_state", "allocation_closed_or_bound")
		}
		if a.ParentOwner != d.ParentTaskRef.OwnerID || a.ReceiverID != tx.Scope().OwnerID || !api.Equal(a.Limits, in.Budget) {
			return TaskOutput{}, api.E("forbidden", "delegation_scope_exceeded")
		}
		ref := taskRef(tx, t)
		a.TaskRef = &ref
		a.Revision++
		if e = tx.Put(ctx, incoming, incomingID(d.AllocationRef), a.Revision-1, a); e != nil {
			return TaskOutput{}, e
		}
		t.IncomingAllocationID = incomingID(d.AllocationRef)
	}
	if e = tx.Create(ctx, tasks, c.TargetID, auth.SubjectID, t); e != nil {
		return TaskOutput{}, e
	}
	if e = bumpCollection(ctx, tx, auth.SubjectID); e != nil {
		return TaskOutput{}, e
	}
	if e = s.saveGoal(ctx, tx, t, "initial", source); e != nil {
		return TaskOutput{}, e
	}
	if len(in.RequirementCandidates) > 0 {
		if uint64(len(in.RequirementCandidates)) > p.MaxRequirements {
			return TaskOutput{}, invalid("requirement_limit")
		}
		pending := candidatePending{TaskID: c.TargetID, Revision: 1, Source: source, SourceKind: "command", Delta: api.RequirementDelta{BaseGoalRevision: 1, Candidates: in.RequirementCandidates, ReasonRef: in.GoalRef}}
		if e = tx.Create(ctx, candidates, c.CommandID, c.TargetID, pending); e != nil {
			return TaskOutput{}, e
		}
	}
	if _, e = raise(ctx, tx, JobAdvance, "advance/"+c.TargetID, taskRef(tx, t)); e != nil {
		return TaskOutput{}, e
	}
	return output(tx, t), nil
}
func checkLimits(limits, policy []api.Amount) error {
	p := map[string]string{}
	for _, a := range policy {
		p[a.Unit] = a.Value
	}
	for _, a := range limits {
		lim, ok := p[a.Unit]
		if !ok {
			return api.E("forbidden", "policy_scope_exceeded")
		}
		cmp, e := api.CompareDecimal(a.Value, lim)
		if e != nil {
			return e
		}
		if cmp > 0 {
			return api.E("forbidden", "policy_scope_exceeded")
		}
	}
	return nil
}
func relationID(taskID, kind, owner, id string) string {
	return taskID + "/" + kind + "/" + owner + "/" + id
}
func (s *Service) fullRelations(ctx context.Context, tx runtime.Tx, taskID string) ([]relation, error) {
	rows, e := tx.List(ctx, relations, taskID, "", int(s.config.MaxRelations)+1)
	if e != nil {
		return nil, e
	}
	if uint64(len(rows)) > s.config.MaxRelations {
		return nil, api.E("dependency_unavailable", "relation_index_capacity")
	}
	out := make([]relation, 0, len(rows))
	for _, r := range rows {
		var v relation
		if e = r.Decode(&v); e != nil {
			return nil, e
		}
		out = append(out, v)
	}
	return out, nil
}
func (s *Service) updateSummary(ctx context.Context, tx runtime.Tx, t *taskState) error {
	rows, e := s.fullRelations(ctx, tx, t.Task.TaskID)
	if e != nil {
		return e
	}
	count, open := uint64(0), uint64(0)
	targets := map[string]bool{}
	for _, r := range rows {
		if r.Kind == "operation" {
			count++
			if !r.Closed || r.MayApplyLater || r.Effect == "unknown" {
				open++
			}
			targets[r.Ref.OwnerID] = true
		}
	}
	t.RelationRevision++
	t.Task.OpenEffects = api.CollectionSummary{CollectionRevision: t.RelationRevision, TotalCount: count, UnresolvedCount: open, Complete: true}
	t.ControlTargets = []string{}
	for k := range targets {
		t.ControlTargets = append(t.ControlTargets, k)
	}
	sort.Strings(t.ControlTargets)
	return nil
}
func (s *Service) controlJobs(ctx context.Context, tx runtime.Tx, t taskState) error {
	for _, owner := range t.ControlTargets {
		if _, e := raise(ctx, tx, JobControl, "control/"+t.Task.TaskID+"/"+owner, taskRef(tx, t)); e != nil {
			return e
		}
	}
	return nil
}
func checkTaskID(id string) error {
	if !api.ValidID(id) {
		return invalid("invalid_task_id")
	}
	return nil
}
func statusAllowed(status string) bool {
	return status == "" || strings.Contains("|active|succeeded|failed|cancelled|", "|"+status+"|")
}

func incomingID(ref api.ObjectRef) string { return ref.OwnerID + "/" + ref.ObjectID }
