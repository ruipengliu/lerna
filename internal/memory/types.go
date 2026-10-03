// Package memory 分别维护准确字节、来源控制与长期记忆；索引不是权威。
package memory

import (
	"context"
	"io"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const MaxContentBytes uint64 = 16 << 20

type ObjectLocation struct {
	Key        string `json:"key"`
	Version    string `json:"version"`
	Durability string `json:"durability"`
}

// ObjectStore 的 Write/Read/Delete 均在元数据 Tx 之外执行。
type ObjectStore interface {
	Write(context.Context, api.ContentRef, io.Reader) (ObjectLocation, error)
	Read(context.Context, ObjectLocation, api.ContentRef, uint64) ([]byte, error)
	Locate(context.Context, api.ContentRef) (ObjectLocation, error)
	Delete(context.Context, ObjectLocation) error
	Durability() string
}

type PolicyValues struct {
	Subjects           []string `json:"subjects"`
	Purposes           []string `json:"purposes"`
	Locations          []string `json:"locations"`
	RetainUntil        string   `json:"retain_until"`
	Continuous         bool     `json:"continuous"`
	IndependentDerived bool     `json:"independent_derived"`
}

type Policy struct {
	PolicyRef api.ComponentRef `json:"policy_ref"`
	Values    PolicyValues     `json:"values"`
	Revision  uint64           `json:"revision"`
	State     string           `json:"state"`
}

// Authorization 只检查显式同库的当前授权，不能在 Tx 内 RPC。返回修订参与分页失效。
type Authorization interface {
	Check(context.Context, runtime.Tx, runtime.Auth, api.ComponentRef, string, string, bool) (uint64, error)
	Visibility(context.Context, runtime.Tx, runtime.Auth) (string, error)
}

type ContentVersion struct {
	ContentRef         api.ContentRef   `json:"content_ref"`
	ObjectLocation     ObjectLocation   `json:"object_location"`
	State              string           `json:"state"`
	ControlRevision    uint64           `json:"control_revision"`
	PolicyRef          api.ComponentRef `json:"policy_ref"`
	RetentionUntil     string           `json:"retention_until"`
	PublishedAt        string           `json:"published_at,omitempty"`
	ProcessedSources   []api.ContentRef `json:"processed_sources"`
	DisclosedSources   []api.ContentRef `json:"disclosed_sources"`
	PublisherID        string           `json:"publisher_id"`
	ClosureKind        string           `json:"closure_kind,omitempty"`
	ExperienceOutcome  string           `json:"experience_outcome,omitempty"`
	ExperienceProofRef *api.ObjectRef   `json:"experience_proof_ref,omitempty"`
}

type Transfer struct {
	TransferID         string           `json:"transfer_id"`
	Revision           uint64           `json:"revision"`
	Kind               string           `json:"kind"`
	CommandRef         api.ObjectRef    `json:"command_ref"`
	ContentRef         api.ContentRef   `json:"content_ref"`
	PolicyRef          api.ComponentRef `json:"policy_ref"`
	ProcessedSources   []api.ContentRef `json:"processed_sources"`
	RetentionUntil     string           `json:"retention_until"`
	ExpiresAt          string           `json:"expires_at"`
	Phase              string           `json:"phase"`
	ObjectLocation     ObjectLocation   `json:"object_location"`
	PublisherID        string           `json:"publisher_id"`
	SourceHolder       *api.ObjectRef   `json:"source_holder,omitempty"`
	TargetHolder       api.ObjectRef    `json:"target_holder"`
	ReferenceIntentRef api.ObjectRef    `json:"reference_intent_ref"`
	MaxBytes           uint64           `json:"max_bytes"`
	Purpose            string           `json:"purpose,omitempty"`
	TargetLocation     string           `json:"target_location,omitempty"`
	CompletionCopyID   string           `json:"completion_copy_id,omitempty"`
	ExperienceOutcome  string           `json:"experience_outcome,omitempty"`
	ExperienceProofRef *api.ObjectRef   `json:"experience_proof_ref,omitempty"`
}

type CopyHolder struct {
	CopyID             string           `json:"copy_id"`
	Revision           uint64           `json:"revision"`
	ContentRef         api.ContentRef   `json:"content_ref"`
	HolderRef          api.ObjectRef    `json:"holder_ref"`
	Purpose            string           `json:"purpose"`
	Location           string           `json:"location"`
	PolicyRef          api.ComponentRef `json:"policy_ref"`
	RetainUntil        string           `json:"retain_until"`
	ReferenceIntentRef api.ObjectRef    `json:"reference_intent_ref"`
	UseState           string           `json:"use_state"`
	CleanupState       string           `json:"cleanup_state"`
	ControlRevision    uint64           `json:"control_revision"`
	EvidenceRefs       []api.ContentRef `json:"evidence_refs"`
	ResidualReason     string           `json:"residual_reason,omitempty"`
	PrincipalID        string           `json:"principal_id"`
	Kind               string           `json:"kind"`
}

type MemoryValues struct {
	Type       string               `json:"type"`
	ContentRef api.ContentRef       `json:"content_ref"`
	Sources    []api.SourceEvidence `json:"sources"`
	ScopeRef   api.ContentRef       `json:"scope_ref"`
	PolicyRef  api.ComponentRef     `json:"policy_ref"`
	ObservedAt string               `json:"observed_at"`
	ValidFrom  string               `json:"valid_from,omitempty"`
	ValidTo    string               `json:"valid_to,omitempty"`
	Confidence *float64             `json:"confidence,omitempty"`
}

type MemoryRecord struct {
	MemoryID     string       `json:"memory_id"`
	Revision     uint64       `json:"revision"`
	Values       MemoryValues `json:"values"`
	State        string       `json:"state"`
	RecordedAt   string       `json:"recorded_at"`
	CreatorID    string       `json:"creator_id"`
	CleanupState string       `json:"cleanup_state"`
	ReviewReason string       `json:"review_reason,omitempty"`
}

type MemoryChange struct {
	MemoryID  string `json:"memory_id"`
	Revision  uint64 `json:"revision"`
	ChangeSeq uint64 `json:"change_seq"`
	Kind      string `json:"kind"`
}

type ChangeHead struct {
	Revision           uint64 `json:"revision"`
	ChangeHead         uint64 `json:"change_head"`
	RegistryVersion    uint64 `json:"registry_version"`
	VisibilityRevision uint64 `json:"visibility_revision"`
}

type ExtractionLimits struct {
	MaxInputBytes uint64     `json:"max_input_bytes"`
	MaxCandidates uint64     `json:"max_candidates"`
	MaxTokens     uint64     `json:"max_tokens"`
	MaxCost       api.Amount `json:"max_cost"`
}

type Extraction struct {
	ExtractionID        string           `json:"extraction_id"`
	Revision            uint64           `json:"revision"`
	InputRefs           []api.ContentRef `json:"input_refs"`
	ExtractorRef        api.ComponentRef `json:"extractor_ref"`
	Limits              ExtractionLimits `json:"limits"`
	Deadline            string           `json:"deadline"`
	SavingMode          string           `json:"saving_mode"`
	SavingGrantRef      *api.ObjectRef   `json:"saving_grant_ref,omitempty"`
	State               string           `json:"state"`
	PrincipalID         string           `json:"principal_id"`
	CheckpointRef       *api.ContentRef  `json:"checkpoint_ref,omitempty"`
	CandidateCount      uint64           `json:"candidate_count"`
	PrincipalGeneration uint64           `json:"principal_generation"`
	FailureReason       string           `json:"failure_reason,omitempty"`
}

type ExtractionCandidate struct {
	CandidateID  string         `json:"candidate_id"`
	Revision     uint64         `json:"revision"`
	ExtractionID string         `json:"extraction_id"`
	Values       MemoryValues   `json:"values"`
	ExpiresAt    string         `json:"expires_at"`
	State        string         `json:"state"`
	MemoryRef    *api.ObjectRef `json:"memory_ref,omitempty"`
}

type MemoryQuerySpec struct {
	TextRef           api.ContentRef   `json:"text_ref"`
	TypeFilter        []string         `json:"type_filter"`
	ValidAt           string           `json:"valid_at,omitempty"`
	ScopeFilter       *api.ContentRef  `json:"scope_filter,omitempty"`
	RankingProfileRef api.ComponentRef `json:"ranking_profile_ref"`
}

type Match struct {
	MemoryRef   api.ObjectRef  `json:"memory_ref"`
	ContentRef  api.ContentRef `json:"content_ref"`
	Score       uint64         `json:"score"`
	Explanation []string       `json:"explanation"`
}

type QueryPart struct {
	ObjectID string `json:"object_id"`
	Digest   string `json:"digest"`
}

type QueryView struct {
	QueryID                   string           `json:"query_id"`
	Revision                  uint64           `json:"revision"`
	PrincipalID               string           `json:"principal_id"`
	Digest                    string           `json:"digest"`
	VisibilityToken           string           `json:"visibility_token"`
	SourceRefs                []api.ContentRef `json:"source_refs,omitempty"`
	SourceParts               []QueryPart      `json:"source_parts,omitempty"`
	ExpiresAt                 string           `json:"expires_at"`
	ChangeHead                uint64           `json:"change_head"`
	Matches                   []Match          `json:"matches,omitempty"`
	MatchParts                []QueryPart      `json:"match_parts,omitempty"`
	Partial                   bool             `json:"partial"`
	Gaps                      []string         `json:"gaps"`
	RemainingPermissionChecks uint64           `json:"remaining_permission_checks"`
}

type View struct {
	ViewID             string          `json:"view_id"`
	Revision           uint64          `json:"revision"`
	PrincipalID        string          `json:"principal_id"`
	VisibilityToken    string          `json:"visibility_token"`
	ExpiresAt          string          `json:"expires_at"`
	SnapshotHead       uint64          `json:"snapshot_head"`
	Snapshot           []api.ObjectRef `json:"snapshot"`
	AckCursor          string          `json:"ack_cursor"`
	IssuedCursor       string          `json:"issued_cursor"`
	PullCursor         string          `json:"pull_cursor"`
	IssuedPage         *ViewPage       `json:"issued_page,omitempty"`
	Partial            bool            `json:"partial"`
	Gaps               []string        `json:"gaps"`
	Purposes           []string        `json:"purposes"`
	ScopeRef           api.ContentRef  `json:"scope_ref"`
	HolderRef          api.ObjectRef   `json:"holder_ref"`
	Location           string          `json:"location"`
	IssuedFromCursor   string          `json:"issued_from_cursor"`
	IssuedAcknowledged bool            `json:"issued_acknowledged"`
	LastReceiptRef     *api.ObjectRef  `json:"last_receipt_ref,omitempty"`
}

type ViewPage struct {
	ViewID           string         `json:"view_id"`
	Records          []MemoryRecord `json:"records"`
	Changes          []MemoryChange `json:"changes"`
	Cursor           string         `json:"cursor"`
	SnapshotComplete bool           `json:"snapshot_complete"`
	ChangeHead       uint64         `json:"change_head"`
	Partial          bool           `json:"partial"`
	Gaps             []string       `json:"gaps"`
}
