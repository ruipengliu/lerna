package providers

import (
	"context"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

// ReferenceQuestion 的 profile 明确限定 JSON 字符串事实，非通用自然语言裁决。
type ReferenceQuestion struct {
	Claims            []ReferenceClaim         `json:"claims"`
	Evidence          []InformationEvidenceRef `json:"evidence"`
	MaxAgeSeconds     uint64                   `json:"max_age_seconds"`
	ObservedAtPointer string                   `json:"observed_at_pointer"`
}
type ReferenceClaim struct {
	Key           string  `json:"key"`
	JSONPointer   string  `json:"json_pointer"`
	ExpectedValue *string `json:"expected_value,omitempty"`
}
type InformationEvidenceRef struct {
	SourceRef api.ComponentRef `json:"source_ref"`
	AttemptID string           `json:"attempt_id"`
}
type ReferenceCitation struct {
	SourceRef   api.ComponentRef `json:"source_ref"`
	BodyRef     api.ContentRef   `json:"body_ref"`
	URL         string           `json:"url"`
	ObtainedAt  string           `json:"obtained_at"`
	ObservedAt  string           `json:"observed_at"`
	JSONPointer string           `json:"json_pointer"`
	Quote       string           `json:"quote"`
}
type ReferencedAnswer struct {
	Key       string              `json:"key"`
	Value     string              `json:"value"`
	Citations []ReferenceCitation `json:"citations"`
}
type ReferenceAssessment struct {
	Profile string             `json:"profile"`
	Status  string             `json:"status"`
	Answers []ReferencedAnswer `json:"answers"`
	Gaps    []string           `json:"gaps"`
}

// EvidenceReader 必须来自宿主登记的可信原行动账，不能接受调用者自报 Observation。
type InformationEvidenceReader interface {
	ReadEvidence(context.Context, runtime.Scope, runtime.Auth, InformationEvidenceRef) (InformationObservation, []byte, error)
}
type ReferenceConfig struct {
	Store  runtime.Store
	Scope  runtime.Scope
	Reader InformationEvidenceReader
}
