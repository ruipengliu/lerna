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

// 只由本人公开输入和真实 Brain/Task Job 生成条件，不预置提案或检查结果。
func TestClarifiedReportAdoptsOriginalUserSourcesBeforeExecution(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL fixture required")
			}
			runClarifiedReport(t, driver, "application/json")
		})
	}
}

func TestRawTextGoalClarifiesAndCompletesOriginalReport(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL fixture required")
			}
			runClarifiedReport(t, driver, "text/plain")
		})
	}
}

func runClarifiedReport(t *testing.T, driver, initialMedia string) {
	t.Helper()
	started := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), reportFixtureTimeout)
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
	t.Logf("original app ready elapsed=%s", time.Since(started))
	initialBytes := []byte("请生成并保存一份报告；需要的正文和路径由我补充。")
	if initialMedia == "application/json" {
		initialBytes = api.Raw(string(initialBytes))
	}
	initial, err := app.Publish(ctx, app.Scope, app.UserAuth, api.NewID("content"), initialMedia, initialBytes, []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	id := api.NewID("task")
	submit := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: app.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: app.Scope.OwnerID, GoalRef: initial, PolicyRef: app.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}, RequirementCandidates: []api.RequirementCandidate{}})}
	submitReceipt, err := app.Dispatcher.Command(ctx, app.UserAuth, api.Raw(submit))
	if err != nil || submitReceipt.Error != nil || submitReceipt.Stage != "applied" {
		t.Fatalf("public submit %+v %v", submitReceipt, err)
	}
	t.Logf("original submit task=%s command=%s elapsed=%s", id, submit.CommandID, time.Since(started))
	var request task.InputRequestView
	for request.Request.RequestID == "" {
		driveClarificationJob(t, ctx, app)
		raw, err := clarificationQuery(ctx, app, "task.input_requests.list", id, task.InputRequestListInput{TaskID: id, Limit: 20})
		var page api.Page[task.InputRequestView]
		if err != nil || api.Decode(raw, &page) != nil {
			t.Fatalf("public original request view %v", err)
		}
		if len(page.Items) > 0 {
			if len(page.Items) != 1 || !page.Exhausted || page.Items[0].Request.State != "pending" {
				t.Fatalf("ambiguous original request %+v", page)
			}
			request = page.Items[0]
		}
	}
	t.Logf("original clarification request=%s elapsed=%s", request.Request.RequestID, time.Since(started))
	goal := brain.GoalSpec{Kind: "report", Title: "Clarified report", Body: "The user's exact clarification supplies this body.", SavePath: "reports/clarified.md"}
	var answerSchema api.Schema
	if err = api.Decode(request.AnswerSchema, &answerSchema); err != nil {
		t.Fatal(err)
	}
	validator, err := api.NewValidator(answerSchema)
	if err != nil || validator.Validate(api.Raw(goal)) != nil {
		t.Fatalf("registered answer schema cannot accept the original clarification %v", err)
	}
	answer, err := app.Publish(ctx, app.Scope, app.UserAuth, api.NewID("content"), "application/json", api.Raw(goal), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	input := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: app.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "task.input", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.InputAnswer{TaskID: id, RequestRef: request.RequestRef, GoalRevision: *request.Request.GoalRevision, AnswerRef: answer})}
	inputReceipt, err := app.Dispatcher.Command(ctx, app.UserAuth, api.Raw(input))
	if err != nil || inputReceipt.Error != nil || inputReceipt.Stage != "accepted" {
		t.Fatalf("public original input %+v %v", inputReceipt, err)
	}
	inputAcceptedAt := time.Now()
	t.Logf("original input accepted command=%s elapsed=%s", input.CommandID, time.Since(started))
	for {
		work := driveClarificationJob(t, ctx, app)
		if work.Job.Kind != task.JobDispatchDecision {
			continue
		}
		view, err := app.Brain.Get(ctx, app.Store, app.Scope, app.ServiceAuth, work.Job.SourceRef.ObjectID)
		if err != nil || view.Decision.Status != "completed" || view.Decision.ProposalRef == nil {
			continue
		}
		body, err := app.Memory.Read(ctx, app.Scope, app.ServiceAuth, *view.Decision.ProposalRef, "brain.output")
		var proposal brain.Proposal
		if err != nil || api.Decode(body, &proposal) != nil {
			t.Fatalf("original published proposal %v", err)
		}
		if proposal.Kind != "refine_requirements" {
			continue
		}
		raw, err := clarificationQuery(ctx, app, "task.read", id, task.ReadInput{})
		var current api.Task
		if err != nil || api.Decode(raw, &current) != nil || len(current.Requirements) != 2 {
			t.Fatalf("first real refine lost original user sources: goal=%d requirements=%d state=%s error=%v", current.GoalRevision, len(current.Requirements), current.RequirementsState, err)
		}
		expectedSources := []api.SourceEvidence{{ContentRef: initial, SourceKind: "user_input", SubmissionRef: func() *api.ObjectRef { r := app.Scope.Ref(submit.CommandID, 1); return &r }()}, {ContentRef: answer, SourceKind: "user_input", SubmissionRef: &request.RequestRef}}
		for _, requirement := range current.Requirements {
			if !api.Equal(requirement.SourceRefs, expectedSources) {
				t.Fatalf("adopted requirement changed original submission/request provenance %+v", requirement.SourceRefs)
			}
		}
		goalBytes, err := app.Memory.Read(ctx, app.Scope, app.UserAuth, current.GoalRef, "task.goal")
		var document api.GoalDocument
		if err != nil || api.Decode(goalBytes, &document) != nil || !api.Equal(document.InitialGoalRef, initial) || !api.Equal(document.AmendmentRefs, []api.ContentRef{answer}) {
			t.Fatalf("clarification rewrote original goal components %+v %v", document, err)
		}
		originalBytes, err := app.Memory.Read(ctx, app.Scope, app.UserAuth, initial, "task.goal")
		if err != nil || string(originalBytes) != string(initialBytes) || api.Hash(originalBytes) != initial.Hash || initial.MediaType != initialMedia {
			t.Fatalf("clarification changed the original input bytes %q %v", originalBytes, err)
		}
		assertRuleGoalSourceClosure(t, ctx, app, view.SnapshotRef, goalBytes, initial)
		t.Logf("original requirements adopted elapsed=%s after_input=%s", time.Since(started), time.Since(inputAcceptedAt))
		break
	}
	var result task.ResultOutput
	lastState := ""
	for {
		driveClarificationJob(t, ctx, app)
		current, err := app.Task.Read(ctx, app.Store, app.Scope, app.UserAuth, id)
		if err != nil {
			t.Fatal(err)
		}
		state := current.Status + "/" + current.RequirementsState
		if state != lastState {
			t.Logf("original Task state=%s revision=%d elapsed=%s after_input=%s", state, current.Revision, time.Since(started), time.Since(inputAcceptedAt))
			lastState = state
		}
		if current.Status == "failed" || current.Status == "cancelled" {
			t.Fatalf("clarification exhausted guardrails instead of retaining original source: %+v", current)
		}
		if current.Status != "succeeded" {
			continue
		}
		result, err = app.Task.Result(ctx, app.Store, app.Scope, app.UserAuth, id, task.ResultInput{})
		if err != nil || result.Result.CompletionBasis != "verified" || len(result.Result.ConditionResults) != 2 {
			t.Fatalf("clarified result lacks independent checks %+v %v", result, err)
		}
		if result.Publication == "published" && result.ContentRef != nil {
			t.Logf("original Result published elapsed=%s after_input=%s", time.Since(started), time.Since(inputAcceptedAt))
			break
		}
	}
	facts, err := app.Task.ContextFacts(ctx, app.Store, app.Scope, app.UserAuth, id)
	if err != nil || len(facts.Operations) != 3 {
		t.Fatalf("clarification repeated logical file steps %+v %v", facts, err)
	}
	for _, operation := range facts.Operations {
		if !operation.Fact.Closed || operation.Fact.Effect == "unknown" || operation.Fact.MayApplyLater {
			t.Fatalf("clarified result hid an unresolved original effect %+v", operation.Fact)
		}
	}
	actual, err := os.ReadFile(filepath.Join(root, "files", "reports", "clarified.md"))
	if err != nil || string(actual) != "# Clarified report\n\nThe user's exact clarification supplies this body.\n" {
		t.Fatalf("independent file bytes differ %q %v", actual, err)
	}
	closedReceipts := []api.Receipt{}
	for _, original := range []api.Command{submit, input} {
		r, err := app.Dispatcher.Lookup(ctx, app.UserAuth, original.CommandID)
		if err != nil || r.Stage != "applied" {
			t.Fatalf("original public command did not close %+v %v", r, err)
		}
		closedReceipts = append(closedReceipts, r)
	}
	consumed, err := app.Task.InputRequestRead(ctx, app.Store, app.Scope, app.UserAuth, request.Request.RequestID, 0)
	if err != nil || consumed.Request.State != "answered" || consumed.Request.ConsumedBy != input.CommandID || consumed.Request.AnswerRef == nil || !api.Equal(*consumed.Request.AnswerRef, answer) {
		t.Fatalf("original request did not consume the exact answer once %+v %v", consumed, err)
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
		t.Fatalf("reopen changed original clarified result %+v %v", after, err)
	}
	t.Logf("original Result and source database reopened elapsed=%s", time.Since(started))
	for i, original := range []api.Command{submit, input} {
		recovered, err := reopened.Dispatcher.Command(ctx, reopened.UserAuth, api.Raw(original))
		if err != nil || !api.Equal(recovered, closedReceipts[i]) {
			t.Fatalf("reopen changed original input or submit receipt %+v %v", recovered, err)
		}
	}
}

// 已声明的 Snapshot 引用不等于来源授权；实际 Engine 边界必须核完整原组件。
func assertRuleGoalSourceClosure(t *testing.T, ctx context.Context, app *App, snapshotRef api.ContentRef, goalBytes []byte, initial api.ContentRef) {
	t.Helper()
	snapshotBytes, err := app.Memory.Read(ctx, app.Scope, app.ServiceAuth, snapshotRef, "brain.input")
	if err != nil {
		t.Fatal(err)
	}
	for _, collection := range []string{"materials", "processed_sources"} {
		t.Run("original_missing_from_"+collection, func(t *testing.T) {
			var snapshot api.Snapshot
			if err := api.Decode(snapshotBytes, &snapshot); err != nil {
				t.Fatal(err)
			}
			refs := snapshot.MaterialRefs
			if collection == "processed_sources" {
				refs = snapshot.ProcessedSources
			}
			filtered := []api.ContentRef{}
			for _, ref := range refs {
				if !api.Equal(ref, initial) {
					filtered = append(filtered, ref)
				}
			}
			if collection == "materials" {
				snapshot.MaterialRefs = filtered
			} else {
				snapshot.ProcessedSources = filtered
			}
			encoding, err := app.Engine.Encode(ctx, snapshot, goalBytes, app.Profile)
			if err != nil {
				t.Fatal(err)
			}
			_, err = app.Engine.Request(ctx, "source_closure_probe", encoding)
			if !api.IsCode(err, "forbidden") {
				t.Fatalf("undeclared original goal component must be rejected: %v", err)
			}
		})
	}
	t.Run("foreign_goal_document", func(t *testing.T) {
		foreign, err := app.Publish(ctx, app.Scope, app.UserAuth, api.NewID("content"), "application/json", []byte(`{"kind":"answer","body":"A foreign document must not become the user's clarification."}`), []api.ContentRef{}, []api.ContentRef{})
		if err != nil {
			t.Fatal(err)
		}
		body := api.Raw(api.GoalDocument{FormatVersion: 1, InitialGoalRef: foreign, AmendmentRefs: []api.ContentRef{}})
		wrapper, err := app.Publish(ctx, app.Scope, app.UserAuth, api.NewID("content"), "application/json", body, []api.ContentRef{foreign}, []api.ContentRef{})
		if err != nil {
			t.Fatal(err)
		}
		var snapshot api.Snapshot
		if err = api.Decode(snapshotBytes, &snapshot); err != nil {
			t.Fatal(err)
		}
		snapshot.GoalRef = wrapper
		snapshot.MaterialRefs = append(snapshot.MaterialRefs, wrapper, foreign)
		snapshot.ProcessedSources = append(snapshot.ProcessedSources, wrapper, foreign)
		encoding, err := app.Engine.Encode(ctx, snapshot, body, app.Profile)
		if err != nil {
			t.Fatal(err)
		}
		_, err = app.Engine.Request(ctx, "source_closure_probe", encoding)
		if !api.IsCode(err, "forbidden") {
			t.Fatalf("published wrapper must not upgrade a foreign source into本人输入: %v", err)
		}
	})
}

// 同 ContentRef 不足以证明提交者、类别或位置；公开候选必须保留原受信来源。
func TestRequirementCandidatesRejectForgedOriginalSourceEvidence(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, "sqlite")
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	app, err := OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer app.Close()
	publish := func(media string, body []byte) api.ContentRef {
		ref, err := app.Publish(ctx, app.Scope, app.UserAuth, api.NewID("content"), media, body, []api.ContentRef{}, []api.ContentRef{})
		if err != nil {
			t.Fatal(err)
		}
		return ref
	}
	goal := publish("application/json", []byte(`{"kind":"answer","body":"Exact answer."}`))
	statement := publish("text/plain", []byte("产物必须与原模板指定的准确正文和摘要一致。"))
	parameters := publish("application/json", api.Raw(brain.RuleParameters{Kind: "answer", ExpectedHash: api.Hash([]byte("Exact answer.")), ExpectedLength: 13}))
	foreign := publish("text/plain", []byte("This document is external data, not the original goal."))
	for _, variant := range []string{"original", "forged_submission", "forged_kind", "forged_locator", "mixed_foreign"} {
		t.Run(variant, func(t *testing.T) {
			id, commandID := api.NewID("task"), api.NewID("command")
			submission := app.Scope.Ref(commandID, 1)
			sources := []api.SourceEvidence{{ContentRef: goal, SourceKind: "user_input", SubmissionRef: &submission}}
			switch variant {
			case "forged_submission":
				fake := app.Scope.Ref(api.NewID("command"), 1)
				sources[0].SubmissionRef = &fake
			case "forged_kind":
				sources[0].SourceKind = "trusted_template"
			case "forged_locator":
				sources[0].Locator = "/body"
			case "mixed_foreign":
				sources = append(sources, api.SourceEvidence{ContentRef: foreign, SourceKind: "user_input"})
			}
			candidate := api.RequirementCandidate{CandidateKey: "artifact_exact", Kind: "quality", StatementRef: statement, SourceRefs: sources, Origin: "derived", RuleRef: app.ArtifactRule, RuleParametersRef: &parameters, Required: true, OpenQuestions: []string{}}
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: app.Scope.OwnerID, CommandID: commandID, TargetID: id, Method: "task.submit", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: app.Scope.OwnerID, GoalRef: goal, PolicyRef: app.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}, RequirementCandidates: []api.RequirementCandidate{candidate}})}
			receipt, err := app.Dispatcher.Command(ctx, app.UserAuth, api.Raw(command))
			if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
				t.Fatalf("original candidate submit %+v %v", receipt, err)
			}
			work, status, err := app.Store.Claim(ctx, app.Scope, api.NewID("worker"), []string{task.JobAdvance}, 1, 30*time.Second)
			if err != nil || status != runtime.Committed || len(work) != 1 {
				t.Fatalf("original candidate validation responsibility %s %v", status, err)
			}
			handler, _ := app.Registry.Job(task.JobAdvance)
			if err = handler(ctx, app.Store, app.Scope, work[0]); err != nil {
				t.Fatal(err)
			}
			current, err := app.Task.Read(ctx, app.Store, app.Scope, app.UserAuth, id)
			if err != nil {
				t.Fatal(err)
			}
			if variant == "original" {
				if len(current.Requirements) != 1 || !api.Equal(current.Requirements[0].SourceRefs, sources) {
					t.Fatal("accurate original source should remain admissible")
				}
			} else if len(current.Requirements) != 0 || current.RequirementsState != "collecting" {
				t.Fatalf("same bytes must not upgrade forged or mixed authority: %+v", current)
			}
		})
	}
}

func clarificationQuery(ctx context.Context, app *App, method, target string, input any) ([]byte, error) {
	return app.Dispatcher.Query(ctx, app.UserAuth, api.Raw(api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: app.Scope.OwnerID, QueryID: api.NewID("query"), Method: method, TargetID: target, Payload: api.Raw(input)}))
}

func driveClarificationJob(t *testing.T, ctx context.Context, app *App) runtime.Work {
	t.Helper()
	for {
		claimStarted := time.Now()
		work, status, err := app.Store.Claim(ctx, app.Scope, api.NewID("worker"), app.Registry.JobKinds(), 1, 30*time.Second)
		if err != nil || status != runtime.Committed {
			t.Fatalf("public job claim %s %v", status, err)
		}
		if len(work) > 0 {
			if elapsed := time.Since(claimStarted); elapsed > time.Second {
				t.Logf("slow original Claim kind=%s source=%s elapsed=%s", work[0].Job.Kind, work[0].Job.SourceRef.ObjectID, elapsed)
			}
			handler, ok := app.Registry.Job(work[0].Job.Kind)
			if !ok {
				t.Fatalf("registered responsibility missing %s", work[0].Job.Kind)
			}
			handlerStarted := time.Now()
			err = handler(ctx, app.Store, app.Scope, work[0])
			if elapsed := time.Since(handlerStarted); elapsed > time.Second {
				t.Logf("slow original handler kind=%s source=%s elapsed=%s", work[0].Job.Kind, work[0].Job.SourceRef.ObjectID, elapsed)
			}
			if err != nil {
				debugCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				if work[0].Job.Kind == brain.JobAdvance {
					view, readErr := app.Brain.Get(debugCtx, app.Store, app.Scope, app.ServiceAuth, work[0].Job.SourceRef.ObjectID)
					t.Logf("original Brain=%s publication=%s status=%s call=%s read_error=%v", work[0].Job.SourceRef.ObjectID, view.Publication, view.Decision.Status, view.CallID, readErr)
					if readErr == nil {
						current, readErr := app.Task.Read(debugCtx, app.Store, app.Scope, app.UserAuth, view.TaskRef.ObjectID)
						t.Logf("original Task=%s status=%s revision=%d goal=%d requirements=%s waits=%v read_error=%v", view.TaskRef.ObjectID, current.Status, current.Revision, current.GoalRevision, current.RequirementsState, current.WaitReasons, readErr)
					}
				}
				cancel()
				t.Fatalf("original job %s id=%s source=%s: %v", work[0].Job.Kind, work[0].Job.JobID, work[0].Job.SourceRef.ObjectID, err)
			}
			return work[0]
		}
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(20 * time.Millisecond):
		}
	}
}
