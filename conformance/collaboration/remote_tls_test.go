package collaboration_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type agentFixture struct {
	ctx                     context.Context
	store                   runtime.Store
	scope                   runtime.Scope
	auth, serviceAuth, peer runtime.Auth
	registry                *runtime.Registry
	dispatch                *runtime.Dispatcher
	memory                  *memory.Service
	policy                  memory.Policy
	goalPolicy              memory.Policy
	task                    *task.Service
	taskPolicy              task.TaskPolicy
	answerSchemaRef         api.ComponentRef
	remote                  *collaboration.Remote
	keys                    *platform.Keyring
	server                  *httptest.Server
	client                  *harness.Client
	lostCreate              atomic.Bool
	creates                 atomic.Int64
	gate                    *agentFixtureGate
	afterRead               func(api.ContentRef)
	afterPublish            func(api.ContentRef)
}

// 此夹具只预批准准确的规则/主体边界；所有 SQL、密码学、网络、Content 与 Task 为实际实现。
type agentFixtureGate struct {
	memory        *memory.Service
	governance    *governance.Service
	user, service runtime.Auth
	rule          api.RuleDefinition
	remote        *collaboration.Remote
}

func (g *agentFixtureGate) CheckTaskCurrentTx(ctx context.Context, tx runtime.Tx, actual api.Task, requireRunning bool) error {
	if g.remote == nil {
		return api.E("dependency_unavailable", "remote_gate_unconfigured")
	}
	return g.remote.CheckTaskCurrentTx(ctx, tx, actual, requireRunning)
}

func (g *agentFixtureGate) CheckSubjectTx(_ context.Context, _ runtime.Tx, a runtime.Auth) error {
	if a.TenantID != g.user.TenantID || a.SubjectID != g.user.SubjectID && a.SubjectID != g.service.SubjectID || a.CredentialGeneration != 1 {
		return api.E("forbidden", "fixture_subject_not_paired")
	}
	return nil
}
func (g *agentFixtureGate) Authorize(ctx context.Context, tx runtime.Tx, a runtime.Auth, _ string, refs []api.ContentRef, _ []api.ObjectRef) error {
	if err := g.CheckSubjectTx(ctx, tx, a); err != nil {
		return err
	}
	for _, ref := range refs {
		if _, err := g.memory.CheckContentTx(ctx, tx, a, ref, "task.goal", "cloud", false); err != nil {
			return err
		}
	}
	return nil
}
func (g *agentFixtureGate) Evidence(ctx context.Context, tx runtime.Tx, t api.Task, refs []api.ObjectRef, _ []api.ComponentRef) error {
	checks := []governance.CheckReference{}
	for _, ref := range refs {
		checks = append(checks, governance.CheckReference{CheckRef: ref})
	}
	_, err := g.governance.CheckEvidenceTx(ctx, tx, governance.EvidenceCompletion{ConsumerTaskRef: tx.Scope().Ref(t.TaskID, t.Revision), Checks: checks, MaxStalenessSeconds: 300})
	return err
}
func (g *agentFixtureGate) RegisterCoverage(ctx context.Context, tx runtime.Tx, t api.Task, c api.GoalCoverage) error {
	_, err := g.governance.RegisterRuleTx(ctx, tx, governance.RuleDefinition{ComponentRef: c.RuleRef, Kind: "effect", Predicate: "current_state", AllowedBasis: []string{"verified"}, RiskClass: "ordinary", MaxObservationAgeSeconds: 600})
	if err != nil {
		return err
	}
	_, err = g.governance.RegisterCheckTx(ctx, tx, governance.ConditionCheck{CheckID: c.CoverageID, Revision: 1, AuthorityID: tx.Scope().OwnerID, TaskID: t.TaskID, GoalRevision: c.GoalRevision, RequirementID: api.NewID("requirement"), RequirementRevision: 1, ArtifactRef: c.GoalRef, ScopeRef: c.MappingReportRef, ObservedAt: c.CheckedAt, CheckedAt: c.CheckedAt, RuleRef: c.RuleRef, EvaluatorRef: c.EvaluatorRef, ConfigRef: c.RuleRef, InstallLockRef: c.RuleRef, Verdict: c.Verdict, Basis: "verified", ReportRef: c.MappingReportRef, EvidenceRefs: []api.ContentRef{c.MappingReportRef}, DependentCheckRefs: []api.ObjectRef{}})
	return err
}

type sourceAgentAuthority struct {
	user, peer runtime.Auth
	receiver   string
}

func (a sourceAgentAuthority) ResolveSourceSubjectTx(_ context.Context, _ runtime.Tx, p runtime.Auth, ref memory.ForeignReference, control bool) (runtime.Auth, error) {
	if !api.Equal(p, a.peer) || ref.HolderRef.OwnerID != a.receiver || ref.HolderRef.ObjectID != a.user.SubjectID || !control && ref.HolderRef.Revision != a.user.CredentialGeneration || control && ref.HolderRef.Revision > a.user.CredentialGeneration {
		return runtime.Auth{}, api.E("forbidden", "agent_source_holder_unpaired")
	}
	return a.user, nil
}
func newAgentBase(t *testing.T, tenant string, user runtime.Auth) *agentFixture {
	return newAgentBaseWithDriver(t, tenant, user, "sqlite")
}
func newAgentBaseWithDriver(t *testing.T, tenant string, user runtime.Auth, driver string) *agentFixture {
	t.Helper()
	ctx := context.Background()
	st, databaseID := openAgentContractStore(t, ctx, driver)
	scope := runtime.Scope{TenantID: tenant, OwnerID: api.NewID("owner"), DatabaseID: databaseID}
	service := runtime.Auth{TenantID: tenant, SubjectID: api.NewID("service"), CredentialGeneration: 1, Roles: []string{"service", "content_admin"}}
	peer := runtime.Auth{TenantID: tenant, SubjectID: scope.OwnerID, CredentialGeneration: 1, Roles: []string{"paired_agent"}}
	objects, err := objectstore.OpenLocal(t.TempDir(), memory.MaxContentBytes)
	if err != nil {
		t.Fatal(err)
	}
	m := memory.New(st, objects)
	m.Location = "cloud"
	if err = m.ConfigureParticipants("task", "collaboration", "governance"); err != nil {
		t.Fatal(err)
	}
	values := memory.PolicyValues{Subjects: []string{user.SubjectID, service.SubjectID}, Purposes: []string{"content.write", "task.goal"}, Locations: []string{"cloud"}, RetainUntil: api.Time(time.Now().Add(time.Hour)), Continuous: true}
	digest, _ := api.Digest(values)
	policy, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: digest}, values)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.InstallPolicy(ctx, scope, user, policy); err != nil {
		t.Fatal(err)
	}
	goalValues := values
	goalValues.Subjects = []string{user.SubjectID}
	goalDigest, _ := api.Digest(goalValues)
	goalPolicy, err := memory.NewPolicy(api.ComponentRef{ComponentID: api.NewID("policy"), Version: "1", Digest: goalDigest}, goalValues)
	if err != nil {
		t.Fatal(err)
	}
	if err = m.InstallPolicy(ctx, scope, user, goalPolicy); err != nil {
		t.Fatal(err)
	}
	keys, err := platform.NewDevelopmentKey(tenant, scope.OwnerID, []string{"agent_allocation", "agent_state", "foreign_content"})
	if err != nil {
		t.Fatal(err)
	}
	taskPolicy := task.TaskPolicy{PolicyRef: remoteComponent("policy"), ContinuationLimit: 100, RepairLimit: 3, NoProgressLimit: 5, ContextRoundLimit: 3, SafeAttemptLimit: 2, MaxRequirements: 100, MaxDelegations: 128, MaxDepth: 4, CostMode: "strict", BudgetLimits: []api.Amount{{Unit: "USD", Value: "100"}}, MaxEvidenceStalenessSeconds: 300, MaxDurationSeconds: 3600}
	ruleRef := remoteComponent("rule")
	age := uint64(600)
	rule := api.RuleDefinition{RuleRef: ruleRef, Kind: "effect", ParametersSchemaRef: ruleRef, Predicate: "current_state", AllowedBasis: []string{"verified"}, RequiredEvidenceSchemaRef: ruleRef, ScopeSchemaRef: ruleRef, MaxObservationAgeSeconds: &age, RiskClass: "ordinary", ApplicabilityPolicyRef: ruleRef}
	f := &agentFixture{ctx: ctx, store: st, scope: scope, auth: user, serviceAuth: service, peer: peer, registry: runtime.NewRegistry(), memory: m, policy: policy, goalPolicy: goalPolicy, keys: keys, taskPolicy: taskPolicy}
	f.gate = &agentFixtureGate{memory: m, governance: governance.New(st, governance.Options{}), user: user, service: service, rule: rule}
	f.dispatch = &runtime.Dispatcher{Store: st, Registry: f.registry, OwnerID: scope.OwnerID}
	return f
}
func (f *agentFixture) publish(t *testing.T, body string) api.ContentRef {
	t.Helper()
	b := []byte(body)
	ref := api.ContentRef{TenantID: f.scope.TenantID, OwnerID: f.scope.OwnerID, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(b), MediaType: "text/plain", ByteLength: uint64(len(b))}
	out, err := f.memory.Upload(f.ctx, f.scope, f.auth, memory.PublicationRequest{ContentRef: ref, TransferID: api.NewID("transfer"), ReserveCommandID: api.NewID("command"), PutCommandID: api.NewID("command"), PolicyRef: f.policy.PolicyRef, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(40 * time.Minute)), TransferDeadline: api.Time(time.Now().Add(time.Minute))}, b)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
func (f *agentFixture) command(method, target string, payload any) api.Command {
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: target, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(payload)}
}
func (f *agentFixture) readyParent(t *testing.T) api.Task {
	t.Helper()
	id := api.NewID("task")
	goal := f.publish(t, "explicit bounded parent goal; child's answer requires independent parent verification")
	receipt, err := f.dispatch.Command(f.ctx, f.auth, api.Raw(f.command("task.submit", id, task.SubmitInput{OrchestratorID: f.scope.OwnerID, GoalRef: goal, PolicyRef: f.taskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(30 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "20"}}})))
	if err != nil || receipt.Stage != "applied" {
		t.Fatalf("parent submit %+v %v", receipt, err)
	}
	current, err := f.task.Read(f.ctx, f.store, f.scope, f.auth, id)
	if err != nil {
		t.Fatal(err)
	}
	statement := f.publish(t, "independently verify delegated artifact")
	validation := f.publish(t, "finite preapproved rule maps entire explicit parent goal")
	candidate := api.RequirementCandidate{CandidateKey: "verified-delegated-artifact", Kind: "effect", StatementRef: statement, SourceRefs: []api.SourceEvidence{{ContentRef: goal, SourceKind: "user_input"}}, Origin: "explicit_user", RuleRef: f.gate.rule.RuleRef, Required: true, OpenQuestions: []string{}}
	_, err = f.task.AdoptRequirements(f.ctx, f.store, f.scope, f.serviceAuth, id, "command", f.scope.Ref(api.NewID("command"), 1), api.RequirementDelta{BaseGoalRevision: 1, Candidates: []api.RequirementCandidate{candidate}, ReasonRef: goal}, task.ValidationReport{Valid: true, ReportRef: validation, SemanticKeys: []string{"effect:required:verified-delegated-artifact"}})
	if err != nil {
		t.Fatal(err)
	}
	current, err = f.task.Read(f.ctx, f.store, f.scope, f.auth, id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.task.StoreCoverage(f.ctx, f.store, f.scope, f.serviceAuth, api.GoalCoverage{CoverageID: api.NewID("coverage"), Revision: 1, TaskRef: f.scope.Ref(id, current.Revision), GoalRevision: current.GoalRevision, GoalRef: goal, RequirementsDigest: current.RequirementsDigest, MappingReportRef: validation, RuleRef: f.gate.rule.RuleRef, EvaluatorRef: f.gate.rule.RuleRef, Verdict: "pass", Applicability: "usable", CheckedAt: api.Time(time.Now().UTC().Truncate(time.Millisecond))})
	if err != nil {
		t.Fatal(err)
	}
	current, err = f.task.Read(f.ctx, f.store, f.scope, f.auth, id)
	if err != nil {
		t.Fatal(err)
	}
	return current
}
func agentServer(t *testing.T, f, caller *agentFixture) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer finite-agent-pair" {
			w.WriteHeader(401)
			return
		}
		var frame struct {
			Kind    string          `json:"kind"`
			Payload json.RawMessage `json:"payload"`
		}
		b, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if err == nil {
			err = api.Decode(b, &frame)
		}
		var out any
		var c api.Command
		if err == nil {
			switch frame.Kind {
			case "command":
				err = api.Decode(frame.Payload, &c)
				if err == nil {
					out, err = f.dispatch.Command(r.Context(), caller.peer, frame.Payload)
				}
			case "query":
				out, err = f.dispatch.Query(r.Context(), caller.peer, frame.Payload)
			case "receipt_lookup":
				var q api.ReceiptLookup
				err = api.Decode(frame.Payload, &q)
				if err == nil {
					out, err = f.dispatch.Lookup(r.Context(), caller.peer, q.CommandID)
				}
			default:
				err = api.E("unsupported", "agent_transport_kind")
			}
		}
		if c.Method == "collaboration.create" {
			f.creates.Add(1)
			if err == nil && f.lostCreate.Swap(false) {
				conn, _, e := w.(http.Hijacker).Hijack()
				if e != nil {
					t.Error(e)
					return
				}
				_ = conn.Close()
				return
			}
		}
		kind := "response"
		if err != nil {
			kind = "error"
			var business *api.Error
			if errors.As(err, &business) {
				out = business
			} else {
				out = api.E("dependency_unavailable", "actual_agent_failed")
			}
		}
		_, _ = w.Write(api.Raw(struct {
			Kind    string          `json:"result_kind"`
			Payload json.RawMessage `json:"payload"`
		}{kind, api.Raw(out)}))
	}))
	t.Cleanup(server.Close)
	return server
}
func agentSDK(t *testing.T, server *httptest.Server, source *agentFixture) *harness.Client {
	t.Helper()
	methods := append(collaboration.RemoteAgentContracts(), providers.ForeignSourceContracts()...)
	digest, _ := api.DigestLimit(methods, 1<<20)
	d := harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: source.scope.OwnerID, SchemaDigest: api.CoreDigest(), Methods: methods, MethodsDigest: digest, IdentityScope: source.scope.TenantID + "/" + source.scope.OwnerID + "/" + source.scope.DatabaseID, Limits: harness.Limits{MaxDomainBytes: api.MaxJSONBytes, MaxFrameBytes: 1 << 20, MaxPending: 4096}}
	j, err := harness.OpenJournal(t.TempDir(), d.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := j.Close(); err != nil {
			t.Error(err)
		}
	})
	c, err := harness.NewClient(&harness.HTTPTransport{BaseURL: server.URL, Token: "finite-agent-pair", HTTP: server.Client()}, j, d)
	if err != nil {
		t.Fatal(err)
	}
	return c
}
func agentPublicKeys(f *agentFixture) *platform.Keyring {
	k := f.keys.Keys["development-es256"]
	k.Private = nil
	return &platform.Keyring{Keys: map[string]platform.RegisteredKey{"development-es256": k}}
}
func pairedAgents(t *testing.T) (*agentFixture, *agentFixture, collaboration.RemoteAgentProfile) {
	return pairedAgentsWithParentDriver(t, "sqlite")
}
func pairedAgentsWithParentDriver(t *testing.T, parentDriver string) (*agentFixture, *agentFixture, collaboration.RemoteAgentProfile) {
	t.Helper()
	tenant := api.NewID("tenant")
	user := runtime.Auth{TenantID: tenant, SubjectID: api.NewID("subject"), CredentialGeneration: 1, Roles: []string{"content_admin"}}
	parent := newAgentBaseWithDriver(t, tenant, user, parentDriver)
	child := newAgentBase(t, tenant, user)
	if parent.scope.OwnerID == child.scope.OwnerID || parent.scope.DatabaseID == child.scope.DatabaseID {
		t.Fatal("remote pair must preserve two original owners and databases")
	}
	t.Logf("actual parent driver=%s owner=%s database=%s child driver=sqlite owner=%s database=%s", parentDriver, parent.scope.OwnerID, parent.scope.DatabaseID, child.scope.OwnerID, child.scope.DatabaseID)
	p, err := collaboration.NewRemoteAgentProfile(api.NewID("agent"), "1.0.0", collaboration.RemoteAgentValues{ParentOwnerID: parent.scope.OwnerID, ReceiverID: child.scope.OwnerID, AgentBindingRef: parent.scope.Ref(api.NewID("binding"), 1), PolicyRef: child.taskPolicy.PolicyRef, InstallLockRef: remoteComponent("lock"), SubjectRefs: []api.ObjectRef{user.Ref(parent.scope.OwnerID)}, PermissionRefs: []api.ObjectRef{}, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, ResourceRefs: []api.ComponentRef{}, BudgetLimits: []api.Amount{{Unit: "USD", Value: "4"}}, MaxDepth: 4, MaxInputs: 16, Location: "cloud", MaterialPurposes: []string{"task.goal", "content.write"}})
	if err != nil {
		t.Fatal(err)
	}
	parent.server = agentServer(t, parent, child)
	child.server = agentServer(t, child, parent)
	parent.client = agentSDK(t, child.server, child)
	child.client = agentSDK(t, parent.server, parent)
	for _, pair := range [][2]*agentFixture{{parent, child}, {child, parent}} {
		f, other := pair[0], pair[1]
		f.remote, err = collaboration.NewRemote(collaboration.RemoteConfig{Store: f.store, Scope: f.scope, Registry: f.registry, Memory: f.memory, ProofPolicy: f.policy, Keys: f.keys, SigningKeyID: "development-es256", Auth: f.serviceAuth, Authority: &remoteAuthority{subject: user, peers: map[string]runtime.Auth{other.scope.OwnerID: other.peer}}, Profiles: []collaboration.RemoteAgentProfile{p}, Peers: []collaboration.RemotePeer{{Scope: other.scope, Client: f.client, Keys: agentPublicKeys(other)}}, Participants: []string{"governance"}})
		if err != nil {
			t.Fatal(err)
		}
		answerSchema := api.Schema{"type": "string", "maxLength": 4096}
		schemaDigest, _ := api.Digest(answerSchema)
		f.answerSchemaRef = api.ComponentRef{ComponentID: api.NewID("answer_schema"), Version: "1", Digest: schemaDigest}
		f.task, err = task.New(task.Config{AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: f.answerSchemaRef, Schema: answerSchema}}, Policies: []task.TaskPolicy{f.taskPolicy}, Rules: []api.RuleDefinition{f.gate.rule}, Participants: []string{"task", "collaboration", "content", "memory", "governance"}}, task.Ports{Content: agentContent{f, api.Time(time.Now().Add(9 * time.Minute))}, Gate: f.gate, Collaboration: f.remote, ClosureProof: f.remote})
		if err != nil {
			t.Fatal(err)
		}
		if err = f.remote.BindTask(f.task); err != nil {
			t.Fatal(err)
		}
		f.gate.remote = f.remote
		f.memory.Register(f.registry)
		if err = f.task.Register(f.registry); err != nil {
			t.Fatal(err)
		}
		if err = f.remote.Register(); err != nil {
			t.Fatal(err)
		}
		source, err := providers.NewForeignSource(providers.ForeignSourceConfig{Store: f.store, Scope: f.scope, Memory: f.memory, Keys: f.keys, SigningKeyID: "development-es256", Authority: sourceAgentAuthority{user: user, peer: other.peer, receiver: other.scope.OwnerID}})
		if err != nil {
			t.Fatal(err)
		}
		if err = source.Register(f.registry); err != nil {
			t.Fatal(err)
		}
		f.memory.Foreign = &providers.ForeignSourceClient{SDK: f.client, Keys: agentPublicKeys(other), SourceScope: other.scope, ConsumerScope: f.scope}
	}
	return parent, child, p
}
func stepRemoteCreate(t *testing.T, f *agentFixture) {
	t.Helper()
	works, status, err := f.store.Claim(f.ctx, f.scope, api.NewID("boot"), []string{collaboration.JobRemoteCreate}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("original child create work %v %v %d", status, err, len(works))
	}
	h, _ := f.registry.Job(collaboration.JobRemoteCreate)
	if err = h(f.ctx, f.store, f.scope, works[0]); err != nil {
		t.Fatal(err)
	}
}
func TestRemoteTLSLostCreateKeepsOriginalAllocationChildAndGoal(t *testing.T) {
	parent, child, profile := pairedAgents(t)
	if parent.scope.OwnerID == child.scope.OwnerID || parent.store.ID() == child.store.ID() {
		t.Fatal("not two authorities")
	}
	current := parent.readyParent(t)
	goalBody := "original delegated goal bytes; receiver must preserve source owner"
	goal := parent.publish(t, goalBody)
	id := api.NewID("delegation")
	in := task.DelegateInput{DelegationID: id, ParentTaskRef: parent.scope.Ref(current.TaskID, current.Revision), ParentGoalRevision: current.GoalRevision, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: profile.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "3"}}, Deadline: api.Time(time.Now().Add(10 * time.Minute)), PolicyRef: child.taskPolicy.PolicyRef, ReceiverID: child.scope.OwnerID}
	delegated, err := parent.task.Delegate(parent.ctx, parent.store, parent.scope, parent.auth, parent.command("task.delegate", id, in), in)
	if err != nil {
		t.Fatal(err)
	}
	var d task.Delegation
	var allocation task.Allocation
	_, err = parent.store.Within(parent.ctx, parent.scope, []string{"task", "content", "memory", "collaboration", "governance"}, func(tx runtime.Tx) error {
		var err error
		d, allocation, err = parent.task.CheckDelegationTx(parent.ctx, tx, parent.auth, id)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	child.lostCreate.Store(true)
	if _, err = parent.remote.Create(parent.ctx, parent.scope, d, allocation); err == nil {
		t.Fatal("actual lost create reply was accepted as known")
	}
	if child.creates.Load() != 1 {
		t.Fatalf("initial physical create calls %d", child.creates.Load())
	}
	pending, err := parent.remote.Create(parent.ctx, parent.scope, d, allocation)
	if err != nil || pending.ChildTaskRef != nil {
		t.Fatalf("recover original acceptance %+v %v", pending, err)
	}
	if child.creates.Load() != 1 {
		t.Fatal("receipt recovery made another physical create")
	}
	stepRemoteCreate(t, child)
	fact, err := parent.remote.Create(parent.ctx, parent.scope, d, allocation)
	if err != nil || fact.ChildTaskRef == nil {
		t.Fatalf("original child %+v %v", fact, err)
	}
	if fact.ChildTaskRef.OwnerID != child.scope.OwnerID || fact.GoalWorkClosed || fact.UsageFinal {
		t.Fatalf("child acceptance promoted to completion %+v", fact)
	}
	actual, err := child.task.Read(child.ctx, child.store, child.scope, child.auth, fact.ChildTaskRef.ObjectID)
	if err != nil {
		t.Fatal(err)
	}
	if !api.Equal(actual.GoalRef, goal) {
		t.Fatal("foreign goal owner/version replaced")
	}
	bytes, err := child.memory.ReadBytes(child.ctx, child.scope, child.auth, actual.GoalRef, "task.goal", "cloud")
	if err != nil || string(bytes) != goalBody {
		t.Fatalf("exact original foreign goal %v", err)
	}
	incoming, err := child.task.IncomingRead(child.ctx, child.store, child.scope, parent.peer, delegated.AllocationRef)
	if err != nil || incoming.TaskRef == nil || incoming.TaskRef.ObjectID != actual.TaskID || incoming.ParentOwner != parent.scope.OwnerID || !api.Equal(incoming.Limits, in.Budget) {
		t.Fatalf("original allocation %+v %v", incoming, err)
	}
	again, err := parent.remote.Create(parent.ctx, parent.scope, d, allocation)
	if err != nil || again.ChildTaskRef == nil || again.ChildTaskRef.ObjectID != actual.TaskID || child.creates.Load() != 1 {
		t.Fatalf("original key changed %+v %v", again, err)
	}
	all, err := child.store.List(child.ctx, child.scope, "task.tasks", child.auth.SubjectID, "", 10)
	if err != nil || len(all) != 1 {
		t.Fatalf("duplicate child tasks %d %v", len(all), err)
	}
	parentCurrent, err := parent.task.Read(parent.ctx, parent.store, parent.scope, parent.auth, current.TaskID)
	if err != nil || parentCurrent.Status != "active" || parentCurrent.ResultRef != nil || parentCurrent.Budget[0].Reserved != "3" {
		t.Fatalf("child fact bypassed parent validation %+v %v", parentCurrent, err)
	}
}

func (g *agentFixtureGate) RegisterCheck(ctx context.Context, tx runtime.Tx, t api.Task, c api.ConditionResult) error {
	_, err := g.governance.RegisterRuleTx(ctx, tx, governance.RuleDefinition{ComponentRef: c.RuleRef, Kind: g.rule.Kind, Predicate: g.rule.Predicate, AllowedBasis: g.rule.AllowedBasis, RiskClass: g.rule.RiskClass, MaxObservationAgeSeconds: 600})
	if err != nil {
		return err
	}
	_, err = g.governance.RegisterCheckTx(ctx, tx, governance.ConditionCheck{CheckID: c.CheckID, Revision: 1, AuthorityID: tx.Scope().OwnerID, TaskID: t.TaskID, GoalRevision: c.GoalRevision, RequirementID: c.RequirementID, RequirementRevision: c.RequirementRevision, ArtifactRef: c.ArtifactRef, ScopeRef: c.ScopeRef, ObservedAt: c.ObservedAt, CheckedAt: c.CheckedAt, RuleRef: c.RuleRef, EvaluatorRef: c.EvaluatorRef, ConfigRef: c.RuleRef, InstallLockRef: c.RuleRef, Verdict: c.Verdict, Basis: c.Basis, ReportRef: c.EvidenceRefs[0], EvidenceRefs: c.EvidenceRefs, DependentCheckRefs: []api.ObjectRef{}})
	return err
}
func (g *agentFixtureGate) BindResult(ctx context.Context, tx runtime.Tx, t api.Task, result api.Result, refs []api.ObjectRef) error {
	checks := []governance.CheckReference{}
	for _, ref := range refs {
		checks = append(checks, governance.CheckReference{CheckRef: ref})
	}
	ref := tx.Scope().Ref(result.ResultID, 1)
	_, err := g.governance.CheckEvidenceTx(ctx, tx, governance.EvidenceCompletion{ConsumerTaskRef: tx.Scope().Ref(t.TaskID, t.Revision), Checks: checks, MaxStalenessSeconds: 300, ResultRef: &ref})
	return err
}
