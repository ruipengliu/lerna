package bootstrap

import (
	"context"
	"crypto/rand"
	"fmt"
	execadapter "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/wss"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/interaction"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type App struct {
	Config                                                           Config
	Store                                                            runtime.Store
	Registry                                                         *runtime.Registry
	Dispatcher                                                       *runtime.Dispatcher
	Identity                                                         *platform.DevIdentity
	Keys                                                             *platform.Keyring
	Memory                                                           *memory.Service
	Task                                                             *task.Service
	Brain                                                            *brain.Service
	Execution                                                        *execution.Service
	Interaction                                                      *interaction.Service
	Governance                                                       *governance.Service
	Objects                                                          *objectstore.Local
	Files                                                            *execadapter.ManagedFiles
	Phones                                                           *execadapter.SimulatedPhones
	Scope                                                            runtime.Scope
	ServiceAuth, UserAuth                                            runtime.Auth
	ContentPolicy                                                    memory.Policy
	TaskPolicy                                                       task.TaskPolicy
	Profile                                                          brain.Profile
	ArtifactRule, SavedRule, CoverageRule, AnswerSchema, InstallLock api.ComponentRef
	ReadBinding, WriteBinding                                        api.ObjectRef
	GrantID                                                          string
}

func component(name string) api.ComponentRef {
	return api.ComponentRef{ComponentID: platform.StableDevelopmentID("component", name), Version: "1.0.0", Digest: api.Hash([]byte("harness-builtin/" + name + "/1"))}
}
func OpenApp(ctx context.Context, c Config, initialize bool) (*App, error) {
	st, e := OpenStore(ctx, c, false)
	if e != nil {
		return nil, e
	}
	a := &App{Config: c, Store: st, Registry: runtime.NewRegistry()}
	ok := false
	defer func() {
		if !ok {
			a.Close()
		}
	}()
	a.Scope = runtime.Scope{TenantID: c.TenantID, OwnerID: c.OwnerID, DatabaseID: st.ID()}
	a.UserAuth = runtime.Auth{TenantID: c.TenantID, SubjectID: c.SubjectID, CredentialGeneration: 1, Roles: []string{"trusted_renderer", "grant_authority", "content_admin", "memory_admin", "maintainer", "evidence_consumer", "evaluation_admin", "release_authority"}}
	a.ServiceAuth = runtime.Auth{TenantID: c.TenantID, SubjectID: c.OwnerID, CredentialGeneration: 1, Roles: []string{"service", "orchestrator", "task_admin", "evidence", "grant_authority", "content_admin", "memory_admin", "usage_reporter", "maintainer", "evidence_consumer"}}
	serviceSecret := make([]byte, 32)
	if _, e = rand.Read(serviceSecret); e != nil {
		return nil, e
	}
	token, e := os.ReadFile(c.TokenFile)
	if e != nil {
		return nil, e
	}
	a.Identity = &platform.DevIdentity{Store: st, OwnerID: c.OwnerID, Principals: []platform.Principal{{Auth: a.UserAuth, TokenHash: api.Hash([]byte(strings.TrimSpace(string(token))))}, {Auth: a.ServiceAuth, TokenHash: api.Hash(serviceSecret)}}, SessionTTL: 8 * time.Hour}
	if initialize {
		if e = a.Identity.Initialize(ctx); e != nil {
			return nil, e
		}
	} else {
		if e = a.Identity.CheckCurrent(ctx, a.UserAuth); e != nil {
			return nil, e
		}
	}
	a.Keys, e = platform.OpenDevelopmentKey(c.KeyFile, c.TenantID, c.OwnerID, []string{"control", "closure", "grant_use", "delivery", "rpc_sender"})
	if e != nil {
		return nil, e
	}
	a.Objects, e = objectstore.OpenLocal(filepath.Join(c.DataRoot, "objects"), memory.MaxContentBytes)
	if e != nil {
		return nil, e
	}
	a.Memory = memory.New(st, a.Objects)
	a.Memory.Location = "cloud"
	if e = a.Memory.ConfigureParticipants("platform", "governance"); e != nil {
		return nil, e
	}
	a.Memory.Authorization = contentAuthority{a}
	purposes := []string{"read", "preview", "content.read", "content.write", "task.goal", "task.context", "task.result", "task.submit", "task.snapshot", "task.dispatch", "task.complete", "task.evidence", "task.input", "task.accept_result", "task.revise", "task.steer", "task.action", "task.attach_evidence", "task.adjust_budget", "task.need_context", "task.delegate", "child.create", "child.new_goal", "child.continue", "billing.adjustment", "brain.input", "brain.output", "result", "memory.save", "memory.read", "memory.query", "memory.extract", "memory.sync", "memory.view", "managed_file_write", "managed_file_read", "execution.intent", "execution.arguments", "execution.output", "execution.control", "interaction.input", "interaction.history", "interaction.surface", "schedule.template", "confirmation.preview", "evaluation.manifest"}
	pv := memory.PolicyValues{Subjects: []string{c.SubjectID, c.OwnerID}, Purposes: purposes, Locations: []string{"cloud", "device"}, RetainUntil: c.PolicyExpiresAt, Continuous: true, IndependentDerived: false}
	policyRef := component("content-policy")
	policyRef.Digest, _ = api.Digest(pv)
	a.ContentPolicy = memory.Policy{PolicyRef: policyRef, Values: pv, Revision: 1, State: "active"}
	a.CoverageRule = component("goal-template-coverage")
	a.ArtifactRule = component("artifact-exact")
	a.SavedRule = component("file-saved-readback")
	a.AnswerSchema = component("goal-answer-schema")
	a.AnswerSchema.Digest, _ = api.Digest(brain.GoalSchema())
	a.InstallLock = component("builtin-install-lock")
	a.ReadBinding = a.Scope.Ref(platform.StableDevelopmentID("binding", "file-read"), 1)
	a.WriteBinding = a.Scope.Ref(platform.StableDevelopmentID("binding", "file-write"), 1)
	a.GrantID = platform.StableDevelopmentID("grant", "development-file-goal")
	a.Profile = brain.Profile{Ref: component("rule-bytes-profile"), ContextLimit: 262144, MaxInputTokens: 250000, MaxOutputTokens: 8192, SafetyMargin: 100, MaxInputBytes: 262144, RequestTimeout: 5 * time.Second}
	a.Governance = governance.New(st, governance.Options{Content: governanceContent{a}, Proof: proofBridge{a}, UsageVerifier: usageVerifier{a}, PreviewGate: previewGate{a}, Participants: []string{"content", "memory", "platform", "task"}})
	if e = os.MkdirAll(filepath.Join(c.DataRoot, "files"), 0700); e != nil {
		return nil, e
	}
	if initialize {
		if e = os.MkdirAll(filepath.Join(c.DataRoot, "files", "reports"), 0700); e != nil {
			return nil, e
		}
	}
	a.Files, e = execadapter.NewManagedFiles(filepath.Join(c.DataRoot, "files"))
	if e != nil {
		return nil, e
	}
	phoneIDs := []string{platform.StableDevelopmentID("resource", "phone-one"), platform.StableDevelopmentID("resource", "phone-two"), platform.StableDevelopmentID("resource", "phone-three")}
	if e = os.MkdirAll(filepath.Join(c.DataRoot, "phones"), 0700); e != nil {
		return nil, e
	}
	a.Phones, e = execadapter.NewSimulatedPhones(filepath.Join(c.DataRoot, "phones"), phoneIDs)
	if e != nil {
		return nil, e
	}
	execContent := executionContent{a}
	a.Execution, e = execution.New(execution.Config{OwnerID: c.OwnerID, Content: execContent, Authority: executionAuthority{a}, AuthorityParticipants: []string{"task", "governance", "content", "memory", "platform"}, Drivers: []execution.Driver{&execadapter.FileDriver{Files: a.Files, Content: execContent, Location: "cloud"}, &execadapter.FileDriver{Files: a.Files, Content: execContent, Location: "cloud", ReadOnly: true}, a.Phones, &execution.TrustedComputeDriver{Content: execContent, Store: st, Location: "cloud"}}, ResourceDriver: a.Phones, Location: "cloud"})
	if e != nil {
		return nil, fmt.Errorf("construct Execution: %w", e)
	}
	engine := &brain.RuleEngine{Facts: factSource{a}, Goals: factSource{a}, ArtifactRule: a.ArtifactRule, SavedRule: a.SavedRule, AnswerSchema: a.AnswerSchema, ReadCapability: execadapter.FileReadCapability().Ref, WriteCapability: execadapter.FileWriteCapability().Ref, ReadBinding: a.ReadBinding, WriteBinding: a.WriteBinding}
	a.Brain, e = brain.New(brain.Config{Profiles: []brain.Profile{a.Profile}, Content: brainContent{a}, Engine: engine, Gate: brainGate{a}, Participants: []string{"content", "memory", "task", "platform"}})
	if e != nil {
		return nil, fmt.Errorf("construct Brain: %w", e)
	}
	a.TaskPolicy = task.TaskPolicy{PolicyRef: component("task-policy"), ContinuationLimit: 30, RepairLimit: 3, NoProgressLimit: 8, ContextRoundLimit: 3, SafeAttemptLimit: 1, MaxRequirements: 20, MaxDelegations: 20, MaxDepth: 4, CostMode: "strict", BudgetLimits: []api.Amount{{Unit: "USD", Value: "100"}}, MaxEvidenceStalenessSeconds: 300, MaxDurationSeconds: 3600, InputPolicyRef: a.AnswerSchema, RuleRegistryRef: component("rule-registry")}
	a.TaskPolicy.PolicyRef.Digest, _ = api.Digest(a.TaskPolicy)
	rules := []api.RuleDefinition{}
	for _, ref := range []api.ComponentRef{a.ArtifactRule, a.SavedRule, a.CoverageRule} {
		kind := "quality"
		predicate := "quality"
		if api.Equal(ref, a.SavedRule) {
			kind = "effect"
			predicate = "current_state"
		}
		age := uint64(300)
		rule := api.RuleDefinition{RuleRef: ref, Kind: kind, ParametersSchemaRef: component("rule-parameters"), Predicate: predicate, AllowedBasis: []string{"verified"}, RequiredEvidenceSchemaRef: component("file-evidence"), ScopeSchemaRef: component("scope"), RiskClass: "ordinary", ApplicabilityPolicyRef: a.TaskPolicy.PolicyRef}
		if predicate == "current_state" {
			rule.MaxObservationAgeSeconds = &age
		}
		rules = append(rules, rule)
	}
	a.Task, e = task.New(task.Config{Policies: []task.TaskPolicy{a.TaskPolicy}, Rules: rules, ControlWindow: 5 * time.Second, Participants: []string{"task", "content", "memory", "governance", "platform"}, AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: a.AnswerSchema, Schema: brain.GoalSchema()}}}, task.Ports{Content: taskContent{a}, Context: contextCompiler{a}, Gate: taskGate{a}, Evidence: evidenceBridge{a}, ControlProof: controlProof{a}, ClosureProof: closureProof{a}, ActionAuthorization: actionAuthorization{a}, Brain: brainBridge{a}, Execution: executionBridge{a}})
	if e != nil {
		return nil, fmt.Errorf("construct Task: %w", e)
	}
	calendar, e := interaction.OpenTZDB(c.TZDBRoot, c.TZDBVersion, []string{"UTC", "Asia/Shanghai", "America/New_York", "Europe/London"})
	if e != nil {
		return nil, e
	}
	a.Interaction, e = interaction.New(interaction.Config{DiscoveryOwnerID: c.OwnerID, Participants: []string{"interaction", "content", "memory", "task", "platform"}, CursorKey: []byte(strings.TrimSpace(string(token)))}, interaction.Ports{Content: interactionContent{a}, Delivery: localDelivery{a}, Closure: localClosure{a}, Requests: requestBridge{a}, Calendar: calendar, ScheduleGate: scheduleGate{a}})
	if e != nil {
		return nil, fmt.Errorf("construct Interaction: %w", e)
	}
	a.Dispatcher = &runtime.Dispatcher{Store: st, OwnerID: c.OwnerID, Registry: a.Registry}
	a.Memory.Register(a.Registry)
	if e = a.Registry.RegisterJob(proofJob, a.publishProof); e != nil {
		return nil, e
	}
	for _, register := range []func(*runtime.Registry) error{a.Task.Register, a.Brain.Register, a.Execution.Register, a.Governance.Register, a.Interaction.Register} {
		if e = register(a.Registry); e != nil {
			return nil, e
		}
	}
	if initialize {
		if e = a.initialize(ctx, rules); e != nil {
			return nil, e
		}
	}
	ok = true
	return a, nil
}
func (a *App) initialize(ctx context.Context, rules []api.RuleDefinition) error {
	if e := a.Memory.InstallPolicy(ctx, a.Scope, a.ServiceAuth, a.ContentPolicy); e != nil {
		return fmt.Errorf("install content policy: %w", e)
	}
	status, e := a.Store.Within(ctx, a.Scope, []string{"governance"}, func(tx runtime.Tx) error {
		for _, r := range rules {
			if _, e := a.Governance.RegisterRuleTx(ctx, tx, governance.RuleDefinition{ComponentRef: r.RuleRef, Kind: r.Kind, Predicate: r.Predicate, AllowedBasis: r.AllowedBasis, RiskClass: "ordinary", MaxObservationAgeSeconds: 300, Calibrated: true}); e != nil {
				return e
			}
		}
		var existing api.Grant
		if _, e := tx.Get(ctx, "governance.grants", a.GrantID, &existing); e == nil {
			return nil
		} else if !api.IsCode(e, "not_found") {
			return e
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		return a.Governance.ProvisionGrantTx(ctx, tx, a.ServiceAuth, api.Grant{GrantID: a.GrantID, OwnerID: a.Scope.OwnerID, Revision: 1, SubjectRef: a.ServiceAuth.Ref(a.Scope.OwnerID), Resources: []string{"managed-files"}, Actions: []string{"file.read", "file.write"}, Purposes: []string{"goal_action", "requirement_check"}, Recipients: []string{a.Scope.OwnerID}, Locations: []string{"cloud"}, Mode: "continuous", State: "active", NotBefore: api.Time(now), ExpiresAt: a.Config.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "100"}}})
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if e != nil {
		return fmt.Errorf("install development governance: %w", e)
	}
	return nil
}
func (a *App) Close() error {
	if a.Files != nil {
		a.Files.Close()
	}
	if a.Store != nil {
		return a.Store.Close()
	}
	return nil
}
func (a *App) Gateway() (*wss.Server, error) {
	return wss.New(wss.Config{OwnerID: a.Config.OwnerID, Store: a.Store, Registry: a.Registry, Identity: a.Identity, Processor: wss.LocalProcessor{Dispatcher: a.Dispatcher}, Content: a.Memory, Uploader: a.Memory, Development: &wss.DevelopmentConfiguration{TenantID: a.Config.TenantID, ContentPolicyRef: a.ContentPolicy.PolicyRef, TaskPolicyRef: a.TaskPolicy.PolicyRef, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, GoalSchema: brain.GoalSchema(), RetentionSeconds: 86400, TaskDeadlineSeconds: 1800}, Origins: a.Config.Origins, AllowInsecureLoopback: a.Config.Development, StaticDir: a.Config.StaticDir, Location: "cloud", MaxConnections: 128, MaxQueuedBytes: 64 << 20})
}
