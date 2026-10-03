package brain_test

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

// 原执行负责方已确认未开始；真实 SQLite 中的原 Decision 必须发布失败提案，
// 不能因 nil Result 解码错误卡住，也不能重做相同 logical step。
func TestRuleDecisionClosesWhenOriginalVerificationNeverStarted(t *testing.T) {
	ctx := context.Background()
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "brain.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	content := &contents{items: map[string][]byte{}}
	fixture := newReportRuleFixture(scope, content)
	fixture.facts[2].Operation.Effect = "not_started"
	fixture.facts[2].Operation.ResultRef = nil
	fixture.facts[2].ResultBytes = nil
	service, err := brain.New(brain.Config{Profiles: []brain.Profile{fixture.profile}, Content: content, Engine: fixture.engine(), Gate: gate{}})
	if err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	if err = service.Register(registry); err != nil {
		t.Fatal(err)
	}
	dispatch := runtime.Dispatcher{Store: store, OwnerID: scope.OwnerID, Registry: registry}
	snapshotRef := content.add(scope, "snapshot", api.Raw(fixture.snapshot))
	decisionID := api.NewID("decision")
	deadline := api.Time(time.Now().Add(time.Minute))
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: api.NewID("command"), TargetID: decisionID, Method: "brain.decide", ExpiresAt: deadline, Payload: api.Raw(brain.DecideInput{DecisionID: decisionID, TaskRef: fixture.snapshot.TaskRef, SnapshotRef: snapshotRef, SnapshotRevision: 1, ModelProfileRef: fixture.profile.Ref, UseRefs: []api.ObjectRef{}, Limits: []api.Amount{{Unit: "USD", Value: "1"}}, Deadline: deadline})}
	receipt, err := dispatch.Command(ctx, auth, api.Raw(command))
	if err != nil || receipt.Stage != "accepted" {
		t.Fatalf("accept original decision: %+v %v", receipt, err)
	}
	for range 10 {
		if err = runtime.Drain(ctx, store, scope, registry, 20); err != nil {
			t.Fatalf("closed original fact must produce a fail proposal: %v", err)
		}
	}
	view, err := service.Get(ctx, store, scope, auth, decisionID)
	if err != nil {
		t.Fatal(err)
	}
	if view.Decision.Status != "completed" || view.Decision.ProposalRef == nil || view.Decision.PhysicalRequestCount != 0 || view.Decision.SendStarted || !view.Decision.UsageFinal {
		t.Fatalf("original decision did not close without a physical call: %+v", view)
	}
	proposalBytes, err := content.Read(ctx, scope, auth, *view.Decision.ProposalRef, "brain.output")
	if err != nil {
		t.Fatal(err)
	}
	var proposal brain.Proposal
	if err = api.Decode(proposalBytes, &proposal); err != nil {
		t.Fatal(err)
	}
	validator, err := api.NewValidator(brain.ProposalSchema())
	if err != nil || validator.Validate(proposalBytes) != nil {
		t.Fatalf("fail proposal must satisfy the public closed schema: %s %v", proposalBytes, err)
	}
	if proposal.Kind != "fail" || proposal.ReasonCode != "file_readback_not_started" || len(proposal.Actions) != 0 || len(proposal.ArtifactRefs) != 0 {
		t.Fatalf("known not_started must fail without repeating verify_file: %+v", proposal)
	}
	reason, err := content.Read(ctx, scope, auth, proposal.ReasonRef, "brain.output")
	if err != nil || !strings.Contains(string(reason), "verify_file") || !strings.Contains(string(reason), "not_started") {
		t.Fatalf("reason must explain the exact failed original step: %q %v", reason, err)
	}
	duplicate, err := dispatch.Command(ctx, auth, api.Raw(command))
	if err != nil || duplicate.Stage != "applied" {
		t.Fatalf("original command must replay its completed receipt: %+v %v", duplicate, err)
	}
	if err = runtime.Drain(ctx, store, scope, registry, 20); err != nil {
		t.Fatal(err)
	}
	replayed, err := service.Get(ctx, store, scope, auth, decisionID)
	if err != nil || !api.Equal(replayed, view) {
		t.Fatalf("recovery must preserve the original closed decision: %+v %v", replayed, err)
	}
}

func TestRuleEngineClosesKnownOriginalFailures(t *testing.T) {
	for index, prefix := range []string{"file_inspection", "file_write", "file_readback"} {
		for _, failure := range []string{"not_started", "result_missing", "result_bytes_missing"} {
			t.Run(prefix+"/"+failure, func(t *testing.T) {
				scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: "rule_fixture"}
				fixture := newReportRuleFixture(scope, &contents{items: map[string][]byte{}})
				fact := &fixture.facts[index]
				wantCode := prefix + "_result_missing"
				switch failure {
				case "not_started":
					// Even stale bytes that happen to match the goal cannot turn a
					// confirmed not_started operation into successful completion.
					fact.Operation.Effect = "not_started"
					wantCode = prefix + "_not_started"
				case "result_missing":
					fact.Operation.ResultRef = nil
				case "result_bytes_missing":
					fact.ResultBytes = nil
				}
				engine := fixture.engine()
				encoding, err := engine.Encode(context.Background(), fixture.snapshot, fixture.goal, fixture.profile)
				if err != nil {
					t.Fatal(err)
				}
				generated, err := engine.Request(context.Background(), "original_rule_call", encoding)
				if err != nil || generated.Draft.Kind != "fail" || generated.Draft.ReasonCode != wantCode || len(generated.Draft.Actions) != 0 || len(generated.Draft.ArtifactLocalIDs) != 0 || len(generated.Draft.ExistingArtifactRefs) != 0 {
					t.Fatalf("closed known %s must yield a failure without another action: %+v %v", failure, generated.Draft, err)
				}
				explained := false
				for _, output := range generated.Contents {
					if output.LocalID == generated.Draft.ReasonLocalID {
						explained = strings.Contains(output.Body, fact.LogicalStepKey) && strings.Contains(output.Body, fact.Operation.OperationID)
					}
				}
				if !explained {
					t.Fatal("failure reason must name the original step and operation")
				}
			})
		}
	}
}

func TestRuleEngineClosesInvalidOriginalResults(t *testing.T) {
	for _, test := range []struct {
		name     string
		index    int
		result   []byte
		effect   string
		wantCode string
	}{
		{"inspection_invalid_json", 0, []byte(`{"path":`), "not_applied", "file_observation_invalid"},
		{"inspection_wrong_path", 0, []byte(`{"path":"other.md","version":"missing"}`), "not_applied", "file_observation_invalid"},
		{"write_not_applied", 1, []byte(`{"path":"report.md"}`), "not_applied", "file_write_not_applied"},
		{"readback_invalid_json", 2, []byte(`{"path":`), "not_applied", "file_readback_invalid"},
		{"readback_wrong_bytes", 2, []byte(`{"path":"report.md","version":"sha256:changed","data_base64":"Y2hhbmdlZA=="}`), "not_applied", "file_readback_mismatch"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: "rule_fixture"}
			content := &contents{items: map[string][]byte{}}
			fixture := newReportRuleFixture(scope, content)
			resultRef := content.add(scope, "result", test.result)
			fixture.facts[test.index].Operation.ResultRef = &resultRef
			fixture.facts[test.index].Operation.Effect = test.effect
			fixture.facts[test.index].ResultBytes = test.result
			engine := fixture.engine()
			encoding, err := engine.Encode(context.Background(), fixture.snapshot, fixture.goal, fixture.profile)
			if err != nil {
				t.Fatal(err)
			}
			generated, err := engine.Request(context.Background(), "original_rule_call", encoding)
			if err != nil || generated.Draft.Kind != "fail" || generated.Draft.ReasonCode != test.wantCode || len(generated.Draft.Actions) != 0 {
				t.Fatalf("known closed invalid original result must fail: %+v %v", generated.Draft, err)
			}
			for _, output := range generated.Contents {
				if output.LocalID == generated.Draft.ReasonLocalID && !strings.Contains(output.Body, fixture.facts[test.index].Operation.OperationID) {
					t.Fatal("reason did not explain the failed original Operation")
				}
			}
		})
	}
}

func TestRuleEnginePreservesReportProgressAndIndependentReadback(t *testing.T) {
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: "rule_fixture"}
	content := &contents{items: map[string][]byte{}}
	fixture := newReportRuleFixture(scope, content)
	for count, wantAction := range []string{"inspect_file", "save_report", "verify_file"} {
		t.Run(wantAction, func(t *testing.T) {
			partial := fixture
			partial.facts = fixture.facts[:count]
			partial.snapshot.FactRefs = fixture.snapshot.FactRefs[:count]
			engine := partial.engine()
			encoding, err := engine.Encode(context.Background(), partial.snapshot, partial.goal, partial.profile)
			if err != nil {
				t.Fatal(err)
			}
			generated, err := engine.Request(context.Background(), "original_rule_call", encoding)
			if err != nil || generated.Draft.Kind != "act" || len(generated.Draft.Actions) != 1 || generated.Draft.Actions[0].LocalKey != wantAction {
				t.Fatalf("successful original stages must advance once: %+v %v", generated.Draft, err)
			}
		})
	}
	// The target file and independent read establish the successful example;
	// the expected bytes do not come from the engine under test.
	path := filepath.Join(t.TempDir(), "report.md")
	if err := os.WriteFile(path, []byte("# Fixture report\n\nAccurate body.\n"), 0600); err != nil {
		t.Fatal(err)
	}
	readback, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	result := api.Raw(map[string]string{"path": "report.md", "version": api.Hash(readback), "data_base64": base64.StdEncoding.EncodeToString(readback), "observed_at": "2026-10-03T00:00:00Z"})
	resultRef := content.add(scope, "result", result)
	fixture.facts[2].Operation.ResultRef = &resultRef
	fixture.facts[2].ResultBytes = result
	artifactRef := content.add(scope, "artifact", readback)
	artifactRef.MediaType = "text/markdown"
	fixture.snapshot.MaterialRefs = []api.ContentRef{artifactRef}
	engine := fixture.engine()
	encoding, err := engine.Encode(context.Background(), fixture.snapshot, fixture.goal, fixture.profile)
	if err != nil {
		t.Fatal(err)
	}
	generated, err := engine.Request(context.Background(), "original_rule_call", encoding)
	if err != nil || generated.Draft.Kind != "complete" || len(generated.Draft.Actions) != 0 || len(generated.Draft.ArtifactLocalIDs) != 0 || !api.Equal(generated.Draft.ExistingArtifactRefs, []api.ContentRef{artifactRef}) {
		t.Fatalf("valid independent readonly Result must preserve the original checked artifact: %+v %v", generated.Draft, err)
	}
}

func TestRuleEngineWaitsForOriginalUnresolvedEffects(t *testing.T) {
	for _, test := range []struct {
		name   string
		state  string
		effect string
		later  any
	}{
		{"closed_unknown", "closed", "unknown", false},
		{"open_unknown", "running", "unknown", true},
		{"future_effect", "closed", "not_applied", true},
		{"unknown_future_effect", "closed", "not_applied", "unknown"},
	} {
		t.Run(test.name, func(t *testing.T) {
			scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: "rule_fixture"}
			fixture := newReportRuleFixture(scope, &contents{items: map[string][]byte{}})
			fixture.facts[2].Operation.ExecutionState = test.state
			fixture.facts[2].Operation.Effect = test.effect
			fixture.facts[2].Operation.MayApplyLater = test.later
			fixture.facts[2].Operation.ResultRef = nil
			fixture.facts[2].ResultBytes = nil
			engine := fixture.engine()
			encoding, err := engine.Encode(context.Background(), fixture.snapshot, fixture.goal, fixture.profile)
			if err != nil {
				t.Fatal(err)
			}
			generated, err := engine.Request(context.Background(), "original_rule_call", encoding)
			if !api.IsCode(err, "dependency_unavailable") || len(generated.Draft.Actions) != 0 {
				t.Fatalf("unresolved original effect must remain pending without repeating the step: %+v %v", generated.Draft, err)
			}
		})
	}
}

// FactSource 是执行负责方的原事实边界；夹具不替换 Brain 规则或持久工作。
type ruleFacts []brain.ActionFact

func (facts ruleFacts) Operations(context.Context, api.Snapshot) ([]brain.ActionFact, error) {
	return append([]brain.ActionFact{}, facts...), nil
}

type reportRuleFixture struct {
	profile  brain.Profile
	snapshot api.Snapshot
	goal     []byte
	facts    ruleFacts
}

func newReportRuleFixture(scope runtime.Scope, content *contents) reportRuleFixture {
	profile := brain.Profile{Ref: api.ComponentRef{ComponentID: api.NewID("profile"), Version: "1", Digest: api.Hash([]byte("rule-fixture"))}, ContextLimit: 20000, MaxInputTokens: 19000, MaxOutputTokens: 1000, SafetyMargin: 10, MaxInputBytes: 65536, RequestTimeout: time.Second}
	goal := []byte(`{"kind":"report","title":"Fixture report","body":"Accurate body.","save_path":"report.md"}`)
	goalRef := content.add(scope, "goal", goal)
	snapshot := api.Snapshot{SnapshotID: api.NewID("snapshot"), Revision: 1, TaskRef: scope.Ref(api.NewID("task"), 1), GoalRevision: 1, ControlRevision: 1, GoalRef: goalRef, Requirements: []api.Requirement{}, RequirementsDigest: api.Hash([]byte("requirements")), RequirementsState: "ready", Purpose: "continue_task", FactRefs: []api.ObjectRef{}, UnresolvedCollections: []api.CollectionSummary{}, PolicyRef: profile.Ref, InstallLockRef: profile.Ref, ModelProfileRef: profile.Ref, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, MaterialRefs: []api.ContentRef{}, SelectionReportRef: goalRef, ProcessedSources: []api.ContentRef{goalRef}, InputTokens: 100, ReservedOutputTokens: 100, SafetyMarginTokens: 10, CountMode: "upper_bound", TokenizerRef: profile.Ref, EncodedDigest: api.Hash([]byte("sealed"))}
	// Read-only success has not_applied plus an immutable Result. The expected
	// report bytes are a literal fixture independent of RuleEngine.ReportBytes.
	readResult := api.Raw(struct {
		Path       string `json:"path"`
		Version    string `json:"version"`
		DataBase64 string `json:"data_base64"`
		ObservedAt string `json:"observed_at"`
	}{"report.md", "sha256:observed", base64.StdEncoding.EncodeToString([]byte("# Fixture report\n\nAccurate body.\n")), "2026-10-03T00:00:00Z"})
	facts := ruleFacts{}
	for _, step := range []string{"inspect_file", "save_report", "verify_file"} {
		result := readResult
		effect := "not_applied"
		if step == "save_report" {
			result = []byte(`{"path":"report.md","version":"sha256:saved"}`)
			effect = "applied"
		}
		ref := content.add(scope, "result", result)
		id := api.NewID("operation")
		fact := brain.ActionFact{Ref: scope.Ref(id, 1), LogicalStepKey: snapshot.TaskRef.ObjectID + "/goal_1/" + step, Operation: api.Operation{OperationID: id, OwnerID: scope.OwnerID, TaskRef: snapshot.TaskRef, Revision: 1, ExecutionState: "closed", Effect: effect, MayApplyLater: false, Attempts: api.CollectionSummary{CollectionRevision: 1, Complete: true}, EvidenceRefs: []api.ContentRef{}, ResultRef: &ref, Usage: []api.Amount{{Unit: "USD", Value: "0"}}, UsageFinal: true, NextAction: "none"}, ResultBytes: result}
		facts = append(facts, fact)
		snapshot.FactRefs = append(snapshot.FactRefs, fact.Ref)
	}
	return reportRuleFixture{profile, snapshot, goal, facts}
}

func (f reportRuleFixture) engine() *brain.RuleEngine {
	return &brain.RuleEngine{Facts: f.facts, ReadCapability: f.profile.Ref, WriteCapability: f.profile.Ref, ReadBinding: f.snapshot.TaskRef, WriteBinding: f.snapshot.TaskRef}
}
