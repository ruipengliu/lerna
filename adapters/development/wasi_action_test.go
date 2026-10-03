package development

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

func TestConfiguredWASIEnvironmentUsesActualPinnedRuntimeAndOriginalPreparation(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			worker := filepath.Join(root, "runtime-worker")
			build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", worker, "../../cmd/wasi-worker")
			build.Env = append(os.Environ(), "CGO_ENABLED=0")
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("build the actual static WASI worker: %s %v", output, err)
			}
			artifact, err := os.ReadFile(worker)
			if err != nil {
				t.Fatal(err)
			}
			// 已有13端口只用来取得真实平台资格；业务断言经过宿主公开方法。
			discovery, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer discovery.Close()
			wasiConfig := WASIConfig{WorkerPath: worker, WorkerHash: api.Hash(artifact), MaxConcurrent: 1}
			qualified, err := configureWASI(discovery, &wasiConfig)
			if err != nil {
				t.Fatalf("actual platform qualification prerequisite: %v", err)
			}
			configRef, installLock := qualified.ConfigRef, qualified.InstallLockRef
			if err = qualified.Close(); err != nil {
				t.Fatal(err)
			}
			if err = discovery.Close(); err != nil {
				t.Fatal(err)
			}
			var configured map[string]any
			if err = json.Unmarshal(api.Raw(cfg), &configured); err != nil {
				t.Fatal(err)
			}
			configured["wasi"] = wasiConfig
			if err = api.Decode(api.Raw(configured), &cfg); err != nil {
				t.Fatalf("qualified runtime has no explicit host configuration: %v", err)
			}
			a, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			id := api.NewID("environment")
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "environment.create", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(execution.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: configRef, InstallLockRef: installLock, Limits: wasi.DefaultLimits(), ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}})}
			receipt, err := a.Dispatcher.Command(ctx, a.ServiceAuth, api.Raw(command))
			if err != nil || receipt.Error != nil || receipt.Stage != "accepted" {
				t.Fatalf("public environment admission did not accept the actual pinned runtime: %v %+v", err, receipt)
			}
			if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 100); err != nil {
				t.Fatal(err)
			}
			raw, err := a.query(ctx, "environment.get", id, execution.EnvironmentIDInput{EnvironmentID: id})
			var original execution.Environment
			if err != nil || api.Decode(raw, &original) != nil || original.Phase != "active" || original.RuntimeKind != "restricted_wasi_preview1" || !original.ReadyForCell || !original.ActuallyExited || original.NamespaceRef == nil || original.NamespaceRevision != 1 || !api.Equal(original.InstallLockRef, installLock) || original.PreparationCommandID != command.CommandID {
				t.Fatalf("actual original preparation/readiness was not preserved: %v %+v", err, original)
			}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			raw, err = reopened.query(ctx, "environment.get", id, execution.EnvironmentIDInput{EnvironmentID: id})
			var after execution.Environment
			if err != nil || api.Decode(raw, &after) != nil || !api.Equal(original, after) {
				t.Fatalf("reopen replaced the original environment/generation/namespace: %v %+v", err, after)
			}
			final, err := reopened.Dispatcher.Command(ctx, reopened.ServiceAuth, api.Raw(command))
			if err != nil || final.Stage != "applied" || final.CommandID != command.CommandID {
				t.Fatalf("reopen replaced the original preparation receipt: %v %+v", err, final)
			}
		})
	}
}

func TestConfiguredWASITaskCommitsOriginalNamespaceAndActualCPU(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), reportFixtureTimeout)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			worker := filepath.Join(root, "runtime-worker")
			build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", worker, "../../cmd/wasi-worker")
			build.Env = append(os.Environ(), "CGO_ENABLED=0")
			if output, err := build.CombinedOutput(); err != nil {
				t.Fatalf("actual pinned worker: %s %v", output, err)
			}
			artifact, err := os.ReadFile(worker)
			if err != nil {
				t.Fatal(err)
			}
			var configured map[string]any
			if err = json.Unmarshal(api.Raw(cfg), &configured); err != nil {
				t.Fatal(err)
			}
			configured["wasi"] = map[string]any{"worker_path": worker, "worker_hash": api.Hash(artifact), "max_concurrent": 1, "cpu_seconds_budget_limit": "6"}
			if err = api.Decode(api.Raw(configured), &cfg); err != nil {
				t.Fatalf("explicit finite Task CPU policy unavailable: %v", err)
			}
			var posts atomic.Int32
			var active atomic.Pointer[App]
			var code, input api.ContentRef
			var env execution.Environment
			binding := api.ObjectRef{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, ObjectID: api.NewID("binding"), Revision: 1}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				posts.Add(1)
				body, err := io.ReadAll(io.LimitReader(r.Body, api.MaxJSONBytes+1))
				var wire struct {
					Messages []struct {
						Content string `json:"content"`
					} `json:"messages"`
				}
				var request struct {
					Snapshot  api.Snapshot `json:"snapshot"`
					Materials []struct {
						Ref  api.ContentRef `json:"ref"`
						Body string         `json:"body_utf8"`
					} `json:"materials"`
				}
				if err != nil || json.Unmarshal(body, &wire) != nil || len(wire.Messages) != 2 || json.Unmarshal([]byte(wire.Messages[1].Content), &request) != nil {
					t.Error("actual frozen request could not be read")
					return
				}
				found, binarySource := false, false
				for n, cap := range request.Snapshot.CapabilityRefs {
					found = found || api.Equal(cap, execution.WASIRunCellCapability().Ref) && n < len(request.Snapshot.BindingRefs) && api.Equal(request.Snapshot.BindingRefs[n], binding)
				}
				for _, ref := range request.Snapshot.ProcessedSources {
					binarySource = binarySource || api.Equal(ref, code)
				}
				for _, material := range request.Materials {
					if api.Equal(material.Ref, code) {
						t.Error("binary program was converted into model text")
						return
					}
				}
				if posts.Load() <= 2 && !found || posts.Load() == 3 && found || !binarySource {
					t.Error("actual Snapshot omitted original WASI pair or binary Source")
					return
				}
				if request.Snapshot.Purpose == "interpret_requirements" {
					_, _ = w.Write(knowledgeRefinementReply(active.Load().ArtifactRule))
					return
				}
				if posts.Load() == 2 {
					var arguments *execution.ComputeArguments
					for _, material := range request.Materials {
						var packet struct {
							Actions []struct {
								BindingRef api.ObjectRef `json:"binding_ref"`
								Cell       *struct {
									Arguments execution.ComputeArguments `json:"arguments"`
								} `json:"cell,omitempty"`
							} `json:"actions"`
						}
						if json.Unmarshal([]byte(material.Body), &packet) != nil {
							continue
						}
						for _, declaration := range packet.Actions {
							if api.Equal(declaration.BindingRef, binding) && declaration.Cell != nil {
								value := declaration.Cell.Arguments
								arguments = &value
							}
						}
					}
					if arguments == nil || arguments.EnvironmentRef.ObjectID != env.EnvironmentID || arguments.ExpectedGeneration != 1 || arguments.ExpectedNamespaceRevision != 1 || !api.Equal(arguments.CodeRef, code) || !api.Equal(arguments.InputRef, input) {
						t.Error("ordinary declaration did not preserve original exact Cell inputs")
						return
					}
					generated := brain.Generated{Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "Run exactly the declared finite Cell without asserting its effect.", DisclosedSources: []api.ContentRef{}}, {LocalID: "arguments", MediaType: "application/json", Body: string(api.Raw(arguments)), DisclosedSources: []api.ContentRef{}}}, Draft: brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "original_cell", CapabilityRef: execution.WASIRunCellCapability().Ref, BindingRef: binding, ArgumentsLocalID: "arguments"}}}}
					_, _ = w.Write(wasiContractReply(generated))
					return
				}
				_, _ = w.Write(knowledgeContractReply())
			}))
			defer server.Close()
			t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "synthetic-contract-token")
			cfg.Model = contractModelConfig(server.URL)
			a, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			id := api.NewID("environment")
			create := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: id, Method: "environment.create", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(execution.EnvironmentCreateInput{EnvironmentID: id, ConfigRef: a.WASI.ConfigRef, InstallLockRef: a.WASI.InstallLockRef, Limits: wasi.DefaultLimits(), ExpiresAt: api.Time(time.Now().Add(time.Hour)), SourceRefs: []api.ContentRef{}})}
			receipt, err := a.Dispatcher.Command(ctx, a.ServiceAuth, api.Raw(create))
			if err != nil || receipt.Error != nil || receipt.Stage != "accepted" {
				t.Fatalf("original service-owned environment not admitted: %v %+v", err, receipt)
			}
			if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 100); err != nil {
				t.Fatal(err)
			}
			raw, err := a.query(ctx, "environment.get", id, execution.EnvironmentIDInput{EnvironmentID: id})
			if err != nil || api.Decode(raw, &env) != nil || !env.ReadyForCell || env.NamespaceRef == nil {
				t.Fatalf("actual original environment missing: %v %+v", err, env)
			}
			output := []byte(`{"format":"harness-passive-namespace/1","bindings":[{"name":"answer","kind":"decimal","decimal":"42"}]}`)
			code, err = a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/wasm", taskWASIOutputModule(output), []api.ContentRef{}, []api.ContentRef{})
			if err != nil {
				t.Fatal(err)
			}
			input, err = a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", []byte(`{"format":"harness-passive-namespace/1","bindings":[]}`), []api.ContentRef{}, []api.ContentRef{})
			if err != nil {
				t.Fatal(err)
			}
			grant := api.Grant{GrantID: api.NewID("grant"), OwnerID: cfg.OwnerID, Revision: 1, SubjectRef: a.ServiceAuth.Ref(a.Scope.OwnerID), Resources: []string{"environment:" + id}, Actions: []string{"environment.run_cell"}, Purposes: []string{"goal_action"}, Recipients: []string{cfg.OwnerID}, Locations: []string{"cloud"}, Mode: "once", State: "active", NotBefore: api.Time(time.Now().Add(-time.Minute)), ExpiresAt: cfg.PolicyExpiresAt, Limits: []api.Amount{{Unit: "cpu_seconds", Value: "6"}}}
			lock := a.WASI.InstallLockRef
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			if err = json.Unmarshal(api.Raw(cfg), &configured); err != nil {
				t.Fatal(err)
			}
			configured["action_bindings"] = []any{map[string]any{"capability_ref": execution.WASIRunCellCapability().Ref, "binding_ref": binding, "install_lock_ref": lock, "grant": grant, "cell": map[string]any{"environment_id": id, "code_ref": code, "input_ref": input}}}
			if err = api.Decode(api.Raw(configured), &cfg); err != nil {
				t.Fatalf("exact WASI action configuration unavailable: %v", err)
			}
			a, err = OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			defer a.Close()
			active.Store(a)
			goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(brain.GoalSpec{Kind: "answer", Body: "knowledge contract"}), []api.ContentRef{}, []api.ContentRef{})
			if err != nil {
				t.Fatal(err)
			}
			taskID := api.NewID("task")
			knowledgePublicCommand(t, ctx, a, "task.submit", taskID, task.SubmitInput{OrchestratorID: cfg.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}, {Unit: "cpu_seconds", Value: "6"}}, RequirementCandidates: []api.RequirementCandidate{}}, nil)
			for {
				if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300); err != nil {
					t.Fatal(err)
				}
				current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
				if err != nil {
					t.Fatal(err)
				}
				if current.Status == "failed" && !current.AccountingOpen {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatalf("original Task did not settle: %+v", current)
				case <-time.After(20 * time.Millisecond):
				}
			}
			facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
			if err != nil || len(facts.Operations) != 1 || posts.Load() != 3 {
				t.Fatalf("actual original Cell action missing: %v %+v posts=%d", err, facts, posts.Load())
			}
			intent, err := a.Task.ReadOperationIntent(ctx, a.Store, a.Scope, a.UserAuth, facts.Operations[0].Intent.OperationID)
			if err != nil || !api.Equal(intent.CostBound, []api.Amount{{Unit: "cpu_seconds", Value: "3"}}) || !api.Equal(intent.InstallLockRef, lock) {
				t.Fatalf("actual Cell CPU reservation/runtime lock changed: %v %+v", err, intent)
			}
			raw, err = a.query(ctx, "execution.get", intent.OperationID, execution.OperationIDInput{OperationID: intent.OperationID})
			var operation execution.OperationView
			if err != nil || api.Decode(raw, &operation) != nil || operation.Operation.Effect != "applied" || !operation.Operation.UsageFinal || operation.Operation.ResultRef == nil || len(operation.Attempts.Items) != 1 || !operation.ActuallyStopped || operation.Attempts.Items[0].StartedAt == "" {
				t.Fatalf("actual Cell was not committed and joined once: %v %+v", err, operation)
			}
			raw, err = a.query(ctx, "environment.get", id, execution.EnvironmentIDInput{EnvironmentID: id})
			var after execution.Environment
			if err != nil || api.Decode(raw, &after) != nil || after.Generation != 1 || after.NamespaceRevision != 2 || !after.ReadyForCell || !after.ActuallyExited || after.NamespaceRef == nil {
				t.Fatalf("original namespace CAS failed: %v %+v", err, after)
			}
			bytes, err := a.Memory.Read(ctx, a.Scope, a.UserAuth, *after.NamespaceRef, "environment_namespace")
			var namespace execution.PassiveNamespace
			if err != nil || api.Decode(bytes, &namespace) != nil || len(namespace.Bindings) != 1 || namespace.Bindings[0].Name != "answer" || namespace.Bindings[0].Kind != "decimal" || namespace.Bindings[0].Decimal != "42" {
				t.Fatalf("independent actual namespace differs: %v %+v", err, namespace)
			}
			var meter wasi.Receipt
			if len(operation.Operation.EvidenceRefs) != 1 {
				t.Fatalf("original native CPU evidence missing: %+v", operation.Operation)
			}
			bytes, err = a.Memory.Read(ctx, a.Scope, a.UserAuth, operation.Operation.EvidenceRefs[0], "execution_result")
			if err != nil || api.Decode(bytes, &meter) != nil || meter.SpawnCount != 1 || !meter.SpawnCountKnown || !meter.ActuallyExited || !meter.UsageFinal || len(meter.Usage) != 1 || meter.Usage[0].Unit != "cpu_seconds" || !api.Equal(meter.Usage, operation.Operation.Usage) {
				t.Fatalf("native original CPU meter missing: %v %+v", err, meter)
			}
			positive, err := api.CompareDecimal(meter.Usage[0].Value, "0")
			if err != nil || positive <= 0 {
				t.Fatalf("actual worker CPU was not metered: %+v", meter)
			}
			balances := map[string]api.BudgetBalance{}
			for _, amount := range facts.Task.Budget {
				balances[amount.Unit] = amount
			}
			if balances["USD"].Spent != "0.00072" || balances["USD"].Reserved != "0" || balances["cpu_seconds"].Spent != meter.Usage[0].Value || balances["cpu_seconds"].Reserved != "0" || facts.Task.ResultRef != nil {
				t.Fatalf("Task failed/final bill erased actual independent CPU or fee: %+v", facts.Task)
			}
			raw, err = a.query(ctx, "grant.read", grant.GrantID, governance.IDInput{ID: grant.GrantID})
			var grantRecord governance.GrantRecord
			if err != nil || api.Decode(raw, &grantRecord) != nil || !grantRecord.OnceConsumed || !api.Equal(grantRecord.Spent, meter.Usage) || len(grantRecord.Reserved) != 1 || grantRecord.Reserved[0].Value != "0" {
				t.Fatalf("original once CPU Grant was double consumed or not settled: %v %+v", err, grantRecord)
			}
			if err = a.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if err = runtime.Drain(ctx, reopened.Store, reopened.Scope, reopened.Registry, 100); err != nil {
				t.Fatal(err)
			}
			raw, err = reopened.query(ctx, "execution.get", intent.OperationID, execution.OperationIDInput{OperationID: intent.OperationID})
			var recovered execution.OperationView
			if err != nil || api.Decode(raw, &recovered) != nil || recovered.Operation.Effect != operation.Operation.Effect || !api.Equal(recovered.Operation.ResultRef, operation.Operation.ResultRef) || !api.Equal(recovered.Operation.EvidenceRefs, operation.Operation.EvidenceRefs) || !api.Equal(recovered.Operation.Usage, operation.Operation.Usage) || !recovered.Operation.UsageFinal || len(recovered.Attempts.Items) != 1 || recovered.Attempts.Items[0].AttemptID != operation.Attempts.Items[0].AttemptID || recovered.Attempts.Items[0].RequestDigest != operation.Attempts.Items[0].RequestDigest || recovered.Attempts.Items[0].StartedAt != operation.Attempts.Items[0].StartedAt || !recovered.ActuallyStopped || posts.Load() != 3 {
				t.Fatalf("reopen replaced original Cell/Attempt/effect/fee: %v %+v", err, recovered)
			}
			raw, err = reopened.query(ctx, "environment.get", id, execution.EnvironmentIDInput{EnvironmentID: id})
			var originalAfter execution.Environment
			if err != nil || api.Decode(raw, &originalAfter) != nil || originalAfter.Generation != after.Generation || originalAfter.NamespaceRevision != 2 || !api.Equal(originalAfter.NamespaceRef, after.NamespaceRef) {
				t.Fatalf("original recovery replayed the Cell namespace: %v %+v", err, originalAfter)
			}
		})
	}
}

func wasiContractReply(g brain.Generated) []byte {
	return api.Raw(map[string]any{"id": "wasi-contract-original-reply", "choices": []any{map[string]any{"index": 0, "finish_reason": "stop", "message": map[string]any{"role": "assistant", "content": string(api.Raw(struct {
		Draft    brain.Draft              `json:"draft"`
		Contents []brain.GeneratedContent `json:"contents"`
	}{g.Draft, g.Contents}))}}}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120, "prompt_tokens_details": map[string]any{"cached_tokens": 40}}})
}

// 固定字面真值经真实 Preview1 fd_write 输出；没有预填领域执行事实。
func taskWASIOutputModule(text []byte) []byte {
	leb := func(n uint32) []byte {
		out := []byte{}
		for {
			x := byte(n & 127)
			n >>= 7
			if n != 0 {
				x |= 128
			}
			out = append(out, x)
			if n == 0 {
				return out
			}
		}
	}
	sleb := func(n int32) []byte {
		out := []byte{}
		for {
			x := byte(n & 127)
			n >>= 7
			last := n == 0 && x&64 == 0 || n == -1 && x&64 != 0
			if !last {
				x |= 128
			}
			out = append(out, x)
			if last {
				return out
			}
		}
	}
	module := []byte{0, 97, 115, 109, 1, 0, 0, 0}
	section := func(id byte, b []byte) {
		module = append(module, id)
		module = append(module, leb(uint32(len(b)))...)
		module = append(module, b...)
	}
	name := func(s string) []byte { return append(leb(uint32(len(s))), []byte(s)...) }
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
	code = append(code, sleb(int32(len(text)))...)
	code = append(code, 0x36, 2, 0, 0x41, 1, 0x41, 0, 0x41, 1, 0x41, 8, 0x10, 0, 0x1a, 0x0b)
	section(10, append(append([]byte{1}, leb(uint32(len(code)))...), code...))
	data := append([]byte{1, 0, 0x41, 0x80, 0x08, 0x0b}, leb(uint32(len(text)))...)
	section(11, append(data, text...))
	return module
}
