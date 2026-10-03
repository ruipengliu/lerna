package governance

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// InstallLeaseTx 由设备宿主配置固定 endpoint/instance；调用前不假定正文
// 提供的实例身份可信，使用已登记 authority 密钥验原有限分配声明。
func (s *Service) InstallLeaseTx(ctx context.Context, tx runtime.Tx, lease GrantLease, authorityRef api.ObjectRef) error {
	if s.Ports.Proof == nil || s.Ports.EndpointID == "" || s.Ports.InstanceID == "" || lease.EndpointID != s.Ports.EndpointID || lease.InstanceID != s.Ports.InstanceID || lease.Revision != 1 || lease.State != "open" || lease.OnceConsumed || len(lease.Cumulative) != 0 || lease.SpendingClosed || lease.UsageFinal {
		return api.E("forbidden", "offline_lease_instance_mismatch")
	}
	if lease.Scope.SubjectRef.TenantID != tx.Scope().TenantID || lease.LeaseID != authorityRef.ObjectID {
		return api.E("forbidden", "offline_lease_scope_mismatch")
	}
	allocation := lease
	allocation.AllocationDigest = ""
	allocation.Proof = ""
	digest, err := api.Digest(allocation)
	if err != nil {
		return err
	}
	if digest != lease.AllocationDigest {
		return api.E("forbidden", "lease_allocation_digest_mismatch")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	statement := ProofStatement{TenantID: tx.Scope().TenantID, IssuerID: authorityRef.OwnerID, AudienceID: s.Ports.EndpointID, Purpose: "grant_lease", ObjectRef: authorityRef, Digest: digest, IssuedAt: lease.IssuedAt, StartBefore: lease.ExpiresAt}
	if err = s.Ports.Proof.VerifyLocal(lease.Proof, statement, now); err != nil {
		return err
	}
	var old GrantLease
	_, err = tx.Get(ctx, ns("leases"), lease.LeaseID, &old)
	if err == nil {
		if old.AllocationDigest != lease.AllocationDigest || old.EndpointID != lease.EndpointID || old.InstanceID != lease.InstanceID {
			return api.E("idempotency_conflict", "lease_allocation_changed")
		}
		return nil
	}
	if !errMissing(err) {
		return err
	}
	lease.Reserved = []api.Amount{}
	return tx.Create(ctx, ns("leases"), lease.LeaseID, lease.EndpointID, lease)
}

func (s *Service) useLease(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in LeaseUseRequest) (runtime.Outcome, error) {
	out, err := s.UseLeaseTx(ctx, tx, auth, in)
	return runtime.Applied(out), err
}
func (s *Service) UseLeaseTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in LeaseUseRequest) (UseReceipt, error) {
	if s.Ports.OfflineGate == nil || s.Ports.EndpointID == "" || s.Ports.InstanceID == "" {
		return UseReceipt{}, api.E("unsupported", "offline_control_gate_unavailable")
	}
	var lease GrantLease
	rev, err := tx.Get(ctx, ns("leases"), in.LeaseRef.ObjectID, &lease)
	if err != nil {
		return UseReceipt{}, err
	}
	request := in.Use
	digest, err := api.Digest(request)
	if err != nil {
		return UseReceipt{}, err
	}
	var old UseReceipt
	if _, e := tx.Get(ctx, ns("uses"), request.UseID, &old); e == nil {
		if old.RequestDigest != digest {
			return old, api.E("idempotency_conflict", "use_intent_mismatch")
		}
		return old, nil
	} else if !errMissing(e) {
		return old, e
	}
	if !api.ValidID(request.UseID) || request.SubjectRef.TenantID != tx.Scope().TenantID || request.SubjectRef.ObjectID != auth.SubjectID || request.SubjectRef.Revision != auth.CredentialGeneration || !api.Equal(request.SubjectRef, lease.Scope.SubjectRef) || lease.EndpointID != s.Ports.EndpointID || lease.InstanceID != s.Ports.InstanceID {
		return old, api.E("forbidden", "offline_lease_instance_mismatch")
	}
	if err = runtime.CheckRef(tx.Scope(), request.TargetRef); err != nil {
		return old, err
	}
	if !contains([]string{"operation", "decision", "content_use"}, request.TargetKind) {
		return old, api.E("invalid_request", "invalid_use_target")
	}
	if err = api.ValidateAmounts(request.RequestedUnits); err != nil {
		return old, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return old, err
	}
	out := UseReceipt{UseID: request.UseID, SubjectRef: request.SubjectRef, TargetRef: request.TargetRef, TargetKind: request.TargetKind, IntentHash: request.IntentHash, RequestDigest: digest, GrantRefs: lease.GrantRefs, Decision: "allowed", Reserved: request.RequestedUnits, CostBound: request.RequestedUnits, Recipient: request.Recipient, Location: request.Location, Purposes: request.Purposes, IssuedAt: api.Time(now), StartBefore: minTime(request.StartBefore, lease.ExpiresAt)}
	if lease.State != "open" || before(now, lease.ExpiresAt) != nil || before(now, request.StartBefore) != nil {
		out.Decision = "denied"
		out.Reason = "window_expired"
	}
	if lease.Mode == "once" && lease.OnceConsumed {
		out.Decision = "denied"
		out.Reason = "once_consumed"
	}
	if !subset(request.Resources, lease.Scope.Resources) || !subset(request.Actions, lease.Scope.Actions) || !subset(request.Purposes, lease.Scope.Purposes) || request.Location != lease.Scope.Location || request.Recipient != lease.Scope.Recipient || !api.Equal(request.GrantRefs, lease.GrantRefs) {
		out.Decision = "denied"
		out.Reason = "scope_exceeded"
	}
	total, err := amountsAdd(lease.Cumulative, lease.Reserved)
	if err != nil {
		return old, err
	}
	total, err = amountsAdd(total, request.RequestedUnits)
	if err != nil {
		return old, err
	}
	if !bounded(total, lease.Limits) {
		out.Decision = "denied"
		out.Reason = "scope_exceeded"
	}
	gateCutoff, err := s.Ports.OfflineGate.CheckTx(ctx, tx, auth, lease, request)
	if err != nil {
		out.Decision = "denied"
		out.Reason = "local_control_closed"
	} else {
		out.StartBefore = minTime(out.StartBefore, gateCutoff)
		if before(now, out.StartBefore) != nil {
			out.Decision = "denied"
			out.Reason = "window_expired"
		}
	}
	if out.Decision == "allowed" {
		lease.Revision = rev + 1
		lease.OnceConsumed = lease.OnceConsumed || lease.Mode == "once"
		lease.Reserved, err = amountsAdd(lease.Reserved, request.RequestedUnits)
		if err != nil {
			return old, err
		}
		if err = tx.Put(ctx, ns("leases"), lease.LeaseID, rev, lease); err != nil {
			return old, err
		}
		if s.Ports.Proof != nil {
			out.Proof, err = s.Ports.Proof.SignLocal(ProofStatement{TenantID: tx.Scope().TenantID, IssuerID: tx.Scope().OwnerID, AudienceID: request.TargetRef.OwnerID, Purpose: "grant_use", ObjectRef: tx.Scope().Ref(out.UseID, 1), Digest: digest, IssuedAt: out.IssuedAt, StartBefore: out.StartBefore})
			if err != nil {
				return old, err
			}
		}
	} else {
		out.Reserved = []api.Amount{}
	}
	if err = tx.Create(ctx, ns("uses"), out.UseID, lease.LeaseID, out); err != nil {
		return old, err
	}
	if err = tx.Create(ctx, ns("lease_use_links"), out.UseID, lease.LeaseID, LeaseUseLink{UseID: out.UseID, LeaseID: lease.LeaseID}); err != nil {
		return old, err
	}
	settlement := UseSettlement{UseID: out.UseID, Revision: 1, CumulativeUsage: []api.Amount{}, SourceRefs: []api.ObjectRef{}, EvidenceRefs: []api.ContentRef{}, RemainingReserved: out.Reserved}
	if out.Decision == "denied" {
		settlement.SpendingClosed = true
		settlement.UsageFinal = true
	}
	if err = tx.Create(ctx, ns("settlements"), out.UseID, lease.LeaseID, settlement); err != nil {
		return old, err
	}
	return out, nil
}

type LeaseUseLink struct {
	UseID   string `json:"use_id"`
	LeaseID string `json:"lease_id"`
}

func (s *Service) applyLeaseUseSettlement(ctx context.Context, tx runtime.Tx, use UseReceipt, u api.UsageSnapshot) (UseSettlement, error) {
	var link LeaseUseLink
	if _, err := tx.Get(ctx, ns("lease_use_links"), use.UseID, &link); err != nil {
		return UseSettlement{}, err
	}
	var lease GrantLease
	lrev, err := tx.Get(ctx, ns("leases"), link.LeaseID, &lease)
	if err != nil {
		return UseSettlement{}, err
	}
	var old UseSettlement
	rev, err := tx.Get(ctx, ns("settlements"), use.UseID, &old)
	if err != nil {
		return old, err
	}
	if u.UsageRevision == 0 || u.UsageDigest == "" || u.SourceRef.TenantID != tx.Scope().TenantID || u.SourceRef.ObjectID != use.TargetRef.ObjectID || u.SourceRef.OwnerID != use.TargetRef.OwnerID {
		return old, api.E("forbidden", "settlement_unverified")
	}
	if u.UsageRevision < old.SourceRevision {
		return old, nil
	}
	if u.UsageRevision == old.SourceRevision {
		if u.UsageDigest != old.SourceDigest {
			return old, api.E("idempotency_conflict", "usage_revision_changed")
		}
		return old, nil
	}
	if !amountsMonotone(old.CumulativeUsage, u.Cumulative) || old.SpendingClosed && !u.SpendingClosed || old.UsageFinal && !u.UsageFinal {
		return old, api.E("invalid_state", "usage_regressed")
	}
	remaining, err := amountsRemaining(use.Reserved, u.Cumulative)
	if err != nil {
		return old, err
	}
	if u.SpendingClosed && u.UsageFinal {
		remaining = []api.Amount{}
	}
	released, err := amountsSubtract(old.RemainingReserved, remaining)
	if err != nil {
		return old, err
	}
	lease.Reserved, err = amountsSubtract(lease.Reserved, released)
	if err != nil {
		return old, err
	}
	delta, err := amountsSubtract(u.Cumulative, old.CumulativeUsage)
	if err != nil {
		return old, err
	}
	lease.Cumulative, err = amountsAdd(lease.Cumulative, delta)
	if err != nil {
		return old, err
	}
	lease.Revision = lrev + 1
	if err = tx.Put(ctx, ns("leases"), lease.LeaseID, lrev, lease); err != nil {
		return old, err
	}
	old.Revision = rev + 1
	old.SourceRevision = u.UsageRevision
	old.SourceDigest = u.UsageDigest
	old.CumulativeUsage = u.Cumulative
	old.SpendingClosed = u.SpendingClosed
	old.UsageFinal = u.UsageFinal
	old.SourceRefs = []api.ObjectRef{u.SourceRef}
	old.EvidenceRefs = u.ProofRefs
	old.RemainingReserved = remaining
	err = tx.Put(ctx, ns("settlements"), use.UseID, rev, old)
	return old, err
}

func (s *Service) reportLease(ctx context.Context, tx runtime.Tx, auth runtime.Auth, c api.Command, in LeaseReport) (runtime.Outcome, error) {
	if err := requireRole(auth, "usage_reporter"); err != nil {
		return runtime.Outcome{}, err
	}
	if s.Ports.UsageVerifier == nil {
		return runtime.Outcome{}, api.E("unsupported", "lease_usage_verifier_unavailable")
	}
	var lease GrantLease
	if _, err := tx.Get(ctx, ns("leases"), in.LeaseRef.ObjectID, &lease); err != nil {
		return runtime.Outcome{}, err
	}
	if in.EndpointID != lease.EndpointID || in.InstanceID != lease.InstanceID || in.Usage.SourceRef.OwnerID != lease.EndpointID || in.Usage.SourceRef.ObjectID != lease.LeaseID || in.ClosureRef.OwnerID != lease.EndpointID {
		return runtime.Outcome{}, api.E("forbidden", "lease_report_instance_mismatch")
	}
	pending := LeaseReportPending{ID: c.CommandID, Revision: 1, CommandID: c.CommandID, Request: in}
	if err := tx.Create(ctx, ns("lease_report_pending"), c.CommandID, lease.LeaseID, pending); err != nil {
		return runtime.Outcome{}, err
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return runtime.Outcome{}, err
	}
	if _, err = tx.Raise(ctx, "governance.lease_report", c.CommandID, tx.Scope().Ref(c.CommandID, 1), now); err != nil {
		return runtime.Outcome{}, err
	}
	return runtime.Accepted(lease), nil
}
func (s *Service) continueLeaseReport(ctx context.Context, store runtime.Store, scope runtime.Scope, work runtime.Work) error {
	var pending LeaseReportPending
	if _, err := store.Read(ctx, scope, ns("lease_report_pending"), work.Job.ResponsibilityKey, 0, &pending); err != nil {
		return err
	}
	if err := s.Ports.UsageVerifier.Verify(ctx, scope, pending.Request.Usage.SourceRef, pending.Request.Usage); err != nil {
		return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
			return runtime.Decide(ctx, tx, pending.CommandID, nil, api.E("forbidden", "settlement_unverified"))
		})
	}
	return finish(ctx, store, scope, s.participants(), work, runtime.Done(), func(tx runtime.Tx) error {
		out, err := s.ApplyLeaseReportTx(ctx, tx, pending.Request)
		if err != nil {
			return err
		}
		return runtime.Decide(ctx, tx, pending.CommandID, out, nil)
	})
}

// ApplyLeaseReportTx 只消费已在 Tx 外核验的固定 endpoint/instance 原账单。
func (s *Service) ApplyLeaseReportTx(ctx context.Context, tx runtime.Tx, in LeaseReport) (GrantLease, error) {
	var lease GrantLease
	rev, err := tx.Get(ctx, ns("leases"), in.LeaseRef.ObjectID, &lease)
	if err != nil {
		return lease, err
	}
	u := in.Usage
	if in.EndpointID != lease.EndpointID || in.InstanceID != lease.InstanceID || u.SourceRef.OwnerID != lease.EndpointID || u.SourceRef.TenantID != tx.Scope().TenantID || u.SourceRef.ObjectID != lease.LeaseID || u.UsageRevision == 0 || u.UsageDigest == "" || in.ClosureRef.OwnerID != lease.EndpointID {
		return lease, api.E("forbidden", "lease_report_instance_mismatch")
	}
	if err = api.ValidateAmounts(u.Cumulative); err != nil {
		return lease, err
	}
	var settlement UseSettlement
	srev, err := tx.Get(ctx, ns("settlements"), lease.LeaseID, &settlement)
	if err != nil {
		return lease, err
	}
	if u.UsageRevision < settlement.SourceRevision {
		return lease, nil
	}
	if u.UsageRevision == settlement.SourceRevision {
		if u.UsageDigest != settlement.SourceDigest {
			return lease, api.E("idempotency_conflict", "usage_revision_changed")
		}
		return lease, nil
	}
	if !amountsMonotone(lease.Cumulative, u.Cumulative) || lease.SpendingClosed && !u.SpendingClosed || lease.UsageFinal && !u.UsageFinal {
		return lease, api.E("invalid_state", "lease_usage_regressed")
	}
	remaining, err := amountsRemaining(lease.Limits, u.Cumulative)
	if err != nil {
		return lease, err
	}
	if u.SpendingClosed && u.UsageFinal {
		remaining = []api.Amount{}
	}
	for _, gr := range lease.GrantRefs {
		var permission api.Grant
		if _, err = tx.Get(ctx, ns("grants"), gr.ObjectID, &permission); err != nil {
			return lease, err
		}
		var balance GrantUsage
		brev, err := tx.Get(ctx, ns("grant_usage"), gr.ObjectID, &balance)
		if err != nil {
			return lease, err
		}
		delta, err := amountsSubtract(u.Cumulative, lease.Cumulative)
		if err != nil {
			return lease, err
		}
		balance.Spent, err = amountsAdd(balance.Spent, delta)
		if err != nil {
			return lease, err
		}
		released, err := amountsSubtract(settlement.RemainingReserved, remaining)
		if err != nil {
			return lease, err
		}
		balance.Reserved, err = amountsSubtract(balance.Reserved, released)
		if err != nil {
			return lease, err
		}
		balance.Revision = brev + 1
		if err = tx.Put(ctx, ns("grant_usage"), balance.GrantID, brev, balance); err != nil {
			return lease, err
		}
	}
	lease.Revision = rev + 1
	lease.Cumulative = u.Cumulative
	lease.Reserved = remaining
	lease.UsageRevision = u.UsageRevision
	lease.SpendingClosed = u.SpendingClosed
	lease.UsageFinal = u.UsageFinal
	if u.SpendingClosed {
		lease.State = "closed"
	}
	if u.SpendingClosed && u.UsageFinal {
		lease.State = "reconciled"
		lease.ClosureRef = &in.ClosureRef
	}
	if err = tx.Put(ctx, ns("leases"), lease.LeaseID, rev, lease); err != nil {
		return lease, err
	}
	settlement.Revision = srev + 1
	settlement.SourceRevision = u.UsageRevision
	settlement.SourceDigest = u.UsageDigest
	settlement.CumulativeUsage = u.Cumulative
	settlement.SpendingClosed = u.SpendingClosed
	settlement.UsageFinal = u.UsageFinal
	settlement.SourceRefs = []api.ObjectRef{u.SourceRef}
	settlement.EvidenceRefs = u.ProofRefs
	settlement.RemainingReserved = remaining
	err = tx.Put(ctx, ns("settlements"), lease.LeaseID, srev, settlement)
	return lease, err
}
