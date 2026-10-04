//go:build integration

package component_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	decisionpg "github.com/ruipengliu/lerna/adapters/postgres/decision_engine"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

func decisionContext(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func encode11[T v.Value](t *testing.T, value T) []byte {
	t.Helper()
	data, err := v.Encode(value)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
func requireAccepted(t *testing.T, result v.TransportOutcome, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	received, ok := result.AsReceived()
	if !ok {
		t.Fatal("expected received")
	}
	if _, ok = received.Receipt.AsAccepted(); !ok {
		data, _ := v.Encode(received.Receipt)
		t.Fatalf("expected accepted: %s", data)
	}
}
func TestDurableDecisionTwoIdentitiesAndFixedLegacyView(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.Service()
	original := encode11(t, scene.Request)
	outcomes := make(chan error, 8)
	var workers sync.WaitGroup
	for index := 0; index < 8; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			request := scene.Request
			if index >= 4 {
				request.CommandID = v.ID("new-command-" + string(rune('a'+index)))
			}
			raw, err := v.Encode(request)
			if err == nil {
				result, e := service.Decide(ctx, raw, &scene.Subject)
				err = e
				if e == nil {
					received, ok := result.AsReceived()
					if !ok {
						err = errors.New("concurrent request not received")
					} else if _, ok = received.Receipt.AsAccepted(); !ok {
						err = errors.New("concurrent request not accepted")
					}
				}
			}
			outcomes <- err
		}(index)
	}
	workers.Wait()
	close(outcomes)
	for err := range outcomes {
		if err != nil {
			t.Fatal(err)
		}
	}
	// An altered original Command fails before Decision's second identity gate.
	changed := scene.Request
	changed.Payload.Limits.MaxRuleSteps = "1000"
	_, err := service.Decide(ctx, encode11(t, changed), &scene.Subject)
	var refusal *v.ContractError
	if !errors.As(err, &refusal) || refusal.Code != "idempotency_conflict" {
		t.Fatalf("changed original key: %v", err)
	}
	changed.CommandID = "different-input-command"
	result, err := service.Decide(ctx, encode11(t, changed), &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := result.AsReceived()
	if !ok {
		t.Fatal("mismatch not received")
	}
	rejected, ok := received.Receipt.AsRejected()
	if !ok || rejected.Reason != "decision_mismatch" {
		t.Fatal("different Decision input was not fixed rejected")
	}
	query, err := v.DecodeCommand(scene.CommandGetJSON)
	if err != nil {
		t.Fatal(err)
	}
	query.Target.ID = changed.CommandID
	query.Payload.CommandRef.CommandID = changed.CommandID
	found, err := service.GetCommand(ctx, encode11(t, query), &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := found.AsFound()
	if !ok {
		t.Fatal("new refusal was not readable")
	}
	reason, ok := fixed.Receipt.AsRejected()
	if !ok || reason.Reason != "decision_mismatch" {
		t.Fatal("new refusal changed")
	}
	legacyQuery := contract.CommandGetRequest{ContractVersion: "1.0.0", Profile: "command", CommandID: "old-view", Target: contract.CommandTarget{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID), Kind: "command", ID: contract.ID(changed.CommandID)}, Method: "command.get", AcceptBefore: contract.Time(scene.Request.AcceptBefore), Payload: contract.CommandGetPayload{CommandRef: contract.CommandRef{Owner: contract.OwnerRef{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}, CommandID: contract.ID(changed.CommandID)}}}
	oldRaw, err := contract.Encode(legacyQuery)
	if err != nil {
		t.Fatal(err)
	}
	oldView, err := contract.ReadCommandFacts(ctx, oldRaw, decision.LegacyReader{Service: service}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok = oldView.AsUnavailable(); !ok {
		t.Fatal("legacy lossy refusal was not unavailable")
	}
	legacyQuery.Target.ID = contract.ID(scene.Request.CommandID)
	legacyQuery.Payload.CommandRef.CommandID = contract.ID(scene.Request.CommandID)
	oldRaw, err = contract.Encode(legacyQuery)
	if err != nil {
		t.Fatal(err)
	}
	oldView, err = contract.ReadCommandFacts(ctx, oldRaw, decision.LegacyReader{Service: service}, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok = oldView.AsFound(); !ok {
		t.Fatal("legacy lossless accepted view unavailable")
	}
	step, err := service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("single original job did not complete: %v %+v", err, step)
	}
	step, err = service.Step(ctx)
	if err != nil || step.Processed != 0 {
		t.Fatalf("duplicate input created extra job: %v %+v", err, step)
	}
	repeated, err := service.Decide(ctx, original, &scene.Subject)
	requireAccepted(t, repeated, err)
}
func TestDurableDecisionRollbackAndTokenIsolation(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.Service()
	owner := contract.OwnerRef{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}
	digest, err := v.DecisionInputDigest(scene.Request, scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	record := decision.Record{Ref: scene.DecisionRef, Input: &scene.Request.Payload, Subject: scene.Subject, InputDigest: digest, Revision: 1, Status: "accepted", ArtifactRefs: []v.ContentRef{}, Usage: v.DecisionUsage{InputBytes: "0", OutputBytes: "0", RuleSteps: "0", ModelRequests: "0", Cost: v.Amount{Unit: "fixture", IntegerValue: "0"}}}
	aborted := errors.New("business transaction deliberately aborted")
	var expired runtime.Tx
	err = world.Store().Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		expired = tx
		if err := world.Store().SaveDecision(ctx, tx, record); err != nil {
			return err
		}
		if _, err := world.Store().Trigger(ctx, tx, contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "decision", ID: contract.ID(scene.DecisionRef.ID)}, "decide", 1, time.Now()); err != nil {
			return err
		}
		return aborted
	})
	if !errors.Is(err, aborted) {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := view.AsResultUnavailable(); !ok {
		t.Fatal("rollback left an original Decision")
	}
	command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := command.AsNotFound(); !ok {
		t.Fatal("rollback left a fixed command receipt")
	}
	if _, err = world.Store().ReadDecision(ctx, expired, scene.DecisionRef); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("expired token accepted: %v", err)
	}
	peer, err := decisionpg.Open(ctx, world.Config())
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	err = world.Store().Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		_, err := peer.ReadDecision(ctx, tx, scene.DecisionRef)
		if !errors.Is(err, runtime.ErrScope) {
			return errors.New("foreign underlying store accepted token")
		}
		wrong := scene.DecisionRef
		wrong.OwnerID = "wrong-owner"
		_, err = world.Store().ReadDecision(ctx, tx, wrong)
		if !errors.Is(err, runtime.ErrScope) {
			return errors.New("wrong owner accepted token")
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := service.Decide(ctx, encode11(t, scene.Request), &scene.Subject)
	requireAccepted(t, result, err)
	step, err := service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("normal after rollback failed: %v", err)
	}
}
func TestDurableDecisionCrossTenantAndOwnerAuthorization(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.Service()
	foreign := scene.Subject
	foreign.TenantID = "foreign-tenant"
	if _, err := service.Decide(ctx, encode11(t, scene.Request), &foreign); err == nil {
		t.Fatal("foreign tenant admitted")
	}
	changed := scene.Request
	changed.Target.OwnerID = "foreign-owner"
	if _, err := service.Decide(ctx, encode11(t, changed), &scene.Subject); err == nil {
		t.Fatal("foreign owner admitted")
	}
	view, err := service.Get(ctx, scene.GetJSON, &foreign)
	if err != nil {
		t.Fatal(err)
	}
	rejected, ok := view.AsRejected()
	if !ok || rejected.Reason != "forbidden" {
		t.Fatal("foreign read disclosed Decision")
	}
	result, err := service.Decide(ctx, encode11(t, scene.Request), &scene.Subject)
	requireAccepted(t, result, err)
	step, err := service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("authorized normal path failed: %v", err)
	}
}

func TestDurableDecisionOriginalReplaySurvivesAcceptanceCutoff(t *testing.T) {
	ctx := decisionContext(t)
	world := fixture.NewWorld(t, ctx)
	scene := world.Scenario()
	service := world.Service()
	cutoff := time.Now().UTC().Add(500 * time.Millisecond).Truncate(time.Microsecond)
	scene.Request.AcceptBefore = v.Time(cutoff.Format("2006-01-02T15:04:05.000000Z"))
	raw := encode11(t, scene.Request)
	first, err := service.Decide(ctx, raw, &scene.Subject)
	requireAccepted(t, first, err)
	if err = (runtime.WallTimer{}).Wait(ctx, time.Until(cutoff)+time.Millisecond); err != nil {
		t.Fatal(err)
	}
	repeated, err := service.Decide(ctx, raw, &scene.Subject)
	requireAccepted(t, repeated, err)
	before := encode11(t, first)
	after := encode11(t, repeated)
	if string(before) != string(after) {
		t.Fatal("cutoff changed original fixed receipt")
	}
	fresh := scene.Request
	fresh.CommandID = "late-new-command"
	outcome, err := service.Decide(ctx, encode11(t, fresh), &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := outcome.AsReceived()
	if !ok {
		t.Fatal("late rejection not received")
	}
	rejected, ok := received.Receipt.AsRejected()
	if !ok || rejected.Reason != "expired" {
		t.Fatal("late new command not fixed expired")
	}
	step, err := service.Step(ctx)
	if err != nil || step.Processed != 1 {
		t.Fatalf("acceptance cutoff incorrectly stopped execution: %v", err)
	}
}
