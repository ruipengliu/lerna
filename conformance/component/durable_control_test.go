//go:build integration

package component_test

import (
	"context"
	"errors"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestDurableCancelBeforeDecideSurvivesBothOwnersReopen(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	request := scene.Request
	request.Target.ID = "first-cancelled-decision"
	request.Payload.DecisionID = request.Target.ID
	digest, err := v.DecisionInputDigest(request, scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	until, err := time.Parse("2006-01-02T15:04:05.000000Z", string(request.AcceptBefore))
	if err != nil {
		t.Fatal(err)
	}
	access := decision.ControlAccess{Subject: scene.Subject, DecisionOwner: v.OwnerRef{TenantID: request.Target.TenantID, OwnerID: request.Target.OwnerID}, DecisionRef: &request.Target, Purposes: []string{"cancel", "get"}, ValidUntil: until}
	if err = world.Source().SeedControlAccess(ctx, access); err != nil {
		t.Fatal(err)
	}
	basis, err := world.Source().IssueControl(ctx, fixture.ControlClaim{Subject: scene.Subject, DecisionRef: request.Target, TaskRef: request.Payload.TaskRef, InputDigest: v.SchemaDigest(digest), ControlRevision: "2", ValidUntil: request.AcceptBefore})
	if err != nil {
		t.Fatal(err)
	}
	control := v.DecisionCancelRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "cancel-before-decide", Target: request.Target, Method: "decision_engine.cancel", AcceptBefore: request.AcceptBefore, Payload: v.DecisionCancelPayload{DecisionRef: request.Target, TaskRef: request.Payload.TaskRef, DecisionInputDigest: v.SchemaDigest(digest), ControlBasis: basis, Reason: "stop before Snapshot admission"}}
	raw, err := v.Encode(control)
	if err != nil {
		t.Fatal(err)
	}
	service := world.Service()
	result, err := service.Cancel(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := result.AsReceived()
	if !ok {
		t.Fatal("cancel receipt not durably received")
	}
	applied, ok := received.Receipt.AsApplied()
	if !ok {
		t.Fatal("valid cancel before decide not applied")
	}
	if applied.ObjectRef.ID != request.Target.ID {
		t.Fatal("cancel receipt changed original identity")
	}
	world.Reopen(ctx)
	service = world.Service()
	getJSON, err := v.Encode(v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "read-closed", Target: request.Target, Method: "decision_engine.get", AcceptBefore: request.AcceptBefore, Payload: v.DecisionGetPayload{DecisionRef: request.Target}})
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, getJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("reopened control-only Decision unavailable")
	}
	closed, ok := found.Decision.AsCancelled()
	if !ok {
		t.Fatal("reopened tombstone was not cancelled")
	}
	if closed.Input != nil || closed.InputDigest != v.SchemaDigest(digest) || closed.TaskRef != request.Payload.TaskRef || closed.Usage.RuleStarts != "0" {
		t.Fatal("pre-admission close invented input or execution")
	}
	if found.CurrentControl == nil || found.CurrentControl.Scope != "local_decision_work" || found.CurrentControl.TaskRef != request.Payload.TaskRef || found.CurrentControl.DecisionInputDigest != v.SchemaDigest(digest) {
		t.Fatal("reopened public get lost original stopping scope")
	}
	wantBasis, err := v.Encode(basis)
	if err != nil {
		t.Fatal(err)
	}
	actualBasis, err := v.Encode(found.CurrentControl.ControlBasis)
	if err != nil {
		t.Fatal(err)
	}
	if string(wantBasis) != string(actualBasis) {
		t.Fatal("reopened public control changed adopted proof")
	}
	late := world.AdditionalScenario(ctx, request.Target.OwnerID, request.Target.ID, until)
	late.Request.CommandID = "late-decide"
	lateJSON, err := v.Encode(late.Request)
	if err != nil {
		t.Fatal(err)
	}
	decisionResult, err := service.Decide(ctx, lateJSON, &late.Subject)
	if err != nil {
		t.Fatal(err)
	}
	lateReceived, ok := decisionResult.AsReceived()
	if !ok {
		t.Fatal("late decide receipt unavailable")
	}
	refused, ok := lateReceived.Receipt.AsRejected()
	if !ok || refused.Reason != "decision_cancelled" {
		t.Fatal("late identical input resurrected cancelled Decision")
	}
	step, err := service.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if step.Processed != 0 {
		t.Fatal("cancel-before-decide created runnable work")
	}
	pool, err := service.ObservePool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pool.Queued["ordinary"] != 0 || pool.Active["ordinary"] != 0 {
		t.Fatal("closed binding retained new execution responsibility")
	}
	world.Reopen(ctx)
	result, err = world.Service().Cancel(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	replay, ok := result.AsReceived()
	if !ok {
		t.Fatal("fixed cancel replay unavailable")
	}
	want, err := v.Encode(received.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	got, err := v.Encode(replay.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != string(got) {
		t.Fatal("reopen changed original applied receipt")
	}
}

func sceneControl(t *testing.T, ctx context.Context, w *fixture.World, scene fixture.Scenario, revision v.Revision, command v.ID) v.DecisionCancelRequest {
	t.Helper()
	digest, err := v.DecisionInputDigest(scene.Request, scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	until, err := time.Parse("2006-01-02T15:04:05.000000Z", string(scene.Request.AcceptBefore))
	if err != nil {
		t.Fatal(err)
	}
	ref := scene.DecisionRef
	if err = w.Source().SeedControlAccess(ctx, decision.ControlAccess{Subject: scene.Subject, DecisionOwner: v.OwnerRef{TenantID: ref.TenantID, OwnerID: ref.OwnerID}, DecisionRef: &ref, Purposes: []string{"cancel", "get"}, ValidUntil: until}); err != nil {
		t.Fatal(err)
	}
	basis, err := w.Source().IssueControl(ctx, fixture.ControlClaim{Subject: scene.Subject, DecisionRef: ref, TaskRef: scene.Request.Payload.TaskRef, InputDigest: v.SchemaDigest(digest), ControlRevision: revision, ValidUntil: scene.Request.AcceptBefore})
	if err != nil {
		t.Fatal(err)
	}
	return v.DecisionCancelRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: command, Target: ref, Method: "decision_engine.cancel", AcceptBefore: scene.Request.AcceptBefore, Payload: v.DecisionCancelPayload{DecisionRef: ref, TaskRef: scene.Request.Payload.TaskRef, DecisionInputDigest: v.SchemaDigest(digest), ControlBasis: basis, Reason: "trusted fixture stop"}}
}

type publicationControlGate struct {
	decision.Publisher
	proposal        v.ContentRef
	proposalKey     string
	proposalBody    []byte
	proposalSources []v.ContentRef
	artifact        v.ContentRef
	afterProposal   bool
	reached         chan struct{}
	resume          chan struct{}
	once            sync.Once
}

func (p *publicationControlGate) PlanPublication(ctx context.Context, key string, body []byte, sources []v.ContentRef, permission decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.PlanPublication(ctx, key, body, sources, permission)
	if err == nil && strings.HasSuffix(key, "/proposal") {
		p.proposal = ref
		p.proposalKey = key
		p.proposalBody = append([]byte(nil), body...)
		p.proposalSources = append([]v.ContentRef(nil), sources...)
	}
	return ref, err
}
func (p *publicationControlGate) ReadPublished(ctx context.Context, ref v.ContentRef, permission decision.Permission) ([]byte, error) {
	body, err := p.Publisher.ReadPublished(ctx, ref, permission)
	if err != nil {
		return nil, err
	}
	if ref != p.proposal {
		p.artifact = ref
	}
	if (ref == p.proposal) == p.afterProposal {
		p.once.Do(func() { close(p.reached) })
		select {
		case <-p.resume:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return body, nil
}
func TestDurableCancelStopsNextPublicationAfterArtifactReadback(t *testing.T) {
	runPublicationControlCase(t, false)
}

func TestDurableCancelBeforeFinishPreservesPublishedUnadoptedBytes(t *testing.T) {
	runPublicationControlCase(t, true)
}

func runPublicationControlCase(t *testing.T, afterProposal bool) {
	runPublicationControlScenario(t, afterProposal, "")
}

func TestDurableControlPreparedV2StopsNextPublicationAfterArtifactReadback(t *testing.T) {
	runPublicationControlScenario(t, false, "delta_candidate_result")
}

func TestDurableControlPreparedV2StopsFinishAfterProposalReadback(t *testing.T) {
	for _, rule := range []string{"delta_candidate_result", "delta_only", "actions_four"} {
		t.Run(rule, func(t *testing.T) {
			runPublicationControlScenario(t, true, rule)
		})
	}
}

func runPublicationControlScenario(t *testing.T, afterProposal bool, proposalCase string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	if proposalCase != "" {
		scene, _ = proposalScenario(t, ctx, w, "fixture-rule/3", proposalCase)
	}
	gate := &publicationControlGate{Publisher: w.Source(), afterProposal: afterProposal, reached: make(chan struct{}), resume: make(chan struct{})}
	s, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: w.Store(), Authority: w.Source(), ControlAuthority: w.Source(), Source: w.Source(), Publisher: gate, Component: scene.Request.Payload.ComponentRef, Worker: "cancel-publication", Lease: 5 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	original := acceptAccounting(t, ctx, s, scene)
	work, err := s.Claim(ctx)
	if err != nil || work == nil {
		t.Fatal("real Claim", err)
	}
	joined := make(chan struct{})
	if err = w.BorrowWorkerExit(joined); err != nil {
		t.Fatal(err)
	}
	var workerErr error
	var resumeOnce sync.Once
	release := func() { resumeOnce.Do(func() { close(gate.resume) }) }
	t.Cleanup(func() {
		release()
		cancel()
		select {
		case <-joined:
		case <-time.After(3 * time.Second):
			t.Error("worker exit unconfirmed; fixture retains both exact scopes")
		}
	})
	go func() { defer close(joined); workerErr = s.RunClaim(ctx, work.Claim) }()
	select {
	case <-gate.reached:
	case <-joined:
		t.Fatalf("worker ended before actual artifact readback: %v", workerErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	control := sceneControl(t, ctx, w, scene, "2", "cancel-between-publications")
	raw, err := v.Encode(control)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Cancel(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := result.AsReceived()
	if !ok {
		t.Fatal("cancel between publications unavailable")
	}
	if _, ok = received.Receipt.AsApplied(); !ok {
		t.Fatal("cancel between publications was not applied")
	}
	release()
	select {
	case <-joined:
	case <-time.After(3 * time.Second):
		t.Fatal("worker exit unconfirmed; retain scopes")
	}
	if !errors.Is(workerErr, runtime.ErrClaim) {
		t.Fatalf("cancelled old worker retained qualification: %v", workerErr)
	}
	permission, err := w.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	noArtifact := proposalCase == "delta_only" || proposalCase == "actions_four"
	if noArtifact {
		if gate.artifact != (v.ContentRef{}) {
			t.Fatal("no-artifact branch invented a publication")
		}
	} else {
		artifact, err := w.Source().ReadPublished(ctx, gate.artifact, permission)
		if err != nil || string(artifact) != "fixture result: alpha\n" {
			t.Fatal("independent original artifact lost", err)
		}
	}
	exists, err := w.Source().PublicationExists(ctx, gate.proposalKey, gate.proposalBody, gate.proposalSources, permission)
	if err != nil || exists != afterProposal {
		t.Fatal("cancel changed the exact observed publication boundary", err)
	}
	proposalBytes, err := w.Source().ReadPublished(ctx, gate.proposal, permission)
	if afterProposal {
		if err != nil || string(proposalBytes) != string(gate.proposalBody) {
			t.Fatal("cancel erased independently published unadopted Proposal bytes", err)
		}
		proposal, decodeErr := v.Decode[v.Proposal](proposalBytes)
		if decodeErr != nil || proposal.DecisionRef != scene.DecisionRef || proposal.SnapshotRef != scene.Request.Payload.SnapshotRef {
			t.Fatal("independent original Proposal lost its binding:", decodeErr)
		}
		if proposalCase == "delta_only" {
			if _, ok := proposal.Advance.AsNone(); !ok {
				t.Fatal("delta-only publication changed its actual no-artifact branch")
			}
		} else if proposalCase == "actions_four" {
			actions, ok := proposal.Advance.AsActions()
			if !ok || len(actions.Actions) != 4 {
				t.Fatal("four-action publication changed its actual no-artifact branch")
			}
		}
	} else if !errors.Is(err, decision.ErrForbidden) {
		t.Fatal("cancel started a new Proposal publication after the public readback gate", err)
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("cancelled running record unavailable")
	}
	closed, ok := found.Decision.AsCancelled()
	if !ok || closed.Input == nil || closed.Usage.RuleStarts != "1" || closed.Usage.Cost.IntegerValue != "1" {
		t.Fatal("cancel lost original running usage or input")
	}
	replay, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := replay.AsFound()
	if !ok {
		t.Fatal("original accepted receipt unavailable")
	}
	before, err := v.Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	after, err := v.Encode(fixed.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("cancel rewrote original accepted receipt")
	}
	closedBefore, err := v.Encode(found.Decision)
	if err != nil {
		t.Fatal(err)
	}
	w.Reopen(ctx)
	restored := controlView(t, ctx, w.Service(), scene)
	closedAfter, err := v.Encode(restored.Decision)
	if err != nil || string(closedAfter) != string(closedBefore) || restored.CurrentControl == nil || restored.CurrentControl.ControlBasis != control.Payload.ControlBasis {
		t.Fatal("reopen changed frozen cancellation facts or adopted control:", err)
	}
	if afterProposal {
		proposalAfter, err := w.Source().ReadPublished(ctx, gate.proposal, permission)
		if err != nil || string(proposalAfter) != string(proposalBytes) {
			t.Fatal("reopen changed independent unadopted Proposal bytes:", err)
		}
	}
	claim, err := w.Service().Claim(ctx)
	if err != nil || claim != nil {
		t.Fatal("reopened cancelled Job was claimable", err)
	}
}

func controlReceipt(t *testing.T, ctx context.Context, s *decision.Service, request v.DecisionCancelRequest, subject v.SubjectBinding) v.CommandReceipt {
	t.Helper()
	raw, err := v.Encode(request)
	if err != nil {
		t.Fatal(err)
	}
	result, err := s.Cancel(ctx, raw, &subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := result.AsReceived()
	if !ok {
		t.Fatal("trusted cancel did not yield a durable receipt")
	}
	return received.Receipt
}
func controlView(t *testing.T, ctx context.Context, s *decision.Service, scene fixture.Scenario) v.DecisionGetResponseFound {
	t.Helper()
	result, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := result.AsFound()
	if !ok {
		t.Fatal("original Decision unavailable")
	}
	return found
}

// The completed case observes Finish's actual commit before applying control.
// The complementary readback gate above applies control before that same Finish.
func TestDurableControlPreservesTerminalFactsAndOrdersRevisions(t *testing.T) {
	for _, terminal := range []string{"completed", "failed", "cancelled"} {
		t.Run(terminal, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			if terminal == "failed" {
				scene.Request.Payload.Limits.MaxOutputBytes = "0"
			}
			s := w.Service()
			original := acceptAccounting(t, ctx, s, scene)
			if terminal == "cancelled" {
				r := sceneControl(t, ctx, w, scene, "2", "first-active-cancel")
				receipt := controlReceipt(t, ctx, s, r, scene.Subject)
				if _, ok := receipt.AsApplied(); !ok {
					t.Fatal("active cancel was not applied")
				}
			} else {
				step, err := s.Step(ctx)
				if err != nil || step.Processed != 1 {
					t.Fatal("real terminal work", err)
				}
			}
			before := controlView(t, ctx, s, scene)
			var revision v.Revision
			switch terminal {
			case "completed":
				completed, ok := before.Decision.AsCompleted()
				if !ok {
					t.Fatal("Finish did not commit completed")
				}
				revision = completed.Revision
				permit, err := w.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
				if err != nil {
					t.Fatal(err)
				}
				published, err := w.Source().ReadPublished(ctx, completed.ProposalRef, permit)
				encoded, encodeErr := v.Encode(completed.Proposal)
				if err != nil || encodeErr != nil || string(published) != string(encoded) {
					t.Fatal("completed original Proposal bytes unavailable", errors.Join(err, encodeErr))
				}
			case "failed":
				failed, ok := before.Decision.AsFailed()
				if !ok || failed.Failure != "output_over_limit" {
					t.Fatal("zero output did not produce original failure")
				}
				revision = failed.Revision
			case "cancelled":
				closed, ok := before.Decision.AsCancelled()
				if !ok {
					t.Fatal("active cancellation did not commit")
				}
				revision = closed.Revision
			}
			frozen, err := v.Encode(before.Decision)
			if err != nil {
				t.Fatal(err)
			}
			high := sceneControl(t, ctx, w, scene, "3", "higher-terminal-control")
			receipt := controlReceipt(t, ctx, s, high, scene.Subject)
			applied, ok := receipt.AsApplied()
			if !ok || applied.Revision != revision {
				t.Fatal("terminal control changed Decision object revision")
			}
			low := sceneControl(t, ctx, w, scene, "2", "lower-terminal-control")
			rejected := controlReceipt(t, ctx, s, low, scene.Subject)
			refusal, ok := rejected.AsRejected()
			if !ok || refusal.Reason != "revision_changed" {
				t.Fatal("lower control was not durably refused")
			}
			same := high
			same.CommandID = "same-terminal-control"
			same.Payload.Reason = "different audit text, same adopted stop"
			receipt = controlReceipt(t, ctx, s, same, scene.Subject)
			if _, ok = receipt.AsApplied(); !ok {
				t.Fatal("same revision and stop binding was not idempotent")
			}
			changed := high
			changed.CommandID = "changed-same-control"
			until, err := time.Parse("2006-01-02T15:04:05.000000Z", string(high.Payload.ControlBasis.ValidUntil))
			if err != nil {
				t.Fatal(err)
			}
			changed.Payload.ControlBasis, err = w.Source().IssueControl(ctx, fixture.ControlClaim{Subject: scene.Subject, DecisionRef: scene.DecisionRef, TaskRef: scene.Request.Payload.TaskRef, InputDigest: high.Payload.DecisionInputDigest, ControlRevision: "3", ValidUntil: v.Time(until.Add(-time.Second).Format("2006-01-02T15:04:05.000000Z"))})
			if err != nil {
				t.Fatal(err)
			}
			rejected = controlReceipt(t, ctx, s, changed, scene.Subject)
			refusal, ok = rejected.AsRejected()
			if !ok || refusal.Reason != "decision_mismatch" {
				t.Fatal("same revision accepted a different durable proof binding")
			}
			maximum := sceneControl(t, ctx, w, scene, "9223372036854775807", "max-terminal-control")
			receipt = controlReceipt(t, ctx, s, maximum, scene.Subject)
			applied, ok = receipt.AsApplied()
			if !ok || applied.Revision != revision {
				t.Fatal("exact large decimal control changed original object revision")
			}
			w.Reopen(ctx)
			s = w.Service()
			after := controlView(t, ctx, s, scene)
			still, err := v.Encode(after.Decision)
			if err != nil || string(still) != string(frozen) {
				t.Fatal("higher controls rewrote frozen terminal facts", err)
			}
			if after.CurrentControl == nil || after.CurrentControl.ControlBasis.ControlRevision != "9223372036854775807" || after.CurrentControl.Scope != "local_decision_work" {
				t.Fatal("reopened control lost exact decimal order or local stopping scope")
			}
			command, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := command.AsFound()
			if !ok {
				t.Fatal("terminal control lost original accepted receipt")
			}
			initial, err := v.Encode(original)
			if err != nil {
				t.Fatal(err)
			}
			actual, err := v.Encode(fixed.Receipt)
			if err != nil || string(actual) != string(initial) {
				t.Fatal("terminal control changed original Command receipt", err)
			}
			claim, err := s.Claim(ctx)
			if err != nil || claim != nil {
				t.Fatal("terminal control re-created work", err)
			}
		})
	}
}
