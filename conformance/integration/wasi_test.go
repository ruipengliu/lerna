package integration_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/adapters/sqlite"
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

func TestRestrictedWASIUnknownBarrierCannotSpawnAndResolvesWithoutNewAttempt(t *testing.T) {
	f, host, cfg := newWASIFixtureConfig(t)
	env := wasiEnvironment(t, f, host, wasi.DefaultLimits())
	if err := host.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.st.Close(); err != nil {
		t.Fatal(err)
	}
	armed := &atomic.Bool{}
	var err error
	if f.postgresDSN != "" {
		f.st, err = postgres.Open(context.Background(), f.postgresDSN, postgres.WithCommitFault(func(phase postgres.CommitPhase) error {
			if phase == postgres.AfterCommit && armed.CompareAndSwap(true, false) {
				return context.Canceled
			}
			return nil
		}))
	} else {
		f.st, err = sqlite.Open(f.databasePath, sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
			if phase == sqlite.AfterCommit && armed.CompareAndSwap(true, false) {
				return context.Canceled
			}
			return nil
		}))
	}
	if err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Store = f.st
	cfg.Store = f.st
	host, err = wasi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := host.Close(); err != nil {
			t.Error(err)
		}
		f.st.Close()
	})
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{&armCommitFaultDriver{Driver: host, armed: armed}}, EnvironmentAdmission: host.Admission()})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = rt.NewRegistry()
	if err = service.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Registry = f.registry
	invoke := wasiInvoke(t, f, env, wasiOutputModule([]byte(`{"format":"harness-passive-namespace/1","bindings":[{"name":"answer","kind":"decimal","decimal":"42"}]}`), 1, nil))
	f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	claims, status, err := f.st.Claim(context.Background(), f.sc, api.NewID("holder"), []string{domain.RunJob}, 1, 30*time.Second)
	if err != nil || status != rt.Committed || len(claims) != 1 {
		t.Fatalf("actual original claim %s %v", status, err)
	}
	run, _ := f.registry.Job(domain.RunJob)
	err = run(context.Background(), f.st, f.sc, claims[0])
	if !errors.Is(err, rt.ErrCommitUnknown) {
		t.Fatalf("original unknown barrier returned %v", err)
	}
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	if len(view.Attempts.Items) != 1 || view.Operation.Effect != "unknown" {
		t.Fatalf("unknown barrier lost original identity %+v", view)
	}
	if _, err = os.Stat(filepath.Join(cfg.Root, view.Attempts.Items[0].AttemptID+".json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unknown barrier created physical run journal %v", err)
	}
	f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
	f.drain(t)
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	if view.Operation.Effect != "not_applied" || view.Operation.MayApplyLater != false || !view.Operation.UsageFinal || len(view.Attempts.Items) != 1 || env.NamespaceRevision != 1 || !env.ReadyForCell || !env.ActuallyExited {
		t.Fatalf("original no-entry resolution fabricated output or stranded the namespace %+v %+v", view, env)
	}
}

func wasiInvoke(t *testing.T, f *executionFixture, env domain.Environment, code []byte) domain.InvokeInput {
	return wasiInvokeWithInput(t, f, env, code, []byte(`{"format":"harness-passive-namespace/1","bindings":[]}`))
}
func wasiInvokeWithInput(t *testing.T, f *executionFixture, env domain.Environment, code, inputBytes []byte) domain.InvokeInput {
	t.Helper()
	module := f.put(t, code)
	input := f.put(t, inputBytes)
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
	intent.CostBound = []api.Amount{{Unit: "cpu_seconds", Value: "6"}}
	invoke.IntentRef = f.put(t, api.Raw(intent))
	invoke.IntentHash, _ = api.Digest(intent)
	return invoke
}

func TestRestrictedWASIMaliciousCellsKeepPriorNamespaceAndAccountActualCPU(t *testing.T) {
	f, runtime := newWASIFixture(t)
	want := []byte(`{"format":"harness-passive-namespace/1","bindings":[{"name":"uncommitted","kind":"decimal","decimal":"999"}]}`)
	cases := []struct {
		name   string
		module []byte
		limit  []api.Amount
		reason string
	}{
		{"trap_after_output", wasiOutputModule(want, 1, []byte{0x00}), wasi.DefaultLimits(), "cell_runtime_failure"},
		{"initial_memory_over_limit", wasiOutputModule(want, 257, nil), wasi.DefaultLimits(), "cell_runtime_failure"},
		{"output_over_limit", wasiOutputModule(make([]byte, 65537), 2, nil), wasi.DefaultLimits(), "cell_output_limit"},
		{"undeclared_native_import", wasiNativeImportModule(), wasi.DefaultLimits(), "cell_runtime_failure"},
		{"cpu_loop", wasiLoopModule(), []api.Amount{{Unit: "namespace_bytes", Value: "65536"}, {Unit: "memory_pages", Value: "256"}, {Unit: "cpu_seconds", Value: "1"}, {Unit: "wall_millis", Value: "10000"}, {Unit: "output_bytes", Value: "65536"}}, "cell_process_failure"},
		{"wall_loop", wasiLoopModule(), []api.Amount{{Unit: "namespace_bytes", Value: "65536"}, {Unit: "memory_pages", Value: "256"}, {Unit: "cpu_seconds", Value: "5"}, {Unit: "wall_millis", Value: "50"}, {Unit: "output_bytes", Value: "65536"}}, "cell_wall_limit"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := wasiEnvironment(t, f, runtime, tc.limit)
			original := *env.NamespaceRef
			invoke := wasiInvoke(t, f, env, tc.module)
			start := time.Now()
			f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
			f.drain(t)
			if time.Since(start) > 8*time.Second {
				t.Fatal("bounded cell did not actually exit")
			}
			var view domain.OperationView
			f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
			f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
			if view.Operation.Effect != "not_applied" || !view.Operation.UsageFinal || !view.ActuallyStopped || view.Operation.ResultRef != nil || len(view.Attempts.Items) != 1 || env.NamespaceRevision != 1 || !api.Equal(*env.NamespaceRef, original) || !env.ReadyForCell || !env.ActuallyExited {
				t.Fatalf("failed cell published partial data or remained live: %+v %+v", view, env)
			}
			if len(view.Operation.Usage) != 1 || view.Operation.Usage[0].Unit != "cpu_seconds" {
				t.Fatalf("actual independent compute meter missing: %+v", view.Operation.Usage)
			}
			var receipt wasi.Receipt
			for _, ref := range view.Operation.EvidenceRefs {
				b, e := f.content.ReadBytes(context.Background(), f.sc, f.auth, ref, "verify", "device")
				if e != nil {
					t.Fatal(e)
				}
				if api.Decode(b, &receipt) != nil {
					t.Fatal("invalid execution proof")
				}
			}
			if receipt.Reason != tc.reason || receipt.AttemptID != view.Attempts.Items[0].AttemptID || receipt.SpawnCount != 1 || !receipt.SpawnCountKnown || !receipt.ActuallyExited || !receipt.UsageFinal || receipt.OutputHash != api.Hash([]byte{}) {
				t.Fatalf("original actual exit proof: %+v", receipt)
			}
		})
	}
}

func wasiNativeImportModule() []byte {
	return []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 4, 1, 0x60, 0, 0, 2, 14, 1, 3, 'e', 'n', 'v', 6, 's', 'y', 's', 't', 'e', 'm', 0, 0, 7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0}
}

func TestRestrictedWASICanReadOnlyPassiveStdinAndHasNoHostHandles(t *testing.T) {
	f, runtime := newWASIFixture(t)
	env := wasiEnvironment(t, f, runtime, wasi.DefaultLimits())
	want := []byte(`{"format":"harness-passive-namespace/1","bindings":[{"name":"host_access","kind":"string","string":"denied"}]}`)
	invoke := wasiInvoke(t, f, env, wasiDeniedHostModule(want))
	f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	f.drain(t)
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	if env.NamespaceRevision != 2 {
		t.Fatalf("real guest could access env/file/network handles, or failed to run: %+v", env)
	}
	b, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, *env.NamespaceRef, "verify", "device")
	if err != nil || string(b) != string(want) {
		t.Fatalf("actual guest deny checks %s %v", b, err)
	}
	previous := env
	invoke = wasiInvokeWithInput(t, f, env, wasiEchoModule(), []byte(`{"format":"harness-passive-namespace/1","bindings":[{"name":"answer","kind":"decimal","decimal":"42"}]}`))
	f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	f.drain(t)
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	b, err = f.content.ReadBytes(context.Background(), f.sc, f.auth, *env.NamespaceRef, "verify", "device")
	if err != nil || env.NamespaceRevision != 3 || string(b) != `{"format":"harness-passive-namespace/1","bindings":[{"name":"answer","kind":"decimal","decimal":"42"},{"name":"host_access","kind":"string","string":"denied"}]}` {
		t.Fatalf("guest must receive old namespace plus declared input: %s %+v %v", b, env, err)
	}
	// 后继原意图持有旧CAS，不能借公开新header悄悄刷新后执行。
	stale := wasiInvoke(t, f, previous, wasiWriteModule(want))
	f.command(t, "execution.invoke", stale.OperationID, stale, nil)
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", stale.OperationID, domain.OperationIDInput{OperationID: stale.OperationID}, &view)
	if view.Operation.Effect != "not_started" || len(view.Attempts.Items) != 0 {
		t.Fatalf("old namespace CAS crossed actual entry %+v", view)
	}
}

func wasiClaimOne(t *testing.T, f *executionFixture, kind string) rt.Work {
	t.Helper()
	work, status, err := f.st.Claim(context.Background(), f.sc, api.NewID("holder"), []string{kind}, 1, 30*time.Second)
	if err != nil || status != rt.Committed || len(work) != 1 {
		t.Fatalf("claim %s %v %v %d", kind, status, err, len(work))
	}
	return work[0]
}

func TestRestrictedWASIReconcileCannotReleaseLiveCellAndStopFencesGeneration(t *testing.T) {
	f, runtime := newWASIFixture(t)
	limits := wasi.DefaultLimits()
	for i := range limits {
		if limits[i].Unit == "cpu_seconds" {
			limits[i].Value = "5"
		}
		if limits[i].Unit == "wall_millis" {
			limits[i].Value = "10000"
		}
	}
	env := wasiEnvironment(t, f, runtime, limits)
	originalNamespace := *env.NamespaceRef
	invoke := wasiInvoke(t, f, env, wasiLoopModule())
	f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	run, _ := f.registry.Job(domain.RunJob)
	work := wasiClaimOne(t, f, domain.RunJob)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	finished := make(chan error, 1)
	go func() { finished <- run(ctx, f.st, f.sc, work) }()
	deadline := time.Now().Add(4 * time.Second)
	for {
		f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
		if !env.ReadyForCell && !env.ActuallyExited && len(env.ActiveOperationIDs) == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("real looping cell never entered %+v", env)
		}
		time.Sleep(10 * time.Millisecond)
	}
	f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
	reconcile, _ := f.registry.Job(domain.ReconcileJob)
	if err := reconcile(context.Background(), f.st, f.sc, wasiClaimOne(t, f, domain.ReconcileJob)); err != nil {
		t.Fatal(err)
	}
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	if env.ReadyForCell || env.ActuallyExited || len(env.ActiveOperationIDs) != 1 || env.NamespaceRevision != 1 || !api.Equal(*env.NamespaceRef, originalNamespace) {
		t.Fatalf("unknown observation must keep original running process busy: %+v", env)
	}
	stop := f.command(t, "environment.stop", env.EnvironmentID, domain.EnvironmentStopInput{EnvironmentID: env.EnvironmentID, ExpectedGeneration: env.Generation, Reason: "cancel original cell"}, &env.Revision)
	if stop.Stage != "applied" {
		t.Fatalf("stop %+v", stop)
	}
	f.drain(t)
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stop did not join actual original WASI process")
	}
	f.drain(t)
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	if env.Generation != 2 || env.Phase != "closed" || !env.ActuallyExited || env.ReadyForCell || env.NamespaceRevision != 1 || !api.Equal(*env.NamespaceRef, originalNamespace) {
		t.Fatalf("cancelled generation must keep prior namespace and actually close: %+v", env)
	}
}

func wasiLoopModule() []byte {
	return []byte{0, 97, 115, 109, 1, 0, 0, 0, 1, 4, 1, 0x60, 0, 0, 3, 2, 1, 0, 7, 10, 1, 6, '_', 's', 't', 'a', 'r', 't', 0, 0, 10, 9, 1, 7, 0, 0x03, 0x40, 0x0c, 0, 0x0b, 0x0b}
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
	return wasiOutputModule(text, 1, nil)
}
func wasiOutputModule(text []byte, memoryPages uint32, tail []byte) []byte {
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
	section(5, append([]byte{1, 0}, wasiLEB(memoryPages)...))
	exports := append([]byte{2}, name("memory")...)
	exports = append(exports, 2, 0)
	exports = append(exports, name("_start")...)
	section(7, append(exports, 0, 1))
	code := []byte{0, 0x41, 0, 0x41, 0x80, 0x08, 0x36, 2, 0, 0x41, 4, 0x41}
	code = append(code, wasiSLEB(int32(len(text)))...)
	code = append(code, 0x36, 2, 0, 0x41, 1, 0x41, 0, 0x41, 1, 0x41, 8, 0x10, 0, 0x1a)
	code = append(code, tail...)
	code = append(code, 0x0b)
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
	f, runtime, _ := newWASIFixtureConfig(t)
	return f, runtime
}
func newWASIFixtureConfig(t *testing.T) (*executionFixture, *wasi.Runtime, wasi.Config) {
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
	cfg := wasi.Config{Root: t.TempDir(), WorkerPath: worker, WorkerHash: api.Hash(workerBytes), Store: f.st, Content: f.content, Scope: f.sc, Location: "device", MaxConcurrent: 2}
	runtime, err := wasi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := runtime.Close(); err != nil {
			t.Error(err)
		}
	})
	f.install = runtime.InstallLockRef()
	mountWASI(t, f, runtime, cfg.Content)
	return f, runtime, cfg
}
func mountWASI(t *testing.T, f *executionFixture, runtime *wasi.Runtime, content domain.ContentPort) {
	t.Helper()
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{runtime}, EnvironmentAdmission: runtime.Admission()})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = rt.NewRegistry()
	if err = service.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Registry = f.registry
}

func TestRestrictedWASIEnvironmentRequiresActualPinnedRuntimeBeforeReady(t *testing.T) {
	f, runtime := newWASIFixture(t)
	t.Logf("actual selected RuntimeManifest: %s", api.Raw(runtime.Manifest()))
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

func TestRestrictedWASICachedProfileCannotRunOrReplaceOriginalWorker(t *testing.T) {
	f, host, cfg := newWASIFixtureConfig(t)
	id := api.NewID("environment")
	changedLock := host.InstallLockRef()
	changedLock.Digest = api.Hash([]byte("unregistered runtime"))
	rejected := f.command(t, "environment.create", id, domain.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: host.EnvironmentConfigRef(), InstallLockRef: changedLock, Limits: wasi.DefaultLimits(), ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}}, nil)
	if rejected.Error == nil || rejected.Error.Reason != "wasi_runtime_lock_changed" {
		t.Fatalf("unregistered runtime must be rejected before environment creation: %+v", rejected)
	}
	profile, err := wasi.LoadProfile(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !api.Equal(profile.EnvironmentConfigRef(), host.EnvironmentConfigRef()) || !api.Equal(profile.InstallLockRef(), host.InstallLockRef()) {
		t.Fatal("cached profile changed original worker identities")
	}
	if _, err = profile.Check(profile.EnvironmentConfigRef(), profile.InstallLockRef(), wasi.DefaultLimits()); err != nil {
		t.Fatal(err)
	}
	if err = profile.Prepare(context.Background(), f.sc, f.auth, domain.Environment{}); err == nil {
		t.Fatal("non-worker cached profile claimed actual preparation")
	}
	if other, err := wasi.New(cfg); err == nil {
		other.Close()
		t.Fatal("two hosts own original process journal")
	}
	changed := cfg
	changed.WorkerHash = api.Hash([]byte("another worker"))
	if _, err = wasi.LoadProfile(changed); err == nil {
		t.Fatal("replacement worker accepted original profile")
	}
	copy := profile.Manifest()
	copy.Probe.RootEntries[0] = "host-root"
	if profile.Manifest().Probe.RootEntries[0] != "worker" {
		t.Fatal("caller can mutate original trusted profile")
	}
	if err = host.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err = host.Check(host.EnvironmentConfigRef(), host.InstallLockRef(), wasi.DefaultLimits()); err == nil {
		t.Fatal("closed host continues to accept new environment preparation")
	}
}

func wasiDeniedHostModule(text []byte) []byte {
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	section := func(id byte, b []byte) {
		module = append(module, id)
		module = append(module, wasiLEB(uint32(len(b)))...)
		module = append(module, b...)
	}
	name := func(s string) []byte { return append(wasiLEB(uint32(len(s))), []byte(s)...) }
	section(1, []byte{4, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 2, 0x7f, 0x7f, 1, 0x7f, 0x60, 3, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 0, 0})
	imports := []byte{4}
	for _, p := range []struct {
		name string
		typ  byte
	}{{"fd_write", 0}, {"environ_sizes_get", 1}, {"fd_prestat_get", 1}, {"sock_accept", 2}} {
		imports = append(imports, name("wasi_snapshot_preview1")...)
		imports = append(imports, name(p.name)...)
		imports = append(imports, 0, p.typ)
	}
	section(2, imports)
	section(3, []byte{1, 3})
	section(5, []byte{1, 0, 1})
	exports := append([]byte{2}, name("memory")...)
	exports = append(exports, 2, 0)
	exports = append(exports, name("_start")...)
	section(7, append(exports, 0, 4))
	// 真实 WASI ABI：env为空，fd3没有预打开目录，fd0不能接受socket。任一检查失效就trap。
	code := []byte{0, 0x41, 16, 0x41, 20, 0x10, 1, 0x04, 0x40, 0x00, 0x0b, 0x41, 16, 0x28, 2, 0, 0x04, 0x40, 0x00, 0x0b, 0x41, 20, 0x28, 2, 0, 0x04, 0x40, 0x00, 0x0b, 0x41, 3, 0x41, 0, 0x10, 2, 0x45, 0x04, 0x40, 0x00, 0x0b, 0x41, 0, 0x41, 0, 0x41, 24, 0x10, 3, 0x45, 0x04, 0x40, 0x00, 0x0b}
	code = append(code, 0x41, 0, 0x41, 0x80, 0x08, 0x36, 2, 0, 0x41, 4, 0x41)
	code = append(code, wasiSLEB(int32(len(text)))...)
	code = append(code, 0x36, 2, 0, 0x41, 1, 0x41, 0, 0x41, 1, 0x41, 8, 0x10, 0, 0x1a, 0x0b)
	section(10, append(append([]byte{1}, wasiLEB(uint32(len(code)))...), code...))
	data := append([]byte{1, 0, 0x41, 0x80, 0x08, 0x0b}, wasiLEB(uint32(len(text)))...)
	section(11, append(data, text...))
	return module
}
func wasiEchoModule() []byte {
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	section := func(id byte, b []byte) {
		module = append(module, id)
		module = append(module, wasiLEB(uint32(len(b)))...)
		module = append(module, b...)
	}
	name := func(s string) []byte { return append(wasiLEB(uint32(len(s))), []byte(s)...) }
	section(1, []byte{2, 0x60, 4, 0x7f, 0x7f, 0x7f, 0x7f, 1, 0x7f, 0x60, 0, 0})
	imports := []byte{2}
	for _, n := range []string{"fd_read", "fd_write"} {
		imports = append(imports, name("wasi_snapshot_preview1")...)
		imports = append(imports, name(n)...)
		imports = append(imports, 0, 0)
	}
	section(2, imports)
	section(3, []byte{1, 1})
	section(5, []byte{1, 0, 2})
	exports := append([]byte{2}, name("memory")...)
	exports = append(exports, 2, 0)
	exports = append(exports, name("_start")...)
	section(7, append(exports, 0, 2))
	code := []byte{0, 0x41, 0, 0x41, 0x80, 0x08, 0x36, 2, 0, 0x41, 4, 0x41, 0x80, 0x80, 0x04, 0x36, 2, 0, 0x41, 0, 0x41, 0, 0x41, 1, 0x41, 8, 0x10, 0, 0x04, 0x40, 0x00, 0x0b, 0x41, 4, 0x41, 8, 0x28, 2, 0, 0x36, 2, 0, 0x41, 1, 0x41, 0, 0x41, 1, 0x41, 12, 0x10, 1, 0x1a, 0x0b}
	section(10, append(append([]byte{1}, wasiLEB(uint32(len(code)))...), code...))
	return module
}

type wasiPublicationReplyLoss struct {
	domain.ContentPort
	armed atomic.Bool
}

func (c *wasiPublicationReplyLoss) Publish(ctx context.Context, sc rt.Scope, a rt.Auth, p domain.Publication, b []byte) (api.ContentRef, error) {
	ref, err := c.ContentPort.Publish(ctx, sc, a, p, b)
	if err == nil && p.Purpose == "execution_result" && c.armed.CompareAndSwap(true, false) {
		return api.ContentRef{}, api.E("dependency_unavailable", "original_content_reply_lost")
	}
	return ref, err
}

func TestRestrictedWASIReopenAfterLostReplyRetainsOriginalResultAndKnownCPU(t *testing.T) {
	f, runtime, cfg := newWASIFixtureConfig(t)
	loss := &wasiPublicationReplyLoss{ContentPort: f.content}
	mountWASI(t, f, runtime, loss)
	env := wasiEnvironment(t, f, runtime, wasi.DefaultLimits())
	want := []byte(`{"format":"harness-passive-namespace/1","bindings":[{"name":"answer","kind":"decimal","decimal":"42"}]}`)
	invoke := wasiInvoke(t, f, env, wasiWriteModule(want))
	f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	loss.armed.Store(true)
	if err := rt.Drain(context.Background(), f.st, f.sc, f.registry, 100); err == nil {
		t.Fatal("original physical WASM result reply fault was not reached")
	}
	var original domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &original)
	if original.Operation.Effect != "unknown" || original.Operation.UsageFinal || len(original.Attempts.Items) != 1 {
		t.Fatalf("lost reply prematurely decided original attempt %+v", original)
	}
	attemptID := original.Attempts.Items[0].AttemptID
	var intent domain.ExecutionIntent
	b, _ := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
	if err := api.Decode(b, &intent); err != nil {
		t.Fatal(err)
	}
	var args domain.ComputeArguments
	b, _ = f.content.ReadBytes(context.Background(), f.sc, f.auth, intent.ArgumentsRef, "prepare", "device")
	if err := api.Decode(b, &args); err != nil {
		t.Fatal(err)
	}
	// 用户模块和原输入实际删除；恢复只能读原退出日志，不能重建正文后再运行。
	for _, ref := range []api.ContentRef{args.CodeRef, args.InputRef} {
		if err := os.Remove(filepath.Join(f.content.root, ref.ContentID)); err != nil {
			t.Fatal(err)
		}
	}
	if err := runtime.Close(); err != nil {
		t.Fatal(err)
	}
	f.restartStore(t)
	cfg.Store = f.st
	reopened, err := wasi.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	mountWASI(t, f, reopened, f.content)
	f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	f.query(t, "environment.get", env.EnvironmentID, domain.EnvironmentIDInput{EnvironmentID: env.EnvironmentID}, &env)
	if view.Operation.Effect != "applied" || !view.Operation.UsageFinal || !view.ActuallyStopped || view.EffectDisputed || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].AttemptID != attemptID || env.NamespaceRevision != 2 || !env.ReadyForCell {
		t.Fatalf("original durable process fact did not recover %+v %+v", view, env)
	}
	if len(view.Operation.Usage) != 1 || view.Operation.Usage[0].Unit != "cpu_seconds" {
		t.Fatalf("original known CPU missing %+v", view.Operation.Usage)
	}
	known := view.Operation.Usage
	b, err = f.content.ReadBytes(context.Background(), f.sc, f.auth, *env.NamespaceRef, "verify", "device")
	if err != nil || string(b) != string(want) {
		t.Fatalf("original actual namespace %s %v", b, err)
	}
	var proof wasi.Receipt
	for _, ref := range view.Operation.EvidenceRefs {
		b, err = f.content.ReadBytes(context.Background(), f.sc, f.auth, ref, "verify", "device")
		if err != nil || api.Decode(b, &proof) != nil {
			t.Fatal("original receipt missing")
		}
	}
	if proof.SpawnCount != 1 || !proof.SpawnCountKnown || proof.AttemptID != attemptID || !proof.ActuallyExited || !api.Equal(proof.Usage, known) {
		t.Fatalf("original physical proof %+v", proof)
	}
	f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
	f.drain(t)
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	if view.Operation.Effect != "applied" || view.EffectDisputed || !api.Equal(view.Operation.Usage, known) || len(view.Attempts.Items) != 1 || view.Attempts.Items[0].AttemptID != attemptID {
		t.Fatalf("lookup rewrote original known usage %+v", view)
	}
}
