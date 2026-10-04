//go:build integration

package component_test

import (
	"context"
	"errors"
	decisionpg "github.com/ruipengliu/lerna/adapters/postgres/decision_engine"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

func TestDurableDecisionPoolCoversNonAnchorDeadlineAtZeroQuota(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.Service()
	deadline := time.Now().UTC().Add(750 * time.Millisecond).Truncate(time.Microsecond)
	second := world.AdditionalScenario(ctx, "second-owner", "early-deadline", deadline)
	cfg := workpool.Default("fixture-decision-pool", []contract.OwnerRef{{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}, {TenantID: contract.ID(second.DecisionRef.TenantID), OwnerID: contract.ID(second.DecisionRef.OwnerID)}})
	for index := range cfg.Quotas {
		if cfg.Quotas[index].Lane == "ordinary" {
			cfg.Quotas[index].Concurrent = 0
		}
	}
	if err := service.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	secondService := world.ServiceFor(v.OwnerRef{TenantID: second.DecisionRef.TenantID, OwnerID: second.DecisionRef.OwnerID}, "second-worker", time.Second)
	result, err := secondService.Decide(ctx, encode11(t, second.Request), &second.Subject)
	requireAccepted(t, result, err)
	step, err := service.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if step.Processed != 0 || step.WaitFor <= 0 || step.NextWake.After(deadline) {
		t.Fatalf("nonanchor deadline not earliest finite wake: %+v", step)
	}
	if err = (runtime.WallTimer{}).Wait(ctx, time.Until(deadline)+time.Millisecond); err != nil {
		t.Fatal(err)
	}
	step, err = service.Step(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if step.Processed != 0 || step.WaitFor <= 0 {
		t.Fatalf("zero-quota maintenance busy loop: %+v", step)
	}
	view, err := secondService.Get(ctx, second.GetJSON, &second.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("expired Decision unavailable")
	}
	failed, ok := found.Decision.AsFailed()
	if !ok || failed.Failure != "deadline_elapsed" {
		t.Fatal("nonanchor responsibility was not closed at zero quota")
	}
	observed, err := service.ObservePool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Active["ordinary"] != 0 || observed.Queued["ordinary"] != 0 {
		t.Fatal("expiry retained execution occupancy")
	}
	// Restoring a positive quota lets a later original Decision finish normally.
	for index := range cfg.Quotas {
		if cfg.Quotas[index].Lane == "ordinary" {
			cfg.Quotas[index].Concurrent = 1
		}
	}
	if err = service.InstallPool(ctx, cfg, 2); err != nil {
		t.Fatal(err)
	}
	result, err = service.Decide(ctx, encode11(t, scene.Request), &scene.Subject)
	requireAccepted(t, result, err)
	step, err = service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("positive-quota normal counterpart failed: %v", err)
	}
}
func TestDurableDecisionStartFencesClaimAndRequiresRecordedStart(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.ServiceFor(v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, "short-worker", 100*time.Millisecond)
	result, err := service.Decide(ctx, encode11(t, scene.Request), &scene.Subject)
	requireAccepted(t, result, err)
	work, err := service.Claim(ctx)
	if err != nil || work == nil {
		t.Fatalf("normal claim unavailable: %v", err)
	}
	owner := contract.OwnerRef{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}
	err = world.Store().Within(ctx, owner, func(ctxctx context.Context, tx runtime.Tx) error {
		return world.Store().Complete(ctxctx, tx, work.Claim, time.Now())
	})
	if !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("completion without actual Start accepted: %v", err)
	}
	wrong := work.Claim
	wrong.Worker = "different-worker"
	if _, err = service.Start(ctx, wrong); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("wrong worker Start accepted: %v", err)
	}
	if err = (runtime.WallTimer{}).Wait(ctx, time.Until(work.Claim.LeaseUntil)+time.Millisecond); err != nil {
		t.Fatal(err)
	}
	if _, err = service.Start(ctx, work.Claim); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("expired Claim Start accepted: %v", err)
	}
	service = world.ServiceFor(v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, "short-worker", 2*time.Second)
	replacement, err := service.Claim(ctx)
	if err != nil || replacement == nil {
		t.Fatalf("replacement unavailable: %v", err)
	}
	if replacement.Claim.Epoch <= work.Claim.Epoch {
		t.Fatal("replacement did not fence original epoch")
	}
	if _, err = service.Start(ctx, work.Claim); !errors.Is(err, runtime.ErrClaim) {
		t.Fatalf("old Claim Start accepted: %v", err)
	}
	if err = service.RunClaim(ctx, replacement.Claim); err != nil {
		t.Fatalf("replacement normal completion failed: %v", err)
	}
}

func TestDurableDecisionLastQueueSlotAcrossStoreInstances(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.Service()
	second := world.AdditionalScenario(ctx, scene.DecisionRef.OwnerID, "second-decision", time.Now().Add(time.Minute))
	cfg := workpool.Default("fixture-decision-pool", []contract.OwnerRef{{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}})
	cfg.Limits[0].Queue = 1
	cfg.Limits[0].Concurrent = 1
	for index := range cfg.Quotas {
		if cfg.Quotas[index].Lane == "ordinary" {
			cfg.Quotas[index].Concurrent = 1
		}
	}
	if err := service.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	peer, err := decisionpg.Open(ctx, world.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	peerService, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: peer, Authority: world.Source(), Source: world.Source(), Publisher: world.Source(), Component: scene.Request.Payload.ComponentRef, Worker: "peer-worker", Lease: 2 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	type response struct {
		scene   fixture.Scenario
		outcome v.TransportOutcome
		err     error
	}
	results := make(chan response, 2)
	start := make(chan struct{})
	for _, entry := range []struct {
		service *decision.Service
		scene   fixture.Scenario
	}{{service, scene}, {peerService, second}} {
		go func(entry struct {
			service *decision.Service
			scene   fixture.Scenario
		}) {
			<-start
			raw, e := v.Encode(entry.scene.Request)
			var out v.TransportOutcome
			if e == nil {
				out, e = entry.service.Decide(ctx, raw, &entry.scene.Subject)
			}
			results <- response{entry.scene, out, e}
		}(entry)
	}
	close(start)
	var acceptedScene, rejectedScene fixture.Scenario
	for count := 0; count < 2; count++ {
		response := <-results
		if response.err == nil {
			requireAccepted(t, response.outcome, nil)
			acceptedScene = response.scene
		} else if errors.Is(response.err, workpool.ErrCapacity) {
			rejectedScene = response.scene
		} else {
			t.Fatal(response.err)
		}
	}
	if acceptedScene.DecisionRef.ID == "" || rejectedScene.DecisionRef.ID == "" {
		t.Fatal("last queue slot was not exclusive across Store instances")
	}
	absent, err := service.Get(ctx, rejectedScene.GetJSON, &rejectedScene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := absent.AsResultUnavailable(); !ok {
		t.Fatal("capacity rollback left a Decision")
	}
	// The accepted original can be associated with a new command at full queue.
	association := acceptedScene.Request
	association.CommandID = "associate-full-queue"
	outcome, err := service.Decide(ctx, encode11(t, association), &acceptedScene.Subject)
	requireAccepted(t, outcome, err)
	observation, err := service.ObservePool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Queued["ordinary"] != 1 {
		t.Fatal("same input association allocated another queue slot")
	}
	step, err := service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("first original could not finish: %v", err)
	}
	outcome, err = service.Decide(ctx, encode11(t, rejectedScene.Request), &rejectedScene.Subject)
	requireAccepted(t, outcome, err)
	step, err = service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("capacity retry on original identity failed: %v", err)
	}
}

func TestDurableDecisionMissingPoolPreservesOriginalIdentity(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.Service()
	inactive := workpool.Default("fixture-decision-pool", []contract.OwnerRef{{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: "idle-owner"}})
	if err := service.InstallPool(ctx, inactive, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Decide(ctx, encode11(t, scene.Request), &scene.Subject); !errors.Is(err, workpool.ErrMissing) {
		t.Fatalf("missing pool admitted original: %v", err)
	}
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := view.AsResultUnavailable(); !ok {
		t.Fatal("missing pool left a Decision")
	}
	command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := command.AsNotFound(); !ok {
		t.Fatal("missing pool left receipt")
	}
	active := workpool.Default("fixture-decision-pool", []contract.OwnerRef{{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}})
	if err = service.InstallPool(ctx, active, 2); err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Decide(ctx, encode11(t, scene.Request), &scene.Subject)
	requireAccepted(t, outcome, err)
	step, err := service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("normal qualified retry failed: %v", err)
	}
	if err = service.InstallPool(ctx, inactive, 3); err != nil {
		t.Fatal(err)
	}
	outcome, err = service.Decide(ctx, encode11(t, scene.Request), &scene.Subject)
	requireAccepted(t, outcome, err)
	changed := scene.Request
	changed.Payload.Limits.MaxRuleSteps = "1000"
	_, err = service.Decide(ctx, encode11(t, changed), &scene.Subject)
	var public *v.ContractError
	if !errors.As(err, &public) || public.Code != "idempotency_conflict" {
		t.Fatalf("changed original bypassed identity: %v", err)
	}
	association := scene.Request
	association.CommandID = "new-unqualified-association"
	if _, err = service.Decide(ctx, encode11(t, association), &scene.Subject); !errors.Is(err, workpool.ErrMissing) {
		t.Fatalf("new unqualified command created fixed fact: %v", err)
	}
	command, err = service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := command.AsFound(); !ok {
		t.Fatal("original read lost with missing pool")
	}
}
func TestDurableDecisionTenantQuotaCombinesTwoOwners(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.Service()
	second := world.AdditionalScenario(ctx, "second-owner", "second-decision", time.Now().Add(time.Minute))
	cfg := workpool.Default("fixture-decision-pool", []contract.OwnerRef{{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}, {TenantID: contract.ID(second.DecisionRef.TenantID), OwnerID: contract.ID(second.DecisionRef.OwnerID)}})
	cfg.Limits[0].Concurrent = 2
	for index := range cfg.Quotas {
		if cfg.Quotas[index].Lane == "ordinary" {
			cfg.Quotas[index].Concurrent = 1
		}
	}
	if err := service.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	secondService := world.ServiceFor(v.OwnerRef{TenantID: second.DecisionRef.TenantID, OwnerID: second.DecisionRef.OwnerID}, "fixture-worker", 5*time.Second)
	out, err := service.Decide(ctx, encode11(t, scene.Request), &scene.Subject)
	requireAccepted(t, out, err)
	out, err = secondService.Decide(ctx, encode11(t, second.Request), &second.Subject)
	requireAccepted(t, out, err)
	first, err := service.Claim(ctx)
	if err != nil || first == nil {
		t.Fatalf("normal first claim: %v", err)
	}
	other, err := secondService.Claim(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if other != nil {
		t.Fatal("same tenant overallocated across owners")
	}
	if err = service.RunClaim(ctx, first.Claim); err != nil {
		t.Fatal(err)
	}
	other, err = secondService.Claim(ctx)
	if err != nil || other == nil {
		t.Fatalf("released quota did not serve second owner: %v", err)
	}
	if err = secondService.RunClaim(ctx, other.Claim); err != nil {
		t.Fatal(err)
	}
	observation, err := service.ObservePool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if observation.Active["ordinary"] != 0 || observation.Queued["ordinary"] != 0 {
		t.Fatal("normal completions retained quota")
	}
}
