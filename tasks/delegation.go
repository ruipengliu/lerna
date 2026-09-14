package tasks

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"time"

	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/schema"
)

type DelegationLimits struct {
	MaxChildren, MaxDepth, MaxConcurrent, MaxChecks int
	IOTimeout                                       time.Duration
}

func (l DelegationLimits) valid() bool {
	return l.MaxChildren >= 1 && l.MaxChildren <= 16 && l.MaxDepth >= 1 && l.MaxDepth <= 8 && l.MaxConcurrent >= 1 && l.MaxConcurrent <= 4 && l.MaxConcurrent <= l.MaxChildren && l.MaxChecks >= 1 && l.MaxChecks <= 64 && l.IOTimeout >= time.Millisecond && l.IOTimeout <= 5*time.Second
}

type ChildSpec struct {
	Key, OperationID, Agent, Goal, Acceptance string
	Input, InputSchema, ResultSchema          []byte
	DependsOn                                 []string
	Required                                  bool
	Budget                                    Constraints
}
type DelegationProposal struct {
	OperationID string
	Children    []ChildSpec
}
type ChildLink struct {
	Intent            *ChildIntent
	Settled           bool
	Spec              ChildSpec
	Child             Ref
	Status            string
	Attempts, Checks  uint32
	DispositionChecks uint32
	Reports           []ChildReport
	Report            *ChildReport
}

// ponytail: one finite batch per task; add a bounded batch journal only when a
// task needs successive delegation rounds. Dependencies cover this batch.
type DelegationState struct {
	ApprovedAt        uint64
	DispositionChecks uint32
	ResultsUpdate     *UpdateRequest
	ResultsReceipt    *InputReceipt
	Checks            uint32
	Next              int
	Limits            DelegationLimits
	Proposal          DelegationProposal
	Qualification     Qualification
	Children          []ChildLink
	Approved          bool
	Evidence          string
}

// DelegationPolicy checks current processing and retention conditions for the
// exact input, or (when report is non-nil) for the incoming result. It is host
// assembly, never supplied by a model or remote peer.
type DelegationPolicy func(context.Context, Task, ChildSpec, *ChildReport) error
type DelegationPort struct {
	policy DelegationPolicy
	*WorkPort
	limits DelegationLimits
}

func (p *WorkPort) Delegations(l DelegationLimits, policy DelegationPolicy) (*DelegationPort, error) {
	if p == nil || !l.valid() || policy == nil {
		return nil, failure(authorization.Invalid)
	}
	return &DelegationPort{WorkPort: p, limits: l, policy: policy}, nil
}
func specDigest(s ChildSpec) string {
	if len(s.DependsOn) == 0 {
		s.DependsOn = nil
	}
	b, _ := json.Marshal(s)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}
func delegationSchema(document, data []byte) error {
	if len(document) == 0 || len(document) > 8192 || len(data) > 16384 {
		return failure(authorization.Invalid)
	}
	var header struct {
		ID string `json:"$id"`
	}
	if json.Unmarshal(document, &header) != nil {
		return failure(authorization.Invalid)
	}
	registry, e := schema.New([]schema.Resource{{Type: "delegation", ID: header.ID, Version: "1", Document: document}})
	if e != nil {
		return failure(authorization.Invalid)
	}
	if data == nil {
		return nil
	}
	return registry.Validate(&wire.DynamicPayload{TypeName: "delegation", SchemaId: header.ID, SchemaVersion: "1", SchemaDigest: schema.Digest(document), Json: data})
}
func (p *DelegationPort) current(j *journal, tx authorization.RuntimeTransaction, ref Ref) (RunSnapshot, error) {
	r, ok := j.Runs[ref.TaskID]
	if !ok || ref.Namespace != p.service.config.Namespace {
		return r, failure(authorization.NotFound)
	}
	id, e := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute")
	if e != nil {
		id, e = tx.Authorize(p.binding.Token, r.Task.Resource, "task.reconcile")
	}
	if e != nil {
		return r, e
	}
	if id.Subject != p.binding.Subject || id.Subject != r.Task.Subject || r.Task.Owner != p.service.config.Owner {
		return r, failure(authorization.Denied)
	}
	if r.Delegations != nil && r.Delegations.Limits != p.limits {
		return r, failure(authorization.IdentityConflict)
	}
	return r, nil
}
func (p *DelegationPort) Admit(ctx context.Context, q Qualification, proposal DelegationProposal) (RunSnapshot, error) {
	if e := p.checkChildBoundary(ctx, q.Ref, "delegate"); e != nil {
		return RunSnapshot{}, e
	}
	if len(proposal.Children) == 0 || len(proposal.Children) > p.limits.MaxChildren || proposal.OperationID == "" {
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	proposal.Children = append([]ChildSpec(nil), proposal.Children...)
	for i := range proposal.Children {
		if len(proposal.Children[i].DependsOn) == 0 {
			proposal.Children[i].DependsOn = nil
		}
	}
	keys := map[string]int{}
	ops := map[string]bool{proposal.OperationID: true}
	for i, c := range proposal.Children {
		if len(c.Input) == 0 || !name(c.Key) || !name(c.Agent) || c.OperationID == "" || len(c.OperationID) > 512 || ops[c.OperationID] || len(c.Acceptance) == 0 || len(c.Acceptance) > 1024 || len(c.DependsOn) > p.limits.MaxChildren || !validInput(Submission{Namespace: q.Ref.Namespace, OperationID: c.OperationID, Goal: c.Goal, Constraints: c.Budget}) {
			return RunSnapshot{}, failure(authorization.Invalid)
		}
		if _, ok := keys[c.Key]; ok {
			return RunSnapshot{}, failure(authorization.Invalid)
		}
		keys[c.Key] = i
		ops[c.OperationID] = true
		if e := delegationSchema(c.InputSchema, c.Input); e != nil {
			return RunSnapshot{}, e
		}
		if e := delegationSchema(c.ResultSchema, nil); e != nil {
			return RunSnapshot{}, e
		}
	}
	visiting := map[string]bool{}
	done := map[string]bool{}
	var visit func(string) bool
	visit = func(key string) bool {
		if done[key] {
			return true
		}
		i, ok := keys[key]
		if !ok || visiting[key] {
			return false
		}
		visiting[key] = true
		for _, dep := range proposal.Children[i].DependsOn {
			if !visit(dep) {
				return false
			}
		}
		visiting[key] = false
		done[key] = true
		return true
	}
	for key := range keys {
		if !visit(key) {
			return RunSnapshot{}, failure(authorization.Invalid)
		}
	}

	ctx, cancel := context.WithTimeout(ctx, p.limits.IOTimeout)
	defer cancel()
	original, e := p.snapshot(ctx, q.Ref)
	if e != nil {
		return RunSnapshot{}, e
	}
	for _, c := range proposal.Children {
		if e = p.policy(ctx, original.Task, c, nil); e != nil {
			return RunSnapshot{}, e
		}
	}
	var out RunSnapshot
	e = p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		r, e := p.current(j, tx, q.Ref)
		if e != nil {
			return e
		}
		if _, e := tx.Authorize(p.binding.Token, r.Task.Resource, "task.execute"); e != nil {
			return e
		}
		if r.Delegations != nil {
			if r.Delegations.Qualification != q || !reflect.DeepEqual(r.Delegations.Proposal, proposal) {
				return failure(authorization.IdentityConflict)
			}
			out = r
			return nil
		}
		if QualificationOf(r) != q || r.Task.State != "RUNNING" || controlIntent(r.Task) != "RUN" || len(r.Work) != 1 || !r.Work[0].InFlight || r.Work[0].Worker != p.binding.WorkerID || r.Work[0].LeaseUntil <= tx.Now().UnixNano() {
			return failure(authorization.Conflict)
		}
		depth := 0
		if r.Parent != nil {
			depth = r.Parent.Depth
		}
		if depth >= p.limits.MaxDepth {
			return failure(authorization.Denied)
		}

		for _, op := range append([]string{proposal.OperationID}, delegationOperations(proposal)...) {
			if delegationOperation(j, op) {
				return failure(authorization.IdentityConflict)
			}
			if _, ok := j.Operations[op]; ok {
				return failure(authorization.IdentityConflict)
			}
			if _, ok := j.Controls[op]; ok {
				return failure(authorization.IdentityConflict)
			}
			if _, ok := j.InputChanges[op]; ok {
				return failure(authorization.IdentityConflict)
			}
		}
		if e = tx.Operation(proposal.OperationID, p.binding.Subject, true); e != nil {
			return e
		}
		state := &DelegationState{Limits: p.limits, Proposal: proposal, Qualification: q}
		for _, c := range proposal.Children {
			if c.Budget.DeadlineUnix > r.Task.Constraints.DeadlineUnix || c.Budget.DeadlineUnix <= tx.Now().Unix() {
				return failure(authorization.Denied)
			}
			if e = tx.Operation(c.OperationID, p.binding.Subject, true); e != nil {
				return e
			}
			r.Task.DelegatedSteps += c.Budget.MaxSteps
			r.Task.DelegatedRequests += c.Budget.ModelRequests
			r.Task.DelegatedTokens += c.Budget.ModelTokens
			state.Children = append(state.Children, ChildLink{Spec: c, Status: "QUEUED"})
		}
		r.Delegations = state
		r.Task.Version++
		j.Runs[q.Ref.TaskID] = r
		out = r
		return nil
	})
	return out, e
}
func delegationPending(r RunSnapshot) bool {
	if r.Delegations == nil {
		return false
	}
	for _, c := range r.Delegations.Children {
		if !childDisposed(c) {
			return true
		}
	}
	return false
}
func delegationInvariant(r RunSnapshot) error {
	if r.Parent != nil {
		b := r.Parent.Spec.Budget
		c := r.Task.Constraints
		if c.MaxSteps > b.MaxSteps || c.ModelRequests > b.ModelRequests || c.ModelTokens > b.ModelTokens || c.DeadlineUnix > b.DeadlineUnix {
			return failure(authorization.Denied)
		}
	}
	if r.Delegations == nil {
		return nil
	}
	steps := r.Task.Attempts
	if r.Actions != nil && uint32(len(r.Actions.Actions)) > steps {
		steps = uint32(len(r.Actions.Actions))
	}
	if uint64(steps)+uint64(r.Task.DelegatedSteps) > uint64(r.Task.Constraints.MaxSteps) || uint64(r.Task.ModelUsedRequests)+uint64(r.Task.ModelReservedRequests)+uint64(r.Task.DelegatedRequests) > uint64(r.Task.Constraints.ModelRequests) || r.Task.ModelUsedTokens+r.Task.ModelReservedTokens+r.Task.DelegatedTokens > r.Task.Constraints.ModelTokens {
		return failure(authorization.Unavailable)
	}
	if r.Delegations != nil && terminal(r.Task) && (delegationPending(r) || r.Task.State == "COMPLETED" && (!r.Delegations.Approved || r.UpdateVersion > r.Delegations.ApprovedAt)) {
		return failure(authorization.Conflict)
	}
	return nil
}

func delegationOperations(p DelegationProposal) []string {
	v := make([]string, 0, len(p.Children))
	for _, c := range p.Children {
		v = append(v, c.OperationID)
	}
	return v
}
func delegationOperation(j *journal, op string) bool {
	for _, r := range j.Runs {
		if r.Delegations != nil {
			if r.Delegations.Proposal.OperationID == op {
				return true
			}
			for _, c := range r.Delegations.Children {
				if c.Spec.OperationID == op {
					return true
				}
			}
		}
	}
	return false
}

func (p *DelegationPort) snapshot(ctx context.Context, ref Ref) (RunSnapshot, error) {
	var out RunSnapshot
	e := p.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		var e error
		out, e = p.current(j, tx, ref)
		return e
	})
	return out, e
}
