package brain_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"lerna/brain"
	"testing"
)

// These literal byte offsets and expected excerpt describe the supplied source,
// not a second implementation of citation validation.
func TestEvidenceAnswerBindsClaimsToAcquiredBytes(t *testing.T) {
	const body = "The bridge opened in 1998."
	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])
	block, _ := json.Marshal(map[string]any{"Status": "acquired", "Body": body, "SHA256": hash, "FetchedAt": "2026-09-12T01:02:03Z"})
	input := brain.Input{Blocks: []brain.Block{{Ref: "source-1", Role: "external-evidence", Text: string(block)}}}
	original := `{"status":"answerable","answer":"The bridge opened in 1998.","scope":"According to the supplied history; fetched September 12, 2026.","claims":[{"text":"The bridge opened in 1998.","citations":[{"source":"source-1","start":21,"end":25,"quote":"1998","sha256":"` + hash + `","fetched_at":"2026-09-12T01:02:03Z"}]}],"gaps":[]}`
	if err := brain.ValidateEvidenceAnswer([]byte(original), input, 8192); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"source", "start", "end", "quote", "sha256", "fetched_at"} {
		t.Run(field, func(t *testing.T) {
			var answer map[string]any
			json.Unmarshal([]byte(original), &answer)
			citation := answer["claims"].([]any)[0].(map[string]any)["citations"].([]any)[0].(map[string]any)
			switch field {
			case "start":
				citation[field] = 20
			case "end":
				citation[field] = 100
			default:
				citation[field] = "fabricated"
			}
			changed, _ := json.Marshal(answer)
			if brain.ValidateEvidenceAnswer(changed, input, 8192) == nil {
				t.Fatal("accepted fabricated citation")
			}
		})
	}
	input.Blocks[0].Role = "search-candidate"
	if brain.ValidateEvidenceAnswer([]byte(original), input, 8192) == nil {
		t.Fatal("search summary was treated as acquired evidence")
	}
}

func TestEvidenceAnswerRequiresObservedFetchGap(t *testing.T) {
	answer := []byte(`{"status":"fetch_failed","answer":"The page could not be retrieved.","scope":"No page contents were acquired.","claims":[],"gaps":[{"kind":"fetch_failed","detail":"The source was unavailable.","sources":["failure-1"]}]}`)
	input := brain.Input{Blocks: []brain.Block{{Ref: "failure-1", Role: "external-evidence-gap", Text: `{"Status":"unavailable","Mode":"http","Requests":1}`}}}
	if err := brain.ValidateEvidenceAnswer(answer, input, 8192); err != nil {
		t.Fatal(err)
	}
	for _, text := range []string{`{"Status":"unavailable","Mode":"http","Requests":null}`, `{"Status":"unavailable","Mode":null,"Requests":0}`, `{"Status":"acquired","Mode":"http","Requests":1}`, `{"Status":"unavailable","Mode":"fixed-replay","Requests":1}`, `{"Status":"unavailable","Mode":"http","Requests":99}`, `{"Status":"unavailable","Status":"denied","Requests":0}`} {
		input.Blocks[0].Text = text
		if brain.ValidateEvidenceAnswer(answer, input, 8192) == nil {
			t.Fatalf("accepted invalid failure facts: %s", text)
		}
	}
}

func TestInsufficientAnswerCanIdentifySearchWithoutClaimingPageEvidence(t *testing.T) {
	input := brain.Input{Blocks: []brain.Block{{Ref: "search-1", Role: "search-candidates", Text: `{"Status":"discovered","Candidates":[]}`}}}
	answer := []byte(`{"status":"insufficient","answer":"The search returned no candidates; the opening date is not established.","scope":"Only this search was observed.","claims":[],"gaps":[{"kind":"insufficient","detail":"No page evidence was acquired.","sources":["search-1"]}]}`)
	if err := brain.ValidateEvidenceAnswer(answer, input, 8192); err != nil {
		t.Fatal(err)
	}
	input.Blocks[0].Text = `{"Status":"guessed","Candidates":[]}`
	if brain.ValidateEvidenceAnswer(answer, input, 8192) == nil {
		t.Fatal("invented search results used as a gap")
	}
}

func TestEvidenceAnswerRejectsNullCitationOffset(t *testing.T) {
	const body = "The bridge opened in 1998."
	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])
	block, _ := json.Marshal(map[string]any{"Status": "acquired", "Body": body, "SHA256": hash, "FetchedAt": "2026-09-12T01:02:03Z"})
	input := brain.Input{Blocks: []brain.Block{{Ref: "source-1", Role: "external-evidence", Text: string(block)}}}
	answer := []byte(`{"status":"answerable","answer":"The bridge opened in 1998.","scope":"The supplied history.","claims":[{"text":"The bridge opened in 1998.","citations":[{"source":"source-1","start":null,"end":26,"quote":"The bridge opened in 1998.","sha256":"` + hash + `","fetched_at":"2026-09-12T01:02:03Z"}]}],"gaps":[]}`)
	// The whole body is 26 bytes, so silently decoding null as zero would
	// otherwise turn an unspecified offset into an apparently valid citation.
	if brain.ValidateEvidenceAnswer(answer, input, 8192) == nil {
		t.Fatal("null citation offset was silently interpreted as zero")
	}
}

func TestEvidenceAnswerCannotOmitObservedFailure(t *testing.T) {
	const body = "The bridge opened in 1998."
	sum := sha256.Sum256([]byte(body))
	hash := hex.EncodeToString(sum[:])
	page, _ := json.Marshal(map[string]any{"Status": "acquired", "Body": body, "SHA256": hash, "FetchedAt": "2026-09-12T01:02:03Z"})
	input := brain.Input{Blocks: []brain.Block{{Ref: "page", Role: "external-evidence", Text: string(page)}, {Ref: "failed", Role: "external-evidence-gap", Text: `{"Status":"denied","Mode":"http","Requests":1}`}}}
	answer := brain.EvidenceAnswer{Status: "answerable", Text: "The bridge opened in 1998.", Scope: "The acquired record.", Claims: []brain.EvidenceClaim{{Text: "The bridge opened in 1998.", Citations: []brain.EvidenceCitation{{Source: "page", Start: 0, End: 26, Quote: body, SHA256: hash, FetchedAt: "2026-09-12T01:02:03Z"}}}}, Gaps: []brain.EvidenceGap{}}
	raw, _ := json.Marshal(answer)
	if brain.ValidateEvidenceAnswer(raw, input, 8192) == nil {
		t.Fatal("successful page masked an observed failed source")
	}
	answer.Status = "fetch_failed"
	answer.Gaps = []brain.EvidenceGap{{Kind: "fetch_failed", Detail: "The other source was denied.", Sources: []string{"failed"}}}
	raw, _ = json.Marshal(answer)
	if err := brain.ValidateEvidenceAnswer(raw, input, 8192); err != nil {
		t.Fatal(err)
	}
}
