//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	decisionpg "github.com/ruipengliu/lerna/adapters/postgres/decision_engine"
	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	process "github.com/ruipengliu/lerna/conformance/internal/testkit/process"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

// This private closed child configuration contains exact scope descriptors,
// never a DSN/deletion capability. Only the creating parent seeds/migrates.
type ruleProcessConfig struct {
	Source                         fixture.SourceDescriptor
	DecisionSchema, Scenario, Gate string
	Generation                     int
	Scene                          fixture.Scenario
}
type ruleProcessFrame struct {
	Scenario, Stage          string
	Generation               int
	LeaseUntil               time.Time                     `json:",omitempty"`
	ProposalRef, ArtifactRef *v.ContentRef                 `json:",omitempty"`
	Settings                 string                        `json:",omitempty"`
	Versions                 []decisionpg.MigrationVersion `json:",omitempty"`
}

func newRuleProcessService(store decision.Store, source *fixture.Store, publisher decision.Publisher, scene fixture.Scenario, worker string) (*decision.Service, error) {
	return decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: store, Authority: source, Source: source, Publisher: publisher, Component: scene.Request.Payload.ComponentRef, Worker: worker, Lease: time.Second, PoolControl: true})
}

type completionMarkerKey struct{}
type completionMarker struct{ stage *ruleProcessFrame }

// This wrapper observes the actual consumer transaction callback. It does not
// replace Commit or manufacture a completed record: the real callback must
// finish SaveDecision and Complete successfully before the pre-COMMIT hold.
type heldRuleStore struct {
	*decisionpg.Store
	cfg   ruleProcessConfig
	pipes *process.Inherited
}

func (s *heldRuleStore) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	marker := &completionMarker{}
	err := s.Store.Within(ctx, owner, func(txctx context.Context, tx runtime.Tx) error {
		txctx = context.WithValue(txctx, completionMarkerKey{}, marker)
		if err := fn(txctx, tx); err != nil {
			return err
		}
		if marker.stage == nil || s.cfg.Gate != "completed_staged_before_commit" {
			return nil
		}
		return s.hold(txctx, *marker.stage)
	})
	if err != nil {
		return err
	}
	if marker.stage != nil && s.cfg.Gate == "completed_commit_before_reply" {
		// Core has actually returned successful Commit. Its transaction context
		// is now cancelled; the original finite child context owns this reply hold.
		return s.hold(ctx, *marker.stage)
	}
	return nil
}

func (s *heldRuleStore) hold(ctx context.Context, stage ruleProcessFrame) error {
	if err := s.pipes.Emit(ctx, stage); err != nil {
		return err
	}
	var release ruleProcessFrame
	if err := s.pipes.Receive(ctx, &release); err != nil {
		return err
	}
	if release.Stage != "release" || release.Scenario != s.cfg.Scenario || release.Generation != s.cfg.Generation {
		return errors.New("wrong original completion gate release")
	}
	return nil
}

func (s *heldRuleStore) SaveDecision(ctx context.Context, tx runtime.Tx, record decision.Record) error {
	if err := s.Store.SaveDecision(ctx, tx, record); err != nil {
		return err
	}
	if record.Status != "completed" || record.Ref != s.cfg.Scene.DecisionRef {
		return nil
	}
	marker, ok := ctx.Value(completionMarkerKey{}).(*completionMarker)
	if !ok || record.ProposalRef == nil || len(record.ArtifactRefs) != 1 {
		return errors.New("original completed callback identity missing")
	}
	proposal, artifact := *record.ProposalRef, record.ArtifactRefs[0]
	marker.stage = &ruleProcessFrame{Scenario: s.cfg.Scenario, Generation: s.cfg.Generation, Stage: s.cfg.Gate, ProposalRef: &proposal, ArtifactRef: &artifact}
	return nil
}

// This private concrete publisher delegates the actual independent Source
// commit, then holds its successful original Proposal before worker Finish.
type heldRulePublisher struct {
	decision.Publisher
	cfg      ruleProcessConfig
	pipes    *process.Inherited
	artifact *v.ContentRef
}

func (p *heldRulePublisher) Publish(ctx context.Context, key string, body []byte, sources []v.ContentRef, permission decision.Permission) (v.ContentRef, error) {
	ref, err := p.Publisher.Publish(ctx, key, body, sources, permission)
	if err != nil {
		return ref, err
	}
	if strings.HasSuffix(key, "/artifact") {
		p.artifact = &ref
	}
	if strings.HasSuffix(key, "/proposal") {
		if p.artifact == nil {
			return ref, errors.New("original artifact publication missing before Proposal")
		}
		stage := ruleProcessFrame{Scenario: p.cfg.Scenario, Generation: p.cfg.Generation, Stage: "published_before_finish", ProposalRef: &ref, ArtifactRef: p.artifact}
		if err = p.pipes.Emit(ctx, stage); err != nil {
			return ref, err
		}
		var release ruleProcessFrame
		if err = p.pipes.Receive(ctx, &release); err != nil {
			return ref, err
		}
		if release.Stage != "release" || release.Scenario != p.cfg.Scenario || release.Generation != p.cfg.Generation {
			return ref, errors.New("wrong original publication gate release")
		}
	}
	return ref, nil
}

func TestDecisionRuleProcess(t *testing.T) {
	if os.Getenv("LERNA_DECISION_RULE_PROCESS") != "1" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	pipes, err := process.OpenInherited()
	if pipes != nil {
		defer func() {
			if e := pipes.Close(); e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	var cfg ruleProcessConfig
	if err = pipes.Receive(ctx, &cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Scenario == "" || cfg.Generation < 1 || (cfg.Gate != "" && cfg.Gate != "published_before_finish" && cfg.Gate != "completed_staged_before_commit" && cfg.Gate != "completed_commit_before_reply") {
		t.Fatal("invalid normal rule process configuration")
	}
	connection := postgres.Config{DSN: os.Getenv("LERNA_TEST_POSTGRES_DSN"), Schema: cfg.Source.Schema, MaxOpenConnections: 4, TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
	source, err := fixture.Open(ctx, connection, cfg.Source.Owner)
	if source != nil {
		defer func() {
			if e := source.Close(); e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	connection.Schema = cfg.DecisionSchema
	store, err := decisionpg.Open(ctx, connection)
	if store != nil {
		defer func() {
			if e := store.Close(); e != nil {
				t.Error(e)
			}
		}()
	}
	if err != nil {
		t.Fatal(err)
	}
	var publisher decision.Publisher = source
	if cfg.Gate == "published_before_finish" {
		publisher = &heldRulePublisher{Publisher: source, cfg: cfg, pipes: pipes}
	}
	var consumerStore decision.Store = store
	if cfg.Gate == "completed_staged_before_commit" || cfg.Gate == "completed_commit_before_reply" {
		consumerStore = &heldRuleStore{Store: store, cfg: cfg, pipes: pipes}
	}
	service, err := newRuleProcessService(consumerStore, source, publisher, cfg.Scene, fmt.Sprintf("rule-process-%d", cfg.Generation))
	if err != nil {
		t.Fatal(err)
	}
	frame := func(stage string) ruleProcessFrame {
		return ruleProcessFrame{Scenario: cfg.Scenario, Generation: cfg.Generation, Stage: stage}
	}
	ready := frame("configured")
	err = store.Within(ctx, contract.OwnerRef{TenantID: contract.ID(cfg.Scene.DecisionRef.TenantID), OwnerID: contract.ID(cfg.Scene.DecisionRef.OwnerID)}, func(ctx context.Context, tx runtime.Tx) error {
		settings, e := store.Settings(ctx, tx)
		if e == nil {
			ready.Settings = fmt.Sprintf("version=%s sync=%s isolation=%s", settings.ServerVersion, settings.SynchronousCommit, settings.Isolation)
		}
		return e
	})
	if err != nil {
		t.Fatal(err)
	}
	ready.Versions, err = store.MigrationVersions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err = pipes.Emit(ctx, ready); err != nil {
		t.Fatal(err)
	}
	var command ruleProcessFrame
	if err = pipes.Receive(ctx, &command); err != nil {
		t.Fatal(err)
	}
	if command.Scenario != cfg.Scenario || command.Generation != cfg.Generation || command.Stage != "run" {
		t.Fatal("wrong original Decision child run")
	}
	work, err := service.Claim(ctx)
	if err != nil || work == nil {
		t.Fatal("normal original Decision Claim:", err)
	}
	claimed := frame("claimed")
	claimed.LeaseUntil = work.Claim.LeaseUntil
	if err = pipes.Emit(ctx, claimed); err != nil {
		t.Fatal(err)
	}
	if err = service.RunClaim(ctx, work.Claim); err != nil {
		t.Fatal(err)
	}
	if err = pipes.Respond(ctx, frame("decision_reply")); err != nil {
		t.Fatal(err)
	}
}

func TestDecisionSIGKILLBeforeCompletionCommitRestoresOriginalProposal(t *testing.T) {
	for _, kill := range []bool{false, true} {
		name := "normal_release"
		if kill {
			name = "SIGKILL_before_completed_commit"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			service := w.Service()
			raw, err := v.Encode(scene.Request)
			if err != nil {
				t.Fatal(err)
			}
			out, err := service.Decide(ctx, raw, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			received, ok := out.AsReceived()
			if !ok {
				t.Fatal("original fixed receipt missing")
			}
			if _, ok = received.Receipt.AsAccepted(); !ok {
				t.Fatal("original Decision rejected")
			}
			child, claimed := startRuleChild(t, w, ctx, "completed_staged_before_commit", 1)
			var stage ruleProcessFrame
			if err = child.Event(ctx, &stage); err != nil {
				t.Fatal("actual completed SQL/pre-COMMIT gate missing:", err)
			}
			if stage.Stage != "completed_staged_before_commit" || stage.Scenario != "original-rule-decision" || stage.Generation != 1 || stage.ProposalRef == nil || stage.ArtifactRef == nil {
				t.Fatal("wrong original staged completion identity")
			}
			proposalBytes, err := w.ReadArtifact(ctx, *stage.ProposalRef)
			if err != nil {
				t.Fatal("independent original Proposal publication absent", err)
			}
			artifact, err := w.ReadArtifact(ctx, *stage.ArtifactRef)
			if err != nil || string(artifact) != "fixture result: alpha\n" {
				t.Fatal("independent original artifact absent", err)
			}
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("original staged Decision absent")
			}
			running, ok := found.Decision.AsRunning()
			if !ok {
				t.Fatal("staged completed became visible before held actual COMMIT")
			}
			if kill {
				killCtx, killCancel := context.WithTimeout(ctx, 2*time.Second)
				defer killCancel()
				if err = child.KillWait(killCtx); err != nil {
					t.Fatal(err)
				}
				var reply ruleProcessFrame
				if err = child.Reply(killCtx, &reply); err != io.EOF {
					t.Fatal("killed staged Decision returned reply", err)
				}
				confirmed, closeErr := child.Stop(killCtx)
				if !confirmed || closeErr != nil {
					t.Fatal("staged Decision exit/pipe cleanup unconfirmed", closeErr)
				}
			} else {
				if err = child.Send(ctx, ruleProcessFrame{Stage: "release", Scenario: stage.Scenario, Generation: 1}); err != nil {
					t.Fatal(err)
				}
				finishRuleChild(t, ctx, child, 1)
			}
			w.Reopen(ctx)
			service = w.Service()
			if kill {
				view, err = service.Get(ctx, scene.GetJSON, &scene.Subject)
				if err != nil {
					t.Fatal(err)
				}
				found, ok = view.AsFound()
				if !ok {
					t.Fatal("original Decision absent after pre-COMMIT SIGKILL")
				}
				if _, ok = found.Decision.AsRunning(); !ok {
					t.Fatal("uncommitted completed survived actual process kill")
				}
				if delay := time.Until(claimed.LeaseUntil.Add(time.Millisecond)); delay > 0 {
					if err = (runtime.WallTimer{}).Wait(ctx, delay); err != nil {
						t.Fatal(err)
					}
				}
				step, stepErr := service.Step(ctx)
				if stepErr != nil || step.Processed != 1 {
					t.Fatal("original prepared Proposal did not resume", stepErr)
				}
			}
			view, err = service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok = view.AsFound()
			if !ok {
				t.Fatal("original completed Decision absent")
			}
			completed, ok := found.Decision.AsCompleted()
			if !ok || completed.ProposalRef != *stage.ProposalRef || completed.Proposal.DecisionRef != scene.DecisionRef || completed.Usage != running.Usage || completed.Usage.RuleStarts != "1" || completed.Usage.Cost.IntegerValue != "1" {
				t.Fatal("pre-COMMIT recovery changed original Proposal or confirmed usage")
			}
			candidate, ok := completed.Proposal.Advance.AsCandidateResult()
			if !ok || len(candidate.ArtifactRefs) != 1 || candidate.ArtifactRefs[0] != *stage.ArtifactRef {
				t.Fatal("pre-COMMIT recovery produced another artifact identity")
			}
			proposalAfter, err := w.ReadArtifact(ctx, completed.ProposalRef)
			if err != nil || !sameRuleJSON(proposalBytes, proposalAfter) {
				t.Fatal("original published Proposal changed", err)
			}
			query, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := query.AsFound()
			if !ok {
				t.Fatal("original accepted receipt unavailable")
			}
			a, err := v.Encode(received.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			b, err := v.Encode(fixed.Receipt)
			if err != nil || !sameRuleJSON(a, b) {
				t.Fatal("pre-COMMIT recovery changed fixed receipt", err)
			}
		})
	}
}

func startRuleChild(t *testing.T, w *fixture.World, ctx context.Context, gate string, generation int) (*process.Child, ruleProcessFrame) {
	t.Helper()
	childCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	t.Cleanup(cancel)
	child, err := process.New(childCtx, "TestDecisionRuleProcess", "LERNA_DECISION_RULE_PROCESS=1")
	if child != nil {
		// New has already acquired physical FDs. This independent holder owns
		// them even if the two-owner borrow refuses before Start is permitted.
		t.Cleanup(func() {
			cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cleanupCancel()
			confirmed, closeErr := child.Stop(cleanupCtx)
			if closeErr != nil || !confirmed {
				t.Error("Decision child physical cleanup:", closeErr, "confirmed:", confirmed)
			}
		})
		if e := w.BorrowChild(child); e != nil {
			t.Fatal(e)
		}
	}
	if err != nil {
		t.Fatal(err)
	}
	if err = child.Start(); err != nil {
		t.Fatal(err)
	}
	cfg := ruleProcessConfig{Source: w.ProcessSource(), DecisionSchema: w.Config().Schema, Scene: w.Scenario(), Scenario: "original-rule-decision", Gate: gate, Generation: generation}
	if err = child.Send(childCtx, cfg); err != nil {
		t.Fatal(err)
	}
	var ready ruleProcessFrame
	if err = child.Event(childCtx, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Scenario != cfg.Scenario || ready.Generation != generation || ready.Stage != "configured" || len(ready.Versions) != 2 || ready.Settings == "" {
		t.Fatal("wrong actual Rule2 child configuration/settings")
	}
	t.Logf("Decision child generation=%d scenario=%s source=%s decision=%s actual=%s migrations=%v", generation, cfg.Scenario, cfg.Source.Schema, cfg.DecisionSchema, ready.Settings, ready.Versions)
	if err = child.Send(childCtx, ruleProcessFrame{Scenario: cfg.Scenario, Generation: generation, Stage: "run"}); err != nil {
		t.Fatal(err)
	}
	var claimed ruleProcessFrame
	if err = child.Event(childCtx, &claimed); err != nil {
		t.Fatal(err)
	}
	if claimed.Stage != "claimed" || claimed.Scenario != cfg.Scenario || claimed.Generation != generation || claimed.LeaseUntil.IsZero() {
		t.Fatal("actual original Claim identity/lease unavailable")
	}
	return child, claimed
}

func finishRuleChild(t *testing.T, ctx context.Context, child *process.Child, generation int) {
	t.Helper()
	var reply ruleProcessFrame
	if err := child.Reply(ctx, &reply); err != nil {
		t.Fatal(err)
	}
	if reply.Stage != "decision_reply" || reply.Scenario != "original-rule-decision" || reply.Generation != generation {
		t.Fatal("wrong actual normal Decision reply")
	}
	confirmed, err := child.Wait(ctx)
	if !confirmed || err != nil {
		t.Fatal("normal Decision child exit:", err)
	}
	confirmed, err = child.Stop(ctx)
	if !confirmed || err != nil {
		t.Fatal("normal Decision child physical cleanup:", err)
	}
}

func TestDecisionNormalChildPublishesOriginalProposalAndReceipt(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	service := w.Service()
	raw, err := v.Encode(scene.Request)
	if err != nil {
		t.Fatal(err)
	}
	out, err := service.Decide(ctx, raw, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := out.AsReceived()
	if !ok {
		t.Fatal("original Decision acceptance missing")
	}
	if _, ok = received.Receipt.AsAccepted(); !ok {
		t.Fatal("original Decision rejected")
	}
	child, _ := startRuleChild(t, w, ctx, "", 1)
	finishRuleChild(t, ctx, child, 1)
	w.Reopen(ctx)
	service = w.Service()
	view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := view.AsFound()
	if !ok {
		t.Fatal("original Decision after child exit missing")
	}
	completed, ok := found.Decision.AsCompleted()
	if !ok || completed.Proposal.DecisionRef != scene.DecisionRef || completed.Proposal.SnapshotRef != scene.Request.Payload.SnapshotRef || completed.Usage.RuleStarts != "1" || completed.Usage.RuleSteps != "1" || completed.Usage.Cost.IntegerValue != "1" || !completed.Usage.MeasurementsComplete {
		t.Fatal("original normal Proposal/usage identity changed")
	}
	candidate, ok := completed.Proposal.Advance.AsCandidateResult()
	if !ok || len(candidate.ArtifactRefs) != 1 {
		t.Fatal("normal Rule2 candidate absent")
	}
	body, err := w.ReadArtifact(ctx, candidate.ArtifactRefs[0])
	if err != nil || string(body) != "fixture result: alpha\n" {
		t.Fatal("independent publisher original artifact readback:", err)
	}
	proposalBytes, err := w.ReadArtifact(ctx, completed.ProposalRef)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := v.Decode[v.Proposal](proposalBytes)
	if err != nil || proposal.DecisionRef != scene.DecisionRef {
		t.Fatal("independent published Proposal identity changed", err)
	}
	query, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := query.AsFound()
	if !ok {
		t.Fatal("original fixed accepted receipt missing")
	}
	a, err := v.Encode(received.Receipt)
	if err != nil {
		t.Fatal(err)
	}
	b, err := v.Encode(fixed.Receipt)
	if err != nil || !sameRuleJSON(a, b) {
		t.Fatal("normal child/reopen changed fixed receipt", err)
	}
}

func sameRuleJSON(a, b []byte) bool { return string(a) == string(b) }

func TestDecisionSIGKILLAfterCompletionCommitRetainsOriginalReplyFact(t *testing.T) {
	for _, kill := range []bool{false, true} {
		name := "normal_release"
		if kill {
			name = "SIGKILL_response_unknown"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			service := w.Service()
			raw, err := v.Encode(scene.Request)
			if err != nil {
				t.Fatal(err)
			}
			out, err := service.Decide(ctx, raw, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			received, ok := out.AsReceived()
			if !ok {
				t.Fatal("original fixed receipt missing")
			}
			if _, ok = received.Receipt.AsAccepted(); !ok {
				t.Fatal("original Decision rejected")
			}
			child, _ := startRuleChild(t, w, ctx, "completed_commit_before_reply", 1)
			var stage ruleProcessFrame
			if err = child.Event(ctx, &stage); err != nil {
				t.Fatal("actual completed COMMIT/pre-reply gate missing:", err)
			}
			if stage.Stage != "completed_commit_before_reply" || stage.Scenario != "original-rule-decision" || stage.Generation != 1 || stage.ProposalRef == nil || stage.ArtifactRef == nil {
				t.Fatal("wrong committed original Decision identity")
			}
			// Independent public Get must see completed before the child returns.
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("original committed Decision missing")
			}
			completed, ok := found.Decision.AsCompleted()
			if !ok || completed.ProposalRef != *stage.ProposalRef || completed.Proposal.DecisionRef != scene.DecisionRef {
				t.Fatal("held stage preceded actual completed COMMIT")
			}
			original, err := v.Encode(found.Decision)
			if err != nil {
				t.Fatal(err)
			}
			proposalBytes, err := w.ReadArtifact(ctx, completed.ProposalRef)
			if err != nil {
				t.Fatal("independent committed Proposal publication missing", err)
			}
			candidate, ok := completed.Proposal.Advance.AsCandidateResult()
			if !ok || len(candidate.ArtifactRefs) != 1 || candidate.ArtifactRefs[0] != *stage.ArtifactRef {
				t.Fatal("original committed artifact identity missing")
			}
			artifact, err := w.ReadArtifact(ctx, candidate.ArtifactRefs[0])
			if err != nil || string(artifact) != "fixture result: alpha\n" {
				t.Fatal("independent original artifact bytes missing", err)
			}
			if kill {
				killCtx, killCancel := context.WithTimeout(ctx, 2*time.Second)
				defer killCancel()
				if err = child.KillWait(killCtx); err != nil {
					t.Fatal(err)
				}
				var reply ruleProcessFrame
				if err = child.Reply(killCtx, &reply); err != io.EOF {
					t.Fatal("killed committed Decision returned reply", err)
				}
				confirmed, closeErr := child.Stop(killCtx)
				if !confirmed || closeErr != nil {
					t.Fatal("committed Decision exit/pipe cleanup unknown", closeErr)
				}
			} else {
				if err = child.Send(ctx, ruleProcessFrame{Stage: "release", Scenario: stage.Scenario, Generation: 1}); err != nil {
					t.Fatal(err)
				}
				finishRuleChild(t, ctx, child, 1)
			}
			w.Reopen(ctx)
			service = w.Service()
			step, err := service.Step(ctx)
			if err != nil || step.Processed != 0 {
				t.Fatal("completed original Decision was processed again", err)
			}
			view, err = service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok = view.AsFound()
			if !ok {
				t.Fatal("original completed Decision absent after reopen")
			}
			after, err := v.Encode(found.Decision)
			if err != nil || !sameRuleJSON(original, after) {
				t.Fatal("original committed Proposal/usage/revision changed", err)
			}
			proposalAfter, err := w.ReadArtifact(ctx, completed.ProposalRef)
			if err != nil || !sameRuleJSON(proposalBytes, proposalAfter) {
				t.Fatal("original committed Proposal bytes changed", err)
			}
			artifactAfter, err := w.ReadArtifact(ctx, candidate.ArtifactRefs[0])
			if err != nil || !sameRuleJSON(artifact, artifactAfter) {
				t.Fatal("original committed artifact bytes changed", err)
			}
			query, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := query.AsFound()
			if !ok {
				t.Fatal("original accepted receipt absent after committed kill")
			}
			a, err := v.Encode(received.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			b, err := v.Encode(fixed.Receipt)
			if err != nil || !sameRuleJSON(a, b) {
				t.Fatal("committed kill changed original fixed receipt", err)
			}
		})
	}
}

func TestDecisionSIGKILLAfterPublicationRecoversOriginalRefs(t *testing.T) {
	for _, kill := range []bool{false, true} {
		name := "normal_release"
		if kill {
			name = "SIGKILL_before_Decision_references"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			service := w.Service()
			raw, err := v.Encode(scene.Request)
			if err != nil {
				t.Fatal(err)
			}
			out, err := service.Decide(ctx, raw, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			received, ok := out.AsReceived()
			if !ok {
				t.Fatal("original fixed receipt missing")
			}
			if _, ok = received.Receipt.AsAccepted(); !ok {
				t.Fatal("original Decision rejected")
			}
			child, claimed := startRuleChild(t, w, ctx, "published_before_finish", 1)
			var stage ruleProcessFrame
			if err = child.Event(ctx, &stage); err != nil {
				t.Fatal("actual post-publication/pre-Finish gate missing:", err)
			}
			if stage.Stage != "published_before_finish" || stage.Scenario != "original-rule-decision" || stage.Generation != 1 || stage.ProposalRef == nil || stage.ArtifactRef == nil {
				t.Fatal("actual publication identity unavailable")
			}
			// These reads use an independent Source connection and authorization.
			// The publisher has committed while Decision still reports running.
			artifact, err := w.ReadArtifact(ctx, *stage.ArtifactRef)
			if err != nil || string(artifact) != "fixture result: alpha\n" {
				t.Fatal("independent published artifact absent", err)
			}
			proposalBytes, err := w.ReadArtifact(ctx, *stage.ProposalRef)
			if err != nil {
				t.Fatal(err)
			}
			proposal, err := v.Decode[v.Proposal](proposalBytes)
			if err != nil || proposal.DecisionRef != scene.DecisionRef {
				t.Fatal("independent original Proposal absent", err)
			}
			view, err := service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := view.AsFound()
			if !ok {
				t.Fatal("original staged Decision absent")
			}
			running, ok := found.Decision.AsRunning()
			if !ok {
				t.Fatal("Decision references completed before held Finish")
			}
			if kill {
				killCtx, killCancel := context.WithTimeout(ctx, 2*time.Second)
				defer killCancel()
				if err = child.KillWait(killCtx); err != nil {
					t.Fatal(err)
				}
				var reply ruleProcessFrame
				if err = child.Reply(killCtx, &reply); err != io.EOF {
					t.Fatal("killed Decision returned a reply", err)
				}
				confirmed, closeErr := child.Stop(killCtx)
				if !confirmed || closeErr != nil {
					t.Fatal("killed Decision physical cleanup unconfirmed", closeErr)
				}
			} else {
				if err = child.Send(ctx, ruleProcessFrame{Stage: "release", Scenario: stage.Scenario, Generation: 1}); err != nil {
					t.Fatal(err)
				}
				finishRuleChild(t, ctx, child, 1)
			}
			w.Reopen(ctx)
			service = w.Service()
			if kill {
				// This is the returned actual claim deadline, not a guessed stage.
				if delay := time.Until(claimed.LeaseUntil.Add(time.Millisecond)); delay > 0 {
					if err = (runtime.WallTimer{}).Wait(ctx, delay); err != nil {
						t.Fatal(err)
					}
				}
				step, stepErr := service.Step(ctx)
				if stepErr != nil || step.Processed != 1 {
					t.Fatal("original prepared work did not recover", stepErr)
				}
			}
			view, err = service.Get(ctx, scene.GetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			found, ok = view.AsFound()
			if !ok {
				t.Fatal("original recovered Decision absent")
			}
			completed, ok := found.Decision.AsCompleted()
			if !ok || completed.ProposalRef != *stage.ProposalRef || completed.Proposal.DecisionRef != scene.DecisionRef || completed.Usage != running.Usage || completed.Usage.RuleStarts != "1" || completed.Usage.Cost.IntegerValue != "1" {
				t.Fatal("recovery changed original Proposal identity or repeated calculation/charge")
			}
			candidate, ok := completed.Proposal.Advance.AsCandidateResult()
			if !ok || len(candidate.ArtifactRefs) != 1 || candidate.ArtifactRefs[0] != *stage.ArtifactRef {
				t.Fatal("recovery created a different artifact identity")
			}
			artifactAfter, err := w.ReadArtifact(ctx, candidate.ArtifactRefs[0])
			if err != nil || !sameRuleJSON(artifact, artifactAfter) {
				t.Fatal("original artifact bytes changed", err)
			}
			proposalAfter, err := w.ReadArtifact(ctx, completed.ProposalRef)
			if err != nil || !sameRuleJSON(proposalBytes, proposalAfter) {
				t.Fatal("original Proposal bytes changed", err)
			}
			query, err := service.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := query.AsFound()
			if !ok {
				t.Fatal("original accepted receipt unavailable")
			}
			a, err := v.Encode(received.Receipt)
			if err != nil {
				t.Fatal(err)
			}
			b, err := v.Encode(fixed.Receipt)
			if err != nil || !sameRuleJSON(a, b) {
				t.Fatal("recovery changed original fixed receipt", err)
			}
		})
	}
}
