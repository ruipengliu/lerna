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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	gate := &publicationControlGate{Publisher: w.Source(), reached: make(chan struct{}), resume: make(chan struct{})}
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
	artifact, err := w.Source().ReadPublished(ctx, gate.artifact, permission)
	if err != nil || string(artifact) != "fixture result: alpha\n" {
		t.Fatal("independent original artifact lost", err)
	}
	exists, err := w.Source().PublicationExists(ctx, gate.proposalKey, gate.proposalBody, gate.proposalSources, permission)
	if err != nil || exists {
		t.Fatal("cancel started the exact original Proposal publication after public readback", err)
	}
	if _, err = w.Source().ReadPublished(ctx, gate.proposal, permission); !errors.Is(err, decision.ErrForbidden) {
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
	w.Reopen(ctx)
	claim, err := w.Service().Claim(ctx)
	if err != nil || claim != nil {
		t.Fatal("reopened cancelled Job was claimable", err)
	}
}
