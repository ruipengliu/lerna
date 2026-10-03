package executor

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func TestDeviceSubjectRevocationClosesOldGenerationAndAllowsOnlyNewSignedAdmission(t *testing.T) {
	f := newDeviceFixture(t)
	f.prepare(t)
	oldBundle := f.b
	revoke := Revocation{AuthorityID: f.b.AuthorityID, EndpointID: f.b.EndpointID, ObjectRef: f.b.Lease.Scope.SubjectRef, Kind: "subject", IssuedAt: api.Time(time.Now().Add(-time.Second)), StartBefore: api.Time(time.Now().Add(time.Minute))}
	digest, err := api.Digest(revoke)
	if err != nil {
		t.Fatal(err)
	}
	revoke.Proof, err = f.keys.Sign("development-es256", revocationClaims(revoke, digest))
	if err != nil {
		t.Fatal(err)
	}
	_, receipt := f.command(t, "executor.revocation.install", revoke.ObjectRef.ObjectID, revoke)
	if receipt.Stage != "applied" {
		t.Fatalf("original subject revoke: %+v", receipt)
	}
	oldCommand := f.invoke(t, f.control(t, "active", "running", 1))
	if err = runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	old := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if old.Operation.Effect != "not_started" || !old.NewAttemptsClosed {
		t.Fatalf("old generation entered: %+v", old)
	}
	if _, err = os.Stat(filepath.Join(f.root, "files", "reports", "result.txt")); !os.IsNotExist(err) {
		t.Fatal("revoked old generation changed target")
	}
	// 此受信新authority fixture另分配原gen2、新Grant/newlease/newOperation；
	// 只证明设备对准确签名依据的代次裁决，不冒充云端新授权流程。
	f.b.BundleID = api.NewID("bundle")
	f.b.OriginalCommandID = api.NewID("command")
	f.b.Intent.OperationID = api.NewID("operation")
	f.b.Intent.AdmissionSourceRef.ObjectID = api.NewID("admission")
	f.b.Intent.LogicalStepKey = "new_generation"
	f.b.Intent.ArgumentsRef.ContentID = api.NewID("content")
	f.raw[contentKey(f.b.Intent.ArgumentsRef)] = f.raw[contentKey(oldBundle.Intent.ArgumentsRef)]
	f.b.ReservationRef.ObjectID = api.NewID("reservation")
	f.b.IntentRef.ContentID = api.NewID("content")
	f.b.IntentRef.Hash = api.Hash(api.Raw(f.b.Intent))
	f.b.IntentRef.ByteLength = uint64(len(api.Raw(f.b.Intent)))
	f.raw[contentKey(f.b.IntentRef)] = api.Raw(f.b.Intent)
	f.b.ExecutionHash, _ = api.Digest(f.b.Intent)
	f.b.AdmissionHash = api.Hash([]byte("new-authority-operation-intent-generation-2"))
	f.b.Lease.Scope.IntentHash = f.b.AdmissionHash
	f.b.Lease.LeaseID = api.NewID("lease")
	f.b.Lease.Scope.UseID = f.b.Lease.LeaseID
	f.b.Lease.Scope.SubjectRef.Revision++
	f.b.Lease.Scope.TargetRef.ObjectID = f.b.Intent.OperationID
	f.b.Lease.Scope.GrantRefs = []api.ObjectRef{{TenantID: f.b.Principal.TenantID, OwnerID: f.b.AuthorityID, ObjectID: api.NewID("grant"), Revision: 1}}
	f.b.Lease.GrantRefs = f.b.Lease.Scope.GrantRefs
	f.b.LeaseRef.ObjectID = f.b.Lease.LeaseID
	f.b.UseRefs = []api.ObjectRef{f.b.LeaseRef}
	f.b.Contents[0].ContentRef = f.b.IntentRef
	f.b.Contents[1].ContentRef = f.b.Intent.ArgumentsRef
	allocation := f.b.Lease
	allocation.AllocationDigest, allocation.Proof = "", ""
	f.b.Lease.AllocationDigest, _ = api.Digest(allocation)
	f.b.Lease.Proof, err = (Proof{Keys: f.keys, SigningKeyID: "development-es256"}).SignLocal(governance.ProofStatement{TenantID: f.b.Principal.TenantID, IssuerID: f.b.AuthorityID, AudienceID: f.b.EndpointID, Purpose: "grant_lease", ObjectRef: f.b.LeaseRef, Digest: f.b.Lease.AllocationDigest, IssuedAt: f.b.Lease.IssuedAt, StartBefore: f.b.Lease.ExpiresAt})
	if err != nil {
		t.Fatal(err)
	}
	digest, _ = BundleDigest(f.b)
	f.b.Proof, err = f.keys.Sign("development-es256", bundleClaims(f.b, digest))
	if err != nil {
		t.Fatal(err)
	}
	f.prepare(t)
	f.invoke(t, f.control(t, "active", "running", 1))
	if err = runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	current := deviceQuery[execution.OperationView](t, f, "execution.get", f.b.Intent.OperationID, execution.OperationIDInput{OperationID: f.b.Intent.OperationID})
	if current.Operation.Effect != "applied" || len(current.Attempts.Items) != 1 {
		t.Fatalf("new signed generation refused: %+v", current)
	}
	if _, _, err = f.h.Call(f.ctx, f.peer, "command", api.Raw(oldCommand)); err != nil {
		t.Fatalf("old original receipt lost: %v", err)
	}
	if err = runtime.Drain(f.ctx, f.h.Store, f.h.Scope, f.h.Registry, 100); err != nil {
		t.Fatal(err)
	}
	oldAgain := deviceQuery[execution.OperationView](t, f, "execution.get", oldBundle.Intent.OperationID, execution.OperationIDInput{OperationID: oldBundle.Intent.OperationID})
	if oldAgain.Operation.Effect != "not_started" || len(oldAgain.Attempts.Items) != len(old.Attempts.Items) {
		t.Fatal("new subject generation revived old admission")
	}
}
