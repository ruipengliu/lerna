package collaboration_test

import (
	"fmt"
	"testing"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 仅验证闭合配置/Schema；不是已获实际Grant、SQL或远端账单的证据。
func TestRemoteActionProfileBindsGrantNamespaceToExactResourceVersion(t *testing.T) {
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: api.NewID("database")}
	receiver := api.NewID("owner")
	capability, resource := remoteComponent("capability"), remoteComponent("resource")
	binding := api.ObjectRef{TenantID: scope.TenantID, OwnerID: receiver, ObjectID: api.NewID("binding"), Revision: 1}
	values := collaboration.RemoteAgentValues{ParentOwnerID: scope.OwnerID, ReceiverID: receiver, AgentBindingRef: scope.Ref(api.NewID("binding"), 1), PolicyRef: remoteComponent("policy"), InstallLockRef: remoteComponent("lock"), SubjectRefs: []api.ObjectRef{scope.Ref(api.NewID("subject"), 1)}, PermissionRefs: []api.ObjectRef{scope.Ref(api.NewID("grant"), 1)}, CapabilityRefs: []api.ComponentRef{capability}, BindingRefs: []api.ObjectRef{binding}, ResourceRefs: []api.ComponentRef{resource}, BudgetLimits: []api.Amount{{Unit: "USD", Value: "2"}}, MaxDepth: 4, MaxInputs: 4, Location: "cloud", MaterialPurposes: []string{}, ActionScopes: []collaboration.RemoteActionScope{{CapabilityRef: capability, BindingRef: binding, Resources: []string{"managed-files"}, ResourceRefs: []api.ComponentRef{resource}, Actions: []string{"file.read"}, Recipient: receiver, Location: "cloud"}}}
	profile, err := collaboration.NewRemoteAgentProfile(api.NewID("agent"), "1.0.0", values)
	if err != nil {
		t.Fatalf("explicit legal namespace/resource pair: %v", err)
	}
	for _, test := range []struct {
		name   string
		change func(*collaboration.RemoteAgentValues)
	}{
		{"missing accurate pair", func(v *collaboration.RemoteAgentValues) { v.ActionScopes[0].ResourceRefs = []api.ComponentRef{} }},
		{"undeclared resource version", func(v *collaboration.RemoteAgentValues) {
			v.ActionScopes[0].ResourceRefs[0].Digest = api.Hash([]byte("different resource version"))
		}},
		{"same component rebound to another namespace", func(v *collaboration.RemoteAgentValues) {
			v.ActionScopes[0].Resources = append(v.ActionScopes[0].Resources, "other-files")
			v.ActionScopes[0].ResourceRefs = append(v.ActionScopes[0].ResourceRefs, resource)
		}},
		{"same namespace with two different resource versions", func(v *collaboration.RemoteAgentValues) {
			other := resource
			other.Digest = api.Hash([]byte("other fixed resource"))
			v.ResourceRefs = append(v.ResourceRefs, other)
			otherBinding := binding
			otherBinding.ObjectID = api.NewID("binding")
			v.BindingRefs = append(v.BindingRefs, otherBinding)
			v.ActionScopes = append(v.ActionScopes, collaboration.RemoteActionScope{CapabilityRef: capability, BindingRef: otherBinding, Resources: []string{"managed-files"}, ResourceRefs: []api.ComponentRef{other}, Actions: []string{"file.read"}, Recipient: receiver, Location: "cloud"})
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var changed collaboration.RemoteAgentValues
			if err := api.Decode(api.Raw(profile.Values), &changed); err != nil {
				t.Fatal(err)
			}
			test.change(&changed)
			if _, err := collaboration.NewRemoteAgentProfile(profile.ProfileRef.ComponentID, profile.ProfileRef.Version, changed); !api.IsCode(err, "forbidden") {
				t.Fatalf("unbound resource granted action scope: %v", err)
			}
		})
	}
	legacy := profile.Values
	legacy.ActionScopes = nil
	legacy.PermissionRefs, legacy.CapabilityRefs, legacy.BindingRefs, legacy.ResourceRefs = []api.ObjectRef{}, []api.ComponentRef{}, []api.ObjectRef{}, []api.ComponentRef{}
	if _, err = collaboration.NewRemoteAgentProfile(profile.ProfileRef.ComponentID, profile.ProfileRef.Version, legacy); err != nil {
		t.Fatalf("old metadata/cleanup profile stopped working: %v", err)
	}
	changed := profile.Values
	changed.ActionScopes = append([]collaboration.RemoteActionScope{}, changed.ActionScopes...)
	changed.ActionScopes[0].Resources = []string{"other-fixed-files"}
	newProfile, err := collaboration.NewRemoteAgentProfile(profile.ProfileRef.ComponentID, profile.ProfileRef.Version, changed)
	if err != nil || newProfile.ProfileRef.Digest == profile.ProfileRef.Digest {
		t.Fatalf("profile digest did not bind exact namespace: %+v %v", newProfile, err)
	}
	validator, err := api.NewValidator(api.SchemaFor[collaboration.RemoteActionScope]())
	if err != nil || validator.Validate(api.Raw(profile.Values.ActionScopes[0])) != nil {
		t.Fatalf("closed method nested resource scope: %v", err)
	}
	var untrusted map[string]any
	if err = api.Decode(api.Raw(profile.Values.ActionScopes[0]), &untrusted); err != nil {
		t.Fatal(err)
	}
	delete(untrusted, "resource_refs")
	if validator.Validate(api.Raw(untrusted)) == nil {
		t.Fatal("method Schema allowed missing resource reference array")
	}
	untrusted["resource_refs"] = []api.ComponentRef{resource}
	untrusted["allow_all"] = true
	if validator.Validate(api.Raw(untrusted)) == nil {
		t.Fatal("method Schema allowed undeclared permission field")
	}
}

// 配置边界证据，不把显式32用途配置当作已取得32项 source 许可。
func TestRemoteMaterialPurposeProfileHasFiniteExplicitBound(t *testing.T) {
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: api.NewID("database")}
	values := collaboration.RemoteAgentValues{ParentOwnerID: scope.OwnerID, ReceiverID: api.NewID("owner"), AgentBindingRef: scope.Ref(api.NewID("binding"), 1), PolicyRef: remoteComponent("policy"), InstallLockRef: remoteComponent("lock"), SubjectRefs: []api.ObjectRef{scope.Ref(api.NewID("subject"), 1)}, PermissionRefs: []api.ObjectRef{}, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, ResourceRefs: []api.ComponentRef{}, BudgetLimits: []api.Amount{{Unit: "USD", Value: "2"}}, MaxDepth: 4, MaxInputs: 4, Location: "cloud", MaterialPurposes: []string{}}
	for i := 0; i < 16; i++ {
		values.MaterialPurposes = append(values.MaterialPurposes, fmt.Sprintf("declared.%d", i))
	}
	old, err := collaboration.NewRemoteAgentProfile(api.NewID("agent"), "1.0.0", values)
	if err != nil {
		t.Fatal(err)
	}
	var full collaboration.RemoteAgentValues
	if err = api.Decode(api.Raw(old.Values), &full); err != nil {
		t.Fatal(err)
	}
	full.MaterialPurposes = append(full.MaterialPurposes, "execution_result")
	for len(full.MaterialPurposes) < 32 {
		full.MaterialPurposes = append(full.MaterialPurposes, fmt.Sprintf("declared.%d", len(full.MaterialPurposes)))
	}
	profile, err := collaboration.NewRemoteAgentProfile(old.ProfileRef.ComponentID, old.ProfileRef.Version, full)
	if err != nil || profile.ProfileRef.Digest == old.ProfileRef.Digest || len(old.Values.MaterialPurposes) != 16 {
		t.Fatalf("explicit complete profile altered old identity: %+v %v", profile, err)
	}
	validator, err := api.NewValidator(collaboration.RemoteAgentProfileSchema())
	if err != nil || validator.Validate(api.Raw(profile)) != nil || validator.Validate(api.Raw(old)) != nil {
		t.Fatalf("closed bounded profile rejected declared32/legacy16: %v", err)
	}
	legacy := old.Values
	legacy.MaterialPurposes = nil
	legacy.PermissionRefs, legacy.CapabilityRefs, legacy.BindingRefs, legacy.ResourceRefs = nil, nil, nil, nil
	legacyProfile, legacyErr := collaboration.NewRemoteAgentProfile(old.ProfileRef.ComponentID, old.ProfileRef.Version, legacy)
	if legacyErr != nil || validator.Validate(api.Raw(legacyProfile)) != nil || legacyProfile.Values.MaterialPurposes != nil {
		t.Fatalf("legacy metadata-only profile gained or lost purpose identity: %+v %v", legacyProfile, legacyErr)
	}
	profile.Values.MaterialPurposes = append(profile.Values.MaterialPurposes, "declared.over_limit")
	if validator.Validate(api.Raw(profile)) == nil {
		t.Fatal("configuration Schema allowed more than32 purposes")
	}
	if _, err = collaboration.NewRemoteAgentProfile(profile.ProfileRef.ComponentID, profile.ProfileRef.Version, profile.Values); !api.IsCode(err, "invalid_request") {
		t.Fatalf("profile constructor allowed more than32 purposes: %v", err)
	}
	var untrusted map[string]any
	if err = api.Decode(api.Raw(old), &untrusted); err != nil {
		t.Fatal(err)
	}
	untrusted["allow_all_purposes"] = true
	if validator.Validate(api.Raw(untrusted)) == nil {
		t.Fatal("configuration Schema allowed an undeclared authority flag")
	}
}
