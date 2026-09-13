package brain

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"lerna/catalog"
	"lerna/internal/jsonvalue"
	"lerna/tasks"
	"sync"
	"time"
)

const ActionContract = "harness_actions_v1"

// ProposedAction contains only proposal-local identities. Exact descriptor
// selection is resolved against the authorized candidates supplied this turn.
type ProposedAction struct {
	Key             string   `json:"key"`
	Capability      string   `json:"capability"`
	DependsOn       []string `json:"depends_on"`
	Arguments       string   `json:"arguments"`
	ResourceVersion uint64   `json:"resource_version"`
}
type ActionOutput struct {
	Kind    string           `json:"kind"`
	Reason  string           `json:"reason"`
	Actions []ProposedAction `json:"actions"`
}
type Assessment struct {
	Satisfied, WrongWrite bool
	// ReadyForAnswer requests a Core phase transition, not task completion.
	ReadyForAnswer bool
	Evidence       string
}

// ActionEnvironment is trusted host assembly. Catalog methods must independently
// authorize source processing/disclosure at the supplied model location; Prepare
// validates exact schemas and saves controlled input. Assess uses business facts,
// not model claims. Execute/Recover use the existing execution admission/outbox.
type ActionEnvironment interface {
	Context
	Search(context.Context, tasks.Task, string) (catalog.Page, error)
	Describe(context.Context, tasks.Task, string, catalog.Ref) (catalog.Entry, error)
	SaveDecision(context.Context, tasks.Task, Input, Result) (string, error)
	SaveQuestion(context.Context, tasks.Task, string) (string, error)
	Prepare(context.Context, tasks.Task, catalog.Entry, ProposedAction) (tasks.Action, error)
	Execute(context.Context, tasks.Task, tasks.Action) error
	Recover(context.Context, tasks.Task, tasks.Action) error
	Assess(context.Context, tasks.RunSnapshot) (Assessment, error)
}

// ReassemblingContext opts a trusted host into fresh context for a NEW Core
// decision after a known, metadata-only invalidation. It must preserve old binds.
type ReassemblingContext interface {
	Reassemble(context.Context, tasks.Task, string, int) (Input, error)
}

// SnapshotAssessor optionally performs a pure phase/goal calculation from
// supplied runtime facts and previously acquired immutable facts. It must not
// perform I/O, acquire new data, or treat a cached fact as current authority.
// false requests the ordinary, query-charged Assess path. A true result still
// goes through the same Core transition and current-context checks below.
type SnapshotAssessor interface {
	AssessSnapshot(tasks.RunSnapshot) (Assessment, bool)
}

// MeteredAssessor is a trusted host whose assessment charges each actual
// observation against the original task before I/O, including failed reads.
// It must not expose an unmetered data path or create a new recovery allowance.
// Hosts without this contract retain the ordinary assessment query charge.
type MeteredAssessor interface {
	AssessMetered(context.Context, tasks.RunSnapshot) (Assessment, error)
}

type ActionCore interface {
	EnsureLease(context.Context, tasks.Qualification) (tasks.RunSnapshot, error)
	WaitInput(context.Context, tasks.Qualification, uint32, string) (tasks.RunSnapshot, error)
	KeepAlive(context.Context, tasks.Qualification) error
	Current(context.Context, tasks.Ref) (tasks.RunSnapshot, error)
	Reserve(context.Context, tasks.Qualification) (tasks.ActionDecision, error)
	Record(context.Context, tasks.Qualification, uint32, tasks.DecisionRecord) error
	Admit(context.Context, tasks.Qualification, uint32) (tasks.RunSnapshot, error)
	Next(context.Context, tasks.Qualification) (tasks.Action, error)
	ChargeQuery(context.Context, tasks.Qualification, string) error
	Finish(context.Context, tasks.Qualification, string, bool) (tasks.RunSnapshot, error)
	Wait(context.Context, tasks.Qualification, string) (tasks.RunSnapshot, error)
}

// AnswerTransitionCore is optional for hosts that compose actions with a later
// governed answer stage. Legacy action-only Core implementations stay usable.
type AnswerTransitionCore interface {
	PrepareAnswer(context.Context, tasks.Qualification) (tasks.RunSnapshot, error)
}

type ActionBrain struct {
	model      Model
	env        ActionEnvironment
	core       ActionCore
	mu         sync.Mutex
	active     map[tasks.Ref]bool
	slots      chan struct{}
	modelSlots chan struct{}
}

func NewActions(m Model, e ActionEnvironment, c ActionCore) (*ActionBrain, error) {
	if m == nil || e == nil || c == nil {
		return nil, Error("INVALID_ARGUMENT")
	}
	return &ActionBrain{model: m, env: e, core: c, active: map[tasks.Ref]bool{}, slots: make(chan struct{}, 4), modelSlots: make(chan struct{}, 4)}, nil
}

// Run is bounded by persisted budgets and the caller's wall-clock deadline. A
// host should use one shared instance and renew its Core worker lease normally.
// It never redispatches a model reservation or replaces an admitted operation.
func (b *ActionBrain) Run(ctx context.Context, ref tasks.Ref) (tasks.RunSnapshot, error) {
	b.mu.Lock()
	if b.active[ref] {
		b.mu.Unlock()
		return tasks.RunSnapshot{}, Error("BUSY")
	}
	b.active[ref] = true
	b.mu.Unlock()
	defer func() { b.mu.Lock(); delete(b.active, ref); b.mu.Unlock() }()
	select {
	case b.slots <- struct{}{}:
		defer func() { <-b.slots }()
	default:
		return tasks.RunSnapshot{}, Error("BUSY")
	}
	initial, e := b.core.Current(ctx, ref)
	if e != nil {
		return initial, e
	}
	if initial.Actions != nil && initial.Actions.AnswerQualification != nil {
		return initial, nil
	}
	initial, e = b.core.EnsureLease(ctx, tasks.QualificationOf(initial))
	if e != nil {
		return initial, e
	}
	if initial.Limits.RenewEvery <= 0 {
		return initial, Error("INPUT_INVALIDATED")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	renewDone := make(chan struct{})
	go func() {
		defer close(renewDone)
		tick := time.NewTicker(initial.Limits.RenewEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				bounded, done := context.WithTimeout(ctx, initial.Limits.IOTimeout)
				r, e := b.core.Current(bounded, ref)
				if e == nil && r.Task.State == "RUNNING" {
					if len(r.Work) == 0 || r.Work[0].Generation != initial.Work[0].Generation || r.Task.Owner != initial.Task.Owner || r.Task.OwnerEpoch != initial.Task.OwnerEpoch {
						done()
						cancel()
						return
					}
					e = b.core.KeepAlive(bounded, tasks.QualificationOf(r))
				}
				done()
				if e != nil && e.Error() != "VERSION_CONFLICT" {
					cancel()
					return
				}
			}
		}
	}()
	defer func() { cancel(); <-renewDone }()
	for {
		r, e := b.core.Current(ctx, ref)
		if e != nil {
			return r, e
		}
		if r.Actions == nil {
			return r, Error("INPUT_INVALIDATED")
		}
		if r.Actions.AnswerQualification != nil {
			return r, nil
		}
		if r.Work[0].Generation != initial.Work[0].Generation || r.Task.OwnerEpoch != initial.Task.OwnerEpoch || r.Task.Owner != initial.Task.Owner {
			return r, Error("INPUT_INVALIDATED")
		}
		if r.Task.State == "WAITING" {
			// Only reconciliation may run in a wait; it cannot authorize a new action.
			if len(r.Task.WaitingReasons) != 1 || r.Task.WaitingReasons[0] != "reconciliation" {
				return r, nil
			}
			recovered := false
			if e = b.charge(ctx, r, "reconcile"); e != nil {
				return r, e
			}
			for _, a := range r.Actions.Actions {
				if a.Status == "DISPATCHED" {
					if e = b.env.Recover(ctx, r.Task, a); e != nil {
						return r, e
					}
					recovered = true
					break
				}
			}
			if !recovered {
				return r, nil
			}
			r, e = b.core.Current(ctx, ref)
			if e != nil || r.Task.State == "WAITING" {
				return r, e
			}
			continue
		}
		if r.Task.State != "RUNNING" {
			return r, nil
		}
		q := tasks.QualificationOf(r)
		if ctx.Err() != nil {
			return r, ctx.Err()
		}
		// Recover a persisted dispatch before any retrieval or generation.
		recovered := false
		for _, a := range r.Actions.Actions {
			if a.Status == "DISPATCHED" {
				if e = b.charge(ctx, r, "reconcile"); e != nil {
					return b.core.Wait(ctx, q, "budget")
				}
				if e = b.env.Recover(ctx, r.Task, a); e != nil {
					return b.core.Wait(ctx, q, "reconciliation")
				}
				recovered = true
				break
			}
		}
		if recovered {
			current, e := b.core.Current(ctx, ref)
			if e != nil {
				return r, e
			}
			if current.Task.Version == r.Task.Version {
				return b.core.Wait(ctx, q, "reconciliation")
			}
			continue
		}
		assessment, calculated := Assessment{}, false
		if local, ok := b.env.(SnapshotAssessor); ok {
			assessment, calculated = local.AssessSnapshot(r)
		}
		if !calculated {
			if metered, ok := b.env.(MeteredAssessor); ok {
				assessment, e = metered.AssessMetered(ctx, r)
			} else {
				if e = b.charge(ctx, r, "goal"); e != nil {
					return b.core.Wait(ctx, q, "budget")
				}
				assessment, e = b.env.Assess(ctx, r)
			}
			if e != nil {
				return b.core.Wait(ctx, q, "unavailable")
			}
		}
		if assessment.ReadyForAnswer && (assessment.Satisfied || assessment.WrongWrite) {
			return r, Error("OUTPUT_INVALID")
		}
		if assessment.WrongWrite {
			return b.core.Finish(ctx, q, assessment.Evidence, true)
		}
		pending := false
		for _, a := range r.Actions.Actions {
			if a.Status == "READY" {
				pending = true
			}
		}
		if assessment.ReadyForAnswer && !pending {
			next, ok := b.core.(AnswerTransitionCore)
			if !ok {
				return r, Error("CAPABILITY_UNAVAILABLE")
			}
			if e = b.env.Validate(ctx, r.Task, b.model.Capabilities().Location); e != nil {
				return r, e
			}
			return next.PrepareAnswer(ctx, q)
		}
		if assessment.Satisfied && !pending {
			return b.core.Finish(ctx, q, assessment.Evidence, false)
		}
		if pending {
			// READY has not been dispatched. Invalid context must not turn a known
			// unsent operation into an apparent unknown external effect.
			if e = b.env.Validate(ctx, r.Task, b.model.Capabilities().Location); e != nil {
				return b.core.Wait(ctx, q, "input_invalidated")
			}
			a, e := b.core.Next(ctx, q)
			if e != nil {
				return r, e
			}
			if a.OperationID == "" {
				continue
			}
			if e = b.env.Execute(ctx, r.Task, a); e != nil {
				return b.core.Wait(ctx, q, "reconciliation")
			}
			continue
		}
		if len(r.Actions.Decisions) > 0 {
			d := r.Actions.Decisions[len(r.Actions.Decisions)-1]
			if d.Record == nil {
				return b.core.Wait(ctx, q, "model_unknown")
			}
			if !d.Admitted && d.Rejection == "" {
				if e = b.env.Validate(ctx, r.Task, b.model.Capabilities().Location); e != nil {
					return b.core.Wait(ctx, q, "input_invalidated")
				}
				if d.Record.Proposal.Kind == "wait" {
					return b.core.WaitInput(ctx, q, d.Number, d.Record.Proposal.Evidence)
				}
				_, e = b.core.Admit(ctx, q, d.Number)
				if e != nil && e.Error() != "PROPOSAL_REJECTED" {
					return r, e
				}
				continue
			}
		}
		cap := b.model.Capabilities()
		l := r.Actions.Limits
		if !cap.Text || !cap.Structured || !cap.HardBounds || cap.Location == "" || cap.InputUpper > l.InputTokens || l.InputTokens+l.OutputTokens > cap.ContextTokens {
			return r, Error("MODEL_LIMIT_UNSUPPORTED")
		}
		d, e := b.core.Reserve(ctx, q)
		if e != nil {
			return b.core.Wait(ctx, q, "budget")
		}
		record := tasks.DecisionRecord{}
		input, result, proposal, usage, reason := b.decide(ctx, r, d)
		record.Usage = usage
		record.Error = reason
		record.Proposal = proposal
		discarded := reason == "CONTEXT_INVALIDATED" || reason == "CONTEXT_DENIED"
		if !discarded {
			save, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
			record.Evidence, e = b.env.SaveDecision(save, r.Task, input, result)
			cancel()
			// Saving can race current revocation. Check under a separate budget even
			// when Save failed; an expired save deadline must not hide known usage.
			check, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			currentErr := b.env.Validate(check, r.Task, b.model.Capabilities().Location)
			cancel()
			if currentErr != nil {
				failure := contextFailure(currentErr)
				if failure == "CONTEXT_INVALIDATED" || failure == "CONTEXT_DENIED" {
					record.Error = failure
					discarded = true
					e = nil
				}
			}
		}
		if discarded {
			record.Evidence = ""
			record.Proposal = tasks.ActionProposal{}
		}
		if e != nil {
			return r, e
		}
		settle, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Second)
		e = b.core.Record(settle, q, d.Number, record)
		cancel()
		if e != nil {
			return r, e
		}
		if discarded {
			if _, ok := b.env.(ReassemblingContext); ok && record.Error == "CONTEXT_INVALIDATED" && usage.UnknownRequests == 0 {
				// Reserve on the next iteration spends the persisted correction budget.
				continue
			}
			wait := "input_invalidated"
			if usage.UnknownRequests != 0 {
				wait = "model_unknown"
			}
			return b.core.Wait(ctx, q, wait)
		}
		// Failed parsing/preparation remains a recorded failed first proposal.
		if reason != "" {
			continue
		}
	}
}
func (b *ActionBrain) charge(ctx context.Context, r tasks.RunSnapshot, kind string) error {
	current, e := b.core.Current(ctx, r.Task.Ref)
	if e != nil {
		return e
	}
	return b.core.ChargeQuery(ctx, tasks.QualificationOf(r), fmt.Sprintf("%d/%d/%s", r.Task.Version, len(current.Actions.Queries), kind))
}
func (b *ActionBrain) decide(ctx context.Context, r tasks.RunSnapshot, d tasks.ActionDecision) (input Input, result Result, proposal tasks.ActionProposal, usage tasks.GenerationUsage, reason string) {
	reason = "OUTPUT_INVALID"
	location := b.model.Capabilities().Location
	var e error
	refresh, ok := b.env.(ReassemblingContext)
	changed := false
	if len(r.Actions.Decisions) > 0 {
		prior := r.Actions.Decisions[len(r.Actions.Decisions)-1]
		changed = !prior.Admitted && prior.Record != nil && prior.Record.Evidence == "" && prior.Record.Error == "CONTEXT_INVALIDATED" && prior.Record.Usage.UnknownRequests == 0
	}
	if ok && changed {
		input, e = refresh.Reassemble(ctx, r.Task, location, MaxInputBytes)
	} else {
		input, e = b.env.Assemble(ctx, r.Task, location, MaxInputBytes)
	}
	if e != nil {
		reason = contextFailure(e)
		return
	}
	// Reserve already charged the model request/token allowance. Admission
	// performs no business-data observation; catalog and context reads retain
	// their own query charges, and current qualification is checked below.
	if e = b.charge(ctx, r, "catalog-search"); e != nil {
		reason = "QUERY_BUDGET_EXCEEDED"
		return
	}
	page, e := b.env.Search(ctx, r.Task, location)
	if e != nil {
		reason = "CATALOG_UNAVAILABLE"
		return
	}
	if page.Coverage != "COMPLETE" || len(page.Items) == 0 || len(page.Items) > 8 {
		reason = "CANDIDATE_OMISSION"
		return
	}
	candidates := map[string]catalog.Entry{}
	for _, match := range page.Items {
		if e = b.charge(ctx, r, "catalog-describe"); e != nil {
			reason = "QUERY_BUDGET_EXCEEDED"
			return
		}
		entry, e := b.env.Describe(ctx, r.Task, location, match.Ref)
		if e != nil {
			reason = "PROCESSING_DENIED"
			return
		}
		if entry.Ref != match.Ref || !entry.Available {
			reason = "CANDIDATE_OMISSION"
			return
		}
		candidates[entry.Ref.Digest] = entry
		raw, _ := json.Marshal(struct {
			Ref                    catalog.Ref
			Preconditions, Effects string
			Input                  json.RawMessage
		}{entry.Ref, entry.Preconditions, entry.Effects, entry.Capability.Input.Document})
		input.Blocks = append(input.Blocks, Block{Ref: entry.Ref.Digest, Text: string(raw), Role: "capability"})
	}
	// Failure codes and confirmed facts drive subsequent decisions. Original raw
	// response remains a controlled reference; no sensitive bytes enter task logs.
	state, _ := json.Marshal(struct {
		Reports   map[string]tasks.ExecutionReport
		Decisions []tasks.ActionDecision
	}{r.ExecutionReports, r.Actions.Decisions})
	input.Blocks = append(input.Blocks, Block{Ref: "run-facts", Text: string(state), Role: "runtime"})
	raw, e := json.Marshal(input)
	if e != nil || len(raw) > MaxInputBytes {
		reason = "INPUT_BUDGET_EXCEEDED"
		return
	}
	if e = b.env.Validate(ctx, r.Task, location); e != nil {
		reason = contextFailure(e)
		return
	}
	current, e := b.core.Current(ctx, r.Task.Ref)
	if e != nil || tasks.QualificationOf(current) != d.Qualification {
		reason = "INPUT_INVALIDATED"
		return
	}
	bounded, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	usage = tasks.GenerationUsage{Requests: 1, UnknownRequests: 1}
	result, e = b.generate(bounded, Request{Contract: ActionContract, Input: input, MaxInput: r.Actions.Limits.InputTokens, MaxOutput: r.Actions.Limits.OutputTokens})
	if result.Usage.Known && result.Usage.Input <= r.Actions.Limits.InputTokens && result.Usage.Output <= r.Actions.Limits.OutputTokens {
		usage.UnknownRequests = 0
		usage.Tokens = result.Usage.Input + result.Usage.Output
	}
	if result.Usage.Known && usage.UnknownRequests != 0 {
		reason = "MODEL_USAGE_INVALID"
		return
	}
	// Even malformed or failed responses cannot bypass current source checks.
	// Retain the already observed usage while discarding no-longer-authorized bytes.
	if currentErr := b.env.Validate(ctx, r.Task, location); currentErr != nil {
		reason = contextFailure(currentErr)
		return
	}
	if e != nil {
		reason = "MODEL_UNAVAILABLE"
		return
	}
	if result.Finish != "stop" {
		reason = "OUTPUT_TRUNCATED"
		return
	}
	// The response has just passed current source validation. Parsing is a
	// bounded local calculation, not another observation or disclosure boundary.
	parsed, e := ParseActions(result.Content)
	if e != nil {
		return
	}
	if parsed.Kind == "wait" {
		proposal.Kind = "wait"
		proposal.Evidence, e = b.env.SaveQuestion(ctx, r.Task, parsed.Reason)
		if e != nil {
			reason = "PROCESSING_DENIED"
			return
		}
		reason = ""
		return
	}
	proposal.Kind = "actions"
	for _, x := range parsed.Actions {
		entry, ok := candidates[x.Capability]
		if !ok {
			reason = "CANDIDATE_OMISSION"
			return
		}
		a, e := b.env.Prepare(ctx, r.Task, entry, x)
		if e != nil {
			reason = "INVALID_ARGUMENTS"
			return
		}
		proposal.Actions = append(proposal.Actions, a)
	}
	reason = ""
	return
}

// Timed-out implementations keep their actual request slot until they return.
// The Core reservation is already durable and cannot be silently redispatched.
func (b *ActionBrain) generate(ctx context.Context, in Request) (Result, error) {
	select {
	case b.modelSlots <- struct{}{}:
	default:
		return Result{}, Error("MODEL_UNAVAILABLE")
	}
	type reply struct {
		r Result
		e error
	}
	done := make(chan reply, 1)
	go func() {
		defer func() {
			<-b.modelSlots
			if recover() != nil {
				done <- reply{e: Error("MODEL_UNAVAILABLE")}
			}
		}()
		r, e := b.model.Generate(ctx, in)
		done <- reply{r, e}
	}()
	select {
	case out := <-done:
		return out.r, out.e
	case <-ctx.Done():
		return Result{}, Error("GENERATION_CANCELLED")
	}
}

func ParseActions(raw []byte) (ActionOutput, error) {
	var out ActionOutput
	if len(raw) > MaxAnswerBytes {
		return out, Error("OUTPUT_INVALID")
	}
	v, e := jsonvalue.Decode(raw)
	if e != nil {
		return out, Error("OUTPUT_INVALID")
	}
	obj, ok := v.(map[string]any)
	if !ok || len(obj) != 3 || obj["kind"] == nil || obj["reason"] == nil || obj["actions"] == nil {
		return out, Error("OUTPUT_INVALID")
	}
	items, ok := obj["actions"].([]any)
	if !ok {
		return out, Error("OUTPUT_INVALID")
	}
	for _, item := range items {
		row, ok := item.(map[string]any)
		if !ok || len(row) != 5 {
			return out, Error("OUTPUT_INVALID")
		}
		for _, key := range []string{"key", "capability", "depends_on", "arguments", "resource_version"} {
			if row[key] == nil {
				return out, Error("OUTPUT_INVALID")
			}
		}
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&out) != nil || dec.Decode(new(any)) != io.EOF || len(out.Reason) > 256 {
		return out, Error("OUTPUT_INVALID")
	}
	if out.Kind == "wait" && len(out.Actions) == 0 && out.Reason != "" {
		return out, nil
	}
	if out.Kind != "actions" || out.Reason != "" || len(out.Actions) == 0 || len(out.Actions) > 8 {
		return out, Error("OUTPUT_INVALID")
	}
	for _, x := range out.Actions {
		if len(x.Key) == 0 || len(x.Key) > 128 || len(x.Capability) != 64 || len(x.DependsOn) > 8 || len(x.Arguments) > 8192 || x.ResourceVersion == 0 {
			return out, Error("OUTPUT_INVALID")
		}
	}
	return out, nil
}

// Translate only explicit context invalidation/denial into metadata-only
// settlement. Availability and arbitrary host errors are not proof of revocation.
func contextFailure(err error) string {
	switch err.Error() {
	case "CONTEXT_INVALIDATED", "INPUT_INVALIDATED":
		return "CONTEXT_INVALIDATED"
	case "PERMISSION_DENIED", "PROCESSING_DENIED":
		return "CONTEXT_DENIED"
	default:
		return "PROCESSING_DENIED"
	}
}
