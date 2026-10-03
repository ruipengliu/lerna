package brain_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/runtime"
)

func TestRuleEngineRawTextGoalKeepsOriginalBytesInClarification(t *testing.T) {
	ctx := context.Background()
	raw := []byte("请生成报告。\n原文不是 JSON；保留 \"引号\" 和路径 /reports/原文.md。")
	goal := api.ContentRef{TenantID: "tenant_original", OwnerID: "owner_original", ContentID: "content_original", Version: 1, Hash: api.Hash(raw), MediaType: "text/plain", ByteLength: uint64(len(raw))}
	snapshot := api.Snapshot{GoalRef: goal, Purpose: "interpret_requirements", ProcessedSources: []api.ContentRef{goal}}
	resolver := &originalGoalResolver{t: t, expected: raw, ref: goal}
	answerSchema := api.ComponentRef{ComponentID: "component_goal_schema", Version: "1", Digest: api.Hash([]byte("goal-schema"))}
	engine := &brain.RuleEngine{Goals: resolver, AnswerSchema: answerSchema}
	encoding, err := engine.Encode(ctx, snapshot, raw, brain.Profile{})
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(encoding.Body) || encoding.Digest != api.Hash(encoding.Body) || encoding.InputTokens != uint64(len(encoding.Body)) || encoding.CountMode != "upper_bound" {
		t.Fatal("original non-JSON bytes must have a bounded, sealed JSON envelope")
	}
	generated, err := engine.Request(ctx, "original_rule_call", encoding)
	if err != nil || generated.Draft.Kind != "request_input" || generated.Draft.Purpose != "clarify_goal" || generated.Draft.AnswerSchemaRef == nil || !api.Equal(*generated.Draft.AnswerSchemaRef, answerSchema) || resolver.calls != 1 {
		t.Fatalf("original text must request a registered clarification: %+v %v", generated.Draft, err)
	}
	if len(generated.Draft.Actions) != 0 || len(generated.Draft.Requirements) != 0 || len(generated.Draft.ArtifactLocalIDs) != 0 || !generated.UsageFinal {
		t.Fatal("uninterpreted text must not admit actions, conditions or success")
	}
}

type originalGoalResolver struct {
	t        *testing.T
	expected []byte
	ref      api.ContentRef
	calls    int
}

func TestRuleEngineRejectsAmbiguousGoalEncodings(t *testing.T) {
	engine := &brain.RuleEngine{}
	for _, body := range []string{
		`{"snapshot":{},"goal":{"kind":"answer","body":"Original"},"goal_bytes":"Y2hhbmdlZA=="}`,
		`{"snapshot":{},"goal":{"kind":"answer","body":"Original"},"goal_bytes":null}`,
		`{"snapshot":{},"goal":null,"goal_bytes":"Y2hhbmdlZA=="}`,
		`{"snapshot":{}}`,
		`{"snapshot":{},"goal_bytes":null}`,
		`{"snapshot":{},"goal_bytes":"broken-base64"}`,
	} {
		t.Run(body, func(t *testing.T) {
			_, err := engine.Request(context.Background(), "original_call", brain.Encoding{Body: []byte(body), Digest: api.Hash([]byte(body))})
			if !api.IsCode(err, "invalid_request") {
				t.Fatalf("exactly one original goal representation is required: %v", err)
			}
		})
	}
}

func (r *originalGoalResolver) ResolveGoal(_ context.Context, snapshot api.Snapshot, original json.RawMessage) (json.RawMessage, error) {
	r.calls++
	if string(original) != string(r.expected) || !api.Equal(snapshot.GoalRef, r.ref) || api.Hash(original) != r.ref.Hash {
		r.t.Fatal("sealed goal changed the original raw bytes, digest or ContentRef")
	}
	return original, nil
}

// 首次版本封存合法 JSON 的原格式；DB 重开后只消费此编码，不读/重编当前目标。
func TestLegacyRuleEncodingRecoversOriginalPublicDecisionWithoutReencoding(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	path := filepath.Join(root, "brain.sqlite")
	store := openBrainStore(t, path)
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: store.ID()}
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"service"}}
	content := &fileContents{root: root}
	fixture := newReportRuleFixture(scope, &contents{items: map[string][]byte{}})
	goalBytes := []byte(`{"kind":"answer","body":"The original legacy answer."}`)
	goalRef, err := content.put(scope, api.NewID("goal"), "application/json", goalBytes)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := fixture.snapshot
	snapshot.GoalRef = goalRef
	snapshot.Purpose = "interpret_requirements"
	snapshot.ProcessedSources, snapshot.MaterialRefs = []api.ContentRef{goalRef}, []api.ContentRef{goalRef}
	initialEngine := fixture.engine()
	initialEngine.ArtifactRule = fixture.profile.Ref
	legacy := &legacyRuleEncoder{Engine: initialEngine}
	_, registry := ruleEncodingService(t, fixture.profile, content, legacy)
	command := acceptProviderDecision(t, store, scope, auth, registry, content, snapshot)
	work, status, err := store.Claim(ctx, scope, api.NewID("worker"), []string{brain.JobAdvance}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(work) != 1 {
		t.Fatalf("original encode responsibility %s %v", status, err)
	}
	handler, _ := registry.Job(brain.JobAdvance)
	if err = handler(ctx, store, scope, work[0]); err != nil {
		t.Fatal(err)
	}
	if len(legacy.original.Body) == 0 {
		t.Fatal("public decision must seal its original legacy encoding")
	}
	current := fixture.engine()
	current.ArtifactRule = fixture.profile.Ref
	fresh, err := current.Encode(ctx, snapshot, goalBytes, fixture.profile)
	if err != nil || string(fresh.Body) != string(legacy.original.Body) || fresh.Digest != legacy.original.Digest {
		t.Fatalf("valid JSON must retain exactly the legacy envelope: %v", err)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(filepath.Join(root, goalRef.ContentID+".blob")); err != nil {
		t.Fatal(err)
	}
	reopened := openBrainStore(t, path)
	recovery := &recoverRuleEncoding{Engine: current}
	service, registry := ruleEncodingService(t, fixture.profile, content, recovery)
	view := drainBrainUntil(t, reopened, scope, auth, service, registry, command.TargetID, "completed")
	if recovery.reencoded || !api.Equal(recovery.original, legacy.original) || view.Decision.PhysicalRequestCount != 0 || view.Decision.SendStarted || view.Decision.ProposalRef == nil {
		t.Fatalf("restart must use exact original encoding without another input read: %+v", view)
	}
	proposalBytes, err := content.Read(ctx, scope, auth, *view.Decision.ProposalRef, "brain.output")
	var proposal brain.Proposal
	if err != nil || api.Decode(proposalBytes, &proposal) != nil || proposal.Kind != "refine_requirements" || proposal.RequirementDelta == nil || len(proposal.RequirementDelta.Candidates) != 1 {
		t.Fatalf("legacy original goal must still produce its valid proposal: %+v %v", proposal, err)
	}
	dispatch := runtime.Dispatcher{Store: reopened, OwnerID: scope.OwnerID, Registry: registry}
	duplicate, err := dispatch.Command(ctx, auth, api.Raw(command))
	if err != nil || duplicate.Stage != "applied" || duplicate.CommandID != command.CommandID {
		t.Fatalf("legacy original command must replay its receipt: %+v %v", duplicate, err)
	}
}

func ruleEncodingService(t *testing.T, profile brain.Profile, content brain.Content, engine brain.Engine) (*brain.Service, *runtime.Registry) {
	t.Helper()
	service, err := brain.New(brain.Config{Profiles: []brain.Profile{profile}, Content: content, Engine: engine, Gate: gate{}})
	if err != nil {
		t.Fatal(err)
	}
	registry := runtime.NewRegistry()
	if err = service.Register(registry); err != nil {
		t.Fatal(err)
	}
	return service, registry
}

type legacyRuleEncoder struct {
	brain.Engine
	original brain.Encoding
}

func (e *legacyRuleEncoder) Encode(_ context.Context, snapshot api.Snapshot, goal []byte, _ brain.Profile) (brain.Encoding, error) {
	snapshot.EncodedDigest, snapshot.InputTokens = "", 0
	body := api.Raw(struct {
		Snapshot api.Snapshot    `json:"snapshot"`
		Goal     json.RawMessage `json:"goal"`
	}{snapshot, goal})
	e.original = brain.Encoding{Body: body, Digest: api.Hash(body), Receiver: "builtin-rule-engine", Location: "cloud", InputTokens: uint64(len(body)), CountMode: "upper_bound", ProcessedSources: snapshot.ProcessedSources}
	return e.original, nil
}

type recoverRuleEncoding struct {
	brain.Engine
	original  brain.Encoding
	reencoded bool
}

func (e *recoverRuleEncoding) Encode(context.Context, api.Snapshot, []byte, brain.Profile) (brain.Encoding, error) {
	e.reencoded = true
	return brain.Encoding{}, api.E("invalid_state", "legacy_encoding_must_not_be_rebuilt")
}
func (e *recoverRuleEncoding) Request(ctx context.Context, callID string, original brain.Encoding) (brain.Generated, error) {
	e.original = original
	return e.Engine.Request(ctx, callID, original)
}
