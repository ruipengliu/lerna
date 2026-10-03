package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

// 夹具仅替代远端 Content/Grant 边界；账本是实际 SQLite，目标是真实受控文件。
// 当前授权前提明确为已准入的受信 orchestrator，实际跨owner签名在装配层另验。
type executionContent struct {
	root          string
	tenant, owner string
	mu            sync.Mutex
	sources       map[string]api.ContentRef
}

func (c *executionContent) Publish(ctx context.Context, sc rt.Scope, a rt.Auth, p domain.Publication, b []byte) (api.ContentRef, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if sc.TenantID != c.tenant {
		return api.ContentRef{}, api.E("forbidden", "tenant_mismatch")
	}
	ref := api.ContentRef{TenantID: c.tenant, OwnerID: c.owner, ContentID: p.ContentID, Version: 1, Hash: api.Hash(b), MediaType: p.MediaType, ByteLength: uint64(len(b))}
	old, ok := c.sources[p.ContentID]
	if ok && !api.Equal(old, ref) {
		return api.ContentRef{}, api.E("idempotency_conflict", "content_changed")
	}
	if err := os.WriteFile(filepath.Join(c.root, p.ContentID), b, 0600); err != nil {
		return api.ContentRef{}, err
	}
	c.sources[p.ContentID] = ref
	return ref, nil
}
func (c *executionContent) ReadBytes(ctx context.Context, sc rt.Scope, a rt.Auth, ref api.ContentRef, purpose, location string) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if ref.TenantID != sc.TenantID || ref.TenantID != c.tenant {
		return nil, api.E("forbidden", "tenant_mismatch")
	}
	b, err := os.ReadFile(filepath.Join(c.root, ref.ContentID))
	if err != nil {
		return nil, err
	}
	if api.Hash(b) != ref.Hash || uint64(len(b)) != ref.ByteLength {
		return nil, api.E("invalid_request", "content_bytes_changed")
	}
	return b, nil
}

type executionAuthority struct{ denied bool }

func (a *executionAuthority) VerifyControl(ctx context.Context, tx rt.Tx, p rt.Auth, s api.ControlSnapshot) error {
	if !p.HasRole("orchestrator") {
		return api.E("forbidden", "untrusted_control")
	}
	return nil
}
func (a *executionAuthority) PrepareStart(ctx context.Context, sc rt.Scope, r domain.StartRequest) (domain.PreparedStart, error) {
	return domain.PreparedStart{OperationID: r.Invoke.OperationID, IntentHash: r.Invoke.IntentHash, Recipient: sc.OwnerID, UseRefs: r.Invoke.UseRefs, ApprovalRefs: []api.ObjectRef{}, AuthorityRevision: 1, StartBefore: r.Invoke.Deadline, ProofRef: r.ControlWindow.ProofRef}, nil
}
func (a *executionAuthority) VerifyStart(ctx context.Context, tx rt.Tx, r domain.StartRequest, p domain.PreparedStart) (domain.StartPermit, error) {
	if a.denied {
		return domain.StartPermit{}, api.E("forbidden", "authorization_changed")
	}
	return domain.StartPermit{StartBefore: p.StartBefore, ProofRefs: []api.ContentRef{p.ProofRef}}, nil
}

type executionFixture struct {
	st         *sqlite.Store
	sc         rt.Scope
	auth       rt.Auth
	registry   *rt.Registry
	dispatcher *rt.Dispatcher
	content    *executionContent
	authority  *executionAuthority
	files      *target.ManagedFiles
	root       string
	binding    api.ObjectRef
	install    api.ComponentRef
}

func newExecutionFixture(t *testing.T) *executionFixture {
	t.Helper()
	ctx := context.Background()
	root := t.TempDir()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "device.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	if err = st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	sc := rt.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("executor"), DatabaseID: st.ID()}
	auth := rt.Auth{TenantID: sc.TenantID, SubjectID: api.NewID("orchestrator"), CredentialGeneration: 1, Roles: []string{"orchestrator", "admin"}}
	content := &executionContent{root: t.TempDir(), tenant: sc.TenantID, owner: api.NewID("content_owner"), sources: map[string]api.ContentRef{}}
	authority := &executionAuthority{}
	files, err := target.NewManagedFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { files.Close() })
	service, err := domain.New(domain.Config{OwnerID: sc.OwnerID, Content: content, Authority: authority, Location: "device", Drivers: []domain.Driver{&target.FileDriver{Files: files, Content: content, Location: "device"}, &target.FileDriver{Files: files, Content: content, Location: "device", ReadOnly: true}}})
	if err != nil {
		t.Fatal(err)
	}
	registry := rt.NewRegistry()
	if err = service.Register(registry); err != nil {
		t.Fatal(err)
	}
	return &executionFixture{st: st, sc: sc, auth: auth, registry: registry, dispatcher: &rt.Dispatcher{Store: st, OwnerID: sc.OwnerID, Registry: registry}, content: content, authority: authority, files: files, root: root, binding: sc.Ref(api.NewID("binding"), 1), install: api.ComponentRef{ComponentID: api.NewID("component"), Version: "1", Digest: api.Hash([]byte("builtin"))}}
}
func (f *executionFixture) put(t *testing.T, b []byte) api.ContentRef {
	t.Helper()
	ref, err := f.content.Publish(context.Background(), f.sc, f.auth, domain.Publication{ContentID: api.NewID("content"), MediaType: "application/json"}, b)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}
func (f *executionFixture) invokeInput(t *testing.T, read bool) domain.InvokeInput {
	t.Helper()
	task := api.ObjectRef{TenantID: f.sc.TenantID, OwnerID: f.auth.SubjectID, ObjectID: api.NewID("task"), Revision: 1}
	id := api.NewID("operation")
	deadline := api.Time(time.Now().UTC().Add(time.Minute))
	proof := f.put(t, []byte(`{"proof":"trusted fixture"}`))
	cap := target.FileWriteCapability().Ref
	body := f.put(t, []byte("report target truth\n"))
	args := api.Raw(target.FileWriteArguments{Path: "report.md", ExpectedVersion: "absent", ContentRef: body})
	sources := []api.ContentRef{body}
	if read {
		cap = target.FileReadCapability().Ref
		args = api.Raw(target.FileReadArguments{Path: "report.md"})
		sources = []api.ContentRef{}
	}
	argRef := f.put(t, args)
	intent := domain.ExecutionIntent{OperationID: id, TaskRef: task, GoalRevision: 1, ControlRevision: 1, AdmissionSourceKind: "decision", AdmissionSourceRef: f.sc.Ref(api.NewID("decision"), 1), SourcePosition: "1", AdmissionPurpose: "goal_action", CapabilityRef: cap, BindingRef: f.binding, InstallLockRef: f.install, ArgumentsRef: argRef, ResourceRefs: []api.ObjectRef{}, RequirementRefs: []api.RequirementRef{}, CostBound: []api.Amount{}, ExecutorID: f.sc.OwnerID, Deadline: deadline, TaskDeadline: deadline, LogicalStepKey: "save-report", ProcessedSourceRefs: sources, DisclosedSourceRefs: []api.ContentRef{}}
	intentRef := f.put(t, api.Raw(intent))
	digest, _ := api.Digest(intent)
	return domain.InvokeInput{OperationID: id, TaskRef: task, GoalRevision: 1, ControlRevision: 1, CapabilityRef: cap, BindingRef: f.binding, IntentRef: intentRef, IntentHash: digest, UseRefs: []api.ObjectRef{f.sc.Ref(api.NewID("use"), 1)}, Deadline: deadline, ControlSnapshot: api.ControlSnapshot{OrchestratorID: task.OwnerID, TaskID: task.ObjectID, GoalRevision: 1, ControlRevision: 1, Status: "active", Control: "running", IssuedAt: api.Time(time.Now().UTC().Add(-time.Second)), StartBefore: deadline, WindowID: api.NewID("window"), ProofRef: proof}, ReservationRef: f.sc.Ref(api.NewID("reservation"), 1)}
}
func (f *executionFixture) command(t *testing.T, method, targetID string, p any, rev *uint64) api.Receipt {
	t.Helper()
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.sc.OwnerID, CommandID: api.NewID("command"), Method: method, TargetID: targetID, ExpiresAt: api.Time(time.Now().UTC().Add(time.Minute)), ExpectedRevision: rev, Payload: api.Raw(p)}
	r, err := f.dispatcher.Command(context.Background(), f.auth, api.Raw(c))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func (f *executionFixture) query(t *testing.T, method, targetID string, p any, out any) {
	t.Helper()
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.sc.OwnerID, QueryID: api.NewID("query"), Method: method, TargetID: targetID, Payload: api.Raw(p)}
	raw, err := f.dispatcher.Query(context.Background(), f.auth, api.Raw(q))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, out); err != nil {
		t.Fatal(err)
	}
}
func (f *executionFixture) drain(t *testing.T) {
	t.Helper()
	if err := rt.Drain(context.Background(), f.st, f.sc, f.registry, 100); err != nil {
		t.Fatal(err)
	}
}
func TestExecutionDurableFileEffectAndIndependentReadOperation(t *testing.T) {
	f := newExecutionFixture(t)
	write := f.invokeInput(t, false)
	r := f.command(t, "execution.invoke", write.OperationID, write, nil)
	if r.Stage != "applied" {
		t.Fatalf("invoke rejected: %+v", r)
	}
	f.drain(t)
	actual, err := os.ReadFile(filepath.Join(f.root, "report.md"))
	if err != nil || string(actual) != "report target truth\n" {
		t.Fatalf("target truth %q %v", actual, err)
	}
	var view domain.OperationView
	f.query(t, "execution.get", write.OperationID, domain.OperationIDInput{OperationID: write.OperationID}, &view)
	if view.Operation.Effect != "applied" || view.Operation.ExecutionState != "closed" || len(view.Attempts.Items) != 1 {
		t.Fatalf("write facts: %+v", view)
	}
	read := f.invokeInput(t, true)
	r = f.command(t, "execution.invoke", read.OperationID, read, nil)
	if r.Stage != "applied" {
		t.Fatalf("read rejected %+v", r)
	}
	f.drain(t)
	f.query(t, "execution.get", read.OperationID, domain.OperationIDInput{OperationID: read.OperationID}, &view)
	if view.Operation.ResultRef == nil {
		t.Fatal("independent read missing result")
	}
	b, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, *view.Operation.ResultRef, "verify", "device")
	if err != nil {
		t.Fatal(err)
	}
	var result target.FileReadResult
	if err = api.Decode(b, &result); err != nil || result.Version != api.Hash(actual) {
		t.Fatalf("readback result %+v %v", result, err)
	}
}

func TestExecutionCancellationBeforeInvokeSurvivesRestart(t *testing.T) {
	f := newExecutionFixture(t)
	input := f.invokeInput(t, false)
	cancel := domain.CancelInput{OperationID: input.OperationID, Reason: "owner cancelled", TaskRef: input.TaskRef, OrchestratorID: input.TaskRef.OwnerID}
	receipt := f.command(t, "execution.cancel", input.OperationID, cancel, nil)
	if receipt.Stage != "applied" {
		t.Fatalf("cancel %+v", receipt)
	}
	receipt = f.command(t, "execution.invoke", input.OperationID, input, nil)
	if receipt.Stage != "rejected" || receipt.Error.Reason != "operation_permanently_cancelled" {
		t.Fatalf("late invoke %+v", receipt)
	}
	f.drain(t)
	if _, err := os.Stat(filepath.Join(f.root, "report.md")); !os.IsNotExist(err) {
		t.Fatalf("cancelled action touched target %v", err)
	}
	var view domain.OperationView
	f.query(t, "execution.get", input.OperationID, domain.OperationIDInput{OperationID: input.OperationID}, &view)
	if view.Operation.Effect != "not_started" || !view.NewAttemptsClosed || !view.ActuallyStopped {
		t.Fatalf("cancel truth %+v", view)
	}
}

func TestExecutionLostWriteReceiptQueriesOriginalAndDoesNotResend(t *testing.T) {
	f := newExecutionFixture(t)
	input := f.invokeInput(t, false)
	f.files.Fault = func(stage string) error {
		if stage == "renamed" {
			return context.Canceled
		}
		return nil
	}
	receipt := f.command(t, "execution.invoke", input.OperationID, input, nil)
	if receipt.Stage != "applied" {
		t.Fatalf("invoke %+v", receipt)
	}
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", input.OperationID, domain.OperationIDInput{OperationID: input.OperationID}, &view)
	if view.Operation.Effect != "unknown" || len(view.Attempts.Items) != 1 {
		t.Fatalf("lost reply was guessed %+v", view)
	}
	f.files.Fault = nil
	f.command(t, "execution.reconcile", input.OperationID, domain.ReconcileInput{OperationID: input.OperationID}, nil)
	f.drain(t)
	f.query(t, "execution.get", input.OperationID, domain.OperationIDInput{OperationID: input.OperationID}, &view)
	if view.Operation.Effect != "applied" || !view.ActuallyStopped || len(view.Attempts.Items) != 1 {
		t.Fatalf("original query recovery %+v", view)
	}
	actual, err := os.ReadFile(filepath.Join(f.root, "report.md"))
	if err != nil || string(actual) != "report target truth\n" {
		t.Fatalf("target %+v %v", actual, err)
	}
}

func TestExecutionSameControlNewWindowAndOldControlRejected(t *testing.T) {
	f := newExecutionFixture(t)
	input := f.invokeInput(t, false)
	r := f.command(t, "execution.invoke", input.OperationID, input, nil)
	if r.Stage != "applied" {
		t.Fatalf("invoke %+v", r)
	}
	newer := input.ControlSnapshot
	newer.WindowID = api.NewID("window")
	newer.IssuedAt = api.Time(time.Now())
	newer.StartBefore = api.Time(time.Now().Add(2 * time.Minute))
	r = f.command(t, "execution.control", input.TaskRef.ObjectID, domain.ControlInput{TaskRef: input.TaskRef, Snapshot: newer}, nil)
	if r.Stage != "applied" {
		t.Fatalf("same control new window rejected %+v", r)
	}
	var controls domain.ControlView
	f.query(t, "execution.control.get", input.TaskRef.ObjectID, domain.ControlGetInput{TaskID: input.TaskRef.ObjectID}, &controls)
	if controls.Gate.ControlRevision != 1 || len(controls.Windows) != 2 {
		t.Fatalf("window changed control %+v", controls)
	}
	paused := newer
	paused.WindowID = api.NewID("window")
	paused.ControlRevision = 2
	paused.Control = "paused"
	r = f.command(t, "execution.control", input.TaskRef.ObjectID, domain.ControlInput{TaskRef: input.TaskRef, Snapshot: paused}, nil)
	if r.Stage != "applied" {
		t.Fatalf("pause rejected %+v", r)
	}
	r = f.command(t, "execution.control", input.TaskRef.ObjectID, domain.ControlInput{TaskRef: input.TaskRef, Snapshot: input.ControlSnapshot}, nil)
	if r.Stage != "rejected" || r.Error.Reason != "task_gate_stale" {
		t.Fatalf("old gate accepted %+v", r)
	}
	f.drain(t)
	if _, err := os.Stat(filepath.Join(f.root, "report.md")); !os.IsNotExist(err) {
		t.Fatalf("paused task started %v", err)
	}
}

func TestExecutionAuthorizationRevocationBeforePhysicalEntry(t *testing.T) {
	f := newExecutionFixture(t)
	input := f.invokeInput(t, false)
	r := f.command(t, "execution.invoke", input.OperationID, input, nil)
	if r.Stage != "applied" {
		t.Fatalf("invoke %+v", r)
	}
	f.authority.denied = true
	f.drain(t)
	if _, err := os.Stat(filepath.Join(f.root, "report.md")); !os.IsNotExist(err) {
		t.Fatalf("revoked action wrote %v", err)
	}
	var view domain.OperationView
	f.query(t, "execution.get", input.OperationID, domain.OperationIDInput{OperationID: input.OperationID}, &view)
	if view.Operation.Effect != "not_started" || !view.Operation.UsageFinal || !view.NewAttemptsClosed {
		t.Fatalf("revoked before entry %+v", view)
	}
}

func TestPassiveEnvironmentCheckpointRestoreDoesNotReplayTargetWrites(t *testing.T) {
	f := newExecutionFixture(t)
	id := api.NewID("environment")
	config := api.ComponentRef{ComponentID: domain.BuiltinComponentID("environment.passive"), Version: "1", Digest: api.Hash([]byte(domain.PassiveEnvironmentFormat))}
	r := f.command(t, "environment.create", id, domain.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: config, InstallLockRef: f.install, Limits: []api.Amount{{Unit: "namespace_bytes", Value: "65536"}}, ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("create %+v", r)
	}
	f.drain(t)
	var env domain.Environment
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	if env.Phase != "active" || env.Generation != 1 || env.NamespaceRevision != 1 {
		t.Fatalf("prepared %+v", env)
	}
	rev := env.Revision
	r = f.command(t, "environment.checkpoint", id, domain.EnvironmentCheckpointInput{EnvironmentID: id, ExpectedGeneration: 1, ExpectedNamespaceRevision: 1}, &rev)
	if r.Stage != "applied" {
		t.Fatalf("checkpoint %+v", r)
	}
	var cp domain.Checkpoint
	if err := api.Decode(r.Output, &cp); err != nil {
		t.Fatal(err)
	}
	r = f.command(t, "environment.stop", id, domain.EnvironmentStopInput{EnvironmentID: id, ExpectedGeneration: 1, Reason: "pause passive holder"}, &rev)
	if r.Stage != "applied" {
		t.Fatalf("stop %+v", r)
	}
	f.drain(t)
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	if env.Phase != "closed" || env.Generation != 2 || !env.ActuallyExited {
		t.Fatalf("closed %+v", env)
	}
	rev = env.Revision
	r = f.command(t, "environment.restore", id, domain.EnvironmentRestoreInput{EnvironmentID: id, ExpectedGeneration: 2, CheckpointRef: f.sc.Ref(cp.CheckpointID, 1), ConfigRef: config, InstallLockRef: f.install}, &rev)
	if r.Stage != "applied" {
		t.Fatalf("restore %+v", r)
	}
	f.drain(t)
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	if env.Generation != 3 || env.Phase != "active" || env.NamespaceRef.Hash != cp.ContentRef.Hash {
		t.Fatalf("restore changed passive content %+v", env)
	}
	if _, err := os.Stat(filepath.Join(f.root, "report.md")); !os.IsNotExist(err) {
		t.Fatalf("checkpoint replayed target write %v", err)
	}
}

func TestTrustedPureComputePublishesOneNamespaceCASAndRetainsHistoricalEffect(t *testing.T) {
	f := newExecutionFixture(t)
	compute := &domain.TrustedComputeDriver{Content: f.content, Store: f.st, Location: "device"}
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{compute}})
	if err != nil {
		t.Fatal(err)
	}
	registry := rt.NewRegistry()
	if err = service.Register(registry); err != nil {
		t.Fatal(err)
	}
	f.registry = registry
	f.dispatcher.Registry = registry
	id := api.NewID("environment")
	config := api.ComponentRef{ComponentID: domain.BuiltinComponentID("environment.passive"), Version: "1", Digest: api.Hash([]byte(domain.PassiveEnvironmentFormat))}
	r := f.command(t, "environment.create", id, domain.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: config, InstallLockRef: f.install, Limits: []api.Amount{{Unit: "namespace_bytes", Value: "65536"}}, ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("create %+v", r)
	}
	f.drain(t)
	var env domain.Environment
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	code := f.put(t, api.Raw(domain.ComputeProgram{Opcode: "add_decimal", TargetName: "total", InputNames: []string{"a", "b"}}))
	inputRef := f.put(t, api.Raw(domain.PassiveNamespace{Format: domain.PassiveEnvironmentFormat, Bindings: []domain.NamespaceBinding{{Name: "a", Kind: "decimal", Decimal: "0.1"}, {Name: "b", Kind: "decimal", Decimal: "0.2"}}}))
	invoke := f.invokeInput(t, false)
	args := domain.ComputeArguments{EnvironmentRef: f.sc.Ref(id, env.Revision), ExpectedGeneration: 1, ExpectedNamespaceRevision: 1, CodeRef: code, InputRef: inputRef, OutputSchemaRef: domain.NamespaceOutputSchemaRef()}
	argRef := f.put(t, api.Raw(args))
	var intent domain.ExecutionIntent
	raw, _ := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
	if err = api.Decode(raw, &intent); err != nil {
		t.Fatal(err)
	}
	invoke.CapabilityRef = domain.TrustedComputeCapability().Ref
	intent.CapabilityRef = invoke.CapabilityRef
	intent.ArgumentsRef = argRef
	intent.ProcessedSourceRefs = []api.ContentRef{code, inputRef, *env.NamespaceRef}
	invoke.IntentRef = f.put(t, api.Raw(intent))
	invoke.IntentHash, _ = api.Digest(intent)
	r = f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	if r.Stage != "applied" {
		t.Fatalf("cell invoke %+v", r)
	}
	f.drain(t)
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	if env.NamespaceRevision != 2 || !env.ReadyForCell || !env.ActuallyExited {
		t.Fatalf("cell did not commit %+v", env)
	}
	nsRaw, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, *env.NamespaceRef, "verify", "device")
	if err != nil {
		t.Fatal(err)
	}
	var ns domain.PassiveNamespace
	if err = api.Decode(nsRaw, &ns); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, v := range ns.Bindings {
		if v.Name == "total" {
			found = v.Kind == "decimal" && v.Decimal == "0.3"
		}
	}
	if !found {
		t.Fatalf("independent known decimal result missing %+v", ns)
	}
	// 原CAS不自动刷新；第二个原命令以旧namespace资格拒绝，并保留前次准确成果。
	f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	if view.Operation.Effect != "applied" || view.EffectDisputed {
		t.Fatalf("reconcile rewrote committed cell %+v", view)
	}
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	if env.NamespaceRevision != 2 {
		t.Fatalf("reconcile replayed cell %+v", env)
	}
}

func TestExecutorDeviceResourceObservationAndTakeoverSeparateSavedFromStopped(t *testing.T) {
	f := newExecutionFixture(t)
	ids := []string{api.NewID("resource"), api.NewID("resource"), api.NewID("resource")}
	phones, err := target.NewSimulatedPhones(t.TempDir(), ids)
	if err != nil {
		t.Fatal(err)
	}
	defer phones.Close()
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{phones}, ResourceDriver: phones})
	if err != nil {
		t.Fatal(err)
	}
	registry := rt.NewRegistry()
	if err = service.Register(registry); err != nil {
		t.Fatal(err)
	}
	f.registry = registry
	f.dispatcher.Registry = registry
	for index, id := range ids {
		holder := api.NewID("holder")
		instance := api.NewID("instance")
		r := f.command(t, "resource.acquire", id, domain.AcquireInput{ResourceID: id, HolderID: holder, InstanceID: instance, LeaseUntil: api.Time(time.Now().Add(time.Minute))}, nil)
		if r.Stage != "applied" {
			t.Fatalf("acquire %+v", r)
		}
		var lease domain.ResourceLease
		api.Decode(r.Output, &lease)
		if lease.ActuallyStopped {
			t.Fatal("saved acquisition falsely claims host has stopped old actions")
		}
		f.drain(t)
		observationID := api.NewID("observation")
		r = f.command(t, "resource.observe", id, domain.ObserveInput{ResourceID: id, ObservationID: observationID, HolderID: holder, InstanceID: instance, ControlEpoch: 1}, nil)
		if r.Stage != "accepted" {
			t.Fatalf("observe %+v", r)
		}
		f.drain(t)
		var observed domain.ObserveOutput
		f.query(t, "resource.observation.get", observationID, domain.ObservationIDInput{ObservationID: observationID}, &observed)
		if !observed.Ready || observed.Observation == nil {
			t.Fatalf("observation %+v", observed)
		}
		invoke := f.invokeInput(t, false)
		var intent domain.ExecutionIntent
		raw, _ := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
		api.Decode(raw, &intent)
		args := target.PhoneActionArguments{ResourceID: id, InstanceID: instance, ControlEpoch: 1, ObservationID: observationID, TargetVersion: observed.Observation.TargetVersion, ActionBefore: observed.Observation.ActionBefore, Action: "set_note", Value: "distinct controlled phone"}
		intent.ArgumentsRef = f.put(t, api.Raw(args))
		invoke.CapabilityRef = target.PhoneCapability().Ref
		intent.CapabilityRef = invoke.CapabilityRef
		intent.ResourceRefs = []api.ObjectRef{observed.Observation.ResourceRef}
		intent.ProcessedSourceRefs = []api.ContentRef{}
		invoke.IntentRef = f.put(t, api.Raw(intent))
		invoke.IntentHash, _ = api.Digest(intent)
		r = f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
		if r.Stage != "applied" {
			t.Fatalf("phone %d invoke %+v", index, r)
		}
		f.drain(t)
		afterID := api.NewID("observation")
		r = f.command(t, "resource.observe", id, domain.ObserveInput{ResourceID: id, ObservationID: afterID, HolderID: holder, InstanceID: instance, ControlEpoch: 1}, nil)
		if r.Stage != "accepted" {
			t.Fatalf("new observe %+v", r)
		}
		f.drain(t)
		f.query(t, "resource.observation.get", afterID, domain.ObservationIDInput{ObservationID: afterID}, &observed)
		var state target.PhoneState
		if err = api.Decode(observed.Observation.Data, &state); err != nil || state.Note != "distinct controlled phone" || state.Version != 2 {
			t.Fatalf("actual device %d %+v %v", index, state, err)
		}
		f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &lease)
		rev := lease.Revision
		r = f.command(t, "resource.takeover", id, domain.TakeoverInput{ResourceID: id, Reason: "device owner takes over"}, &rev)
		if r.Stage != "applied" {
			t.Fatalf("takeover %+v", r)
		}
		api.Decode(r.Output, &lease)
		if lease.ControlEpoch != 2 || lease.ActuallyStopped {
			t.Fatalf("saved takeover conflates stopped %+v", lease)
		}
		f.drain(t)
		f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &lease)
		if !lease.ActuallyStopped || lease.State != "released" {
			t.Fatalf("physical takeover residual %+v", lease)
		}
		r = f.command(t, "resource.acquire", id, domain.AcquireInput{ResourceID: id, HolderID: holder, InstanceID: api.NewID("instance"), ExpectedControlEpoch: 1, LeaseUntil: api.Time(time.Now().Add(time.Minute))}, nil)
		if r.Stage != "rejected" {
			t.Fatalf("old epoch reacquired %+v", r)
		}
	}
}

type blockedCompute struct {
	*domain.TrustedComputeDriver
	entered chan struct{}
	resume  chan struct{}
}

func (d *blockedCompute) Start(ctx context.Context, q domain.AttemptRequest, barrier func(context.Context) error) (domain.Fact, error) {
	return d.TrustedComputeDriver.Start(ctx, q, func(ctx context.Context) error {
		if err := barrier(ctx); err != nil {
			return err
		}
		close(d.entered)
		select {
		case <-d.resume:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	})
}

type hostAdmission struct {
	root      string
	mu        sync.Mutex
	loseFirst bool
}
type admittedChild struct {
	Kind       string        `json:"kind"`
	TargetRef  api.ObjectRef `json:"target_ref"`
	ReceiptRef api.ObjectRef `json:"receipt_ref"`
	Closed     bool          `json:"closed"`
}

func (h *hostAdmission) Admit(ctx context.Context, sc rt.Scope, call domain.HostCall) (domain.HostCallDecision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := filepath.Join(h.root, call.CommandRef.ObjectID)
	if _, err := os.Stat(p); err == nil {
		return domain.HostCallDecision{}, api.E("idempotency_conflict", "fixture_should_query_original")
	}
	child := admittedChild{Kind: call.Kind, TargetRef: sc.Ref(api.NewID(call.Kind), 1), ReceiptRef: call.CommandRef}
	if err := os.WriteFile(p, api.Raw(child), 0600); err != nil {
		return domain.HostCallDecision{}, err
	}
	if h.loseFirst {
		h.loseFirst = false
		return domain.HostCallDecision{}, context.Canceled
	}
	return domain.HostCallDecision{TargetKind: child.Kind, TargetRef: child.TargetRef, ReceiptRef: child.ReceiptRef}, nil
}
func (h *hostAdmission) Resolve(ctx context.Context, sc rt.Scope, call domain.HostCall) (domain.HostCallDecision, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	b, err := os.ReadFile(filepath.Join(h.root, call.CommandRef.ObjectID))
	if err != nil {
		return domain.HostCallDecision{}, err
	}
	var child admittedChild
	if err = api.Decode(b, &child); err != nil {
		return domain.HostCallDecision{}, err
	}
	return domain.HostCallDecision{TargetKind: child.Kind, TargetRef: child.TargetRef, ReceiptRef: child.ReceiptRef}, nil
}
func (h *hostAdmission) Close(ctx context.Context, sc rt.Scope, call domain.HostCall) (bool, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	p := filepath.Join(h.root, call.CommandRef.ObjectID)
	b, err := os.ReadFile(p)
	if err != nil {
		return false, err
	}
	var child admittedChild
	if err = api.Decode(b, &child); err != nil {
		return false, err
	}
	if call.TargetRef == nil || !api.Equal(child.TargetRef, *call.TargetRef) {
		return false, api.E("invalid_request", "child_mapping_mismatch")
	}
	child.Closed = true
	return true, os.WriteFile(p, api.Raw(child), 0600)
}

func TestEnvironmentDurableHostCallsKeepThreeKindsAndRecoverLostAdmission(t *testing.T) {
	f := newExecutionFixture(t)
	blocked := &blockedCompute{TrustedComputeDriver: &domain.TrustedComputeDriver{Content: f.content, Store: f.st, Location: "device"}, entered: make(chan struct{}), resume: make(chan struct{})}
	admission := &hostAdmission{root: t.TempDir(), loseFirst: true}
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{blocked}, HostCalls: admission})
	if err != nil {
		t.Fatal(err)
	}
	registry := rt.NewRegistry()
	if err = service.Register(registry); err != nil {
		t.Fatal(err)
	}
	f.registry = registry
	f.dispatcher.Registry = registry
	id := api.NewID("environment")
	config := api.ComponentRef{ComponentID: domain.BuiltinComponentID("environment.passive"), Version: "1", Digest: api.Hash([]byte(domain.PassiveEnvironmentFormat))}
	r := f.command(t, "environment.create", id, domain.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: config, InstallLockRef: f.install, Limits: []api.Amount{{Unit: "namespace_bytes", Value: "65536"}}, ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("create %+v", r)
	}
	f.drain(t)
	var env domain.Environment
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	code := f.put(t, api.Raw(domain.ComputeProgram{Opcode: "copy", TargetName: "result", InputNames: []string{"value"}}))
	inputRef := f.put(t, api.Raw(domain.PassiveNamespace{Format: domain.PassiveEnvironmentFormat, Bindings: []domain.NamespaceBinding{{Name: "value", Kind: "string", String: "passive"}}}))
	invoke := f.invokeInput(t, false)
	var intent domain.ExecutionIntent
	raw, _ := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
	api.Decode(raw, &intent)
	args := domain.ComputeArguments{EnvironmentRef: f.sc.Ref(id, env.Revision), ExpectedGeneration: 1, ExpectedNamespaceRevision: 1, CodeRef: code, InputRef: inputRef, OutputSchemaRef: domain.NamespaceOutputSchemaRef()}
	intent.ArgumentsRef = f.put(t, api.Raw(args))
	invoke.CapabilityRef = domain.TrustedComputeCapability().Ref
	intent.CapabilityRef = invoke.CapabilityRef
	intent.ProcessedSourceRefs = []api.ContentRef{code, inputRef, *env.NamespaceRef}
	invoke.IntentRef = f.put(t, api.Raw(intent))
	invoke.IntentHash, _ = api.Digest(intent)
	r = f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	if r.Stage != "applied" {
		t.Fatalf("invoke %+v", r)
	}
	works, status, err := f.st.Claim(context.Background(), f.sc, api.NewID("holder"), []string{domain.RunJob}, 1, 30*time.Second)
	if err != nil || status != rt.Committed || len(works) != 1 {
		t.Fatalf("cell claim %v %v", status, err)
	}
	run, _ := registry.Job(domain.RunJob)
	finished := make(chan error, 1)
	go func() { finished <- run(context.Background(), f.st, f.sc, works[0]) }()
	select {
	case <-blocked.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("cell never reached actual entry")
	}
	for _, kind := range []string{"operation", "decision", "delegation"} {
		input := f.put(t, api.Raw(struct {
			Kind string `json:"kind"`
		}{kind}))
		status, err = f.st.Within(context.Background(), f.sc, []string{domain.Namespace}, func(tx rt.Tx) error {
			_, err := service.AllocateHostCallTx(context.Background(), tx, id, invoke.OperationID, domain.HostCallInput{Kind: kind, TypedInputRef: input, InputDigest: input.Hash, ExpectedGeneration: 1})
			return err
		})
		if err != nil || status != rt.Committed {
			t.Fatalf("allocate kind %s: %s %v", kind, status, err)
		}
	}
	hostWorks, status, err := f.st.Claim(context.Background(), f.sc, api.NewID("holder"), []string{domain.HostCallJob}, 3, 30*time.Second)
	if err != nil || status != rt.Committed || len(hostWorks) != 3 {
		t.Fatalf("host claim %s %v", status, err)
	}
	hostRun, _ := registry.Job(domain.HostCallJob)
	for i, work := range hostWorks {
		err = hostRun(context.Background(), f.st, f.sc, work)
		if i == 0 {
			if err == nil {
				t.Fatal("expected lost admission reply")
			}
			err = hostRun(context.Background(), f.st, f.sc, work)
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	actual, err := os.ReadDir(admission.root)
	if err != nil || len(actual) != 3 {
		t.Fatalf("host admission duplicated: %d %v", len(actual), err)
	}
	close(blocked.resume)
	if err = <-finished; err != nil {
		t.Fatal(err)
	}
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	rev := env.Revision
	r = f.command(t, "environment.stop", id, domain.EnvironmentStopInput{EnvironmentID: id, ExpectedGeneration: 1, Reason: "close all child responsibility kinds"}, &rev)
	if r.Stage != "applied" {
		t.Fatalf("stop %+v", r)
	}
	f.drain(t)
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	if env.Phase != "closed" || len(env.StopResiduals) != 0 {
		t.Fatalf("host mappings lost stop responsibility %+v", env)
	}
	for _, entry := range actual {
		b, err := os.ReadFile(filepath.Join(admission.root, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		var child admittedChild
		if err = api.Decode(b, &child); err != nil || !child.Closed {
			t.Fatalf("original child not closed %+v %v", child, err)
		}
	}
}

func TestExecutionUsesNewIndependentControlWindowWithoutChangingOriginalIntent(t *testing.T) {
	f := newExecutionFixture(t)
	input := f.invokeInput(t, false)
	input.ControlSnapshot.IssuedAt = api.Time(time.Now().Add(-3 * time.Second))
	input.ControlSnapshot.StartBefore = api.Time(time.Now().Add(-time.Second))
	r := f.command(t, "execution.invoke", input.OperationID, input, nil)
	if r.Stage != "applied" {
		t.Fatalf("accepted original intent %+v", r)
	}
	window := input.ControlSnapshot
	window.WindowID = api.NewID("window")
	window.IssuedAt = api.Time(time.Now().Add(-time.Millisecond))
	window.StartBefore = input.Deadline
	r = f.command(t, "execution.control", input.TaskRef.ObjectID, domain.ControlInput{TaskRef: input.TaskRef, Snapshot: window}, nil)
	if r.Stage != "applied" {
		t.Fatalf("fresh window %+v", r)
	}
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", input.OperationID, domain.OperationIDInput{OperationID: input.OperationID}, &view)
	if view.Operation.Effect != "applied" || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].ControlWindowID != window.WindowID {
		t.Fatalf("fresh window not bound to physical attempt %+v", view)
	}
}
