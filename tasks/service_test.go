package tasks_test

import (
	"context"
	"google.golang.org/protobuf/proto"
	sqliteauth "lerna/adapters/authorization/sqlite"
	tasklocal "lerna/adapters/tasks/local"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"lerna/sdk"
	"lerna/tasks"
	"path/filepath"
	"testing"
	"time"
)

type clock struct{ now time.Time }

func (c *clock) Now() (time.Time, error) { return c.now, nil }
func setup(t *testing.T) (*tasks.Service, *authorization.Service, string, *clock, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tasks.db")
	db, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	c := &clock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	a, err := authorization.New(db, c, authConfig())
	if err != nil {
		t.Fatal(err)
	}
	token, err := a.Bootstrap(context.Background(), "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	mutate(t, a, token, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "tasks", Scope: scope}}}}})
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 1, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "tasks", Subject: "admin", Scope: scope, Mode: "continuous"}}})
	service, err := tasks.New(a, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	return service, a, token, c, path
}
func authConfig() authorization.Config {
	return authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: 2 * time.Minute, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second}
}
func mutate(t *testing.T, a *authorization.Service, token string, cmd *wire.AuthorizationCommand) {
	t.Helper()
	id, err := a.NewOperation(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Execute(context.Background(), token, authorization.Mutation{Namespace: "local", OperationID: id, Command: cmd})
	if err != nil {
		t.Fatal(err)
	}
}
func TestSubmitSurvivesReopen(t *testing.T) {
	s, a, token, c, path := setup(t)
	ctx := context.Background()
	op, err := a.NewOperation(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	task, err := s.Submit(ctx, token, tasks.Submission{Namespace: "local", OperationID: op, Goal: "prepare an answer", Constraints: tasks.Constraints{MaxSteps: 3, DeadlineUnix: c.now.Add(time.Minute).Unix()}})
	if err != nil {
		t.Fatal(err)
	}
	if task.State != "QUEUED" || task.Version != 1 {
		t.Fatalf("unexpected initial task: %+v", task)
	}
	db, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	auth, err := authorization.New(db, c, authConfig())
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := tasks.New(auth, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	got, err := reopened.Get(ctx, token, task.Ref)
	if err != nil || got.Goal != "prepare an answer" {
		t.Fatalf("reopen: %+v %v", got, err)
	}
	snap, err := reopened.Load(ctx, task.Ref)
	if err != nil || len(snap.Work) != 1 || len(snap.Records) != 1 {
		t.Fatalf("atomic initial work: %+v %v", snap, err)
	}
}

func TestInternalCommitCanBeReconciledFromRecoveredSnapshot(t *testing.T) {
	s, _, _, c, _ := setup(t)
	ctx := context.Background()
	change := tasks.RunChange{ChangeID: "create-1", MustNotExist: true, Task: tasks.Task{Ref: tasks.Ref{Namespace: "local", TaskID: "internal-1"}, InputRefs: []string{}, Goal: "answer", Subject: "admin", Resource: "root", Owner: "local-owner", OwnerEpoch: 1, Version: 1, State: "QUEUED", Constraints: tasks.Constraints{MaxSteps: 1, DeadlineUnix: c.now.Add(time.Minute).Unix()}}, Work: []tasks.Work{{ID: "internal-1/initial", Kind: "decide"}}, Records: []tasks.Record{{Kind: "submitted", Version: 1}}}
	receipt, err := s.Commit(ctx, change)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.Load(ctx, change.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	got, err := s.LookupCommit(ctx, snap.Task.Ref, snap.LastCommit.ChangeID)
	if err != nil || got != receipt {
		t.Fatalf("reconcile: %+v %v", got, err)
	}
	again, err := s.Commit(ctx, change)
	if err != nil || again != receipt {
		t.Fatalf("replay: %+v %v", again, err)
	}
	change.Task.Goal = "different"
	if _, err = s.Commit(ctx, change); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("semantic conflict: %v", err)
	}
	change.ChangeID = "new"
	change.MustNotExist = false
	change.ExpectedVersion = 0
	if _, err = s.Commit(ctx, change); !authorization.Is(err, authorization.Conflict) {
		t.Fatalf("version conflict: %v", err)
	}
	change.ExpectedVersion = 1
	change.Task.State = "COMPLETED"
	if _, err = s.Commit(ctx, change); !authorization.Is(err, authorization.Unsupported) {
		t.Fatalf("arbitrary state: %v", err)
	}
}

func submission(t *testing.T, a *authorization.Service, token string, c *clock) tasks.Submission {
	t.Helper()
	id, err := a.NewOperation(context.Background(), token)
	if err != nil {
		t.Fatal(err)
	}
	return tasks.Submission{Namespace: "local", OperationID: id, Goal: "answer", Constraints: tasks.Constraints{MaxSteps: 2, DeadlineUnix: c.now.Add(time.Minute).Unix()}}
}
func TestRepeatedConcurrentAndConflictingAdmission(t *testing.T) {
	s, a, token, c, _ := setup(t)
	ctx := context.Background()
	in := submission(t, a, token, c)
	results := make(chan tasks.Task, 4)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() { out, err := s.Submit(ctx, token, in); results <- out; errs <- err }()
	}
	first := <-results
	for i := 0; i < 3; i++ {
		if other := <-results; other.Ref != first.Ref {
			t.Fatal("duplicate task")
		}
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
	page, err := s.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
	if err != nil || len(page.Runs) != 1 || len(page.Runs[0].Work) != 1 {
		t.Fatalf("duplicates: %+v %v", page, err)
	}
	in.Goal = "changed"
	if _, err = s.Submit(ctx, token, in); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, "admin", first.Ref); !authorization.Is(err, authorization.Unauthenticated) {
		t.Fatal(err)
	}
	if _, err = s.Get(ctx, token, tasks.Ref{Namespace: "other", TaskID: first.Ref.TaskID}); !authorization.Is(err, authorization.Denied) {
		t.Fatal(err)
	}
	// The same operation cannot become a management change.
	_, err = a.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: in.OperationID, Command: &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}}})
	if !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("cross-domain conflict: %v", err)
	}
	mgmt, err := a.NewOperation(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	_, err = a.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: mgmt, Command: &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "other", Parent: "root"}}}})
	if err != nil {
		t.Fatal(err)
	}
	in.OperationID = mgmt
	if _, err = s.Submit(ctx, token, in); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("reverse cross-domain conflict: %v", err)
	}
}
func TestClosedWindowAndManagementCleanupPreserveTask(t *testing.T) {
	s, a, token, c, _ := setup(t)
	ctx := context.Background()
	in := submission(t, a, token, c)
	unused := submission(t, a, token, c)
	first, err := s.Submit(ctx, token, in)
	if err != nil {
		t.Fatal(err)
	}
	c.now = c.now.Add(3 * time.Minute)
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_CleanRecords{CleanRecords: true}})
	if _, err = s.Submit(ctx, token, unused); !authorization.Is(err, authorization.Expired) {
		t.Fatalf("expired accepted: %v", err)
	}
	repeat, err := s.Submit(ctx, token, in)
	if err != nil || repeat.Ref != first.Ref {
		t.Fatalf("retained replay: %+v %v", repeat, err)
	}
	got, err := s.LookupOperation(ctx, token, "local", in.OperationID)
	if err != nil || got.Ref != first.Ref {
		t.Fatalf("lookup: %+v %v", got, err)
	}
	mutate(t, a, token, &wire.AuthorizationCommand{ExpectedRevision: 3, Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}})
	if _, err = s.Get(ctx, token, first.Ref); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("current policy ignored: %v", err)
	}
	if _, err = s.LookupOperation(ctx, token, "local", in.OperationID); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("receipt leaked: %v", err)
	}
}
func TestBoundedRecoveryPagesAndCapacity(t *testing.T) {
	_, a, token, c, _ := setup(t)
	s, err := tasks.New(a, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 3, MaxPage: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	seen := map[string]bool{}
	for i := 0; i < 3; i++ {
		if _, err := s.Submit(ctx, token, submission(t, a, token, c)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Submit(ctx, token, submission(t, a, token, c)); !authorization.Is(err, authorization.Unavailable) {
		t.Fatalf("capacity: %v", err)
	}
	after := ""
	for {
		page, err := s.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 2, After: after})
		if err != nil {
			t.Fatal(err)
		}
		for _, run := range page.Runs {
			if seen[run.Task.Ref.TaskID] {
				t.Fatal("duplicate page")
			}
			seen[run.Task.Ref.TaskID] = true
		}
		if page.Next == "" {
			break
		}
		after = page.Next
	}
	if len(seen) != 3 {
		t.Fatalf("lost task: %v", seen)
	}
	if _, err := s.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 3}); !authorization.Is(err, authorization.Invalid) {
		t.Fatalf("unbounded page: %v", err)
	}
	if _, err := s.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "other", Limit: 1}); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("cross-namespace page: %v", err)
	}
}

// This wrapper pauses one real storage commit after validation. Another handle
// commits the competing control change; release must force revalidation on CAS.
type pausedStore struct {
	authorization.Store
	ready, release chan struct{}
	once           bool
}

func (p *pausedStore) Commit(ctx context.Context, v uint64, st authorization.State) error {
	if len(st.RuntimeData) > 0 && !p.once {
		p.once = true
		close(p.ready)
		<-p.release
	}
	return p.Store.Commit(ctx, v, st)
}
func TestControlChangeWinsBeforeTaskCommit(t *testing.T) {
	for _, kind := range []string{"policy", "window", "principal"} {
		t.Run(kind, func(t *testing.T) {
			_, a, token, c, path := setup(t)
			ctx := context.Background()
			in := submission(t, a, token, c)
			db, err := sqliteauth.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			pause := &pausedStore{Store: db, ready: make(chan struct{}), release: make(chan struct{})}
			gated, err := authorization.New(pause, c, authConfig())
			if err != nil {
				t.Fatal(err)
			}
			s, err := tasks.New(gated, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 100, MaxPage: 10})
			if err != nil {
				t.Fatal(err)
			}
			result := make(chan error, 1)
			go func() { _, err := s.Submit(ctx, token, in); result <- err }()
			<-pause.ready
			cmd := &wire.AuthorizationCommand{ExpectedRevision: 2}
			code := authorization.Denied
			switch kind {
			case "policy":
				cmd.Change = &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}
			case "window":
				cmd.Change = &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}
				code = authorization.Expired
			case "principal":
				cmd.Change = &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "admin", CredentialSha256: authorization.CredentialDigest(token), ExpiresUnix: c.now.Add(time.Hour).Unix(), Disabled: true}}
				code = authorization.Unauthenticated
			}
			mutate(t, a, token, cmd)
			close(pause.release)
			if err := <-result; !authorization.Is(err, code) {
				t.Fatalf("stale qualification committed: %v", err)
			}
			page, err := s.ListRecoverable(ctx, tasks.RecoveryQuery{Namespace: "local", Limit: 10})
			if err != nil || len(page.Runs) != 0 {
				t.Fatalf("partial task: %+v %v", page, err)
			}
		})
	}
}

func TestSDKDurableAdmission(t *testing.T) {
	s, a, token, c, _ := setup(t)
	ctx := context.Background()
	client := sdk.NewTaskClient(tasklocal.Bind(s, token), "local")
	in := submission(t, a, token, c)
	result, err := client.Submit(ctx, in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := client.Get(ctx, tasks.Ref{Namespace: "local", TaskID: result.Ref.TaskID})
	if err != nil || got.Goal != in.Goal {
		t.Fatalf("SDK get: %+v %v", got, err)
	}
	again, err := client.LookupOperation(ctx, in.OperationID)
	if err != nil || again.Ref != result.Ref {
		t.Fatalf("SDK lookup: %+v %v", again, err)
	}
	raw, _ := proto.Marshal(&wire.TaskRequest{MessageId: "unknown", Namespace: "local", Body: &wire.TaskRequest_Submit{Submit: &wire.DurableSubmission{OperationId: in.OperationID, Goal: in.Goal, Constraints: &wire.TaskConstraints{MaxSteps: 2, DeadlineUnix: in.Constraints.DeadlineUnix}}}})
	// Unknown top-level required behavior cannot be silently ignored.
	raw = append(raw, 0xa0, 0x06, 0x01)
	data, err := tasklocal.Bind(s, token).Exchange(ctx, raw)
	if err != nil {
		t.Fatal(err)
	}
	resp := new(wire.TaskResponse)
	if err = proto.Unmarshal(data, resp); err != nil || resp.GetFailure().GetCode() != string(authorization.Unsupported) {
		t.Fatalf("unknown request: %v %v", resp, err)
	}
}

func TestTaskCreationRejectsInvalidInternalPayload(t *testing.T) {
	s, _, _, _, _ := setup(t)
	_, err := s.Commit(context.Background(), tasks.RunChange{ChangeID: "bad-create", MustNotExist: true, Task: tasks.Task{Ref: tasks.Ref{Namespace: "local", TaskID: "bad"}, State: "QUEUED", Version: 1, Owner: "local-owner", OwnerEpoch: 1, Resource: "root", Subject: "admin"}, Work: []tasks.Work{{ID: "bad/initial", Kind: "decide"}}, Records: []tasks.Record{{Kind: "submitted", Version: 1}}})
	if !authorization.Is(err, authorization.Invalid) {
		t.Fatalf("invalid internal task accepted: %v", err)
	}
}

func TestCurrentSubjectAndGrantBoundaries(t *testing.T) {
	s, a, admin, c, _ := setup(t)
	ctx := context.Background()
	in := submission(t, a, admin, c)
	task, err := s.Submit(ctx, admin, in)
	if err != nil {
		t.Fatal(err)
	}
	other, err := authorization.NewCredential()
	if err != nil {
		t.Fatal(err)
	}
	mutate(t, a, admin, &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "other", CredentialSha256: authorization.CredentialDigest(other), ExpiresUnix: c.now.Add(time.Hour).Unix()}}})
	// Authenticated identity without a grant is insufficient.
	if _, err = s.Submit(ctx, other, submission(t, a, other, c)); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("grant missing: %v", err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"task.submit", "task.read"}, Purposes: []string{"task"}, Locations: []string{"local"}, ExpiresUnix: c.now.Add(time.Hour).Unix()}
	mutate(t, a, admin, &wire.AuthorizationCommand{ExpectedRevision: 3, Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "other", Subject: "other", Scope: scope, Mode: "continuous"}}})
	if _, err = s.Get(ctx, other, task.Ref); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("cross-subject read: %v", err)
	}
	if _, err = s.LookupOperation(ctx, other, "local", in.OperationID); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("cross-subject receipt: %v", err)
	}
	if _, err = s.Submit(ctx, other, in); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("cross-subject replay: %v", err)
	}
	c.now = c.now.Add(time.Hour)
	if _, err = s.Get(ctx, admin, task.Ref); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("expired grant: %v", err)
	}
}

// An independent, temporary transaction implementation proves the public seam
// needs no concrete authorization.Service or private callback construction.
// This fixture is not a second durable backend or an authorization conformance claim.
type independentRuntime struct {
	data    []byte
	claimed bool
}
type independentTransaction struct{ independentRuntime }

func (m *independentRuntime) UpdateRuntime(ctx context.Context, fn func(authorization.RuntimeTransaction) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	tx := &independentTransaction{independentRuntime{data: append([]byte(nil), m.data...), claimed: m.claimed}}
	if err := fn(tx); err != nil {
		return err
	}
	*m = tx.independentRuntime
	return nil
}
func (m *independentTransaction) Data() []byte        { return m.data }
func (m *independentTransaction) SetData(data []byte) { m.data = append([]byte(nil), data...) }
func (m *independentTransaction) Namespace() string   { return "local" }
func (m *independentTransaction) Now() time.Time      { return time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC) }
func (m *independentTransaction) Authorize(token, resource, action string) (authorization.Identity, error) {
	if token != "fixture-credential" || resource != "root" || (action != "task.submit" && action != "task.read") {
		return authorization.Identity{}, &authorization.Error{Code: authorization.Denied}
	}
	return authorization.Identity{Subject: "fixture-subject", Namespace: "local"}, nil
}
func (m *independentTransaction) Operation(id, subject string, claim bool) error {
	if id != "fixture-operation" || subject != "fixture-subject" {
		return &authorization.Error{Code: authorization.Invalid}
	}
	m.claimed = m.claimed || claim
	return nil
}
func TestIndependentRuntimeImplementation(t *testing.T) {
	backend := &independentRuntime{}
	s, err := tasks.New(backend, tasks.Config{Namespace: "local", Resource: "root", Owner: "local-owner", MaxTasks: 2, MaxPage: 2})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	in := tasks.Submission{Namespace: "local", OperationID: "fixture-operation", Goal: "answer", Constraints: tasks.Constraints{MaxSteps: 1, DeadlineUnix: time.Date(2026, 9, 10, 0, 1, 0, 0, time.UTC).Unix()}}
	first, err := s.Submit(ctx, "fixture-credential", in)
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.Submit(ctx, "fixture-credential", in)
	if err != nil || second.Ref != first.Ref {
		t.Fatalf("replacement cannot preserve admission: %+v %v", second, err)
	}
	got, err := s.Get(ctx, "fixture-credential", first.Ref)
	if err != nil || got.Goal != "answer" || !backend.claimed {
		t.Fatalf("replacement access: %+v %v", got, err)
	}
}
