package sessions_test

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/egress"
	"github.com/ruipengliu/lerna/core/grants"
	"github.com/ruipengliu/lerna/core/sessions"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 仅暴露消费方声明的方法，不提升 SQLite 的其他能力。
type declaredSessionStore struct{ sessions.Store }
type declaredSessionDurable struct{ sessions.Durable }
type declaredGrantStore struct{ grants.Store }
type declaredGrantDecisions struct{ grants.Decisions }

var (
	_ sessions.Store              = (*sqlite.Store)(nil)
	_ sessions.Durable            = (*durable.Service)(nil)
	_ sessions.Tasks              = (*tasks.Service)(nil)
	_ sessions.Content            = (*content.Service)(nil)
	_ sessions.ConfirmationFacts  = (*tasks.Service)(nil)
	_ sessions.ConfirmationFacts  = (*grants.Service)(nil)
	_ grants.Store                = (*sqlite.Store)(nil)
	_ grants.Decisions            = (*durable.Service)(nil)
	_ grants.Admissions           = (*tasks.Service)(nil)
	_ grants.ConfirmationSessions = (*sessions.Service)(nil)
	_ grants.ConfirmationContent  = (*content.Service)(nil)
	_ grants.RevocationExits      = (*egress.Service)(nil)
)

// 规则：G3、G4、G12、R6、R7
func TestDeclaredSessionAdapterRetainsInputOrderAndOriginalQuestionAnswer(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	store, err := sqlite.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d := durable.New(store, "alice", "local")
	h.Tasks = tasks.New(store, "alice", "local").WithDecisions(d).WithCancellationJobs(d)
	h.Content, err = content.New(store, durable.New(store.ContentWork(), "alice", "local/content"), "alice", "local/content")
	if err != nil {
		t.Fatal(err)
	}
	h.Content.WithAssociations(h.Tasks).WithObservations(h.Ledger)
	h.Sessions, err = sessions.New(declaredSessionStore{store}, declaredSessionDurable{d}, h.Tasks, h.Content, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	h.Sessions.WithConfirmations(h.Tasks, h.Grants)
	if err = h.Sessions.ValidateDependencies(); err != nil {
		t.Fatal(err)
	}
	h.Tasks.WithAdmission(h.Grants, h.Budget, h.Content, h.Sessions, d, h.Ledger)
	id := session(t, h)
	task := goal(t, h, id)
	bind(t, h, task)
	task, err = h.Tasks.QueryTask(ctx, caller, task.TaskId)
	if err != nil {
		t.Fatal(err)
	}
	hdr := header("declared-question")
	hdr.Identity.IssuerId = "host"
	r, err := h.Sessions.PublishQuestion(ctx, &v1.Caller{UserId: "alice", IssuerId: "host"}, &v1.PublishQuestionCommand{Header: hdr, SessionId: id, TaskRef: &v1.Ref{Name: task.TaskId, Revision: task.Revision, SchemaId: "lerna.v1.Task"}, ContentRef: task.GoalRef, ExpiresAtUnixMs: 4102444800000, ChangesBasis: false})
	questionRef := accepted(t, r, err).ResultRef
	c := &v1.SubmitInputCommand{Header: header("declared-answer"), SessionId: id, TaskId: task.TaskId, InputKind: "ANSWER", ContentRef: stage(t, h, "declared-answer-body", "yes"), ExpectedInputVersion: 1, ExpectedRequirementsVersion: 1, RequestRef: questionRef, DependsOn: []*v1.CommandIdentity{header("goal").Identity}}
	r, err = h.Sessions.SubmitInput(ctx, caller, c)
	first := accepted(t, r, err)
	delivery, err := h.Sessions.QueryInput(ctx, caller, first.ResultRef)
	if err != nil || delivery.Input.SessionSeq != 2 || delivery.Input.TaskInputSeq != 2 || delivery.Input.RoutingStatus != "TASK_ACCEPTED" || !proto.Equal(delivery.Input.RequestRef, questionRef) || !proto.Equal(delivery.OriginalCommand, c) {
		t.Fatalf("original answer delivery: %v %v", delivery, err)
	}
	question, err := h.Sessions.QueryQuestion(ctx, caller, questionRef)
	if err != nil || question.Status != "ANSWERED" || !proto.Equal(question.ResponseInputRef, first.ResultRef) {
		t.Fatalf("original question response: %v %v", question, err)
	}
	r, err = h.Sessions.SubmitInput(ctx, caller, c)
	if err != nil || !proto.Equal(first, r) {
		t.Fatalf("original answer receipt: %v %v", r, err)
	}
	saved, err := h.Sessions.QuerySession(ctx, caller, id)
	if err != nil || saved.LastCommittedSeq != 2 || len(saved.Inputs) != 2 || len(saved.TaskRefs) != 1 {
		t.Fatalf("original session order: %v %v", saved, err)
	}
}

// 规则：G3、G4、G7、R6、R7
func TestDeclaredSessionAdapterConsumesOriginalGrantConfirmationOnce(t *testing.T) {
	path := filepath.Join(t.TempDir(), "confirmations.db")
	h, err := assembly.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = h.Close() })
	store, err := sqlite.Open(path, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d := durable.New(store, "alice", "local")
	h.Grants, err = grants.New(declaredGrantStore{store}, declaredGrantDecisions{d}, "alice", "local", "host")
	if err != nil {
		t.Fatal(err)
	}
	h.Grants.WithAdmissions(h.Tasks).WithConfirmationContent(h.Content).WithRevocationExits(h.Egress)
	h.Sessions, err = sessions.New(declaredSessionStore{store}, declaredSessionDurable{d}, h.Tasks, h.Content, "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	h.Sessions.WithConfirmations(h.Tasks, h.Grants)
	if err = h.Sessions.ValidateDependencies(); err != nil {
		t.Fatal(err)
	}
	h.Grants.WithConfirmations(h.Sessions)
	if err = h.Grants.ValidateDependencies(); err != nil {
		t.Fatal(err)
	}
	id := session(t, h)
	actor := &v1.Caller{UserId: "alice", IssuerId: "host"}
	hostHeader := func(id string) *v1.CommandHeader { h := header(id); h.Identity.IssuerId = "host"; return h }
	spec := &v1.Grant{Subject: &v1.GlobalName{UserId: "alice", AuthorityDomainId: "local", ObjectKind: "task", LocalId: "future-task"}, Permissions: []*v1.PermissionClause{{Action: "CREATE", Resource: "record-A", UseRight: "INVOKE", ProcessingPurpose: "CURRENT_TASK", ExecutorEndpointId: "local-api", ParameterMode: "ANY"}}, ValidFromUnixMs: time.Now().Add(-time.Minute).UnixMilli(), ValidUntilUnixMs: time.Now().Add(time.Hour).UnixMilli(), UseMode: "CONTINUOUS", MaxAdmissions: 3}
	r, err := h.Grants.RequestGrantConfirmation(ctx, actor, &v1.RequestGrantConfirmationCommand{Header: hostHeader("declared-grant-request"), Grant: spec, SessionId: id})
	pendingRef := accepted(t, r, err).ResultRef
	pending, err := h.Sessions.ReadConfirmation(ctx, actor, pendingRef)
	if err != nil || pending.MatterType != "GRANT_ISSUANCE" || pending.State != "PENDING" || pending.Description == "" {
		t.Fatalf("original grant confirmation: %v %v", pending, err)
	}
	response := &v1.RespondConfirmationCommand{Header: hostHeader("declared-approve"), ConfirmationRef: pending.Ref, BindingDigest: pending.BindingDigest, Decision: "APPROVE"}
	r, err = h.Sessions.RespondConfirmation(ctx, actor, response)
	approved := accepted(t, r, err)
	issue := &v1.IssueGrantCommand{Header: hostHeader("declared-issue"), ConfirmationRef: approved.ResultRef}
	r, err = h.Grants.IssueGrant(ctx, actor, issue)
	issued := accepted(t, r, err)
	consumed, err := h.Sessions.QueryCurrentConfirmation(ctx, actor, pending.Ref.Name)
	if err != nil || consumed.State != "CONSUMED" || consumed.GetConsumedGrantIssuanceRef() == nil || consumed.GetConsumedAdmissionRef() != nil {
		t.Fatalf("typed consumption: %v %v", consumed, err)
	}
	issuance, err := h.Grants.QueryGrantIssuance(ctx, actor, consumed.GetConsumedGrantIssuanceRef())
	if err != nil || issuance.State != "ISSUED" || !proto.Equal(issuance.GrantRef, issued.ResultRef) {
		t.Fatalf("original grant issuance: %v %v", issuance, err)
	}
	r, err = h.Sessions.RespondConfirmation(ctx, actor, response)
	if err != nil || !proto.Equal(r, approved) {
		t.Fatalf("original approval receipt: %v %v", r, err)
	}
	r, err = h.Grants.IssueGrant(ctx, actor, issue)
	if err != nil || !proto.Equal(r, issued) {
		t.Fatalf("original issuance receipt: %v %v", r, err)
	}
	issue.Header = hostHeader("declared-issue-again")
	r, err = h.Grants.IssueGrant(ctx, actor, issue)
	if err != nil || r.GetDecision() != v1.Decision_DECISION_REJECTED || r.GetError().GetCode() != "CONFIRMATION_INVALID" {
		t.Fatalf("reused confirmation: %v %v", r, err)
	}
	saved, err := h.Sessions.QuerySession(ctx, caller, id)
	if err != nil || saved.LastCommittedSeq != 1 || len(saved.Inputs) != 1 || len(saved.TaskRefs) != 0 || !proto.Equal(saved.Inputs[0].ConfirmationRef, approved.ResultRef) {
		t.Fatalf("confirmation input order: %v %v", saved, err)
	}
	confirmations, err := h.Sessions.QueryTaskConfirmations(ctx, actor, spec.Subject)
	if err != nil || len(confirmations) != 1 || confirmations[0].State != "CONSUMED" {
		t.Fatalf("confirmation snapshot: %v %v", confirmations, err)
	}
	grant, err := h.Grants.QueryGrant(ctx, actor, issued.ResultRef)
	if err != nil || grant.Status != "ACTIVE" {
		t.Fatalf("issued grant: %v %v", grant, err)
	}
	revoke := &v1.RevokeGrantCommand{Header: hostHeader("declared-revoke"), GrantId: grant.Ref.Name}
	r, err = h.Grants.Revoke(ctx, actor, revoke)
	revoked := accepted(t, r, err)
	progress, err := h.Grants.QueryRevocation(ctx, actor, revoked.ResultRef)
	if err != nil || progress.Status != "COMPLETE" || len(progress.Closures) != 0 {
		t.Fatalf("original revocation: %v %v", progress, err)
	}
	r, err = h.Grants.Revoke(ctx, actor, revoke)
	if err != nil || !proto.Equal(r, revoked) {
		t.Fatalf("original revocation receipt: %v %v", r, err)
	}
	historical, err := h.Grants.QueryGrant(ctx, actor, issued.ResultRef)
	if err != nil || !proto.Equal(historical, grant) {
		t.Fatalf("original grant history: %v %v", historical, err)
	}
}

// 规则：R6、G1
func TestGrantConstructorRejectsMissingBaseDependencies(t *testing.T) {
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "grant-dependencies.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	var nilDecisions *durable.Service
	for _, test := range []struct {
		name      string
		store     grants.Store
		decisions grants.Decisions
		want      string
	}{
		{name: "nil-decisions", store: store, want: "grants.decisions"},
		{name: "typed-nil-decisions", store: store, decisions: nilDecisions, want: "grants.decisions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := grants.New(test.store, test.decisions, "alice", "local", "host")
			if service != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("constructor: %v %v", service, err)
			}
		})
	}
}

// 规则：R6、G1
func TestGrantCompletionRequiresEveryConfirmationAndRevocationLink(t *testing.T) {
	h := open(t)
	var nilTasks *tasks.Service
	var nilSessions *sessions.Service
	var nilContent *content.Service
	var nilExits *egress.Service
	for _, test := range []struct {
		name          string
		admissions    grants.Admissions
		confirmations grants.ConfirmationSessions
		content       grants.ConfirmationContent
		exits         grants.RevocationExits
		want          string
	}{
		{name: "nil-admissions", confirmations: h.Sessions, content: h.Content, exits: h.Egress, want: "grants.admissions"},
		{name: "typed-nil-admissions", admissions: nilTasks, confirmations: h.Sessions, content: h.Content, exits: h.Egress, want: "grants.admissions"},
		{name: "nil-confirmations", admissions: h.Tasks, content: h.Content, exits: h.Egress, want: "grants.confirmations"},
		{name: "typed-nil-confirmations", admissions: h.Tasks, confirmations: nilSessions, content: h.Content, exits: h.Egress, want: "grants.confirmations"},
		{name: "nil-content", admissions: h.Tasks, confirmations: h.Sessions, exits: h.Egress, want: "grants.confirmationContent"},
		{name: "typed-nil-content", admissions: h.Tasks, confirmations: h.Sessions, content: nilContent, exits: h.Egress, want: "grants.confirmationContent"},
		{name: "nil-exits", admissions: h.Tasks, confirmations: h.Sessions, content: h.Content, want: "grants.revocationExits"},
		{name: "typed-nil-exits", admissions: h.Tasks, confirmations: h.Sessions, content: h.Content, exits: nilExits, want: "grants.revocationExits"},
		{name: "complete", admissions: h.Tasks, confirmations: h.Sessions, content: h.Content, exits: h.Egress},
	} {
		t.Run(test.name, func(t *testing.T) {
			h.Grants.WithAdmissions(test.admissions).WithConfirmations(test.confirmations).WithConfirmationContent(test.content).WithRevocationExits(test.exits)
			err := h.Grants.ValidateDependencies()
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("completion: %v", err)
			}
		})
	}
}

// 规则：R6、G1
func TestSessionConstructorRejectsMissingBaseDependencies(t *testing.T) {
	h := open(t)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "dependencies.db"), "alice", "local")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	d := durable.New(store, "alice", "local")
	var nilStore *sqlite.Store
	var nilDurable *durable.Service
	var nilTasks *tasks.Service
	var nilContent *content.Service
	for _, test := range []struct {
		name    string
		store   sessions.Store
		durable sessions.Durable
		tasks   sessions.Tasks
		content sessions.Content
		want    string
	}{
		{name: "nil-store", durable: d, tasks: h.Tasks, content: h.Content, want: "sessions.store"},
		{name: "typed-nil-store", store: nilStore, durable: d, tasks: h.Tasks, content: h.Content, want: "sessions.store"},
		{name: "nil-durable", store: store, tasks: h.Tasks, content: h.Content, want: "sessions.durable"},
		{name: "typed-nil-durable", store: store, durable: nilDurable, tasks: h.Tasks, content: h.Content, want: "sessions.durable"},
		{name: "nil-tasks", store: store, durable: d, content: h.Content, want: "sessions.tasks"},
		{name: "typed-nil-tasks", store: store, durable: d, tasks: nilTasks, content: h.Content, want: "sessions.tasks"},
		{name: "nil-content", store: store, durable: d, tasks: h.Tasks, want: "sessions.content"},
		{name: "typed-nil-content", store: store, durable: d, tasks: h.Tasks, content: nilContent, want: "sessions.content"},
	} {
		t.Run(test.name, func(t *testing.T) {
			service, err := sessions.New(test.store, test.durable, test.tasks, test.content, "alice", "local")
			if service != nil || err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("constructor: %v %v", service, err)
			}
		})
	}
}

// 规则：R6、G1
func TestSessionCompletionRequiresBothConfirmationFactOwners(t *testing.T) {
	h := open(t)
	var nilTasks *tasks.Service
	var nilGrants *grants.Service
	for _, test := range []struct {
		name               string
		operations, grants sessions.ConfirmationFacts
		want               string
	}{
		{name: "nil-operation-facts", grants: h.Grants, want: "sessions.operationFacts"},
		{name: "typed-nil-operation-facts", operations: nilTasks, grants: h.Grants, want: "sessions.operationFacts"},
		{name: "nil-grant-facts", operations: h.Tasks, want: "sessions.grantFacts"},
		{name: "typed-nil-grant-facts", operations: h.Tasks, grants: nilGrants, want: "sessions.grantFacts"},
		{name: "complete", operations: h.Tasks, grants: h.Grants},
	} {
		t.Run(test.name, func(t *testing.T) {
			h.Sessions.WithConfirmations(test.operations, test.grants)
			err := h.Sessions.ValidateDependencies()
			if test.want == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("completion: %v", err)
			}
		})
	}
}
