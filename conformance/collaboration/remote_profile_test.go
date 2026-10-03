package collaboration_test

import (
	"context"
	"github.com/ruipengliu/lerna/internal/memory"
	"github.com/ruipengliu/lerna/internal/task"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/collaboration"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 配对 Authority 是显式受信夹具；不模拟 SQL、Task 接纳或目标效果。
type remoteAuthority struct {
	subject runtime.Auth
	peers   map[string]runtime.Auth
}

func (a *remoteAuthority) CheckPeerTx(_ context.Context, _ runtime.Tx, peer runtime.Auth, owner string) error {
	if original, ok := a.peers[owner]; !ok || !api.Equal(original, peer) {
		return api.E("forbidden", "peer_unpaired")
	}
	return nil
}
func (a *remoteAuthority) ResolveSubjectTx(_ context.Context, _ runtime.Tx, ref api.ObjectRef, _ collaboration.RemoteAgentProfile, control bool) (runtime.Auth, error) {
	if ref.TenantID != a.subject.TenantID || ref.ObjectID != a.subject.SubjectID || (!control && ref.Revision != a.subject.CredentialGeneration) || control && ref.Revision > a.subject.CredentialGeneration {
		return runtime.Auth{}, api.E("forbidden", "subject_unpaired")
	}
	return a.subject, nil
}
func remoteComponent(kind string) api.ComponentRef {
	return api.ComponentRef{ComponentID: api.NewID(kind), Version: "1.0.0", Digest: api.Hash([]byte(kind))}
}
func TestRemoteProfileRequiresPairedAuthorityBeforeResponsibility(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "parent.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: st.ID()}
	subject := runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1}
	child := api.NewID("owner")
	values := collaboration.RemoteAgentValues{ParentOwnerID: scope.OwnerID, ReceiverID: child, AgentBindingRef: scope.Ref(api.NewID("binding"), 1), PolicyRef: remoteComponent("policy"), InstallLockRef: remoteComponent("lock"), SubjectRefs: []api.ObjectRef{subject.Ref(scope.OwnerID)}, PermissionRefs: []api.ObjectRef{}, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, ResourceRefs: []api.ComponentRef{}, BudgetLimits: []api.Amount{{Unit: "USD", Value: "3"}}, MaxDepth: 4, MaxInputs: 16, Location: "cloud"}
	profile, err := collaboration.NewRemoteAgentProfile(api.NewID("agent"), "1.0.0", values)
	if err != nil {
		t.Fatal(err)
	}
	keys, err := platform.NewDevelopmentKey(scope.TenantID, scope.OwnerID, []string{"agent_allocation", "agent_state"})
	if err != nil {
		t.Fatal(err)
	}
	cfg := collaboration.RemoteConfig{Store: st, Scope: scope, Registry: runtime.NewRegistry(), Keys: keys, SigningKeyID: "development-es256", Auth: runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("service"), CredentialGeneration: 1, Roles: []string{"service"}}, Profiles: []collaboration.RemoteAgentProfile{profile}}
	if _, err = collaboration.NewRemote(cfg); !api.IsCode(err, "unsupported") {
		t.Fatalf("missing authority admitted: %v", err)
	}
	cfg.Authority = &remoteAuthority{subject: subject, peers: map[string]runtime.Auth{}}
	r, err := collaboration.NewRemote(cfg)
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.Within(ctx, scope, []string{"collaboration", "task"}, func(tx runtime.Tx) error { return r.CheckCollaborationTx(ctx, tx, subject, "delegate", child) })
	if !api.IsCode(err, "unsupported") {
		t.Fatalf("no actual transport admitted: %v", err)
	}
	changed := profile
	changed.Values.BudgetLimits = []api.Amount{{Unit: "USD", Value: "4"}}
	cfg.Profiles = []collaboration.RemoteAgentProfile{changed}
	if _, err = collaboration.NewRemote(cfg); !api.IsCode(err, "invalid_request") {
		t.Fatalf("profile digest did not bind authority bounds: %v", err)
	}
}

func TestRemoteCloseBeforeCreateSealsOriginalIncomingGate(t *testing.T) {
	ctx := context.Background()
	st, err := sqlite.Open(filepath.Join(t.TempDir(), "receiver.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := st.Close(); err != nil {
			t.Error(err)
		}
	})
	if err = st.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("owner"), DatabaseID: st.ID()}
	parent := api.NewID("owner")
	auth := runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("subject"), CredentialGeneration: 1}
	peer := runtime.Auth{TenantID: scope.TenantID, SubjectID: parent, CredentialGeneration: 1, Roles: []string{"paired_agent"}}
	policy := remoteComponent("policy")
	p, err := collaboration.NewRemoteAgentProfile(api.NewID("agent"), "1.0.0", collaboration.RemoteAgentValues{ParentOwnerID: parent, ReceiverID: scope.OwnerID, AgentBindingRef: api.ObjectRef{TenantID: scope.TenantID, OwnerID: parent, ObjectID: api.NewID("binding"), Revision: 1}, PolicyRef: policy, InstallLockRef: remoteComponent("lock"), SubjectRefs: []api.ObjectRef{auth.Ref(parent)}, PermissionRefs: []api.ObjectRef{}, CapabilityRefs: []api.ComponentRef{}, BindingRefs: []api.ObjectRef{}, ResourceRefs: []api.ComponentRef{}, BudgetLimits: []api.Amount{{Unit: "USD", Value: "3"}}, MaxDepth: 4, MaxInputs: 16, Location: "cloud"})
	if err != nil {
		t.Fatal(err)
	}
	keys, err := platform.NewDevelopmentKey(scope.TenantID, scope.OwnerID, []string{"agent_allocation", "agent_state"})
	if err != nil {
		t.Fatal(err)
	}
	reg := runtime.NewRegistry()
	r, err := collaboration.NewRemote(collaboration.RemoteConfig{Store: st, Scope: scope, Registry: reg, Keys: keys, SigningKeyID: "development-es256", Auth: runtime.Auth{TenantID: scope.TenantID, SubjectID: api.NewID("service"), CredentialGeneration: 1, Roles: []string{"service"}}, Authority: &remoteAuthority{subject: auth, peers: map[string]runtime.Auth{parent: peer}}, Profiles: []collaboration.RemoteAgentProfile{p}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := task.New(task.Config{Policies: []task.TaskPolicy{{PolicyRef: policy, ContinuationLimit: 10, RepairLimit: 1, NoProgressLimit: 3, ContextRoundLimit: 3, SafeAttemptLimit: 2, MaxRequirements: 10, MaxDelegations: 128, MaxDepth: 4, CostMode: "strict", BudgetLimits: []api.Amount{{Unit: "USD", Value: "10"}}, MaxEvidenceStalenessSeconds: 300, MaxDurationSeconds: 3600}}, Participants: []string{"task", "collaboration"}}, task.Ports{})
	if err != nil {
		t.Fatal(err)
	}
	if err = service.Register(reg); err != nil {
		t.Fatal(err)
	}
	if err = r.BindTask(service); err != nil {
		t.Fatal(err)
	}
	if err = r.Register(); err != nil {
		t.Fatal(err)
	}
	parentRef := api.ObjectRef{TenantID: scope.TenantID, OwnerID: parent, ObjectID: api.NewID("task"), Revision: 1}
	allocationRef := api.ObjectRef{TenantID: scope.TenantID, OwnerID: parent, ObjectID: api.NewID("allocation"), Revision: 1}
	goal := api.ContentRef{TenantID: scope.TenantID, OwnerID: parent, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash([]byte("original bounded goal")), MediaType: "text/plain", ByteLength: 21}
	id := api.NewID("delegation")
	packet := collaboration.RemoteCreateInput{CreationKey: id, CreateCommandID: api.NewID("command"), ChildTaskID: api.NewID("task"), ProfileRef: p.ProfileRef, DelegationRef: api.ObjectRef{TenantID: scope.TenantID, OwnerID: parent, ObjectID: id, Revision: 1}, AllocationRef: allocationRef, SourceDatabaseID: api.NewID("database"), SubjectRef: auth.Ref(parent), Input: task.DelegateInput{DelegationID: id, ParentTaskRef: parentRef, ParentGoalRevision: 1, GoalRef: goal, InputRefs: []api.ContentRef{}, AgentBindingRef: p.Values.AgentBindingRef, PermissionRefs: []api.ObjectRef{}, Budget: []api.Amount{{Unit: "USD", Value: "2"}}, Deadline: api.Time(time.Now().Add(10 * time.Minute)), PolicyRef: policy, ReceiverID: scope.OwnerID}, AncestorTaskRefs: []api.ObjectRef{parentRef}, ForeignReferences: []memory.ForeignReference{}}
	packet.OriginalCommandRef = api.ObjectRef{TenantID: scope.TenantID, OwnerID: parent, ObjectID: api.NewID("command"), Revision: 1}
	packet.ParentSources = []api.SourceEvidence{}
	d := runtime.Dispatcher{Store: st, Registry: reg, OwnerID: scope.OwnerID}
	closeCommand := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: api.NewID("command"), Method: "collaboration.allocation.close", TargetID: allocationRef.ObjectID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(task.AllocationCloseInput{AllocationRef: allocationRef, ParentTaskRef: parentRef, Reason: "original parent cancelled before create"})}
	closed, err := d.Command(ctx, peer, api.Raw(closeCommand))
	if err != nil || closed.Stage != "applied" {
		t.Fatalf("close prior to mapping %+v %v", closed, err)
	}
	create := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: scope.OwnerID, CommandID: packet.CreateCommandID, Method: "collaboration.create", TargetID: id, ExpiresAt: packet.Input.Deadline, Payload: api.Raw(packet)}
	late, err := d.Command(ctx, peer, api.Raw(create))
	if err != nil || late.Stage != "accepted" {
		t.Fatalf("persist original create for recovery %+v %v", late, err)
	}
	works, status, err := st.Claim(ctx, scope, api.NewID("boot"), []string{collaboration.JobRemoteCreate}, 1, 30*time.Second)
	if err != nil || status != runtime.Committed || len(works) != 1 {
		t.Fatalf("create responsibility %v %v", status, err)
	}
	handler, ok := reg.Job(collaboration.JobRemoteCreate)
	if !ok {
		t.Fatal("original recovery handler absent")
	}
	if err = handler(ctx, st, scope, works[0]); err != nil {
		t.Fatal(err)
	}
	receipt, err := d.Lookup(ctx, peer, create.CommandID)
	if err != nil || receipt.Stage != "rejected" || receipt.Error == nil || receipt.Error.Reason != "allocation_closed" {
		t.Fatalf("late create revived %+v %v", receipt, err)
	}
	original, err := service.IncomingRead(ctx, st, scope, peer, allocationRef)
	if err != nil || original.Gate != "closed" || original.TaskRef != nil {
		t.Fatalf("original incoming resurrected %+v %v", original, err)
	}
	again, err := d.Command(ctx, peer, api.Raw(create))
	if err != nil || !api.Equal(receipt, again) {
		t.Fatalf("original creation changed %+v %v", again, err)
	}
}
