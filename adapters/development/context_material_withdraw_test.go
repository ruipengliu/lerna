package development

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// 故障边界仅在实际只读查找返回之后暂停；存储、查询、Content 和当前许可均为真实实现。
type heldContextLookup struct {
	contextLookup
	resolved chan task.ContextLookupResult
	release  chan struct{}
}

func (p heldContextLookup) Resolve(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in task.ContextLookupRequest) (task.ContextLookupResult, error) {
	result, err := p.contextLookup.Resolve(ctx, scope, auth, in)
	if err != nil {
		return result, err
	}
	// 小端口允许只返回获准原正文。省略派生报告，避免其另一条来源撤权
	// 旁路掩盖准确 MemoryRef 当前版本门禁；查询及正文仍来自实际 Memory。
	if len(result.Materials) > 0 && result.Materials[0].MemoryRef != nil {
		result.Materials = result.Materials[:1]
	}
	select {
	case p.resolved <- result:
	case <-ctx.Done():
		return task.ContextLookupResult{}, ctx.Err()
	}
	select {
	case <-p.release:
		return result, nil
	case <-ctx.Done():
		return task.ContextLookupResult{}, ctx.Err()
	}
}

func TestMemoryWithdrawnAfterActualLookupCannotBecomeNextDecisionMaterial(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
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
			t.Cleanup(func() {
				if err := a.Close(); err != nil {
					t.Error(err)
				}
			})
			held := heldContextLookup{contextLookup: contextLookup{a}, resolved: make(chan task.ContextLookupResult), release: make(chan struct{})}
			// 经已授权消费方小端口装配故障点，不改任何持久 Task 或 Memory 事实。
			a.Task, err = task.New(task.Config{Policies: []task.TaskPolicy{a.TaskPolicy}, Participants: []string{"task", "content", "memory", "platform", "governance"}}, task.Ports{Content: taskContent{a}, ContextLookup: held, Gate: taskGate{a}})
			if err != nil {
				t.Fatal(err)
			}
			a.Registry = runtime.NewRegistry()
			a.Memory.Register(a.Registry)
			for _, register := range []func(*runtime.Registry) error{a.Task.Register, a.Governance.Register} {
				if err = register(a.Registry); err != nil {
					t.Fatal(err)
				}
			}
			a.Dispatcher.Registry = a.Registry
			publish := func(media string, body []byte) api.ContentRef {
				t.Helper()
				ref, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), media, body, []api.ContentRef{}, []api.ContentRef{})
				if err != nil {
					t.Fatal(err)
				}
				return ref
			}
			scopeRef := publish("application/json", []byte(`{"scope":"original preference"}`))
			bodyRef := publish("text/plain", []byte("exact ordinary preference"))
			textRef := publish("text/plain", []byte("preference"))
			specRef := publish("application/json", api.Raw(memory.MemoryQuerySpec{TextRef: textRef, TypeFilter: []string{"preference"}, ScopeFilter: &scopeRef, RankingProfileRef: memory.LexicalProfile()}))
			memoryID := api.NewID("memory")
			values := memory.MemoryValues{Type: "preference", ContentRef: bodyRef, Sources: []api.SourceEvidence{{ContentRef: bodyRef, SourceKind: "user_input"}}, ScopeRef: scopeRef, PolicyRef: a.ContentPolicy.PolicyRef, ObservedAt: api.Time(time.Now())}
			send := func(method, id string, revision *uint64, input any) api.Receipt {
				t.Helper()
				command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: method, ExpectedRevision: revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(input)}
				r, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
				if err != nil || r.Error != nil || r.Stage != "applied" {
					t.Fatalf("public %s %+v %v", method, r, err)
				}
				return r
			}
			send("memory.create", memoryID, nil, memory.CreateInput{MemoryID: memoryID, Values: values})
			goal := publish("application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: "Keep the original answer and user source."}))
			id := api.NewID("task")
			send("task.submit", id, nil, task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})
			current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, id)
			if err != nil {
				t.Fatal(err)
			}
			prepared, err := (contextCompiler{a}).Prepare(ctx, a.Scope, a.UserAuth, current)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = a.Task.PrepareDecision(ctx, a.Store, a.Scope, a.ServiceAuth, prepared); err != nil {
				t.Fatal(err)
			}
			query := publish("application/json", api.Raw(memory.QueryInput{QueryRef: specRef, ScopeRef: scopeRef, Purposes: []string{"task.context"}, Limits: memory.QueryLimits{MaxCandidates: 20, MaxReadBytes: 4096, MaxPermissionChecks: 500, Deadline: api.Time(time.Now().Add(time.Minute))}, Limit: 5}))
			p := task.Proposal{DecisionID: prepared.DecisionID, Kind: "need_context", ReasonRef: goal, Lookups: []task.ContextLookup{{Kind: "memory_query", TargetRef: a.Scope.Ref(scopeRef.ContentID, scopeRef.Version), QueryRef: query}}}
			consumed, err := a.Task.ConsumeProposal(ctx, a.Store, a.Scope, a.ServiceAuth, p, nil)
			if err != nil || consumed.Outcome != "adopted" {
				t.Fatalf("original lookup batch %+v %v", consumed, err)
			}
			works, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{task.JobContextLookup}, 1, time.Minute)
			if err != nil || status != runtime.Committed || len(works) != 1 {
				t.Fatalf("original lookup claim %+v %v", works, err)
			}
			handler, ok := a.Registry.Job(task.JobContextLookup)
			if !ok {
				t.Fatal("lookup handler missing")
			}
			done := make(chan error, 1)
			go func() { done <- handler(ctx, a.Store, a.Scope, works[0]) }()
			joined := false
			defer func() {
				cancel()
				select {
				case <-held.release:
				default:
					close(held.release)
				}
				if !joined {
					<-done
				}
			}()
			select {
			case actual := <-held.resolved:
				if len(actual.Materials) != 1 || actual.Materials[0].MemoryRef == nil || !api.Equal(*actual.Materials[0].MemoryRef, a.Scope.Ref(memoryID, 1)) || !api.Equal(actual.Materials[0].ContentRef, bodyRef) {
					t.Fatalf("actual original Memory query omitted its accurate assertion/body: %+v", actual)
				}
			case err := <-done:
				joined = true
				t.Fatalf("actual lookup did not reach its result boundary: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			one := uint64(1)
			send("memory.delete", memoryID, &one, memory.DeleteInput{MemoryID: memoryID, Reason: "Withdraw assertion after actual read, retaining the original Content."})
			close(held.release)
			err = <-done
			joined = true
			if err != nil {
				t.Fatal(err)
			}
			if _, err = a.Memory.Read(ctx, a.Scope, a.UserAuth, bodyRef, "task.context"); err != nil {
				t.Fatalf("deleting an assertion incorrectly destroyed independently held Content: %v", err)
			}
			facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, id)
			if err != nil || !facts.ContextPending || len(facts.ContextMaterials) != 0 || facts.ContextBudget.Calls != 1 || len(facts.SourceRefs) != 1 || !api.Equal(facts.SourceRefs[0].ContentRef, goal) {
				t.Fatalf("late ordinary result bypassed current assertion or rewrote user evidence: %+v %v", facts, err)
			}
			blocked := false
			for _, reason := range facts.Task.WaitReasons {
				t.Logf("original lookup wait after withdrawal: %s", reason.ResumeCondition)
				blocked = blocked || strings.Contains(reason.ResumeCondition, "original_context_lookup:")
			}
			if !blocked {
				t.Fatal("withdrawn material lacked its durable, original bounded waiting responsibility")
			}
			if replay, err := a.Task.ConsumeProposal(ctx, a.Store, a.Scope, a.ServiceAuth, p, nil); err != nil || !api.Equal(replay, consumed) {
				t.Fatalf("replay replaced the blocked original lookup responsibility: %+v %v", replay, err)
			}
			if works, _, err = a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{task.JobContextLookup}, 1, time.Minute); err != nil || len(works) != 0 {
				t.Fatalf("definitive withdrawal retried a new lookup or refreshed the original deadline: %+v %v", works, err)
			}
		})
	}
}
