package governance

import (
	"context"
	"sort"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

func minimum(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
func intersectControls(parent, config KnowledgeControls) KnowledgeControls {
	out := KnowledgeControls{MaxInputBytes: minimum(parent.MaxInputBytes, config.MaxInputBytes), MaxOutputTokens: minimum(parent.MaxOutputTokens, config.MaxOutputTokens), MaxActionsPerDecision: minimum(parent.MaxActionsPerDecision, config.MaxActionsPerDecision), MaxDelegationsPerDecision: minimum(parent.MaxDelegationsPerDecision, config.MaxDelegationsPerDecision), MaxDepth: minimum(parent.MaxDepth, config.MaxDepth), MaxActionDurationSeconds: minimum(parent.MaxActionDurationSeconds, config.MaxActionDurationSeconds), MaxCallCostBound: []api.Amount{}}
	for _, bound := range parent.MaxCallCostBound {
		value := amountValue(config.MaxCallCostBound, bound.Unit)
		cmp, _ := api.CompareDecimal(bound.Value, value)
		if cmp < 0 {
			value = bound.Value
		}
		out.MaxCallCostBound = append(out.MaxCallCostBound, api.Amount{Unit: bound.Unit, Value: value})
	}
	return out
}
func hasComponent(refs []api.ComponentRef, ref api.ComponentRef) bool {
	for _, candidate := range refs {
		if api.Equal(candidate, ref) {
			return true
		}
	}
	return false
}
func uniqueKnowledgeSources(refs []api.ContentRef) []api.ContentRef {
	out := []api.ContentRef{}
	seen := map[string]bool{}
	for _, ref := range refs {
		key := digestID("content", ref)
		if !seen[key] {
			seen[key] = true
			out = append(out, ref)
		}
	}
	return out
}
func knowledgeSnapshotLock(request KnowledgeRequest, locks []api.ComponentRef) api.ComponentRef {
	digest, _ := api.Digest(struct {
		Kind      string
		Parent    api.ComponentRef
		Knowledge []api.ComponentRef
	}{"knowledge-snapshot-install/1", request.ParentInstallLockRef, locks})
	return api.ComponentRef{ComponentID: digestID("install_lock", request.SnapshotID), Version: "1", Digest: digest}
}
func validateKnowledgeRequest(scope runtime.Scope, in KnowledgeRequest) error {
	if err := ownerRef(scope, in.TaskRef); err != nil {
		return err
	}
	if !api.ValidID(in.SnapshotID) {
		return api.E("invalid_request", "knowledge_snapshot_id_invalid")
	}
	for _, ref := range []api.ComponentRef{in.BrainRef, in.ParentInstallLockRef} {
		if err := api.ValidateRecord("ComponentRef", ref); err != nil {
			return err
		}
	}
	if err := validateComponentSet(in.SkillRefs, 8); err != nil {
		return err
	}
	if err := validateComponentSet(in.CapabilityRefs, 32); err != nil {
		return err
	}
	if in.AgentConfigRef != nil {
		if err := api.ValidateRecord("ComponentRef", *in.AgentConfigRef); err != nil {
			return err
		}
	}
	if _, err := api.ParseTime(in.ExpiresAt); err != nil {
		return api.E("invalid_request", "knowledge_expiry_invalid")
	}
	return validateKnowledgeControls(in.ControlLimits)
}

// LoadKnowledge 在 Tx 外读准确正文，纯 Tx 当前来源检查始终先于领域强锁。
// 返回的 Packet 只能作为普通 Material；它不修改原 Grant/预算或模型系统指令。
func (s *Service) LoadKnowledge(ctx context.Context, scope runtime.Scope, auth runtime.Auth, in KnowledgeRequest) (KnowledgeBundle, error) {
	if err := validateKnowledgeRequest(scope, in); err != nil {
		return KnowledgeBundle{}, err
	}
	out := KnowledgeBundle{Selection: KnowledgeSelection{ID: digestID("knowledge", []any{in.SnapshotID, auth.Ref(scope.OwnerID)}), Revision: 1, Request: in, SubjectRef: auth.Ref(scope.OwnerID), SkillRecordRefs: []api.ObjectRef{}, KnowledgeLockRefs: []api.ComponentRef{}, EffectiveCapabilityRefs: append([]api.ComponentRef{}, in.CapabilityRefs...), EffectiveControls: in.ControlLimits, SourceRefs: []api.ContentRef{}}, Packet: KnowledgePacket{Kind: "ordinary_knowledge/1", Skills: []LoadedSkill{}}}
	var agent *AgentConfigRecord
	if in.AgentConfigRef != nil {
		record, err := s.readAgentConfig(ctx, s.Store, scope, auth, api.Query{}, AgentConfigReference{AgentConfigRef: *in.AgentConfigRef})
		if err != nil {
			return KnowledgeBundle{}, err
		}
		if record.State != "active" || record.Controls == nil {
			return KnowledgeBundle{}, api.E("invalid_state", "agent_config_not_active")
		}
		if !api.Equal(record.Definition.BrainRef, in.BrainRef) {
			return KnowledgeBundle{}, api.E("unsupported", "agent_config_brain_not_registered")
		}
		agent = &record
		out.Selection.AgentRecordRef = new(scope.Ref(record.ID, record.Revision))
		out.Selection.EffectiveCapabilityRefs = []api.ComponentRef{}
		for _, ref := range in.CapabilityRefs {
			if hasComponent(record.Definition.CapabilityRefs, ref) {
				out.Selection.EffectiveCapabilityRefs = append(out.Selection.EffectiveCapabilityRefs, ref)
			}
		}
		out.Selection.EffectiveControls = intersectControls(in.ControlLimits, *record.Controls)
		out.Selection.KnowledgeLockRefs = append(out.Selection.KnowledgeLockRefs, record.InstallLockRef)
		out.Selection.SourceRefs = append(out.Selection.SourceRefs, agentContents(record.Definition)...)
		out.Packet.AgentConfig = &record.Definition
	}
	skills := map[string]SkillRecord{}
	for _, ref := range in.SkillRefs {
		record, err := s.readSkill(ctx, s.Store, scope, auth, api.Query{}, SkillReference{SkillRef: ref})
		if err != nil {
			return KnowledgeBundle{}, err
		}
		if record.State != "active" || record.Usage == nil {
			return KnowledgeBundle{}, api.E("invalid_state", "skill_not_active")
		}
		for _, dep := range record.Usage.ToolDependencies {
			if !hasComponent(out.Selection.EffectiveCapabilityRefs, dep) {
				return KnowledgeBundle{}, api.E("invalid_state", "skill_tool_dependency_unavailable")
			}
		}
		for _, conflict := range record.Usage.Conflicts {
			if hasComponent(in.SkillRefs, conflict) || hasComponent(out.Selection.EffectiveCapabilityRefs, conflict) {
				return KnowledgeBundle{}, api.E("invalid_state", "skill_conflict_selected")
			}
		}
		body, err := s.knowledgeBytes(ctx, scope, auth, record.Definition.BodyRef, "skill.load", maxSkillBodyBytes)
		if err != nil {
			return KnowledgeBundle{}, err
		}
		skills[record.ID] = record
		out.Packet.Skills = append(out.Packet.Skills, LoadedSkill{Definition: record.Definition, Body: string(body), Usage: *record.Usage})
		out.Selection.SkillRecordRefs = append(out.Selection.SkillRecordRefs, scope.Ref(record.ID, record.Revision))
		out.Selection.KnowledgeLockRefs = append(out.Selection.KnowledgeLockRefs, record.InstallLockRef)
		out.Selection.SourceRefs = append(out.Selection.SourceRefs, skillContents(record.Definition)...)
	}
	out.Selection.SourceRefs = uniqueKnowledgeSources(out.Selection.SourceRefs)
	packetBytes := api.Raw(out.Packet)
	if len(packetBytes) > 128<<10 {
		return KnowledgeBundle{}, api.E("invalid_request", "knowledge_packet_limit")
	}
	out.Selection.PacketHash, out.Selection.PacketByteLength = api.Hash(packetBytes), uint64(len(packetBytes))
	out.Selection.InstallLockRef = knowledgeSnapshotLock(in, out.Selection.KnowledgeLockRefs)
	status, err := s.Store.Within(ctx, scope, s.participants(), func(tx runtime.Tx) error {
		now, err := tx.Now(ctx)
		if err != nil {
			return err
		}
		if err := before(now, in.ExpiresAt); err != nil {
			return err
		}
		if err := s.checkKnowledgeTx(ctx, tx, auth, out.Selection.SourceRefs, "knowledge.load"); err != nil {
			return err
		}
		ids := []string{}
		for id := range skills {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			var current SkillRecord
			rev, err := tx.Get(ctx, ns("skills"), id, &current)
			if err != nil {
				return err
			}
			if current.State != "active" || rev != skills[id].Revision {
				return api.E("revision_conflict", "skill_changed_during_load")
			}
		}
		if agent != nil {
			var current AgentConfigRecord
			rev, err := tx.Get(ctx, ns("agent_configs"), agent.ID, &current)
			if err != nil {
				return err
			}
			if current.State != "active" || rev != agent.Revision {
				return api.E("revision_conflict", "agent_config_changed_during_load")
			}
		}
		return nil
	})
	if status == runtime.CommitUnknown {
		return KnowledgeBundle{}, runtime.ErrCommitUnknown
	}
	if err != nil {
		return KnowledgeBundle{}, err
	}
	return out, nil
}
