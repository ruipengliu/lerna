//go:build integration

package component_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strconv"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

// Each case owns a separate real Source/Decision scope and a single binding.
// Source.Seed fixes the case and all bytes before the public command is sent.
func proposalScenario(t *testing.T, ctx context.Context, world *fixture.World, version, rule string) (fixture.Scenario, decision.Snapshot) {
	t.Helper()
	scene := world.Scenario()
	source := world.Source()
	permission, err := source.Authorize(ctx, scene.Subject, scene.DecisionRef, "start", &scene.Request.Payload)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.ReadSnapshot(ctx, scene.Request.Payload.SnapshotRef, permission, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	material, err := source.ReadMaterial(ctx, scene.MaterialRef, "rule.input", permission, v.MaxBodyBytes)
	if err != nil {
		t.Fatal(err)
	}
	digest := func(body []byte) v.SchemaDigest {
		sum := sha256.Sum256(body)
		return v.SchemaDigest("sha256:" + hex.EncodeToString(sum[:]))
	}
	scene.DecisionRef.ID = "proposal-decision"
	snapshot.Raw = nil
	snapshot.Ref.ID = "proposal-snapshot"
	snapshot.Rule = rule
	snapshot.ComponentRef.ArtifactDigest = digest([]byte(version))
	snapshot.ComponentRef.ConfigDigest = digest([]byte(rule))
	snapshot.ComponentRef.InstallLockRef.ID = "proposal-lock"
	ruleBytes := []byte(`{"kind":"exact_match","expected":"alpha"}`)
	ruleRef := source.Ref("proposal-condition-rule", "application/json", ruleBytes)
	snapshot.MaterialRefs = append(snapshot.MaterialRefs, ruleRef)
	materials := []fixture.Material{{Ref: scene.MaterialRef, Bytes: material}, {Ref: ruleRef, Bytes: ruleBytes}}
	purposes := []string{"decide", "get", "command.get", "start", "material", "rule.input", "fixture.lock", "publish", "proposal.publish", "artifact.publish", "rule.condition"}
	if rule == "actions_four" || rule == "invalid_actions_depends_on" || rule == "invalid_actions_binding_pair" {
		for i := 1; i <= 4; i++ {
			id := v.ID("slot-" + strconv.Itoa(i))
			body := []byte(`{"target":"` + string(id) + `","operation":"read"}`)
			ref := source.Ref(v.ID("arguments-"+string(id)), "application/json", body)
			materials = append(materials, fixture.Material{Ref: ref, Bytes: body})
			snapshot.MaterialRefs = append(snapshot.MaterialRefs, ref)
			snapshot.CapabilityBindings = append(snapshot.CapabilityBindings, decision.CapabilityBinding{CapabilityRef: v.CapabilityRef{TenantID: snapshot.Ref.TenantID, OwnerID: snapshot.Ref.OwnerID, Kind: "capability", ID: "fixture-read", Revision: "1"}, BindingRef: v.BindingRef{TenantID: snapshot.Ref.TenantID, OwnerID: snapshot.Ref.OwnerID, Kind: "binding", ID: id, Revision: "1"}, ArgumentsRef: ref, Purpose: "fixture.read"})
		}
		purposes = append(purposes, "fixture.read")
	}
	if rule == "input_request" {
		question := []byte("Which source should the report compare?")
		questionRef := source.Ref("proposal-question", "text/plain", question)
		schema := []byte(`{"type":"string","maxLength":256}`)
		schemaRef := source.Ref("proposal-answer-schema", "application/schema+json", schema)
		materials = append(materials, fixture.Material{Ref: questionRef, Bytes: question}, fixture.Material{Ref: schemaRef, Bytes: schema})
		snapshot.MaterialRefs = append(snapshot.MaterialRefs, questionRef)
		snapshot.AnswerSchemaRefs = []v.ContentRef{schemaRef}
		purposes = append(purposes, "rule.question", "rule.answer_schema", "rule.preview")
	}
	permission.ComponentRef = snapshot.ComponentRef
	permission.RuleVersion = version
	scene.ManifestRef, err = source.Seed(ctx, fixture.Bundle{DecisionRef: scene.DecisionRef, Permission: permission, Snapshot: snapshot, Materials: materials, Purposes: purposes, RuleVersion: version, ChargeBasis: permission.ChargeBasis, RuleStartCharge: permission.RuleStartCharge})
	if err != nil {
		t.Fatal(err)
	}
	scene.Request.CommandID = "proposal-command"
	scene.Request.Target = scene.DecisionRef
	scene.Request.Payload.DecisionID = scene.DecisionRef.ID
	scene.Request.Payload.SnapshotRef = snapshot.Ref
	scene.Request.Payload.ComponentRef = snapshot.ComponentRef
	scene.GetJSON, err = v.Encode(v.DecisionGetRequest{ContractVersion: v.Version, Profile: "decision_engine", CommandID: "read-proposal", Target: scene.DecisionRef, Method: "decision_engine.get", AcceptBefore: scene.Request.AcceptBefore, Payload: v.DecisionGetPayload{DecisionRef: scene.DecisionRef}})
	if err != nil {
		t.Fatal(err)
	}
	scene.CommandGetJSON, err = v.Encode(v.CommandGetRequest{ContractVersion: v.Version, Profile: "command", CommandID: "read-proposal-command", Target: v.CommandTarget{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID, Kind: "command", ID: scene.Request.CommandID}, Method: "command.get", AcceptBefore: scene.Request.AcceptBefore, Payload: v.CommandGetPayload{CommandRef: v.CommandRef{Owner: permission.DecisionOwner, CommandID: scene.Request.CommandID}}})
	if err != nil {
		t.Fatal(err)
	}
	return scene, snapshot
}

func proposalService(t *testing.T, world *fixture.World, scene fixture.Scenario) *decision.Service {
	t.Helper()
	service, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: world.Store(), Authority: world.Source(), Source: world.Source(), Publisher: world.Source(), Component: scene.Request.Payload.ComponentRef, Worker: "proposal-worker", Lease: 5 * time.Second, PoolControl: true})
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func TestDurableProposalDeltaOnlyHasNoInventedArtifact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, snapshot := proposalScenario(t, ctx, world, "fixture-rule/3", "delta_only")
	service := proposalService(t, world, scene)
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := outcome.AsReceived()
	if !ok {
		t.Fatal("new fixed case not received")
	}
	if _, ok := received.Receipt.AsAccepted(); !ok {
		t.Fatal("new fixed case not accepted")
	}
	world.Reopen(ctx)
	service = proposalService(t, world, scene)
	if _, err := service.Step(ctx); err != nil {
		t.Fatal(err)
	}
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("original case unavailable")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("delta-only case did not complete")
	}
	if _, ok := completed.Proposal.Advance.AsNone(); !ok {
		t.Fatal("delta-only case must have no advance")
	}
	if len(completed.ArtifactRefs) != 0 || len(completed.Proposal.RequirementDelta) != 1 {
		t.Fatal("delta-only proposal invented an artifact or lost its delta")
	}
	delta := completed.Proposal.RequirementDelta[0]
	if delta.StatementRef != scene.MaterialRef || delta.RuleRef != snapshot.MaterialRefs[1] || delta.ReplacesRef == nil || *delta.ReplacesRef != snapshot.RequirementRefs[0] {
		t.Fatal("delta did not bind the actual statement/rule/current condition")
	}
	if completed.Proposal.GoalRevision != "1" || completed.Proposal.ControlRevision != "1" || completed.Usage.RuleStarts != "1" || completed.Usage.Cost.IntegerValue != "1" {
		t.Fatal("proposal adopted Task revisions or charged more than its one actual start")
	}
	permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := world.Source().ReadPublished(ctx, completed.ProposalRef, permission)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := v.Decode[v.Proposal](bytes)
	if err != nil {
		t.Fatal(err)
	}
	if len(decoded.ProcessedSourceRefs) != 3 {
		t.Fatal("published proposal lost its manifest or actually read material")
	}
}

func TestDurableProposalFourIndependentActions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, snapshot := proposalScenario(t, ctx, world, "fixture-rule/3", "actions_four")
	service := proposalService(t, world, scene)
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := outcome.AsReceived()
	if !ok {
		t.Fatal("actions input not received")
	}
	if _, ok := received.Receipt.AsAccepted(); !ok {
		t.Fatal("actions input not accepted")
	}
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
		t.Fatal("original actions Decision unavailable after reopen")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("four independent actions did not complete")
	}
	advance, ok := completed.Proposal.Advance.AsActions()
	if !ok || len(advance.Actions) != 4 || len(completed.ArtifactRefs) != 0 || len(completed.Proposal.ProcessedSourceRefs) != 7 {
		t.Fatal("actions output lost its bounded branch or complete processed sources")
	}
	seen := map[v.ID]bool{}
	for i, action := range advance.Actions {
		binding := snapshot.CapabilityBindings[i]
		if seen[action.LocalKey] || action.CapabilityRef != binding.CapabilityRef || action.BindingRef != binding.BindingRef || action.ArgumentsRef != binding.ArgumentsRef || action.Purpose != "fixture.read" {
			t.Fatal("action identity, binding, arguments or purpose was not accurate and independent")
		}
		seen[action.LocalKey] = true
	}
	permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := world.Source().ReadMaterial(ctx, advance.Actions[0].ArgumentsRef, "fixture.read", permission, v.MaxBodyBytes)
	if err != nil || string(body) != `{"target":"slot-1","operation":"read"}` {
		t.Fatal("action did not retain readable pre-existing arguments", err)
	}
	if completed.Usage.RuleStarts != "1" || completed.Usage.ModelRequests != "0" {
		t.Fatal("one fixture evaluation became repeated execution or a physical model request")
	}
}

func TestDurableProposalInputRequestIsClarificationWithReadableSchema(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, snapshot := proposalScenario(t, ctx, world, "fixture-rule/3", "input_request")
	service := proposalService(t, world, scene)
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := outcome.AsReceived()
	if !ok {
		t.Fatal("input clarification not received")
	}
	if _, ok := received.Receipt.AsAccepted(); !ok {
		t.Fatal("input clarification not accepted")
	}
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
		t.Fatal("original clarification unavailable")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("clarification proposal did not complete")
	}
	request, ok := completed.Proposal.Advance.AsInputRequest()
	if !ok || request.Purpose != "clarification" || request.AnswerSchemaRef != snapshot.AnswerSchemaRefs[0] || len(request.PreviewRefs) != 1 || request.PreviewRefs[0] != scene.MaterialRef || len(completed.ArtifactRefs) != 0 {
		t.Fatal("request expanded into authorization, invented an artifact or lost its fixed schema/preview")
	}
	permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	question, err := world.Source().ReadMaterial(ctx, request.QuestionRef, "rule.question", permission, v.MaxBodyBytes)
	if err != nil || string(question) != "Which source should the report compare?" {
		t.Fatal("question not bound to actual readable bytes", err)
	}
	schema, err := world.Source().ReadMaterial(ctx, request.AnswerSchemaRef, "rule.answer_schema", permission, v.MaxBodyBytes)
	if err != nil || string(schema) != `{"type":"string","maxLength":256}` {
		t.Fatal("schema not bound to actual readable bytes", err)
	}
	if len(completed.Proposal.ProcessedSourceRefs) != 5 {
		t.Fatal("schema outside MaterialRefs omitted from actually processed sources")
	}
}

func TestDurableProposalDeltaWithCandidatePreservesBothSuggestions(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, snapshot := proposalScenario(t, ctx, world, "fixture-rule/3", "delta_candidate_result")
	service := proposalService(t, world, scene)
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := outcome.AsReceived()
	if !ok {
		t.Fatal("delta/candidate not received")
	}
	if _, ok := received.Receipt.AsAccepted(); !ok {
		t.Fatal("delta/candidate not accepted")
	}
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
		t.Fatal("delta/candidate unavailable")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("legal delta plus candidate did not complete")
	}
	candidate, ok := completed.Proposal.Advance.AsCandidateResult()
	if !ok || len(candidate.ArtifactRefs) != 1 || len(completed.Proposal.RequirementDelta) != 1 || len(completed.ArtifactRefs) != 1 || completed.ArtifactRefs[0] != candidate.ArtifactRefs[0] {
		t.Fatal("legal candidate/delta lost one suggestion or its real artifact")
	}
	if len(candidate.Evidence) != 1 || candidate.Evidence[0].RequirementRef != snapshot.RequirementRefs[0] || len(candidate.Evidence[0].EvidenceRefs) != 1 || candidate.Evidence[0].EvidenceRefs[0] != candidate.ArtifactRefs[0] {
		t.Fatal("candidate evidence does not bind the current condition and actual artifact")
	}
	permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := world.Source().ReadPublished(ctx, candidate.ArtifactRefs[0], permission)
	if err != nil || string(body) != "fixture result: alpha\n" {
		t.Fatal("candidate artifact is not independently readable after reopen", err)
	}
	proposalBody, err := world.Source().ReadPublished(ctx, completed.ProposalRef, permission)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := v.Decode[v.Proposal](proposalBody)
	if err != nil || len(proposal.RequirementDelta) != 1 || proposal.GoalRevision != "1" || proposal.ControlRevision != "1" {
		t.Fatal("published delta was adopted as Task authority or lost", err)
	}
	if completed.Usage.RuleStarts != "1" || completed.Usage.RuleSteps != "1" || completed.Usage.Cost.IntegerValue != "1" {
		t.Fatal("one actual fixture evaluation/fee was not preserved")
	}
}

func TestDurableProposalCannotContinueCompletesWithoutTaskVerdict(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, snapshot := proposalScenario(t, ctx, world, "fixture-rule/3", "cannot_continue")
	service := proposalService(t, world, scene)
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	outcome, err := service.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := outcome.AsReceived()
	if !ok {
		t.Fatal("cannot-continue input not received")
	}
	if _, ok := received.Receipt.AsAccepted(); !ok {
		t.Fatal("cannot-continue input not accepted")
	}
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
		t.Fatal("original cannot-continue unavailable")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok {
		t.Fatal("cannot_continue is a completed proposal, not a failed Decision or Task verdict")
	}
	cannot, ok := completed.Proposal.Advance.AsCannotContinue()
	if !ok || cannot.Reason != "fixture has no further applicable action" || len(cannot.MissingRequirements) != 1 || cannot.MissingRequirements[0] != snapshot.RequirementRefs[0] || len(completed.ArtifactRefs) != 0 {
		t.Fatal("cannot_continue lost its precise bounded reason/gap or invented an artifact")
	}
	permission, err := world.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := world.Source().ReadPublished(ctx, completed.ProposalRef, permission)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := v.Decode[v.Proposal](body)
	if err != nil || proposal.GoalRevision != "1" || proposal.ControlRevision != "1" || len(proposal.ProcessedSourceRefs) != 3 {
		t.Fatal("cannot_continue adopted Task authority or omitted its complete sources", err)
	}
}

func TestDurableProposalFixedDependsOnOutputFailsThroughPublicCodec(t *testing.T) {
	assertFixedProposalFailure(t, "invalid_actions_depends_on")
}

func TestDurableProposalRejectsValidBindingRefsInWrongArgumentPair(t *testing.T) {
	assertFixedProposalFailure(t, "invalid_actions_binding_pair")
}

func assertFixedProposalFailure(t *testing.T, rule string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	world := fixture.NewWorld(t, ctx)
	scene, _ := proposalScenario(t, ctx, world, "fixture-rule/3", rule)
	service := proposalService(t, world, scene)
	receipt := acceptAccounting(t, ctx, service, scene)
	if step, err := service.Step(ctx); err != nil || step.Processed != 1 {
		t.Fatal("fixed erroneous output did not finish its original job", err)
	}
	world.Reopen(ctx)
	service = proposalService(t, world, scene)
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("fixed erroneous Decision unavailable after reopen")
	}
	failed, ok := found.Decision.AsFailed()
	if !ok || failed.Failure != "proposal_invalid" {
		t.Fatal("fixed invalid output entered a consumable completed Proposal")
	}
	outputBytes, err := strconv.ParseInt(string(failed.Usage.OutputBytes), 10, 64)
	if err != nil || outputBytes <= 0 {
		t.Fatal("fixed erroneous output was never generated for the public decoder", err)
	}
	if failed.Usage.RuleStarts != "1" || failed.Usage.RuleSteps != "1" || failed.Usage.Cost.IntegerValue != "1" || !failed.Usage.MeasurementsComplete || failed.Input.ComponentRef != scene.Request.Payload.ComponentRef {
		t.Fatal("failure erased its fixed binding, actual evaluation or original fee")
	}
	command, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := command.AsFound()
	if !ok || !reflect.DeepEqual(fixed.Receipt, receipt) {
		t.Fatal("erroneous proposal replaced the accepted receipt")
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
		t.Fatal("replay refreshed admission for fixed erroneous output")
	}
	if step, err := service.Step(ctx); err != nil || step.Processed != 0 {
		t.Fatal("terminal erroneous proposal retained an unbounded repair job", err)
	}
}
