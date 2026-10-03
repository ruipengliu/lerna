package executor

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/adapters/sqlite"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func referenceBundle(t *testing.T) (AdmissionBundle, *platform.Keyring) {
	t.Helper()
	now := time.Now().UTC()
	tenant, owner, device := api.NewID("tenant"), api.NewID("owner"), api.NewID("executor")
	s := runtime.Scope{TenantID: tenant, OwnerID: owner}
	leaseID, operation := api.NewID("lease"), api.NewID("operation")
	principal := runtime.Auth{TenantID: tenant, SubjectID: owner, CredentialGeneration: 1, Roles: []string{"service", "orchestrator"}}
	args := []byte(`{"path":"reports/result.txt","content_ref":null,"expected_version":"absent"}`)
	argsRef := api.ContentRef{TenantID: tenant, OwnerID: owner, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(args), MediaType: "application/json", ByteLength: uint64(len(args))}
	deadline := api.Time(now.Add(time.Minute))
	leaseRef := s.Ref(leaseID, 1)
	i := domain.ExecutionIntent{OperationID: operation, TaskRef: s.Ref(api.NewID("task"), 1), GoalRevision: 1, ControlRevision: 1, AdmissionSourceKind: "decision", AdmissionSourceRef: s.Ref(api.NewID("admission"), 1), SourcePosition: "0", AdmissionPurpose: "goal_action", CapabilityRef: execution.FileWriteCapability().Ref, BindingRef: s.Ref(api.NewID("binding"), 1), InstallLockRef: api.ComponentRef{ComponentID: api.NewID("component"), Version: "1", Digest: api.Hash([]byte("lock"))}, ArgumentsRef: argsRef, ResourceRefs: []api.ObjectRef{}, RequirementRefs: []api.RequirementRef{}, CostBound: []api.Amount{{Unit: "USD", Value: "1"}}, ExecutorID: device, Deadline: deadline, TaskDeadline: deadline, LogicalStepKey: "save"}
	intentBytes := api.Raw(i)
	intentRef := api.ContentRef{TenantID: tenant, OwnerID: owner, ContentID: api.NewID("content"), Version: 1, Hash: api.Hash(intentBytes), MediaType: "application/json", ByteLength: uint64(len(intentBytes))}
	hash, _ := api.Digest(i)
	admissionHash := api.Hash([]byte("original-task-operationintent"))
	instance := api.NewID("instance")
	lease := governance.GrantLease{LeaseID: leaseID, Revision: 1, Mode: "once", IssuedAt: api.Time(now.Add(-time.Second)), EndpointID: device, InstanceID: instance, GrantRefs: []api.ObjectRef{s.Ref(api.NewID("grant"), 1)}, Scope: governance.UseRequest{UseID: leaseID, SubjectRef: s.Ref(api.NewID("user"), 1), TargetRef: api.ObjectRef{TenantID: tenant, OwnerID: device, ObjectID: operation, Revision: 1}, TargetKind: "operation", IntentHash: admissionHash, Resources: []string{"managed-files"}, Actions: []string{"file.write"}, Recipient: device, Location: "device", Purposes: []string{"save"}, RequestedUnits: i.CostBound, StartBefore: deadline}, Limits: i.CostBound, ExpiresAt: deadline, State: "open", Cumulative: []api.Amount{}, Reserved: i.CostBound}
	lease.Scope.GrantRefs = lease.GrantRefs
	b := AdmissionBundle{BundleID: api.NewID("bundle"), Revision: 1, AuthorityID: owner, EndpointID: device, InstanceID: instance, OriginalCommandID: api.NewID("command"), AdmissionHash: admissionHash, ExecutionHash: hash, IntentRef: intentRef, ReservationRef: s.Ref(api.NewID("reservation"), 1), Intent: i, Principal: PrincipalOf(principal), LeaseRef: leaseRef, UseRefs: []api.ObjectRef{leaseRef}, Lease: lease, IssuedAt: api.Time(now.Add(-time.Second)), StartBefore: deadline, Contents: []ContentPermission{{ContentRef: intentRef, Purposes: []string{"execution_intent"}, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetainUntil: deadline}, {ContentRef: argsRef, Purposes: []string{"execution_arguments"}, ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}, RetainUntil: deadline}}}
	keys, err := platform.NewDevelopmentKey(tenant, owner, []string{"executor_admission", "grant_lease", "control", "executor_revocation"})
	if err != nil {
		t.Fatal(err)
	}
	return b, keys
}
func TestAdmissionBindsOriginalOwnerDigestsPrincipalAndLease(t *testing.T) {
	b, keys := referenceBundle(t)
	store, err := sqlite.Open(filepath.Join(t.TempDir(), "cloud.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	if err = store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	scope := runtime.Scope{TenantID: b.Principal.TenantID, OwnerID: b.AuthorityID, DatabaseID: store.ID()}
	status, err := store.Within(ctx, scope, []string{Namespace}, func(tx runtime.Tx) error {
		var err error
		b, err = SealAdmissionTx(ctx, tx, Proof{Keys: keys, SigningKeyID: "development-es256"}, b)
		return err
	})
	if status != runtime.Committed || err != nil {
		t.Fatalf("seal: %s %v", status, err)
	}
	digest, _ := BundleDigest(b)
	if _, err = keys.Verify(b.Proof, bundleClaims(b, digest), time.Now()); err != nil {
		t.Fatal(err)
	}
	for _, mutation := range []struct {
		name   string
		change func(*AdmissionBundle)
	}{
		{"execution_hash", func(c *AdmissionBundle) { c.ExecutionHash = api.Hash([]byte("different")) }},
		{"admission_hash", func(c *AdmissionBundle) { c.AdmissionHash = c.ExecutionHash }},
		{"recipient", func(c *AdmissionBundle) { c.EndpointID = api.NewID("executor") }},
		{"principal", func(c *AdmissionBundle) { c.Principal.CredentialGeneration++ }},
		{"command", func(c *AdmissionBundle) { c.OriginalCommandID = api.NewID("command") }},
		{"content", func(c *AdmissionBundle) { c.Contents[0].Purposes = []string{"arbitrary_signing"} }},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			var modified AdmissionBundle
			if err := api.Decode(api.Raw(b), &modified); err != nil {
				t.Fatal(err)
			}
			mutation.change(&modified)
			digest, _ := BundleDigest(modified)
			if _, err := keys.Verify(modified.Proof, bundleClaims(modified, digest), time.Now()); err == nil {
				t.Fatal("modified original admission accepted")
			}
		})
	}
}
