package governance_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type knowledgeContent struct{ root string }

func (c knowledgeContent) Read(_ context.Context, _ runtime.Scope, _ runtime.Auth, ref api.ContentRef, _ string) ([]byte, error) {
	return os.ReadFile(filepath.Join(c.root, ref.ContentID))
}
func TestAgentConfigFreezesBrainAndNarrowsParentCapabilityAndControlBounds(t *testing.T) {
	root := t.TempDir()
	f := environment(t, governance.Options{Content: knowledgeContent{root}, KnowledgeGate: knowledgeGate{}})
	f.auth.Roles = nil
	brainRef := api.ComponentRef{ComponentID: api.NewID("brain"), Version: "1", Digest: api.Hash([]byte("registered-brain"))}
	inspect := api.ComponentRef{ComponentID: api.NewID("capability"), Version: "1", Digest: api.Hash([]byte("inspect"))}
	write := api.ComponentRef{ComponentID: api.NewID("capability"), Version: "1", Digest: api.Hash([]byte("write"))}
	agentBounds := governance.KnowledgeControls{MaxInputBytes: 32768, MaxOutputTokens: 2000, MaxActionsPerDecision: 2, MaxDelegationsPerDecision: 1, MaxDepth: 2, MaxActionDurationSeconds: 10, MaxCallCostBound: []api.Amount{{Unit: "usd", Value: "0.02"}}}
	controlRef := knowledgeBytes(t, f, root, api.Raw(agentBounds), "application/json")
	agent, err := governance.SealAgentConfig(governance.AgentConfigDefinition{AgentConfigRef: api.ComponentRef{ComponentID: api.NewID("agent"), Version: "1"}, BrainRef: brainRef, CapabilityRefs: []api.ComponentRef{inspect, write}, ControlLimitsRef: controlRef, SourceRefs: []api.ContentRef{}})
	if err != nil {
		t.Fatal(err)
	}
	_, receipt := command(t, f, "agent_config.register", agent.AgentConfigRef.ComponentID, agent, nil)
	if receipt.Stage != "accepted" {
		t.Fatalf("agent registration %+v", receipt)
	}
	drain(t, f, governance.KnowledgeValidationJob)
	record := query[governance.AgentConfigRecord](t, f, "agent_config.get", governance.AgentConfigReference{AgentConfigRef: agent.AgentConfigRef})
	if record.State != "active" || record.Controls == nil {
		t.Fatalf("validated config %+v", record)
	}
	request := governance.KnowledgeRequest{TaskRef: f.scope.Ref(api.NewID("task"), 1), SnapshotID: api.NewID("snapshot"), BrainRef: brainRef, ParentInstallLockRef: api.ComponentRef{ComponentID: api.NewID("install_lock"), Version: "1", Digest: api.Hash([]byte("original-parent-install"))}, SkillRefs: []api.ComponentRef{}, AgentConfigRef: &agent.AgentConfigRef, CapabilityRefs: []api.ComponentRef{inspect}, ControlLimits: governance.KnowledgeControls{MaxInputBytes: 65536, MaxOutputTokens: 1000, MaxActionsPerDecision: 4, MaxDelegationsPerDecision: 0, MaxDepth: 1, MaxActionDurationSeconds: 30, MaxCallCostBound: []api.Amount{{Unit: "usd", Value: "0.01"}}}, ExpiresAt: api.Time(time.Now().Add(time.Minute))}
	bundle := query[governance.KnowledgeBundle](t, f, "knowledge.load", request)
	effective := bundle.Selection.EffectiveControls
	if len(bundle.Selection.EffectiveCapabilityRefs) != 1 || !api.Equal(bundle.Selection.EffectiveCapabilityRefs[0], inspect) || effective.MaxInputBytes != 32768 || effective.MaxOutputTokens != 1000 || effective.MaxActionsPerDecision != 2 || effective.MaxDelegationsPerDecision != 0 || effective.MaxDepth != 1 || effective.MaxActionDurationSeconds != 10 || len(effective.MaxCallCostBound) != 1 || effective.MaxCallCostBound[0].Value != "0.01" || bundle.Packet.Kind != "ordinary_knowledge/1" {
		t.Fatalf("configuration exceeded original parent ceiling %+v", bundle)
	}
	_, receipt = command(t, f, "agent_config.withdraw", record.ID, governance.KnowledgeChange{Ref: f.scope.Ref(record.ID, record.Revision), Reason: "withdraw original configuration"}, &record.Revision)
	if receipt.Stage != "applied" {
		t.Fatalf("agent withdrawal %+v", receipt)
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: api.NewID("query"), Method: "knowledge.load", TargetID: f.scope.OwnerID, Payload: api.Raw(request)}
	if _, err := f.dispatcher.Query(f.ctx, f.auth, api.Raw(q)); !api.IsCode(err, "invalid_state") {
		t.Fatalf("withdrawn agent was selected %v", err)
	}
	record = query[governance.AgentConfigRecord](t, f, "agent_config.get", governance.AgentConfigReference{AgentConfigRef: agent.AgentConfigRef})
	_, receipt = command(t, f, "agent_config.reopen", record.ID, governance.KnowledgeChange{Ref: f.scope.Ref(record.ID, record.Revision), Reason: "recheck original control bytes"}, &record.Revision)
	if receipt.Stage != "accepted" {
		t.Fatalf("agent reopen %+v", receipt)
	}
	drain(t, f, governance.KnowledgeValidationJob)
	record = query[governance.AgentConfigRecord](t, f, "agent_config.get", governance.AgentConfigReference{AgentConfigRef: agent.AgentConfigRef})
	if record.State != "active" || record.Revision != 5 || !api.Equal(record.Definition, agent) {
		t.Fatalf("agent original version reopened %+v", record)
	}
}

type knowledgeGate struct{}

func (knowledgeGate) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ContentRef, _ string) error {
	return (previewGate{}).CheckTx(ctx, tx, auth, refs)
}
func knowledgeBytes(t *testing.T, f *fixture, root string, b []byte, media string) api.ContentRef {
	t.Helper()
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(b), MediaType: media, ByteLength: uint64(len(b))}
	if err := os.WriteFile(filepath.Join(root, ref.ContentID), b, 0600); err != nil {
		t.Fatal(err)
	}
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		return tx.Create(f.ctx, "governance/fixture_preview", ref.ContentID, f.auth.SubjectID, previewControl{Ref: ref, SubjectID: f.auth.SubjectID, Generation: f.auth.CredentialGeneration})
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("actual content gate fixture %s %v", status, err)
	}
	return ref
}
func exampleSkill(t *testing.T, f *fixture, root string) governance.SkillDefinition {
	t.Helper()
	body := knowledgeBytes(t, f, root, []byte("Inspect the saved bytes before proposing completion. This knowledge cannot issue permissions."), "text/plain")
	usage := knowledgeBytes(t, f, root, api.Raw(governance.SkillUsageContract{Purpose: "file_report", Preconditions: []string{"the original target is disclosed"}, Counterexamples: []string{"a write receipt alone is not a readback"}, ToolDependencies: []api.ComponentRef{}, EvidenceDependencies: []api.ContentRef{}, Conflicts: []api.ComponentRef{}, ExitRules: []string{"stop when original effect is unknown"}}), "application/json")
	in, err := governance.SealSkill(governance.SkillDefinition{SkillRef: api.ComponentRef{ComponentID: api.NewID("skill"), Version: "1"}, BodyRef: body, UsageContractRef: usage, SourceRefs: []api.ContentRef{}})
	if err != nil {
		t.Fatal(err)
	}
	return in
}
func TestCustomSkillRegistersAccurateVersionAndValidatedUsageFromRealBytes(t *testing.T) {
	root := t.TempDir()
	f := environment(t, governance.Options{Content: knowledgeContent{root}, KnowledgeGate: knowledgeGate{}})
	f.auth.Roles = nil // 普通用户的操作知识不能因此获得任何管理或Grant权威。
	in := exampleSkill(t, f, root)
	c, receipt := command(t, f, "skill.register", in.SkillRef.ComponentID, in, nil)
	if receipt.Stage != "accepted" {
		t.Fatalf("custom skill registration %+v", receipt)
	}
	drain(t, f, governance.KnowledgeValidationJob)
	got := query[governance.SkillRecord](t, f, "skill.get", governance.SkillReference{SkillRef: in.SkillRef})
	if got.State != "active" || got.Revision != 2 || !api.Equal(got.Definition, in) || got.Usage == nil || got.Usage.Purpose != "file_report" || got.Usage.ExitRules[0] != "stop when original effect is unknown" || !api.Equal(got.InstallLockRef, governance.SkillInstallLock(in)) {
		t.Fatalf("verified original knowledge %+v", got)
	}
	original, err := f.store.LookupCommand(f.ctx, f.scope, c.CommandID)
	if err != nil || original.Receipt.Stage != "applied" {
		t.Fatalf("original validation responsibility %v %+v", err, original.Receipt)
	}
}

func TestCustomSkillWithdrawsAndReopensOnlyOriginalAccurateVersion(t *testing.T) {
	root := t.TempDir()
	f := environment(t, governance.Options{Content: knowledgeContent{root}, KnowledgeGate: knowledgeGate{}})
	f.auth.Roles = nil
	in := exampleSkill(t, f, root)
	command(t, f, "skill.register", in.SkillRef.ComponentID, in, nil)
	drain(t, f, governance.KnowledgeValidationJob)
	loaded := query[governance.LoadedSkill](t, f, "skill.load", governance.SkillReference{SkillRef: in.SkillRef})
	if loaded.Body != "Inspect the saved bytes before proposing completion. This knowledge cannot issue permissions." || loaded.Usage.Counterexamples[0] != "a write receipt alone is not a readback" {
		t.Fatalf("accurate ordinary knowledge %+v", loaded)
	}
	state := query[governance.SkillRecord](t, f, "skill.get", governance.SkillReference{SkillRef: in.SkillRef})
	_, receipt := command(t, f, "skill.withdraw", state.ID, governance.KnowledgeChange{Ref: f.scope.Ref(state.ID, state.Revision), Reason: "user withdrew the original version"}, &state.Revision)
	if receipt.Stage != "applied" {
		t.Fatalf("withdrawal %+v", receipt)
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: api.NewID("query"), Method: "skill.load", TargetID: f.scope.OwnerID, Payload: api.Raw(governance.SkillReference{SkillRef: in.SkillRef})}
	if _, err := f.dispatcher.Query(f.ctx, f.auth, api.Raw(q)); !api.IsCode(err, "invalid_state") {
		t.Fatalf("withdrawn skill read %v", err)
	}
	state = query[governance.SkillRecord](t, f, "skill.get", governance.SkillReference{SkillRef: in.SkillRef})
	c, receipt := command(t, f, "skill.reopen", state.ID, governance.KnowledgeChange{Ref: f.scope.Ref(state.ID, state.Revision), Reason: "revalidate original source bytes"}, &state.Revision)
	if receipt.Stage != "accepted" {
		t.Fatalf("reopen %+v", receipt)
	}
	drain(t, f, governance.KnowledgeValidationJob)
	state = query[governance.SkillRecord](t, f, "skill.get", governance.SkillReference{SkillRef: in.SkillRef})
	if state.State != "active" || state.Revision != 5 || state.ValidationCommandID != c.CommandID || !api.Equal(state.Definition, in) {
		t.Fatalf("original version reopened %+v", state)
	}
}

func TestSnapshotKnowledgeAdmissionKeepsOriginalHoldersAndBlocksWithdrawnSkill(t *testing.T) {
	root := t.TempDir()
	f := environment(t, governance.Options{Content: knowledgeContent{root}, KnowledgeGate: knowledgeGate{}})
	f.auth.Roles = nil
	skill := exampleSkill(t, f, root)
	command(t, f, "skill.register", skill.SkillRef.ComponentID, skill, nil)
	drain(t, f, governance.KnowledgeValidationJob)
	profile := api.ComponentRef{ComponentID: api.NewID("brain"), Version: "1", Digest: api.Hash([]byte("fixed-brain"))}
	req := governance.KnowledgeRequest{TaskRef: f.scope.Ref(api.NewID("task"), 1), SnapshotID: api.NewID("snapshot"), BrainRef: profile, ParentInstallLockRef: profile, SkillRefs: []api.ComponentRef{skill.SkillRef}, CapabilityRefs: []api.ComponentRef{}, ControlLimits: governance.KnowledgeControls{MaxInputBytes: 65536, MaxOutputTokens: 1000, MaxActionsPerDecision: 2, MaxDelegationsPerDecision: 0, MaxDepth: 0, MaxActionDurationSeconds: 10, MaxCallCostBound: []api.Amount{}}, ExpiresAt: api.Time(time.Now().Add(time.Minute))}
	bundle := query[governance.KnowledgeBundle](t, f, "knowledge.load", req)
	packet := knowledgeBytes(t, f, root, api.Raw(bundle.Packet), "application/vnd.harness.knowledge+json")
	goal := knowledgeBytes(t, f, root, []byte("inspect the original report"), "text/plain")
	sources := append(append([]api.ContentRef{}, bundle.Selection.SourceRefs...), packet, goal)
	snap := api.Snapshot{SnapshotID: req.SnapshotID, Revision: 1, TaskRef: req.TaskRef, GoalRevision: 1, ControlRevision: 1, GoalRef: goal, Requirements: []api.Requirement{}, RequirementsDigest: api.Hash([]byte("requirements")), RequirementsState: "ready", Purpose: "continue_task", FactRefs: []api.ObjectRef{}, UnresolvedCollections: []api.CollectionSummary{}, PolicyRef: profile, InstallLockRef: bundle.Selection.InstallLockRef, ModelProfileRef: profile, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, MaterialRefs: sources, SelectionReportRef: goal, ProcessedSources: sources, InputTokens: 100, ReservedOutputTokens: 100, SafetyMarginTokens: 10, CountMode: "upper_bound", TokenizerRef: profile, EncodedDigest: api.Hash([]byte("encoded"))}
	snapshotRef := knowledgeBytes(t, f, root, api.Raw(snap), "application/vnd.harness.snapshot+json")
	admission := governance.KnowledgeAdmission{Selection: bundle.Selection, Snapshot: snap, SnapshotRef: snapshotRef, PacketRef: packet, DecisionID: api.NewID("decision")}
	stage := func() (governance.KnowledgeCommit, runtime.CommitStatus, error) {
		var result governance.KnowledgeCommit
		status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
			var e error
			result, e = f.svc.StageSelectionTx(f.ctx, tx, f.auth, admission)
			return e
		})
		return result, status, err
	}
	rollback := errors.New("host business admission failed after staging knowledge")
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		if _, err := f.svc.StageSelectionTx(f.ctx, tx, f.auth, admission); err != nil {
			return err
		}
		return rollback
	})
	if status != runtime.RolledBack || !errors.Is(err, rollback) {
		t.Fatalf("atomic rollback %s %v", status, err)
	}
	original, status, err := stage()
	if err != nil || status != runtime.Committed || len(original.HolderRefs) != 4 {
		t.Fatalf("original Task+Decision data holders %s %v %+v", status, err, original)
	}
	replay, status, err := stage()
	if err != nil || status != runtime.Committed || !api.Equal(original, replay) {
		t.Fatalf("same original snapshot must reuse holders %s %v %+v", status, err, replay)
	}
	public := query[governance.KnowledgeCommit](t, f, "knowledge.selection.get", governance.KnowledgeSelectionReference{SnapshotRef: snapshotRef, DecisionID: admission.DecisionID})
	if !api.Equal(public, original) {
		t.Fatalf("published original exact closure %+v", public)
	}
	state := query[governance.SkillRecord](t, f, "skill.get", governance.SkillReference{SkillRef: skill.SkillRef})
	command(t, f, "skill.withdraw", state.ID, governance.KnowledgeChange{Ref: f.scope.Ref(state.ID, state.Revision), Reason: "withdraw before the next physical decision"}, &state.Revision)
	status, err = f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		_, e := f.svc.CheckSelectionTx(f.ctx, tx, f.auth, snapshotRef, admission.DecisionID)
		return e
	})
	if status != runtime.RolledBack || !api.IsCode(err, "invalid_state") {
		t.Fatalf("withdrawn knowledge still dispatched %s %v", status, err)
	}
}
