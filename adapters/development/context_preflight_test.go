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
)

func TestOriginalOrdinaryMaterialRemainsCurrentAtBrainJobPreflight(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
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
			publish := func(body []byte) api.ContentRef {
				t.Helper()
				ref, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "text/plain", body, []api.ContentRef{}, []api.ContentRef{})
				if err != nil {
					t.Fatal(err)
				}
				return ref
			}
			goal := publish([]byte("The original user goal remains distinct from ordinary context material."))
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: api.NewID("task"), ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}}})}
			if receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command)); err != nil || receipt.Error != nil || receipt.Stage != "applied" {
				t.Fatalf("original Task submit: %+v %v", receipt, err)
			}
			prepare := func() task.PreparedDecision {
				t.Helper()
				current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, command.TargetID)
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
				return prepared
			}
			first := prepare()
			material := publish([]byte("Existing licensed ordinary material; it cannot replace the user's goal."))
			if outcome, err := a.Task.ConsumeProposal(ctx, a.Store, a.Scope, a.ServiceAuth, task.Proposal{DecisionID: first.DecisionID, Kind: "need_context", ReasonRef: goal, ContextRefs: []api.ContentRef{material}}, nil); err != nil || outcome.Outcome != "adopted" {
				t.Fatalf("original ordinary material: %+v %v", outcome, err)
			}
			second := prepare()
			in := brain.DecideInput{DecisionID: second.DecisionID, TaskRef: second.Snapshot.TaskRef, SnapshotRef: second.SnapshotRef, SnapshotRevision: second.Snapshot.Revision, ModelProfileRef: second.Snapshot.ModelProfileRef, Limits: second.CostBound, UseRefs: []api.ObjectRef{}, Deadline: api.Time(time.Now().Add(time.Minute))}
			preparedContext, err := (brainGate{a}).PrepareGate(ctx, a.Scope, a.ServiceAuth, in, nil)
			if err != nil || preparedContext == nil {
				t.Fatalf("original Brain preflight rejected accurate ordinary material: %v", err)
			}
			facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, command.TargetID)
			if err != nil || len(facts.ContextMaterials) != 1 || !api.Equal(facts.ContextMaterials[0].ContentRef, material) || len(facts.SourceRefs) != 1 || !api.Equal(facts.SourceRefs[0].ContentRef, goal) {
				t.Fatalf("preflight changed original ordinary/user evidence: %+v %v", facts, err)
			}
		})
	}
}
