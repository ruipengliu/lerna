package development

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	execadapter "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 通过实际宿主的公开 Command/Query 与不可变 Content，不预填 Skill 状态。
func TestSkillCatalogValidatesExactPublishedBytesThroughPublicHost(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			a, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			skill, body := registerPublishedKnowledgeSkill(t, ctx, a)
			raw, err := a.query(ctx, "skill.load", skill.SkillRef.ComponentID, governance.SkillReference{SkillRef: skill.SkillRef})
			var loaded governance.LoadedSkill
			if err != nil || api.Decode(raw, &loaded) != nil || loaded.Body != body || !api.Equal(loaded.Definition, skill) || len(loaded.Usage.Counterexamples) != 1 || len(loaded.Usage.ExitRules) != 1 {
				t.Fatalf("original published Skill bytes and contract changed: %v %+v", err, loaded)
			}
		})
	}
}

func TestConfiguredKnowledgeUsesActualTaskSnapshotAndOrdinaryModelMaterial(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredKnowledge(t, driver)
		})
	}
}

func runConfiguredKnowledge(t *testing.T, driver string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var sends atomic.Int32
	wireBody := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxJSONBytes+1))
		if err != nil {
			t.Error(err)
			return
		}
		wireBody <- body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(knowledgeContractReply())
	}))
	defer server.Close()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "synthetic-contract-token")
	cfg.Model = contractModelConfig(server.URL)
	a, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	skill, skillBody := registerPublishedKnowledgeSkill(t, ctx, a)
	agentLimits := governance.KnowledgeControls{MaxInputBytes: 65536, MaxOutputTokens: 128, MaxActionsPerDecision: 1, MaxDelegationsPerDecision: 0, MaxDepth: 0, MaxActionDurationSeconds: 2, MaxCallCostBound: []api.Amount{{Unit: "USD", Value: "0.2"}}}
	limits, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(agentLimits), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	agent, err := governance.SealAgentConfig(governance.AgentConfigDefinition{AgentConfigRef: api.ComponentRef{ComponentID: api.NewID("agent"), Version: "1"}, BrainRef: a.Profile.Ref, CapabilityRefs: []api.ComponentRef{execadapter.FileReadCapability().Ref}, ControlLimitsRef: limits, SourceRefs: []api.ContentRef{}})
	if err != nil {
		t.Fatal(err)
	}
	knowledgePublicCommand(t, ctx, a, "agent_config.register", agent.AgentConfigRef.ComponentID, agent, nil)
	if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 200); err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Knowledge = &KnowledgeConfig{SkillRefs: []api.ComponentRef{skill.SkillRef}, AgentConfigRef: &agent.AgentConfigRef, ControlLimits: governance.KnowledgeControls{MaxInputBytes: api.MaxJSONBytes, MaxOutputTokens: 512, MaxActionsPerDecision: 4, MaxDelegationsPerDecision: 2, MaxDepth: 2, MaxActionDurationSeconds: 30, MaxCallCostBound: []api.Amount{{Unit: "USD", Value: "1"}}}}
	a, err = OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "text/plain", []byte("请依据准确知识检查报告是否可以继续。"), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	taskID := api.NewID("task")
	knowledgePublicCommand(t, ctx, a, "task.submit", taskID, task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}, RequirementCandidates: []api.RequirementCandidate{}}, nil)
	for {
		if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300); err != nil {
			t.Fatal(err)
		}
		got, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "failed" && !got.AccountingOpen {
			if len(got.Budget) != 1 || got.Budget[0].Spent != "0.00024" || got.Budget[0].Reserved != "0" || got.ResultRef != nil {
				t.Fatalf("knowledge changed actual failure or known fee: %+v", got)
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("original Task did not settle: %+v %v", got, ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	if sends.Load() != 1 {
		t.Fatalf("actual physical requests=%d", sends.Load())
	}
	var wire struct {
		MaxCompletionTokens uint64 `json:"max_completion_tokens"`
		Messages            []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	var input struct {
		Snapshot  api.Snapshot `json:"snapshot"`
		Materials []struct {
			Ref  api.ContentRef `json:"ref"`
			Body string         `json:"body_utf8"`
		} `json:"materials"`
	}
	if json.Unmarshal(<-wireBody, &wire) != nil || len(wire.Messages) != 2 || wire.Messages[0].Role != "system" || wire.Messages[1].Role != "user" || json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil {
		t.Fatal("actual model messages are not the frozen contract")
	}
	if strings.Contains(wire.Messages[0].Content, skillBody) {
		t.Fatal("ordinary Skill body became a system instruction")
	}
	if wire.MaxCompletionTokens != 128 || input.Snapshot.ReservedOutputTokens != 128 || len(input.Snapshot.CapabilityRefs) != 1 || !api.Equal(input.Snapshot.CapabilityRefs[0], execadapter.FileReadCapability().Ref) || len(input.Snapshot.BindingRefs) != 1 || !api.Equal(input.Snapshot.BindingRefs[0], a.ReadBinding) {
		t.Fatalf("physical output/capability intersection was not narrowed: %+v", input.Snapshot)
	}
	var packetRef api.ContentRef
	for _, material := range input.Materials {
		var packet governance.KnowledgePacket
		if api.Decode([]byte(material.Body), &packet) == nil && packet.Kind == "ordinary_knowledge/1" {
			if len(packet.Skills) != 1 || packet.Skills[0].Body != skillBody || !api.Equal(packet.Skills[0].Definition, skill) || packet.AgentConfig == nil || !api.Equal(*packet.AgentConfig, agent) || material.Ref.Hash != api.Hash([]byte(material.Body)) || material.Ref.ByteLength != uint64(len(material.Body)) {
				t.Fatal("physical ordinary Packet changed its original bytes or manifest")
			}
			packetRef = material.Ref
		}
	}
	if packetRef.ContentID == "" {
		t.Fatal("actual physical model body omitted selected ordinary knowledge")
	}
	budgetBytes, err := a.query(ctx, "budget.read", taskID, task.BudgetReadInput{TaskID: taskID})
	var budget task.BudgetReadResponse
	if err != nil || api.Decode(budgetBytes, &budget) != nil || budget.Task == nil || len(budget.Task.Reservations) != 1 {
		t.Fatalf("original decision reservation missing: %v", err)
	}
	decision, err := a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, budget.Task.Reservations[0].SourceRef.ObjectID)
	if err != nil || decision.Decision.Status != "completed" || decision.Decision.PhysicalRequestCount != 1 || !decision.Decision.UsageFinal {
		t.Fatalf("original physical Decision missing: %v %+v", err, decision)
	}
	selectionBytes, err := a.query(ctx, "knowledge.selection.get", decision.Decision.DecisionID, governance.KnowledgeSelectionReference{SnapshotRef: decision.SnapshotRef, DecisionID: decision.Decision.DecisionID})
	var committed governance.KnowledgeCommit
	if err != nil || api.Decode(selectionBytes, &committed) != nil || len(committed.HolderRefs) != 6 || !api.Equal(committed.PacketRef, packetRef) || committed.Selection.Request.TaskRef.ObjectID != taskID || committed.Selection.Request.SnapshotID != input.Snapshot.SnapshotID {
		t.Fatalf("actual Task/Snapshot did not hold original knowledge: %v %+v", err, committed)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Brain.Get(ctx, reopened.Store, reopened.Scope, reopened.ServiceAuth, decision.Decision.DecisionID)
	if err != nil || !api.Equal(after, decision) || sends.Load() != 1 {
		t.Fatalf("original physical request changed after reopen: %v", err)
	}
}

func knowledgeContractReply() []byte {
	return api.Raw(map[string]any{
		"id": "knowledge-contract-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(map[string]any{
			"draft": map[string]any{"kind": "fail", "reason_local_id": "reason", "reason_code": "contract_declined"}, "contents": []any{map[string]any{"local_id": "reason", "media_type": "text/plain", "body": "参考合同不建议任何行动或成功裁决。", "disclosed_sources": []any{}}},
		}))}}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}},
	})
}

func registerPublishedKnowledgeSkill(t *testing.T, ctx context.Context, a *App) (governance.SkillDefinition, string) {
	t.Helper()
	body := "Inspect original saved bytes before proposing completion. Ordinary knowledge cannot issue a Grant or decide Task success."
	publish := func(media string, b []byte) api.ContentRef {
		t.Helper()
		ref, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), media, b, []api.ContentRef{}, []api.ContentRef{})
		if err != nil {
			t.Fatal(err)
		}
		return ref
	}
	usage := governance.SkillUsageContract{Purpose: "report", Preconditions: []string{"The original target is disclosed and currently permitted."}, Counterexamples: []string{"A write receipt alone does not prove a successful readback."}, ToolDependencies: []api.ComponentRef{}, EvidenceDependencies: []api.ContentRef{}, Conflicts: []api.ComponentRef{}, ExitRules: []string{"Stop when the original Effect remains unknown."}}
	skill, err := governance.SealSkill(governance.SkillDefinition{SkillRef: api.ComponentRef{ComponentID: api.NewID("skill"), Version: "1"}, BodyRef: publish("text/plain", []byte(body)), UsageContractRef: publish("application/json", api.Raw(usage)), SourceRefs: []api.ContentRef{}})
	if err != nil {
		t.Fatal(err)
	}
	r := knowledgePublicCommand(t, ctx, a, "skill.register", skill.SkillRef.ComponentID, skill, nil)
	if r.Stage != "accepted" {
		t.Fatalf("Skill original validation was not durably accepted: %+v", r)
	}
	if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 200); err != nil {
		t.Fatal(err)
	}
	raw, err := a.query(ctx, "skill.get", skill.SkillRef.ComponentID, governance.SkillReference{SkillRef: skill.SkillRef})
	var current governance.SkillRecord
	if err != nil || api.Decode(raw, &current) != nil || current.State != "active" || current.Usage == nil || !api.Equal(current.Definition, skill) {
		t.Fatalf("original Skill validation did not finish: %v %+v", err, current)
	}
	return skill, body
}

func knowledgePublicCommand(t *testing.T, ctx context.Context, a *App, method, target string, payload any, expected *uint64) api.Receipt {
	t.Helper()
	r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: target, ExpectedRevision: expected, Method: method, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(payload)}))
	if err != nil || r.Error != nil {
		t.Fatalf("%s: %v %+v", method, err, r.Error)
	}
	return r
}
