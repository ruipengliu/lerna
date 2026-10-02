package integration_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/durable"
	o "github.com/ruipengliu/lerna/internal/orchestrator"
	pg "github.com/ruipengliu/lerna/internal/storage/postgres"
)

func taskCommand(method, target string, payload any, revision *int64) api.Command {
	return api.Command{CommandID: durable.NewID("command"), Method: method, TargetID: target, ExpiresAt: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), ExpectedRevision: revision, Payload: o.Raw(payload)}
}
func taskSubmitCommand(f *taskFixture) api.Command {
	return taskCommand("task.submit", taskScope.OwnerID, api.TaskSubmitInput{OrchestratorID: taskScope.OwnerID, GoalRef: f.goal, Constraints: []string{}, PolicyRef: f.policy.Ref, Deadline: time.Now().Add(time.Minute).UTC().Format(time.RFC3339Nano), Budget: []api.BudgetLimit{{Unit: "usd", Limit: "10"}}}, nil)
}
func executeTask(t *testing.T, s api.Orchestrator, command api.Command) api.CommandResult {
	t.Helper()
	r, e := s.Execute(ctx(), taskCaller, command)
	if e != nil {
		t.Fatal(command.Method, e)
	}
	return r
}

func executeTaskAtCurrentRevision(t *testing.T, s api.Orchestrator, method, taskID string, payload any) api.CommandResult {
	t.Helper()
	for attempt := 0; attempt < 8; attempt++ {
		current := readTask(t, s, taskID)
		result := executeTask(t, s, taskCommand(method, taskID, payload, &current.Revision))
		if result.Stage != "rejected" || result.Failure == nil || result.Failure.Code != "revision_conflict" {
			return result
		}
	}
	t.Fatalf("%s remained in revision conflict after refreshing the task", method)
	return api.CommandResult{}
}
func inputRequest(t *testing.T, s api.Orchestrator, task api.Task) api.InputRequestView {
	t.Helper()
	for _, w := range task.WaitReasons {
		if w.Kind == "input" {
			r, e := s.Query(ctx(), taskCaller, api.Query{Method: "interaction.request_read", TargetID: taskScope.OwnerID, Payload: o.Raw(api.InteractionRequestReadInput{RequestRef: *w.ObjectRef})})
			if e != nil {
				t.Fatal(e)
			}
			var v api.InteractionRequestReadOutput
			_ = json.Unmarshal(r.Value, &v)
			return v.Request
		}
	}
	t.Fatal("no input request")
	return api.InputRequestView{}
}

func TestOrchestratorOriginalCommandAndStrictBytes(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			service := newTaskService(t, s, f)
			command := taskSubmitCommand(f)
			results := make(chan api.CommandResult, 8)
			failures := make(chan error, 8)
			var wg sync.WaitGroup
			for i := 0; i < 8; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					r, e := service.Execute(ctx(), taskCaller, command)
					results <- r
					failures <- e
				}()
			}
			wg.Wait()
			close(results)
			close(failures)
			for e := range failures {
				if e != nil {
					t.Fatal(e)
				}
			}
			resource := ""
			for r := range results {
				if r.Stage != "applied" || resource != "" && r.ResourceID != resource {
					t.Fatal(r)
				}
				resource = r.ResourceID
			}
			f.mu.Lock()
			f.ready = false
			f.mu.Unlock()
			replayed := executeTask(t, service, command)
			if replayed.ResourceID != resource {
				t.Fatal(replayed)
			}
			changed := command
			changed.Payload = json.RawMessage(strings.Replace(string(command.Payload), "\"10\"", "\"11\"", 1))
			if _, e := service.Execute(ctx(), taskCaller, changed); !errors.Is(e, durable.ErrConflict) {
				t.Fatal("changed command", e)
			}
			malformed := taskSubmitCommand(f)
			malformed.Payload = []byte(`{"orchestrator_id":"x","orchestrator_id":"y"}`)
			if _, e := service.Execute(ctx(), taskCaller, malformed); e == nil {
				t.Fatal("duplicate original key accepted")
			}
			if _, e := service.CommandStatus(ctx(), taskCaller, malformed.CommandID); !errors.Is(e, durable.ErrNotFound) {
				t.Fatal("malformed command created identity", e)
			}
			f.mu.Lock()
			f.deny[resource] = true
			f.mu.Unlock()
			if _, e := service.CommandStatus(ctx(), taskCaller, command.CommandID); e == nil {
				t.Fatal("cached receipt bypassed current disclosure")
			}
		})
	}
}

func TestOrchestratorInputConsumedOnce(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			f.mode = "input"
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "10")
			runTaskService(t, service)
			waiting := awaitTask(t, service, f, task.TaskID, func(v api.Task) bool {
				for _, w := range v.WaitReasons {
					if w.Kind == "input" && w.ResumeCondition == "consume_exact_request_revision" {
						return true
					}
				}
				return false
			})
			request := inputRequest(t, service, waiting)
			body, _ := durable.CanonicalJSON([]byte(`{"format":"input-answer/1","action_id":"answer","fields":{"answer":"1"}}`))
			f.mu.Lock()
			answer := f.put("answer", body)
			f.mu.Unlock()
			input := api.TaskInputInput{RequestID: request.RequestID, RequestRevision: request.Revision, AnswerRef: o.Raw(answer), PreviewRefs: request.RequiredContentRefs}
			commands := []api.Command{taskCommand("task.input", task.TaskID, input, nil), taskCommand("task.input", task.TaskID, input, nil)}
			results := make(chan api.CommandResult, 2)
			var wg sync.WaitGroup
			for _, command := range commands {
				wg.Add(1)
				go func(command api.Command) {
					defer wg.Done()
					r, e := service.Execute(ctx(), taskCaller, command)
					if e != nil {
						t.Error(e)
					}
					results <- r
				}(command)
			}
			wg.Wait()
			close(results)
			applied := 0
			rejected := 0
			for r := range results {
				if r.Stage == "applied" {
					applied++
				} else if r.Stage == "rejected" {
					rejected++
				}
			}
			if applied != 1 || rejected != 1 {
				t.Fatal(applied, rejected)
			}
			awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" })
		})
	}
}

func TestOrchestratorTrustedAcceptance(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			f.mode = "acceptance"
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "10")
			runTaskService(t, service)
			waiting := awaitTask(t, service, f, task.TaskID, func(v api.Task) bool {
				for _, w := range v.WaitReasons {
					if w.Kind == "input" && w.ResumeCondition == "consume_exact_request_revision" {
						return true
					}
				}
				return false
			})
			request := inputRequest(t, service, waiting)
			if request.Kind != "acceptance" {
				t.Fatal(request.Kind)
			}
			confirmationID := o.ID("confirmation", task.TaskID)
			input := api.TaskAcceptResultInput{Decision: "accept", RequestID: request.RequestID, RequestRevision: request.Revision, CandidateHash: *request.CandidateHash, GoalRevision: *request.GoalRevision, ConfirmationRef: api.ObjectRef{OwnerID: taskScope.OwnerID, ID: confirmationID, Revision: 2}}
			command := taskCommand("task.accept_result", task.TaskID, input, nil)
			consumer := api.ConfirmationConsumerCommand{CommandID: command.CommandID, Method: command.Method, TargetID: command.TargetID, ExpiresAt: command.ExpiresAt, Payload: command.Payload}
			now := time.Now().UTC().Format(time.RFC3339Nano)
			actor := taskCaller.ActorID
			f.mu.Lock()
			f.confirmation = &api.ConfirmationRecord{ConfirmationID: confirmationID, OwnerID: taskScope.OwnerID, Revision: 2, ConsumerMethod: command.Method, ConsumerCommandID: command.CommandID, ConsumerTargetID: task.TaskID, ConsumerCommand: consumer, IntentHash: o.Hash(map[string]any{"owner_id": taskScope.OwnerID, "consumer_command": consumer}), Challenge: "sha256:" + strings.Repeat("c", 64), ExpiresAt: command.ExpiresAt, State: "approved", DecidedAt: &now, DecidedBy: &actor, TrustedUserSessionRef: &api.ObjectRef{OwnerID: o.ID("identity", "tests"), ID: o.ID("session", "tests"), Revision: 1}}
			f.mu.Unlock()
			r := executeTask(t, service, command)
			if r.Stage != "applied" {
				t.Fatal(r.Failure)
			}
			finished := awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" })
			if len(finished.OpenEffects) != 0 {
				t.Fatal("acceptance hid effects")
			}
			r = executeTask(t, service, command)
			if r.Stage != "applied" {
				t.Fatal(r)
			}
			result, e := service.Query(ctx(), taskCaller, api.Query{Method: "task.result", TargetID: task.TaskID, Payload: o.Raw(api.TaskResultInput{})})
			if e != nil {
				t.Fatal(e)
			}
			var view api.TaskResultView
			_ = json.Unmarshal(result.Value, &view)
			if view.Result.CompletionBasis != "user_accepted" {
				t.Fatal(view.Result)
			}
		})
	}
}

func TestOrchestratorPauseAndCancelKeepOriginalBill(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			s := newSuite(t, driver)
			f := newTaskFixture()
			f.mode = "pending"
			service := newTaskService(t, s, f)
			task := submitTask(t, service, f, "10")
			runTaskService(t, service)
			awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { f.mu.Lock(); defer f.mu.Unlock(); return len(f.decisions) == 1 })
			paused := executeTaskAtCurrentRevision(t, service, "task.pause", task.TaskID, api.TaskPauseInput{Reason: "pause"})
			if paused.Stage != "applied" {
				t.Fatalf("pause was not applied: stage=%s failure=%+v", paused.Stage, paused.Failure)
			}
			awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Control == "paused" && !v.AccountingOpen })
			cancelled := executeTaskAtCurrentRevision(t, service, "task.cancel", task.TaskID, api.TaskCancelInput{Reason: "cancel"})
			if cancelled.Stage != "applied" {
				t.Fatalf("cancel was not applied: stage=%s failure=%+v", cancelled.Stage, cancelled.Failure)
			}
			current := readTask(t, service, task.TaskID)
			if current.Status != "cancelled" || current.Budget[0].Spent.Amount != "0.2" {
				t.Fatal(current)
			}
			revision := current.Revision
			rejected := executeTask(t, service, taskCommand("task.resume", task.TaskID, api.TaskResumeInput{Reason: "resume"}, &revision))
			if rejected.Stage != "rejected" {
				t.Fatal("terminal reopened", rejected)
			}
			f.mu.Lock()
			defer f.mu.Unlock()
			if f.physicalDecisions != 1 {
				t.Fatal(f.physicalDecisions)
			}
		})
	}
}

func TestOrchestratorDefectGateAndHistoricalNotice(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		for _, first := range []bool{true, false} {
			name := "after"
			if first {
				name = "before"
			}
			t.Run(driver+"/"+name, func(t *testing.T) {
				s := newSuite(t, driver)
				f := newTaskFixture()
				service := newTaskService(t, s, f)
				task := submitTask(t, service, f, "10")
				rule := f.policy.Requirements[0].RuleRef
				eval := api.ComponentRef{ID: o.ID("evaluator", "test"), Version: "1.0.0", Digest: "sha256:" + strings.Repeat("b", 64)}
				f.mu.Lock()
				proof := f.put("defect-proof", []byte("trusted fixed-scope defect"))
				f.mu.Unlock()
				d := api.EvidenceDefect{ID: o.ID("defect", task.TaskID), RuleRef: rule, EvaluatorRef: eval, TaskID: task.TaskID, GoalRevision: 1, ArtifactHash: f.artifact.Hash, ProofRef: proof}
				caller := taskCaller
				caller.SourceOwnerID = eval.ID
				if first {
					if e := service.RegisterEvidenceDefect(ctx(), caller, d); e != nil {
						t.Fatal(e)
					}
				}
				runTaskService(t, service)
				if !first {
					awaitTask(t, service, f, task.TaskID, func(v api.Task) bool { return v.Status == "succeeded" })
					if e := service.RegisterEvidenceDefect(ctx(), caller, d); e != nil {
						t.Fatal(e)
					}
					deadline := time.Now().Add(3 * time.Second)
					for time.Now().Before(deadline) {
						r, e := service.Query(ctx(), taskCaller, api.Query{Method: "task.result", TargetID: task.TaskID, Payload: o.Raw(api.TaskResultInput{})})
						if e != nil {
							t.Fatal(e)
						}
						if len(r.Gaps) > 0 {
							return
						}
						time.Sleep(10 * time.Millisecond)
					}
					t.Fatal("historical notice was not handed off")
				} else {
					awaitTask(t, service, f, task.TaskID, func(v api.Task) bool {
						f.mu.Lock()
						defer f.mu.Unlock()
						return len(f.decisions) > 0 && v.Budget[0].Spent.Amount != "0"
					})
					time.Sleep(150 * time.Millisecond)
					if v := readTask(t, service, task.TaskID); v.Status == "succeeded" {
						t.Fatal("known defect bypassed completion")
					}
				}
			})
		}
	}
}

func TestOrchestratorRealUnknownSubmitCommit(t *testing.T) {
	s := newSuite(t, "postgres")
	proxy, armed := commitProxy(t, s.appURL)
	backend, e := pg.Open(ctx(), proxy, []durable.Scope{taskScope}, 2, false)
	if e != nil {
		t.Fatal(e)
	}
	defer backend.Close()
	original := s.store
	s.store = backend
	f := newTaskFixture()
	service := newTaskService(t, s, f)
	s.store = original
	command := taskSubmitCommand(f)
	// The first lookup is read-only; cut the acknowledgement of the actual
	// domain commit, including Task, strict balances, first job and receipt.
	f.mu.Lock()
	f.beforeSubmit = func() { armed.Store(true) }
	f.mu.Unlock()
	_, e = service.Execute(ctx(), taskCaller, command)
	if !errors.Is(e, api.ErrCommitUnknown) {
		t.Fatal("actual acknowledgement loss", e)
	}
	if armed.Load() {
		t.Fatal("proxy not triggered")
	}
	r, e := service.CommandStatus(context.Background(), taskCaller, command.CommandID)
	if e != nil {
		t.Fatal(e)
	}
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	var count int
	if e = s.raw.QueryRowContext(ctx(), "SELECT count(*) FROM orchestrator_tasks").Scan(&count); e != nil || count != 1 {
		t.Fatal(count, e)
	}
}
