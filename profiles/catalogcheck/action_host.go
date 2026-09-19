package catalogcheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	contentpolicy "lerna/adapters/content/policy"
	"lerna/authorization"
	"lerna/brain"
	"lerna/catalog"
	"lerna/contextassembly"
	"lerna/execution"
	wire "lerna/gen/harness/v1"
	"lerna/memory"
	"lerna/schema"
	"lerna/sdk"
	"lerna/tasks"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// This reference assembly reuses ticket 13's complete directory, real grants,
// content and SQLite execution hosts. Business assertions remain independent
// from the driver's transition implementation and from the model's suggestions.
type actionHost struct {
	crash       string
	badReadUsed bool
	personal    *personalizedMemory
	selected    string
	f           *fixture
	h           *harness
	mode        string
	scanned     int
	location    string
}
type ActionReport struct {
	MemoryComparisonErased                bool
	Deletion                              *memory.DeletionStatus `json:",omitempty"`
	RetiredContexts                       int
	ContextSnapshots                      int
	MetadataDecisions                     int
	SettlementReplayVerified              bool
	MemoryBindingRestored                 bool
	Invocations, StartedInvocations       int
	GovernedArtifacts, RevokedArtifacts   int
	OtherState                            string
	MemoryReadAllocated                   uint64
	ReadyOperations, DispatchedOperations int
	SelectedRecord                        string
	StateDirectory                        string `json:",omitempty"`
	Case, State, StopReason, FinalState   string
	Decisions, Operations, DirectorySize  int
	Corrections                           uint32
	FirstCorrect                          bool
	Ledger                                int64
	UsedRequests, UnknownRequests         uint32
	UsedTokens, ReservedTokens            uint64
	Errors                                []string
}

func RunActionCase(ctx context.Context, mode string, model brain.Model) (ActionReport, error) {
	return runActionCase(ctx, mode, model, false)
}

// RunActionRegression retains private SQLite journals and raw decision evidence.
// Only the explicit real-provider command uses it; conformance remains offline.
func RunActionRegression(ctx context.Context, model brain.Model) (ActionReport, error) {
	return runActionCase(ctx, "multistep", model, true)
}
func runActionCase(ctx context.Context, mode string, model brain.Model, retain bool) (ActionReport, error) {
	return runActionCaseAt(ctx, mode, model, retain, "", "")
}
func runActionCaseAt(ctx context.Context, mode string, model brain.Model, retain bool, root string, crash string) (report ActionReport, err error) {
	f, e := newFixtureAt(ctx, root)
	if e != nil {
		return ActionReport{}, e
	}
	defer f.close()
	f.retain = retain
	if retain {
		defer func() { report.StateDirectory = f.root }()
	}
	def := f.defs[0]
	if _, e = f.manager.Resolve(ctx, def.Kind); e != nil {
		return ActionReport{}, e
	}
	h := f.manager.current
	if model == nil {
		model = &actionScript{mode: mode}
	}
	env := &actionHost{crash: crash, f: f, h: h, mode: mode, location: model.Capabilities().Location, selected: "item"}
	personalized := strings.HasPrefix(mode, "personalized-")
	if mode == "personalized-alternative" || (mode == "personalized-reassemble-once" || mode == "personalized-reassemble-gap" || mode == "personalized-reassemble-bound-failure") || strings.HasPrefix(mode, "personalized-reopen-") {
		env.selected = "alternative"
	}
	if e = env.allowPublic(ctx); e != nil {
		return ActionReport{}, e
	}
	before := "draft"
	if mode == "single" || mode == "wrong-write" || personalized {
		before = "submitted"
	}
	if mode == "personalized-multistep" {
		before = "draft"
	}
	if e = h.target.Seed(ctx, "item", before, 3, 10, true); e != nil {
		return ActionReport{}, e
	}
	if personalized {
		if e = h.target.Seed(ctx, "alternative", "submitted", 5, 10, true); e != nil {
			return ActionReport{}, e
		}
	}
	inputBody := []byte(`{"record":"item","quantity":3,"limit":10,"verified":true}`)
	if strings.HasPrefix(mode, "missing") {
		inputBody = []byte(`{"quantity":3,"limit":10,"verified":true}`)
	}
	input, e := h.put(ctx, inputBody)
	if e != nil {
		return ActionReport{}, e
	}
	op, e := h.operation(ctx)
	if e != nil {
		return ActionReport{}, e
	}
	requests, tokens, steps, queries, ttl := uint32(12), uint64(262144), uint32(12), uint32(128), 10*time.Minute
	if mode == "single" {
		requests, tokens, steps, queries, ttl = 4, 65536, 3, 32, 2*time.Minute
	}
	goal := "Authorize the existing " + def.Title + " record item. If necessary submit it first. Do not reject, withdraw, archive or create other records. Finish with authorized state and ledger 997."
	if strings.HasPrefix(mode, "missing") {
		goal = "Authorize an existing " + def.Title + ". The required record identity has not been provided; ask for it."
	}
	if personalized {
		goal = "Authorize exactly one existing " + def.Title + " record. Choose item by default, or alternative when an applicable preferred-record Memory says alternative. Both are submitted; do not change the other record."
	}
	if mode == "personalized-multistep" {
		goal = "Authorize the existing record item, submitting it first if necessary. Use applicable preferred-record Memory. Do not change the other record."
	}
	task, e := h.core.Submit(ctx, h.token, tasks.Submission{Namespace: h.namespace, OperationID: op, Goal: goal, InputRefs: []string{input}, Constraints: tasks.Constraints{ModelRequests: requests, ModelTokens: tokens, MaxSteps: steps, DeadlineUnix: h.now().Add(ttl).Unix()}})
	if e != nil {
		return ActionReport{}, e
	}
	r, e := h.core.Load(ctx, task.Ref)
	if e != nil {
		return ActionReport{}, e
	}
	for _, k := range []string{"claim", "start"} {
		r, e = h.work.Commit(ctx, tasks.WorkChange{ChangeID: k, Kind: k, Qualification: tasks.QualificationOf(r)})
		if e != nil {
			return ActionReport{}, e
		}
	}
	p, e := h.work.Actions(tasks.ActionLimits{MaxOperations: steps, MaxQueries: queries, MaxCorrections: 2, InputTokens: model.Capabilities().InputUpper, OutputTokens: 1024})
	if e != nil {
		return ActionReport{}, e
	}
	u, e := h.core.Updates(env, tasks.UpdateLimits{MaxOperations: 32, MaxInteractions: 8, MaxRefs: 16, IOTimeout: time.Second, Control: controlLimits()})
	if e != nil {
		return report, e
	}
	p, e = p.WithInteractions(u)
	if e != nil {
		return report, e
	}
	r, e = p.Initialize(ctx, tasks.QualificationOf(r))
	if e != nil {
		return ActionReport{}, e
	}
	if personalized {
		style := "item"
		if mode == "personalized-alternative" || mode == "personalized-inapplicable" || strings.HasPrefix(mode, "personalized-reopen-") {
			style = "alternative"
		}
		env.personal, e = env.personalize(ctx, p, r, style, mode != "personalized-inapplicable")
		if e != nil {
			return report, fmt.Errorf("initialize action memory context: %w", e)
		}

		if strings.HasPrefix(mode, "personalized-reopen-") {
			if mode == "personalized-reopen-revoked" {
				if e = env.personal.change(ctx, h, "revoke"); e != nil {
					env.personal.close()
					return report, e
				}
			}
			original := env.personal.checkpoint
			before, err := env.personal.snapshots.Read(ctx, original.Key)
			if err != nil {
				return report, err
			}
			raw, err := json.Marshal(original)
			if err != nil {
				return report, err
			}
			var saved actionMemoryBinding
			if err = json.Unmarshal(raw, &saved); err != nil {
				return report, err
			}
			env.personal.close()
			env.personal, e = env.personalizeBound(ctx, p, r, "", false, &saved)
			if e != nil {
				return report, fmt.Errorf("reopen original action memory context: %w", e)
			}
			after, err := env.personal.snapshots.Read(ctx, original.Key)
			if err != nil {
				env.personal.close()
				return report, err
			}
			if !bytes.Equal(before.Document, after.Document) || env.personal.checkpoint.ReadID != original.ReadID || env.personal.grantID != original.GrantID {
				env.personal.close()
				return report, fmt.Errorf("restoration replaced original Memory identity")
			}
			report.MemoryBindingRestored = true
		}
		defer env.personal.close()
	}
	if change, phase := personalizedFault(mode); phase == "model" {
		model = contextAfterModel{Model: model, change: func() error { return env.personal.change(ctx, h, change) }}
	}
	if strings.HasPrefix(mode, "personalized-reassemble-") {
		model = contextAfterModel{Model: model, change: func() error {
			if mode == "personalized-reassemble-unstable" {
				env.personal.changed = false
			}
			change := "related"
			if mode == "personalized-reassemble-revoked" {
				change = "revoke"
			}
			return env.personal.change(ctx, h, change)
		}}
	}
	var core brain.ActionCore = p
	if change, phase := personalizedFault(mode); phase == "admitted" {
		core = &contextAfterAdmission{ActionPort: p, revoke: func() error { return env.personal.change(ctx, h, change) }}
	}
	if mode == "revoked-admitted" {
		core = &contextAfterAdmission{ActionPort: p, revoke: func() error { return f.dataPolicy.Replace(nil) }}
	}
	if strings.Contains(mode, "record") || mode == "recover-dispatch" {
		core = &actionRecoveryPoint{ActionPort: p, phase: mode}
	}
	var environment brain.ActionEnvironment = env
	if strings.HasPrefix(mode, "personalized-reassemble-") {
		environment = reassemblingActionHost{env}
	}
	b, e := brain.NewActions(model, environment, core)
	if e != nil {
		return ActionReport{}, e
	}
	bounded, cancel := context.WithTimeout(ctx, ttl)
	defer cancel()
	r, e = b.Run(bounded, task.Ref)
	if e == errActionInterrupted {
		h.clock.advance(2 * time.Minute)
		if mode == "revoked-record" {
			if e = f.dataPolicy.Replace(nil); e != nil {
				return report, e
			}
		}
		b, e = brain.NewActions(model, env, p)
		if e != nil {
			return report, e
		}
		r, e = b.Run(bounded, task.Ref)
	}
	if e != nil {
		return ActionReport{}, e
	}
	if mode == "missing-resume" {
		if len(r.Interactions) != 1 || r.Work[0].InFlight {
			return report, fmt.Errorf("missing actionable interaction")
		}
		ref, e := h.put(ctx, []byte(`"item"`))
		if e != nil {
			return report, e
		}
		op, e := h.operation(ctx)
		if e != nil {
			return report, e
		}
		if _, e = u.ProvideInput(ctx, h.token, tasks.InputRequest{OperationID: op, Ref: r.Task.Ref, ExpectedVersion: r.Task.Version, InteractionID: r.Interactions[0].ID, AnswerRef: ref}); e != nil {
			return report, e
		}
		r, e = b.Run(bounded, task.Ref)
		if e != nil {
			return report, e
		}
	}
	if _, phase := personalizedFault(mode); (phase == "model" || phase == "save" || strings.HasPrefix(mode, "personalized-reassemble-")) && r.Task.State == "WAITING" {
		d := r.Actions.Decisions[len(r.Actions.Decisions)-1]
		if d.Record == nil || d.Record.Evidence != "" {
			return report, fmt.Errorf("context failure was not metadata-only")
		}
		before, err := json.Marshal(r)
		if err != nil {
			return report, err
		}
		if err = p.Record(ctx, d.Qualification, d.Number, *d.Record); err != nil {
			return report, err
		}
		r, err = b.Run(bounded, task.Ref)
		if err != nil {
			return report, err
		}
		after, err := json.Marshal(r)
		if err != nil || !bytes.Equal(before, after) {
			return report, fmt.Errorf("settlement replay changed original decision or budget")
		}
		report.SettlementReplayVerified = true
	}
	snapshot, e := h.target.Snapshot(ctx, env.selected)
	if e != nil {
		return ActionReport{}, e
	}
	report = ActionReport{SettlementReplayVerified: report.SettlementReplayVerified, MemoryBindingRestored: report.MemoryBindingRestored, SelectedRecord: env.selected, Case: mode, State: r.Task.State, StopReason: r.Task.StopReason, Decisions: len(r.Actions.Decisions), Operations: len(r.Actions.Actions), DirectorySize: env.scanned, Corrections: r.Actions.Corrections, FirstCorrect: true, FinalState: snapshot.State, Ledger: snapshot.Ledger, UsedRequests: r.Task.ModelUsedRequests, UnknownRequests: r.Task.ModelReservedRequests, UsedTokens: r.Task.ModelUsedTokens, ReservedTokens: r.Task.ModelReservedTokens}
	if personalized {
		other := "alternative"
		if env.selected == "alternative" {
			other = "item"
		}
		state, err := h.target.Snapshot(ctx, other)
		if err != nil {
			return report, err
		}
		report.OtherState = state.State
		if change, _ := personalizedFault(mode); change == "delete" {
			wait, stop := context.WithTimeout(ctx, 2*time.Second)
			tick := time.NewTicker(10 * time.Millisecond)
			for {
				_, e := env.personal.snapshots.Read(wait, env.personal.checkpoint.Key)
				if e == contextassembly.Invalidated {
					break
				}
				if e != nil {
					tick.Stop()
					stop()
					return report, e
				}
				select {
				case <-wait.Done():
					tick.Stop()
					stop()
					return report, fmt.Errorf("deletion cleanup not observed")
				case <-tick.C:
				}
			}
			tick.Stop()
			stop()
		}
		if env.personal.deletion != nil {
			status, e := env.personal.observeDeletionCleanup(ctx)
			if e != nil {
				return report, e
			}
			report.Deletion = &status
			if env.personal.original != nil {
				original, e := h.auth.InspectMemoryOperation(ctx, h.token, env.personal.original.OperationId)
				if e != nil {
					return report, e
				}
				report.MemoryComparisonErased = original.State == "reserved" && original.Admission.SemanticSHA256 == ""
			}
		}
		for number, digest := range env.personal.snapshotDigests {
			key := contextassembly.Key{Namespace: h.namespace, TaskID: r.Task.Ref.TaskID, Decision: uint64(number)}
			snapshot, err := env.personal.snapshots.Read(ctx, key)
			if err == contextassembly.Invalidated {
				report.ContextSnapshots++
				report.RetiredContexts++
				continue
			}
			if err != nil {
				return report, err
			}
			if sha256.Sum256(snapshot.Document) != digest {
				return report, fmt.Errorf("old decision snapshot changed")
			}
			report.ContextSnapshots++
		}
		for _, id := range env.personal.grantIDs {
			grant, err := env.personal.grants.Get(ctx, h.token, id)
			if err != nil {
				return report, err
			}
			report.MemoryReadAllocated += grant.Allocated
		}
	}
	for _, a := range r.Actions.Actions {
		if personalized && a.Status != "READY" && a.Status != "SKIPPED" {
			invocation, err := h.services[a.Descriptor].GetInvocation(ctx, a.OperationID)
			if err != nil {
				return report, err
			}
			report.Invocations++
			if invocation.Started {
				report.StartedInvocations++
			}
		}
		if a.Status == "READY" {
			report.ReadyOperations++
		}
		if a.Status == "DISPATCHED" {
			report.DispatchedOperations++
		}
	}
	for i, d := range r.Actions.Decisions {
		if d.Record != nil && d.Record.Evidence == "" && d.Record.Error != "" {
			report.MetadataDecisions++
		}
		if i == 0 && d.Record != nil && d.Record.Proposal.Kind == "wait" && !strings.HasPrefix(mode, "missing") {
			report.FirstCorrect = false
		}
		if d.Rejection != "" {
			report.Errors = append(report.Errors, d.Rejection)
			if i == 0 {
				report.FirstCorrect = false
			}
		}
	}
	if r.Actions.WrongWrite {
		report.FirstCorrect = false
	}
	for _, fact := range r.ExecutionReports {
		if fact.Result != "SUCCESS" {
			report.FirstCorrect = false
		}
	}
	if personalized && r.Task.State == "COMPLETED" {
		if e = env.checkDerived(ctx, r, &report); e != nil {
			return report, e
		}
	}
	if retain {
		raw, e := json.MarshalIndent(r, "", "  ")
		if e != nil {
			return report, e
		}
		if e = os.WriteFile(filepath.Join(f.root, "task-snapshot.json"), raw, 0600); e != nil {
			return report, e
		}
	}
	return report, nil
}
func (a *actionHost) allowPublic(ctx context.Context) error {
	locations := []string{"local"}
	if a.location != "local" {
		locations = append(locations, a.location)
	}
	// Both policies belong to the test's public material, never inherited from a
	// user's execution grant. Tests can independently revoke either source later.
	if e := a.h.policy.Replace([]contentpolicy.Rule{{Kind: "input", Key: "public-counter", Revision: 1, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: locations, RetainUntil: a.h.now().Add(time.Hour).Unix()}}); e != nil {
		return e
	}
	if e := a.f.dataPolicy.Replace([]contentpolicy.Rule{{Kind: "catalog", Key: "business-manifest", Revision: 1, Actions: []string{"process", "discover", "disclose"}, Purposes: []string{"task"}, Locations: locations, RetainUntil: a.h.now().Add(time.Hour).Unix()}, {Kind: "query", Key: "host-session", Revision: 1, Actions: []string{"process"}, Purposes: []string{"task"}, Locations: locations, RetainUntil: a.h.now().Add(time.Hour).Unix()}}); e != nil {
		return e
	}
	for _, h := range []*harness{a.h, a.f.host} {
		snap, e := h.db.Load(ctx)
		if e != nil {
			return e
		}
		scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read", "task.execute", "task.reconcile", "task.input", "catalog.list", "catalog.search", "catalog.describe", "capability.invoke", "capability.read", "capability.reconcile", "resource.change", "content.store", "content.process", "content.retain", "content.discover", "content.disclose", "resource.control.read"}, Purposes: []string{"task"}, Locations: locations, ExpiresUnix: h.now().Add(time.Hour).Unix()}
		for _, cmd := range []*wire.AuthorizationCommand{{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "actions-public", Scope: scope}}}}}, {Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "actions-public", Subject: "operator", Scope: scope, Mode: "continuous"}}}} {
			cmd.ExpectedRevision = snap.State.Revision
			op, e := h.operation(ctx)
			if e != nil {
				return e
			}
			out, e := h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: h.namespace, OperationID: op, Command: cmd})
			if e != nil {
				return e
			}
			_ = out
			snap, e = h.db.Load(ctx)
			if e != nil {
				return e
			}
		}
	}
	return nil
}
func (a *actionHost) Validate(ctx context.Context, t tasks.Task, location string) error {
	if a.personal != nil {
		return a.personal.session.Validate(ctx, t, location)
	}
	return a.baseValidate(ctx, t, location)
}
func (a *actionHost) baseValidate(ctx context.Context, t tasks.Task, location string) error {
	for _, h := range []*harness{a.h, a.f.host} {
		view, e := h.auth.ViewActions(ctx, h.token, []*wire.AuthorizationAction{{Resource: "root", Action: "content.process", Purpose: "task", Location: location}, {Resource: "root", Action: "content.disclose", Purpose: "task", Location: location}})
		if e != nil {
			return e
		}
		if len(view.Allowed) != 2 || !view.Allowed[0] || !view.Allowed[1] {
			return &authorization.Error{Code: authorization.Denied}
		}
	}
	for _, action := range []string{"process", "disclose"} {
		if e := a.h.policy.Check(ctx, &wire.ContentSource{Kind: "input", Key: "public-counter", Revision: 1}, action, "task", location, a.h.now().Unix()); e != nil {
			return e
		}
		if e := a.f.dataPolicy.Check(ctx, &wire.ContentSource{Kind: "catalog", Key: "business-manifest", Revision: 1}, action, "task", location, a.h.now().Unix()); e != nil {
			return e
		}
	}
	cap := a.h.cap
	cap.Location = location
	for _, ref := range t.InputRefs {
		if _, e := a.h.access.Read(ctx, a.h.token, ref, cap); e != nil {
			return e
		}
	}
	return nil
}
func (a *actionHost) Assemble(ctx context.Context, t tasks.Task, location string, max int) (brain.Input, error) {
	if a.personal != nil {
		if e := a.advanceContext(ctx, t); e != nil {
			return brain.Input{}, e
		}
		return a.personal.session.Assemble(ctx, t, location, max)
	}
	return a.baseAssemble(ctx, t, location, max)
}
func (a *actionHost) baseAssemble(ctx context.Context, t tasks.Task, location string, max int) (brain.Input, error) {
	if e := a.baseValidate(ctx, t, location); e != nil {
		return brain.Input{}, e
	}
	s, e := a.h.target.Snapshot(ctx, "item")
	if e != nil {
		return brain.Input{}, e
	}
	raw, _ := json.Marshal(s)
	in := brain.Input{Goal: t.Goal, Constraints: "Every action uses the exact candidate Ref.Digest in capability. Use submitted input facts and record identity from the goal or a newer user reply. resource_version is the current record version; each confirmed transition increments it by one. Use dependencies for actions in one batch. Do not invent missing arguments. Return wait for missing input. Never claim completion; Core verifies the goal.", Blocks: []brain.Block{{Ref: "business-state", Text: string(raw), Role: "state"}}}
	cap := a.h.cap
	cap.Location = location
	for _, ref := range t.InputRefs {
		body, e := a.h.access.Read(ctx, a.h.token, ref, cap)
		if e != nil {
			return in, e
		}
		in.Blocks = append(in.Blocks, brain.Block{Ref: ref, Text: string(body), Subject: t.Subject, Role: "input"})
	}
	for _, fact := range t.InputFacts {
		if fact.Kind != "reply" {
			continue
		}
		body, e := a.h.access.Read(ctx, a.h.token, fact.Reference, cap)
		if e != nil {
			return in, e
		}
		in.Blocks = append(in.Blocks, brain.Block{Ref: fact.Reference, Text: string(body), Subject: fact.Subject, Role: "user-reply", ReplyTo: []brain.ReplyTo{{InteractionID: fact.InteractionID, QuestionRef: fact.QuestionRef}}})
	}
	return in, nil
}
func (a *actionHost) Search(ctx context.Context, t tasks.Task, location string) (catalog.Page, error) {
	if e := a.Validate(ctx, t, location); e != nil {
		return catalog.Page{}, e
	}
	page, e := a.f.client.Search(ctx, catalog.Query{Text: t.Goal, ResourceType: t.Ref.Namespace, Purpose: "task", Location: "local", Limit: 6, Budget: 2048})
	if e == nil {
		a.scanned = page.Scanned
	}
	return page, e
}
func (a *actionHost) Describe(ctx context.Context, t tasks.Task, location string, ref catalog.Ref) (catalog.Entry, error) {
	if e := a.Validate(ctx, t, location); e != nil {
		return catalog.Entry{}, e
	}
	if a.mode == "read-only-correction" && !a.badReadUsed {
		a.badReadUsed = true
		ref.Version = "missing-fixture-version"
	}
	return a.f.client.Describe(ctx, ref)
}
func (a *actionHost) SaveDecision(ctx context.Context, t tasks.Task, in brain.Input, result brain.Result) (string, error) {
	if change, phase := personalizedFault(a.mode); phase == "save" {
		if err := a.personal.change(ctx, a.h, change); err != nil {
			return "", err
		}
	}

	raw, e := json.Marshal(struct {
		Input  brain.Input
		Result brain.Result
	}{in, result})
	if e != nil {
		return "", e
	}
	return a.putDerived(ctx, raw)
}

func (a *actionHost) SaveQuestion(ctx context.Context, t tasks.Task, question string) (string, error) {
	raw, e := json.Marshal(question)
	if e != nil {
		return "", e
	}
	return a.putDerived(ctx, raw)
}
func (a *actionHost) ValidateInput(ctx context.Context, token string, t tasks.Task, refs []string, constraint *tasks.AnswerConstraint) error {
	for _, ref := range refs {
		raw, e := a.h.access.Read(ctx, token, ref, a.h.cap)
		if e != nil {
			return e
		}
		if constraint != nil {
			var value string
			if constraint.Kind != "text" || json.Unmarshal(raw, &value) != nil || value == "" || len(value) > int(constraint.MaxBytes) {
				return &authorization.Error{Code: authorization.Invalid}
			}
		}
	}
	return nil
}

func (a *actionHost) bindRecovered(ctx context.Context, task tasks.Task, descriptor string) (*execution.Service, error) {
	// The Core binding carries current worker eligibility separately from the
	// original immutable request qualification. Older service objects are fenced.
	run, e := a.h.core.Load(ctx, task.Ref)
	if e != nil {
		return nil, e
	}
	if run.Task.Version != task.Version || run.Task.Owner != task.Owner || run.Task.OwnerEpoch != task.OwnerEpoch {
		return nil, &authorization.Error{Code: authorization.Conflict}
	}
	svc := a.h.services[descriptor]
	if svc == nil {
		return nil, fmt.Errorf("unbound descriptor")
	}
	bound, e := svc.BindCore(a.h.work.WithActionRecovery(tasks.QualificationOf(run)))
	if e != nil {
		return nil, e
	}
	a.h.services[descriptor] = bound
	return bound, nil
}
func (a *actionHost) Prepare(ctx context.Context, t tasks.Task, entry catalog.Entry, p brain.ProposedAction) (tasks.Action, error) {
	if entry.Ref.Namespace != t.Ref.Namespace {
		return tasks.Action{}, fmt.Errorf("wrong namespace")
	}
	registry, e := schema.New([]schema.Resource{entry.Capability.Input})
	if e != nil {
		return tasks.Action{}, e
	}
	s := entry.Capability.Input
	if e = registry.Validate(&wire.DynamicPayload{TypeName: s.Type, SchemaId: s.ID, SchemaVersion: s.Version, SchemaDigest: schema.Digest(s.Document), Json: []byte(p.Arguments)}); e != nil {
		return tasks.Action{}, e
	}
	ref, e := a.putDerived(ctx, []byte(p.Arguments))
	if e != nil {
		return tasks.Action{}, e
	}
	op, e := a.h.operation(ctx)
	if e != nil {
		return tasks.Action{}, e
	}
	// Reserve the issued identity in the execution namespace before the issuance
	// window can close. This grants no effect authority: Core still atomically
	// admits the full mapping and validates it at Invoke/Start.
	e = a.h.grants.UpdateExecution(ctx, func(tx authorization.ExecutionTransaction) error {
		id, e := tx.AuthorizeAction(a.h.token, &wire.AuthorizationAction{Resource: entry.Resource, Action: "capability.invoke", Purpose: entry.Purpose, Location: entry.Location})
		if e != nil {
			return e
		}
		if id.Subject != a.h.binding.Subject {
			return &authorization.Error{Code: authorization.Denied}
		}
		return tx.ReserveExecutionOperation(op, id.Subject)
	})
	if e != nil {
		return tasks.Action{}, e
	}
	return tasks.Action{Key: p.Key, DependsOn: p.DependsOn, OperationID: op, Descriptor: entry.Ref.Digest, InputRef: ref, ResourceVersion: p.ResourceVersion, ControlVersion: 1, Write: true}, nil
}
func (a *actionHost) actionService(x tasks.Action) (catalog.Entry, *execution.Service, error) {
	for _, entry := range a.h.entries {
		if entry.Ref.Digest == x.Descriptor {
			return entry, a.h.services[x.Descriptor], nil
		}
	}
	return catalog.Entry{}, nil, fmt.Errorf("unknown descriptor")
}
func (a *actionHost) Execute(ctx context.Context, t tasks.Task, x tasks.Action) error {
	if e := a.Validate(ctx, t, a.location); e != nil {
		return e
	}
	entry, svc, e := a.actionService(x)
	if e != nil {
		return e
	}
	svc, e = a.bindRecovered(ctx, t, x.Descriptor)
	if e != nil {
		return e
	}
	if _, e = a.f.client.Describe(ctx, entry.Ref); e != nil {
		return e
	}
	req := execution.Request{OperationID: x.OperationID, Qualification: x.Qualification, Capability: entry.Ref.Name, Version: entry.Ref.Version, Implementation: entry.Ref.Implementation, ImplementationVersion: entry.Ref.ImplementationVersion, DescriptorSHA256: x.Descriptor, InputRef: x.InputRef, ResourceVersion: x.ResourceVersion, ControlVersion: x.ControlVersion}
	material, e := a.h.issue(ctx, req)
	if e != nil {
		return e
	}
	client := sdk.NewCapabilityClient(a.f.transport, t.Ref.Namespace)
	if _, e = client.Invoke(ctx, req, material); e != nil {
		// Query the original operation after an uncertain admission. Never allocate
		// a replacement identity. Absence/permission ambiguity stays a wait.
		if _, lookup := svc.GetInvocation(ctx, x.OperationID); lookup != nil {
			return e
		}
	}
	if a.crash != "" && (a.mode != "personalized-multistep" || a.personal.checkpoint.Key.Decision == 2) {
		if a.crash == "effect" {
			svc, e = a.effectCheckpointService(ctx, req)
			if e != nil {
				return e
			}
		} else {
			return a.checkpointInvocation(ctx, req)
		}
	}
	if change, phase := personalizedFault(a.mode); phase == "invoked" {
		if e = a.personal.change(ctx, a.h, change); e != nil {
			return e
		}
	}
	if a.personal != nil {
		guard, err := a.personal.session.StartGuard(req, t)
		if err != nil {
			return err
		}
		svc, e = svc.BindStartGuard(guard)
		if e != nil {
			return e
		}
		a.h.services[x.Descriptor] = svc
	}
	if _, e = svc.Run(ctx, x.OperationID); e != nil {
		return e
	}
	return svc.Drain(ctx, 16)
}
func (a *actionHost) Recover(ctx context.Context, t tasks.Task, x tasks.Action) error {
	_, svc, e := a.actionService(x)
	if e != nil {
		return e
	}
	svc, e = a.bindRecovered(ctx, t, x.Descriptor)
	if e != nil {
		return e
	}
	r, e := svc.GetInvocation(ctx, x.OperationID)
	if e != nil {
		// Replay the original immutable request through all current admission
		// gates. Existing Invoke deduplicates before effects; Run never re-starts
		// a Started operation. A denied/missing read cannot mint a replacement.
		return a.Execute(ctx, t, x)
	}
	if !r.Started {
		return a.Execute(ctx, t, x)
	} else if r.Effect == "UNKNOWN" {
		_, e = svc.Reconcile(ctx, x.OperationID)
	}
	if e != nil {
		return e
	}
	return svc.Drain(ctx, 16)
}
func (a *actionHost) Assess(ctx context.Context, r tasks.RunSnapshot) (brain.Assessment, error) {
	record := "item"
	if a.selected != "" {
		record = a.selected
	}
	s, e := a.h.target.Snapshot(ctx, record)
	if e != nil {
		return brain.Assessment{}, e
	}
	wrong := s.State == "rejected" || s.State == "withdrawn" || s.State == "archived"
	// Inspect each confirmed action, not just final state: an unrelated create or
	// wrong write remains failure even if a later action restores the target.
	for _, x := range r.Actions.Actions {
		if r.ExecutionReports[x.OperationID].Effect == "CONFIRMED" {
			entry, _, e := a.actionService(x)
			if e != nil {
				return brain.Assessment{}, e
			}
			if !strings.HasSuffix(entry.Ref.Name, ".submit") && !strings.HasSuffix(entry.Ref.Name, ".authorize") {
				wrong = true
			}
			data, e := a.h.access.Read(ctx, a.h.token, x.InputRef, entry.Capability)
			if e != nil {
				return brain.Assessment{}, e
			}
			var args struct {
				Record string `json:"record"`
			}
			if json.Unmarshal(data, &args) != nil || args.Record != record {
				wrong = true
			}
		}
	}
	satisfied := s.State == "authorized" && s.Ledger == expectedLedger(record)
	if !wrong && !satisfied {
		return brain.Assessment{}, nil
	}
	raw, _ := json.Marshal(s)
	evidence, e := a.putDerived(ctx, raw)
	return brain.Assessment{Satisfied: satisfied, WrongWrite: wrong, Evidence: evidence}, e
}

type actionScript struct {
	mode  string
	calls int
}

func (*actionScript) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "scripted-action-fixture", Version: "1", Location: "local", Text: true, Structured: true, HardBounds: true, InputUpper: 8192, ContextTokens: 32768}
}
func (m *actionScript) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	m.calls++
	if m.mode == "personalized-multistep" {
		expected := "draft"
		if m.calls == 2 {
			expected = "submitted"
		}
		observed := false
		for _, block := range in.Input.Blocks {
			if block.Ref == "business-state" {
				var state struct {
					State   string
					Version uint64
				}
				if json.Unmarshal([]byte(block.Text), &state) == nil && state.State == expected && state.Version == uint64(m.calls) {
					observed = true
				}
			}
		}
		if !observed || m.calls > 2 {
			return brain.Result{}, fmt.Errorf("decision did not receive current business facts")
		}
	}
	// This is deliberately labelled a deterministic fixture, never real-model
	// quality evidence. It selects from the actual authorized catalog input.
	action := "submit"
	version := uint64(1)
	if m.mode == "single" || m.calls > 1 || strings.HasPrefix(m.mode, "personalized-") {
		action = "authorize"
	}
	if m.mode == "personalized-multistep" && m.calls == 1 {
		action = "submit"
	}
	if m.calls > 1 {
		version = 2
	}
	if strings.HasPrefix(m.mode, "personalized-reassemble-") {
		found := false
		for _, block := range in.Input.Blocks {
			if block.Ref == "business-state" {
				var state struct{ Version uint64 }
				if json.Unmarshal([]byte(block.Text), &state) == nil && state.Version > 0 {
					version = state.Version
					found = true
				}
			}
		}
		if !found {
			return brain.Result{}, fmt.Errorf("current business version missing")
		}
	}
	if m.mode == "wrong-write" {
		action = "reject"
	}
	if m.mode == "missing" || m.mode == "missing-resume" && m.calls == 1 {
		raw, _ := json.Marshal(brain.ActionOutput{Kind: "wait", Reason: "record input is missing", Actions: []brain.ProposedAction{}})
		return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 40}}, nil
	}
	if m.mode == "missing-resume" {
		answered := false
		for _, block := range in.Input.Blocks {
			var reply string
			if block.Role == "user-reply" && len(block.ReplyTo) == 1 && json.Unmarshal([]byte(block.Text), &reply) == nil && reply == "item" {
				answered = true
			}
		}
		if !answered {
			return brain.Result{}, fmt.Errorf("current bound user reply missing")
		}
		action = "submit"
		version = 1
		if m.calls > 2 {
			action = "authorize"
			version = 2
		}
	}
	if m.mode == "no-progress" {
		return brain.Result{Content: []byte(`{"kind":"actions","reason":"","actions":[]}`), Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 40}}, nil
	}
	digest := scriptCapability(in.Input, action)
	args := `{"record":"item"}`
	if strings.HasPrefix(m.mode, "personalized-") {
		for _, block := range in.Input.Blocks {
			if block.Role == "memory" {
				var value struct{ Claim, Value string }
				if json.Unmarshal([]byte(block.Text), &value) == nil && value.Claim == "preferred-record" && value.Value == "alternative" {
					args = `{"record":"alternative"}`
				}
			}
		}
	}

	if m.mode == "invalid-then-correct" && m.calls == 1 {
		args = `{}`
	}
	if m.mode == "invalid-then-correct" && m.calls == 2 {
		action = "submit"
		version = 1
		digest = scriptCapability(in.Input, "submit")
	}
	if m.mode == "invalid-then-correct" && m.calls == 3 {
		version = 2
	}
	actions := []brain.ProposedAction{{Key: "step", Capability: digest, DependsOn: []string{}, Arguments: args, ResourceVersion: version}}
	if m.mode == "batch" {
		actions[0].Key = "submit"
		actions = append(actions, brain.ProposedAction{Key: "authorize", Capability: scriptCapability(in.Input, "authorize"), DependsOn: []string{"submit"}, Arguments: args, ResourceVersion: 2})
	}
	raw, _ := json.Marshal(brain.ActionOutput{Kind: "actions", Actions: actions})
	return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 40}}, nil
}

func scriptCapability(in brain.Input, action string) string {
	for _, b := range in.Blocks {
		if b.Role != "capability" {
			continue
		}
		var row struct{ Ref catalog.Ref }
		if json.Unmarshal([]byte(b.Text), &row) == nil && strings.HasSuffix(row.Ref.Name, "."+action) {
			return row.Ref.Digest
		}
	}
	return ""
}

// contextAfterAdmission injects a policy change between Core admission and the
// next new dispatch. It does not alter the independent target state or driver.
type contextAfterAdmission struct {
	*tasks.ActionPort
	revoke func() error
}

func (p *contextAfterAdmission) Admit(ctx context.Context, q tasks.Qualification, n uint32) (tasks.RunSnapshot, error) {
	out, e := p.ActionPort.Admit(ctx, q, n)
	if e != nil {
		return out, e
	}
	return out, p.revoke()
}

func expectedLedger(record string) int64 {
	if record == "alternative" {
		return 995
	}
	return 997
}

func (a *actionHost) putDerived(ctx context.Context, data []byte) (string, error) {
	if a.personal != nil {
		return a.h.putLineage(ctx, data, a.personal.lineage)
	}
	return a.h.put(ctx, data)
}

// Assemble is called after Core reserves a decision. Rebinding is allowed only
// for that next unrecorded decision; Validate and existing StartGuards never rebind.
func (a *actionHost) advanceContext(ctx context.Context, t tasks.Task) error {
	current, e := a.h.core.Load(ctx, t.Ref)
	if e != nil {
		return e
	}
	if current.Actions == nil || len(current.Actions.Decisions) == 0 {
		return contextassembly.Invalid
	}
	decision := current.Actions.Decisions[len(current.Actions.Decisions)-1]
	if uint64(decision.Number) == a.personal.checkpoint.Key.Decision {
		return nil
	}
	if uint64(decision.Number) != a.personal.checkpoint.Key.Decision+1 || decision.Record != nil {
		return contextassembly.Invalidated
	}
	return a.personal.bindDecision(ctx, current, decision.Number)
}

// Only hosts that can obtain a new authorized revision implement this opt-in.
type reassemblingActionHost struct{ *actionHost }

func (a reassemblingActionHost) Reassemble(ctx context.Context, t tasks.Task, location string, max int) (brain.Input, error) {
	current, e := a.h.core.Load(ctx, t.Ref)
	if e != nil {
		return brain.Input{}, e
	}
	if current.Actions == nil || len(current.Actions.Decisions) < 2 {
		return brain.Input{}, contextassembly.Invalidated
	}
	d := current.Actions.Decisions[len(current.Actions.Decisions)-1]
	if d.Record != nil || uint64(d.Number) != a.personal.checkpoint.Key.Decision+1 {
		return brain.Input{}, contextassembly.Invalidated
	}
	if e = a.personal.refreshDecision(ctx, current, d.Number); e != nil {
		return brain.Input{}, e
	}
	return a.personal.session.Assemble(ctx, t, location, max)
}
