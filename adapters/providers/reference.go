package providers

import (
	"context"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/runtime"
)

const ReferenceProfile = "reference-json-string-facts/1"

type ReferenceEvaluator struct{ cfg ReferenceConfig }

func NewReferenceEvaluator(c ReferenceConfig) (*ReferenceEvaluator, error) {
	if c.Store == nil || c.Reader == nil || c.Scope.DatabaseID != c.Store.ID() || !api.ValidID(c.Scope.TenantID) || !api.ValidID(c.Scope.OwnerID) {
		return nil, api.E("invalid_request", "reference_ports_or_scope_invalid")
	}
	return &ReferenceEvaluator{cfg: c}, nil
}

// ReadEvidence 从原 Attempt 读取来源，准确 body/current policy 均须成立。
// 调用者只能给原键，不能把自行拼出的 Observation 冒充可信取得记录。
func (h *HTTPInformation) ReadEvidence(ctx context.Context, s runtime.Scope, a runtime.Auth, ref InformationEvidenceRef) (InformationObservation, []byte, error) {
	if err := h.scopeAuth(s, a); err != nil {
		return InformationObservation{}, nil, err
	}
	if ref.SourceRef != h.cfg.Source.SourceRef || !api.ValidID(ref.AttemptID) {
		return InformationObservation{}, nil, api.E("forbidden", "information_evidence_binding_changed")
	}
	var record informationRecord
	_, err := h.cfg.Store.Read(ctx, s, informationNamespace, ref.AttemptID, 0, &record)
	if err != nil {
		return InformationObservation{}, nil, err
	}
	if record.PrincipalRef != a.Ref(s.OwnerID) {
		return InformationObservation{}, nil, api.E("forbidden", "information_evidence_subject_changed")
	}
	o := record.Observation
	if record.Phase == "reserved" {
		o = InformationObservation{SourceRef: ref.SourceRef, AttemptID: ref.AttemptID, Items: []SearchItem{}, Gaps: []string{}, Coverage: "unknown", MediaType: "application/octet-stream", Cache: "unspecified"}
	}
	if o.SourceRef != ref.SourceRef || o.AttemptID != ref.AttemptID {
		return InformationObservation{}, nil, api.E("invalid_state", "information_evidence_origin_missing")
	}
	if record.Phase != "published" || o.BodyRef == nil {
		if _, err = h.cfg.Content.ReadBytes(ctx, s, a, record.ArgumentsRef, InformationPurpose, h.cfg.Source.Location); err != nil {
			return InformationObservation{}, nil, err
		}
		o.Items = []SearchItem{}
		o.Cursor = nil
		o.Exhausted = false
		o.Gaps = append(o.Gaps, "acquisition_unknown")
		return o, nil, nil
	}
	b, err := h.cfg.Content.ReadBytes(ctx, s, a, *o.BodyRef, InformationPurpose, h.cfg.Source.Location)
	if err != nil {
		return InformationObservation{}, nil, err
	}
	if api.Hash(b) != o.BodyRef.Hash || uint64(len(b)) != o.BodyRef.ByteLength {
		return InformationObservation{}, nil, api.E("invalid_request", "information_evidence_bytes_changed")
	}
	if err = h.expandSearch(&record, b); err != nil {
		return InformationObservation{}, nil, err
	}
	return record.Observation, b, nil
}

type referenceMaterial struct {
	observation InformationObservation
	value       any
	observedAt  string
	current     bool
}

func (e *ReferenceEvaluator) Assess(ctx context.Context, s runtime.Scope, a runtime.Auth, q ReferenceQuestion) (ReferenceAssessment, error) {
	out := ReferenceAssessment{Profile: ReferenceProfile, Status: "insufficient", Answers: []ReferencedAnswer{}, Gaps: []string{}}
	if s != e.cfg.Scope || a.TenantID != s.TenantID || !api.ValidID(a.SubjectID) || a.CredentialGeneration == 0 {
		return out, api.E("forbidden", "reference_scope_mismatch")
	}
	if len(q.Claims) < 1 || len(q.Claims) > 16 || len(q.Evidence) > 16 || q.MaxAgeSeconds == 0 || q.MaxAgeSeconds > 7*24*3600 || len(q.ObservedAtPointer) > 512 || (q.ObservedAtPointer != "" && !strings.HasPrefix(q.ObservedAtPointer, "/")) {
		return out, api.E("invalid_request", "reference_question_limits")
	}
	keys := map[string]bool{}
	for _, claim := range q.Claims {
		if claim.Key == "" || len(claim.Key) > 64 || keys[claim.Key] || len(claim.JSONPointer) > 512 || !strings.HasPrefix(claim.JSONPointer, "/") || (claim.ExpectedValue != nil && (len(*claim.ExpectedValue) > 4096 || !utf8.ValidString(*claim.ExpectedValue))) {
			return out, api.E("invalid_request", "reference_claim_invalid")
		}
		keys[claim.Key] = true
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	var now time.Time
	status, err := e.cfg.Store.Within(ctx, s, []string{"providers"}, func(tx runtime.Tx) error { var err error; now, err = tx.Now(ctx); return err })
	if status == runtime.CommitUnknown {
		return out, runtime.ErrCommitUnknown
	}
	if err != nil {
		return out, err
	}
	materials := []referenceMaterial{}
	seen := map[InformationEvidenceRef]bool{}
	var bytes uint64
	failed, stale, insufficient, conflict := false, false, false, false
	gap := func(g string) {
		for _, existing := range out.Gaps {
			if existing == g {
				return
			}
		}
		out.Gaps = append(out.Gaps, g)
	}
	for _, ref := range q.Evidence {
		if seen[ref] {
			return out, api.E("invalid_request", "reference_evidence_duplicate")
		}
		seen[ref] = true
		o, b, err := e.cfg.Reader.ReadEvidence(ctx, s, a, ref)
		if err != nil {
			if api.IsCode(err, "not_found") {
				failed = true
				gap("original_evidence_missing")
				continue
			}
			return out, err
		}
		if o.SourceRef != ref.SourceRef || o.AttemptID != ref.AttemptID {
			return out, api.E("invalid_request", "reference_evidence_origin_changed")
		}
		if o.BodyRef == nil || o.PartialRead || o.Truncated || o.HTTPStatus < 200 || o.HTTPStatus >= 300 || len(o.Gaps) > 0 {
			failed = true
			gap("retrieval_failed")
			for _, g := range o.Gaps {
				if len(g) > 128 {
					return out, api.E("invalid_request", "reference_gap_invalid")
				}
				gap(g)
			}
			continue
		}
		if api.Hash(b) != o.BodyRef.Hash || uint64(len(b)) != o.BodyRef.ByteLength || o.BodyRef.TenantID != s.TenantID || o.BodyRef.OwnerID != s.OwnerID {
			return out, api.E("invalid_request", "reference_original_bytes_changed")
		}
		if uint64(len(b)) > 2<<20-bytes {
			insufficient = true
			gap("evidence_bytes_limit")
			break
		}
		bytes += uint64(len(b))
		value, err := api.ParseJSONLimit(b, 1<<20)
		if err != nil {
			failed = true
			gap("source_json_invalid")
			continue
		}
		observed := o.ObservedAt
		if q.ObservedAtPointer != "" {
			v, ok := referencePointer(value, q.ObservedAtPointer)
			if !ok {
				stale = true
				gap("source_time_missing")
				continue
			}
			observed, ok = v.(string)
			if !ok {
				stale = true
				gap("source_time_invalid")
				continue
			}
		}
		obtainedAt, e1 := api.ParseTime(o.ObtainedAt)
		observedAt, e2 := api.ParseTime(observed)
		age := time.Duration(q.MaxAgeSeconds) * time.Second
		current := e1 == nil && e2 == nil && !observedAt.After(now) && !obtainedAt.After(now) && now.Sub(observedAt) <= age && now.Sub(obtainedAt) <= age
		if !current {
			stale = true
			gap("source_stale_or_time_invalid")
		}
		materials = append(materials, referenceMaterial{observation: o, value: value, observedAt: observed, current: current})
	}
	if len(q.Evidence) == 0 {
		gap("evidence_missing")
	}
	for _, claim := range q.Claims {
		values := map[string]bool{}
		citations := []ReferenceCitation{}
		for _, material := range materials {
			if !material.current {
				continue
			}
			value, ok := referencePointer(material.value, claim.JSONPointer)
			if !ok {
				continue
			}
			literal, ok := value.(string)
			if !ok || len(literal) > 4096 {
				continue
			}
			values[literal] = true
			if claim.ExpectedValue == nil || literal == *claim.ExpectedValue {
				o := material.observation
				citations = append(citations, ReferenceCitation{SourceRef: o.SourceRef, BodyRef: *o.BodyRef, URL: o.URL, ObtainedAt: o.ObtainedAt, ObservedAt: material.observedAt, JSONPointer: claim.JSONPointer, Quote: literal})
			}
		}
		if len(values) > 1 {
			conflict = true
			gap("source_conflict")
			continue
		}
		if len(citations) == 0 {
			insufficient = true
			gap("claim_not_supported")
			continue
		}
		out.Answers = append(out.Answers, ReferencedAnswer{Key: claim.Key, Value: citations[0].Quote, Citations: citations})
		if len(api.Raw(out)) > 128<<10 {
			out.Answers = []ReferencedAnswer{}
			insufficient = true
			gap("citation_bytes_limit")
			break
		}
	}
	switch {
	case conflict:
		out.Status = "conflict"
	case len(out.Answers) == len(q.Claims) && !insufficient:
		out.Status = "supported"
	case failed:
		out.Status = "retrieval_failed"
	case stale:
		out.Status = "stale"
	default:
		out.Status = "insufficient"
	}
	if out.Status != "supported" {
		out.Answers = []ReferencedAnswer{}
	}
	return out, nil
}

func referencePointer(value any, pointer string) (any, bool) {
	if pointer == "" {
		return value, true
	}
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	for _, part := range strings.Split(pointer[1:], "/") {
		for i := 0; i < len(part); i++ {
			if part[i] == '~' && (i+1 == len(part) || (part[i+1] != '0' && part[i+1] != '1')) {
				return nil, false
			}
			if part[i] == '~' {
				i++
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		switch node := value.(type) {
		case map[string]any:
			var ok bool
			value, ok = node[part]
			if !ok {
				return nil, false
			}
		case []any:
			index, err := strconv.Atoi(part)
			if err != nil || index < 0 || index >= len(node) || strconv.Itoa(index) != part {
				return nil, false
			}
			value = node[index]
		default:
			return nil, false
		}
	}
	return value, true
}

var _ InformationEvidenceReader = (*HTTPInformation)(nil)
