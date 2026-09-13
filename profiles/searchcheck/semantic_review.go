package searchcheck

import (
	"encoding/json"
	"fmt"
	"lerna/brain"
	"strings"
)

// SemanticReview is a post-generation worksheet. Checks are independent human
// judgments; validation authenticates their binding, not their truth or author.
// Coverage retains the original frozen fact denominator.
type SemanticReview struct {
	Version     string
	InputSHA256 string
	Coverage    CoverageReview
	Checks      []FactReview
}

type SemanticSummary struct {
	Coverage         CoverageSummary
	CitationSupport  Fraction
	UnreviewedChecks uint64
	FailedChecks     []string
	ReviewComplete   bool
}

// NewSemanticReview binds the exact answer and acquired input bytes. Structural
// traceability is checked before review; it is not semantic support. Each cited
// claim/quotation pair is counted, including repeated sources on different claims.
func NewSemanticReview(caseID string, answer, input []byte) (SemanticReview, error) {
	coverage, err := NewReview(caseID, answer)
	if err != nil {
		return SemanticReview{}, err
	}
	var in brain.Input
	if err = json.Unmarshal(input, &in); err != nil {
		return SemanticReview{}, err
	}
	if err = brain.ValidateEvidenceAnswer(answer, in, brain.MaxAnswerBytes); err != nil {
		return SemanticReview{}, err
	}
	var a brain.EvidenceAnswer
	if err = json.Unmarshal(answer, &a); err != nil {
		return SemanticReview{}, err
	}
	out := SemanticReview{Version: "search-semantic-review-v1", InputSHA256: answerDigest(input), Coverage: coverage, Checks: []FactReview{}}
	add := func(id string) { out.Checks = append(out.Checks, FactReview{ID: id, Verdict: "unreviewed"}) }
	for i, c := range a.Claims {
		for j := range c.Citations {
			add(fmt.Sprintf("citation:%d:%d", i, j))
		}
	}
	// Answer prose can introduce unsupported conclusions beyond its claims array.
	add("answer_support")
	add("scope")
	add("disposition")
	rubric, err := LoadExpectations()
	if err != nil {
		return SemanticReview{}, err
	}
	for _, c := range rubric.Cases {
		if c.ID == caseID {
			for i := range c.Forbidden {
				add(fmt.Sprintf("forbidden:%d", i))
			}
		}
	}
	return out, nil
}

// SummarizeSemanticReview cannot infer correctness from a filled worksheet. It
// reports failures and pending checks separately and has no quality-pass flag.
// A zero citation denominator is N/A, even when every other check is completed.
func SummarizeSemanticReview(answer, input []byte, review SemanticReview) (SemanticSummary, error) {
	expected, err := NewSemanticReview(review.Coverage.CaseID, answer, input)
	if err != nil {
		return SemanticSummary{}, err
	}
	invalid := fmt.Errorf("searchcheck: invalid or mismatched semantic review")
	if review.Version != expected.Version || review.InputSHA256 != expected.InputSHA256 || len(review.Checks) != len(expected.Checks) {
		return SemanticSummary{}, invalid
	}
	coverage, err := SummarizeReview(answer, review.Coverage)
	if err != nil {
		return SemanticSummary{}, err
	}
	ids := map[string]bool{}
	out := SemanticSummary{Coverage: coverage, FailedChecks: []string{}}
	for _, c := range expected.Checks {
		ids[c.ID] = true
		if strings.HasPrefix(c.ID, "citation:") {
			out.CitationSupport.Denominator++
		}
	}
	for _, c := range review.Checks {
		if !ids[c.ID] {
			return SemanticSummary{}, invalid
		}
		delete(ids, c.ID)
		switch c.Verdict {
		case "unreviewed":
			out.UnreviewedChecks++
		case "pass", "fail":
			if strings.TrimSpace(c.Reason) == "" {
				return SemanticSummary{}, invalid
			}
			if c.Verdict == "fail" {
				out.FailedChecks = append(out.FailedChecks, c.ID)
			} else if strings.HasPrefix(c.ID, "citation:") {
				out.CitationSupport.Numerator++
			}
		default:
			return SemanticSummary{}, invalid
		}
	}
	out.ReviewComplete = coverage.CoverageReviewComplete && out.UnreviewedChecks == 0
	return out, nil
}
