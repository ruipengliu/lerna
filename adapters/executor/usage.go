package executor

import (
	"context"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/execution"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func leaseReportClaims(report SignedLeaseReport, digest string) platform.ProofClaims {
	return platform.ProofClaims{TenantID: report.Report.Usage.SourceRef.TenantID, Issuer: report.Report.EndpointID, Audience: report.Report.LeaseRef.OwnerID, Purpose: "executor_usage", ObjectRef: report.Report.Usage.SourceRef, Digest: digest, WindowID: report.Report.LeaseRef.ObjectID, IssuedAt: report.IssuedAt, StartBefore: report.StartBefore}
}
func LeaseReportDigest(in SignedLeaseReport) (string, error) { in.Proof = ""; return api.Digest(in) }

// VerifyLeaseReport 保存迟到事实时只验原来源签名；它不能给新执行签发窗口。
func VerifyLeaseReport(keys *platform.Keyring, in SignedLeaseReport) error {
	u := in.Report.Usage
	unsigned := u
	unsigned.UsageDigest = ""
	digest, err := api.Digest(unsigned)
	if err != nil || digest != u.UsageDigest {
		return api.E("forbidden", "device_lease_usage_digest_mismatch")
	}
	digest, err = LeaseReportDigest(in)
	if err != nil {
		return err
	}
	_, err = keys.VerifySource(in.Proof, leaseReportClaims(in, digest))
	return err
}
func (h *Host) leaseUsage(ctx context.Context, st runtime.Store, s runtime.Scope, a runtime.Auth, q api.Query, in LeaseID) (SignedLeaseReport, error) {
	if err := h.peer(a); err != nil {
		return SignedLeaseReport{}, err
	}
	var lease governance.GrantLease
	if _, err := st.Read(ctx, s, "governance/leases", in.LeaseID, 0, &lease); err != nil {
		return SignedLeaseReport{}, err
	}
	operationID := lease.Scope.TargetRef.ObjectID
	var admission admissionRecord
	if _, err := st.Read(ctx, s, Namespace+".admissions", operationID, 0, &admission); err != nil {
		return SignedLeaseReport{}, err
	}
	b := admission.Bundle
	if b.Lease.LeaseID != lease.LeaseID || b.EndpointID != s.OwnerID || b.InstanceID != h.Config.InstanceID {
		return SignedLeaseReport{}, api.E("forbidden", "original_lease_source_mismatch")
	}
	query := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: s.OwnerID, QueryID: api.NewID("query"), Method: "execution.usage.get", TargetID: operationID, Payload: api.Raw(execution.OperationIDInput{OperationID: operationID})}
	raw, err := h.Dispatcher.Query(ctx, b.Principal.Auth(), api.Raw(query))
	if err != nil {
		return SignedLeaseReport{}, err
	}
	var operationUsage api.UsageSnapshot
	if err = api.Decode(raw, &operationUsage); err != nil {
		return SignedLeaseReport{}, err
	}
	// 当前本机原源用量机械归并到本机lease；不用调用者自报账单，事务内无跨库查询。
	status, err := st.Within(ctx, s, []string{Namespace, "governance", "execution"}, func(tx runtime.Tx) error {
		var current struct {
			Operation api.Operation `json:"operation"`
		}
		if _, err := tx.Get(ctx, "execution.operations", operationID, &current); err != nil {
			return err
		}
		if current.Operation.Revision != operationUsage.UsageRevision {
			return api.E("revision_conflict", "device_usage_source_advanced")
		}
		var use governance.UseReceipt
		if _, err := tx.Get(ctx, "governance/uses", operationID, &use); err == nil {
			if _, err = h.Governance.ApplySettlementTx(ctx, tx, governance.SettleRequest{UseID: operationID, Usage: operationUsage}); err != nil {
				return err
			}
		} else if !api.IsCode(err, "not_found") {
			return err
		} else if len(current.Operation.Usage) != 0 {
			return api.E("accounting_unknown", "local_use_basis_missing")
		}
		rev, err := tx.Get(ctx, "governance/leases", in.LeaseID, &lease)
		if err != nil {
			return err
		}
		if lease.SpendingClosed != operationUsage.SpendingClosed || lease.UsageFinal != operationUsage.UsageFinal || len(lease.Cumulative) == 0 {
			lease.SpendingClosed = operationUsage.SpendingClosed
			lease.UsageFinal = operationUsage.UsageFinal
			lease.Cumulative = operationUsage.Cumulative
			if lease.SpendingClosed {
				lease.State = "closed"
			}
			if lease.SpendingClosed && lease.UsageFinal {
				lease.Reserved = []api.Amount{}
			}
			lease.Revision = rev + 1
			return tx.Put(ctx, "governance/leases", lease.LeaseID, rev, lease)
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return SignedLeaseReport{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return SignedLeaseReport{}, err
	}
	proofBody := LeaseUsageProof{LeaseRef: b.LeaseRef, EndpointID: s.OwnerID, InstanceID: h.Config.InstanceID, AllocationDigest: b.Lease.AllocationDigest, LocalLease: lease, OperationUsage: operationUsage}
	proofRef, err := deviceContent{h}.Publish(ctx, s, b.Principal.Auth(), execution.Publication{ContentID: apiID("content", lease.LeaseID+"/lease-usage/"+apiString(lease.Revision)), MediaType: "application/json", Purpose: "execution_usage_proof", Location: "device", ProcessedSources: operationUsage.ProofRefs, DisclosedSources: []api.ContentRef{}}, api.Raw(proofBody))
	if err != nil {
		return SignedLeaseReport{}, err
	}
	snapshot := api.UsageSnapshot{SourceRef: s.Ref(lease.LeaseID, lease.Revision), UsageRevision: lease.Revision, Cumulative: lease.Cumulative, SpendingClosed: lease.SpendingClosed, UsageFinal: lease.UsageFinal, ProofRefs: []api.ContentRef{proofRef}}
	snapshot.UsageDigest, err = api.Digest(snapshot)
	if err != nil {
		return SignedLeaseReport{}, err
	}
	var report SignedLeaseReport
	closureID := apiID("closure", lease.LeaseID+"/"+apiString(lease.Revision))
	status, err = st.Within(ctx, s, []string{Namespace}, func(tx runtime.Tx) error {
		if _, err := tx.Get(ctx, Namespace+".lease_closures", closureID, &report); err == nil {
			if !api.Equal(report.Report.Usage, snapshot) {
				return api.E("idempotency_conflict", "original_lease_usage_changed")
			}
			return nil
		} else if !api.IsCode(err, "not_found") {
			return err
		}
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		report = SignedLeaseReport{SourceDatabaseID: s.DatabaseID, Report: governance.LeaseReport{LeaseRef: b.LeaseRef, EndpointID: s.OwnerID, InstanceID: h.Config.InstanceID, Usage: snapshot, ClosureRef: s.Ref(closureID, 1)}, IssuedAt: api.Time(now), StartBefore: proofBody.LocalLease.Scope.StartBefore}
		// 已关闭账单的source签名用issued+独立保留截止，绝不复用为执行grant。
		var content contentRecord
		if _, err = tx.Get(ctx, Namespace+".contents", contentKey(proofRef), &content); err != nil {
			return err
		}
		report.StartBefore = content.Permission.RetainUntil
		digest, err := LeaseReportDigest(report)
		if err != nil {
			return err
		}
		report.Proof, err = h.Keys.Sign(h.Proof.SigningKeyID, leaseReportClaims(report, digest))
		if err != nil {
			return err
		}
		return tx.Create(ctx, Namespace+".lease_closures", closureID, lease.LeaseID, report)
	})
	if status == runtime.CommitUnknown {
		return report, runtime.ErrCommitUnknown
	}
	return report, err
}
