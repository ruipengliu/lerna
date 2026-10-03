package executor

import (
	"context"
	"strconv"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

type Proof struct {
	Keys         *platform.Keyring
	SigningKeyID string
}

func (p Proof) SignLocal(s governance.ProofStatement) (string, error) {
	return p.Keys.Sign(p.SigningKeyID, leaseClaims(s))
}
func leaseClaims(s governance.ProofStatement) platform.ProofClaims {
	return platform.ProofClaims{TenantID: s.TenantID, Issuer: s.IssuerID, Audience: s.AudienceID, Purpose: s.Purpose, ObjectRef: s.ObjectRef, Digest: s.Digest, WindowID: s.ObjectRef.ObjectID, IssuedAt: s.IssuedAt, StartBefore: s.StartBefore}
}
func (p Proof) VerifyLocal(token string, s governance.ProofStatement, now time.Time) error {
	_, err := p.Keys.Verify(token, leaseClaims(s), now)
	return err
}
func BundleDigest(b AdmissionBundle) (string, error) { b.Proof = ""; return api.Digest(b) }
func bundleClaims(b AdmissionBundle, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: b.Principal.TenantID, Issuer: b.AuthorityID, Audience: b.EndpointID, Purpose: "executor_admission", ObjectRef: api.ObjectRef{TenantID: b.Principal.TenantID, OwnerID: b.AuthorityID, ObjectID: b.BundleID, Revision: b.Revision}, Digest: digest, ControlRevision: b.Intent.ControlRevision, WindowID: b.BundleID, IssuedAt: b.IssuedAt, StartBefore: b.StartBefore}
}

// SealAdmissionTx 固定原行动和 lease；调用者须先在原云端事务完成真实准入。
// 本 helper 不裁决 Task、不另作 Grant Use、也不做网络或 Content IO。
func SealAdmissionTx(ctx context.Context, tx runtime.Tx, p Proof, b AdmissionBundle) (AdmissionBundle, error) {
	if b.AuthorityID != tx.Scope().OwnerID || b.Principal.TenantID != tx.Scope().TenantID {
		return b, api.E("forbidden", "admission_authority_mismatch")
	}
	if err := validateBundle(b); err != nil {
		return b, err
	}
	digest, err := BundleDigest(b)
	if err != nil {
		return b, err
	}
	var old admissionRecord
	if _, err = tx.Get(ctx, Namespace+".admissions", b.Intent.OperationID, &old); err == nil {
		if old.Digest != digest {
			return b, api.E("idempotency_conflict", "admission_bundle_changed")
		}
		return old.Bundle, nil
	} else if !api.IsCode(err, "not_found") {
		return b, err
	}
	b.Proof, err = p.Keys.Sign(p.SigningKeyID, bundleClaims(b, digest))
	if err != nil {
		return b, err
	}
	err = tx.Create(ctx, Namespace+".admissions", b.Intent.OperationID, b.Intent.TaskRef.ObjectID, admissionRecord{b, digest})
	return b, err
}
func validateBundle(b AdmissionBundle) error {
	i, l := b.Intent, b.Lease
	if !api.ValidID(b.DeviceDatabaseID) {
		return api.E("forbidden", "original_device_database_required")
	}
	if b.ReservationRef.OwnerID != b.AuthorityID || b.ReservationRef.TenantID != i.TaskRef.TenantID || !api.ValidID(b.ReservationRef.ObjectID) || b.ReservationRef.Revision == 0 {
		return api.E("forbidden", "original_reservation_binding_mismatch")
	}
	if !api.ValidID(b.BundleID) || b.Revision != 1 || !api.ValidID(b.AuthorityID) || !api.ValidID(b.EndpointID) || !api.ValidID(b.InstanceID) || !api.ValidID(b.OriginalCommandID) || b.Principal.TenantID != i.TaskRef.TenantID || b.Principal.SubjectID != b.AuthorityID || b.Principal.CredentialGeneration == 0 || !b.Principal.Auth().HasRole("orchestrator") || i.TaskRef.OwnerID != b.AuthorityID || i.ExecutorID != b.EndpointID || i.OperationID == "" || b.LeaseRef.OwnerID != b.AuthorityID || b.LeaseRef.ObjectID != l.LeaseID || b.LeaseRef.Revision != 1 || b.LeaseRef.TenantID != i.TaskRef.TenantID || l.EndpointID != b.EndpointID || l.InstanceID != b.InstanceID || l.Scope.IntentHash != b.AdmissionHash || l.Scope.TargetKind != "operation" || l.Scope.TargetRef.ObjectID != i.OperationID || l.Scope.TargetRef.OwnerID != b.EndpointID || l.Scope.TargetRef.TenantID != i.TaskRef.TenantID || l.Scope.Recipient != b.EndpointID || l.Scope.Location != "device" || !api.Equal(i.CostBound, l.Limits) || !api.Equal(b.UseRefs, []api.ObjectRef{b.LeaseRef}) {
		return api.E("forbidden", "admission_bundle_binding_mismatch")
	}
	digest, err := api.Digest(i)
	if err != nil || digest != b.ExecutionHash {
		return api.E("forbidden", "execution_digest_mismatch")
	}
	if b.IntentRef.TenantID != i.TaskRef.TenantID || b.IntentRef.OwnerID != b.AuthorityID || api.ValidateRecord("ContentRef", b.IntentRef) != nil || b.IntentRef.Hash != api.Hash(api.Raw(i)) {
		return api.E("forbidden", "original_intent_content_mismatch")
	}
	issued, err := api.ParseTime(b.IssuedAt)
	if err != nil {
		return err
	}
	before, err := api.ParseTime(b.StartBefore)
	if err != nil || !before.After(issued) {
		return api.E("forbidden", "invalid_admission_window")
	}
	for _, cut := range []string{l.ExpiresAt, i.Deadline, i.TaskDeadline} {
		t, e := api.ParseTime(cut)
		if e != nil || before.After(t) {
			return api.E("forbidden", "admission_window_expansion")
		}
	}
	if len(b.Contents) < 2 || len(b.Contents) > 128 {
		return api.E("invalid_request", "admission_content_set_bounds")
	}
	seen := map[string]bool{}
	hasIntent, hasArgs := false, false
	for _, c := range b.Contents {
		key := contentKey(c.ContentRef)
		if seen[key] || c.ContentRef.TenantID != i.TaskRef.TenantID || c.ContentRef.ByteLength > MaxContentBytes || api.ValidateRecord("ContentRef", c.ContentRef) != nil || len(c.Purposes) == 0 || len(c.Purposes) > 32 {
			return api.E("forbidden", "admission_content_permission_invalid")
		}
		seen[key] = true
		t, e := api.ParseTime(c.RetainUntil)
		if e != nil || before.After(t) {
			return api.E("forbidden", "content_retention_window_expansion")
		}
		if api.Equal(c.ContentRef, b.IntentRef) && has(c.Purposes, "execution_intent") {
			hasIntent = true
		}
		if api.Equal(c.ContentRef, i.ArgumentsRef) && has(c.Purposes, "execution_arguments") {
			hasArgs = true
		}
	}
	if !hasIntent || !hasArgs {
		return api.E("forbidden", "original_content_permission_missing")
	}
	return nil
}
func contentKey(r api.ContentRef) string {
	return "copy_" + api.Hash([]byte(r.TenantID + "/" + r.OwnerID + "/" + r.ContentID + "/" + apiString(r.Version)))[7:39]
}
func apiString(v uint64) string { return strconv.FormatUint(v, 10) }
func has(xs []string, x string) bool {
	for _, s := range xs {
		if s == x {
			return true
		}
	}
	return false
}
func subset(xs, ys []string) bool {
	for _, s := range xs {
		if !has(ys, s) {
			return false
		}
	}
	return true
}
