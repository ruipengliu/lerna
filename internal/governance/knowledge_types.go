package governance

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// Skill 是带来源的操作知识，不是系统指令、执行权限或成功证据。
type SkillDefinition struct {
	SkillRef         api.ComponentRef `json:"skill_ref"`
	BodyRef          api.ContentRef   `json:"body_ref"`
	UsageContractRef api.ContentRef   `json:"usage_contract_ref"`
	SourceRefs       []api.ContentRef `json:"source_refs"`
}
type SkillUsageContract struct {
	Purpose              string             `json:"purpose"`
	Preconditions        []string           `json:"preconditions"`
	Counterexamples      []string           `json:"counterexamples"`
	ToolDependencies     []api.ComponentRef `json:"tool_dependencies"`
	EvidenceDependencies []api.ContentRef   `json:"evidence_dependencies"`
	Conflicts            []api.ComponentRef `json:"conflicts"`
	ExitRules            []string           `json:"exit_rules"`
}
type SkillRecord struct {
	ID                  string              `json:"id"`
	Revision            uint64              `json:"revision"`
	Definition          SkillDefinition     `json:"definition"`
	AuthorRef           api.ObjectRef       `json:"author_ref"`
	InstallLockRef      api.ComponentRef    `json:"install_lock_ref"`
	State               string              `json:"state"`
	ValidationCommandID string              `json:"validation_command_id"`
	Usage               *SkillUsageContract `json:"usage,omitempty"`
	FailureReason       string              `json:"failure_reason,omitempty"`
}
type SkillReference struct {
	SkillRef api.ComponentRef `json:"skill_ref"`
}
type LoadedSkill struct {
	Definition SkillDefinition    `json:"definition"`
	Body       string             `json:"body"`
	Usage      SkillUsageContract `json:"usage"`
}

// 控制上限限本轮 Decision/行动；Task 累计预算和 Grant 始终由原 owner 核验。
type KnowledgeControls struct {
	MaxInputBytes             uint64       `json:"max_input_bytes"`
	MaxOutputTokens           uint64       `json:"max_output_tokens"`
	MaxActionsPerDecision     uint64       `json:"max_actions_per_decision"`
	MaxDelegationsPerDecision uint64       `json:"max_delegations_per_decision"`
	MaxDepth                  uint64       `json:"max_depth"`
	MaxActionDurationSeconds  uint64       `json:"max_action_duration_seconds"`
	MaxCallCostBound          []api.Amount `json:"max_call_cost_bound"`
}
type AgentConfigDefinition struct {
	AgentConfigRef   api.ComponentRef   `json:"agent_config_ref"`
	BrainRef         api.ComponentRef   `json:"brain_ref"`
	CapabilityRefs   []api.ComponentRef `json:"capability_refs"`
	ControlLimitsRef api.ContentRef     `json:"control_limits_ref"`
	SourceRefs       []api.ContentRef   `json:"source_refs"`
}
type AgentConfigRecord struct {
	ID                  string                `json:"id"`
	Revision            uint64                `json:"revision"`
	Definition          AgentConfigDefinition `json:"definition"`
	AuthorRef           api.ObjectRef         `json:"author_ref"`
	InstallLockRef      api.ComponentRef      `json:"install_lock_ref"`
	State               string                `json:"state"`
	ValidationCommandID string                `json:"validation_command_id"`
	Controls            *KnowledgeControls    `json:"controls,omitempty"`
	FailureReason       string                `json:"failure_reason,omitempty"`
}
type AgentConfigReference struct {
	AgentConfigRef api.ComponentRef `json:"agent_config_ref"`
}
type KnowledgeRequest struct {
	TaskRef              api.ObjectRef      `json:"task_ref"`
	SnapshotID           string             `json:"snapshot_id"`
	BrainRef             api.ComponentRef   `json:"brain_ref"`
	ParentInstallLockRef api.ComponentRef   `json:"parent_install_lock_ref"`
	SkillRefs            []api.ComponentRef `json:"skill_refs"`
	AgentConfigRef       *api.ComponentRef  `json:"agent_config_ref,omitempty"`
	CapabilityRefs       []api.ComponentRef `json:"capability_refs"`
	ControlLimits        KnowledgeControls  `json:"control_limits"`
	ExpiresAt            string             `json:"expires_at"`
}
type KnowledgePacket struct {
	Kind        string                 `json:"kind"`
	Skills      []LoadedSkill          `json:"skills"`
	AgentConfig *AgentConfigDefinition `json:"agent_config,omitempty"`
}
type KnowledgeSelection struct {
	ID                      string             `json:"id"`
	Revision                uint64             `json:"revision"`
	Request                 KnowledgeRequest   `json:"request"`
	SubjectRef              api.ObjectRef      `json:"subject_ref"`
	SkillRecordRefs         []api.ObjectRef    `json:"skill_record_refs"`
	AgentRecordRef          *api.ObjectRef     `json:"agent_record_ref,omitempty"`
	InstallLockRef          api.ComponentRef   `json:"install_lock_ref"`
	KnowledgeLockRefs       []api.ComponentRef `json:"knowledge_lock_refs"`
	EffectiveCapabilityRefs []api.ComponentRef `json:"effective_capability_refs"`
	EffectiveControls       KnowledgeControls  `json:"effective_controls"`
	SourceRefs              []api.ContentRef   `json:"source_refs"`
	PacketHash              string             `json:"packet_hash"`
	PacketByteLength        uint64             `json:"packet_byte_length"`
}
type KnowledgeBundle struct {
	Selection KnowledgeSelection `json:"selection"`
	Packet    KnowledgePacket    `json:"packet"`
}
type KnowledgeChange struct {
	Ref    api.ObjectRef `json:"ref"`
	Reason string        `json:"reason"`
}

// KnowledgeGate 必须在读正文之前与准入事务内核当前准确 Content 权利。
// 依赖权限/来源闭包由实际 Content owner 检查，接口不能出站或跨库读取。
type KnowledgeGate interface {
	CheckTx(context.Context, runtime.Tx, runtime.Auth, []api.ContentRef, string) error
}

func SealSkill(in SkillDefinition) (SkillDefinition, error) {
	in.SkillRef.Digest = ""
	digest, err := api.Digest(struct {
		Kind       string
		Definition SkillDefinition
	}{"skill/1", in})
	if err != nil {
		return in, err
	}
	in.SkillRef.Digest = digest
	return in, nil
}

// SkillInstallLock 是准确数据制品闭包，不声明程序安装、自检或 runtime readiness。
func SkillInstallLock(in SkillDefinition) api.ComponentRef {
	digest, _ := api.Digest(struct {
		Kind  string
		Skill SkillDefinition
	}{"knowledge-data-install/1", in})
	return api.ComponentRef{ComponentID: in.SkillRef.ComponentID, Version: in.SkillRef.Version, Digest: digest}
}
func SealAgentConfig(in AgentConfigDefinition) (AgentConfigDefinition, error) {
	in.AgentConfigRef.Digest = ""
	digest, err := api.Digest(struct {
		Kind       string
		Definition AgentConfigDefinition
	}{"agent-config/1", in})
	in.AgentConfigRef.Digest = digest
	return in, err
}
func AgentConfigInstallLock(in AgentConfigDefinition) api.ComponentRef {
	digest, _ := api.Digest(struct {
		Kind  string
		Agent AgentConfigDefinition
	}{"knowledge-agent-install/1", in})
	return api.ComponentRef{ComponentID: in.AgentConfigRef.ComponentID, Version: in.AgentConfigRef.Version, Digest: digest}
}
