// Package brain 保存决策、单次物理请求及原输出发布；不裁决 Task 成功。
package brain

import (
	"context"
	"encoding/json"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
	"time"
)

const Namespace = "brain"
const JobAdvance = "brain.advance"
const records = "brain.decisions"

type Profile struct {
	Ref                                                                        api.ComponentRef
	ContextLimit, MaxInputTokens, MaxOutputTokens, SafetyMargin, MaxInputBytes uint64
	RequestTimeout                                                             time.Duration
}
type Content interface {
	Read(context.Context, runtime.Scope, runtime.Auth, api.ContentRef, string) ([]byte, error)
	Publish(context.Context, runtime.Scope, runtime.Auth, Publication, []byte) (api.ContentRef, error)
}
type Publication struct {
	ContentID                          string
	MediaType                          string
	ProcessedSources, DisclosedSources []api.ContentRef
}
type Gate interface {
	CheckTx(context.Context, runtime.Tx, runtime.Auth, DecideInput, *Encoding) error
}

// Engine 的真实出口须比较完整编码摘要；Lookup 只能核原调用，绝不重发。
type Engine interface {
	Physical() bool
	Encode(context.Context, api.Snapshot, []byte, Profile) (Encoding, error)
	Request(context.Context, string, Encoding) (Generated, error)
	Lookup(context.Context, string, Encoding) (Generated, error)
}
type Encoding struct {
	Body             json.RawMessage  `json:"body"`
	Digest           string           `json:"digest"`
	Receiver         string           `json:"receiver"`
	Location         string           `json:"location"`
	InputTokens      uint64           `json:"input_tokens"`
	CountMode        string           `json:"count_mode"`
	ProcessedSources []api.ContentRef `json:"processed_sources"`
}
type Config struct {
	Profiles     []Profile
	Content      Content
	Engine       Engine
	Gate         Gate
	Participants []string
}
type DecideInput struct {
	DecisionID       string           `json:"decision_id"`
	TaskRef          api.ObjectRef    `json:"task_ref"`
	SnapshotRef      api.ContentRef   `json:"snapshot_ref"`
	SnapshotRevision uint64           `json:"snapshot_revision"`
	ModelProfileRef  api.ComponentRef `json:"model_profile_ref"`
	UseRefs          []api.ObjectRef  `json:"use_refs"`
	Limits           []api.Amount     `json:"limits"`
	Deadline         string           `json:"deadline"`
}
type CancelInput struct {
	DecisionID string        `json:"decision_id"`
	TaskRef    api.ObjectRef `json:"task_ref"`
	Reason     string        `json:"reason"`
}
type GetInput struct {
	DecisionID string `json:"decision_id"`
}
type Output struct {
	DecisionRef api.ObjectRef `json:"decision_ref"`
	Status      string        `json:"status"`
}
type View struct {
	Decision        api.DecisionRecord `json:"decision"`
	TaskRef         api.ObjectRef      `json:"task_ref"`
	SnapshotRef     api.ContentRef     `json:"snapshot_ref"`
	Publication     string             `json:"publication"`
	CallID          string             `json:"call_id"`
	CancelRequested bool               `json:"cancel_requested"`
}
type ActionCandidate struct {
	LocalKey            string           `json:"local_key"`
	CapabilityRef       api.ComponentRef `json:"capability_ref"`
	BindingRef          api.ObjectRef    `json:"binding_ref"`
	ArgumentsRef        api.ContentRef   `json:"arguments_ref"`
	ProcessedSourceRefs []api.ContentRef `json:"processed_source_refs"`
	DisclosedSourceRefs []api.ContentRef `json:"disclosed_source_refs"`
}
type CheckSuggestion struct {
	RequirementRef api.RequirementRef `json:"requirement_ref"`
	EvidenceRefs   []api.ContentRef   `json:"evidence_refs"`
}
type ContextLookup struct {
	Kind      string         `json:"kind"`
	TargetRef api.ObjectRef  `json:"target_ref"`
	QueryRef  api.ContentRef `json:"query_ref"`
}

// Proposal 的 kind 特有字段在 Validator 的 oneOf 中闭合。不得含 Grant/Operation 权威。
type Proposal struct {
	Kind               string                `json:"kind"`
	ReasonRef          api.ContentRef        `json:"reason_ref"`
	RequirementDelta   *api.RequirementDelta `json:"requirement_delta,omitempty"`
	Actions            []ActionCandidate     `json:"actions,omitempty"`
	Lookups            []ContextLookup       `json:"lookups,omitempty"`
	QuestionRef        *api.ContentRef       `json:"question_ref,omitempty"`
	AnswerSchemaRef    *api.ComponentRef     `json:"answer_schema_ref,omitempty"`
	PreviewRefs        []api.ContentRef      `json:"preview_refs,omitempty"`
	Purpose            string                `json:"purpose,omitempty"`
	ArtifactRefs       []api.ContentRef      `json:"artifact_refs,omitempty"`
	CheckSuggestions   []CheckSuggestion     `json:"check_suggestions,omitempty"`
	ReasonCode         string                `json:"reason_code,omitempty"`
	EvidenceRefs       []api.ContentRef      `json:"evidence_refs,omitempty"`
	ExplanationRef     *api.ContentRef       `json:"explanation_ref,omitempty"`
	ResumeConditionRef *api.ContentRef       `json:"resume_condition_ref,omitempty"`
}

// Draft 中只有受限 local_id；真实 Content 身份由发布责任持久分配。
type GeneratedContent struct {
	LocalID          string           `json:"local_id"`
	MediaType        string           `json:"media_type"`
	Body             string           `json:"body"`
	DisclosedSources []api.ContentRef `json:"disclosed_sources"`
	ContentLocalID   string           `json:"content_local_id,omitempty"`
}
type DraftRequirement struct {
	CandidateKey      string           `json:"candidate_key"`
	Kind              string           `json:"kind"`
	StatementLocalID  string           `json:"statement_local_id"`
	ParametersLocalID string           `json:"parameters_local_id,omitempty"`
	RuleRef           api.ComponentRef `json:"rule_ref"`
	Required          bool             `json:"required"`
}
type DraftAction struct {
	LocalKey         string           `json:"local_key"`
	CapabilityRef    api.ComponentRef `json:"capability_ref"`
	BindingRef       api.ObjectRef    `json:"binding_ref"`
	ArgumentsLocalID string           `json:"arguments_local_id"`
}
type Draft struct {
	Kind                 string             `json:"kind"`
	ReasonLocalID        string             `json:"reason_local_id"`
	Requirements         []DraftRequirement `json:"requirements,omitempty"`
	Actions              []DraftAction      `json:"actions,omitempty"`
	ArtifactLocalIDs     []string           `json:"artifact_local_ids,omitempty"`
	ExistingArtifactRefs []api.ContentRef   `json:"existing_artifact_refs,omitempty"`
	QuestionLocalID      string             `json:"question_local_id,omitempty"`
	AnswerSchemaRef      *api.ComponentRef  `json:"answer_schema_ref,omitempty"`
	Purpose              string             `json:"purpose,omitempty"`
	ReasonCode           string             `json:"reason_code,omitempty"`
}
type Generated struct {
	Draft      Draft              `json:"draft"`
	Contents   []GeneratedContent `json:"contents"`
	Usage      []api.Amount       `json:"usage"`
	UsageFinal bool               `json:"usage_final"`
}
type pendingContent struct {
	GeneratedContent
	ContentID string          `json:"content_id"`
	Ref       *api.ContentRef `json:"ref,omitempty"`
}
type decision struct {
	Revision          uint64             `json:"revision"`
	Record            api.DecisionRecord `json:"record"`
	Input             DecideInput        `json:"input"`
	Principal         runtime.Auth       `json:"principal"`
	CommandID         string             `json:"command_id"`
	InputDigest       string             `json:"input_digest"`
	Phase             string             `json:"phase"`
	Snapshot          *api.Snapshot      `json:"snapshot,omitempty"`
	Encoding          *Encoding          `json:"encoding,omitempty"`
	CallID            string             `json:"call_id"`
	Generated         *Generated         `json:"generated,omitempty"`
	Publications      []pendingContent   `json:"publications"`
	ProposalContentID string             `json:"proposal_content_id"`
	CancelRequested   bool               `json:"cancel_requested"`
}
