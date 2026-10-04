//go:build integration

package component_test

import (
	"context"
	"errors"
	"strconv"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

// Faults surround the real owner transaction. They do not synthesize business
// state or replace the physical COMMIT/ROLLBACK with a mock result.
type transactionBoundaryStore struct {
	decision.Store
	rollbackStart bool
	losePrepared  bool
	loseFinish    bool
	boundary      bool
}

var startRollback = errors.New("injected rollback after durable Start write")

func (s *transactionBoundaryStore) SaveDecision(ctx context.Context, tx runtime.Tx, record decision.Record) error {
	if err := s.Store.SaveDecision(ctx, tx, record); err != nil {
		return err
	}
	if s.rollbackStart && record.Status == "running" {
		s.rollbackStart = false
		return startRollback
	}
	if s.losePrepared && record.Prepared != nil && record.Status == "running" {
		s.losePrepared = false
		s.boundary = true
	}
	return nil
}
func (s *transactionBoundaryStore) Complete(ctx context.Context, tx runtime.Tx, claim runtime.Claim, now time.Time) error {
	if err := s.Store.Complete(ctx, tx, claim, now); err != nil {
		return err
	}
	if s.loseFinish {
		s.loseFinish = false
		s.boundary = true
	}
	return nil
}
func (s *transactionBoundaryStore) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	err := s.Store.Within(ctx, owner, fn)
	if err == nil && s.boundary {
		s.boundary = false
		return runtime.ErrCommitUnknown
	}
	return err
}
func boundaryService(t *testing.T, w *fixture.World, store decision.Store) *decision.Service {
	t.Helper()
	scene := w.Scenario()
	service, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: store, Authority: w.Source(), Source: w.Source(), Publisher: w.Source(), Component: scene.Request.Payload.ComponentRef, Worker: "transaction-boundary", Lease: 5 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	return service
}
func TestDurableDecisionStartRollbackKeepsOriginalAllowance(t *testing.T) {
	ctx := decisionContext(t)
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	scene.Request.Payload.Limits.MaxRuleSteps = "1"
	scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
	store := &transactionBoundaryStore{Store: w.Store(), rollbackStart: true}
	s := boundaryService(t, w, store)
	receipt := acceptAccounting(t, ctx, s, scene)
	claimed, err := s.Claim(ctx)
	if err != nil || claimed == nil {
		t.Fatal("claim", err)
	}
	if _, err = s.Start(ctx, claimed.Claim); !errors.Is(err, startRollback) {
		t.Fatal("actual Start did not roll back", err)
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	accepted, ok := found.Decision.AsAccepted()
	if !ok || accepted.Usage.RuleStarts != "0" || accepted.Usage.Cost.IntegerValue != "0" || accepted.Usage.RuleSteps != "0" || !accepted.Usage.MeasurementsComplete {
		t.Fatal("rolled back Start consumed original allowance")
	}
	if err = s.RunClaim(ctx, claimed.Claim); err != nil {
		t.Fatal(err)
	}
	assertBoundaryCompletion(t, ctx, w, s, scene, receipt)
}
func TestDurableDecisionCommitReplyLossPreservesPreparedAndCompletedFacts(t *testing.T) {
	for _, phase := range []string{"prepared", "finish"} {
		t.Run(phase, func(t *testing.T) {
			ctx := decisionContext(t)
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			scene.Request.Payload.Limits.MaxRuleSteps = "1"
			scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
			store := &transactionBoundaryStore{Store: w.Store(), losePrepared: phase == "prepared", loseFinish: phase == "finish"}
			s := boundaryService(t, w, store)
			receipt := acceptAccounting(t, ctx, s, scene)
			_, err := s.Step(ctx)
			if phase == "finish" {
				if !errors.Is(err, runtime.ErrCommitUnknown) {
					t.Fatal("actual Finish commit reply not lost", err)
				}
			} else if err != nil {
				t.Fatal("prepared digest recovery", err)
			}
			assertBoundaryCompletion(t, ctx, w, s, scene, receipt)
			before, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			prior, _ := v.Encode(before)
			w.Reopen(ctx)
			s = w.Service()
			step, err := s.Step(ctx)
			if err != nil || step.Processed != 0 {
				t.Fatal("committed work acquired another job", err)
			}
			after, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			next, _ := v.Encode(after)
			if string(prior) != string(next) {
				t.Fatal("reopen replaced the committed original Proposal")
			}
			assertBoundaryCompletion(t, ctx, w, s, scene, receipt)
		})
	}
}
func assertBoundaryCompletion(t *testing.T, ctx context.Context, w *fixture.World, s *decision.Service, scene fixture.Scenario, receipt v.CommandReceipt) {
	t.Helper()
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	completed, ok := found.Decision.AsCompleted()
	if !ok || completed.Usage.RuleStarts != "1" || completed.Usage.RuleSteps != "1" || completed.Usage.Cost.IntegerValue != "1" || !completed.Usage.MeasurementsComplete {
		t.Fatal("original one-start completion missing")
	}
	candidate, _ := completed.Proposal.Advance.AsCandidateResult()
	if len(candidate.ArtifactRefs) != 1 {
		t.Fatal("artifact missing")
	}
	body, err := w.ReadArtifact(ctx, candidate.ArtifactRefs[0])
	if err != nil || string(body) != "fixture result: alpha\n" {
		t.Fatal("durable independent artifact", err)
	}
	permit, err := w.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := w.Source().ReadPublished(ctx, completed.ProposalRef, permit)
	if err != nil {
		t.Fatal(err)
	}
	expected, err := v.Encode(completed.Proposal)
	if err != nil || string(expected) != string(proposal) {
		t.Fatal("durable independent Proposal", err)
	}
	query, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	command, _ := query.AsFound()
	original, _ := v.Encode(receipt)
	actual, _ := v.Encode(command.Receipt)
	if string(original) != string(actual) {
		t.Fatal("transaction boundary rewrote fixed accepted receipt")
	}
}

func TestDurableDecisionSnapshotLockAndManifestConsumeInputBound(t *testing.T) {
	for _, stage := range []string{"snapshot", "lock", "manifest"} {
		t.Run(stage, func(t *testing.T) {
			ctx := decisionContext(t)
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			source := w.Source()
			permit, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permit, v.MaxBodyBytes)
			if err != nil {
				t.Fatal(err)
			}
			lock, err := source.ReadFixtureLock(ctx, scene.Request.Payload.ComponentRef.InstallLockRef, permit, v.MaxBodyBytes)
			if err != nil {
				t.Fatal(err)
			}
			cap := len(snapshot.Raw) - 1
			if stage == "lock" {
				cap += len(lock.Raw)
			}
			if stage == "manifest" {
				cap += len(lock.Raw) + len(lock.ManifestRaw)
			}
			scene.Request.Payload.Limits.MaxInputBytes = v.Revision(strconv.Itoa(cap))
			s := accountingService(t, w, &publicationForbidden{Publisher: source, t: t}, "input-bound", 5*time.Second)
			acceptAccounting(t, ctx, s, scene)
			if _, err = s.Step(ctx); err != nil {
				t.Fatal(err)
			}
			view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, _ := view.AsFound()
			failed, ok := found.Decision.AsFailed()
			if !ok || failed.Failure != "input_over_limit" || failed.Usage.RuleStarts != "1" || failed.Usage.RuleSteps != "0" || failed.Usage.Cost.IntegerValue != "1" || !failed.Usage.MeasurementsComplete {
				t.Fatal("input boundary did not preserve exact known start and zero computation")
			}
			normal := w.AdditionalScenario(ctx, scene.DecisionRef.OwnerID, v.ID("normal-"+stage), time.Now().Add(15*time.Second))
			service := w.Service()
			receipt := acceptAccounting(t, ctx, service, normal)
			if _, err = service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			assertBoundaryCompletion(t, ctx, w, service, normal, receipt)
		})
	}
}
func TestDurableDecisionPreparedPublicationCannotCrossOriginalDeadline(t *testing.T) {
	ctx := decisionContext(t)
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	scene.Request.Payload.Deadline = v.Time(time.Now().Add(3 * time.Second).UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	scene.Request.Payload.Limits.MaxRuleSteps = "1"
	scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
	p := &leaseLostAfterPublication{Publisher: w.Source()}
	s := accountingService(t, w, p, "deadline", 10*time.Second)
	receipt := acceptAccounting(t, ctx, s, scene)
	claimed, err := s.Claim(ctx)
	if err != nil || claimed == nil {
		t.Fatal("claim", err)
	}
	err = s.RunClaim(ctx, claimed.Claim)
	if !p.reached || !errors.Is(err, runtime.ErrClaim) {
		t.Fatal("original deadline failed to fence publication handoff", err)
	}
	if _, err = s.Maintain(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := view.AsFound()
	failed, ok := found.Decision.AsFailed()
	if !ok || failed.Failure != "deadline_elapsed" || failed.Usage.RuleStarts != "1" || failed.Usage.RuleSteps != "1" || failed.Usage.Cost.IntegerValue != "1" || !failed.Usage.MeasurementsComplete {
		t.Fatal("late prepared work erased observations or completed after original deadline")
	}
	fixed, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	command, _ := fixed.AsFound()
	original, _ := v.Encode(receipt)
	actual, _ := v.Encode(command.Receipt)
	if string(original) != string(actual) {
		t.Fatal("deadline changed the original accepted receipt")
	}
}

// The original fixture grant really expires. The publisher waits outside every
// owner transaction; a separate still-current owner command-read grant can
// continue to read the original receipt without refreshing execution permission.
type permissionExpiredBeforePublication struct {
	decision.Publisher
	service  *decision.Service
	scene    fixture.Scenario
	prepared *v.DecisionRunning
}

func (p *permissionExpiredBeforePublication) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, permit decision.Permission) (v.ContentRef, error) {
	view, err := p.service.Get(ctx, p.scene.GetJSON, &p.scene.Subject)
	if err != nil {
		return v.ContentRef{}, err
	}
	found, ok := view.AsFound()
	if !ok {
		return v.ContentRef{}, errors.New("prepared state not observable before expiry")
	}
	running, ok := found.Decision.AsRunning()
	if !ok {
		return v.ContentRef{}, errors.New("publication started without running state")
	}
	p.prepared = &running
	<-ctx.Done()
	return p.Publisher.Publish(ctx, key, body, sources, permit)
}
func TestDurableDecisionPreparedCannotRefreshOriginalExpiredPermission(t *testing.T) {
	ctx := decisionContext(t)
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	source := w.Source()
	permit, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	material, err := source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permit, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	scene.DecisionRef.ID = "short-permission-decision"
	scene.Request.Target = scene.DecisionRef
	scene.Request.Payload.DecisionID = scene.DecisionRef.ID
	scene.Request.CommandID = "short-permission-command"
	scene.Request.Payload.Limits.MaxRuleSteps = "1"
	scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
	scene.Request.Payload.Deadline = v.Time(time.Now().Add(15 * time.Second).UTC().Truncate(time.Microsecond).Format("2006-01-02T15:04:05.000000Z"))
	permit.ValidUntil = time.Now().Add(3 * time.Second).UTC().Truncate(time.Microsecond)
	_, err = source.Seed(ctx, fixture.Bundle{DecisionRef: scene.DecisionRef, Permission: permit, Snapshot: snapshot, Materials: []fixture.Material{{Ref: scene.MaterialRef, Bytes: material}}, Purposes: []string{"decide", "get", "command.get", "start", "material", "rule.input", "fixture.lock", "publish", "proposal.publish", "artifact.publish"}, RuleVersion: permit.RuleVersion, ChargeBasis: permit.ChargeBasis, RuleStartCharge: permit.RuleStartCharge})
	if err != nil {
		t.Fatal(err)
	}
	scene.GetJSON, err = v.Encode(v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "read-short", Target: scene.DecisionRef, Method: "decision_engine.get", AcceptBefore: scene.Request.AcceptBefore, Payload: v.DecisionGetPayload{DecisionRef: scene.DecisionRef}})
	if err != nil {
		t.Fatal(err)
	}
	command, err := v.DecodeCommand(scene.CommandGetJSON)
	if err != nil {
		t.Fatal(err)
	}
	command.Target.ID = scene.Request.CommandID
	command.Payload.CommandRef.CommandID = scene.Request.CommandID
	scene.CommandGetJSON, err = v.Encode(command)
	if err != nil {
		t.Fatal(err)
	}
	publisher := &permissionExpiredBeforePublication{Publisher: source, scene: scene}
	service := accountingService(t, w, publisher, "original-permission", 10*time.Second)
	publisher.service = service
	receipt := acceptAccounting(t, ctx, service, scene)
	claimed, err := service.Claim(ctx)
	if err != nil || claimed == nil {
		t.Fatal("claim", err)
	}
	err = service.RunClaim(ctx, claimed.Claim)
	if !errors.Is(err, decision.ErrForbidden) || publisher.prepared == nil {
		t.Fatal("expired original grant was refreshed for prepared execution", err)
	}
	if publisher.prepared.Usage.RuleStarts != "1" || publisher.prepared.Usage.RuleSteps != "1" || publisher.prepared.Usage.Cost.IntegerValue != "1" || !publisher.prepared.Usage.MeasurementsComplete {
		t.Fatal("prepared observations missing before expiry")
	}
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	rejected, ok := view.AsRejected()
	if !ok || rejected.Reason != "forbidden" {
		t.Fatal("expired current get qualification was bypassed")
	}
	fixed, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, _ := fixed.AsFound()
	original, _ := v.Encode(receipt)
	actual, _ := v.Encode(found.Receipt)
	if string(original) != string(actual) {
		t.Fatal("permission expiry changed the original accepted receipt")
	}
}
