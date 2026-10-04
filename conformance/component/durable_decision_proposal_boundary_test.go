//go:build integration

package component_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

// This finite environment fault delegates ordinary rule input to the real
// Source. At the declared semantic purpose it actually closes that native
// owner, then attempts the real read; it never manufactures a dependency error.
type closingProposalSource struct {
	decision.Source
	native    *fixture.Store
	purpose   string
	readError error
}

func (s *closingProposalSource) ReadMaterial(ctx context.Context, ref v.ContentRef, purpose string, permission decision.Permission, limit int64) ([]byte, error) {
	if purpose == s.purpose {
		if err := s.native.Close(); err != nil {
			return nil, err
		}
	}
	body, err := s.Source.ReadMaterial(ctx, ref, purpose, permission, limit)
	if purpose == s.purpose {
		s.readError = err
	}
	return body, err
}

func TestDurableProposalNativeSourceFailureRetainsDependencyMeaning(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, _ := proposalScenario(t, ctx, world, "fixture-rule/3", "actions_four")
	source := &closingProposalSource{Source: world.Source(), native: world.Source(), purpose: "fixture.read"}
	service, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: world.Store(), Source: source, Authority: world.Source(), Publisher: world.Source(), Component: scene.Request.Payload.ComponentRef, Worker: "native-source-failure-worker", Lease: 5 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	receipt := acceptAccounting(t, ctx, service, scene)
	if step, err := service.Step(ctx); err != nil || step.Processed != 1 {
		t.Fatal("finite native failure step", err)
	}
	if source.readError == nil {
		t.Fatal("declared purpose did not execute an actual closed native read", source.readError)
	}
	world.Reopen(ctx)
	service = proposalService(t, world, scene)
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("original dependency failure unavailable")
	}
	failed, ok := found.Decision.AsFailed()
	if !ok || failed.Failure != "snapshot_unavailable" {
		t.Fatalf("native Source failure: expected snapshot_unavailable, got %s", failed.Failure)
	}
	if failed.Usage.RuleStarts != "1" || failed.Usage.RuleSteps != "1" || failed.Usage.Cost.IntegerValue != "1" || failed.Usage.ModelRequests != "0" || failed.Usage.OutputBytes == "0" || !failed.Usage.MeasurementsComplete {
		t.Fatal("dependency failure lost actual bounded observations", failed.Usage)
	}
	command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := command.AsFound()
	if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
		t.Fatal("dependency failure changed original accepted receipt")
	}
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := replay.AsReceived()
	if !ok || !reflect.DeepEqual(received.Receipt, receipt) {
		t.Fatal("dependency failure created another charging identity")
	}
	if step, err := service.Step(ctx); err != nil || step.Processed != 0 {
		t.Fatal("dependency terminal retained automatic repair", err)
	}
}

func TestDurableProposalImmutableCaseBindingSurvivesRejectedReseed(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, _ := proposalScenario(t, ctx, world, "fixture-rule/3", "delta_only")
	service := proposalService(t, world, scene)
	receipt := acceptAccounting(t, ctx, service, scene)
	permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := world.Source().ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permission, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	lock, err := world.Source().ReadFixtureLock(ctx, scene.Request.Payload.ComponentRef.InstallLockRef, permission, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	materials := []fixture.Material{}
	for _, ref := range snapshot.MaterialRefs {
		body, err := world.Source().ReadMaterial(ctx, ref, "rule.input", permission, v.MaxBodyBytes)
		if err != nil {
			t.Fatal(err)
		}
		materials = append(materials, fixture.Material{Ref: ref, Bytes: body})
	}
	for _, mutation := range []string{"same_snapshot_new_case", "wrong_rule_version", "wrong_config_digest"} {
		t.Run(mutation, func(t *testing.T) {
			changed := snapshot
			changed.Raw = nil
			permit := permission
			version := "fixture-rule/3"
			switch mutation {
			case "same_snapshot_new_case":
				changed.Rule = "actions_four"
				sum := sha256.Sum256([]byte(changed.Rule))
				changed.ComponentRef.ConfigDigest = v.SchemaDigest("sha256:" + hex.EncodeToString(sum[:]))
				permit.ComponentRef = changed.ComponentRef
			case "wrong_rule_version":
				version, permit.RuleVersion = "fixture-rule/2", "fixture-rule/2"
			case "wrong_config_digest":
				changed.ComponentRef.ConfigDigest = v.SchemaDigest("sha256:" + strings.Repeat("0", 64))
				permit.ComponentRef = changed.ComponentRef
			}
			if _, err := world.Source().Seed(ctx, fixture.Bundle{DecisionRef: scene.DecisionRef, Permission: permit, Snapshot: changed, Materials: materials, Purposes: []string{"decide", "get", "command.get", "start", "material", "rule.input", "fixture.lock", "publish", "proposal.publish", "artifact.publish", "rule.condition"}, RuleVersion: version, ChargeBasis: permission.ChargeBasis, RuleStartCharge: permission.RuleStartCharge}); err == nil {
				t.Fatal("fixed original binding was changed")
			} else if mutation == "same_snapshot_new_case" && !errors.Is(err, decision.ErrPublicationConflict) {
				t.Fatal("new valid case did not reach actual immutable identity rejection", err)
			}
			preserved, err := world.Source().ReadSnapshot(ctx, snapshot.Ref, permission, v.MaxBodyBytes)
			if err != nil || !reflect.DeepEqual(preserved, snapshot) {
				t.Fatal("rejected reseed changed original immutable Snapshot", err)
			}
			preservedLock, err := world.Source().ReadFixtureLock(ctx, scene.Request.Payload.ComponentRef.InstallLockRef, permission, v.MaxBodyBytes)
			if err != nil || !reflect.DeepEqual(preservedLock, lock) {
				t.Fatal("rejected reseed changed original manifest or lock", err)
			}
		})
	}
	if step, err := service.Step(ctx); err != nil || step.Processed != 1 {
		t.Fatal("original rule responsibility lost after rejected reseed", err)
	}
	world.Reopen(ctx)
	service = proposalService(t, world, scene)
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("original fixed Decision missing")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok || completed.Input.ComponentRef != scene.Request.Payload.ComponentRef || completed.Usage.RuleStarts != "1" || completed.Usage.Cost.IntegerValue != "1" || len(completed.ArtifactRefs) != 0 {
		t.Fatal("original case changed meaning or allowance")
	}
	if _, ok := completed.Proposal.Advance.AsNone(); !ok {
		t.Fatal("original delta-only became another case")
	}
	command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := command.AsFound()
	if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
		t.Fatal("reseed changed original accepted receipt")
	}
}

type observedProposalPublication struct {
	key     string
	ref     v.ContentRef
	body    []byte
	sources []v.ContentRef
}

// This observer delegates every identity and body to the real independent
// publisher. Its finite reply-loss option is a mechanical boundary fault;
// business assertions read the actual Source facts, never call counts.
type proposalPublisherObserver struct {
	decision.Publisher
	planned        []observedProposalPublication
	loseFirstReply bool
	lost           bool
}

func (p *proposalPublisherObserver) PlanPublication(ctx context.Context, key string, body []byte, sources []v.ContentRef, permission decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.PlanPublication(ctx, key, body, sources, permission)
	if err == nil {
		p.planned = append(p.planned, observedProposalPublication{key, ref, slices.Clone(body), slices.Clone(sources)})
	}
	return ref, err
}

func (p *proposalPublisherObserver) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, permission decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.Publish(ctx, key, body, sources, permission)
	if err == nil && p.loseFirstReply && !p.lost {
		p.lost = true
		return v.ContentRef{}, errors.New("fixture publication reply lost after independent commit")
	}
	return ref, err
}

func TestDurableProposalOriginalLimitsMatrix(t *testing.T) {
	type limitCase struct {
		name, rule                 string
		steps, cost, input, output v.Revision
		actions                    v.Revision
		failure                    v.DecisionFailure
		starts, confirmed          v.Revision
	}
	cases := []limitCase{
		{"zero_steps", "delta_only", "0", "1", "1048576", "1048576", "4", "rule_limit_exceeded", "0", "0"},
		{"zero_cost", "delta_only", "1", "0", "1048576", "1048576", "4", "budget_exhausted", "0", "0"},
		{"bounded_input", "input_request", "1", "1", "1", "1048576", "4", "input_over_limit", "1", "0"},
		{"whole_artifact_and_proposal", "delta_candidate_result", "1", "1", "1048576", "64", "4", "output_over_limit", "1", "1"},
		{"whole_no_artifact_proposal", "delta_only", "1", "1", "1048576", "64", "4", "output_over_limit", "1", "1"},
	}
	for actions := 0; actions <= 4; actions++ {
		failure := v.DecisionFailure("proposal_invalid")
		if actions == 4 {
			failure = ""
		}
		cases = append(cases, limitCase{"max_actions_" + strconv.Itoa(actions), "actions_four", "1", "1", "1048576", "1048576", v.Revision(strconv.Itoa(actions)), failure, "1", "1"})
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			world := fixture.NewWorld(t, ctx)
			scene, _ := proposalScenario(t, ctx, world, "fixture-rule/3", tc.rule)
			limits := &scene.Request.Payload.Limits
			limits.MaxRuleSteps, limits.MaxCost.IntegerValue = tc.steps, tc.cost
			limits.MaxInputBytes, limits.MaxOutputBytes, limits.MaxActions = tc.input, tc.output, tc.actions
			observer := &proposalPublisherObserver{Publisher: world.Source()}
			service := proposalServiceWithPublisher(t, world, scene, observer)
			receipt := acceptAccounting(t, ctx, service, scene)
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			world.Reopen(ctx)
			service = proposalService(t, world, scene)
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("original bounded Decision unavailable")
			}
			var usage v.DecisionUsage
			if tc.failure == "" {
				completed, ok := found.Decision.AsCompleted()
				if !ok || completed.Input.Limits != *limits {
					t.Fatal("legal four-action limit did not complete with its original allowance")
				}
				usage = completed.Usage
				permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := world.Source().ReadPublished(ctx, completed.ProposalRef, permission); err != nil {
					t.Fatal("legal bounded Proposal is not independently readable", err)
				}
			} else {
				failed, ok := found.Decision.AsFailed()
				if !ok || failed.Failure != tc.failure || failed.Input.Limits != *limits {
					t.Fatalf("expected original bounded failure %s", tc.failure)
				}
				usage = failed.Usage
				permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
				if err != nil {
					t.Fatal(err)
				}
				for _, output := range observer.planned {
					if _, err := world.Source().ReadPublished(ctx, output.ref, permission); !errors.Is(err, decision.ErrForbidden) {
						t.Fatal("over-limit output became independently readable", err)
					}
				}
			}
			if usage.RuleStarts != tc.starts || usage.RuleSteps != tc.confirmed || usage.Cost.IntegerValue != tc.starts || usage.ModelRequests != "0" || !usage.MeasurementsComplete {
				t.Fatal("original limit reset or fabricated measurement", usage)
			}
			command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := command.AsFound()
			if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
				t.Fatal("limit changed original accepted receipt")
			}
			if step, err := service.Step(ctx); err != nil || step.Processed != 0 {
				t.Fatal("bounded terminal retained runnable repair", err)
			}
		})
	}
}

func TestDurableProposalPreparedV2RecoversActualReplyLoss(t *testing.T) {
	for _, rule := range []string{"delta_only", "actions_four", "input_request", "delta_candidate_result", "cannot_continue"} {
		t.Run(rule, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			world := fixture.NewWorld(t, ctx)
			scene, _ := proposalScenario(t, ctx, world, "fixture-rule/3", rule)
			scene.Request.Payload.Limits.MaxRuleSteps = "1"
			scene.Request.Payload.Limits.MaxCost.IntegerValue = "1"
			observer := &proposalPublisherObserver{Publisher: world.Source(), loseFirstReply: true}
			service := proposalServiceWithPublisher(t, world, scene, observer)
			receipt := acceptAccounting(t, ctx, service, scene)
			step, err := service.Step(ctx)
			if err != nil {
				t.Fatal(err)
			}
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("reply-loss original Decision unavailable")
			}
			waiting, ok := found.Decision.AsWaiting()
			if !ok || waiting.Reason != "dependency_unavailable" || waiting.Usage.RuleStarts != "1" || waiting.Usage.Cost.IntegerValue != "1" || waiting.Usage.RuleSteps != "1" || !waiting.Usage.MeasurementsComplete {
				t.Fatal("durable handoff/usage missing before publication retry")
			}
			permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
			if err != nil {
				t.Fatal(err)
			}
			if len(observer.planned) == 0 {
				t.Fatal("actual publisher produced no immutable identity")
			}
			first := observer.planned[0]
			body, err := world.Source().ReadPublished(ctx, first.ref, permission)
			if err != nil || !bytes.Equal(body, first.body) {
				t.Fatal("lost reply did not follow actual independent publication", err)
			}
			world.Reopen(ctx)
			service = proposalService(t, world, scene)
			if remaining := time.Until(step.NextWake); remaining > 0 {
				if err := (runtime.WallTimer{}).Wait(ctx, remaining+time.Millisecond); err != nil {
					t.Fatal(err)
				}
			}
			if step, err := service.Step(ctx); err != nil || step.Processed != 1 {
				t.Fatal("replacement did not resume actual prepared output", err)
			}
			view, err = service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok = view.AsFound()
			if !ok {
				t.Fatal("recovered Decision unavailable")
			}
			completed, ok := found.Decision.AsCompleted()
			if !ok || completed.Usage != waiting.Usage || completed.InputDigest != waiting.InputDigest {
				t.Fatal("recovery recalculated, reset observations or charged a second start")
			}
			wantArtifacts := 0
			if rule == "delta_candidate_result" {
				wantArtifacts = 1
			}
			if len(completed.ArtifactRefs) != wantArtifacts {
				t.Fatal("recovery invented or lost an actual artifact")
			}
			permission, err = world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
			if err != nil {
				t.Fatal(err)
			}
			for _, output := range observer.planned {
				body, err := world.Source().ReadPublished(ctx, output.ref, permission)
				if err != nil || !bytes.Equal(body, output.body) {
					t.Fatal("recovery lost the real publisher's exact original identity/body", err)
				}
			}
			command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := command.AsFound()
			if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
				t.Fatal("prepared recovery changed accepted receipt")
			}
		})
	}
}

func TestDurableProposalRuleVersionRetainsUnknownCaseMeaning(t *testing.T) {
	for _, tc := range []struct{ version, rule string }{{"fixture-rule/2", "actions_four"}, {"fixture-rule/3", "undefined_case"}} {
		t.Run(tc.version, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			world := fixture.NewWorld(t, ctx)
			scene, _ := proposalScenario(t, ctx, world, tc.version, tc.rule)
			service := proposalService(t, world, scene)
			receipt := acceptAccounting(t, ctx, service, scene)
			if _, err := service.Step(ctx); err != nil {
				t.Fatal(err)
			}
			world.Reopen(ctx)
			service = proposalService(t, world, scene)
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("unknown fixed case unavailable after reopen")
			}
			failed, ok := found.Decision.AsFailed()
			if !ok || failed.Failure != "proposal_invalid" || failed.Usage.RuleStarts != "1" || failed.Usage.Cost.IntegerValue != "1" || failed.Usage.OutputBytes != "0" {
				t.Fatal("unsupported case fell back or changed its old fixed meaning")
			}
			command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := command.AsFound()
			if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
				t.Fatal("unsupported case changed original acceptance")
			}
		})
	}
}

func TestDurableProposalCandidateSourceEvidenceUsesAllowedPurpose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, snapshot := proposalScenario(t, ctx, world, "fixture-rule/3", "candidate_source_evidence")
	service := proposalService(t, world, scene)
	acceptAccounting(t, ctx, service, scene)
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	world.Reopen(ctx)
	service = proposalService(t, world, scene)
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("candidate source evidence unavailable")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("legal evidence purpose did not complete")
	}
	candidate, ok := completed.Proposal.Advance.AsCandidateResult()
	if !ok || len(candidate.Evidence) != 1 || candidate.Evidence[0].RequirementRef != snapshot.RequirementRefs[0] || len(candidate.Evidence[0].EvidenceRefs) != 1 || candidate.Evidence[0].EvidenceRefs[0] != snapshot.MaterialRefs[0] || len(completed.ArtifactRefs) != 1 {
		t.Fatal("source evidence lost its current condition/material or real artifact")
	}
	permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	if body, err := world.Source().ReadMaterial(ctx, candidate.Evidence[0].EvidenceRefs[0], "rule.evidence", permission, v.MaxBodyBytes); err != nil || string(body) != "alpha\n" {
		t.Fatal("evidence is not actually readable under its allowed purpose", err)
	}
	if _, err := world.Source().ReadPublished(ctx, completed.ProposalRef, permission); err != nil {
		t.Fatal("source-evidence Proposal is not independently readable", err)
	}
}
