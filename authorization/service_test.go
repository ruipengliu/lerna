package authorization_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
)

func config() authorization.Config {
	return authorization.Config{CredentialTTL: 24 * time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: 2 * time.Minute, MaxRules: 32, MaxResources: 128, MaxDepth: 16, MaxWork: 4096, EvaluationTimeout: time.Second}
}
func TestBootstrapCannotReplaceTrustAndSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	store, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	service, err := authorization.New(store, authorization.SystemClock{}, config())
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Bootstrap(ctx, "other", "intruder"); err == nil {
		t.Fatal("bootstrap replaced existing trust")
	}
	identity, err := service.Authenticate(ctx, token)
	if err != nil || identity.Subject != "admin" || identity.Namespace != "local" {
		t.Fatalf("identity: %+v %v", identity, err)
	}
	if _, err := service.Authenticate(ctx, "admin"); err == nil {
		t.Fatal("display name authenticated")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err = authorization.New(store, authorization.SystemClock{}, config())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Authenticate(ctx, token); err != nil {
		t.Fatalf("lost trusted identity after reopen: %v", err)
	}
}

func TestManagementCommitIsIdempotentBeforeVersionCheck(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := authorization.New(store, authorization.SystemClock{}, config())
	if err != nil {
		t.Fatal(err)
	}
	token, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	op, err := service.NewOperation(ctx, token)
	if err != nil {
		t.Fatal(err)
	}
	command := &wire.AuthorizationCommand{ExpectedRevision: 0, Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "doc", Parent: "root"}}}
	mutation := authorization.Mutation{Namespace: "local", OperationID: op, Command: command}
	first, err := service.Execute(ctx, token, mutation)
	if err != nil {
		t.Fatal(err)
	}
	repeat, err := service.Execute(ctx, token, mutation)
	if err != nil || !proto.Equal(first, repeat) {
		t.Fatalf("retry lost original receipt: %v %v", repeat, err)
	}
	command.GetRegisterResource().Parent = "doc"
	if _, err := service.Execute(ctx, token, mutation); !authorization.Is(err, authorization.IdentityConflict) {
		t.Fatalf("changed intent: %v", err)
	}
	if _, err := service.LookupOperation(ctx, "admin", op); !authorization.Is(err, authorization.Unauthenticated) {
		t.Fatalf("unauthenticated lookup: %v", err)
	}
}

type testClock struct{ at time.Time }

func (c *testClock) Now() (time.Time, error) { return c.at, nil }
func TestContinuousGrantAndHardDeny(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := &testClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	service, err := authorization.New(store, clock, config())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	apply := func(change *wire.AuthorizationCommand) {
		t.Helper()
		view, err := service.GetPolicy(ctx, admin)
		if err != nil {
			t.Fatal(err)
		}
		change.ExpectedRevision = view.Revision
		op, err := service.NewOperation(ctx, admin)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: op, Command: change}); err != nil {
			t.Fatal(err)
		}
	}
	user, err := authorization.NewCredential()
	if err != nil {
		t.Fatal(err)
	}
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "user", CredentialSha256: authorization.CredentialDigest(user), ExpiresUnix: clock.at.Add(time.Hour).Unix()}}})
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "doc", Parent: "root"}}})
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Subtree{Subtree: "root"}}, Actions: []string{"read"}, Purposes: []string{"answer"}, Locations: []string{"local"}, ExpiresUnix: clock.at.Add(30 * time.Minute).Unix()}
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: scope}}}}})
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "g1", Subject: "user", Scope: scope, Mode: "continuous"}}})
	action := &wire.AuthorizationAction{Resource: "doc", Action: "read", Purpose: "answer", Location: "local"}
	for i := 0; i < 2; i++ {
		decision, err := service.Evaluate(ctx, user, action)
		if err != nil || !decision.Allowed {
			t.Fatalf("continuous grant: %+v %v", decision, err)
		}
	}
	op, err := service.NewOperation(ctx, user)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, user, authorization.Mutation{Namespace: "local", OperationID: op, Command: &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{}}}}); !authorization.Is(err, authorization.Denied) {
		t.Fatalf("execution grant became administration: %v", err)
	}
	apply(&wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: scope}, {Id: "deny", Scope: scope, Deny: true}}}}})
	decision, err := service.Evaluate(ctx, user, action)
	if err != nil || decision.Allowed {
		t.Fatalf("deny bypass: %+v %v", decision, err)
	}
	clock.at = clock.at.Add(31 * time.Minute)
	decision, err = service.Evaluate(ctx, user, action)
	if err != nil || decision.Allowed {
		t.Fatalf("expired grant allowed: %+v %v", decision, err)
	}
}

func TestClosedWindowCannotReviveAfterCleanup(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	store, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	clock := &testClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	service, err := authorization.New(store, clock, config())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	used, err := service.NewOperation(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	unused, err := service.NewOperation(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "doc", Parent: "root"}}}
	receipt, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: used, Command: cmd})
	if err != nil {
		t.Fatal(err)
	}
	closeID, err := service.NewOperation(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: closeID, Command: &wire.AuthorizationCommand{ExpectedRevision: receipt.Revision, Change: &wire.AuthorizationCommand_CloseWindows{CloseWindows: true}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: unused, Command: cmd}); !authorization.Is(err, authorization.Expired) {
		t.Fatalf("closed unaccepted operation: %v", err)
	}
	if _, err := service.LookupOperation(ctx, admin, used); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	clock.at = clock.at.Add(3 * time.Minute)
	store, err = sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err = authorization.New(store, clock, config())
	if err != nil {
		t.Fatal(err)
	}
	view, err := service.GetPolicy(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	cleanup, err := service.NewOperation(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: cleanup, Command: &wire.AuthorizationCommand{ExpectedRevision: view.Revision, Change: &wire.AuthorizationCommand_CleanRecords{CleanRecords: true}}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.LookupOperation(ctx, admin, used); !authorization.Is(err, authorization.Expired) {
		t.Fatalf("cleaned record should report expired: %v", err)
	}
	if _, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: used, Command: cmd}); !authorization.Is(err, authorization.Expired) {
		t.Fatalf("cleaned operation revived: %v", err)
	}
}

func TestConcurrentMutationAndBootstrap(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	a, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	b, err := sqliteauth.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	sa, err := authorization.New(a, authorization.SystemClock{}, config())
	if err != nil {
		t.Fatal(err)
	}
	sb, err := authorization.New(b, authorization.SystemClock{}, config())
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		token string
		err   error
	}
	results := make(chan result, 2)
	for _, s := range []*authorization.Service{sa, sb} {
		go func() { token, err := s.Bootstrap(ctx, "local", "admin"); results <- result{token, err} }()
	}
	var token string
	success := 0
	for i := 0; i < 2; i++ {
		r := <-results
		if r.err == nil {
			success++
			token = r.token
		} else if !authorization.Is(r.err, authorization.Conflict) {
			t.Fatal(r.err)
		}
	}
	if success != 1 {
		t.Fatalf("%d bootstrap winners", success)
	}
	ids := make([]string, 2)
	for i := range ids {
		ids[i], err = sa.NewOperation(ctx, token)
		if err != nil {
			t.Fatal(err)
		}
	}
	errors := make(chan error, 2)
	for i, s := range []*authorization.Service{sa, sb} {
		go func() {
			_, err := s.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: ids[i], Command: &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: fmt.Sprintf("r%d", i), Parent: "root"}}}})
			errors <- err
		}()
	}
	success = 0
	for i := 0; i < 2; i++ {
		err := <-errors
		if err == nil {
			success++
		} else if !authorization.Is(err, authorization.Conflict) {
			t.Fatal(err)
		}
	}
	if success != 1 {
		t.Fatalf("%d updates accepted same revision", success)
	}
}

func TestEquivalentPolicySetOrderKeepsOperationIdentity(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := &testClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	service, err := authorization.New(store, clock, config())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	op, err := service.NewOperation(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"read", "write"}, Purposes: []string{"answer"}, Locations: []string{"local"}, ExpiresUnix: clock.at.Add(time.Hour).Unix()}
	cmd := &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "p", Scope: scope}}}}}
	first, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: op, Command: cmd})
	if err != nil {
		t.Fatal(err)
	}
	scope.Actions = []string{"write", "read"}
	second, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: op, Command: cmd})
	if err != nil || !proto.Equal(first, second) {
		t.Fatalf("equivalent policy changed identity: %v", err)
	}
}

func TestOperationIdentityRejectsAlternateEncoding(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	service, err := authorization.New(store, authorization.SystemClock{}, config())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	id, err := service.NewOperation(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	command := &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: "one", Parent: "root"}}}
	receipt, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: id, Command: command})
	if err != nil {
		t.Fatal(err)
	}
	alternate := strings.Replace(id, ".", "\n.", 1)
	command.ExpectedRevision = receipt.Revision
	command.GetRegisterResource().Id = "two"
	if _, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: alternate, Command: command}); err == nil {
		t.Fatal("alternate text of original identity accepted as a new operation")
	}
}

func TestFiniteResourceSetCannotIssueFutureSubtree(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := &testClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	service, err := authorization.New(store, clock, config())
	if err != nil {
		t.Fatal(err)
	}
	admin, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	issuer, err := authorization.NewCredential()
	if err != nil {
		t.Fatal(err)
	}
	apply := func(token string, cmd *wire.AuthorizationCommand) error {
		view, err := service.GetPolicy(ctx, admin)
		if err != nil {
			return err
		}
		cmd.ExpectedRevision = view.Revision
		id, err := service.NewOperation(ctx, token)
		if err != nil {
			return err
		}
		_, err = service.Execute(ctx, token, authorization.Mutation{Namespace: "local", OperationID: id, Command: cmd})
		return err
	}
	if err = apply(admin, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_RegisterPrincipal{RegisterPrincipal: &wire.RegisterPrincipal{Subject: "issuer", CredentialSha256: authorization.CredentialDigest(issuer), ExpiresUnix: clock.at.Add(time.Hour).Unix()}}}); err != nil {
		t.Fatal(err)
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Subtree{Subtree: "root"}}, Actions: []string{"read"}, Purposes: []string{"answer"}, Locations: []string{"local"}, ExpiresUnix: clock.at.Add(time.Hour).Unix()}
	if err = apply(admin, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: []*wire.PolicyRule{{Id: "allow", Scope: proto.Clone(scope).(*wire.AuthorizationScope)}}}}}); err != nil {
		t.Fatal(err)
	}
	scope.Resources = &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}
	if err = apply(admin, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "issuer-right", Subject: "issuer", Scope: proto.Clone(scope).(*wire.AuthorizationScope), MayIssue: true, Mode: "continuous"}}}); err != nil {
		t.Fatal(err)
	}
	scope.Resources = &wire.ResourceSelector{Selection: &wire.ResourceSelector_Subtree{Subtree: "root"}}
	err = apply(issuer, &wire.AuthorizationCommand{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "future", Subject: "issuer", Scope: scope, Mode: "continuous"}}})
	if !authorization.Is(err, authorization.Denied) {
		t.Fatalf("finite selector widened to an open subtree: %v", err)
	}
}

func TestFullOperationHistoryCanBeCleanedThroughPublicAPI(t *testing.T) {
	ctx := context.Background()
	store, err := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	clock := &testClock{time.Date(2026, 9, 10, 0, 0, 0, 0, time.UTC)}
	cfg := config()
	cfg.MaxWork = 2
	service, err := authorization.New(store, clock, cfg)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := service.Bootstrap(ctx, "local", "admin")
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		id, err := service.NewOperation(ctx, admin)
		if err != nil {
			t.Fatal(err)
		}
		_, err = service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: id, Command: &wire.AuthorizationCommand{ExpectedRevision: uint64(i), Change: &wire.AuthorizationCommand_RegisterResource{RegisterResource: &wire.RegisterResource{Id: fmt.Sprintf("r%d", i), Parent: "root"}}}})
		if err != nil {
			t.Fatal(err)
		}
	}
	clock.at = clock.at.Add(3 * time.Minute)
	cleanup, err := service.NewOperation(ctx, admin)
	if err != nil {
		t.Fatal(err)
	}
	receipt, err := service.Execute(ctx, admin, authorization.Mutation{Namespace: "local", OperationID: cleanup, Command: &wire.AuthorizationCommand{ExpectedRevision: 2, Change: &wire.AuthorizationCommand_CleanRecords{CleanRecords: true}}})
	if err != nil || receipt.Revision != 3 {
		t.Fatalf("full history cannot reclaim expired records: %+v %v", receipt, err)
	}
}
