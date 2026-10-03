package executor

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type deviceAuthority struct{ h *Host }

func controlClaims(c api.ControlSnapshot, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: c.ProofRef.TenantID, Issuer: c.OrchestratorID, Audience: c.OrchestratorID, Purpose: "control", ObjectRef: api.ObjectRef{TenantID: c.ProofRef.TenantID, OwnerID: c.OrchestratorID, ObjectID: c.TaskID, Revision: 1}, Digest: digest, ControlRevision: c.ControlRevision, WindowID: c.WindowID, IssuedAt: c.IssuedAt, StartBefore: c.StartBefore}
}
func verifyControl(keys *platform.Keyring, c api.ControlSnapshot, compact string) error {
	if c.ProofRef.Hash != api.Hash([]byte(compact)) || c.ProofRef.ByteLength != uint64(len(compact)) || c.ProofRef.MediaType != "application/jose" || c.ProofRef.OwnerID != c.OrchestratorID {
		return api.E("forbidden", "control_content_binding_mismatch")
	}
	unsigned := c
	unsigned.ProofRef = api.ContentRef{}
	digest, err := api.Digest(unsigned)
	if err != nil {
		return err
	}
	_, err = keys.VerifySource(compact, controlClaims(c, digest))
	return err
}
func (a deviceAuthority) VerifyControl(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.ControlSnapshot) error {
	if auth.TenantID != tx.Scope().TenantID || auth.SubjectID != a.h.Config.Authority.OwnerID || !auth.HasRole("orchestrator") || c.OrchestratorID != a.h.Config.Authority.OwnerID {
		return api.E("forbidden", "control_source_not_paired")
	}
	var saved ControlDelivery
	if _, err := tx.Get(ctx, Namespace+".controls", c.WindowID, &saved); err != nil {
		return api.E("forbidden", "original_control_proof_missing")
	}
	if !api.Equal(saved.Snapshot, c) {
		return api.E("forbidden", "original_control_window_changed")
	}
	return verifyControl(a.h.Keys, c, saved.Compact)
}
func (a deviceAuthority) PrepareStart(ctx context.Context, scope runtime.Scope, r execution.StartRequest) (execution.PreparedStart, error) {
	var rec admissionRecord
	if _, err := a.h.Store.Read(ctx, scope, Namespace+".admissions", r.Intent.OperationID, 0, &rec); err != nil {
		return execution.PreparedStart{}, err
	}
	if !api.Equal(rec.Bundle.Intent, r.Intent) || !api.Equal(rec.Bundle.Principal.Auth(), r.Auth) {
		return execution.PreparedStart{}, api.E("forbidden", "original_admission_changed")
	}
	b := rec.Bundle
	cut := earliest(b.StartBefore, b.Lease.ExpiresAt, r.ControlWindow.StartBefore, r.Intent.Deadline, r.Intent.TaskDeadline)
	// 原签名内容在 Tx 外写入耐久介质；prepared只指该不可变凭据，不RPC。
	ref, err := deviceContent{a.h}.Publish(ctx, scope, r.Auth, execution.Publication{ContentID: apiID("content", b.BundleID+"/admission-proof"), MediaType: "application/jose", Purpose: "execution_start_proof", Location: "device", ProcessedSources: []api.ContentRef{}, DisclosedSources: []api.ContentRef{}}, []byte(b.Proof))
	if err != nil {
		return execution.PreparedStart{}, err
	}
	return execution.PreparedStart{OperationID: r.Intent.OperationID, IntentHash: b.ExecutionHash, Recipient: scope.OwnerID, UseRefs: append([]api.ObjectRef{}, b.UseRefs...), ApprovalRefs: []api.ObjectRef{}, AuthorityRevision: 1, StartBefore: cut, ProofRef: ref}, nil
}
func (a deviceAuthority) VerifyStart(ctx context.Context, tx runtime.Tx, r execution.StartRequest, p execution.PreparedStart) (execution.StartPermit, error) {
	var rec admissionRecord
	if _, err := tx.Get(ctx, Namespace+".admissions", r.Intent.OperationID, &rec); err != nil {
		return execution.StartPermit{}, err
	}
	b := rec.Bundle
	if !api.Equal(r.Intent, b.Intent) || !api.Equal(r.Auth, b.Principal.Auth()) || r.Invoke.IntentHash != b.ExecutionHash || !api.Equal(r.Invoke.IntentRef, b.IntentRef) || !api.Equal(r.Invoke.UseRefs, b.UseRefs) || !api.Equal(r.Invoke.ReservationRef, b.ReservationRef) || p.OperationID != r.Intent.OperationID || p.IntentHash != b.ExecutionHash || p.Recipient != tx.Scope().OwnerID || p.AuthorityRevision != 1 || !api.Equal(p.UseRefs, b.UseRefs) || p.ProofRef.Hash != api.Hash([]byte(b.Proof)) || p.ProofRef.OwnerID != tx.Scope().OwnerID || p.ProofRef.TenantID != tx.Scope().TenantID {
		return execution.StartPermit{}, api.E("forbidden", "prepared_admission_binding_mismatch")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return execution.StartPermit{}, err
	}
	if err = a.h.verifyBundle(b, now, true); err != nil {
		return execution.StartPermit{}, err
	}
	if err = a.h.checkDenyTx(ctx, tx, b); err != nil {
		return execution.StartPermit{}, err
	}
	if err = a.VerifyControl(ctx, tx, r.Auth, r.ControlWindow); err != nil {
		return execution.StartPermit{}, err
	}
	cut := earliest(b.StartBefore, b.Lease.ExpiresAt, r.ControlWindow.StartBefore, r.Intent.Deadline, r.Intent.TaskDeadline)
	if cut != p.StartBefore || before(now, cut) != nil {
		return execution.StartPermit{}, api.E("expired", "original_admission_window_expired")
	}
	// 本机 Use 在实际入口与 send_started 同事务提交；不存在云端权限 RPC。
	use := b.Lease.Scope
	use.UseID = b.Intent.OperationID
	use.RequestedUnits = b.Intent.CostBound
	use.StartBefore = cut
	use.GrantRefs = b.Lease.GrantRefs
	user := runtime.Auth{TenantID: use.SubjectRef.TenantID, SubjectID: use.SubjectRef.ObjectID, CredentialGeneration: use.SubjectRef.Revision, Roles: []string{}}
	receipt, err := a.h.Governance.UseLeaseTx(ctx, tx, user, governance.LeaseUseRequest{LeaseRef: b.LeaseRef, Use: use})
	if err != nil {
		return execution.StartPermit{}, err
	}
	if receipt.Decision != "allowed" {
		return execution.StartPermit{}, api.E("forbidden", receipt.Reason)
	}
	return execution.StartPermit{StartBefore: earliest(cut, receipt.StartBefore), ProofRefs: []api.ContentRef{p.ProofRef, r.ControlWindow.ProofRef}}, nil
}
func (a deviceAuthority) CheckTx(ctx context.Context, tx runtime.Tx, _ runtime.Auth, l governance.GrantLease, u governance.UseRequest) (string, error) {
	var rec admissionRecord
	if _, err := tx.Get(ctx, Namespace+".admissions", u.TargetRef.ObjectID, &rec); err != nil {
		return "", err
	}
	b := rec.Bundle
	if l.LeaseID != b.Lease.LeaseID || u.IntentHash != b.AdmissionHash || !api.Equal(u.TargetRef, b.Lease.Scope.TargetRef) {
		return "", api.E("forbidden", "offline_admission_mismatch")
	}
	if err := a.h.checkDenyTx(ctx, tx, b); err != nil {
		return "", err
	}
	var gate execution.TaskGate
	if _, err := tx.Get(ctx, "execution.gates", b.Intent.TaskRef.OwnerID+":"+b.Intent.TaskRef.ObjectID, &gate); err != nil {
		return "", err
	}
	if gate.GoalRevision != b.Intent.GoalRevision || gate.ControlRevision != b.Intent.ControlRevision || gate.Status != "active" || gate.Control != "running" {
		return "", api.E("forbidden", "local_control_closed")
	}
	return earliest(b.StartBefore, u.StartBefore, l.ExpiresAt), nil
}
func (h *Host) verifyBundle(b AdmissionBundle, now time.Time, current bool) error {
	if b.Principal.TenantID != h.Scope.TenantID || b.AuthorityID != h.Config.Authority.OwnerID || b.EndpointID != h.Scope.OwnerID || b.InstanceID != h.Config.InstanceID || b.DeviceDatabaseID != h.Scope.DatabaseID {
		return api.E("forbidden", "device_admission_instance_mismatch")
	}
	if err := validateBundle(b); err != nil {
		return err
	}
	matched := false
	for _, binding := range h.Config.Bindings {
		if api.Equal(binding.CapabilityRef, b.Intent.CapabilityRef) && api.Equal(binding.BindingRef, b.Intent.BindingRef) && api.Equal(binding.InstallLockRef, b.Intent.InstallLockRef) && subset(b.Lease.Scope.Resources, binding.Resources) && subset(b.Lease.Scope.Actions, binding.Actions) {
			matched = true
		}
	}
	if !matched {
		return api.E("forbidden", "device_binding_scope_not_installed")
	}
	digest, err := BundleDigest(b)
	if err != nil {
		return err
	}
	if current {
		_, err = h.Keys.Verify(b.Proof, bundleClaims(b, digest), now)
	} else {
		_, err = h.Keys.VerifySource(b.Proof, bundleClaims(b, digest))
	}
	return err
}
func (h *Host) checkDenyTx(ctx context.Context, tx runtime.Tx, b AdmissionBundle) error {
	refs := append([]api.ObjectRef{b.Lease.Scope.SubjectRef, b.Principal.Auth().Ref(b.AuthorityID), b.LeaseRef, b.Intent.TaskRef}, b.Lease.GrantRefs...)
	for _, ref := range refs {
		var denied denyRecord
		if _, err := tx.Get(ctx, Namespace+".denials", denyKey(ref), &denied); err == nil {
			return api.E("forbidden", "known_authority_revocation")
		} else if !api.IsCode(err, "not_found") {
			return err
		}
	}
	return nil
}
func denyKey(r api.ObjectRef) string { return r.OwnerID + ":" + r.ObjectID }
func before(now time.Time, s string) error {
	t, e := api.ParseTime(s)
	if e != nil || !now.Before(t) {
		return api.E("expired", "finite_window_expired")
	}
	return nil
}
func earliest(times ...string) string {
	var earliestTime time.Time
	out := ""
	for _, s := range times {
		t, e := api.ParseTime(s)
		if e != nil {
			return ""
		}
		if out == "" || t.Before(earliestTime) {
			out = s
			earliestTime = t
		}
	}
	return out
}
func apiID(prefix, name string) string {
	return prefix + "_" + api.Hash([]byte("harness-independent-executor/" + name))[7:39]
}
