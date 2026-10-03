package development

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

// The goal traverses real SQLite, immutable content, Brain, authorization, executor
// journal and independent read-back. No proposal or effect is inserted by the test.
func TestReportGoalCompletesOnlyAfterIndependentFileReadback(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires an actual HARNESS_TEST_POSTGRES_DSN")
			}
			runReportGoal(t, driver)
		})
	}
}

func runReportGoal(t *testing.T, driver string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	app, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	goal := brain.GoalSpec{Kind: "report", Title: "Verified report", Body: "The report bytes are checked independently.", SavePath: "reports/verified.md"}
	ref, err := app.Publish(ctx, app.Scope, app.UserAuth, api.NewID("content"), "application/json", api.Raw(goal), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	id := api.NewID("task")
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: app.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: app.Scope.OwnerID, GoalRef: ref, PolicyRef: app.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}, RequirementCandidates: []api.RequirementCandidate{}})}
	receipt, err := app.Dispatcher.Command(ctx, app.UserAuth, api.Raw(original))
	if err != nil || receipt.Error != nil {
		t.Fatalf("submit err=%v receipt=%+v", err, receipt)
	}
	for {
		if err = runtime.Drain(ctx, app.Store, app.Scope, app.Registry, 500); err != nil {
			var business *api.Error
			if errors.As(err, &business) {
				debugReport(t, app, id)
				t.Fatalf("%v; internal cause: %v", err, business.Cause)
			}
			t.Fatal(err)
		}
		got, err := app.Task.Read(ctx, app.Store, app.Scope, app.UserAuth, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status == "succeeded" {
			facts, err := app.Task.ContextFacts(ctx, app.Store, app.Scope, app.UserAuth, id)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts.Operations) != 3 {
				t.Fatalf("expected inspect/save/verify, got %d", len(facts.Operations))
			}
			for _, op := range facts.Operations {
				if !op.Fact.Closed || op.Fact.Effect == "unknown" || op.Fact.MayApplyLater {
					t.Fatalf("unclosed operation %+v", op.Fact)
				}
			}
			b, err := os.ReadFile(filepath.Join(root, "files", goal.SavePath))
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != "# Verified report\n\nThe report bytes are checked independently.\n" {
				t.Fatalf("actual saved bytes differ: %q", b)
			}
			result, err := app.Task.Result(ctx, app.Store, app.Scope, app.UserAuth, id, task.ResultInput{})
			if err != nil {
				t.Fatal(err)
			}
			if result.Result.TaskID != id || result.Result.CompletionBasis != "verified" || len(result.Result.ConditionResults) != 2 {
				t.Fatalf("immutable result missing %+v", result)
			}
			if result.Publication != "published" || result.ContentRef == nil {
				continue
			}
			published, err := app.Memory.Read(ctx, app.Scope, app.UserAuth, *result.ContentRef, "task.result")
			if err != nil {
				t.Fatal(err)
			}
			var exported api.Result
			if err = api.Decode(published, &exported); err != nil || !api.Equal(exported, result.Result) {
				t.Fatalf("published result differs from original: %v", err)
			}
			duplicate, err := app.Dispatcher.Command(ctx, app.UserAuth, api.Raw(original))
			if err != nil || duplicate.CommandID != receipt.CommandID || duplicate.Stage != receipt.Stage {
				t.Fatalf("original command changed %+v %v", duplicate, err)
			}
			if err = app.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			after, err := reopened.Task.Result(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, id, task.ResultInput{})
			if err != nil || !api.Equal(after, result) {
				t.Fatalf("reopening changed the durable result: %v", err)
			}
			duplicate, err = reopened.Dispatcher.Command(ctx, reopened.UserAuth, api.Raw(original))
			if err != nil || !api.Equal(duplicate, receipt) {
				t.Fatalf("reopening changed the original receipt: %v", err)
			}
			return
		}
		if got.Status == "failed" || got.Status == "cancelled" {
			t.Fatalf("unexpected terminal task %+v", got)
		}
		select {
		case <-ctx.Done():
			debugCtx := context.Background()
			facts, _ := app.Task.ContextFacts(debugCtx, app.Store, app.Scope, app.UserAuth, id)
			for _, op := range facts.Operations {
				raw, e := app.query(debugCtx, "execution.get", op.Fact.Ref.ObjectID, struct {
					ID string `json:"operation_id"`
				}{op.Fact.Ref.ObjectID})
				t.Logf("executor fact=%s error=%v", raw, e)
			}
			t.Fatalf("pipeline stopped at %+v: %v", got, ctx.Err())
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func debugReport(t *testing.T, app *App, id string) {
	t.Helper()
	ctx := context.Background()
	facts, e := app.Task.ContextFacts(ctx, app.Store, app.Scope, app.UserAuth, id)
	t.Logf("facts error=%v requirements=%s checks=%d operations=%d", e, facts.Task.RequirementsState, len(facts.Checks), len(facts.Operations))
	for _, op := range facts.Operations {
		raw, e := app.query(ctx, "execution.get", op.Fact.Ref.ObjectID, struct {
			ID string `json:"operation_id"`
		}{op.Fact.Ref.ObjectID})
		t.Logf("step=%s executor=%s error=%v", op.Intent.LogicalStepKey, raw, e)
	}
	for _, ch := range facts.Checks {
		t.Logf("check %s %s %s", ch.RequirementID, ch.Verdict, ch.Applicability)
	}
}
