package collaboration

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

const remoteRejectedCreations = "collaboration.remote_rejected_creations"

// RemoteRejectedCreation 是原创建命令的耐久永久拒绝，不伪造 TaskClosure。
// 原命令、原关闭额度和已核签名进入同一个 State 的完整摘要。
type RemoteRejectedCreation struct {
	CommandRef    api.ObjectRef  `json:"command_ref"`
	AllocationRef api.ObjectRef  `json:"allocation_ref"`
	ParentTaskRef api.ObjectRef  `json:"parent_task_ref"`
	ChildTaskID   string         `json:"child_task_id"`
	PacketDigest  string         `json:"packet_digest"`
	Receipt       api.Receipt    `json:"receipt"`
	ClosedAt      string         `json:"closed_at"`
	CheckedAt     string         `json:"checked_at"`
	ProofRef      api.ContentRef `json:"proof_ref"`
}

func rejectedCreationID(packet RemoteCreateInput) string {
	return remoteID("rejection", packet.DelegationRef.OwnerID, packet.CreationKey, packet.CreateCommandID)
}

func validateRejectedCreation(packet RemoteCreateInput, out RemoteState, rejected RemoteRejectedCreation) error {
	packetDigest, err := api.Digest(packet)
	if err != nil {
		return err
	}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: packet.Input.ReceiverID, CommandID: packet.CreateCommandID, Method: "collaboration.create", TargetID: packet.CreationKey, ExpiresAt: packet.Input.Deadline, Payload: api.Raw(packet)}
	digest, err := api.Digest(command)
	if err != nil {
		return err
	}
	if out.Task != nil || out.Fact.ChildTaskRef != nil || out.TaskClosure != nil || out.ResultRef != nil || out.Incoming == nil || out.Incoming.TaskRef != nil || out.Incoming.Gate != "closed" || rejected.CommandRef != (api.ObjectRef{TenantID: packet.SubjectRef.TenantID, OwnerID: packet.Input.ReceiverID, ObjectID: packet.CreateCommandID, Revision: 1}) || rejected.AllocationRef != packet.AllocationRef || rejected.ParentTaskRef != packet.Input.ParentTaskRef || rejected.ChildTaskID != packet.ChildTaskID || rejected.PacketDigest != packetDigest || rejected.Receipt.CommandID != packet.CreateCommandID || rejected.Receipt.RequestDigest != digest || rejected.Receipt.Stage != "rejected" || rejected.Receipt.AcceptedAt == "" || rejected.Receipt.Error == nil || rejected.Receipt.Error.Code != "invalid_state" || rejected.Receipt.Error.Reason != "allocation_closed" || rejected.ClosedAt != out.Incoming.ClosedAt || !api.Equal(out.Incoming.Limits, packet.Input.Budget) || len(out.Incoming.Cumulative) != len(packet.Input.Budget) {
		return api.E("forbidden", "remote_original_no_child_rejection_changed")
	}
	for i, limit := range packet.Input.Budget {
		if out.Incoming.Cumulative[i].Unit != limit.Unit || out.Incoming.Cumulative[i].Value != "0" {
			return api.E("forbidden", "remote_original_no_child_usage_unknown")
		}
	}
	if api.ValidateRecord("ContentRef", rejected.ProofRef) != nil || rejected.ProofRef.TenantID != packet.SubjectRef.TenantID || rejected.ProofRef.OwnerID != packet.Input.ReceiverID || rejected.ProofRef.MediaType != "application/jose" {
		return api.E("forbidden", "remote_original_no_child_proof_changed")
	}
	checked, err := api.ParseTime(rejected.CheckedAt)
	if err != nil {
		return err
	}
	closed, err := api.ParseTime(rejected.ClosedAt)
	if err != nil {
		return err
	}
	decided, err := api.ParseTime(rejected.Receipt.DecidedAt)
	if err != nil {
		return err
	}
	if checked.Before(closed) || checked.Before(decided) {
		return api.E("forbidden", "remote_original_no_child_time_changed")
	}
	return nil
}

// 原 Runtime 拒绝、Incoming 关闭与 Task 从未存在已由 Core 同 Tx 强核。
// 首次准确证明和委派 Closure 持久化；重复 State 不更换它们的身份或时间。
func (r *Remote) sealRejectedDelegationTx(ctx context.Context, tx runtime.Tx, handoffID string, packet RemoteCreateInput, original task.RejectedIncomingClosure, out *RemoteState) error {
	if !out.Fact.GoalWorkClosed || !out.Fact.EffectsClosed || !out.Fact.TransfersClosed || !out.Fact.UsageFinal {
		return nil
	}
	if err := validateStateUsageBindings(packet, *out); err != nil {
		return err
	}
	id := rejectedCreationID(packet)
	var rejection RemoteRejectedCreation
	err := tx.GetVersion(ctx, remoteRejectedCreations, id, 1, &rejection)
	if api.IsCode(err, "not_found") {
		now, clockErr := tx.Now(ctx)
		if clockErr != nil {
			return clockErr
		}
		rejection = RemoteRejectedCreation{CommandRef: original.CommandRef, AllocationRef: packet.AllocationRef, ParentTaskRef: packet.Input.ParentTaskRef, ChildTaskID: packet.ChildTaskID, PacketDigest: out.PacketDigest, Receipt: original.Receipt, ClosedAt: original.Incoming.ClosedAt, CheckedAt: api.Time(now)}
		digest, digestErr := api.Digest(rejection)
		if digestErr != nil {
			return digestErr
		}
		rejection.ProofRef, err = r.sealPublication(ctx, tx, packet.CreationKey, platform.ProofClaims{TenantID: tx.Scope().TenantID, Issuer: tx.Scope().OwnerID, Audience: packet.DelegationRef.OwnerID, Purpose: "agent_state", ObjectRef: original.CommandRef, Digest: digest, WindowID: packet.CreateCommandID, IssuedAt: api.Time(now), StartBefore: api.Time(now.Add(30 * time.Second))})
		if err != nil {
			return err
		}
		if err = tx.Create(ctx, remoteRejectedCreations, id, handoffID, rejection); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if !api.Equal(rejection.Receipt, original.Receipt) || rejection.CommandRef != original.CommandRef {
		return api.E("forbidden", "original_no_child_receipt_changed")
	}
	if err = validateRejectedCreation(packet, *out, rejection); err != nil {
		return err
	}
	budgetRefDigest, err := api.Digest(original.ClosureRef)
	if err != nil {
		return err
	}
	closureID := remoteID("closure", packet.DelegationRef.OwnerID, packet.CreationKey, budgetRefDigest)
	var actual remoteDelegationClosure
	err = tx.GetVersion(ctx, remoteDelegationClosures, closureID, 1, &actual)
	if api.IsCode(err, "not_found") {
		now, clockErr := tx.Now(ctx)
		if clockErr != nil {
			return clockErr
		}
		actual = remoteDelegationClosure{Closure: task.DelegationClosure{DelegationID: packet.CreationKey, Revision: 1, GoalWorkClosed: true, EffectsClosed: true, AllocationClosureRef: original.ClosureRef, TransfersClosed: true, ProofRefs: []api.ContentRef{rejection.ProofRef, original.Closure.ProofRef}, ClosedAt: api.Time(now)}, RejectedCreation: &rejection}
		if err = tx.Create(ctx, remoteDelegationClosures, closureID, handoffID, actual); err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	if actual.TaskClosure != nil || actual.RejectedCreation == nil || !api.Equal(*actual.RejectedCreation, rejection) {
		return api.E("forbidden", "original_no_child_closure_changed")
	}
	ref := tx.Scope().Ref(closureID, 1)
	out.RejectedCreation, out.DelegationClosure, out.DelegationClosureRef, out.Fact.ClosureRef = actual.RejectedCreation, &actual.Closure, &ref, &ref
	return validateDelegationClosure(packet, *out)
}
