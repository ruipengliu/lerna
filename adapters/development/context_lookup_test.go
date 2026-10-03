package development

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestPublicContextLookupsCarryExactContentCapabilityAndOriginalTaskFacts(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			ctx, stop := context.WithTimeout(context.Background(), 90*time.Second)
			defer stop()
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
			publish := func(body []byte) api.ContentRef {
				t.Helper()
				ref, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", body, []api.ContentRef{}, []api.ContentRef{})
				if err != nil {
					t.Fatal(err)
				}
				return ref
			}
			goal := publish(api.Raw(brain.GoalSpec{Kind: "answer", Body: "original answer is independent of ordinary lookup materials"}))
			id := api.NewID("task")
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})}
			receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
			if err != nil || receipt.Error != nil {
				t.Fatalf("submit %+v %v", receipt, err)
			}
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
			ordinary := publish([]byte(`{"ordinary":"accurate original bytes"}`))
			contentQuery := publish(api.Raw(existingContentLookup{ordinary}))
			capability := prepared.Snapshot.CapabilityRefs[0]
			capabilityQuery := publish(api.Raw(capabilityLookup{capability}))
			factQuery := publish(api.Raw(originalFactLookup{"task", prepared.Snapshot.TaskRef}))
			p := task.Proposal{DecisionID: prepared.DecisionID, Kind: "need_context", ReasonRef: goal, Lookups: []task.ContextLookup{
				{Kind: "existing_content", TargetRef: a.Scope.Ref(ordinary.ContentID, ordinary.Version), QueryRef: contentQuery},
				{Kind: "capability_describe", TargetRef: a.Scope.Ref(capability.ComponentID, 1), QueryRef: capabilityQuery},
				{Kind: "original_fact", TargetRef: prepared.Snapshot.TaskRef, QueryRef: factQuery},
			}}
			out, err := a.Task.ConsumeProposal(ctx, a.Store, a.Scope, a.ServiceAuth, p, nil)
			if err != nil || out.Outcome != "adopted" {
				t.Fatalf("original bounded lookup batch %+v %v", out, err)
			}
			for {
				work, status, err := a.Store.Claim(ctx, a.Scope, api.NewID("worker"), []string{task.JobContextLookup}, 1, time.Minute)
				if err != nil || status != runtime.Committed || len(work) != 1 {
					t.Fatalf("original lookup job not available %+v %v", work, err)
				}
				handler, ok := a.Registry.Job(task.JobContextLookup)
				if !ok {
					t.Fatal("lookup handler missing")
				}
				if err = handler(ctx, a.Store, a.Scope, work[0]); err != nil {
					t.Fatal(err)
				}
				facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, id)
				if err != nil {
					t.Fatal(err)
				}
				if facts.ContextPending {
					continue
				}
				if len(facts.ContextMaterials) != 3 || facts.ContextBudget.Calls != 3 || len(facts.SourceRefs) != 1 || !api.Equal(facts.SourceRefs[0].ContentRef, goal) {
					t.Fatalf("ordinary material, user source or lifetime budget lost: %+v", facts)
				}
				if !api.Equal(facts.ContextMaterials[0].ContentRef, ordinary) {
					t.Fatal("existing content was silently re-published as a different original")
				}
				for _, material := range facts.ContextMaterials {
					if _, err = a.Memory.Read(ctx, a.Scope, a.UserAuth, material.ContentRef, "task.context"); err != nil {
						t.Fatal(err)
					}
				}
				break
			}
			if again, err := a.Task.ConsumeProposal(ctx, a.Store, a.Scope, a.ServiceAuth, p, nil); err != nil || !api.Equal(out, again) {
				t.Fatalf("original proposal replay allocated new lookups: %+v %v", again, err)
			}
		})
	}
}
