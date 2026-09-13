// Package worker verifies bounded scripted decisions; it makes no model-quality
// or external-effect claim.
package worker

import (
	"context"
	"fmt"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/tasklocal"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"lerna/tasks"
	"path/filepath"
	"sync"
	"time"
)

type clock struct {
	mu     sync.Mutex
	now    time.Time
	broken bool
}

func (c *clock) Now() (time.Time, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken {
		return time.Time{}, fmt.Errorf("untrusted clock")
	}
	return c.now, nil
}
func (c *clock) advance(d time.Duration) { c.mu.Lock(); defer c.mu.Unlock(); c.now = c.now.Add(d) }
func config() authorization.Config {
	return authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: 2 * time.Minute, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second}
}
func taskConfig() tasks.Config {
	return tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10}
}
func limits() tasks.RunLimits {
	return tasks.RunLimits{Lease: time.Second, RenewEvery: 200 * time.Millisecond, DecisionTimeout: time.Second, IOTimeout: time.Second, MaxAttempts: 3, MaxConcurrent: 1}
}

type harness struct {
	db         *sqliteauth.Store
	auth       *authorization.Service
	service    *tasks.Service
	client     *sdk.TaskClient
	clock      *clock
	token, dir string
	revision   uint64
}

func open(dir, token string, c *clock) (*harness, error) {
	db, err := sqliteauth.Open(filepath.Join(dir, "tasks.db"))
	if err != nil {
		return nil, err
	}
	h, err := assemble(db, dir, token, c)
	if err != nil {
		db.Close()
		return nil, err
	}
	h.db = db
	return h, nil
}
func assemble(store authorization.Store, dir, token string, c *clock) (*harness, error) {
	a, err := authorization.New(store, c, config())
	if err != nil {
		return nil, err
	}
	s, err := tasks.New(a, taskConfig())
	if err != nil {
		return nil, err
	}
	return &harness{auth: a, service: s, client: sdk.NewTaskClient(tasklocal.Bind(s, token), "local"), clock: c, token: token, dir: dir}, nil
}
func setup(ctx context.Context, dir string) (*harness, error) {
	h, err := open(dir, "", &clock{now: time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)})
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			h.db.Close()
		}
	}()
	token, err := h.auth.Bootstrap(ctx, "local", "operator")
	if err != nil {
		return nil, err
	}
	h.token = token
	h.client = sdk.NewTaskClient(tasklocal.Bind(h.service, token), "local")
	if err = h.policy(ctx, true); err != nil {
		return nil, err
	}
	now, _ := h.clock.Now()
	if err = h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "tasks", Subject: "operator", Mode: "continuous", Scope: scope(now, true)}}}); err != nil {
		return nil, err
	}
	ok = true
	return h, nil
}
func scope(now time.Time, execute bool) *wire.AuthorizationScope {
	actions := []string{"task.submit", "task.read"}
	if execute {
		actions = append(actions, "task.execute")
	}
	return &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: actions, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: now.Add(time.Hour).Unix()}
}
func (h *harness) mutate(ctx context.Context, c *wire.AuthorizationCommand) error {
	id, err := h.auth.NewOperation(ctx, h.token)
	if err != nil {
		return err
	}
	c.ExpectedRevision = h.revision
	_, err = h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: id, Command: c})
	if err == nil {
		h.revision++
	}
	return err
}
func (h *harness) policy(ctx context.Context, execute bool) error {
	now, _ := h.clock.Now()
	return h.mutate(ctx, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "tasks", Scope: scope(now, execute)}}}}})
}
func (h *harness) submit(ctx context.Context, steps uint32) (tasks.RunSnapshot, error) {
	id, err := h.auth.NewOperation(ctx, h.token)
	if err != nil {
		return tasks.RunSnapshot{}, err
	}
	now, _ := h.clock.Now()
	t, err := h.client.Submit(ctx, tasks.Submission{Namespace: "local", OperationID: id, Goal: "return the scripted result", Constraints: tasks.Constraints{MaxSteps: steps, DeadlineUnix: now.Add(time.Minute).Unix()}})
	if err != nil {
		return tasks.RunSnapshot{}, err
	}
	return h.service.Load(ctx, t.Ref)
}
func (h *harness) port(id string) (*tasks.WorkPort, error) {
	return h.service.BindWorker(tasks.WorkerBinding{Token: h.token, Subject: "operator", WorkerID: id}, limits())
}
func proposal(s tasks.RunSnapshot) tasks.Proposal {
	return tasks.Proposal{Kind: "result", BaseVersion: s.Task.Version, Complete: true, Result: "scripted answer"}
}
func mutation(id, kind string, s tasks.RunSnapshot) tasks.WorkChange {
	return tasks.WorkChange{ChangeID: id, Kind: kind, Qualification: tasks.QualificationOf(s)}
}
func complete(ctx context.Context, p *tasks.WorkPort, id string, s tasks.RunSnapshot) (tasks.RunSnapshot, error) {
	c := mutation(id, "complete", s)
	c.Proposal = proposal(s)
	return p.Commit(ctx, c)
}
func expect(err error, code authorization.Code) error {
	if !authorization.Is(err, code) {
		return fmt.Errorf("want %s; observed %v", code, err)
	}
	return nil
}
func require(ok bool, message string) error {
	if !ok {
		return fmt.Errorf("%s", message)
	}
	return nil
}
func script(_ context.Context, in tasks.DecisionInput) (tasks.Proposal, error) {
	return tasks.Proposal{Kind: "result", BaseVersion: in.Task.Version, Complete: true, Result: "scripted answer"}, nil
}
