package development

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestCurrentPreferenceChangesActualNextReportAndWithdrawalReturnsToExplicitDefault(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("requires actual PostgreSQL")
			}
			// 三份报告各自仍由原Task5min裁决；测试另留最多1min观察出版/关闭。
			// 只改变观察者等待，不更新Task/Command/Control或原Lookup期限。
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
			defer cancel()
			root := t.TempDir()
			if destination := os.Getenv("HARNESS_CONTEXT14_EVIDENCE_DIR"); destination != "" {
				var err error
				root, err = os.MkdirTemp(destination, "context14-preference-"+driver+"-")
				if err != nil {
					t.Fatal(err)
				}
			}
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
			publish := func(media string, bytes []byte) api.ContentRef {
				t.Helper()
				ref, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), media, bytes, []api.ContentRef{}, []api.ContentRef{})
				if err != nil {
					t.Fatal(err)
				}
				return ref
			}
			scopeRef := publish("application/json", []byte(`{"scope":"report_format"}`))
			textRef := publish("text/plain", []byte("report.preference"))
			queryRef := publish("application/json", api.Raw(memory.MemoryQuerySpec{TextRef: textRef, TypeFilter: []string{"preference"}, ScopeFilter: &scopeRef, RankingProfileRef: memory.LexicalProfile()}))
			id := api.NewID("memory")
			writePreference := func(format string, revision *uint64) memory.MemoryRecord {
				t.Helper()
				body := publish("application/vnd.harness.report-preference+json", api.Raw(map[string]any{"kind": "report.preference", "format": format}))
				values := memory.MemoryValues{Type: "preference", ContentRef: body, Sources: []api.SourceEvidence{{ContentRef: body, SourceKind: "user_input"}}, ScopeRef: scopeRef, PolicyRef: a.ContentPolicy.PolicyRef, ObservedAt: api.Time(time.Now())}
				method := "memory.create"
				if revision != nil {
					method = "memory.replace"
				}
				command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: id, ExpectedRevision: revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CreateInput{MemoryID: id, Values: values})}
				receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
				if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
					t.Fatalf("current preference %s: %+v %v", method, receipt, err)
				}
				record, err := a.Memory.ReadMemory(ctx, a.Scope, a.UserAuth, memory.ReadMemoryInput{MemoryID: id})
				if err != nil {
					t.Fatal(err)
				}
				return record
			}
			firstMemory := writePreference("bullet", nil)
			var originalResults []task.ResultOutput
			var originalCommands []api.Command
			var originalReceipts []api.Receipt
			var evidence []struct {
				TaskID  string            `json:"task_id"`
				Command api.Command       `json:"original_command"`
				Receipt api.Receipt       `json:"original_receipt"`
				Result  task.ResultOutput `json:"result"`
				Path    string            `json:"actual_path"`
				Bytes   string            `json:"independent_actual_bytes"`
			}
			for index, format := range []string{"bullet", "plain", "plain"} {
				if index == 1 {
					second := writePreference("plain", &firstMemory.Revision)
					if second.Revision != 2 {
						t.Fatal("correction replaced original identity")
					}
				}
				if index == 2 {
					revision := uint64(2)
					command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "memory.delete", TargetID: id, ExpectedRevision: &revision, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.DeleteInput{MemoryID: id, Reason: "withdraw this ordinary format preference"})}
					receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(command))
					if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
						t.Fatalf("withdraw preference %+v %v", receipt, err)
					}
				}
				path := []string{"reports/preferred-bullet.md", "reports/corrected-plain.md", "reports/withdrawn-default.md"}[index]
				goalBytes := api.Raw(map[string]any{"kind": "report", "title": "Original title", "body": "original first\noriginal second", "save_path": path, "preference": map[string]any{"query_ref": queryRef, "scope_ref": scopeRef, "allowed_formats": []string{"plain", "bullet"}, "default_format": "plain"}})
				goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", goalBytes, []api.ContentRef{queryRef, scopeRef, textRef}, []api.ContentRef{})
				if err != nil {
					t.Fatal(err)
				}
				taskID := api.NewID("task")
				original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), Method: "task.submit", TargetID: taskID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.SubmitInput{OrchestratorID: a.Scope.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}, RequirementCandidates: []api.RequirementCandidate{}})}
				receipt, err := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(original))
				if err != nil || receipt.Error != nil {
					t.Fatalf("original submit %+v %v", receipt, err)
				}
				originalCommands = append(originalCommands, original)
				originalReceipts = append(originalReceipts, receipt)
				preferenceStageEvidence(t, a, driver, root, "accepted", original, nil)
				claimStore := &preferenceClaimStore{Store: a.Store, t: t}
				stepCtx, stop := context.WithTimeout(ctx, 6*time.Minute)
				for {
					if err = runtime.Drain(stepCtx, claimStore, a.Scope, a.Registry, 500); err != nil {
						stop()
						preferenceStageEvidence(t, a, driver, root, "failed", original, err, claimStore.claims...)
						t.Fatal(err)
					}
					current, err := a.Task.Read(stepCtx, a.Store, a.Scope, a.UserAuth, taskID)
					if err != nil {
						stop()
						t.Fatal(err)
					}
					if current.Status == "succeeded" {
						result, err := a.Task.Result(stepCtx, a.Store, a.Scope, a.UserAuth, taskID, task.ResultInput{})
						if err != nil {
							stop()
							t.Fatal(err)
						}
						if result.Publication != "published" {
							continue
						}
						originalResults = append(originalResults, result)
						facts, err := a.Task.ContextFacts(stepCtx, a.Store, a.Scope, a.UserAuth, taskID)
						if err != nil || len(facts.Operations) != 3 || len(facts.Checks) != 2 || facts.ContextBudget.Calls != 1 || facts.ContextPending {
							stop()
							t.Fatalf("original independent effects/checks/lookup incomplete: %+v %v", facts, err)
						}
						memoryVersion := uint64(0)
						for _, material := range facts.ContextMaterials {
							if material.MemoryRef != nil {
								memoryVersion = material.MemoryRef.Revision
							}
						}
						wantVersion := uint64(index + 1)
						if index == 2 {
							wantVersion = 0
						}
						if memoryVersion != wantVersion || len(facts.SourceRefs) != 1 || !api.Equal(facts.SourceRefs[0].ContentRef, goal) {
							stop()
							t.Fatalf("current ordinary Memory/source refs differ: version%d want%d %+v", memoryVersion, wantVersion, facts.SourceRefs)
						}
						break
					}
					if current.Status == "failed" || current.RequirementsState == "awaiting_input" {
						stop()
						preferenceStageEvidence(t, a, driver, root, "blocked", original, nil, claimStore.claims...)
						t.Fatalf("explicit finite original preference template did not advance: %+v", current)
					}
					select {
					case <-stepCtx.Done():
						stop()
						preferenceStageEvidence(t, a, driver, root, "failed", original, stepCtx.Err(), claimStore.claims...)
						t.Fatal(stepCtx.Err())
					case <-time.After(50 * time.Millisecond):
					}
				}
				stop()
				actual, err := os.ReadFile(filepath.Join(root, "files", path))
				want := "# Original title\n\noriginal first\noriginal second\n"
				if format == "bullet" {
					want = "# Original title\n\n- original first\n- original second\n"
				}
				if err != nil || string(actual) != want {
					t.Fatalf("current preference did not affect actual exact independent file: %q %v", actual, err)
				}
				t.Logf("public preference report %d: task=%s command=%s result=%s path=%s", index, taskID, original.CommandID, originalResults[index].Result.ResultID, path)
				evidence = append(evidence, struct {
					TaskID  string            `json:"task_id"`
					Command api.Command       `json:"original_command"`
					Receipt api.Receipt       `json:"original_receipt"`
					Result  task.ResultOutput `json:"result"`
					Path    string            `json:"actual_path"`
					Bytes   string            `json:"independent_actual_bytes"`
				}{taskID, original, receipt, originalResults[index], path, string(actual)})
				for _, check := range originalResults[index].Result.ConditionResults {
					if check.Basis != "verified" || check.Verdict != "pass" {
						t.Fatalf("template choice self-reported success: %+v", check)
					}
				}
				for previous := 0; previous < index; previous++ {
					result, err := a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, originalCommands[previous].TargetID, task.ResultInput{})
					if err != nil || !api.Equal(result, originalResults[previous]) {
						t.Fatalf("ordinary correction/withdrawal rewrote an original Result: %+v %v", result, err)
					}
				}
			}
			if err := a.Close(); err != nil {
				t.Fatal(err)
			}
			a, err = OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			for index, original := range originalCommands {
				receipt, err := a.Dispatcher.Lookup(ctx, a.UserAuth, original.CommandID)
				if err != nil || !api.Equal(receipt, originalReceipts[index]) {
					t.Fatalf("reopen changed the original preference Task receipt: %+v %v", receipt, err)
				}
				result, err := a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, original.TargetID, task.ResultInput{})
				if err != nil || !api.Equal(result, originalResults[index]) {
					t.Fatalf("reopen changed the original preference Result: %+v %v", result, err)
				}
			}
			if destination := os.Getenv("HARNESS_CONTEXT14_EVIDENCE_DIR"); destination != "" {
				body := api.Raw(struct {
					Scope   runtime.Scope `json:"scope"`
					Driver  string        `json:"driver"`
					Reports any           `json:"reports"`
				}{a.Scope, driver, evidence})
				file, err := os.OpenFile(filepath.Join(destination, "context14-preference-"+driver+"-public-records.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err != nil {
					t.Fatal(err)
				}
				_, writeErr := file.Write(body)
				closeErr := file.Close()
				if writeErr != nil || closeErr != nil {
					t.Fatalf("public evidence write/close: %v %v", writeErr, closeErr)
				}
			}
		})
	}
}
