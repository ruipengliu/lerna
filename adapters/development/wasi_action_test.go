package development

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	execadapter "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/wasi"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
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
		t.Run(driver, func(t *testing.T) { runConfiguredWASITask(t, driver, "cell") })
	}
}

func TestConfiguredWASIReportCompletesFromActualNamespaceAndIndependentReadback(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runConfiguredWASITask(t, driver, "report") })
	}
}

func TestConfiguredWASITaskRejectsInsufficientCPUBeforeOriginalOnceUseAndNativeEntry(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runConfiguredWASITask(t, driver, "budget") })
	}
}

func TestConfiguredWASITaskRejectsChangedOriginalCellCASBeforeNativeEntry(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runConfiguredWASITask(t, driver, "arguments") })
	}
}

func TestConfiguredWASITaskCodeWithdrawalBeforeEntryPreservesOriginalOnceAndKnownFees(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runConfiguredWASITask(t, driver, "withdraw_before_entry") })
	}
}

func TestConfiguredWASITaskCodeWithdrawalPreservesOriginalAppliedEffectAndKnownFees(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) { runConfiguredWASITask(t, driver, "withdraw") })
	}
}

func runConfiguredWASITask(t *testing.T, driver, scenario string, observers ...wasiWithdrawalObserver) {
	report := scenario == "report"
	withdrawal := scenario == "withdraw" || scenario == "withdraw_before_entry"
	actualWorker := report || withdrawal
	t.Helper()
	if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
		t.Skip("actual PostgreSQL DSN required")
	}
	observeFor := reportFixtureTimeout
	if report {
		// 原 PG 现场在四分钟观察截止前仍有合法新启动的 verify；此时原
		// Task 五分钟期限尚余约 79 秒。观察覆盖该原期限和费用收尾，
		// 不改变 Task deadline、Control/Use 窗口或据此认定产品成功。
		observeFor = 6 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), observeFor)
	defer cancel()
	root := ""
	if actualWorker {
		root = configuredWASITestDataRoot(t)
	} else {
		root = t.TempDir()
	}
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
	var actionCall atomic.Value
	var sourceClosed atomic.Bool
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
			Goal      string       `json:"goal_utf8"`
			Materials []struct {
				Ref  api.ContentRef `json:"ref"`
				Body string         `json:"body_utf8"`
			} `json:"materials"`
		}
		if err != nil || json.Unmarshal(body, &wire) != nil || len(wire.Messages) != 2 || json.Unmarshal([]byte(wire.Messages[1].Content), &request) != nil {
			t.Error("actual frozen request could not be read")
			return
		}
		if actualWorker {
			t.Logf("original HTTP post=%d purpose=%s task_ref=%s materials=%d", posts.Load(), request.Snapshot.Purpose, api.Raw(request.Snapshot.TaskRef), len(request.Materials))
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
		expectCapability := posts.Load() <= 2 || scenario == "budget"
		if found != expectCapability || !binarySource {
			t.Error("actual Snapshot omitted original WASI pair or binary Source")
			return
		}
		rules := brain.RuleEngine{Goals: factSource{active.Load()}, Facts: factSource{active.Load()}, ArtifactRule: active.Load().ArtifactRule, SavedRule: active.Load().SavedRule, AnswerSchema: active.Load().AnswerSchema, ReadCapability: execadapter.FileReadCapability().Ref, WriteCapability: execadapter.FileWriteCapability().Ref, ReadBinding: active.Load().ReadBinding, WriteBinding: active.Load().WriteBinding}
		ruleReply := func() {
			encoding, err := rules.Encode(r.Context(), request.Snapshot, []byte(request.Goal), active.Load().Profile)
			if err != nil {
				t.Error(err)
				return
			}
			generated, err := rules.Request(r.Context(), r.Header.Get("X-Harness-Call-ID"), encoding)
			if err != nil {
				t.Error(err)
				return
			}
			_, _ = w.Write(wasiContractReply(generated))
		}
		if request.Snapshot.Purpose == "interpret_requirements" {
			if report {
				ruleReply()
			} else {
				_, _ = w.Write(knowledgeRefinementReply(active.Load().ArtifactRule))
			}
			return
		}
		withdrawCode := func() bool {
			one := uint64(1)
			a := active.Load()
			close := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: code.ContentID, Method: "content.close", ExpectedRevision: &one, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(memory.CloseInput{ContentRef: code, Reason: "Withdraw this exact executable Source while preserving original effects and fees."})}
			receipt, err := a.Dispatcher.Command(r.Context(), a.UserAuth, api.Raw(close))
			if err != nil || receipt.Error != nil || receipt.Stage != "applied" {
				t.Errorf("original public Code withdrawal failed: %v %+v", err, receipt)
				return false
			}
			sourceClosed.Store(true)
			return true
		}
		if posts.Load() == 2 {
			actionCall.Store(r.Header.Get("X-Harness-Call-ID"))
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
			if scenario == "arguments" {
				arguments.ExpectedNamespaceRevision++
			}
			// 原 HTTP 输入／准确 Snapshot 已发出，尚未消费此 act 或进入 Cell。
			if scenario == "withdraw_before_entry" && !withdrawCode() {
				return
			}
			generated := brain.Generated{Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "Run exactly the declared finite Cell without asserting its effect.", DisclosedSources: []api.ContentRef{}}, {LocalID: "arguments", MediaType: "application/json", Body: string(api.Raw(arguments)), DisclosedSources: []api.ContentRef{}}}, Draft: brain.Draft{Kind: "act", ReasonLocalID: "reason", Actions: []brain.DraftAction{{LocalKey: "original_cell", CapabilityRef: execution.WASIRunCellCapability().Ref, BindingRef: binding, ArgumentsLocalID: "arguments"}}}}
			_, _ = w.Write(wasiContractReply(generated))
			return
		}
		if scenario == "withdraw" && posts.Load() == 3 && !withdrawCode() {
			return
		}
		if report {
			actualNamespace := false
			for _, material := range request.Materials {
				var ns execution.PassiveNamespace
				if material.Ref.Hash == api.Hash([]byte(material.Body)) && material.Ref.ByteLength == uint64(len(material.Body)) && api.Decode([]byte(material.Body), &ns) == nil && len(ns.Bindings) == 1 && ns.Bindings[0].Name == "answer" && ns.Bindings[0].Kind == "decimal" && ns.Bindings[0].Decimal == "42" {
					actualNamespace = true
				}
			}
			if !actualNamespace {
				// 明确拒绝缺少准确原输出的草稿；不插入成功或任何工具事实。
				_, _ = w.Write(wasiContractReply(brain.Generated{Contents: []brain.GeneratedContent{{LocalID: "reason", MediaType: "text/plain", Body: "The frozen actual Cell namespace was not supplied.", DisclosedSources: []api.ContentRef{}}}, Draft: brain.Draft{Kind: "fail", ReasonLocalID: "reason", ReasonCode: "actual_cell_namespace_missing"}}))
				return
			}
			ruleReply()
			return
		}
		_, _ = w.Write(knowledgeContractReply())
	}))
	defer server.Close()
	t.Setenv("HARNESS_CONTRACT_MODEL_CREDENTIAL", "synthetic-contract-token")
	cfg.Model = contractModelConfig(server.URL)
	if err = SaveConfig(filepath.Join(root, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	if actualWorker {
		// 只保留原测试模型使用过的准确私有引用和值，不向日志输出凭据。
		if err = privateFile(filepath.Join(root, "model-fixture.env"), []byte(cfg.Model.CredentialEnv+"="+os.Getenv(cfg.Model.CredentialEnv)+"\n")); err != nil {
			t.Fatal(err)
		}
	}
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
	if err = SaveConfig(filepath.Join(root, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	a, err = OpenApp(ctx, cfg, true)
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	active.Store(a)
	goalSpec := brain.GoalSpec{Kind: "answer", Body: "knowledge contract"}
	if report {
		goalSpec = brain.GoalSpec{Kind: "report", Title: "Actual WASI", Body: "42", SavePath: "reports/wasi.md"}
	}
	goal, err := a.Publish(ctx, a.Scope, a.UserAuth, api.NewID("content"), "application/json", api.Raw(goalSpec), []api.ContentRef{}, []api.ContentRef{})
	if err != nil {
		t.Fatal(err)
	}
	taskID := api.NewID("task")
	cpuBudget := "6"
	if scenario == "budget" {
		cpuBudget = "2"
	}
	knowledgePublicCommand(t, ctx, a, "task.submit", taskID, task.SubmitInput{OrchestratorID: cfg.OwnerID, GoalRef: goal, PolicyRef: a.TaskPolicy.PolicyRef, Deadline: api.Time(time.Now().Add(5 * time.Minute)), Budget: []api.Amount{{Unit: "USD", Value: "1"}, {Unit: "cpu_seconds", Value: cpuBudget}}, RequirementCandidates: []api.RequirementCandidate{}}, nil)
	stopWorker := func() {}
	workerJoined := false
	var workerDone <-chan error
	if actualWorker {
		// 实际 App worker 沿原 Claim 续租；不把 Drain 的一次 30 秒领取
		// 当作完整报告的生产 worker，也不延长 Task 或开始窗口。
		workerCtx, cancelWorker := context.WithCancel(ctx)
		done := make(chan error, 1)
		workerDone = done
		go func() {
			done <- a.Run(workerCtx, false, true)
			close(done)
		}()
		stopped := false
		stopWorker = func() {
			if stopped {
				return
			}
			stopped = true
			cancelWorker()
			select {
			case runErr := <-done:
				workerJoined = true
				if runErr != nil {
					t.Errorf("actual original App worker did not close: %v", runErr)
				}
			case <-time.After(30 * time.Second):
				t.Error("actual original App worker did not join")
			}
		}
		defer stopWorker()
	}
	cancelledWithdrawal := false
	observedWithdrawal := false
	for {
		if actualWorker {
			select {
			case runErr := <-workerDone:
				debugWASITask(t, a, taskID, driver, scenario, posts.Load())
				t.Fatalf("actual App worker exited before original Task closed: %v", runErr)
			default:
			}
		}
		if !actualWorker {
			err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 300)
		}
		if err != nil {
			if scenario == "arguments" {
				var refusal *api.Error
				wantReason := "original_wasi_cell_arguments_changed"
				if !errors.As(err, &refusal) || refusal.Reason != wantReason {
					t.Fatalf("wrong original Cell refusal: %v", err)
				}
				assertRejectedWASITask(t, ctx, a, cfg, taskID, id, root, grant.GrantID, cpuBudget, 2, "0.00048", &posts)
				return
			}
			if actualWorker {
				debugWASITask(t, a, taskID, driver, scenario, posts.Load())
			}
			t.Fatal(err)
		}
		current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil {
			if actualWorker {
				debugWASITask(t, a, taskID, driver, scenario, posts.Load())
			}
			t.Fatal(err)
		}
		if withdrawal && sourceClosed.Load() && !cancelledWithdrawal {
			wantUSD := "0.00072"
			if scenario == "withdraw_before_entry" {
				wantUSD = "0.00048"
			}
			knownFeesClosed := false
			for _, balance := range current.Budget {
				knownFeesClosed = knownFeesClosed || balance.Unit == "USD" && balance.Spent == wantUSD && balance.Reserved == "0"
			}
			if knownFeesClosed {
				if !observedWithdrawal && len(observers) == 1 && observers[0].BeforeCancellation != nil {
					if err := observers[0].BeforeCancellation(ctx, a, taskID, code); err != nil {
						t.Fatalf("observe original closed Code before cancellation: %v", err)
					}
					observedWithdrawal = true
				}
				// 先观察原已发请求的最终账单与真实当前来源拒绝，再提交控制；
				// 不能靠取消尚未完成的请求制造零 Cell 结论。
				assertWASISourceClosed(t, ctx, a, code, "environment_code")
				cancelCommand := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: api.NewID("command"), TargetID: taskID, ExpectedRevision: &current.Revision, Method: "task.cancel", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.ControlInput{TaskID: taskID, Reason: "Join original Task after exact Code withdrawal while preserving every effect and known fee."})}
				cancelReceipt, cancelErr := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(cancelCommand))
				if cancelErr != nil || cancelReceipt.Error != nil && cancelReceipt.Error.Code != "revision_conflict" {
					t.Fatalf("original Source withdrawal cancellation: %v %+v", cancelErr, cancelReceipt)
				}
				if cancelReceipt.Error == nil {
					cancelledWithdrawal = true
					if len(observers) == 1 && observers[0].AfterCancellation != nil {
						if err := observers[0].AfterCancellation(ctx, a, taskID, code); err != nil {
							t.Fatalf("recover original known CPU invoice after cancellation: %v", err)
						}
					}
				}
			}
		}
		if (current.Status == "failed" || current.Status == "succeeded" || withdrawal && current.Status == "cancelled") && !current.AccountingOpen {
			if scenario == "arguments" {
				t.Fatalf("unconfirmed refusal bypassed the original admission check: %+v", current)
			}
			if scenario == "budget" {
				assertWASIBudgetConsumption(t, ctx, a, taskID, actionCall.Load().(string))
				assertRejectedWASITask(t, ctx, a, cfg, taskID, id, root, grant.GrantID, cpuBudget, 3, "0.00072", &posts)
				return
			}
			if report && current.Status != "succeeded" {
				debugWASITask(t, a, taskID, driver, scenario, posts.Load())
				t.Fatalf("actual namespace must lead through native save/readback to verified Result, got %+v", current)
			}
			if report {
				// 成功 Result 与其原正文出版责任分别持久化。保持实际 Worker
				// 运行到该责任完成，再 cancel/join；不因已成功而中断出版。
				publication, publicationErr := a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, taskID, task.ResultInput{})
				if publicationErr != nil {
					t.Fatalf("observe original successful Result publication: %v", publicationErr)
				}
				if publication.Publication != "published" || publication.ContentRef == nil {
					select {
					case <-ctx.Done():
						t.Fatalf("original successful Result body did not publish: %v", ctx.Err())
					case <-time.After(250 * time.Millisecond):
					}
					continue
				}
			}
			break
		}
		select {
		case <-ctx.Done():
			if withdrawal {
				debugWASITask(t, a, taskID, driver, scenario, posts.Load())
			}
			t.Fatalf("original Task did not settle: %+v", current)
		case <-time.After(250 * time.Millisecond):
		}
	}
	stopWorker()
	if actualWorker && !workerJoined {
		t.Fatal("original Task/fee closure cannot bypass actual App worker join")
	}
	if scenario == "withdraw_before_entry" {
		assertPreEntryWithdrawnWASITask(t, ctx, a, cfg, taskID, id, root, grant.GrantID, code, env, &posts)
		return
	}
	wantOperations, wantPosts, wantUSD := 1, int32(3), "0.00072"
	if report {
		wantOperations, wantPosts, wantUSD = 4, 6, "0.00144"
	}
	facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
	if err != nil || len(facts.Operations) != wantOperations || posts.Load() != wantPosts {
		t.Fatalf("actual original Cell action missing: %v %+v posts=%d", err, facts, posts.Load())
	}
	cellOperationID := ""
	for _, fact := range facts.Operations {
		if api.Equal(fact.Intent.CapabilityRef, execution.WASIRunCellCapability().Ref) {
			cellOperationID = fact.Intent.OperationID
		}
	}
	intent, err := a.Task.ReadOperationIntent(ctx, a.Store, a.Scope, a.UserAuth, cellOperationID)
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
	if scenario == "withdraw" {
		assertWithdrawnWASITask(t, ctx, a, cfg, facts.Task, operation, after, grant.GrantID, code, root, &posts)
		return
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
	if balances["USD"].Spent != wantUSD || balances["USD"].Reserved != "0" || balances["cpu_seconds"].Spent != meter.Usage[0].Value || balances["cpu_seconds"].Reserved != "0" || !report && facts.Task.ResultRef != nil {
		t.Fatalf("Task failed/final bill erased actual independent CPU or fee: %+v", facts.Task)
	}
	raw, err = a.query(ctx, "grant.read", grant.GrantID, governance.IDInput{ID: grant.GrantID})
	var grantRecord governance.GrantRecord
	if err != nil || api.Decode(raw, &grantRecord) != nil || !grantRecord.OnceConsumed || !api.Equal(grantRecord.Spent, meter.Usage) || len(grantRecord.Reserved) != 1 || grantRecord.Reserved[0].Value != "0" {
		t.Fatalf("original once CPU Grant was double consumed or not settled: %v %+v", err, grantRecord)
	}
	var originalResult task.ResultOutput
	originalFileHash := ""
	if report {
		actualFile, err := os.ReadFile(filepath.Join(root, "files", goalSpec.SavePath))
		if err != nil || string(actualFile) != "# Actual WASI\n\n42\n" {
			t.Fatalf("independent native report differs from literal truth: %v %q", err, actualFile)
		}
		originalFileHash = api.Hash(actualFile)
		for _, fact := range facts.Operations {
			if !fact.Fact.Closed || fact.Fact.Effect == "unknown" || fact.Fact.Effect == "not_started" || fact.Fact.MayApplyLater {
				t.Fatalf("Result claims an open or unstarted effect: %+v", fact)
			}
		}
		originalResult, err = a.Task.Result(ctx, a.Store, a.Scope, a.UserAuth, taskID, task.ResultInput{})
		if err != nil || originalResult.Result.TaskID != taskID || originalResult.Result.CompletionBasis != "verified" || len(originalResult.Result.ConditionResults) != 2 || len(facts.Checks) != 2 || originalResult.Publication != "published" || originalResult.ContentRef == nil {
			t.Fatalf("independent saved readback has no verified immutable Result: %v %+v", err, originalResult)
		}
		completedAt, completeErr := api.ParseTime(originalResult.Result.CompletedAt)
		deadline, deadlineErr := api.ParseTime(facts.Task.Deadline)
		if completeErr != nil || deadlineErr != nil || !completedAt.Before(deadline) {
			t.Fatalf("verified Result must complete within the original Task deadline: completed=%s deadline=%s", originalResult.Result.CompletedAt, facts.Task.Deadline)
		}
		published, err := a.Memory.Read(ctx, a.Scope, a.UserAuth, *originalResult.ContentRef, "task.result")
		var result api.Result
		if err != nil || api.Decode(published, &result) != nil || !api.Equal(result, originalResult.Result) {
			t.Fatalf("published Result differs from verified original: %v %+v", err, result)
		}
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
	if err != nil || api.Decode(raw, &recovered) != nil || recovered.Operation.Effect != operation.Operation.Effect || !api.Equal(recovered.Operation.ResultRef, operation.Operation.ResultRef) || !api.Equal(recovered.Operation.EvidenceRefs, operation.Operation.EvidenceRefs) || !api.Equal(recovered.Operation.Usage, operation.Operation.Usage) || !recovered.Operation.UsageFinal || len(recovered.Attempts.Items) != 1 || recovered.Attempts.Items[0].AttemptID != operation.Attempts.Items[0].AttemptID || recovered.Attempts.Items[0].RequestDigest != operation.Attempts.Items[0].RequestDigest || recovered.Attempts.Items[0].StartedAt != operation.Attempts.Items[0].StartedAt || !recovered.ActuallyStopped || posts.Load() != wantPosts {
		t.Fatalf("reopen replaced original Cell/Attempt/effect/fee: %v %+v", err, recovered)
	}
	raw, err = reopened.query(ctx, "environment.get", id, execution.EnvironmentIDInput{EnvironmentID: id})
	if report {
		recoveredResult, err := reopened.Task.Result(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, taskID, task.ResultInput{})
		if err != nil || !api.Equal(recoveredResult, originalResult) {
			t.Fatalf("reopen replaced immutable Task Result: %v %+v", err, recoveredResult)
		}
	}
	var originalAfter execution.Environment
	if err != nil || api.Decode(raw, &originalAfter) != nil || originalAfter.Generation != after.Generation || originalAfter.NamespaceRevision != 2 || !api.Equal(originalAfter.NamespaceRef, after.NamespaceRef) {
		t.Fatalf("original recovery replayed the Cell namespace: %v %+v", err, originalAfter)
	}
	t.Logf("original WASI verification=%s", api.Raw(struct {
		Scenario          string                `json:"scenario"`
		Database          string                `json:"database"`
		TaskRef           api.ObjectRef         `json:"task_ref"`
		TaskStatus        string                `json:"task_status"`
		TaskDeadline      string                `json:"task_deadline"`
		ResultCompletedAt string                `json:"result_completed_at,omitempty"`
		AccountingOpen    bool                  `json:"accounting_open"`
		CellOperationID   string                `json:"cell_operation_id"`
		CellAttemptID     string                `json:"cell_attempt_id"`
		NativeSpawnCount  uint64                `json:"native_spawn_count"`
		ActualCPU         []api.Amount          `json:"actual_cpu"`
		ModelUSD          string                `json:"model_usd"`
		ActualPosts       int32                 `json:"actual_posts"`
		NamespaceRef      *api.ContentRef       `json:"namespace_ref"`
		InstallLockRef    api.ComponentRef      `json:"install_lock_ref"`
		NativeReportHash  string                `json:"native_report_hash,omitempty"`
		VerifiedResultRef *api.ObjectRef        `json:"verified_result_ref,omitempty"`
		ConditionChecks   []api.ConditionResult `json:"condition_checks"`
		DatabaseReopened  bool                  `json:"database_reopened"`
		ActualAppWorker   bool                  `json:"actual_app_worker"`
		AppWorkerJoined   bool                  `json:"app_worker_joined"`
	}{scenario, driver, a.Scope.Ref(taskID, facts.Task.Revision), facts.Task.Status, facts.Task.Deadline, originalResult.Result.CompletedAt, facts.Task.AccountingOpen, intent.OperationID, operation.Attempts.Items[0].AttemptID, meter.SpawnCount, meter.Usage, balances["USD"].Spent, posts.Load(), after.NamespaceRef, lock, originalFileHash, facts.Task.ResultRef, facts.Checks, true, actualWorker, workerJoined}))
}

func assertWithdrawnWASITask(t *testing.T, ctx context.Context, a *App, cfg Config, current api.Task, operation execution.OperationView, env execution.Environment, grantID string, code api.ContentRef, root string, posts *atomic.Int32) {
	t.Helper()
	if current.Status != "cancelled" || current.AccountingOpen || current.ResultRef != nil || posts.Load() != 3 {
		t.Fatalf("Source withdrawal claimed success or dropped original calls: %+v posts=%d", current, posts.Load())
	}
	balances := map[string]api.BudgetBalance{}
	for _, value := range current.Budget {
		balances[value.Unit] = value
	}
	if len(operation.Operation.Usage) != 1 || balances["USD"].Spent != "0.00072" || balances["USD"].Reserved != "0" || balances["cpu_seconds"].Spent != operation.Operation.Usage[0].Value || balances["cpu_seconds"].Reserved != "0" {
		t.Fatalf("Source withdrawal lost known original fees: %+v %+v", balances, operation.Operation)
	}
	assertWASISourceClosed(t, ctx, a, code, "environment_code")
	assertWASISourceClosed(t, ctx, a, *env.NamespaceRef, "environment_namespace")
	journalHash := assertOriginalWASIJournal(t, root, operation)
	raw, err := a.query(ctx, "grant.read", grantID, governance.IDInput{ID: grantID})
	var grant governance.GrantRecord
	if err != nil || api.Decode(raw, &grant) != nil || !grant.OnceConsumed || !api.Equal(grant.Spent, operation.Operation.Usage) || len(grant.Reserved) != 1 || grant.Reserved[0].Value != "0" {
		t.Fatalf("withdrawal revived the once Grant or erased original CPU: %v %+v", err, grant)
	}
	if err = a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	exerciseReopenedWASIWorker(t, ctx, reopened)
	after, err := reopened.Task.Read(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, current.TaskID)
	if err != nil || after.Status != "cancelled" || after.AccountingOpen || !api.Equal(after.Budget, current.Budget) || posts.Load() != 3 {
		t.Fatalf("reopen lost original withdrawn Source fees: %v %+v", err, after)
	}
	raw, err = reopened.query(ctx, "execution.get", operation.Operation.OperationID, execution.OperationIDInput{OperationID: operation.Operation.OperationID})
	var original execution.OperationView
	if err != nil || api.Decode(raw, &original) != nil || original.Operation.Effect != "applied" || !api.Equal(original.Operation.ResultRef, operation.Operation.ResultRef) || !api.Equal(original.Operation.Usage, operation.Operation.Usage) || !original.Operation.UsageFinal || len(original.Attempts.Items) != 1 || original.Attempts.Items[0].AttemptID != operation.Attempts.Items[0].AttemptID || original.Attempts.Items[0].StartedAt != operation.Attempts.Items[0].StartedAt || !original.ActuallyStopped {
		t.Fatalf("reopen replayed or erased original withdrawn Cell: %v %+v", err, original)
	}
	raw, err = reopened.query(ctx, "environment.get", env.EnvironmentID, execution.EnvironmentIDInput{EnvironmentID: env.EnvironmentID})
	var afterEnv execution.Environment
	if err != nil || api.Decode(raw, &afterEnv) != nil || afterEnv.Generation != env.Generation || afterEnv.NamespaceRevision != 2 || !api.Equal(afterEnv.NamespaceRef, env.NamespaceRef) || !afterEnv.ActuallyExited || !afterEnv.ReadyForCell || len(afterEnv.ActiveOperationIDs) != 0 {
		t.Fatalf("withdrawal/reopen replaced the original committed namespace: %v %+v", err, afterEnv)
	}
	assertWASISourceClosed(t, ctx, reopened, code, "environment_code")
	assertWASISourceClosed(t, ctx, reopened, *afterEnv.NamespaceRef, "environment_namespace")
	if afterHash := assertOriginalWASIJournal(t, root, original); afterHash != journalHash {
		t.Fatalf("reopen changed the original native Attempt journal: %s => %s", journalHash, afterHash)
	}
	logWithdrawnWASITask(t, reopened, after, afterEnv, code, "applied", original.Operation.OperationID, original.Attempts.Items[0].AttemptID, journalHash, 1, posts.Load())
}

func assertPreEntryWithdrawnWASITask(t *testing.T, ctx context.Context, a *App, cfg Config, taskID, envID, root, grantID string, code api.ContentRef, before execution.Environment, posts *atomic.Int32) {
	t.Helper()
	facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
	if err != nil || facts.Task.Status != "cancelled" || facts.Task.AccountingOpen || facts.Task.ResultRef != nil || len(facts.Operations) != 0 || posts.Load() != 2 {
		t.Fatalf("pre-entry Source withdrawal created an effect or lost original calls: %v %+v posts=%d", err, facts, posts.Load())
	}
	balances := map[string]api.BudgetBalance{}
	for _, value := range facts.Task.Budget {
		balances[value.Unit] = value
	}
	if balances["USD"].Spent != "0.00048" || balances["USD"].Reserved != "0" || balances["cpu_seconds"].Limit != "6" || balances["cpu_seconds"].Spent != "0" || balances["cpu_seconds"].Reserved != "0" {
		t.Fatalf("pre-entry Source refusal erased known fees or reserved Cell CPU: %+v", balances)
	}
	check := func(host *App) execution.Environment {
		assertWASISourceClosed(t, ctx, host, code, "environment_code")
		raw, err := host.query(ctx, "environment.get", envID, execution.EnvironmentIDInput{EnvironmentID: envID})
		var env execution.Environment
		if err != nil || api.Decode(raw, &env) != nil || env.Generation != before.Generation || env.NamespaceRevision != 1 || !api.Equal(env.NamespaceRef, before.NamespaceRef) || !env.ReadyForCell || !env.ActuallyExited || len(env.ActiveOperationIDs) != 0 {
			t.Fatalf("pre-entry withdrawal entered or replaced the original Environment: %v %+v", err, env)
		}
		raw, err = host.query(ctx, "grant.read", grantID, governance.IDInput{ID: grantID})
		var grant governance.GrantRecord
		if err != nil || api.Decode(raw, &grant) != nil || grant.OnceConsumed || len(grant.Spent) != 0 || len(grant.Reserved) != 0 {
			t.Fatalf("pre-entry withdrawal consumed/reserved original once Grant: %v %+v", err, grant)
		}
		entries, err := os.ReadDir(filepath.Join(root, "wasi"))
		if err != nil || len(entries) > 10 {
			t.Fatalf("read bounded original native journals: %v", err)
		}
		for _, entry := range entries {
			if strings.HasPrefix(entry.Name(), "attempt_") {
				t.Fatalf("withdrawn code created a native Attempt: %s", entry.Name())
			}
		}
		return env
	}
	check(a)
	if err := a.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenApp(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	exerciseReopenedWASIWorker(t, ctx, reopened)
	after, err := reopened.Task.ContextFacts(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, taskID)
	if err != nil || after.Task.Status != "cancelled" || after.Task.AccountingOpen || after.Task.ResultRef != nil || !api.Equal(after.Task.Budget, facts.Task.Budget) || len(after.Operations) != 0 || posts.Load() != 2 {
		t.Fatalf("pre-entry withdrawal/reopen made another effect or lost fees: %v %+v posts=%d", err, after, posts.Load())
	}
	logWithdrawnWASITask(t, reopened, after.Task, check(reopened), code, "before_entry", "", "", "", 0, posts.Load())
}

func assertWASISourceClosed(t *testing.T, ctx context.Context, a *App, ref api.ContentRef, purpose string) {
	t.Helper()
	_, err := a.Memory.Read(ctx, a.Scope, a.UserAuth, ref, purpose)
	var refusal *api.Error
	if !errors.As(err, &refusal) || refusal.Code != "forbidden" || refusal.Reason != "source_closed" {
		t.Fatalf("exact original Content requires current Source refusal: %v", err)
	}
}

// 独立 native 日志是真实目标真值；来源撤回后不绕 Content 门禁读回正文。
func assertOriginalWASIJournal(t *testing.T, root string, operation execution.OperationView) string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(root, "wasi"))
	if err != nil || len(entries) > 10 {
		t.Fatalf("read bounded native Attempt journals: %v", err)
	}
	wantFile := operation.Attempts.Items[0].AttemptID + ".json"
	nativeAttempts := 0
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "attempt_") {
			nativeAttempts++
			if entry.Name() != wantFile {
				t.Fatalf("withdrawn Source created another native Attempt: %s", entry.Name())
			}
		}
	}
	if nativeAttempts != 1 {
		t.Fatalf("original applied Cell must retain one native Attempt, got %d", nativeAttempts)
	}
	path := filepath.Join(root, "wasi", operation.Attempts.Items[0].AttemptID+".json")
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	bytes, readErr := io.ReadAll(io.LimitReader(f, api.MaxJSONBytes+1))
	closeErr := f.Close()
	var journal struct {
		AttemptID       string       `json:"attempt_id"`
		Phase           string       `json:"phase"`
		SpawnCount      uint64       `json:"spawn_count"`
		SpawnCountKnown bool         `json:"spawn_count_known"`
		Usage           []api.Amount `json:"usage"`
		UsageFinal      bool         `json:"usage_final"`
		Result          struct {
			Success bool   `json:"success"`
			Output  []byte `json:"output"`
		} `json:"result"`
	}
	if readErr != nil || closeErr != nil || len(bytes) > api.MaxJSONBytes || json.Unmarshal(bytes, &journal) != nil || journal.AttemptID != operation.Attempts.Items[0].AttemptID || journal.Phase != "finished" || journal.SpawnCount != 1 || !journal.SpawnCountKnown || !journal.Result.Success || !journal.UsageFinal || !api.Equal(journal.Usage, operation.Operation.Usage) {
		t.Fatalf("original native journal lost its one actual execution/fee: %v %v", readErr, closeErr)
	}
	if len(journal.Usage) != 1 || journal.Usage[0].Unit != "cpu_seconds" {
		t.Fatalf("original native CPU invoice missing: %+v", journal.Usage)
	}
	positive, err := api.CompareDecimal(journal.Usage[0].Value, "0")
	if err != nil || positive <= 0 {
		t.Fatalf("original native CPU must be measured positive usage: %+v", journal.Usage)
	}
	var ns execution.PassiveNamespace
	if api.Decode(journal.Result.Output, &ns) != nil || len(ns.Bindings) != 1 || ns.Bindings[0].Name != "answer" || ns.Bindings[0].Kind != "decimal" || ns.Bindings[0].Decimal != "42" {
		t.Fatal("original native truth lost the complete committed namespace")
	}
	return api.Hash(bytes)
}

func exerciseReopenedWASIWorker(t *testing.T, ctx context.Context, a *App) {
	t.Helper()
	workerCtx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- a.Run(workerCtx, false, true) }()
	select {
	case err := <-done:
		cancel()
		t.Fatalf("reopened original App worker exited before explicit join: %v", err)
	case <-ctx.Done():
	case <-time.After(time.Second):
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("reopened original App worker failed to join: %v", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("reopened original App worker did not actually join")
	}
}

func logWithdrawnWASITask(t *testing.T, a *App, current api.Task, env execution.Environment, code api.ContentRef, stage, op, attempt, journalHash string, spawns uint64, posts int32) {
	t.Helper()
	t.Logf("original WASI Source withdrawal verification=%s", api.Raw(struct {
		Stage             string              `json:"stage"`
		TaskRef           api.ObjectRef       `json:"task_ref"`
		Status            string              `json:"status"`
		AccountingOpen    bool                `json:"accounting_open"`
		Budget            []api.BudgetBalance `json:"budget"`
		CodeRef           api.ContentRef      `json:"code_ref"`
		NamespaceRef      *api.ContentRef     `json:"namespace_ref"`
		Generation        uint64              `json:"generation"`
		NamespaceRevision uint64              `json:"namespace_revision"`
		CellOperationID   string              `json:"cell_operation_id,omitempty"`
		CellAttemptID     string              `json:"cell_attempt_id,omitempty"`
		NativeJournalHash string              `json:"native_journal_hash,omitempty"`
		NativeSpawnCount  uint64              `json:"native_spawn_count"`
		ActualPosts       int32               `json:"actual_posts"`
		WorkerJoined      bool                `json:"worker_joined"`
		DatabaseReopened  bool                `json:"database_reopened"`
	}{stage, a.Scope.Ref(current.TaskID, current.Revision), current.Status, current.AccountingOpen, current.Budget, code, env.NamespaceRef, env.Generation, env.NamespaceRevision, op, attempt, journalHash, spawns, posts, true, true}))
}

// 重读并幂等消费生产桥已保存的原提案，只核对真实拒绝依据，不制造另一行动。
func assertWASIBudgetConsumption(t *testing.T, ctx context.Context, a *App, taskID, originalCallID string) {
	t.Helper()
	raw, err := a.query(ctx, "budget.read", taskID, task.BudgetReadInput{TaskID: taskID})
	var budget task.BudgetReadResponse
	if err != nil || api.Decode(raw, &budget) != nil || budget.Task == nil {
		t.Fatalf("original model reservations missing: %v", err)
	}
	for _, reservation := range budget.Task.Reservations {
		if reservation.SourceKind != "brain_decision" {
			continue
		}
		view, err := a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, reservation.SourceRef.ObjectID)
		if err != nil {
			t.Fatal(err)
		}
		if view.CallID != originalCallID {
			continue
		}
		intent := api.DecisionDispatchIntent{DecisionID: view.Decision.DecisionID, BrainOwnerID: a.Scope.OwnerID, TaskRef: view.TaskRef, SnapshotRef: view.SnapshotRef, SnapshotRevision: view.Decision.SnapshotRevision}
		proposal, err := (brainBridge{a}).ReadProposal(ctx, a.Scope, intent)
		if err != nil {
			t.Fatal(err)
		}
		var outcome task.Consumption
		status, err := a.Store.Within(ctx, a.Scope, []string{"task", "platform", "content", "memory", "governance"}, func(tx runtime.Tx) error {
			var err error
			outcome, err = a.Task.ConsumeProposalTx(ctx, tx, a.ServiceAuth, proposal, nil)
			return err
		})
		if err != nil || status != runtime.Committed || outcome.Outcome != "rejected" || len(outcome.AdmittedOperationIDs) != 0 || len(outcome.ReasonCodes) != 1 || outcome.ReasonCodes[0] != "invalid_state: budget_unavailable" {
			t.Fatalf("original CPU budget did not reject the actual original proposal: %v %s %+v", err, status, outcome)
		}
		return
	}
	t.Fatal("original physical action call has no Task reservation")
}

func assertRejectedWASITask(t *testing.T, ctx context.Context, a *App, cfg Config, taskID, envID, root, grantID, cpuBudget string, wantPosts int32, wantUSD string, posts *atomic.Int32) {
	t.Helper()
	current, err := a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
	if err != nil || posts.Load() != wantPosts || current.ResultRef != nil {
		t.Fatalf("refusal changed the original model calls or claimed a Result: %v %+v posts=%d", err, current, posts.Load())
	}
	wantStatus := current.Status
	if wantStatus != "failed" {
		wantStatus = "cancelled"
		knowledgePublicCommand(t, ctx, a, "task.cancel", taskID, task.ControlInput{TaskID: taskID, Reason: "Close the original rejected Cell while preserving every actual model fee."}, &current.Revision)
	}
	for {
		if err = runtime.Drain(ctx, a.Store, a.Scope, a.Registry, 200); err != nil {
			t.Fatal(err)
		}
		current, err = a.Task.Read(ctx, a.Store, a.Scope, a.UserAuth, taskID)
		if err != nil {
			t.Fatal(err)
		}
		if current.Status == wantStatus && !current.AccountingOpen {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatalf("original refused Task did not settle: %+v", current)
		case <-time.After(20 * time.Millisecond):
		}
	}
	facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
	if err != nil || len(facts.Operations) != 0 || len(facts.Task.Budget) != 2 || posts.Load() != wantPosts || facts.Task.ResultRef != nil {
		t.Fatalf("refusal created an Operation or another model call: %v %+v", err, facts)
	}
	balances := map[string]api.BudgetBalance{}
	for _, value := range facts.Task.Budget {
		balances[value.Unit] = value
	}
	if balances["USD"].Spent != wantUSD || balances["USD"].Reserved != "0" || balances["cpu_seconds"].Limit != cpuBudget || balances["cpu_seconds"].Spent != "0" || balances["cpu_seconds"].Reserved != "0" {
		t.Fatalf("original refusal erased known fees or reserved guest CPU: %+v", balances)
	}
	raw, err := a.query(ctx, "environment.get", envID, execution.EnvironmentIDInput{EnvironmentID: envID})
	var env execution.Environment
	if err != nil || api.Decode(raw, &env) != nil || env.Generation != 1 || env.NamespaceRevision != 1 || !env.ReadyForCell || !env.ActuallyExited || len(env.ActiveOperationIDs) != 0 {
		t.Fatalf("refused Cell entered the original Environment: %v %+v", err, env)
	}
	raw, err = a.query(ctx, "grant.read", grantID, governance.IDInput{ID: grantID})
	var grant governance.GrantRecord
	if err != nil || api.Decode(raw, &grant) != nil || grant.OnceConsumed || len(grant.Spent) != 0 || len(grant.Reserved) != 0 {
		t.Fatalf("refusal consumed or reserved the original once Grant: %v %+v", err, grant)
	}
	entries, err := os.ReadDir(filepath.Join(root, "wasi"))
	if err != nil || len(entries) > 10 {
		t.Fatalf("original native journal cannot be inspected: %v", err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "attempt_") {
			t.Fatalf("refused Cell left a physical original Attempt journal: %s", entry.Name())
		}
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
	after, err := reopened.Task.Read(ctx, reopened.Store, reopened.Scope, reopened.UserAuth, taskID)
	if err != nil || after.Status != wantStatus || after.AccountingOpen || !api.Equal(after.Budget, current.Budget) || posts.Load() != wantPosts {
		t.Fatalf("reopen lost the original refusal/fees or started another call: %v %+v", err, after)
	}
}

func debugWASITask(t *testing.T, a *App, taskID, driver, scenario string, posts int32) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	facts, err := a.Task.ContextFacts(ctx, a.Store, a.Scope, a.UserAuth, taskID)
	t.Logf("original public WASI Task posts=%d error=%v facts=%s", posts, err, api.Raw(facts))
	budgetBytes, err := a.query(ctx, "budget.read", taskID, task.BudgetReadInput{TaskID: taskID})
	var budget task.BudgetReadResponse
	if err != nil || api.Decode(budgetBytes, &budget) != nil || budget.Task == nil {
		t.Logf("original budget diagnostic: %v", err)
		return
	}
	views := []brain.View{}
	for _, reservation := range budget.Task.Reservations {
		if reservation.SourceKind != "brain_decision" {
			continue
		}
		view, err := a.Brain.Get(ctx, a.Store, a.Scope, a.ServiceAuth, reservation.SourceRef.ObjectID)
		t.Logf("original Brain ref=%s error=%v view=%s", api.Raw(reservation.SourceRef), err, api.Raw(view))
		if err == nil {
			views = append(views, view)
		}
	}
	evidenceRoot, err := configuredWASITestEvidenceRoot()
	if err != nil {
		t.Logf("skip optional original diagnostic artifact: %v", err)
		return
	}
	if evidenceRoot == "" {
		return
	}
	artifact, err := json.Marshal(struct {
		TaskFacts task.ContextFacts       `json:"task_facts"`
		Budget    task.BudgetReadResponse `json:"budget"`
		Decisions []brain.View            `json:"decisions"`
		Posts     int32                   `json:"actual_posts"`
	}{facts, budget, views, posts})
	if err == nil {
		path := filepath.Join(evidenceRoot, "wasi15-"+scenario+"-"+driver+"-"+taskID+"-public-failure.json")
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if err != nil {
			t.Logf("preserve original diagnostic artifact, create failed: %v", err)
			return
		}
		n, writeErr := file.Write(artifact)
		closeErr := file.Close()
		t.Logf("original public diagnostic artifact=%s bytes=%d write_error=%v close_error=%v", path, n, writeErr, closeErr)
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
