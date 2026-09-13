package brain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lerna/fetch"
	"lerna/internal/jsonvalue"
	"strings"
	"time"
	"unicode/utf8"
)

const EvidenceAnswerContract = "harness_evidence_answer_v1"

// EvidenceAnswer is a distinct answer contract. Traceable quotations are not
// proof that the cited text semantically supports a claim; evaluation is separate.
type EvidenceAnswer struct {
	Status string          `json:"status"`
	Text   string          `json:"answer"`
	Scope  string          `json:"scope"`
	Claims []EvidenceClaim `json:"claims"`
	Gaps   []EvidenceGap   `json:"gaps"`
}
type EvidenceClaim struct {
	Text      string             `json:"text"`
	Citations []EvidenceCitation `json:"citations"`
}
type EvidenceCitation struct {
	Source    string `json:"source"`
	Start     int    `json:"start"`
	End       int    `json:"end"`
	Quote     string `json:"quote"`
	SHA256    string `json:"sha256"`
	FetchedAt string `json:"fetched_at"`
}

// Offsets are UTF-8 byte offsets into the complete acquired Body, end exclusive.
// A gap refers to supplied evidence, never an invented source or failed request.
type EvidenceGap struct {
	Kind    string   `json:"kind"`
	Detail  string   `json:"detail"`
	Sources []string `json:"sources"`
}

func exactEvidenceKeys(v any, keys ...string) bool {
	m, ok := v.(map[string]any)
	if !ok || len(m) != len(keys) {
		return false
	}
	for _, k := range keys {
		// Every field in these evidence objects is required and non-null.
		// encoding/json otherwise silently maps null integers to zero and
		// null strings to empty strings, inventing omitted acquisition facts.
		if value, ok := m[k]; !ok || value == nil {
			return false
		}
	}
	return true
}
func evidenceShape(v any) bool {
	if !exactEvidenceKeys(v, "status", "answer", "scope", "claims", "gaps") {
		return false
	}
	m := v.(map[string]any)
	claims, ok := m["claims"].([]any)
	if !ok {
		return false
	}
	gaps, ok := m["gaps"].([]any)
	if !ok {
		return false
	}
	for _, claim := range claims {
		if !exactEvidenceKeys(claim, "text", "citations") {
			return false
		}
		citations, ok := claim.(map[string]any)["citations"].([]any)
		if !ok {
			return false
		}
		for _, c := range citations {
			if !exactEvidenceKeys(c, "source", "start", "end", "quote", "sha256", "fetched_at") {
				return false
			}
		}
	}
	for _, g := range gaps {
		if !exactEvidenceKeys(g, "kind", "detail", "sources") {
			return false
		}
	}
	return true
}

// ValidateEvidenceAnswer checks the complete contract and acquired-byte lineage.
// The caller must supply currently authorized context; this pure check grants no
// access and cannot replace current Content authorization before publication.
func ValidateEvidenceAnswer(data []byte, in Input, limit int) error {
	_, err := parseEvidenceAnswer(data, in, limit)
	return err
}
func parseEvidenceAnswer(data []byte, in Input, limit int) (EvidenceAnswer, error) {
	invalid := Error("OUTPUT_INVALID")
	var out EvidenceAnswer
	if limit < 1 || limit > MaxAnswerBytes || len(data) > limit || !utf8.Valid(data) {
		return out, invalid
	}
	value, err := jsonvalue.Decode(data)
	if err != nil || !evidenceShape(value) || json.Unmarshal(data, &out) != nil {
		return EvidenceAnswer{}, invalid
	}
	if strings.TrimSpace(out.Text) == "" || strings.TrimSpace(out.Scope) == "" || len(out.Claims) > 16 || len(out.Gaps) > 16 {
		return EvidenceAnswer{}, invalid
	}
	switch out.Status {
	case "answerable", "insufficient", "conflicting", "fetch_failed":
	default:
		return EvidenceAnswer{}, invalid
	}
	blocks := map[string]Block{}
	for _, b := range in.Blocks {
		if b.Ref == "" {
			return EvidenceAnswer{}, invalid
		}
		if _, exists := blocks[b.Ref]; exists {
			return EvidenceAnswer{}, invalid
		}
		blocks[b.Ref] = b
	}
	for _, claim := range out.Claims {
		if strings.TrimSpace(claim.Text) == "" || len(claim.Citations) < 1 || len(claim.Citations) > 8 {
			return EvidenceAnswer{}, invalid
		}
		for _, c := range claim.Citations {
			b, ok := blocks[c.Source]
			if !ok || b.Role != "external-evidence" && b.Role != "external-search-evidence" {
				return EvidenceAnswer{}, invalid
			}
			raw, e := jsonvalue.Decode([]byte(b.Text))
			if e != nil {
				return EvidenceAnswer{}, invalid
			}
			m, ok := raw.(map[string]any)
			if !ok {
				return EvidenceAnswer{}, invalid
			}
			body, bodyOK := m["Body"].(string)
			hash, hashOK := m["SHA256"].(string)
			acquired, timeOK := m["FetchedAt"].(string)
			when, e := time.Parse(time.RFC3339Nano, acquired)
			sum := sha256.Sum256([]byte(body))
			if m["Status"] != "acquired" || !bodyOK || !hashOK || !timeOK || e != nil || when.IsZero() || hash != hex.EncodeToString(sum[:]) || c.SHA256 != hash || c.FetchedAt != acquired || c.Start < 0 || c.End <= c.Start || c.End > len(body) {
				return EvidenceAnswer{}, invalid
			}
			if c.Quote != body[c.Start:c.End] || !utf8.ValidString(body[:c.Start]) || !utf8.ValidString(body[c.End:]) {
				return EvidenceAnswer{}, invalid
			}
		}
	}
	matched := false
	accountedFailures := map[string]bool{}
	for _, g := range out.Gaps {
		if strings.TrimSpace(g.Detail) == "" || g.Sources == nil || len(g.Sources) > 8 {
			return EvidenceAnswer{}, invalid
		}
		switch g.Kind {
		case "insufficient", "conflicting", "fetch_failed":
		default:
			return EvidenceAnswer{}, invalid
		}
		if g.Kind == out.Status {
			matched = true
		}
		seen := map[string]bool{}
		for _, ref := range g.Sources {
			b, ok := blocks[ref]
			if !ok || seen[ref] {
				return EvidenceAnswer{}, invalid
			}
			seen[ref] = true
			if g.Kind == "fetch_failed" {
				if b.Role != "external-evidence-gap" || !observedFetchGap(b.Text) {
					return EvidenceAnswer{}, invalid
				}
				accountedFailures[ref] = true
			} else if b.Role != "external-evidence" && b.Role != "external-search-evidence" {
				if g.Kind != "insufficient" || b.Role != "search-candidates" || !observedDiscovery(b.Text) {
					return EvidenceAnswer{}, invalid
				}
			}
		}
		if g.Kind == "conflicting" && len(seen) < 2 || g.Kind == "fetch_failed" && len(seen) < 1 {
			return EvidenceAnswer{}, invalid
		}
	}
	// Known acquisition failures are part of the host-selected context, not
	// optional citations. Validate them before saving or recovering an answer.
	for ref, block := range blocks {
		if block.Role == "external-evidence-gap" && !accountedFailures[ref] {
			return EvidenceAnswer{}, invalid
		}
	}
	if out.Status == "answerable" {
		if len(out.Claims) == 0 || len(out.Gaps) != 0 {
			return EvidenceAnswer{}, invalid
		}
	} else if !matched {
		return EvidenceAnswer{}, invalid
	}
	return out, nil
}

func observedFetchGap(text string) bool {
	value, err := jsonvalue.Decode([]byte(text))
	if err != nil || !exactEvidenceKeys(value, "Status", "Mode", "Requests") {
		return false
	}
	var facts struct {
		Status, Mode string
		Requests     uint32
	}
	if json.Unmarshal([]byte(text), &facts) != nil || !fetch.IsFailureStatus(facts.Status) || facts.Requests > 5 {
		return false
	}
	return facts.Mode == "http" || facts.Mode == "" || facts.Mode == "fixed-replay" && facts.Requests == 0
}

func observedDiscovery(text string) bool {
	value, err := jsonvalue.Decode([]byte(text))
	if err != nil {
		return false
	}
	fields, ok := value.(map[string]any)
	if !ok || fields["Status"] != "discovered" {
		return false
	}
	candidates, ok := fields["Candidates"].([]any)
	return ok && len(candidates) <= 16
}
