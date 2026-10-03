package integration_test

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

func wasiEnvironment(t *testing.T, f *executionFixture, runtime *wasi.Runtime, limits []api.Amount) domain.Environment {
	t.Helper()
	id := api.NewID("environment")
	r := f.command(t, "environment.create", id, domain.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: runtime.EnvironmentConfigRef(), InstallLockRef: runtime.InstallLockRef(), Limits: limits, ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("environment preparation %+v", r)
	}
	f.drain(t)
	var env domain.Environment
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	return env
}

func wasiInvoke(t *testing.T, f *executionFixture, env domain.Environment, code []byte) domain.InvokeInput {
	t.Helper()
	module := f.put(t, code)
	input := f.put(t, []byte(`{"format":"harness-passive-namespace/1","bindings":[]}`))
	invoke := f.invokeInput(t, false)
	var intent domain.ExecutionIntent
	raw, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
	if err != nil || api.Decode(raw, &intent) != nil {
		t.Fatalf("original intent %v", err)
	}
	invoke.CapabilityRef = domain.WASIRunCellCapability().Ref
	intent.CapabilityRef = invoke.CapabilityRef
	intent.ArgumentsRef = f.put(t, api.Raw(domain.ComputeArguments{EnvironmentRef: f.sc.Ref(env.EnvironmentID, env.Revision), ExpectedGeneration: env.Generation, ExpectedNamespaceRevision: env.NamespaceRevision, CodeRef: module, InputRef: input, OutputSchemaRef: domain.NamespaceOutputSchemaRef()}))
	intent.ProcessedSourceRefs = append([]api.ContentRef{}, env.ProcessedSources...)
	intent.ProcessedSourceRefs = append(intent.ProcessedSourceRefs, module, input, *env.NamespaceRef)
	intent.CostBound = []api.Amount{{Unit: "cpu_seconds", Value: "5"}}
	invoke.IntentRef = f.put(t, api.Raw(intent))
	invoke.IntentHash, _ = api.Digest(intent)
	return invoke
}

func TestRestrictedWASIRunsRealModuleAndCommitsWholeNamespaceOnce(t *testing.T) {
	f, runtime := newWASIFixture(t)
	env := wasiEnvironment(t, f, runtime, wasi.DefaultLimits())
	want := []byte(`{"format":"harness-passive-namespace/1","bindings":[{"name":"answer","kind":"decimal","decimal":"42"}]}`)
	invoke := wasiInvoke(t, f, env, wasiWriteModule(want))
	r := f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	if r.Stage != "applied" {
		t.Fatalf("invoke %+v", r)
	}
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	if view.Operation.Effect != "applied" || !view.Operation.UsageFinal || !view.ActuallyStopped || view.Operation.ResultRef == nil || len(view.Attempts.Items) != 1 {
		t.Fatalf("actual WASI must succeed with one physical attempt and final usage: %+v", view)
	}
	originalAttempt := view.Attempts.Items[0].AttemptID
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	if env.NamespaceRevision != 2 || !env.ReadyForCell || !env.ActuallyExited || len(env.ActiveOperationIDs) != 0 {
		t.Fatalf("namespace head %+v", env)
	}
	got, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, *env.NamespaceRef, "verify", "device")
	if err != nil || string(got) != string(want) {
		t.Fatalf("independent original WASM result: %s %v", got, err)
	}
	for i := 0; i < 2; i++ {
		f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
		f.drain(t)
	}
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	if view.Operation.Effect != "applied" || view.EffectDisputed || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].AttemptID != originalAttempt || env.NamespaceRevision != 2 {
		t.Fatalf("original reconciliation must not replay a cell: %+v %+v", view, env)
	}
}

// 字面真值由实际 Preview1 fd_write WASM 输出；不替代引擎、不手填执行事实。
func wasiWriteModule(text []byte) []byte {
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	section := func(id byte, b []byte) {
		module = append(module, id)
		module = append(module, wasiLEB(uint32(len(b)))...)
		module = append(module, b...)
	}
	name := func(s string) []byte { return append(wasiLEB(uint32(len(s))), []byte(s)...) }
	section(1, []byte{2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 0, 0})
	imports := append([]byte{1}, name("wasi_snapshot_preview1")...)
	imports = append(imports, name("fd_write")...)
	section(2, append(imports, 0, 0))
	section(3, []byte{1, 1})
	section(5, []byte{1, 0, 1})
	exports := append([]byte{2}, name("memory")...)
	exports = append(exports, 2, 0)
	exports = append(exports, name("_start")...)
	section(7, append(exports, 0, 1))
	code := []byte{0, 0x41, 0, 0x41, 0x80, 0x08, 0x36, 2, 0, 0x41, 4, 0x41}
	code = append(code, wasiSLEB(int32(len(text)))...)
	code = append(code, 0x36, 2, 0, 0x41, 1, 0x41, 0, 0x41, 1, 0x41, 8, 0x10, 0, 0x1a, 0x0b)
	section(10, append(append([]byte{1}, wasiLEB(uint32(len(code)))...), code...))
	data := append([]byte{1, 0, 0x41, 0x80, 0x08, 0x0b}, wasiLEB(uint32(len(text)))...)
	section(11, append(data, text...))
	return module
}
func wasiLEB(n uint32) []byte {
	b := []byte{}
	for {
		x := byte(n & 127)
		n >>= 7
		if n != 0 {
			x |= 128
		}
		b = append(b, x)
		if n == 0 {
			return b
		}
	}
}
func wasiSLEB(n int32) []byte {
	b := []byte{}
	for {
		x := byte(n & 127)
		n >>= 7
		last := n == 0 && x&64 == 0 || n == -1 && x&64 != 0
		if !last {
			x |= 128
		}
		b = append(b, x)
		if last {
			return b
		}
	}
}

func newWASIFixture(t *testing.T) (*executionFixture, *wasi.Runtime) {
	t.Helper()
	f := newExecutionFixture(t)
	worker := filepath.Join(t.TempDir(), "worker")
	build := exec.Command("go", "build", "-trimpath", "-o", worker, "../../cmd/wasi-worker")
	build.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build actual pinned worker: %s %v", b, err)
	}
	workerBytes, err := os.ReadFile(worker)
	if err != nil {
		t.Fatal(err)
	}
	runtime, err := wasi.New(wasi.Config{Root: t.TempDir(), WorkerPath: worker, WorkerHash: api.Hash(workerBytes), Store: f.st, Content: f.content, Scope: f.sc, Location: "device", MaxConcurrent: 2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})
	f.install = runtime.InstallLockRef()
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{runtime}, EnvironmentAdmission: runtime.Admission()})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = rt.NewRegistry()
	if err = service.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Registry = f.registry
	return f, runtime
}

func TestRestrictedWASIEnvironmentRequiresActualPinnedRuntimeBeforeReady(t *testing.T) {
	f, runtime := newWASIFixture(t)
	id := api.NewID("environment")
	r := f.command(t, "environment.create", id, domain.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: runtime.EnvironmentConfigRef(), InstallLockRef: runtime.InstallLockRef(), Limits: wasi.DefaultLimits(), ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("real pinned WASI environment must accept preparation: %+v", r)
	}
	f.drain(t)
	var env domain.Environment
	f.query(t, "environment.get", id, domain.EnvironmentIDInput{EnvironmentID: id}, &env)
	if env.RuntimeKind != "restricted_wasi_preview1" || env.Phase != "active" || !env.ReadyForCell || !env.ActuallyExited || env.NamespaceRevision != 1 || env.NamespaceRef == nil || !api.Equal(env.InstallLockRef, runtime.InstallLockRef()) {
		t.Fatalf("actual prepared WASI state: %+v", env)
	}
}
