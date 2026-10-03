package development

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"errors"
	"fmt"
	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/adapters/endpointchannel"
	execadapter "github.com/ruipengliu/lerna/adapters/execution"
	rpcadapter "github.com/ruipengliu/lerna/adapters/grpc"
	"github.com/ruipengliu/lerna/adapters/objectstore"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/providers"
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
	Knowledge                                                        *KnowledgeAssembly
	WASI                                                             *WASIAssembly
	RemoteAgent                                                      *collaboration.Remote
	Objects                                                          *objectstore.Local
	Files                                                            *execadapter.ManagedFiles
	Phones                                                           *execadapter.SimulatedPhones
	Scope                                                            runtime.Scope
	ServiceAuth, UserAuth                                            runtime.Auth
	ContentPolicy                                                    memory.Policy
	TaskPolicy                                                       task.TaskPolicy
	Profile                                                          brain.Profile
	Engine                                                           brain.Engine
	Model                                                            *providers.OpenAI
	Information                                                      []*providers.HTTPInformation
	TokenizerRef                                                     api.ComponentRef
	ArtifactRule, SavedRule, CoverageRule, AnswerSchema, InstallLock api.ComponentRef
	ReadBinding, WriteBinding                                        api.ObjectRef
	ApplicationBinding                                               api.ObjectRef
	ApplicationEventSchema                                           api.Schema
	GrantID                                                          string
	OwnsTargets                                                      bool
	Role                                                             string
	closeGovernance                                                  func() error
	actions                                                          *actionRegistry
	information                                                      []configuredInformation
	endpointAuthority                                                *rpcadapter.StaticEndpointAuthority
	endpointRouter                                                   *endpointchannel.Router
	endpointServerTLS                                                *tls.Config
	remoteAgents                                                     *remoteAgentAssembly
	foreignConsumerSource                                            *providers.ForeignSource
	remoteExecutors                                                  *remoteExecutors
}

func component(name string) api.ComponentRef {
	return api.ComponentRef{ComponentID: platform.StableDevelopmentID("component", name), Version: "1.0.0", Digest: api.Hash([]byte("harness-builtin/" + name + "/1"))}
}
func OpenApp(ctx context.Context, c Config, initialize bool) (*App, error) {
	return OpenAppForRole(ctx, c, initialize, "dev")
}

func OpenAppForRole(ctx context.Context, c Config, initialize bool, role string) (app *App, err error) {
	if role != "dev" && role != "worker" && role != "gateway" && role != "application" && role != "management" {
		return nil, api.E("unsupported", "process_role_not_configured")
	}
	if err = validateEndpointChannels(c); err != nil {
		return nil, err
	}
	if err = validateRemoteAgent(c); err != nil {
		return nil, err
	}
	if err = validateForeignConsumers(c); err != nil {
		return nil, err
	}
	st, e := OpenStore(ctx, c, false)
	if e != nil {
		return nil, e
	}
	a := &App{Config: c, Store: st, Registry: runtime.NewRegistry(), OwnsTargets: role == "dev" || role == "worker" && c.WorkerPool == nil, Role: role}
	ok := false
	defer func() {
		if !ok {
			err = errors.Join(err, a.Close())
		}
	}()
	a.Scope = runtime.Scope{TenantID: c.TenantID, OwnerID: c.OwnerID, DatabaseID: st.ID()}
	userRoles := c.UserRoles
	if userRoles == nil {
		// 旧配置保留原凭据的角色集合，初始化不能静默扩大权限。
		userRoles = []string{"trusted_renderer", "grant_authority", "content_admin", "memory_admin", "maintainer", "evidence_consumer", "evaluation_admin", "release_authority"}
	}
	a.UserAuth = runtime.Auth{TenantID: c.TenantID, SubjectID: c.SubjectID, CredentialGeneration: 1, Roles: append([]string{}, userRoles...)}
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
	remotePrincipals, e := remoteAgentPrincipals(c)
	if e != nil {
		return nil, e
	}
	a.Identity.Principals = append(a.Identity.Principals, remotePrincipals...)
	foreignPrincipals, e := foreignConsumerPrincipals(c)
	if e != nil {
		return nil, e
	}
	a.Identity.Principals = append(a.Identity.Principals, foreignPrincipals...)
	if initialize {
		if e = a.Identity.Initialize(ctx); e != nil {
			return nil, e
		}
	} else {
		if e = a.Identity.CheckCurrent(ctx, a.UserAuth); e != nil {
			return nil, e
		}
	}
	keyPurposes := []string{"control", "closure", "grant_use", "delivery", "rpc_sender", "evidence_changes", "grant_lease", "allocation_closure", "evaluation_prepare", "evaluation_start"}
	if c.RemoteAgent != nil {
		keyPurposes = append(keyPurposes, "agent_allocation", "agent_state", "foreign_content")
	}
	if len(c.ForeignConsumers) > 0 && !containsString(keyPurposes, "foreign_content") {
		keyPurposes = append(keyPurposes, "foreign_content")
	}
	if len(c.RemoteExecutors) > 0 {
		keyPurposes = append(keyPurposes, "executor_admission", "executor_revocation")
	}
	a.Keys, e = platform.OpenDevelopmentKey(c.KeyFile, c.TenantID, c.OwnerID, keyPurposes)
	if e != nil {
		return nil, e
	}
	if e = a.configureRemoteExecutors(); e != nil {
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
	if len(c.RemoteExecutors) > 0 {
		a.Memory.Foreign = deviceSources{a}
	}
	purposes := []string{"read", "preview", "content.read", "content.write", "task.goal", "task.context", "task.result", "task.submit", "task.snapshot", "task.dispatch", "task.complete", "task.evidence", "task.input", "task.accept_result", "task.revise", "task.steer", "task.action", "task.attach_evidence", "task.adjust_budget", "task.need_context", "task.delegate", "child.create", "child.new_goal", "child.continue", "billing.adjustment", "brain.input", "brain.output", "result", "memory.save", "memory.read", "memory.query", "memory.extract", "memory.sync", "memory.view", "managed_file_write", "managed_file_read", "execution.intent", "execution.arguments", "execution.output", "execution.control", "execution_intent", "execution_arguments", "execution_result", "execution_usage_proof", "environment_namespace", "environment_input", "environment_compute_spec", "environment_restore", "interaction.input", "interaction.history", "interaction.surface", "schedule.template", "confirmation.preview", "evaluation.manifest"}
	for _, purpose := range append([]string{"interaction.snapshot", "interaction.preview"}, RequiredContentPurposes()...) {
		if !containsString(purposes, purpose) {
			purposes = append(purposes, purpose)
		}
	}
	if len(c.Information) > 0 {
		purposes = append(purposes, providers.InformationPurpose, providers.InformationSearch, providers.InformationBody)
	}
	purposes = append(purposes, RequiredKnowledgeContentPurposes()...)
	if c.WASI != nil && c.WASI.CPUSecondsBudgetLimit != "" {
		for _, purpose := range RequiredWASIContentPurposes() {
			if !containsString(purposes, purpose) {
				purposes = append(purposes, purpose)
			}
		}
	}
	pv := memory.PolicyValues{Subjects: []string{c.SubjectID, c.OwnerID}, Purposes: purposes, Locations: []string{"cloud", "device"}, RetainUntil: c.PolicyExpiresAt, Continuous: true, IndependentDerived: false}
	if c.RemoteAgent != nil {
		for _, subject := range c.RemoteAgent.SourceSubjectRefs {
			if !containsString(pv.Subjects, subject.ObjectID) {
				pv.Subjects = append(pv.Subjects, subject.ObjectID)
			}
		}
	}
	for _, consumer := range c.ForeignConsumers {
		for _, holder := range consumer.Holders {
			if !containsString(pv.Subjects, holder.SubjectRef.ObjectID) {
				pv.Subjects = append(pv.Subjects, holder.SubjectRef.ObjectID)
			}
		}
		for _, purpose := range consumer.Purposes {
			if !containsString(pv.Purposes, purpose) {
				pv.Purposes = append(pv.Purposes, purpose)
			}
		}
		for _, location := range consumer.Locations {
			if !containsString(pv.Locations, location) {
				pv.Locations = append(pv.Locations, location)
			}
		}
	}
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
	a.ApplicationBinding = a.Scope.Ref(platform.StableDevelopmentID("binding", "development-application"), 1)
	a.ApplicationEventSchema = api.Object(map[string]any{"reason": api.Schema{"type": "string", "minLength": 1, "maxLength": 200}}, "reason")
	a.GrantID = platform.StableDevelopmentID("grant", "development-file-goal")
	a.Profile = brain.Profile{Ref: component("rule-bytes-profile"), ContextLimit: 262144, MaxInputTokens: 250000, MaxOutputTokens: 8192, SafetyMargin: 100, MaxInputBytes: 262144, RequestTimeout: 5 * time.Second}
	lifecycle, evaluation, closeGovernance, err := configureBuiltinGovernance(a, c.Governance)
	if err != nil {
		return nil, err
	}
	a.closeGovernance = closeGovernance
	a.Governance = governance.New(st, governance.Options{GrantMetadataGate: grantMetadataGate{}, Content: governanceContent{a}, Proof: proofBridge{a}, UsageVerifier: usageVerifier{a}, PreviewGate: previewGate{a}, KnowledgeGate: knowledgeContentGate{a}, ResultNotices: resultNoticeBridge{a}, Lifecycle: lifecycle, Runner: evaluation, Participants: []string{"content", "memory", "platform", "task"}})
	if e = os.MkdirAll(filepath.Join(c.DataRoot, "files"), 0700); e != nil {
		return nil, e
	}
	if initialize {
		if e = os.MkdirAll(filepath.Join(c.DataRoot, "files", "reports"), 0700); e != nil {
			return nil, e
		}
	}
	if a.OwnsTargets {
		a.Files, e = execadapter.NewManagedFiles(filepath.Join(c.DataRoot, "files"))
		if e != nil {
			return nil, e
		}
	}
	phoneIDs := []string{platform.StableDevelopmentID("resource", "phone-one"), platform.StableDevelopmentID("resource", "phone-two"), platform.StableDevelopmentID("resource", "phone-three")}
	if e = os.MkdirAll(filepath.Join(c.DataRoot, "phones"), 0700); e != nil {
		return nil, e
	}
	if a.OwnsTargets {
		a.Phones, e = execadapter.NewSimulatedPhones(filepath.Join(c.DataRoot, "phones"), phoneIDs)
		if e != nil {
			return nil, e
		}
	}
	execContent := executionContent{a}
	drivers := []execution.Driver{&execadapter.FileDriver{Files: a.Files, Content: execContent, Location: "cloud"}, &execadapter.FileDriver{Files: a.Files, Content: execContent, Location: "cloud", ReadOnly: true}, a.Phones, &execadapter.PhoneGUIDriver{Phones: a.Phones}, &execution.TrustedComputeDriver{Content: execContent, Store: st, Location: "cloud"}}
	var environmentAdmission execution.EnvironmentAdmission
	if c.WASI != nil {
		assembly, err := configureWASI(a, c.WASI)
		if err != nil {
			return nil, err
		}
		a.WASI = &assembly
		drivers = append(drivers, assembly.Driver)
		environmentAdmission = assembly.Admission
	}
	informationDrivers, err := a.configureInformation()
	if err != nil {
		return nil, err
	}
	drivers = append(drivers, informationDrivers...)
	if e = a.configureActionRegistry(drivers); e != nil {
		return nil, e
	}
	if c.InformationReferenceAnswer {
		if len(a.information) == 0 {
			return nil, api.E("unsupported", "reference_information_source_required")
		}
		lock := a.InstallLock
		a.InstallLock = component("development-reference-answer-assembly")
		a.InstallLock.Digest, e = api.Digest(struct {
			Actions  api.ComponentRef `json:"actions"`
			Rule     api.ComponentRef `json:"rule"`
			Question api.Schema       `json:"question"`
		}{lock, component("source-reference-answer"), InformationQuestionSchema()})
		if e != nil {
			return nil, e
		}
	}
	var resources execution.ResourceDriver = a.Phones
	if !a.OwnsTargets {
		for i, driver := range drivers {
			drivers[i] = contractOnlyDriver{capability: driver.Capability()}
		}
		resources = contractOnlyResources{}
	}
	remoteParts := func(parts []string) []string {
		if c.RemoteAgent != nil {
			parts = append(parts, "collaboration")
		}
		return parts
	}
	a.Execution, e = execution.New(execution.Config{OwnerID: c.OwnerID, Content: execContent, Authority: executionAuthority{a}, AuthorityParticipants: remoteParts([]string{"task", "governance", "content", "memory", "platform"}), Drivers: drivers, ResourceDriver: resources, EnvironmentAdmission: environmentAdmission, Location: "cloud"})
	if e != nil {
		return nil, fmt.Errorf("construct Execution: %w", e)
	}
	a.Engine = &brain.RuleEngine{Facts: factSource{a}, Goals: factSource{a}, ArtifactRule: a.ArtifactRule, SavedRule: a.SavedRule, AnswerSchema: a.AnswerSchema, ReadCapability: execadapter.FileReadCapability().Ref, WriteCapability: execadapter.FileWriteCapability().Ref, ReadBinding: a.ReadBinding, WriteBinding: a.WriteBinding}
	a.TokenizerRef = component("rule-byte-count")
	if e = a.configureModel(); e != nil {
		return nil, e
	}
	a.Brain, e = brain.New(brain.Config{Profiles: []brain.Profile{a.Profile}, Content: brainContent{a}, Engine: a.Engine, Gate: brainGate{a}, Participants: remoteParts([]string{"content", "memory", "task", "platform", "governance"})})
	if e != nil {
		return nil, fmt.Errorf("construct Brain: %w", e)
	}
	a.TaskPolicy = task.TaskPolicy{PolicyRef: component("task-policy"), ContinuationLimit: 30, RepairLimit: 3, NoProgressLimit: 8, ContextRoundLimit: 3, SafeAttemptLimit: 1, MaxRequirements: 20, MaxDelegations: 20, MaxDepth: 4, CostMode: "strict", BudgetLimits: []api.Amount{{Unit: "USD", Value: "100"}}, MaxEvidenceStalenessSeconds: 300, MaxDurationSeconds: 3600, InputPolicyRef: a.AnswerSchema, RuleRegistryRef: component("rule-registry")}
	if c.WASI != nil && c.WASI.CPUSecondsBudgetLimit != "" {
		a.TaskPolicy.BudgetLimits = append(a.TaskPolicy.BudgetLimits, api.Amount{Unit: "cpu_seconds", Value: c.WASI.CPUSecondsBudgetLimit})
	}
	a.TaskPolicy.PolicyRef.Digest, _ = api.Digest(a.TaskPolicy)
	a.Knowledge, e = configureKnowledge(a, c.Knowledge)
	if e != nil {
		return nil, e
	}
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
	if c.InformationReferenceAnswer {
		rules = append(rules, api.RuleDefinition{RuleRef: component("source-reference-answer"), Kind: "quality", ParametersSchemaRef: component("information-reference-question"), Predicate: "quality", AllowedBasis: []string{"verified"}, RequiredEvidenceSchemaRef: component("information-reference-evidence"), ScopeSchemaRef: component("information-reference-scope"), RiskClass: "ordinary", ApplicabilityPolicyRef: a.TaskPolicy.PolicyRef})
	}
	cooperation, e := collaboration.New(collaboration.Config{Store: st, Registry: a.Registry, OwnerID: c.OwnerID, Auth: a.ServiceAuth, SubjectGate: taskGate{a}, Participants: []string{"collaboration", "task", "platform"}})
	if e != nil {
		return nil, e
	}
	a.RemoteAgent, e = a.configureRemoteAgent(cooperation)
	if e != nil {
		return nil, e
	}
	if a.Memory.Foreign != nil {
		a.Memory.Foreign = flowSources{a.Memory.Foreign}
		a.Registry.SetContextFactory(a.foreignContextFactory)
	}
	var collaborationPort task.CollaborationPort = cooperation
	if a.RemoteAgent != nil {
		collaborationPort = a.RemoteAgent
	}
	a.Task, e = task.New(task.Config{Policies: []task.TaskPolicy{a.TaskPolicy}, Rules: rules, ControlWindow: 5 * time.Second, Participants: remoteParts([]string{"task", "content", "memory", "governance", "platform"}), AnswerSchemas: []task.AnswerSchemaDefinition{{Ref: a.AnswerSchema, Schema: brain.GoalSchema()}}}, task.Ports{Content: taskContent{a}, Context: contextCompiler{a}, Gate: taskGate{a}, Evidence: evidenceBridge{a}, ControlProof: controlProof{a}, ClosureProof: closureProof{a}, ActionAuthorization: actionAuthorization{a}, Brain: brainBridge{a}, Execution: executionBridge{a}, Collaboration: collaborationPort})
	if e != nil {
		return nil, fmt.Errorf("construct Task: %w", e)
	}
	if e = cooperation.BindTask(a.Task); e != nil {
		return nil, e
	}
	if a.RemoteAgent != nil {
		if e = a.RemoteAgent.BindTask(a.Task); e != nil {
			return nil, e
		}
		if e = a.RemoteAgent.Register(); e != nil {
			return nil, e
		}
	}
	calendar, e := interaction.OpenTZDB(c.TZDBRoot, c.TZDBVersion, []string{"UTC", "Asia/Shanghai", "America/New_York", "Europe/London"})
	if e != nil {
		return nil, e
	}
	one := uint64(1)
	a.Interaction, e = interaction.New(interaction.Config{DiscoveryOwnerID: c.OwnerID, Participants: remoteParts([]string{"interaction", "content", "memory", "task", "platform", "governance"}), CursorKey: []byte(strings.TrimSpace(string(token))), EventBindings: []interaction.EventBinding{{BindingRef: a.ApplicationBinding, Events: []interaction.EventRule{{Name: "archive_demo_session", Schema: api.Raw(a.ApplicationEventSchema), OwnerID: c.OwnerID, Method: "session.archive", TargetID: platform.StableDevelopmentID("session", "development-surface"), AcceptForSeconds: 60, ExpectedRevision: &one, RequiresRendered: true}}}}}, interaction.Ports{Content: interactionContent{a}, Delivery: localDelivery{a}, Closure: localClosure{a}, Requests: requestBridge{a}, Calendar: calendar, ScheduleGate: scheduleGate{a}})
	if e != nil {
		return nil, fmt.Errorf("construct Interaction: %w", e)
	}
	a.Dispatcher = &runtime.Dispatcher{Store: st, OwnerID: c.OwnerID, Registry: a.Registry}
	a.Memory.Register(a.Registry)
	if e = a.configureForeignConsumerSource(ctx, initialize); e != nil {
		return nil, e
	}
	if a.foreignConsumerSource != nil {
		if e = a.foreignConsumerSource.Register(a.Registry); e != nil {
			return nil, e
		}
	} else if a.remoteAgents != nil {
		if e = a.remoteAgents.source.Register(a.Registry); e != nil {
			return nil, e
		}
	}
	if e = a.Registry.RegisterJob(proofJob, a.publishProof); e != nil {
		return nil, e
	}
	for _, register := range []func(*runtime.Registry) error{a.Task.Register, a.Brain.Register, a.Execution.Register, a.Governance.Register, a.Interaction.Register} {
		if e = register(a.Registry); e != nil {
			return nil, e
		}
	}
	if role == "worker" && c.WorkerPool != nil {
		if _, e = a.classifiedWorker(); e != nil {
			return nil, e
		}
	}
	if e = a.configureEndpointChannels(); e != nil {
		return nil, e
	}
	if e = a.configureForeignSourceTLS(); e != nil {
		return nil, e
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
		if e := a.provisionActionGrantsTx(ctx, tx); e != nil {
			return e
		}
		for _, r := range rules {
			if _, e := a.Governance.RegisterRuleTx(ctx, tx, governance.RuleDefinition{ComponentRef: r.RuleRef, Kind: r.Kind, Predicate: r.Predicate, AllowedBasis: r.AllowedBasis, RiskClass: "ordinary", MaxObservationAgeSeconds: 300, Calibrated: true}); e != nil {
				return fmt.Errorf("register development rule: %w", e)
			}
		}
		exists, e := a.Governance.GrantExistsTx(ctx, tx, a.ServiceAuth, a.GrantID)
		if e != nil {
			return e
		}
		if exists {
			return a.provisionModelGrantTx(ctx, tx)
		}
		now, e := tx.Now(ctx)
		if e != nil {
			return e
		}
		if e = a.Governance.ProvisionGrantTx(ctx, tx, a.ServiceAuth, api.Grant{GrantID: a.GrantID, OwnerID: a.Scope.OwnerID, Revision: 1, SubjectRef: a.ServiceAuth.Ref(a.Scope.OwnerID), Resources: []string{"managed-files"}, Actions: []string{"file.read", "file.write"}, Purposes: []string{"goal_action", "requirement_check"}, Recipients: []string{a.Scope.OwnerID}, Locations: []string{"cloud"}, Mode: "continuous", State: "active", NotBefore: api.Time(now), ExpiresAt: a.Config.PolicyExpiresAt, Limits: []api.Amount{{Unit: "USD", Value: "100"}}}); e != nil {
			return fmt.Errorf("provision development grant: %w", e)
		}
		return a.provisionModelGrantTx(ctx, tx)
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	if e != nil {
		return fmt.Errorf("install development governance: %w", e)
	}
	// Management reuses the original demo creation; restarting never resets it.
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: a.Scope.OwnerID, CommandID: platform.StableDevelopmentID("command", "development-surface-session"), Method: "session.create", TargetID: a.Scope.OwnerID, ExpiresAt: a.Config.PolicyExpiresAt, Payload: api.Raw(interaction.CreateSessionInput{SessionID: platform.StableDevelopmentID("session", "development-surface"), DefaultBranchID: platform.StableDevelopmentID("branch", "development-surface"), ConfigRef: component("development-surface-session")})}
	r, e := a.Dispatcher.Command(ctx, a.UserAuth, api.Raw(c))
	if e != nil {
		return e
	}
	if r.Error != nil {
		return r.Error
	}
	return nil
}
func (a *App) Close() error {
	var err error
	if a.endpointRouter != nil {
		err = errors.Join(err, a.endpointRouter.Close())
		a.endpointRouter = nil
	}
	err = errors.Join(err, a.remoteExecutors.close())
	for _, source := range a.Information {
		err = errors.Join(err, source.Close())
	}
	a.Information = nil
	if a.WASI != nil {
		if closeErr := a.WASI.Close(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		a.WASI = nil
	}
	if a.closeGovernance != nil {
		if closeErr := a.closeGovernance(); closeErr != nil {
			return errors.Join(err, closeErr)
		}
		a.closeGovernance = nil
	}
	if a.Model != nil {
		err = errors.Join(err, a.Model.Close())
		a.Model = nil
	}
	if a.remoteAgents != nil {
		err = errors.Join(err, a.remoteAgents.close())
		a.remoteAgents = nil
	}
	if a.Files != nil {
		err = errors.Join(err, a.Files.Close())
		a.Files = nil
	}
	if a.Phones != nil {
		err = errors.Join(err, a.Phones.Close())
		a.Phones = nil
	}
	if a.Store != nil {
		err = errors.Join(err, a.Store.Close())
		a.Store = nil
	}
	return err
}

func containsString(values []string, expected string) bool {
	for _, value := range values {
		if value == expected {
			return true
		}
	}
	return false
}
func (a *App) Gateway() (*wss.Server, error) {
	var processor wss.Processor = wss.LocalProcessor{Dispatcher: a.Dispatcher}
	if a.Role == "gateway" && a.endpointRouter != nil {
		processor = a.endpointRouter
	} else if a.Role == "gateway" {
		forward, err := rpcadapter.NewForwardProcessor("grpc://"+a.Config.GRPCAddr, a.Config.OwnerID, a.Registry.Contracts(), a.forwardCredential, a.Config.Development)
		if err != nil {
			return nil, err
		}
		processor = forward
	}
	return wss.New(wss.Config{OwnerID: a.Config.OwnerID, Store: a.Store, Registry: a.Registry, Identity: a.Identity, Processor: processor, Content: a.Memory, Uploader: a.Memory, Development: &wss.DevelopmentConfiguration{TenantID: a.Config.TenantID, ContentPolicyRef: a.ContentPolicy.PolicyRef, TaskPolicyRef: a.TaskPolicy.PolicyRef, Budget: []api.Amount{{Unit: "USD", Value: "20"}}, GoalSchema: brain.GoalSchema(), RetentionSeconds: 86400, TaskDeadlineSeconds: 1800, ApplicationBindingRef: &a.ApplicationBinding, ApplicationEvents: []wss.DevelopmentEvent{{Name: "archive_demo_session", Schema: a.ApplicationEventSchema, RequiresRendered: true}}}, Origins: a.Config.Origins, AllowInsecureLoopback: a.Config.Development, StaticDir: a.Config.StaticDir, Location: "cloud", MaxConnections: 128, MaxQueuedBytes: 64 << 20})
}
