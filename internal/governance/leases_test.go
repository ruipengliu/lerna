package governance_test

import (
	"context"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type localProof struct{ ring *platform.Keyring }

func proofClaims(s governance.ProofStatement) platform.ProofClaims {
	return platform.ProofClaims{TenantID: s.TenantID, Issuer: s.IssuerID, Audience: s.AudienceID, Purpose: s.Purpose, ObjectRef: s.ObjectRef, Digest: s.Digest, WindowID: s.ObjectRef.ObjectID, IssuedAt: s.IssuedAt, StartBefore: s.StartBefore}
}
func (p localProof) SignLocal(s governance.ProofStatement) (string, error) {
	for id, key := range p.ring.Keys {
		if key.Issuer == s.IssuerID && key.Private != nil {
			return p.ring.Sign(id, proofClaims(s))
		}
	}
	return "", api.E("forbidden", "unknown_test_issuer")
}
func (p localProof) VerifyLocal(token string, s governance.ProofStatement, now time.Time) error {
	claims, err := p.ring.Verify(token, proofClaims(s), now)
	if err != nil {
		return err
	}
	if claims.StartBefore != s.StartBefore || claims.IssuedAt != s.IssuedAt {
		return api.E("forbidden", "proof_window_binding_mismatch")
	}
	return nil
}

type offlineControl struct {
	TargetRef   api.ObjectRef `json:"target_ref"`
	IntentHash  string        `json:"intent_hash"`
	StartBefore string        `json:"start_before"`
	Closed      bool          `json:"closed"`
}
type offlineGate struct{}

func (offlineGate) CheckTx(ctx context.Context, tx runtime.Tx, _ runtime.Auth, _ governance.GrantLease, use governance.UseRequest) (string, error) {
	var gate offlineControl
	if _, err := tx.Get(ctx, "governance/fixture_offline_control", use.TargetRef.ObjectID, &gate); err != nil {
		return "", err
	}
	if gate.Closed || !api.Equal(gate.TargetRef, use.TargetRef) || gate.IntentHash != use.IntentHash {
		return "", api.E("forbidden", "task_control_closed")
	}
	return gate.StartBefore, nil
}
func TestOfflineLeaseBindsOriginalInstanceAndNeverRestoresOnceOnReconcile(t *testing.T) {
	cloud := environment(t, governance.Options{UsageVerifier: usageEvidence{}})
	device := environment(t, governance.Options{UsageVerifier: usageEvidence{}})
	device.scope.TenantID = cloud.scope.TenantID
	device.auth.TenantID = cloud.scope.TenantID
	device.auth.SubjectID = cloud.auth.SubjectID
	endpoint := device.scope.OwnerID
	instance := api.NewID("instance")
	cloudKeys, err := platform.NewDevelopmentKey(cloud.scope.TenantID, cloud.scope.OwnerID, []string{"grant_use", "grant_lease"})
	if err != nil {
		t.Fatal(err)
	}
	deviceKeys, err := platform.NewDevelopmentKey(cloud.scope.TenantID, device.scope.OwnerID, []string{"grant_use"})
	if err != nil {
		t.Fatal(err)
	}
	for _, key := range deviceKeys.Keys {
		cloudKeys.Keys["device-es256"] = key
	}
	proof := localProof{ring: cloudKeys}
	cloud.svc.Ports.Proof = proof
	device.svc.Ports.Proof = proof
	device.svc.Ports.EndpointID = endpoint
	device.svc.Ports.InstanceID = instance
	device.svc.Ports.OfflineGate = offlineGate{}
	g := issue(t, cloud, "once")
	scope := useRequest(cloud, g)
	scope.TargetRef = device.scope.Ref(api.NewID("operation"), 1)
	leaseID := api.NewID("lease")
	_, allocated := command(t, cloud, "grant.lease.allocate", leaseID, governance.LeaseAllocate{LeaseID: leaseID, EndpointID: endpoint, InstanceID: instance, Scope: scope, Limits: []api.Amount{{Unit: "USD", Value: "2"}}, ExpiresAt: scope.StartBefore, CostMode: "strict"}, nil)
	if allocated.Stage != "applied" {
		t.Fatalf("allocate: %+v", allocated)
	}
	var lease governance.GrantLease
	if err = api.Decode(allocated.Output, &lease); err != nil {
		t.Fatal(err)
	}
	status, err := device.store.Within(device.ctx, device.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		if err := device.svc.InstallLeaseTx(device.ctx, tx, lease, cloud.scope.Ref(leaseID, 1)); err != nil {
			return err
		}
		return tx.Create(device.ctx, "governance/fixture_offline_control", scope.TargetRef.ObjectID, "", offlineControl{TargetRef: scope.TargetRef, IntentHash: scope.IntentHash, StartBefore: scope.StartBefore})
	})
	if err != nil || status != runtime.Committed {
		t.Fatalf("import lease: %s %v", status, err)
	}
	use := scope
	use.UseID = api.NewID("use")
	_, used := command(t, device, "grant.lease.use", use.UseID, governance.LeaseUseRequest{LeaseRef: cloud.scope.Ref(leaseID, 1), Use: use}, nil)
	if used.Stage != "applied" {
		t.Fatalf("lease use: %+v", used)
	}
	var receipt governance.UseReceipt
	if err = api.Decode(used.Output, &receipt); err != nil || receipt.Decision != "allowed" {
		t.Fatalf("lease use: %+v %v", receipt, err)
	}
	device.auth.Roles = append(device.auth.Roles, "usage_reporter")
	usage := api.UsageSnapshot{SourceRef: use.TargetRef, UsageRevision: 1, UsageDigest: api.Hash([]byte("device observed never started")), Cumulative: []api.Amount{{Unit: "USD", Value: "0"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{ref(t, device, "closure")}}
	_, settling := command(t, device, "grant.use.settle", use.UseID, governance.SettleRequest{UseID: use.UseID, Usage: usage}, nil)
	if settling.Stage != "accepted" {
		t.Fatalf("local settle: %+v", settling)
	}
	drain(t, device, "governance.settle")
	use.UseID = api.NewID("use")
	_, again := command(t, device, "grant.lease.use", use.UseID, governance.LeaseUseRequest{LeaseRef: cloud.scope.Ref(leaseID, 1), Use: use}, nil)
	var denied governance.UseReceipt
	if err = api.Decode(again.Output, &denied); err != nil || denied.Decision != "denied" || denied.Reason != "once_consumed" {
		t.Fatalf("lease once resurrected: %+v %v", denied, err)
	}
	device.svc.Ports.InstanceID = api.NewID("instance")
	status, err = device.store.Within(device.ctx, device.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		return device.svc.InstallLeaseTx(device.ctx, tx, lease, cloud.scope.Ref(leaseID, 1))
	})
	if status != runtime.RolledBack || err == nil {
		t.Fatalf("new instance inherited old lease: %s %v", status, err)
	}
	device.svc.Ports.InstanceID = instance
	cloud.auth.Roles = append(cloud.auth.Roles, "usage_reporter")
	report := governance.LeaseReport{LeaseRef: cloud.scope.Ref(leaseID, 1), EndpointID: endpoint, InstanceID: instance, Usage: api.UsageSnapshot{SourceRef: device.scope.Ref(leaseID, 1), UsageRevision: 1, UsageDigest: api.Hash([]byte("complete device closed ledger")), Cumulative: []api.Amount{{Unit: "USD", Value: "0"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{ref(t, cloud, "device_ledger")}}, ClosureRef: device.scope.Ref(api.NewID("closure"), 1)}
	_, reconcile := command(t, cloud, "grant.lease.report", leaseID, report, nil)
	if reconcile.Stage != "accepted" {
		t.Fatalf("lease report: %+v", reconcile)
	}
	drain(t, cloud, "governance.lease_report")
	current := query[governance.GrantLease](t, cloud, "grant.lease.read", governance.IDInput{ID: leaseID})
	if current.State != "reconciled" || len(current.Reserved) != 0 {
		t.Fatalf("reconciliation: %+v", current)
	}
	grant := query[governance.GrantRecord](t, cloud, "grant.read", governance.IDInput{ID: g.GrantID})
	if !grant.OnceConsumed {
		t.Fatal("parent once restored on lease reconciliation")
	}
}
