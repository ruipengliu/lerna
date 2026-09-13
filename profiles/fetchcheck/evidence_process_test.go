package fetchcheck

import (
	"context"
	"encoding/json"
	"errors"
	"lerna/adapters/contentpolicy"
	"lerna/adapters/fetchcontent"
	"lerna/adapters/researchcontext"
	"lerna/adapters/taskcontent"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/brain"
	wire "lerna/gen/harness/v1"
	"lerna/tasks"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type evidenceProcessConfig struct {
	Token, Mode string
	URLs        []string
	Task        tasks.Ref
	Evidence    string
}

func TestEvidenceAnswerRecoversAcrossProcessExit(t *testing.T) {
	for _, scenario := range []struct {
		mode      string
		sameTask  bool
		revoke    bool
		exhausted bool
	}{{"response", false, false, false}, {"content", false, false, false}, {"response", true, false, false}, {"content", true, false, false}, {"content", true, true, false}, {"content", true, false, true}} {
		mode := scenario.mode
		name := mode
		if scenario.sameTask {
			name = "action_" + mode
		}
		if scenario.revoke {
			name += "_revoked"
		}
		if scenario.exhausted {
			name += "_budget_exhausted"
		}
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			var hits atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				hits.Add(1)
				w.Header().Set("Content-Type", "text/plain")
				w.Write([]byte("The bridge opened in 1998."))
			}))
			defer server.Close()
			urls := []string{server.URL + "/start", server.URL + "/final"}
			root := t.TempDir()
			h, err := open(ctx, root, "", urls)
			if err != nil {
				t.Fatal(err)
			}
			var task tasks.Task
			var snapshot tasks.RunSnapshot
			var evidenceRef string
			if scenario.sameTask {
				snapshot, evidenceRef = prepareActionAnswerProcess(t, ctx, h)
				task = snapshot.Task
			} else {
				input, _ := json.Marshal(map[string]any{"url": urls[0], "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000})
				request, grant, err := h.request(ctx, input)
				if err != nil {
					h.close()
					t.Fatal(err)
				}
				if _, err = h.client.Invoke(ctx, request, grant); err != nil {
					h.close()
					t.Fatal(err)
				}
				if _, err = h.exec.Run(ctx, request.OperationID); err != nil {
					h.close()
					t.Fatal(err)
				}
				if err = h.exec.Drain(ctx, 16); err != nil {
					h.close()
					t.Fatal(err)
				}
				acquiredResult, known, err := h.attempts.Outcome(ctx, "local", request.OperationID)
				if err != nil || !known || acquiredResult.Status != "acquired" {
					h.close()
					t.Fatal("source acquisition failed")
				}
				evidenceRef = acquiredResult.Reference
				op, err := h.operation(ctx)
				if err != nil {
					h.close()
					t.Fatal(err)
				}
				task, err = h.core.Submit(ctx, h.token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "Answer using the acquired evidence", InputRefs: []string{evidenceRef}, Constraints: tasks.Constraints{MaxSteps: 1, ModelRequests: 1, ModelTokens: 4096, DeadlineUnix: h.now().Add(time.Minute).Unix()}})
				if err != nil {
					h.close()
					t.Fatal(err)
				}
				snapshot, err = h.core.Load(ctx, task.Ref)
				if err != nil {
					h.close()
					t.Fatal(err)
				}
				for _, kind := range []string{"claim", "start"} {
					snapshot, err = h.work.Commit(ctx, tasks.WorkChange{ChangeID: kind, Kind: kind, Qualification: tasks.QualificationOf(snapshot)})
					if err != nil {
						h.close()
						t.Fatal(err)
					}
				}
			}
			generations, err := h.work.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 2048, OutputTokens: 512})
			if err != nil {
				h.close()
				t.Fatal(err)
			}
			generations = generations.WithOutputIdentities(h.operation)
			baseline, err := generations.ReserveDecision(ctx, snapshot)
			if err != nil {
				h.close()
				t.Fatal(err)
			}
			original := baseline.Generations[0]
			saved := evidenceProcessConfig{Token: h.token, Mode: mode, URLs: urls, Task: task.Ref, Evidence: evidenceRef}
			raw, _ := json.Marshal(saved)
			if err = os.WriteFile(filepath.Join(root, "answer-probe.json"), raw, 0600); err != nil {
				h.close()
				t.Fatal(err)
			}
			h.close()
			child := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestEvidenceAnswerCrashProbe$")
			child.Env = append(os.Environ(), "LERNA_EVIDENCE_PROCESS_ROOT="+root)
			output, err := child.CombinedOutput()
			var exited *exec.ExitError
			if !errors.As(err, &exited) || exited.ExitCode() != 76 {
				t.Fatalf("child exit: %v %s", err, output)
			}
			h, err = open(ctx, root, saved.Token, urls)
			if err != nil {
				t.Fatal(err)
			}
			defer h.close()
			current, err := h.core.Load(ctx, task.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.sameTask && len(current.Actions.Queries) <= len(baseline.Actions.Queries) {
				t.Fatal("child evidence reads bypassed task query budget")
			}
			if len(current.Generations) != 1 || current.Generations[0].OutputOperation != original.OutputOperation || current.Generations[0].Started != 1 {
				t.Fatal("process exit lost original answer dispatch identity")
			}
			if scenario.exhausted {
				// Reinstall the same explicit public source/goal policy as normal
				// recovery before checking the retained answer with an observer.
				if _, _, _, err = bindEvidenceProcess(ctx, h, current, evidenceRef); err != nil {
					t.Fatal(err)
				}
				exhaustEvidenceQueries(t, ctx, h, current, evidenceRef)
				h.close()
				h, err = open(ctx, root, saved.Token, urls)
				if err != nil {
					t.Fatal(err)
				}
				defer h.close()
				current, err = h.core.Load(ctx, task.Ref)
				if err != nil {
					t.Fatal(err)
				}
			}
			content, result, generations, err := bindEvidenceProcess(ctx, h, current, evidenceRef)
			if err != nil {
				t.Fatal(err)
			}
			port, err := answers.BindEvidencePort(generations, result, "local", content)
			if err != nil {
				t.Fatal(err)
			}
			queriesBeforeRecovery := 0
			if current.Actions != nil {
				queriesBeforeRecovery = len(current.Actions.Queries)
			}
			expectedRecoveredRef := ""
			if scenario.revoke {
				expectedRecoveredRef = verifyRevokedAnswerRecovery(t, ctx, h, port, current, evidenceRef)
				// Restore the explicit public test policy. Recovery must then
				// adopt the same saved output, without a new model request.
				content, result, generations, err = bindEvidenceProcess(ctx, h, current, evidenceRef)
				if err != nil {
					t.Fatal(err)
				}
				port, err = answers.BindEvidencePort(generations, result, "local", content)
				if err != nil {
					t.Fatal(err)
				}
			}
			completed, recoveryErr := port.Recover(ctx, task.Ref)
			if mode == "content" && !scenario.exhausted {
				if expectedRecoveredRef != "" && completed.Task.Result != expectedRecoveredRef {
					t.Fatal("restored authorization replaced original saved answer")
				}
				if recoveryErr != nil || completed.Task.State != "COMPLETED" {
					t.Fatalf("stored evidence answer not recovered: %v", recoveryErr)
				}
				raw, err := h.access.Read(ctx, h.token, completed.Task.Result, h.cap)
				if err != nil {
					t.Fatal(err)
				}
				observer, err := researchcontext.NewPages(h.evidence, completed.Task, "local", []string{evidenceRef})
				if err != nil {
					t.Fatal(err)
				}
				actualInput, err := observer.Assemble(ctx, completed.Task, "local", brain.MaxInputBytes)
				if err != nil {
					t.Fatal(err)
				}
				if err = brain.ValidateEvidenceAnswer(raw, actualInput, brain.MaxAnswerBytes); err != nil {
					t.Fatal(err)
				}
				before := completed.Task.Result
				replay, err := port.Recover(ctx, task.Ref)
				if err != nil || replay.Task.Result != before {
					t.Fatal("publication replay changed the answer identity")
				}
			} else {
				if recoveryErr == nil {
					t.Fatal("unsaved model response became recoverable")
				}
			}
			if scenario.exhausted {
				if recoveryErr == nil || !strings.Contains(recoveryErr.Error(), "QUERY_BUDGET_EXCEEDED") {
					t.Fatalf("exhausted recovery bypassed quota: %v", recoveryErr)
				}
				if _, err = port.Recover(ctx, task.Ref); err == nil || !strings.Contains(err.Error(), "QUERY_BUDGET_EXCEEDED") {
					t.Fatalf("repeated recovery renewed quota: %v", err)
				}
			}
			current, err = h.core.Load(ctx, task.Ref)
			if err != nil {
				t.Fatal(err)
			}
			if (mode == "response" || scenario.exhausted) && (current.Task.Result != "" || current.Task.State == "COMPLETED") {
				t.Fatal("unrecoverable answer completed the task")
			}
			if current.Task.ModelReservedTokens != baseline.Task.ModelReservedTokens {
				t.Fatal("unknown model token reservation was refunded")
			}
			if current.Task.ModelUsedRequests+current.Task.ModelReservedRequests != baseline.Task.ModelUsedRequests+1 || current.Generations[0].OutputOperation != original.OutputOperation || current.Generations[0].Started != 1 || hits.Load() != 1 {
				t.Fatal("answer recovery redispatched work or released unknown usage")
			}
			if scenario.sameTask {
				if scenario.exhausted && (queriesBeforeRecovery != int(current.Actions.Limits.MaxQueries) || len(current.Actions.Queries) != queriesBeforeRecovery) {
					t.Fatal("exhausted query history changed across restart or recovery")
				}
				if (!scenario.exhausted && len(current.Actions.Queries) <= queriesBeforeRecovery) || len(current.Actions.Queries) > int(current.Actions.Limits.MaxQueries) {
					t.Fatal("recovery did not consume original remaining query quota")
				}
				if !sameActionsWithAppendedQueries(baseline.Actions, current.Actions) || !reflect.DeepEqual(current.ExecutionReports, baseline.ExecutionReports) {
					t.Fatal("answer recovery changed original action history")
				}
				budget, err := h.attempts.Budget(ctx, task.Ref)
				if err != nil || budget.Charged != 1 {
					t.Fatal("answer recovery changed the original action budget")
				}
			}
			if err = generations.BeginRequest(ctx, original.Qualification, 0); err == nil {
				t.Fatal("original model request ordinal became reusable")
			}
			if current.Actions != nil {
				t.Logf("queries_before_child=%d before_recovery=%d after_recovery=%d limit=%d", len(baseline.Actions.Queries), queriesBeforeRecovery, len(current.Actions.Queries), current.Actions.Limits.MaxQueries)
			}
			t.Logf("same_task=%t revoked_then_restored=%t window=%s child_exit=76 http=%d state=%s model_used=%d model_reserved=%d tokens_reserved=%d", scenario.sameTask, scenario.revoke, mode, hits.Load(), current.Task.State, current.Task.ModelUsedRequests, current.Task.ModelReservedRequests, current.Task.ModelReservedTokens)
		})
	}
}

func bindEvidenceProcess(ctx context.Context, h *harness, run tasks.RunSnapshot, ref string) (*researchcontext.PageContext, *answers.ContentAccess, *tasks.GenerationPort, error) {
	rules := []contentpolicy.Rule{}
	for _, source := range []*wire.ContentSource{{Kind: "web", Key: "start", Revision: 1}, {Kind: "web", Key: "final", Revision: 1}, {Kind: "task-goal", Key: "inline", Revision: 1}} {
		rules = append(rules, contentpolicy.Rule{Kind: source.Kind, Key: source.Key, Revision: source.Revision, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(10 * time.Minute).Unix()})
	}
	if err := h.policy.Replace(rules); err != nil {
		return nil, nil, nil, err
	}
	var content answers.Content = h.content
	evidence := h.evidence
	if run.Actions != nil {
		actions, err := h.work.Actions(run.Actions.Limits)
		if err != nil {
			return nil, nil, nil, err
		}
		content, err = taskcontent.New(h.content, actions, tasks.QualificationOf(run))
		if err != nil {
			return nil, nil, nil, err
		}
		sources := map[string]*wire.ContentSource{}
		for i, url := range h.urls {
			sources[url] = &wire.ContentSource{Kind: "web", Key: fixtureSourceKey(i), Revision: 1}
		}
		evidence, err = fetchcontent.New(content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, fetchcontent.Config{Clock: h.clock, Resource: "root", Purpose: "task", RetainUntil: h.now().Add(10 * time.Minute).Unix(), Sources: sources})
		if err != nil {
			return nil, nil, nil, err
		}
	}
	input, err := researchcontext.NewPages(evidence, run.Task, "local", []string{ref})
	if err != nil {
		return nil, nil, nil, err
	}
	output, err := answers.NewContentAccess(content, h.policy, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, &wire.ContentSource{Kind: "task-goal", Key: "inline", Revision: 1}, "task", h.clock, time.Minute)
	if err != nil {
		return nil, nil, nil, err
	}
	generations, err := h.work.Generations(tasks.GenerationLimits{Requests: 1, InputTokens: 2048, OutputTokens: 512})
	if err != nil {
		return nil, nil, nil, err
	}
	return input, output, generations.WithOutputIdentities(h.operation), nil
}

type crashEvidenceModel struct {
	brain.Model
	mode string
}

func (m crashEvidenceModel) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	result, err := m.Model.Generate(ctx, in)
	if err == nil && m.mode == "response" {
		os.Exit(76)
	}
	return result, err
}

type crashEvidenceOutput struct {
	brain.Output
	mode string
}

func (o crashEvidenceOutput) Save(ctx context.Context, in tasks.DecisionInput, data []byte) (string, error) {
	ref, err := o.Output.Save(ctx, in, data)
	if err == nil && o.mode == "content" {
		os.Exit(76)
	}
	return ref, err
}
func TestEvidenceAnswerCrashProbe(t *testing.T) {
	root := os.Getenv("LERNA_EVIDENCE_PROCESS_ROOT")
	if root == "" {
		t.Skip("subprocess probe")
	}
	raw, err := os.ReadFile(filepath.Join(root, "answer-probe.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved evidenceProcessConfig
	if json.Unmarshal(raw, &saved) != nil {
		t.Fatal("invalid answer probe configuration")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	h, err := open(ctx, root, saved.Token, saved.URLs)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	run, err := h.core.Load(ctx, saved.Task)
	if err != nil {
		t.Fatal(err)
	}
	if run.Actions != nil {
		model := &fetchActionModel{}
		actions, err := h.work.Actions(run.Actions.Limits)
		if err != nil {
			t.Fatal(err)
		}
		runner, err := brain.NewActions(model, &fetchActionHost{h: h}, actions)
		if err != nil {
			t.Fatal(err)
		}
		restored, err := runner.Run(ctx, saved.Task)
		if err != nil || model.calls != 0 || !reflect.DeepEqual(restored.Actions, run.Actions) {
			t.Fatal("child process restarted completed actions")
		}
	}
	input, output, generations, err := bindEvidenceProcess(ctx, h, run, saved.Evidence)
	if err != nil {
		t.Fatal(err)
	}
	writer, err := brain.NewEvidenceAnswer(crashEvidenceModel{decisionFixtureModel{evidence: true}, saved.Mode}, input, crashEvidenceOutput{output, saved.Mode}, generations, brain.Config{MaxInputBytes: brain.MaxInputBytes, MaxOutputBytes: 1024, SettlementTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := writer.Decide(ctx, tasks.DecisionInput{Task: run.Task, Work: run.Work[0], Generation: run.Generations[0]})
	t.Fatalf("answer exit window not reached: proposal=%s error=%v", proposal.Kind, err)
}

func verifyRevokedAnswerRecovery(t *testing.T, ctx context.Context, h *harness, port *answers.Port, before tasks.RunSnapshot, evidenceRef string) string {
	t.Helper()
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	stored, err := h.content.Call(ctx, binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: before.Generations[0].OutputOperation, Purpose: "task"})
	if err != nil || stored.GetRecord().GetState() != "available" {
		t.Fatal("saved answer missing before revocation")
	}
	answerRef := answers.Reference(stored.Record.Ref)
	rules := []contentpolicy.Rule{}
	for _, source := range []*wire.ContentSource{{Kind: "web", Key: "final", Revision: 1}, {Kind: "task-goal", Key: "inline", Revision: 1}} {
		rules = append(rules, contentpolicy.Rule{Kind: source.Kind, Key: source.Key, Revision: source.Revision, Actions: []string{"store", "process", "retain", "discover", "disclose", "delete"}, Purposes: []string{"task"}, Locations: []string{"local"}, RetainUntil: h.now().Add(10 * time.Minute).Unix()})
	}
	if err = h.policy.Replace(rules); err != nil {
		t.Fatal(err)
	}
	if _, err = port.Recover(ctx, before.Task.Ref); err == nil {
		t.Fatal("revoked saved answer was published")
	}
	for _, ref := range []string{evidenceRef, answerRef} {
		parsed, err := answers.ParseReference(ref)
		if err != nil {
			t.Fatal(err)
		}
		out, err := h.content.Call(ctx, binding, &wire.ContentRequest{Method: "READ", Ref: parsed, Purpose: "task", Limit: 1})
		if artifacts.Code(err) != "PERMISSION_DENIED" || len(out.GetData()) != 0 {
			t.Fatal("revoked source or derived answer disclosed bytes")
		}
	}
	after, err := h.core.Load(ctx, before.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if after.Task.Result != "" || after.Task.State == "COMPLETED" || after.Task.ModelReservedRequests != before.Task.ModelReservedRequests || after.Task.ModelReservedTokens != before.Task.ModelReservedTokens || !sameActionsWithAppendedQueries(before.Actions, after.Actions) || !reflect.DeepEqual(after.ExecutionReports, before.ExecutionReports) {
		t.Fatal("denied recovery mutated task facts or released usage")
	}
	if before.Actions != nil && len(after.Actions.Queries) != len(before.Actions.Queries)+1 {
		t.Fatal("denied recovery lookup was not charged exactly once")
	}
	return answerRef
}

// Query observations are append-only; every other action fact must be identical.
func sameActionsWithAppendedQueries(before, after *tasks.ActionState) bool {
	if before == nil || after == nil {
		return before == nil && after == nil
	}
	if len(after.Queries) < len(before.Queries) || !reflect.DeepEqual(after.Queries[:len(before.Queries)], before.Queries) {
		return false
	}
	a, b := *before, *after
	b.Queries = a.Queries
	return reflect.DeepEqual(a, b)
}

// Spend the remaining quota on actual authorized evidence observations, then
// verify that the saved answer is still present before testing recovery denial.
func exhaustEvidenceQueries(t *testing.T, ctx context.Context, h *harness, run tasks.RunSnapshot, ref string) {
	t.Helper()
	actions, err := h.work.Actions(run.Actions.Limits)
	if err != nil {
		t.Fatal(err)
	}
	content, err := taskcontent.New(h.content, actions, tasks.QualificationOf(run))
	if err != nil {
		t.Fatal(err)
	}
	binding := artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}
	parsed, err := answers.ParseReference(ref)
	if err != nil {
		t.Fatal(err)
	}
	for i := len(run.Actions.Queries); i < int(run.Actions.Limits.MaxQueries); i++ {
		if _, err = content.Call(ctx, binding, &wire.ContentRequest{Method: "READ", Ref: parsed, Purpose: "task", Limit: 1}); err != nil {
			t.Fatal(err)
		}
	}
	// An independently authorized observer establishes that an answer exists;
	// the task worker cannot use this observer to bypass its exhausted budget.
	out, err := h.content.Call(ctx, binding, &wire.ContentRequest{Method: "LOOKUP", OperationId: run.Generations[0].OutputOperation, Purpose: "task"})
	if err != nil || out.GetRecord().GetState() != "available" {
		t.Fatal("no saved answer to test exhausted recovery")
	}
}
