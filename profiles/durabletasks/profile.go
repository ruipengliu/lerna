// Package durabletasks verifies actual local task admission, not worker execution.
package durabletasks

import (
	"context"
	"encoding/json"
	"fmt"
	sqliteauth "lerna/adapters/authorization/sqlite"
	tasklocal "lerna/adapters/tasks/local"
	"lerna/authorization"
	"lerna/conformance"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

const ID = "durable-tasks-v1"

type clock struct{ now time.Time }

func (c *clock) Now() (time.Time, error) { return c.now, nil }
func config() authorization.Config {
	return authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: 2 * time.Minute, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second}
}
func taskConfig() tasks.Config {
	return tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10}
}

type harness struct {
	db       *sqliteauth.Store
	auth     *authorization.Service
	service  *tasks.Service
	client   *sdk.TaskClient
	token    string
	clock    *clock
	dir      string
	revision uint64
}

func open(dir, token string, now time.Time) (*harness, error) {
	db, err := sqliteauth.Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		return nil, err
	}
	c := &clock{now}
	a, err := authorization.New(db, c, config())
	if err != nil {
		db.Close()
		return nil, err
	}
	s, err := tasks.New(a, taskConfig())
	if err != nil {
		db.Close()
		return nil, err
	}
	return &harness{db: db, auth: a, service: s, client: sdk.NewTaskClient(tasklocal.Bind(s, token), "local"), token: token, clock: c, dir: dir}, nil
}
func (h *harness) mutate(ctx context.Context, cmd *wire.AuthorizationCommand) error {
	id, err := h.auth.NewOperation(ctx, h.token)
	if err != nil {
		return err
	}
	cmd.ExpectedRevision = h.revision
	_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: id, Command: cmd})
	if err == nil {
		h.revision++
	}
	return err
}
func setup(ctx context.Context, dir string) (*harness, error) {
	h, err := open(dir, "", time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC))
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			h.db.Close()
		}
	}()
	token, err := h.auth.Bootstrap(ctx, "local", "admin")
	if err != nil {
		return nil, err
	}
	h.token = token
	h.client = sdk.NewTaskClient(tasklocal.Bind(h.service, token), "local")
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: h.clock.now.Add(time.Hour).Unix()}
	if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "tasks", Scope: scope}}}}}); err != nil {
		return nil, err
	}
	if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "tasks", Subject: "admin", Mode: "continuous", Scope: scope}}}); err != nil {
		return nil, err
	}
	ok = true
	return h, nil
}
func (h *harness) submission(ctx context.Context) (tasks.Submission, error) {
	id, err := h.auth.NewOperation(ctx, h.token)
	return tasks.Submission{Namespace: "local", OperationID: id, Goal: "prepare an answer", InputRefs: []string{"evidence/reference"}, Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: h.clock.now.Add(time.Minute).Unix()}}, err
}
func expect(err error, code authorization.Code) error {
	if !authorization.Is(err, code) {
		return fmt.Errorf("expected %s, observed %v", code, err)
	}
	return nil
}
func check(ctx context.Context, name string) error {
	dir, err := os.MkdirTemp("", "lerna-task-check-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := setup(ctx, dir)
	if err != nil {
		return err
	}
	defer func() {
		if h != nil {
			h.db.Close()
		}
	}()
	in, err := h.submission(ctx)
	if err != nil {
		return err
	}
	if name == "missing-constraints" {
		in.Constraints = tasks.Constraints{}
		_, err = h.client.Submit(ctx, in)
		return expect(err, authorization.Invalid)
	}
	if name == "forged-identity" {
		_, err = sdk.NewTaskClient(tasklocal.Bind(h.service, "admin"), "local").Submit(ctx, in)
		return expect(err, authorization.Unauthenticated)
	}
	if name == "cross-namespace" {
		in.Namespace = "other"
		_, err = h.service.Submit(ctx, h.token, in)
		return expect(err, authorization.Denied)
	}
	if name == "expired-first-admission" {
		h.clock.now = h.clock.now.Add(2 * time.Minute)
		_, err = h.client.Submit(ctx, in)
		return expect(err, authorization.Expired)
	}
	first, err := h.client.Submit(ctx, in)
	if err != nil {
		return err
	}
	switch name {
	case "submit-get-reopen":
		if err = h.db.Close(); err != nil {
			return err
		}
		h, err = open(dir, h.token, h.clock.now)
		if err != nil {
			return err
		}
		task, err := h.client.Get(ctx, first.Ref)
		if err != nil {
			return err
		}
		if task.State != "QUEUED" || task.Version != 1 || task.Goal != in.Goal {
			return fmt.Errorf("incorrect recovered task")
		}
		run, err := h.service.Load(ctx, first.Ref)
		if err != nil {
			return err
		}
		if len(run.Work) != 1 || len(run.Records) != 1 {
			return fmt.Errorf("incomplete atomic bundle")
		}
		receipt, err := h.service.LookupCommit(ctx, first.Ref, run.LastCommit.ChangeID)
		if err != nil {
			return err
		}
		if receipt != run.LastCommit {
			return fmt.Errorf("commit mismatch")
		}
	case "duplicate-submission":
		second, err := h.client.Submit(ctx, in)
		if err != nil {
			return err
		}
		if second.Ref != first.Ref {
			return fmt.Errorf("duplicate task")
		}
	case "semantic-conflict":
		in.Goal = "different"
		_, err = h.client.Submit(ctx, in)
		return expect(err, authorization.IdentityConflict)
	case "cross-domain-conflict":
		_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: in.OperationID, Command: &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}}})
		return expect(err, authorization.IdentityConflict)
	case "current-policy-before-receipt":
		if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}}); err != nil {
			return err
		}
		_, err = h.client.LookupOperation(ctx, in.OperationID)
		return expect(err, authorization.Denied)
	case "closed-window-cleanup-reopen":
		h.clock.now = h.clock.now.Add(3 * time.Minute)
		if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_CleanRecords{CleanRecords: true}}); err != nil {
			return err
		}
		if err = h.db.Close(); err != nil {
			return err
		}
		h, err = open(dir, h.token, h.clock.now)
		if err != nil {
			return err
		}
		second, err := h.client.Submit(ctx, in)
		if err != nil {
			return err
		}
		if second.Ref != first.Ref {
			return fmt.Errorf("old task revived")
		}
	case "bounded-recovery":
		for i := 0; i < 2; i++ {
			next, err := h.submission(ctx)
			if err != nil {
				return err
			}
			if _, err = h.client.Submit(ctx, next); err != nil {
				return err
			}
		}
		seen := map[string]bool{}
		after := ""
		for {
			page, err := h.service.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 1, After: after})
			if err != nil {
				return err
			}
			for _, r := range page.Runs {
				if seen[r.Task.Ref.TaskID] {
					return fmt.Errorf("duplicate page")
				}
				seen[r.Task.Ref.TaskID] = true
			}
			if page.Next == "" {
				break
			}
			after = page.Next
		}
		if len(seen) != 3 {
			return fmt.Errorf("missing candidates")
		}
	default:
		return fmt.Errorf("unknown check")
	}
	return nil
}

type plan struct {
	Token string
	Input tasks.Submission
	Now   time.Time
}
type crashStore struct {
	authorization.Store
	point string
}

func (s *crashStore) Commit(ctx context.Context, v uint64, state authorization.State) error {
	if len(state.RuntimeData) == 0 {
		return s.Store.Commit(ctx, v, state)
	}
	if s.point == "before-commit" {
		os.Exit(73)
	}
	err := s.Store.Commit(ctx, v, state)
	if err == nil {
		os.Exit(73)
	}
	return err
}

// RunProbe is an explicit disposable-process verification entry, not a service RPC.
func RunProbe(ctx context.Context, dir, point string) error {
	if point != "before-commit" && point != "lost-commit-reply" {
		return fmt.Errorf("unknown probe")
	}
	data, err := os.ReadFile(filepath.Join(dir, "plan.json"))
	if err != nil {
		return err
	}
	var p plan
	if err = json.Unmarshal(data, &p); err != nil {
		return err
	}
	db, err := sqliteauth.Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		return err
	}
	defer db.Close()
	a, err := authorization.New(&crashStore{db, point}, &clock{p.Now}, config())
	if err != nil {
		return err
	}
	s, err := tasks.New(a, taskConfig())
	if err != nil {
		return err
	}
	_, err = sdk.NewTaskClient(tasklocal.Bind(s, p.Token), "local").Submit(ctx, p.Input)
	if err != nil {
		return err
	}
	return fmt.Errorf("probe did not terminate")
}
func crashCheck(ctx context.Context, executable, point string) error {
	dir, err := os.MkdirTemp("", "lerna-task-crash-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	h, err := setup(ctx, dir)
	if err != nil {
		return err
	}
	in, err := h.submission(ctx)
	if err != nil {
		h.db.Close()
		return err
	}
	p := plan{h.token, in, h.clock.now}
	data, err := json.Marshal(p)
	if err != nil {
		h.db.Close()
		return err
	}
	if err = os.WriteFile(filepath.Join(dir, "plan.json"), data, 0600); err != nil {
		h.db.Close()
		return err
	}
	h.db.Close()
	probeCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	command := exec.CommandContext(probeCtx, executable, "task-crash-probe", dir, point)
	err = command.Run()
	if command.ProcessState == nil || command.ProcessState.ExitCode() != 73 {
		return fmt.Errorf("probe exit: %v", err)
	}
	h, err = open(dir, p.Token, p.Now)
	if err != nil {
		return err
	}
	defer h.db.Close()
	task, lookupErr := h.client.LookupOperation(ctx, in.OperationID)
	page, err := h.service.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
	if err != nil {
		return err
	}
	if point == "before-commit" {
		if err = expect(lookupErr, authorization.NotFound); err != nil {
			return err
		}
		if len(page.Runs) != 0 {
			return fmt.Errorf("partial creation")
		}
	} else {
		if lookupErr != nil {
			return lookupErr
		}
		if len(page.Runs) != 1 {
			return fmt.Errorf("missing committed work")
		}
	}
	retry, err := h.client.Submit(ctx, in)
	if err != nil {
		return err
	}
	if point == "lost-commit-reply" && retry.Ref != task.Ref {
		return fmt.Errorf("duplicate after unknown result")
	}
	run, err := h.service.Load(ctx, retry.Ref)
	if err != nil {
		return err
	}
	if len(run.Work) != 1 || len(run.Records) != 1 {
		return fmt.Errorf("incomplete recovery")
	}
	receipt, err := h.service.LookupCommit(ctx, retry.Ref, run.LastCommit.ChangeID)
	if err != nil {
		return err
	}
	if receipt != run.LastCommit {
		return fmt.Errorf("commit receipt mismatch")
	}
	return nil
}
func Profile(executable string) (conformance.Profile, error) {
	dir, err := os.MkdirTemp("", "lerna-task-version-")
	if err != nil {
		return conformance.Profile{}, err
	}
	defer os.RemoveAll(dir)
	db, err := sqliteauth.Open(filepath.Join(dir, "version.db"))
	if err != nil {
		return conformance.Profile{}, err
	}
	version, err := db.Version(context.Background())
	db.Close()
	if err != nil {
		return conformance.Profile{}, err
	}
	p := conformance.Profile{ID: ID, Version: "1", Method: "contractcheck -profile " + ID, Configuration: map[string]string{"sqlite_runtime": version, "sqlite_driver": "modernc.org/sqlite v1.58.0", "storage": "shared authorization/runtime atomic SQLite snapshot; WAL; synchronous=FULL", "clock": "controlled 2026-09-10; local trust only", "task_limits": "100 tasks; page limit 10; runtime 8 MiB; combined snapshot 16 MiB", "authorization_limits": "credential 24h; grant 1h; window 1m; management retention 2m; 32 rules; 128 resources; depth 16; work 4096; evaluation 1s", "task_policy": "root resource; task.submit/task.read; purpose task; location local; creator-only reads"}, Limitations: []string{"This profile verifies admission only; execution is verified separately by bounded-worker-v1. No external effects.", "RunStore and runtime transaction callbacks are trusted local interfaces, never public remote APIs.", "Task recovery records are retained; task cleanup and archive are unsupported. Capacity exhaustion rejects new work.", "Controlled clock and process termination do not prove hostile-host, power-loss or disk-corruption resilience."}}
	for _, name := range []string{"submit-get-reopen", "duplicate-submission", "semantic-conflict", "cross-domain-conflict", "current-policy-before-receipt", "closed-window-cleanup-reopen", "bounded-recovery", "missing-constraints", "forged-identity", "cross-namespace", "expired-first-admission"} {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "durable_task_sqlite", Input: name + " with real SQLite and binary SDK", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			err := check(ctx, name)
			if err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	for _, point := range []string{"before-commit", "lost-commit-reply"} {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "sqlite_task_process_recovery", Input: "terminate child at " + point + "; reopen original file", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := crashCheck(ctx, executable, point); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	p.Cases = append(p.Cases, conformance.Case{ID: "worker-execution", Evidence: "task_execution", Availability: conformance.NotRun, Expected: "verified separately by bounded-worker-v1"})
	return p, nil
}
