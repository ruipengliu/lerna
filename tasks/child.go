package tasks

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"reflect"
	"unicode/utf8"
)

type ChildIntent struct {
	Parent        Ref
	ParentOwner   string
	ParentEpoch   uint64
	Depth         int
	Ancestors     []Ref
	Spec          ChildSpec
	GrantMaterial string
}

// ChildReference carries only association metadata. Cancellation and queries
// never need to retransmit input bytes, goals, schemas or grant material.
type ChildReference struct {
	Parent                     Ref
	ParentOwner                string
	ParentEpoch                uint64
	OperationID, Agent, Digest string
}

func ChildReferenceOf(in ChildIntent) ChildReference {
	return ChildReference{in.Parent, in.ParentOwner, in.ParentEpoch, in.Spec.OperationID, in.Spec.Agent, DelegationDigest(in)}
}
func (in ChildReference) id() string {
	b, _ := json.Marshal(struct{ Namespace, Owner, Operation string }{in.Parent.Namespace, in.ParentOwner, in.OperationID})
	return fmt.Sprintf("child-%x", sha256.Sum256(b))
}

type ChildReport struct {
	Parent, Child                      Ref
	OperationID, Digest, Owner, State  string
	Version, TaskVersion               uint64
	Result                             []byte
	Evidence, Source, Coverage, Effect string
	Unresolved                         []string
	Used                               Constraints
	UsageKnown                         bool
}

// ChildEvidence is produced by the trusted child host from visible artifacts
// and effect observations. The parent still performs its own assessment.
type ChildEvidence struct {
	Evidence, Source, Coverage, Effect string
	Unresolved                         []string
}

// ChildPolicy checks accept, execute, delegate or disclose under current policy.
// A delegate check must additionally authorize onward delegation.
type ChildPolicy func(context.Context, ChildIntent, string) error
type ChildPort struct {
	routeKeys          map[string]ed25519.PublicKey
	routePolicy        ParentRoutePolicy
	peer               *authorization.GrantPresentation
	service            *Service
	token, parentOwner string
	policy             ChildPolicy
	inspect            func(context.Context, RunSnapshot) (ChildEvidence, error)
	newOperation       func(context.Context) (string, error)
	controls           *ControlService
	limits             DelegationLimits
}

func (s *Service) Children(token, parentOwner string, policy ChildPolicy, inspect func(context.Context, RunSnapshot) (ChildEvidence, error), newOperation func(context.Context) (string, error), limits DelegationLimits, controls ControlLimits) (*ChildPort, error) {
	if token == "" || !name(parentOwner) || policy == nil || inspect == nil || newOperation == nil || !limits.valid() {
		return nil, failure(authorization.Invalid)
	}
	owned := *s
	owned.childPolicy = policy
	control, e := owned.Controls(controls)
	if e != nil {
		return nil, e
	}
	return &ChildPort{service: &owned, token: token, parentOwner: parentOwner, policy: policy, inspect: inspect, newOperation: newOperation, controls: control, limits: limits}, nil
}
func childID(in ChildIntent) string { return ChildReferenceOf(in).id() }
func (c *ChildPort) valid(in ChildIntent) bool {
	if in.Parent.Namespace != c.service.config.Namespace || !name(in.Parent.TaskID) || in.ParentOwner != c.parentOwner || in.ParentEpoch == 0 || in.Depth < 1 || in.Depth > c.limits.MaxDepth || len(in.Ancestors) != in.Depth || in.Spec.Agent != c.service.config.Owner || len(in.GrantMaterial) == 0 || len(in.GrantMaterial) > 32768 {
		return false
	}
	seen := map[Ref]bool{}
	for _, r := range in.Ancestors {
		if seen[r] || r.TaskID == childID(in) || r.Namespace != in.Parent.Namespace {
			return false
		}
		seen[r] = true
	}
	return in.Ancestors[len(in.Ancestors)-1] == in.Parent
}
func (c *ChildPort) Accept(ctx context.Context, in ChildIntent) (Task, error) {
	ctx, cancel := context.WithTimeout(ctx, c.limits.IOTimeout)
	defer cancel()
	if len(in.Spec.DependsOn) == 0 {
		in.Spec.DependsOn = nil
	}
	if len(in.Spec.Input) == 0 || !c.valid(in) || !validInput(Submission{Namespace: in.Parent.Namespace, OperationID: in.Spec.OperationID, Goal: in.Spec.Goal, Constraints: in.Spec.Budget}) {
		return Task{}, failure(authorization.Invalid)
	}
	if e := delegationSchema(in.Spec.InputSchema, in.Spec.Input); e != nil {
		return Task{}, e
	}
	if e := delegationSchema(in.Spec.ResultSchema, nil); e != nil {
		return Task{}, e
	}
	if e := c.policy(ctx, in, "accept"); e != nil {
		return Task{}, e
	}
	cancellation, e := c.newOperation(ctx)
	if e != nil {
		return Task{}, e
	}
	var out Task
	var replay RunSnapshot
	e = c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		identity, e := tx.Authorize(c.token, c.service.config.Resource, "task.submit")
		if e != nil {
			return e
		}
		id := childID(in)
		if _, closed := j.ClosedDelegations[id]; closed {
			return failure(authorization.Denied)
		}
		if old, ok := j.Runs[id]; ok {
			if old.Parent == nil || !reflect.DeepEqual(*old.Parent, in) {
				return failure(authorization.IdentityConflict)
			}
			if old.Task.Subject != identity.Subject {
				return failure(authorization.Denied)
			}
			if e := c.matchParentRoute(old); e != nil {
				return e
			}
			replay = old
			out = old.Task
			return nil
		}
		if in.Spec.Budget.DeadlineUnix <= tx.Now().Unix() {
			return failure(authorization.Expired)
		}
		task := Task{Ref: Ref{in.Parent.Namespace, id}, Goal: in.Spec.Goal, Constraints: in.Spec.Budget, State: "QUEUED", Subject: identity.Subject, Resource: c.service.config.Resource, Owner: c.service.config.Owner, OwnerEpoch: 1, Version: 1}
		_, e = c.service.commit(j, RunChange{ChangeID: id, MustNotExist: true, Task: task, Work: []Work{{ID: id + "/initial", Kind: "decide"}}, Records: []Record{{Kind: "submitted", Version: 1}}})
		if e != nil {
			return e
		}
		r := j.Runs[id]
		r.Parent = &in
		r.ParentCancelOperation = cancellation
		j.Runs[id] = r
		out = task
		return nil
	})
	if e == nil && replay.ParentRoute != nil {
		e = c.routePolicy(ctx, *replay.Parent, *c.peer)
	}
	if e != nil {
		return Task{}, e
	}
	return out, e
}
func (c *ChildPort) lookup(ctx context.Context, in ChildReference) (RunSnapshot, error) {
	if in.Parent.Namespace != c.service.config.Namespace || in.ParentOwner != c.parentOwner || in.Agent != c.service.config.Owner || !name(in.Parent.TaskID) || in.ParentEpoch == 0 || len(in.OperationID) == 0 || len(in.OperationID) > 512 || len(in.Digest) != 64 {
		return RunSnapshot{}, failure(authorization.Denied)
	}
	digest, e := hex.DecodeString(in.Digest)
	if e != nil || len(digest) != 32 || hex.EncodeToString(digest) != in.Digest {
		return RunSnapshot{}, failure(authorization.Invalid)
	}
	ref := Ref{in.Parent.Namespace, in.id()}
	if _, e := c.service.Get(ctx, c.token, ref); e != nil {
		return RunSnapshot{}, e
	}
	r, e := c.service.Load(ctx, ref)
	if e != nil {
		return r, e
	}
	if r.ParentRoute != nil {
		if e := c.matchParentRoute(r); e != nil {
			return r, e
		}
		if e = c.routePolicy(ctx, *r.Parent, *c.peer); e != nil {
			return r, e
		}
	}
	if r.Parent == nil || ChildReferenceOf(*r.Parent) != in {
		return r, failure(authorization.IdentityConflict)
	}
	return r, nil
}
func (c *ChildPort) Lookup(ctx context.Context, in ChildReference) (Task, error) {
	ctx, cancel := context.WithTimeout(ctx, c.limits.IOTimeout)
	defer cancel()
	r, e := c.lookup(ctx, in)
	return r.Task, e
}
func (c *ChildPort) Cancel(ctx context.Context, in ChildReference) (ControlReceipt, error) {
	ctx, cancel := context.WithTimeout(ctx, c.limits.IOTimeout)
	defer cancel()
	r, e := c.lookup(ctx, in)
	if authorization.Is(e, authorization.NotFound) {
		e = c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
			if _, e := tx.Authorize(c.token, c.service.config.Resource, "task.cancel"); e != nil {
				return e
			}
			id := in.id()
			if _, ok := j.Runs[id]; ok {
				return failure(authorization.Conflict)
			}
			if old, ok := j.ClosedDelegations[id]; ok {
				if !reflect.DeepEqual(old, in) {
					return failure(authorization.IdentityConflict)
				}
				return nil
			}
			if len(j.ClosedDelegations) >= c.service.config.MaxTasks {
				return failure(authorization.Unavailable)
			}
			j.ClosedDelegations[id] = in
			return nil
		})
		return ControlReceipt{Ref: Ref{in.Parent.Namespace, in.id()}, Intent: "CANCEL", Outcome: "APPLIED"}, e
	}
	if e != nil {
		return ControlReceipt{}, e
	}
	old, e := c.controls.Lookup(ctx, c.token, in.Parent.Namespace, r.ParentCancelOperation)
	if e == nil {
		return old, nil
	}
	if !authorization.Is(e, authorization.NotFound) {
		return ControlReceipt{}, e
	}
	return c.controls.Request(ctx, c.token, ControlRequest{OperationID: r.ParentCancelOperation, Ref: r.Task.Ref, ExpectedVersion: r.Task.Version, Intent: "CANCEL"})
}
func (c *ChildPort) Report(ctx context.Context, ref ChildReference) (ChildReport, error) {
	ctx, cancel := context.WithTimeout(ctx, c.limits.IOTimeout)
	defer cancel()
	r, e := c.lookup(ctx, ref)
	if e != nil {
		return ChildReport{}, e
	}
	in := *r.Parent
	if e := c.policy(ctx, in, "disclose"); e != nil {
		return ChildReport{}, e
	}
	evidence, e := c.inspect(ctx, r)
	if e != nil {
		return ChildReport{}, e
	}
	if !validChildEvidence(evidence) {
		return ChildReport{}, failure(authorization.Invalid)
	}
	var result []byte
	if r.Task.Result != "" {
		result = []byte(r.Task.Result)
	}
	if len(evidence.Unresolved) == 0 {
		evidence.Unresolved = nil
	}
	if r.Task.State == "COMPLETED" {
		if e = delegationSchema(in.Spec.ResultSchema, result); e != nil {
			return ChildReport{}, e
		}
	}
	steps := r.Task.Attempts + r.Task.DelegatedSteps
	if r.Actions != nil && uint32(len(r.Actions.Actions))+r.Task.DelegatedSteps > steps {
		steps = uint32(len(r.Actions.Actions)) + r.Task.DelegatedSteps
	}
	report := ChildReport{Parent: in.Parent, Child: r.Task.Ref, OperationID: in.Spec.OperationID, Digest: specDigest(in.Spec), Owner: r.Task.Owner, State: r.Task.State, Version: r.Task.Version, TaskVersion: r.Task.Version, Result: result, Evidence: evidence.Evidence, Source: evidence.Source, Coverage: evidence.Coverage, Effect: evidence.Effect, Unresolved: evidence.Unresolved, Used: Constraints{MaxSteps: steps, ModelRequests: r.Task.ModelUsedRequests + r.Task.DelegatedRequests, ModelTokens: r.Task.ModelUsedTokens + r.Task.DelegatedTokens}, UsageKnown: terminal(r.Task) && r.Task.ModelReservedRequests == 0 && r.Task.ModelReservedTokens == 0}
	e = c.service.transaction(ctx, func(j *journal, tx authorization.RuntimeTransaction) error {
		current, ok := j.Runs[r.Task.Ref.TaskID]
		if !ok || current.Task.Version != r.Task.Version {
			return failure(authorization.Conflict)
		}
		if _, e := tx.Authorize(c.token, current.Task.Resource, "task.read"); e != nil {
			return e
		}
		if old := current.ChildReport; old != nil {
			comparable := report
			comparable.Version = old.Version
			if reflect.DeepEqual(comparable, *old) {
				report = *old
				return nil
			}
			if report.Version <= old.Version {
				report.Version = old.Version + 1
			}
		}
		changes := &current.ChildReportChanges
		if controlIntent(current.Task) == "CANCEL" {
			changes = &current.ChildDispositionReportChanges
		}
		if *changes >= uint32(c.limits.MaxChecks) {
			return failure(authorization.Unavailable)
		}
		current.ChildReport = &report
		*changes++
		j.Runs[r.Task.Ref.TaskID] = current
		return nil
	})
	return report, e

}
func (c *ChildPort) BindWorker(b WorkerBinding, l RunLimits) (*WorkPort, error) {
	return c.service.BindWorker(b, l)
}

// DelegationDigest binds signed authority to the complete immutable handoff,
// including its parent incarnation, recipient, ancestry, data and budget.
func DelegationDigest(in ChildIntent) string {
	if len(in.Spec.DependsOn) == 0 {
		in.Spec.DependsOn = nil
	}
	in.GrantMaterial = ""
	b, _ := json.Marshal(in)
	return fmt.Sprintf("%x", sha256.Sum256(b))
}

// SignedChildPolicy rechecks a current, operation-bound narrowed grant at each
// child start and result disclosure. Presentation and action come from the
// authenticated host, never the proposed child input. Separate grant issuance
// uses GrantAuthority.Mutate(DERIVE) with a durable original mutation request.
func SignedChildPolicy(g *authorization.GrantAuthority, presentation authorization.GrantPresentation, action *wire.AuthorizationAction, owner string) ChildPolicy {
	return func(ctx context.Context, in ChildIntent, boundary string) error {
		if boundary == "delegate" {
			return failure(authorization.Denied)
		} // This binding issues leaf grants.
		if g == nil || action == nil || owner != in.Spec.Agent {
			return failure(authorization.Denied)
		}
		p := presentation
		p.OperationID = in.Spec.OperationID
		p.SemanticSHA256 = DelegationDigest(in)
		record, e := g.Verify(ctx, in.GrantMaterial, p, action)
		if e != nil {
			return e
		}
		if record.Parent == "" || record.Spec.Mode != "single" || record.Spec.Scope.ExpiresUnix > in.Spec.Budget.DeadlineUnix {
			return failure(authorization.Denied)
		}
		return nil
	}
}

// BindPeer is trusted assembly: it maps a logical Task Owner to one exact
// authenticated transport peer, without conflating owner names with node IDs.
func (c *ChildPort) BindPeer(p authorization.GrantPresentation) (*ChildPort, error) {
	if p.Namespace != c.service.config.Namespace || p.Subject == "" || p.Presenter == "" || p.Audience == "" || p.CertificateSHA256 == "" {
		return nil, failure(authorization.Invalid)
	}
	copy := *c
	copy.peer = &p
	return &copy, nil
}
func (c *ChildPort) MatchPeer(p authorization.GrantPresentation) error {
	if c.peer == nil || *c.peer != p {
		return failure(authorization.Denied)
	}
	return nil
}

func validChildEvidence(e ChildEvidence) bool {
	if len(e.Evidence) > 1024 || len(e.Source) > 1024 || len(e.Coverage) > 1024 || len(e.Unresolved) > 16 {
		return false
	}
	if e.Effect != "NONE" && e.Effect != "CONFIRMED" && e.Effect != "UNKNOWN" {
		return false
	}
	for _, v := range append([]string{e.Evidence, e.Source, e.Coverage}, e.Unresolved...) {
		if len(v) > 1024 || !utf8.ValidString(v) {
			return false
		}
	}
	return true
}

func (c *ChildPort) matchParentRoute(r RunSnapshot) error {
	if r.ParentRoute != nil && (r.Parent == nil || c.peer == nil || *c.peer != r.ParentRoute.Peer || c.routePolicy == nil) {
		return failure(authorization.Denied)
	}
	return nil
}
