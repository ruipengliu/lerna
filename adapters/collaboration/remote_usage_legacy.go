package collaboration

import (
	"context"
	"fmt"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// 旧版把已验签的进行中 State 保存在私有费用头，尚无 Content 账单证明。
// 只恢复该已持久的非最终形状，不改变公开 UsageSnapshot 或最终账单合同。
func (r *Remote) validateStoredUsageHeadTx(ctx context.Context, tx runtime.Tx, packet RemoteCreateInput, head remoteDelegatedUsageHead) error {
	validator, err := api.NewValidator(api.SchemaFor[remoteDelegatedUsageHead]())
	if err != nil {
		return err
	}
	if err = validator.Validate(api.Raw(head)); err == nil {
		return nil
	}
	for _, usage := range []api.UsageSnapshot{head.OriginalUsage, head.Usage} {
		if usage.SpendingClosed || usage.UsageFinal || len(usage.ProofRefs) != 0 {
			return err
		}
	}
	open := api.Object(map[string]any{
		"source_ref": api.Ref("ObjectRef"), "usage_revision": api.Ref("Revision"), "usage_digest": api.Ref("Digest"),
		"cumulative": api.Array(api.Ref("Amount"), 1, 100), "spending_closed": api.Schema{"const": false},
		"usage_final": api.Schema{"const": false}, "proof_refs": api.Array(api.Ref("ContentRef"), 0, 0),
	}, "source_ref", "usage_revision", "usage_digest", "cumulative", "spending_closed", "usage_final", "proof_refs")
	legacy, schemaErr := api.NewValidator(api.Object(map[string]any{
		"packet_digest": api.Ref("Digest"), "original_usage": open, "usage": open,
	}, "packet_digest", "original_usage", "usage"))
	if schemaErr != nil {
		return schemaErr
	}
	if schemaErr = legacy.Validate(api.Raw(head)); schemaErr != nil {
		return schemaErr
	}
	// 头必须逐字段对应已有不可变投影及原签名，不能借兼容路径接纳
	// 新来的空证明账单、缺失证据或自行编辑的累计金额。
	var saved remoteDelegatedUsage
	if err = tx.GetVersion(ctx, remoteDelegatedUsages, remoteID("usage", packet.DelegationRef.ObjectID, fmt.Sprint(head.Usage.UsageRevision)), 1, &saved); err != nil {
		return err
	}
	if saved.PacketDigest != head.PacketDigest || saved.OriginalUsage == nil || !api.Equal(*saved.OriginalUsage, head.OriginalUsage) || !api.Equal(saved.Usage, head.Usage) || saved.State == nil || saved.Closure != nil {
		return api.E("idempotency_conflict", "remote_legacy_open_usage_unverified")
	}
	state := *saved.State
	packetDigest, err := api.Digest(packet)
	if err != nil {
		return err
	}
	peer, ok := r.peers[packet.Input.ReceiverID]
	if !ok || state.PacketDigest != packetDigest || state.ParentOwnerID != tx.Scope().OwnerID || state.CreationKey != packet.CreationKey || state.SourceDatabaseID != peer.Scope.DatabaseID || state.Incoming == nil || state.Incoming.Gate != "open" || state.Fact.Usage != nil || state.Fact.UsageFinal || !api.Equal(state.Incoming.Cumulative, head.OriginalUsage.Cumulative) || state.Incoming.UsageRevision != head.OriginalUsage.UsageRevision || !api.Equal(head.Usage.Cumulative, head.OriginalUsage.Cumulative) {
		return api.E("idempotency_conflict", "remote_legacy_open_state_changed")
	}
	if err = r.validateStateUsage(packet, state); err != nil {
		return err
	}
	issued, err := api.ParseTime(state.IssuedAt)
	if err != nil {
		return err
	}
	digest, err := remoteStateDigest(state)
	if err != nil {
		return err
	}
	// 这里只验证原持久历史事实的签名；不携带它授予当前开始权或延期限。
	_, err = peer.Keys.Verify(state.Proof, remoteStateClaims(peer.Scope, state, digest), issued)
	return err
}
