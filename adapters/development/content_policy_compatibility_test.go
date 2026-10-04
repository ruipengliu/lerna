package development

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/runtime"
)

// 旧宿主仅经显式管理入口安装原政策，关闭 setup 后由实际重开宿主消费。
func TestReopenedLegacyContentPolicyAdmitsOriginalPublicUpload(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cfg, policy := prepareLegacyPolicyConfiguration(t, ctx, driver)
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatalf("reopen original legacy configuration: %v", err)
			}
			defer func() {
				if closeErr := reopened.Close(); closeErr != nil {
					t.Errorf("close reopened policy host: %v", closeErr)
				}
			}()
			command := originalPolicyUploadCommand(reopened.Scope, reopened.ContentPolicy.PolicyRef)
			actual, err := reopened.Dispatcher.Command(ctx, reopened.UserAuth, api.Raw(command))
			if err != nil || actual.Stage != "applied" || actual.Error != nil {
				t.Fatalf("reopened original public upload was not admitted: %v %+v", err, actual)
			}
			if !api.Equal(reopened.ContentPolicy, policy) {
				t.Fatalf("reopen substituted original registered policy or permissions: %+v", reopened.ContentPolicy.PolicyRef)
			}
			replay, err := reopened.Dispatcher.Command(ctx, reopened.UserAuth, api.Raw(command))
			if err != nil || !api.Equal(actual, replay) {
				t.Fatalf("original upload replay changed receipt: %v %+v", err, replay)
			}
			var out memory.ReserveOutput
			if err = api.Decode(actual.Output, &out); err != nil || out.ContentRef != commandContentRef(command) {
				t.Fatalf("original upload output changed exact content: %v", err)
			}
		})
	}
}

func originalPolicyUploadCommand(scope runtime.Scope, policy api.ComponentRef) api.Command {
	body := []byte("original public policy compatibility upload")
	content := api.ContentRef{TenantID: scope.TenantID, OwnerID: scope.OwnerID, ContentID: api.NewID("upload"), Version: 1, Hash: api.Hash(body), ByteLength: uint64(len(body)), MediaType: "text/plain"}
	input := memory.ReserveInput{TransferID: api.NewID("transfer"), ContentRef: content, PolicyRef: policy, ProcessedSources: []api.ContentRef{}, RetentionUntil: api.Time(time.Now().Add(time.Hour)), TransferDeadline: api.Time(time.Now().Add(time.Minute))}
	return api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: api.NewID("command"), TargetID: content.ContentID, Method: "content.upload_reserve", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(input)}
}

func commandContentRef(command api.Command) api.ContentRef {
	var input memory.ReserveInput
	_ = api.Decode(command.Payload, &input)
	return input.ContentRef
}

// 冻结升级前实际公开宿主安装的用途，独立于当前政策构造器。
func legacyRegisteredContentPurposes() []string {
	return []string{
		"read",
		"preview",
		"content.read",
		"content.write",
		"task.goal",
		"task.context",
		"task.result",
		"task.submit",
		"task.snapshot",
		"task.dispatch",
		"task.complete",
		"task.evidence",
		"task.input",
		"task.accept_result",
		"task.revise",
		"task.steer",
		"task.action",
		"task.attach_evidence",
		"task.adjust_budget",
		"task.need_context",
		"task.delegate",
		"child.create",
		"child.new_goal",
		"child.continue",
		"billing.adjustment",
		"brain.input",
		"brain.output",
		"result",
		"memory.save",
		"memory.read",
		"memory.query",
		"memory.extract",
		"memory.sync",
		"memory.view",
		"managed_file_write",
		"managed_file_read",
		"execution.intent",
		"execution.arguments",
		"execution.output",
		"execution.control",
		"execution_intent",
		"execution_arguments",
		"execution_result",
		"execution_usage_proof",
		"environment_namespace",
		"environment_input",
		"environment_compute_spec",
		"environment_restore",
		"interaction.input",
		"interaction.history",
		"interaction.surface",
		"schedule.template",
		"confirmation.preview",
		"evaluation.manifest",
		"interaction.snapshot",
		"interaction.preview",
		"extension.prepare.artifact",
		"evaluation.runner.input",
		"evaluation.runner.truth",
		"evaluation.runner.prepared_current",
		"evaluation.runner.start_current",
	}
}

func prepareLegacyPolicyConfiguration(t *testing.T, ctx context.Context, driver string) (Config, memory.Policy) {
	t.Helper()
	root := t.TempDir()
	cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
	if err != nil {
		t.Fatal(err)
	}
	cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
	if err = SaveConfig(filepath.Join(root, "config.json"), cfg); err != nil {
		t.Fatal(err)
	}
	store, err := OpenStore(ctx, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	storeClosed := false
	t.Cleanup(func() {
		if !storeClosed {
			if closeErr := store.Close(); closeErr != nil {
				t.Errorf("close original policy setup store: %v", closeErr)
			}
		}
	})
	scope := runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: store.ID()}
	user := runtime.Auth{TenantID: cfg.TenantID, SubjectID: cfg.SubjectID, CredentialGeneration: 1, Roles: cfg.UserRoles}
	service := runtime.Auth{TenantID: cfg.TenantID, SubjectID: cfg.OwnerID, CredentialGeneration: 1, Roles: []string{"service", "orchestrator", "task_admin", "evidence", "grant_authority", "content_admin", "memory_admin", "usage_reporter", "maintainer", "evidence_consumer"}}
	token, err := os.ReadFile(cfg.TokenFile)
	if err != nil {
		t.Fatal(err)
	}
	identity := &platform.DevIdentity{Store: store, OwnerID: cfg.OwnerID, SessionTTL: 8 * time.Hour, Principals: []platform.Principal{{Auth: user, TokenHash: api.Hash([]byte(strings.TrimSpace(string(token))))}, {Auth: service, TokenHash: api.Hash([]byte("explicit legacy setup service"))}}}
	if err = identity.Initialize(ctx); err != nil {
		t.Fatalf("explicit original identity setup: %v", err)
	}
	objects, err := objectstore.OpenLocal(filepath.Join(root, "objects"), memory.MaxContentBytes)
	if err != nil {
		t.Fatal(err)
	}
	mem := memory.New(store, objects)
	if err = mem.ConfigureParticipants("platform"); err != nil {
		t.Fatal(err)
	}
	mem.Authorization = contentAuthority{&App{Config: cfg}}
	registry := runtime.NewRegistry()
	mem.Register(registry)
	management := &runtime.Dispatcher{Store: store, OwnerID: cfg.OwnerID, Registry: registry}
	values := memory.PolicyValues{Subjects: []string{cfg.SubjectID, cfg.OwnerID}, Purposes: legacyRegisteredContentPurposes(), Locations: []string{"cloud", "device"}, RetainUntil: cfg.PolicyExpiresAt, Continuous: true}
	ref := api.ComponentRef{ComponentID: platform.StableDevelopmentID("component", "content-policy"), Version: "1.0.0"}
	ref.Digest, err = api.Digest(values)
	if err != nil {
		t.Fatal(err)
	}
	policy, err := memory.NewPolicy(ref, values)
	if err != nil {
		t.Fatal(err)
	}
	install := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: api.NewID("command"), TargetID: ref.ComponentID, Method: "content.policy.install", ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(policy)}
	receipt, err := management.Command(ctx, service, api.Raw(install))
	if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
		t.Fatalf("public original policy installation: %v %+v", err, receipt)
	}
	// 用公开 reserve 核实现代候选没有许可，禁止私有 SQL 造注册状态。
	modernValues := values
	modernValues.Purposes = append(append([]string{}, values.Purposes...), "skill.register", "skill.read", "skill.load", "skill.validate", "skill.reopen", "agent_config.register", "agent_config.read", "agent_config.validate", "agent_config.reopen", "knowledge.load", "knowledge.consume")
	modern := ref
	modern.Digest, err = api.Digest(modernValues)
	if err != nil {
		t.Fatal(err)
	}
	absent := originalPolicyUploadCommand(scope, modern)
	unavailable, err := management.Command(ctx, user, api.Raw(absent))
	if err != nil || unavailable.Stage != "rejected" || unavailable.Error == nil || unavailable.Error.Code != "forbidden" || unavailable.Error.Reason != "saving_policy_unregistered" {
		t.Fatalf("modern candidate was not genuinely unregistered: %v %+v", err, unavailable)
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	storeClosed = true
	return cfg, policy
}

func TestReopenedModernContentPolicyPreservesIndependentCatalog(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			fresh, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			policy := fresh.ContentPolicy
			if cfg.Knowledge != nil || fresh.Knowledge.Enabled() {
				t.Fatal("fixture unexpectedly selected task Knowledge")
			}
			if err = fresh.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenApp(ctx, cfg, false)
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := reopened.Close(); closeErr != nil {
					t.Errorf("close reopened policy host: %v", closeErr)
				}
			}()
			if !api.Equal(reopened.ContentPolicy, policy) {
				t.Fatal("modern cold reopen changed original policy")
			}
			command := originalPolicyUploadCommand(reopened.Scope, reopened.ContentPolicy.PolicyRef)
			receipt, err := reopened.Dispatcher.Command(ctx, reopened.UserAuth, api.Raw(command))
			if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
				t.Fatalf("modern public upload: %v %+v", err, receipt)
			}
			replay, err := reopened.Dispatcher.Command(ctx, reopened.UserAuth, api.Raw(command))
			if err != nil || !api.Equal(receipt, replay) {
				t.Fatalf("modern original upload replay changed: %v", err)
			}
			skill, body := registerPublishedKnowledgeSkill(t, ctx, reopened)
			raw, err := reopened.query(ctx, "skill.load", skill.SkillRef.ComponentID, governance.SkillReference{SkillRef: skill.SkillRef})
			var loaded governance.LoadedSkill
			if err != nil || api.Decode(raw, &loaded) != nil || loaded.Body != body || !api.Equal(loaded.Definition, skill) {
				t.Fatalf("cold modern independent catalog lost original bytes: %v", err)
			}
		})
	}
}

func TestLegacyPolicyCannotEnableUnregisteredKnowledge(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cfg, _ := prepareLegacyPolicyConfiguration(t, ctx, driver)
			cfg.Knowledge = &KnowledgeConfig{SkillRefs: []api.ComponentRef{}, ControlLimits: governance.KnowledgeControls{MaxInputBytes: 65536, MaxOutputTokens: 512, MaxActionsPerDecision: 1, MaxActionDurationSeconds: 30, MaxCallCostBound: []api.Amount{{Unit: "USD", Value: "1"}}}}
			if err := governance.ValidateKnowledgeControls(cfg.Knowledge.ControlLimits); err != nil {
				t.Fatalf("knowledge fixture was not valid: %v", err)
			}
			app, err := OpenApp(ctx, cfg, false)
			if app != nil {
				if closeErr := app.Close(); closeErr != nil {
					t.Errorf("close unexpected policy host: %v", closeErr)
				}
			}
			if !api.IsCode(err, "forbidden") || err.Error() != "forbidden: saving_policy_unregistered" {
				t.Fatalf("legacy policy silently enabled Knowledge: %v", err)
			}
		})
	}
}

func TestReopenedContentPolicyRejectsRevokedOriginalServiceActor(t *testing.T) {
	for _, driver := range []string{"sqlite", "postgres"} {
		t.Run(driver, func(t *testing.T) {
			if driver == "postgres" && os.Getenv("HARNESS_TEST_POSTGRES_DSN") == "" {
				t.Skip("actual PostgreSQL DSN required")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			root := t.TempDir()
			cfg, err := InitializeConfig(ctx, filepath.Join(root, "config.json"), root, driver)
			if err != nil {
				t.Fatal(err)
			}
			cfg.TenantID, cfg.OwnerID, cfg.SubjectID = api.NewID("tenant"), api.NewID("owner"), api.NewID("subject")
			original, err := OpenApp(ctx, cfg, true)
			if err != nil {
				t.Fatal(err)
			}
			if err = original.Identity.Revoke(ctx, original.ServiceAuth); err != nil {
				t.Fatal(err)
			}
			if err = original.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenApp(ctx, cfg, false)
			if reopened != nil {
				if closeErr := reopened.Close(); closeErr != nil {
					t.Errorf("close unexpected reopened policy host: %v", closeErr)
				}
			}
			if err == nil || err.Error() != "forbidden: credential_revoked" {
				t.Fatalf("reopen substituted revoked service authority: %v", err)
			}
		})
	}
}

// 原管理读的提交未知不能发布临时选择；明确恢复后仍消费原已登记许可。
func TestContentPolicySelectionKeepsOriginalFrameOnReadFaultAndCommitUnknown(t *testing.T) {
	for _, name := range []string{"raw_read", "mixed_missing", "after_commit_unknown"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
			defer cancel()
			cfg, legacy := prepareLegacyPolicyConfiguration(t, ctx, "sqlite")
			originalFault := errors.New("original policy metadata boundary reply lost")
			armed := false
			store, err := sqlite.Open(cfg.DatabasePath, sqlite.WithExpectedDatabaseID(cfg.DatabaseID), sqlite.WithCommitFault(func(phase sqlite.CommitPhase) error {
				if armed && name == "after_commit_unknown" && phase == sqlite.AfterCommit {
					return originalFault
				}
				return nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			defer func() {
				if closeErr := store.Close(); closeErr != nil {
					t.Errorf("close original policy fault store: %v", closeErr)
				}
			}()
			var readFault error
			if name == "raw_read" {
				readFault = originalFault
			}
			if name == "mixed_missing" {
				readFault = errors.Join(runtime.ErrNotFound, originalFault)
			}
			reads := 0
			boundary := policySelectionFaultStore{Store: store, fault: &readFault, reads: &reads}
			objects, err := objectstore.OpenLocal(filepath.Join(cfg.DataRoot, "objects"), memory.MaxContentBytes)
			if err != nil {
				t.Fatal(err)
			}
			values := legacy.Values
			values.Purposes = append(append([]string{}, values.Purposes...), "skill.register", "skill.read", "skill.load", "skill.validate", "skill.reopen", "agent_config.register", "agent_config.read", "agent_config.validate", "agent_config.reopen", "knowledge.load", "knowledge.consume")
			modernRef := legacy.PolicyRef
			modernRef.Digest, err = api.Digest(values)
			if err != nil {
				t.Fatal(err)
			}
			modern, err := memory.NewPolicy(modernRef, values)
			if err != nil {
				t.Fatal(err)
			}
			app := &App{Config: cfg, Store: boundary, Scope: runtime.Scope{TenantID: cfg.TenantID, OwnerID: cfg.OwnerID, DatabaseID: store.ID()}, ContentPolicy: modern}
			app.ServiceAuth = runtime.Auth{TenantID: cfg.TenantID, SubjectID: cfg.OwnerID, CredentialGeneration: 1, Roles: []string{"service", "orchestrator", "task_admin", "evidence", "grant_authority", "content_admin", "memory_admin", "usage_reporter", "maintainer", "evidence_consumer"}}
			app.Memory = memory.New(boundary, objects)
			app.Memory.Location = "cloud"
			if err = app.Memory.ConfigureParticipants("platform"); err != nil {
				t.Fatal(err)
			}
			app.Memory.Authorization = contentAuthority{app}
			armed = true
			err = app.selectRegisteredContentPolicy(ctx)
			if !errors.Is(err, originalFault) || !api.Equal(app.ContentPolicy, modern) {
				t.Fatalf("metadata failure published a replacement policy or lost cause: %v", err)
			}
			if name == "after_commit_unknown" {
				if !errors.Is(err, runtime.ErrCommitUnknown) || reads != 2 {
					t.Fatalf("original metadata commit-unknown boundary changed: reads=%d error=%v", reads, err)
				}
			} else if reads != 1 {
				t.Fatalf("unclassified original read fault fell back to another policy: reads=%d", reads)
			}
			// 禁止提交未知的只读恢复偷偷安装现代许可；核对实际原库事实。
			var unexpected memory.Policy
			if _, e := store.Read(ctx, app.Scope, "content.policies", modern.PolicyRef.ComponentID+":"+modern.PolicyRef.Version+":"+modern.PolicyRef.Digest, 0, &unexpected); e != runtime.ErrNotFound {
				t.Fatalf("metadata recovery installed a new license: %v", e)
			}
			armed = false
			readFault = nil
			if err = app.selectRegisteredContentPolicy(ctx); err != nil || !api.Equal(app.ContentPolicy, legacy) {
				t.Fatalf("known recovery did not retain exact original registered policy: %v", err)
			}
			registry := runtime.NewRegistry()
			app.Memory.Register(registry)
			dispatcher := runtime.Dispatcher{Store: boundary, OwnerID: cfg.OwnerID, Registry: registry}
			user := runtime.Auth{TenantID: cfg.TenantID, SubjectID: cfg.SubjectID, CredentialGeneration: 1, Roles: cfg.UserRoles}
			original := originalPolicyUploadCommand(app.Scope, app.ContentPolicy.PolicyRef)
			receipt, err := dispatcher.Command(ctx, user, api.Raw(original))
			if err != nil || receipt.Stage != "applied" || receipt.Error != nil {
				t.Fatalf("original public upload after known recovery: %v %+v", err, receipt)
			}
			replay, err := dispatcher.Command(ctx, user, api.Raw(original))
			if err != nil || !api.Equal(receipt, replay) {
				t.Fatalf("recovered original command replay changed: %v", err)
			}
		})
	}
}

type policySelectionFaultStore struct {
	runtime.Store
	fault *error
	reads *int
}

func (s policySelectionFaultStore) Within(ctx context.Context, scope runtime.Scope, parts []string, fn func(runtime.Tx) error) (runtime.CommitStatus, error) {
	return s.Store.Within(ctx, scope, parts, func(tx runtime.Tx) error { return fn(policySelectionFaultTx{Tx: tx, fault: s.fault, reads: s.reads}) })
}

type policySelectionFaultTx struct {
	runtime.Tx
	fault *error
	reads *int
}

func (tx policySelectionFaultTx) Get(ctx context.Context, ns, id string, value any) (uint64, error) {
	if ns == "content.policies" {
		*tx.reads++
		if *tx.fault != nil {
			return 0, *tx.fault
		}
	}
	return tx.Tx.Get(ctx, ns, id, value)
}
