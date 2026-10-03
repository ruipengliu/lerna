package development

import (
	"context"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/brain"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/internal/task"
	"github.com/ruipengliu/lerna/runtime"
)

type KnowledgeConfig struct {
	SkillRefs      []api.ComponentRef           `json:"skill_refs"`
	AgentConfigRef *api.ComponentRef            `json:"agent_config_ref,omitempty"`
	ControlLimits  governance.KnowledgeControls `json:"control_limits"`
}
type KnowledgeAssembly struct {
	a      *App
	config *KnowledgeConfig
}
type knowledgePrepared struct {
	ID          string                        `json:"id"`
	Revision    uint64                        `json:"revision"`
	Selection   governance.KnowledgeSelection `json:"selection"`
	PacketRef   api.ContentRef                `json:"packet_ref"`
	SnapshotRef api.ContentRef                `json:"snapshot_ref"`
	DecisionID  string                        `json:"decision_id"`
}

func RequiredKnowledgeContentPurposes() []string {
	return []string{"skill.register", "skill.read", "skill.load", "skill.validate", "skill.reopen", "agent_config.register", "agent_config.read", "agent_config.validate", "agent_config.reopen", "knowledge.load", "knowledge.consume"}
}

type knowledgeContentGate struct{ a *App }

func (g knowledgeContentGate) CheckTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, refs []api.ContentRef, purpose string) error {
	for _, ref := range refs {
		if _, err := g.a.Memory.CheckContentTx(ctx, tx, auth, ref, purpose, "cloud", true); err != nil {
			return err
		}
	}
	return nil
}
func configureKnowledge(a *App, cfg *KnowledgeConfig) (*KnowledgeAssembly, error) {
	if a == nil || a.Store == nil || a.Memory == nil || a.Governance == nil || a.Scope.DatabaseID != a.Store.ID() {
		return nil, api.E("invalid_request", "bound_knowledge_services_required")
	}
	assembly := &KnowledgeAssembly{a: a}
	if cfg == nil {
		return assembly, nil
	}
	if len(cfg.SkillRefs) > 8 {
		return nil, api.E("invalid_request", "skill_selection_limit")
	}
	if err := governance.ValidateKnowledgeControls(cfg.ControlLimits); err != nil {
		return nil, err
	}
	for _, ref := range cfg.SkillRefs {
		if err := api.ValidateRecord("ComponentRef", ref); err != nil {
			return nil, err
		}
	}
	if cfg.AgentConfigRef != nil {
		if err := api.ValidateRecord("ComponentRef", *cfg.AgentConfigRef); err != nil {
			return nil, err
		}
	}
	var frozen KnowledgeConfig
	if err := api.Decode(api.Raw(cfg), &frozen); err != nil {
		return nil, err
	}
	assembly.config = &frozen
	return assembly, nil
}
func (k *KnowledgeAssembly) Enabled() bool { return k != nil && k.config != nil }
func narrowKnowledgeAmountBounds(config, policy []api.Amount) []api.Amount {
	out := []api.Amount{}
	for _, candidate := range config {
		value := "0"
		for _, limit := range policy {
			if limit.Unit == candidate.Unit {
				value = limit.Value
				break
			}
		}
		cmp, _ := api.CompareDecimal(candidate.Value, value)
		if cmp < 0 {
			value = candidate.Value
		}
		out = append(out, api.Amount{Unit: candidate.Unit, Value: value})
	}
	return out
}
func smallestKnowledgeBound(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}

// Load 只发布普通资料；cap/binding 同索引过滤与真实编码核验由 Context 随后完成。
func (k *KnowledgeAssembly) Load(ctx context.Context, scope runtime.Scope, auth runtime.Auth, t api.Task, snapshotID string, parentLock api.ComponentRef, caps []api.ComponentRef) (governance.KnowledgeBundle, api.ContentRef, error) {
	if !k.Enabled() {
		return governance.KnowledgeBundle{}, api.ContentRef{}, api.E("unsupported", "knowledge_selection_not_configured")
	}
	if !api.Equal(scope, k.a.Scope) {
		return governance.KnowledgeBundle{}, api.ContentRef{}, api.E("forbidden", "knowledge_scope_mismatch")
	}
	bounds := k.config.ControlLimits
	bounds.MaxInputBytes = smallestKnowledgeBound(bounds.MaxInputBytes, k.a.Profile.MaxInputBytes)
	bounds.MaxOutputTokens = smallestKnowledgeBound(bounds.MaxOutputTokens, k.a.Profile.MaxOutputTokens)
	bounds.MaxDelegationsPerDecision = smallestKnowledgeBound(bounds.MaxDelegationsPerDecision, k.a.TaskPolicy.MaxDelegations)
	bounds.MaxDepth = smallestKnowledgeBound(bounds.MaxDepth, k.a.TaskPolicy.MaxDepth)
	bounds.MaxActionDurationSeconds = smallestKnowledgeBound(bounds.MaxActionDurationSeconds, k.a.TaskPolicy.MaxDurationSeconds)
	bounds.MaxCallCostBound = narrowKnowledgeAmountBounds(bounds.MaxCallCostBound, k.a.TaskPolicy.BudgetLimits)
	request := governance.KnowledgeRequest{TaskRef: scope.Ref(t.TaskID, t.Revision), SnapshotID: snapshotID, BrainRef: k.a.Profile.Ref, ParentInstallLockRef: parentLock, SkillRefs: k.config.SkillRefs, AgentConfigRef: k.config.AgentConfigRef, CapabilityRefs: caps, ControlLimits: bounds, ExpiresAt: t.Deadline}
	bundle, err := k.a.Governance.LoadKnowledge(ctx, scope, auth, request)
	if err != nil {
		return bundle, api.ContentRef{}, err
	}
	packet, err := k.a.Publish(ctx, scope, k.a.ServiceAuth, stableID("content", "knowledge/"+snapshotID), "application/vnd.harness.knowledge+json", api.Raw(bundle.Packet), bundle.Selection.SourceRefs, []api.ContentRef{})
	return bundle, packet, err
}
func knowledgeCostsBounded(actual, ceilings []api.Amount) bool {
	if api.ValidateAmounts(actual) != nil {
		return false
	}
	for _, fee := range actual {
		limit := "0"
		for _, bound := range ceilings {
			if bound.Unit == fee.Unit {
				limit = bound.Value
				break
			}
		}
		cmp, err := api.CompareDecimal(fee.Value, limit)
		if err != nil || cmp > 0 {
			return false
		}
	}
	return true
}

// CheckEncoding 使用实际完整编码 bytes 与费用上界；不能仅报告取过 min。
func (k *KnowledgeAssembly) CheckEncoding(bundle governance.KnowledgeBundle, snapshot api.Snapshot, encoding brain.Encoding, cost []api.Amount) error {
	limits := bundle.Selection.EffectiveControls
	if uint64(len(encoding.Body)) > limits.MaxInputBytes || snapshot.ReservedOutputTokens > limits.MaxOutputTokens || !knowledgeCostsBounded(cost, limits.MaxCallCostBound) {
		return api.E("forbidden", "knowledge_model_control_exceeded")
	}
	return nil
}

// Freeze 在 Tx 外的本方短事务保存机械准备元数据，不改变 Task 或预算。
func (k *KnowledgeAssembly) Freeze(ctx context.Context, scope runtime.Scope, auth runtime.Auth, bundle governance.KnowledgeBundle, packet api.ContentRef, p task.PreparedDecision) error {
	if !k.Enabled() {
		return api.E("unsupported", "knowledge_selection_not_configured")
	}
	if !api.Equal(scope, k.a.Scope) || !api.Equal(auth.Ref(scope.OwnerID), bundle.Selection.SubjectRef) || p.Snapshot.SnapshotID != bundle.Selection.Request.SnapshotID {
		return api.E("forbidden", "knowledge_preparation_identity_mismatch")
	}
	prepared := knowledgePrepared{ID: p.DecisionID, Revision: 1, Selection: bundle.Selection, PacketRef: packet, SnapshotRef: p.SnapshotRef, DecisionID: p.DecisionID}
	status, err := k.a.Store.Within(ctx, scope, []string{"platform"}, func(tx runtime.Tx) error {
		var old knowledgePrepared
		_, err := tx.Get(ctx, "platform.knowledge_prepared", prepared.ID, &old)
		if err == runtime.ErrNotFound {
			return tx.Create(ctx, "platform.knowledge_prepared", prepared.ID, p.Snapshot.SnapshotID, prepared)
		}
		if err != nil {
			return err
		}
		if !api.Equal(old, prepared) {
			return api.E("idempotency_conflict", "original_prepared_knowledge_changed")
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}

// CommitTx 可由 ContextCompiler 的 task.ContextCommitter 机械委托；在最终 Job 前运行。
func (k *KnowledgeAssembly) CommitTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, p task.PreparedDecision) error {
	if k == nil {
		return nil
	}
	var prepared knowledgePrepared
	err := tx.GetVersion(ctx, "platform.knowledge_prepared", p.DecisionID, 1, &prepared)
	if err == runtime.ErrNotFound && !k.Enabled() {
		return nil
	}
	if err != nil {
		return err
	}
	if !api.Equal(prepared.SnapshotRef, p.SnapshotRef) || prepared.DecisionID != p.DecisionID {
		return api.E("forbidden", "original_prepared_knowledge_mismatch")
	}
	if !knowledgeCostsBounded(p.CostBound, prepared.Selection.EffectiveControls.MaxCallCostBound) {
		return api.E("forbidden", "knowledge_model_control_exceeded")
	}
	_, err = k.a.Governance.StageSelectionTx(ctx, tx, auth, governance.KnowledgeAdmission{Selection: prepared.Selection, PacketRef: prepared.PacketRef, Snapshot: p.Snapshot, SnapshotRef: p.SnapshotRef, DecisionID: p.DecisionID})
	return err
}

// CheckActionTx 重核当前选择后才可 Use/准入，防止 Proposal 准备与接纳间撤回。
func (k *KnowledgeAssembly) CheckActionTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, intent task.OperationIntent) error {
	if k == nil || intent.AdmissionSourceKind != "decision" {
		return nil
	}
	commit, found, err := k.a.Governance.FindDecisionSelectionTx(ctx, tx, auth, intent.AdmissionSourceRef.ObjectID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	if commit.Selection.Request.TaskRef.ObjectID != intent.TaskRef.ObjectID {
		return api.E("forbidden", "knowledge_original_task_mismatch")
	}
	allowed := false
	for _, cap := range commit.Selection.EffectiveCapabilityRefs {
		allowed = allowed || api.Equal(cap, intent.CapabilityRef)
	}
	if !allowed || !knowledgeCostsBounded(intent.CostBound, commit.Selection.EffectiveControls.MaxCallCostBound) {
		return api.E("forbidden", "knowledge_action_control_exceeded")
	}
	now, err := tx.Now(ctx)
	if err != nil {
		return err
	}
	deadline, err := api.ParseTime(intent.Deadline)
	if err != nil {
		return err
	}
	if !deadline.After(now) || deadline.Sub(now) > time.Duration(commit.Selection.EffectiveControls.MaxActionDurationSeconds)*time.Second {
		return api.E("forbidden", "knowledge_action_duration_exceeded")
	}
	return nil
}

// CheckActionSnapshotLock 核复合数据锁的原父程序资格；数据锁不能替换 executable leaf。
func (k *KnowledgeAssembly) CheckActionSnapshotLock(ctx context.Context, scope runtime.Scope, auth runtime.Auth, intent api.DecisionDispatchIntent, snapshot api.Snapshot, parent api.ComponentRef) error {
	if k == nil {
		if !api.Equal(parent, snapshot.InstallLockRef) {
			return api.E("forbidden", "original_action_snapshot_mismatch")
		}
		return nil
	}
	status, err := k.a.Store.Within(ctx, scope, []string{"content", "memory", "governance", "platform"}, func(tx runtime.Tx) error {
		commit, found, err := k.a.Governance.FindDecisionSelectionTx(ctx, tx, auth, intent.DecisionID)
		if err != nil {
			return err
		}
		if !found {
			if !api.Equal(parent, snapshot.InstallLockRef) {
				return api.E("forbidden", "original_action_snapshot_mismatch")
			}
			return nil
		}
		body := api.Raw(snapshot)
		if !api.Equal(commit.SnapshotRef, intent.SnapshotRef) || !api.Equal(commit.Selection.Request.TaskRef, intent.TaskRef) || snapshot.SnapshotID != commit.Selection.Request.SnapshotID || snapshot.Revision != intent.SnapshotRevision || api.Hash(body) != commit.SnapshotRef.Hash || uint64(len(body)) != commit.SnapshotRef.ByteLength || !api.Equal(snapshot.InstallLockRef, commit.Selection.InstallLockRef) || !api.Equal(parent, commit.Selection.Request.ParentInstallLockRef) {
			return api.E("forbidden", "knowledge_original_action_snapshot_mismatch")
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return runtime.ErrCommitUnknown
	}
	return err
}

// CheckBrainTx 同样核已保存的历史 selection，即使配置已停用；原账单恢复另由 Brain 负责。
func (k *KnowledgeAssembly) CheckBrainTx(ctx context.Context, tx runtime.Tx, auth runtime.Auth, in brain.DecideInput, encoding *brain.Encoding) error {
	if k == nil {
		return nil
	}
	commit, found, err := k.a.Governance.FindSelectionTx(ctx, tx, auth, in.SnapshotRef, in.DecisionID)
	if err != nil {
		return err
	}
	if !found {
		return nil
	}
	limits := commit.Selection.EffectiveControls
	if !knowledgeCostsBounded(in.Limits, limits.MaxCallCostBound) || !api.Equal(in.ModelProfileRef, commit.Selection.Request.BrainRef) {
		return api.E("forbidden", "knowledge_model_control_exceeded")
	}
	deadline, err := api.ParseTime(in.Deadline)
	if err != nil {
		return err
	}
	original, err := api.ParseTime(commit.Selection.Request.ExpiresAt)
	if err != nil {
		return err
	}
	if deadline.After(original) {
		return api.E("forbidden", "knowledge_model_deadline_expanded")
	}
	if encoding != nil && uint64(len(encoding.Body)) > limits.MaxInputBytes {
		return api.E("forbidden", "knowledge_model_input_exceeded")
	}
	return nil
}

// ProposalLimits 在已解析原 Proposal 之后、任何 Use/准入之前校验行动数量与准确能力。
func (k *KnowledgeAssembly) ProposalLimits(ctx context.Context, scope runtime.Scope, auth runtime.Auth, intent api.DecisionDispatchIntent, proposal brain.Proposal) (*governance.KnowledgeCommit, error) {
	if k == nil {
		return nil, nil
	}
	var commit governance.KnowledgeCommit
	var found bool
	status, err := k.a.Store.Within(ctx, scope, []string{"content", "memory", "governance", "platform"}, func(tx runtime.Tx) error {
		var err error
		commit, found, err = k.a.Governance.FindSelectionTx(ctx, tx, auth, intent.SnapshotRef, intent.DecisionID)
		return err
	})
	if status == runtime.CommitUnknown {
		return nil, runtime.ErrCommitUnknown
	}
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}
	if uint64(len(proposal.Actions)) > commit.Selection.EffectiveControls.MaxActionsPerDecision {
		return nil, api.E("forbidden", "knowledge_action_count_exceeded")
	}
	for _, action := range proposal.Actions {
		allowed := false
		for _, cap := range commit.Selection.EffectiveCapabilityRefs {
			allowed = allowed || api.Equal(cap, action.CapabilityRef)
		}
		if !allowed {
			return nil, api.E("forbidden", "knowledge_action_control_exceeded")
		}
	}
	return &commit, nil
}
