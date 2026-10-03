package development

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	execadapter "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
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
			runConfiguredKnowledge(t, driver, "")
		})
	}
}

func TestConfiguredKnowledgeInputLimitPreventsAnyPhysicalModelRequest(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredKnowledge(t, driver, "input")
		})
	}
}

func TestConfiguredKnowledgeCallCostLimitPreventsAnyPhysicalModelRequest(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredKnowledge(t, driver, "cost")
		})
	}
}

func TestConfiguredKnowledgeActionCountRejectsOriginalTwoActionProposalBeforeUse(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredKnowledge(t, driver, "count")
		})
	}
}

func TestConfiguredKnowledgeCapabilityIntersectionRejectsUnselectedWriteBeforeUse(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredKnowledge(t, driver, "capability")
		})
	}
}

func TestConfiguredKnowledgeCompositeSnapshotKeepsActualLeafQualificationAndDuration(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredKnowledge(t, driver, "action")
		})
	}
}

func TestConfiguredKnowledgeWithdrawalClosesOriginalDecisionAndPreservesActualFeeAfterReopen(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			runConfiguredKnowledge(t, driver, "withdraw")
		})
	}
}

func runConfiguredKnowledge(t *testing.T, driver, blockedControl string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	var sends atomic.Int32
	var active atomic.Pointer[App]
	wireBody := make(chan []byte, 3)
	replyRelease := make(chan struct{})
	var released sync.Once
	release := func() { released.Do(func() { close(replyRelease) }) }
	defer release()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sends.Add(1)
		body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxJSONBytes+1))
		if err != nil {
			t.Error(err)
			return
		}
		wireBody <- body
		if blockedControl == "withdraw" {
			select {
			case <-replyRelease:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		reply := knowledgeContractReply()
		if blockedControl == "count" || blockedControl == "action" || blockedControl == "capability" {
			var wire struct {
				Messages []struct {
					Content string `json:"content"`
				} `json:"messages"`
			}
			var input struct {
				Snapshot api.Snapshot `json:"snapshot"`
			}
			if json.Unmarshal(body, &wire) != nil || len(wire.Messages) != 2 || json.Unmarshal([]byte(wire.Messages[1].Content), &input) != nil || len(input.Snapshot.BindingRefs) != 1 {
				t.Error("actual frozen capability/binding missing")
				return
			}
			if blockedControl == "count" {
				reply = knowledgeReadActionsReply(input.Snapshot, 2)
			} else if blockedControl == "capability" {
				reply = knowledgeWriteActionReply(input.Snapshot, active.Load().WriteBinding)
			} else if input.Snapshot.Purpose == "interpret_requirements" {
				reply = knowledgeRefinementReply(active.Load().ArtifactRule)
			} else if sends.Load() == 2 {
				reply = knowledgeReadActionsReply(input.Snapshot, 1)
			}
		}
		_, _ = w.Write(reply)
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
	if blockedControl == "input" {
		agentLimits.MaxInputBytes = 1
	} else if blockedControl == "cost" {
		agentLimits.MaxCallCostBound = []api.Amount{{Unit: "USD", Value: "0"}}
	} else if blockedControl == "action" {
		agentLimits.MaxActionDurationSeconds = 30
	}
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
	const originalFile = "Exact original bytes from the managed target."
	if blockedControl == "action" {
		cfg.Knowledge.ControlLimits.MaxActionDurationSeconds = 60
		if err = os.WriteFile(filepath.Join(root, "files", "knowledge-original.txt"), []byte(originalFile), 0600); err != nil {
			t.Fatal(err)
		}
	}
	a, err = OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	active.Store(a)
	goalMedia, goalBody := "text/plain", []byte("请依据准确知识检查报告是否可以继续。")
	if blockedControl == "action" {
		goalMedia, goalBody = "application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: "knowledge contract"})
	}
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), goalMedia, goalBody, []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	taskID := api.NewID("task")
	knowledgePublicCommand(t, ctx, a, "task.submit", taskID, task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}, RequirementCandidates: []api.RequirementCandidate{}}, nil)
	if blockedControl == "withdraw" {
		runKnowledgeWithdrawnCall(t, ctx, cancel, a, cfg, taskID, skill, wireBody, release, &sends)
		return
	}
	if blockedControl == "input" || blockedControl == "cost" {
		err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300)
		var refusal *api.Error
		if !errors.As(err, &refusal) || refusal.Code != "forbidden" || refusal.Reason != "knowledge_model_control_exceeded" {
			t.Fatalf("actual encoding must exceed the selected %s bound: %v sends=%d", blockedControl, err, sends.Load())
		}
		got, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil || got.ResultRef != nil || got.Status == "succeeded" || len(got.Budget) != 1 || got.Budget[0].Spent != "0" || got.Budget[0].Reserved != "0" || sends.Load() != 0 {
			t.Fatalf("input bound admitted a physical request, fee or success: %v sends=%d %+v", err, sends.Load(), got)
		}
		budgetRaw, err := a.query(ctx, "budget.read", taskID, task.BudgetReadInput{TaskID: taskID})
		var budget task.BudgetReadResponse
		if err != nil || api.Decode(budgetRaw, &budget) != nil || budget.Task == nil || len(budget.Task.Reservations) != 0 {
			t.Fatalf("blocked model encoding created a Decision reservation: %v %+v", err, budget)
		}
		return
	}
	if blockedControl == "count" || blockedControl == "capability" {
		for {
			err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300)
			if err != nil {
				break
			}
			select {
			case <-ctx.Done():
				t.Fatal("original action proposal did not reach consumption", ctx.Err())
			case <-time.After(20 * time.Millisecond):
			}
		}
		var refusal *api.Error
		reason := "knowledge_action_count_exceeded"
		keys := []string{"read1", "read2"}
		if blockedControl == "capability" {
			reason, keys = "knowledge_action_control_exceeded", []string{"write1"}
		}
		if !errors.As(err, &refusal) || refusal.Code != "forbidden" || refusal.Reason != reason {
			t.Fatalf("actual original proposal exceeded the selected %s control: %v sends=%d", blockedControl, err, sends.Load())
		}
		budgetRaw, err := a.query(ctx, "budget.read", taskID, task.BudgetReadInput{TaskID: taskID})
		var budget task.BudgetReadResponse
		if err != nil || api.Decode(budgetRaw, &budget) != nil || budget.Task == nil || len(budget.Task.Reservations) != 1 {
			t.Fatalf("action refusal changed original model responsibility: %v %+v", err, budget)
		}
		decisionID := budget.Task.Reservations[0].SourceRef.ObjectID
		decision, err := a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, decisionID)
		if err != nil || decision.Decision.Status != "completed" || !decision.Decision.UsageFinal || decision.Decision.PhysicalRequestCount != 1 || !api.Equal(decision.Decision.Usage, []api.Amount{{Unit: "USD", Value: "0.00024"}}) || sends.Load() != 1 {
			t.Fatalf("known original model fee/request was lost: %v %+v sends=%d", err, decision, sends.Load())
		}
		for _, key := range keys {
			operationID := stableID("operation", decisionID+"/"+key)
			if _, err := a.Task.ReadOperationIntent(ctx, a.Store, a.Scope, a.UserAuth, operationID); !api.IsCode(err, "not_found") {
				t.Fatalf("count refusal admitted an operation: %v", err)
			}
			useID := stableID("use", decisionID+"/"+key)
			if _, err := a.query(ctx, "grant.use.get", useID, governance.IDInput{ID: useID}); !api.IsCode(err, "not_found") {
				t.Fatalf("count refusal consumed a tool Grant: %v", err)
			}
		}
		if _, err := os.ReadFile(filepath.Join(root, "files", "knowledge-forbidden.txt")); !os.IsNotExist(err) {
			t.Fatalf("selected knowledge control allowed an unexpected native target write: %v", err)
		}
		return
	}
	for {
		if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300); err != nil {
			t.Fatal(err)
		}
		got, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "failed" && !got.AccountingOpen {
			spent := "0.00024"
			if blockedControl == "action" {
				spent = "0.00072"
			}
			if len(got.Budget) != 1 || got.Budget[0].Spent != spent || got.Budget[0].Reserved != "0" || got.ResultRef != nil {
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
	if blockedControl == "action" {
		facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil || len(facts.Operations) != 1 || !facts.Operations[0].Fact.Closed || facts.Operations[0].Fact.MayApplyLater || sends.Load() != 3 {
			t.Fatalf("qualified original action did not actually finish once: %v %+v sends=%d", err, facts.Operations, sends.Load())
		}
		intent, err := a.Task.ReadOperationIntent(ctx, a.Store, a.Scope, a.UserAuth, facts.Operations[0].Intent.OperationID)
		if err != nil || intent.MaxDurationSeconds != 30 || !api.Equal(intent.InstallLockRef, component("builtin-install-lock")) || !api.Equal(intent.CapabilityRef, execadapter.FileReadCapability().Ref) {
			t.Fatalf("knowledge replaced original program qualification or duration: %v %+v", err, intent)
		}
		deadline, _ := api.ParseTime(intent.Deadline)
		taskDeadline, _ := api.ParseTime(facts.Task.Deadline)
		if !deadline.Before(taskDeadline.Add(-2 * time.Minute)) {
			t.Fatal("effective 30-second action duration did not tighten the five-minute Task deadline")
		}
		raw, err := a.query(ctx, "execution.get", intent.OperationID, execution.OperationIDInput{OperationID: intent.OperationID})
		var operation execution.OperationView
		if err != nil || api.Decode(raw, &operation) != nil || operation.Operation.ExecutionState != "closed" || operation.Operation.ResultRef == nil || len(operation.Attempts.Items) != 1 || operation.Attempts.Items[0].StartedAt == "" || !operation.Attempts.Items[0].ActuallyStopped || operation.Attempts.Items[0].MayApplyLater != false {
			t.Fatalf("original target readback missing: %v %+v", err, operation)
		}
		bytes, err := a.Memory.Read(ctx, a.Scope, a.UserAuth, *operation.Operation.ResultRef, "execution_result")
		var observed execadapter.FileReadResult
		if err != nil || api.Decode(bytes, &observed) != nil || observed.DataBase64 != base64.StdEncoding.EncodeToString([]byte(originalFile)) {
			t.Fatalf("actual target bytes differ: %v %+v", err, observed)
		}
		if err = a.Close(); err != nil {
			t.Fatal(err)
		}
		reopened, err := OpenApp(ctx, cfg, false)
		if err != nil {
			t.Fatal(err)
		}
		defer reopened.Close()
		after, err := reopened.Task.ReadOperationIntent(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, intent.OperationID)
		if err != nil || !api.Equal(after, intent) || sends.Load() != 3 {
			t.Fatalf("reopen refreshed original action identity/deadline: %v %+v", err, after)
		}
		return
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

func runKnowledgeWithdrawnCall(t *testing.T, ctx context.Context, cancel context.CancelFunc, a *App, cfg Config, taskID string, skill governance.SkillDefinition, wire <-chan []byte, release func(), sends *atomic.Int32) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300) }()
	joined := false
	defer func() {
		release()
		cancel()
		if !joined {
			select {
			case <-done:
			case <-time.After(10 * time.Second):
				t.Error("original model worker did not actually exit")
			}
		}
	}()
	select {
	case <-wire:
	case <-ctx.Done():
		t.Fatal("original physical request never entered the actual HTTP target", ctx.Err())
	}
	raw, err := a.query(ctx, "skill.get", skill.SkillRef.ComponentID, governance.SkillReference{SkillRef: skill.SkillRef})
	var current governance.SkillRecord
	if err != nil || api.Decode(raw, &current) != nil || current.State != "active" {
		t.Fatalf("actual selected Skill head unavailable: %v %+v", err, current)
	}
	knowledgePublicCommand(t, ctx, a, "skill.withdraw", current.ID, governance.KnowledgeChange{Ref: a.Scope.Ref(current.ID, current.Revision), Reason: "stop future selected knowledge consumption after the original model send"}, &current.Revision)
	release()
	drainErr := <-done
	joined = true
	if drainErr != nil && !api.IsCode(drainErr, "invalid_state") {
		t.Fatalf("original physical response did not retain its responsibility: %v", drainErr)
	}
	budgetRaw, err := a.query(ctx, "budget.read", taskID, task.BudgetReadInput{TaskID: taskID})
	var budget task.BudgetReadResponse
	if err != nil || api.Decode(budgetRaw, &budget) != nil || budget.Task == nil || len(budget.Task.Reservations) != 1 {
		t.Fatalf("withdrawal lost the original Task model reservation: %v %+v", err, budget)
	}
	decisionID := budget.Task.Reservations[0].SourceRef.ObjectID
	decision, err := a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, decisionID)
	if err != nil || decision.Decision.Status != "cancelled" || decision.Decision.ProposalRef != nil || !decision.Decision.UsageFinal || decision.Decision.PhysicalRequestCount != 1 || decision.CallID == "" || !api.Equal(decision.Decision.Usage, []api.Amount{{Unit: "USD", Value: "0.00024"}}) || sends.Load() != 1 {
		t.Fatalf("withdrawn original Decision published a proposal or lost its known fee/call: %v %+v sends=%d", err, decision, sends.Load())
	}
	if _, err = a.query(ctx, "knowledge.selection.get", decisionID, governance.KnowledgeSelectionReference{SnapshotRef: decision.SnapshotRef, DecisionID: decisionID}); !api.IsCode(err, "invalid_state") {
		t.Fatalf("withdrawn Skill remained eligible in the original actual Snapshot: %v", err)
	}
	currentTask, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
	if err != nil || currentTask.ResultRef != nil || currentTask.Status == "succeeded" {
		t.Fatalf("withdrawal created Task success: %v %+v", err, currentTask)
	}
	knowledgePublicCommand(t, ctx, a, "task.cancel", taskID, task.ControlInput{TaskID: taskID, Reason: "join original task while preserving the already observed model fee"}, &currentTask.Revision)
	for {
		if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300); err != nil {
			t.Fatal(err)
		}
		currentTask, err = a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if currentTask.Status == "cancelled" && !currentTask.AccountingOpen {
			if currentTask.ResultRef != nil || len(currentTask.Budget) != 1 || currentTask.Budget[0].Spent != "0.00024" || currentTask.Budget[0].Reserved != "0" || sends.Load() != 1 {
				t.Fatalf("negative Task control erased the original known fee or resent the call: %+v sends=%d", currentTask, sends.Load())
			}
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("original Task bill did not settle", ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
	decision, err = a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, decisionID)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	cfg.Knowledge = nil
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	after, err := reopened.Brain.Get(ctx, reopened.Store, reopened.Scope, reopened.ServiceAuth, decisionID)
	if err != nil || !api.Equal(after, decision) || sends.Load() != 1 {
		t.Fatalf("disabled configuration/reopen changed original Decision/call/fee: %v %+v", err, after)
	}
	if _, err = reopened.query(ctx, "knowledge.selection.get", decisionID, governance.KnowledgeSelectionReference{SnapshotRef: after.SnapshotRef, DecisionID: decisionID}); !api.IsCode(err, "invalid_state") {
		t.Fatalf("reopen revived the withdrawn original Skill selection: %v", err)
	}
}

func knowledgeReadActionsReply(snapshot api.Snapshot, count int) []byte {
	draft := brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{}}
	contents := []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "Read original bytes without declaring Task success.", DisclosedSources: []api.ContentRef{}}}
	for _, key := range []string{"read1", "read2"}[:count] {
		args := "args_" + key
		draft.Actions = append(draft.Actions, brain.DraftAction{LocalKey: key, CapabilityRef: execadapter.FileReadCapability().Ref, BindingRef: snapshot.BindingRefs[0], ArgumentsLocalID: args})
		contents = append(contents, brain.GeneratedContent{LocalID: args, MediaType: "application/json", Body: string(api.Raw(execadapter.FileReadArguments{Path: "knowledge-original.txt"})), DisclosedSources: []api.ContentRef{}})
	}
	return api.Raw(map[string]any{
		"id": "knowledge-actions-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(struct {
			Draft    brain.Draft              `json:"draft"`
			Contents []brain.GeneratedContent `json:"contents"`
		}{draft, contents}))}}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}},
	})
}

func knowledgeWriteActionReply(snapshot api.Snapshot, binding api.ObjectRef) []byte {
	draft := brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "write1", CapabilityRef: execadapter.FileWriteCapability().Ref, BindingRef: binding, ArgumentsLocalID: "arguments"}}}
	contents := []brain.GeneratedContent{
		{LocalID: "reason", MediaType: "text/plain", Body: "This proposal uses an original granted tool that the selected AgentConfig has excluded.", DisclosedSources: []api.ContentRef{}},
		{LocalID: "arguments", MediaType: "application/json", Body: string(api.Raw(execadapter.FileWriteArguments{Path: "knowledge-forbidden.txt", ExpectedVersion: "absent", ContentRef: snapshot.GoalRef})), DisclosedSources: []api.ContentRef{}},
	}
	return api.Raw(map[string]any{
		"id": "knowledge-unselected-write-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(struct {
			Draft    brain.Draft              `json:"draft"`
			Contents []brain.GeneratedContent `json:"contents"`
		}{draft, contents}))}}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}},
	})
}

func knowledgeRefinementReply(rule api.ComponentRef) []byte {
	draft := brain.Draft{Kind: "refine_requirements", ReasonLocalID: "reason", Requirements: []brain.DraftRequirement{{CandidateKey: "original_answer", Kind: "quality", StatementLocalID: "statement", ParametersLocalID: "parameters", RuleRef: rule, Required: true}}}
	contents := []brain.GeneratedContent{
		{LocalID: "reason", MediaType: "text/plain", Body: "Preserve the original user goal before any tool admission.", DisclosedSources: []api.ContentRef{}},
		{LocalID: "statement", MediaType: "text/plain", Body: "The answer must match the original knowledge contract.", DisclosedSources: []api.ContentRef{}},
		{LocalID: "parameters", MediaType: "application/json", Body: string(api.Raw(brain.RuleParameters{Kind: "answer", ExpectedHash: api.Hash([]byte("knowledge contract")), ExpectedLength: 18})), DisclosedSources: []api.ContentRef{}},
	}
	return api.Raw(map[string]any{
		"id": "knowledge-refinement-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(struct {
			Draft    brain.Draft              `json:"draft"`
			Contents []brain.GeneratedContent `json:"contents"`
		}{draft, contents}))}}},
		"usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}},
	})
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
