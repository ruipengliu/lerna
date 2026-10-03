package development

import (
	"context"
	"sort"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/providers"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const maxActionBindings = 16
const maxActionSchemaBytes = 96 << 10

// ActionBindingConfig 是受信开发宿主的显式装配，不是模型可自行提供的授权。
// initialize 只导入原ID许可；重启不会重新创建撤回或已消费的许可。
type ActionBindingConfig struct {
	CapabilityRef  api.ComponentRef `json:"capability_ref"`
	BindingRef     api.ObjectRef    `json:"binding_ref"`
	InstallLockRef api.ComponentRef `json:"install_lock_ref"`
	Grant          api.Grant        `json:"grant"`
	Cell           *WASICellConfig  `json:"cell,omitempty"`
}

type actionDescriptor struct {
	Kind              string                                 `json:"kind"`
	Capability        execution.Capability                   `json:"capability"`
	BindingRef        api.ObjectRef                          `json:"binding_ref"`
	InstallLockRef    api.ComponentRef                       `json:"install_lock_ref"`
	GrantRef          api.ObjectRef                          `json:"grant_ref"`
	Resources         []string                               `json:"resources"`
	Actions           []string                               `json:"actions"`
	Recipient         string                                 `json:"recipient"`
	Location          string                                 `json:"location"`
	ConfigHash        string                                 `json:"config_hash"`
	ConfiguredGrant   *api.Grant                             `json:"configured_grant,omitempty"`
	Source            *providers.InformationSourceDescriptor `json:"source,omitempty"`
	SourceRetainUntil string                                 `json:"source_retain_until,omitempty"`
	ExecutorID        string                                 `json:"executor_id,omitempty"`
	RemoteConfigHash  string                                 `json:"remote_config_hash,omitempty"`
	Cell              *WASICellConfig                        `json:"cell,omitempty"`
	PreparedCell      *wasiCellDeclaration                   `json:"prepared_cell,omitempty"`
}

type actionRegistry struct{ entries []actionDescriptor }

type actionDeclaration struct {
	Capability       execution.Capability          `json:"capability"`
	BindingRef       api.ObjectRef                 `json:"binding_ref"`
	InstallLockRef   api.ComponentRef              `json:"install_lock_ref"`
	AllowedResources []string                      `json:"allowed_resources"`
	AllowedActions   []string                      `json:"allowed_actions"`
	Source           *informationSourceDeclaration `json:"source,omitempty"`
	Cell             *wasiCellDeclaration          `json:"cell,omitempty"`
}

type informationSourceDeclaration struct {
	SourceRef         api.ComponentRef `json:"source_ref"`
	Origin            string           `json:"origin"`
	SearchPath        string           `json:"search_path,omitempty"`
	FetchPathPrefixes []string         `json:"fetch_path_prefixes"`
	MaxQueryBytes     uint64           `json:"max_query_bytes"`
	MaxItems          uint64           `json:"max_items"`
}

func (v actionSnapshot) declarations() []actionDeclaration {
	out := []actionDeclaration{}
	for _, d := range v.Entries {
		declaration := actionDeclaration{Capability: d.Capability, BindingRef: d.BindingRef, InstallLockRef: d.InstallLockRef, AllowedResources: d.Resources, AllowedActions: d.Actions, Cell: d.PreparedCell}
		if d.Source != nil {
			s := d.Source
			declaration.Source = &informationSourceDeclaration{s.SourceRef, s.Origin, s.SearchPath, s.FetchPathPrefixes, s.MaxQueryBytes, s.MaxItems}
		}
		out = append(out, declaration)
	}
	return out
}

type actionSnapshot struct {
	Scope          runtime.Scope      `json:"scope"`
	SnapshotID     string             `json:"snapshot_id"`
	InstallLockRef api.ComponentRef   `json:"install_lock_ref"`
	Entries        []actionDescriptor `json:"entries"`
}

// actionAdmission 的作者仅为读过原Snapshot/Proposal/Arguments的受信编译器。
// Tx授权只接受该不可变行与PreparedAction的完整相等，不相信caller自报资源。
type actionAdmission struct {
	Scope      runtime.Scope       `json:"scope"`
	DecisionID string              `json:"decision_id"`
	SnapshotID string              `json:"snapshot_id"`
	Descriptor actionDescriptor    `json:"descriptor"`
	Prepared   task.PreparedAction `json:"prepared"`
	Resources  []string            `json:"resources"`
	Actions    []string            `json:"actions"`
}

func (a *App) configureActionRegistry(drivers []execution.Driver) error {
	if !a.Config.Development {
		return api.E("unsupported", "development_action_configuration_required")
	}
	if len(a.Config.ActionBindings) > maxActionBindings-2 {
		return api.E("invalid_request", "action_binding_limit")
	}
	r := &actionRegistry{}
	fileLock := component("builtin-install-lock")
	for _, entry := range []struct {
		kind    string
		cap     execution.Capability
		binding api.ObjectRef
		action  string
	}{{"file.read", target.FileReadCapability(), a.ReadBinding, "file.read"}, {"file.write", target.FileWriteCapability(), a.WriteBinding, "file.write"}} {
		d := actionDescriptor{Kind: entry.kind, Capability: entry.cap, BindingRef: entry.binding, InstallLockRef: fileLock, GrantRef: a.Scope.Ref(a.GrantID, 1), Resources: []string{"managed-files"}, Actions: []string{entry.action}, Recipient: a.Scope.OwnerID, Location: "cloud"}
		var err error
		d.ConfigHash, err = api.Digest(d)
		if err != nil {
			return err
		}
		r.entries = append(r.entries, d)
	}
	for _, configured := range a.Config.ActionBindings {
		var frozen ActionBindingConfig
		if err := api.Decode(api.Raw(configured), &frozen); err != nil {
			return err
		}
		configured = frozen
		if configured.Cell != nil && !api.Equal(configured.CapabilityRef, execution.WASIRunCellCapability().Ref) {
			return api.E("invalid_request", "cell_configuration_requires_wasi_capability")
		}
		if err := runtime.CheckRef(a.Scope, configured.BindingRef); err != nil {
			return err
		}
		if err := api.ValidateRecord("ComponentRef", configured.InstallLockRef); err != nil {
			return err
		}
		if err := api.ValidateRecord("Grant", configured.Grant); err != nil {
			return err
		}
		g := configured.Grant
		if g.OwnerID != a.Scope.OwnerID || g.Revision != 1 || g.State != "active" || !api.Equal(g.SubjectRef, a.ServiceAuth.Ref(a.Scope.OwnerID)) || len(g.Resources) == 0 || len(g.Resources) > 32 || len(g.Actions) == 0 || len(g.Actions) > 8 || len(g.Purposes) != 1 || g.Purposes[0] != "goal_action" || len(g.Recipients) != 1 || len(g.Locations) != 1 {
			return api.E("invalid_request", "action_grant_configuration_invalid")
		}
		var cap execution.Capability
		found := false
		for _, driver := range drivers {
			if api.Equal(driver.Capability().Ref, configured.CapabilityRef) {
				cap = driver.Capability()
				found = true
				break
			}
		}
		if !found {
			return api.E("unsupported", "action_driver_not_configured")
		}
		d := actionDescriptor{Capability: cap, BindingRef: configured.BindingRef, InstallLockRef: configured.InstallLockRef, GrantRef: a.Scope.Ref(g.GrantID, 1), Resources: append([]string{}, g.Resources...), Actions: append([]string{}, g.Actions...), Recipient: g.Recipients[0], Location: g.Locations[0], ConfiguredGrant: &g, Cell: configured.Cell}
		if configured.BindingRef.OwnerID != a.Scope.OwnerID && (api.Equal(cap.Ref, target.FileReadCapability().Ref) || api.Equal(cap.Ref, target.FileWriteCapability().Ref)) {
			route, err := a.remoteExecutors.binding(d.BindingRef, cap.Ref, d.InstallLockRef)
			if err != nil {
				return err
			}
			d.Kind = "file.read"
			if api.Equal(cap.Ref, target.FileWriteCapability().Ref) {
				d.Kind = "file.write"
			}
			if d.Recipient != route.Config.OwnerID || d.Location != "device" || !api.Equal(d.Resources, []string{"managed-files"}) || !api.Equal(d.Actions, []string{d.Kind}) {
				return api.E("forbidden", "device_grant_scope_mismatch")
			}
			d.ExecutorID, d.RemoteConfigHash = route.Config.OwnerID, route.Digest
		} else if api.Equal(cap.Ref, execution.WASIRunCellCapability().Ref) {
			if err := a.validateWASICellConfiguration(d); err != nil {
				return err
			}
			d.Kind = "environment.run_cell"
		} else if api.Equal(cap.Ref, target.PhoneGUICapability().Ref) {
			d.Kind = "phone.gui"
			if d.Recipient != a.Scope.OwnerID || d.Location != "cloud" {
				return api.E("invalid_request", "action_grant_configuration_invalid")
			}
			if !api.Equal(d.InstallLockRef, fileLock) {
				return api.E("forbidden", "action_install_lock_mismatch")
			}
			for _, action := range d.Actions {
				if !containsString([]string{"gui.click", "gui.swipe", "gui.input", "gui.back"}, action) {
					return api.E("invalid_request", "gui_action_scope_invalid")
				}
			}
			for _, id := range d.Resources {
				if !api.ValidID(id) || !containsString(developmentPhoneIDs(), id) {
					return api.E("invalid_request", "gui_resource_scope_invalid")
				}
			}
		} else {
			matched := false
			for _, source := range a.information {
				for _, driver := range source.Source.Drivers() {
					if !api.Equal(driver.Capability().Ref, cap.Ref) {
						continue
					}
					matched = true
					d.Kind = providers.InformationBody
					if source.Source.SearchDriver() != nil && api.Equal(source.Source.SearchDriver().Capability().Ref, cap.Ref) {
						d.Kind = providers.InformationSearch
					}
					lock, err := InformationInstallLock(source.Source)
					if err != nil {
						return err
					}
					if !api.Equal(lock, d.InstallLockRef) {
						return api.E("forbidden", "action_install_lock_mismatch")
					}
					s := source.Source.Descriptor()
					if len(d.Resources) != 1 || d.Resources[0] != "source:"+s.SourceRef.ComponentID || len(d.Actions) != 1 || d.Actions[0] != d.Kind || d.Recipient != s.Receiver || d.Location != s.Location {
						return api.E("invalid_request", "information_action_scope_invalid")
					}
					d.Source = &s
					d.SourceRetainUntil = source.Config.RetainUntil
				}
			}
			if !matched {
				return api.E("unsupported", "action_projection_not_configured")
			}
		}
		for _, existing := range r.entries {
			if api.Equal(existing.BindingRef, d.BindingRef) {
				return api.E("invalid_request", "duplicate_action_binding")
			}
		}
		var err error
		d.ConfigHash, err = api.Digest(struct {
			Scope         runtime.Scope                          `json:"scope"`
			Configuration ActionBindingConfig                    `json:"configuration"`
			Source        *providers.InformationSourceDescriptor `json:"source,omitempty"`
			RetainUntil   string                                 `json:"retain_until,omitempty"`
		}{a.Scope, configured, d.Source, d.SourceRetainUntil})
		if err != nil {
			return err
		}
		r.entries = append(r.entries, d)
	}
	sort.SliceStable(r.entries, func(i, j int) bool { return r.entries[i].BindingRef.ObjectID < r.entries[j].BindingRef.ObjectID })
	total := 0
	for _, d := range r.entries {
		if _, err := api.NewValidator(d.Capability.InputSchema); err != nil {
			return err
		}
		total += len(api.Raw(d.Capability.InputSchema)) + len(api.Raw(d.Capability.OutputSchema))
	}
	if total > maxActionSchemaBytes || len(api.Raw(r.entries)) > api.MaxJSONBytes {
		return api.E("invalid_request", "action_schema_limit")
	}
	a.actions = r
	if len(a.Config.ActionBindings) > 0 {
		a.InstallLock = component("development-action-assembly")
		var err error
		a.InstallLock.Digest, err = api.Digest(r.entries)
		if err != nil {
			return err
		}
	}
	return nil
}

func developmentPhoneIDs() []string {
	return []string{platform.StableDevelopmentID("resource", "phone-one"), platform.StableDevelopmentID("resource", "phone-two"), platform.StableDevelopmentID("resource", "phone-three")}
}

func (a *App) provisionActionGrantsTx(ctx context.Context, tx runtime.Tx) error {
	for _, d := range a.actions.entries {
		if d.ConfiguredGrant == nil {
			continue
		}
		exists, err := a.Governance.GrantExistsTx(ctx, tx, a.ServiceAuth, d.GrantRef.ObjectID)
		if err != nil {
			return err
		}
		if !exists {
			if err = a.Governance.ProvisionGrantTx(ctx, tx, a.ServiceAuth, *d.ConfiguredGrant); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *App) prepareActionSnapshot(ctx context.Context, id string, deadline string) (actionSnapshot, error) {
	var original actionSnapshot
	_, err := a.Store.Read(ctx, a.Scope, "platform.action_snapshots", id, 1, &original)
	if err == nil {
		return original, nil
	}
	if !api.IsCode(err, "not_found") {
		return original, err
	}
	view := actionSnapshot{Scope: a.Scope, SnapshotID: id, InstallLockRef: a.InstallLock, Entries: []actionDescriptor{}}
	now, err := a.now(ctx, a.Scope)
	if err != nil {
		return view, err
	}
	for _, d := range a.actions.entries {
		if d.Kind == "phone.gui" && !a.UserAuth.HasRole("device_controller") && !a.UserAuth.HasRole("executor") && !a.UserAuth.HasRole("admin") {
			continue
		}
		if d.Source != nil {
			if _, err := a.configuredInformation(d.Source.SourceRef); err != nil {
				if api.IsCode(err, "unsupported") {
					continue
				}
				return view, err
			}
			until, err := api.ParseTime(d.SourceRetainUntil)
			if err != nil {
				return view, err
			}
			if !now.Before(until) {
				continue
			}
		}
		in := governance.UseRequest{UseID: stableID("use", "context/"+id+"/"+d.BindingRef.ObjectID), SubjectRef: a.ServiceAuth.Ref(a.Scope.OwnerID), TargetRef: a.Scope.Ref(id, 1), TargetKind: "operation", IntentHash: d.ConfigHash, GrantRefs: []api.ObjectRef{d.GrantRef}, RequestedUnits: []api.Amount{}, Resources: d.Resources, Actions: d.Actions, Recipient: d.Recipient, Location: d.Location, Purposes: []string{"goal_action"}, StartBefore: deadline}
		raw, err := a.query(ctx, "grant.check", a.Scope.OwnerID, in)
		if api.IsCode(err, "not_found") || api.IsCode(err, "forbidden") {
			continue
		}
		if err != nil {
			return view, err
		}
		var receipt governance.UseReceipt
		if err = api.Decode(raw, &receipt); err != nil {
			return view, err
		}
		if receipt.Decision == "allowed" {
			if d.Kind == "environment.run_cell" {
				declaration, err := a.freezeWASICell(ctx, a.Scope, d)
				if err != nil {
					return view, err
				}
				d.PreparedCell = &declaration
			}
			view.Entries = append(view.Entries, d)
		}
	}
	status, err := a.Store.Within(ctx, a.Scope, []string{"platform"}, func(tx runtime.Tx) error {
		var fixed actionSnapshot
		_, err := tx.Get(ctx, "platform.action_snapshots", id, &fixed)
		if err == nil {
			original = fixed
			return nil
		}
		if !api.IsCode(err, "not_found") {
			return err
		}
		original = view
		return tx.Create(ctx, "platform.action_snapshots", id, "", view)
	})
	if status == runtime.CommitUnknown {
		return view, runtime.ErrCommitUnknown
	}
	return original, err
}

func (a *App) prepareAction(ctx context.Context, s runtime.Scope, i api.DecisionDispatchIntent, snap api.Snapshot, candidate brain.ActionCandidate, reqs []api.RequirementRef) (task.PreparedAction, actionAdmission, error) {
	var fixed actionSnapshot
	_, err := a.Store.Read(ctx, s, "platform.action_snapshots", snap.SnapshotID, 1, &fixed)
	if api.IsCode(err, "not_found") && api.Equal(snap.InstallLockRef, component("builtin-install-lock")) {
		// 旧File Snapshot有原明确声明但没有新登记快照；只解释原两个File pair。
		fixed = actionSnapshot{Scope: s, SnapshotID: snap.SnapshotID, InstallLockRef: snap.InstallLockRef}
		for _, d := range a.actions.entries {
			if d.ConfiguredGrant == nil {
				fixed.Entries = append(fixed.Entries, d)
			}
		}
	} else if err != nil {
		return task.PreparedAction{}, actionAdmission{}, err
	}
	if !api.Equal(fixed.Scope, s) || fixed.SnapshotID != snap.SnapshotID {
		return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "original_action_snapshot_mismatch")
	}
	if err := a.Knowledge.CheckActionSnapshotLock(ctx, s, a.ServiceAuth, i, snap, fixed.InstallLockRef); err != nil {
		return task.PreparedAction{}, actionAdmission{}, err
	}
	var descriptor actionDescriptor
	matched := false
	for _, d := range fixed.Entries {
		if api.Equal(d.Capability.Ref, candidate.CapabilityRef) && api.Equal(d.BindingRef, candidate.BindingRef) {
			descriptor = d
			matched = true
			break
		}
	}
	if !matched {
		return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "action_pair_not_declared")
	}
	declared := false
	for n, cap := range snap.CapabilityRefs {
		if api.Equal(cap, descriptor.Capability.Ref) && n < len(snap.BindingRefs) && api.Equal(snap.BindingRefs[n], descriptor.BindingRef) {
			declared = true
		}
	}
	if !declared {
		return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "action_pair_not_declared")
	}
	args, err := a.ReadContent(ctx, s, a.ServiceAuth, candidate.ArgumentsRef, "execution.arguments")
	if err != nil {
		return task.PreparedAction{}, actionAdmission{}, err
	}
	validator, err := api.NewValidator(descriptor.Capability.InputSchema)
	if err != nil {
		return task.PreparedAction{}, actionAdmission{}, err
	}
	if err = validator.Validate(args); err != nil {
		return task.PreparedAction{}, actionAdmission{}, err
	}
	resourceRefs := []api.ObjectRef{}
	resources := []string{}
	resourceKeys := []string{}
	actions := []string{}
	executorID := s.OwnerID
	if descriptor.ExecutorID != "" {
		route, err := a.remoteExecutors.binding(descriptor.BindingRef, descriptor.Capability.Ref, descriptor.InstallLockRef)
		if err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		if descriptor.RemoteConfigHash != route.Digest || descriptor.ExecutorID != route.Config.OwnerID {
			return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "original_device_route_changed")
		}
		executorID = descriptor.ExecutorID
	}
	costBound := []api.Amount{{Unit: "USD", Value: "0"}}
	switch descriptor.Kind {
	case "file.read":
		var input target.FileReadArguments
		if err = api.Decode(args, &input); err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		resourceKeys = []string{"file:" + input.Path}
		resources = []string{"managed-files"}
		actions = []string{"file.read"}
	case "file.write":
		var input target.FileWriteArguments
		if err = api.Decode(args, &input); err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		resourceKeys = []string{"file:" + input.Path}
		resources = []string{"managed-files"}
		actions = []string{"file.write"}
	case "phone.gui":
		var input target.PhoneGUIArguments
		if err = api.Decode(args, &input); err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		if !containsString(descriptor.Resources, input.ResourceID) || !containsString(descriptor.Actions, "gui."+input.Action) {
			return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "action_scope_not_configured")
		}
		raw, err := a.queryAs(ctx, a.UserAuth, "resource.observation.get", input.ObservationID, execution.ObservationIDInput{ObservationID: input.ObservationID})
		if err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		var out execution.ObserveOutput
		if err = api.Decode(raw, &out); err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		if !out.Ready || out.Observation == nil {
			return task.PreparedAction{}, actionAdmission{}, api.E("dependency_unavailable", "original_observation_not_ready")
		}
		ob := out.Observation
		if ob.ResourceRef.ObjectID != input.ResourceID || ob.InstanceID != input.InstanceID || ob.ControlEpoch != input.ControlEpoch || ob.TargetVersion != input.TargetVersion || ob.ActionBefore != input.ActionBefore {
			return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "original_observation_mismatch")
		}
		resourceRefs = []api.ObjectRef{ob.ResourceRef}
		resourceKeys = []string{"device:" + input.ResourceID}
		resources = []string{input.ResourceID}
		actions = []string{"gui." + input.Action}
	case "environment.run_cell":
		if descriptor.PreparedCell == nil || descriptor.Cell == nil || a.WASI == nil {
			return task.PreparedAction{}, actionAdmission{}, api.E("unsupported", "original_wasi_cell_not_configured")
		}
		var input execution.ComputeArguments
		if err = api.Decode(args, &input); err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		if !api.Equal(input, descriptor.PreparedCell.Arguments) {
			return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "original_wasi_cell_arguments_changed")
		}
		costBound = descriptor.PreparedCell.CostBound
		if err = a.prepareWASICell(ctx, s, snap, descriptor, candidate, args); err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		resourceRefs = []api.ObjectRef{input.EnvironmentRef}
		resources = []string{"environment:" + input.EnvironmentRef.ObjectID}
		resourceKeys = append([]string{}, resources...)
		actions = []string{"environment.run_cell"}
	case providers.InformationSearch, providers.InformationBody:
		if descriptor.Source == nil {
			return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "information_source_snapshot_missing")
		}
		source, err := a.configuredInformation(descriptor.Source.SourceRef)
		if err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		var driver execution.Driver
		if descriptor.Kind == providers.InformationSearch {
			driver = source.Source.SearchDriver()
		} else {
			driver = source.Source.BodyDriver()
		}
		if driver == nil || !api.Equal(driver.Capability().Ref, descriptor.Capability.Ref) {
			return task.PreparedAction{}, actionAdmission{}, api.E("unsupported", "original_information_driver_not_configured")
		}
		opID := stableID("operation", i.DecisionID+"/"+candidate.LocalKey)
		intent := execution.ExecutionIntent{OperationID: opID, TaskRef: snap.TaskRef, CapabilityRef: candidate.CapabilityRef, ArgumentsRef: candidate.ArgumentsRef, ProcessedSourceRefs: candidate.ProcessedSourceRefs, DisclosedSourceRefs: candidate.DisclosedSourceRefs}
		// 真实驱动的纯准备验证准确 typed 参数、源 URL 或 query 许可；没有 DNS/HTTP。
		if _, err = driver.Prepare(ctx, s, a.ServiceAuth, execution.InvokeInput{OperationID: opID, TaskRef: snap.TaskRef, CapabilityRef: candidate.CapabilityRef}, intent, args); err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		resources = []string{"source:" + descriptor.Source.SourceRef.ComponentID}
		resourceKeys = append([]string{}, resources...)
		actions = []string{descriptor.Kind}
	default:
		return task.PreparedAction{}, actionAdmission{}, api.E("unsupported", "action_projection_not_configured")
	}
	if executorID != s.OwnerID {
		for n, key := range resourceKeys {
			resourceKeys[n] = "executor:" + executorID + "/" + key
		}
	}
	if descriptor.ConfiguredGrant != nil {
		raw, err := a.query(ctx, "grant.check", s.OwnerID, governance.UseRequest{UseID: stableID("use", "prepare/"+i.DecisionID+"/"+candidate.LocalKey), SubjectRef: a.ServiceAuth.Ref(s.OwnerID), TargetRef: s.Ref(stableID("operation", i.DecisionID+"/"+candidate.LocalKey), 1), TargetKind: "operation", IntentHash: descriptor.ConfigHash, GrantRefs: []api.ObjectRef{descriptor.GrantRef}, RequestedUnits: costBound, Resources: resources, Actions: actions, Recipient: descriptor.Recipient, Location: descriptor.Location, Purposes: []string{"goal_action"}, StartBefore: descriptor.ConfiguredGrant.ExpiresAt})
		if err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		var checked governance.UseReceipt
		if err = api.Decode(raw, &checked); err != nil {
			return task.PreparedAction{}, actionAdmission{}, err
		}
		if checked.Decision != "allowed" {
			return task.PreparedAction{}, actionAdmission{}, api.E("forbidden", "action_current_grant_denied")
		}
	}
	resourceContent, err := a.Publish(ctx, s, a.ServiceAuth, stableID("content", "resources/"+i.DecisionID+"/"+candidate.LocalKey), "application/json", api.Raw(resourceRefs), candidate.ProcessedSourceRefs, []api.ContentRef{})
	if err != nil {
		return task.PreparedAction{}, actionAdmission{}, err
	}
	prepared := task.PreparedAction{OperationID: stableID("operation", i.DecisionID+"/"+candidate.LocalKey), ExecutorID: executorID, CapabilityRef: candidate.CapabilityRef, BindingRef: candidate.BindingRef, InstallLockRef: descriptor.InstallLockRef, ArgumentsRef: candidate.ArgumentsRef, ResourcesRef: resourceContent, RequirementRefs: reqs, UseIntentRefs: []api.ObjectRef{s.Ref(stableID("use", i.DecisionID+"/"+candidate.LocalKey), 1)}, CostBound: costBound, LogicalStepKey: snap.TaskRef.ObjectID + "/" + candidate.LocalKey, ProcessedSourceRefs: candidate.ProcessedSourceRefs, DisclosedSourceRefs: candidate.DisclosedSourceRefs, ResourceKeys: resourceKeys, Independent: true, SafeRequirementCheck: false, CommandID: stableID("command", "invoke/"+i.DecisionID+"/"+candidate.LocalKey)}
	return prepared, actionAdmission{Scope: s, DecisionID: i.DecisionID, SnapshotID: snap.SnapshotID, Descriptor: descriptor, Prepared: prepared, Resources: resources, Actions: actions}, nil
}

func (a *App) readActionAdmissionTx(ctx context.Context, tx runtime.Tx, i task.OperationIntent) (actionAdmission, error) {
	var admission actionAdmission
	_, err := tx.Get(ctx, "platform.action_admissions", i.OperationID, &admission)
	if api.IsCode(err, "not_found") {
		// 已接受的旧File意图仍按固定旧leaf lock解释；新能力不能采用该分支。
		for _, d := range a.actions.entries {
			if d.ConfiguredGrant == nil && api.Equal(d.Capability.Ref, i.CapabilityRef) && api.Equal(d.BindingRef, i.BindingRef) && api.Equal(d.InstallLockRef, i.InstallLockRef) {
				var original task.Proposal
				if i.AdmissionSourceKind != "decision" {
					return admission, api.E("forbidden", "original_action_admission_missing")
				}
				if _, err := tx.Get(ctx, "platform.prepared_proposals", i.AdmissionSourceRef.ObjectID, &original); err != nil {
					return admission, err
				}
				matched := false
				for _, prepared := range original.Actions {
					matched = matched || api.Equal(prepared, i.PreparedAction)
				}
				if !matched {
					return admission, api.E("forbidden", "original_action_admission_mismatch")
				}
				return actionAdmission{Scope: tx.Scope(), Descriptor: d, Prepared: i.PreparedAction, Resources: d.Resources, Actions: d.Actions}, nil
			}
		}
		return admission, api.E("forbidden", "original_action_admission_missing")
	}
	if err != nil {
		return admission, err
	}
	if !api.Equal(admission.Scope, tx.Scope()) || !api.Equal(admission.Prepared, i.PreparedAction) {
		return admission, api.E("forbidden", "original_action_admission_mismatch")
	}
	return admission, nil
}
