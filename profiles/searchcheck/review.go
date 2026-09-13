package searchcheck

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// CoverageReview records independent reviewer judgments, not model self-scoring.
// It binds exact answer bytes and the preregistered rubric. It is deliberately
// only a coverage worksheet: citation support, scope and disposition need their
// own review and cannot be inferred from a coverage numerator.
type CoverageReview struct {
	CaseID, AnswerSHA256, ExpectationsSHA256 string
	Facts                                    []FactReview
}

type FactReview struct {
	ID, Verdict, Reason string
}

// Fraction has no percentage when Denominator is zero: that means N/A, not 100%.
type Fraction struct{ Numerator, Denominator uint64 }
type CoverageSummary struct {
	Coverage               Fraction
	UnreviewedFacts        uint64
	CoverageReviewComplete bool
}

func answerDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

// NewReview leaves every required fact unreviewed. Call only after generation;
// this worksheet and the rubric must not enter the candidate's context.
func NewReview(caseID string, answer []byte) (CoverageReview, error) {
	if !json.Valid(answer) {
		return CoverageReview{}, fmt.Errorf("searchcheck: invalid answer JSON")
	}
	rubric, err := LoadExpectations()
	if err != nil {
		return CoverageReview{}, err
	}
	for _, c := range rubric.Cases {
		if c.ID != caseID {
			continue
		}
		out := CoverageReview{CaseID: caseID, AnswerSHA256: answerDigest(answer), ExpectationsSHA256: rubric.SHA256, Facts: []FactReview{}}
		for _, f := range c.RequiredFacts {
			out.Facts = append(out.Facts, FactReview{ID: f.ID, Verdict: "unreviewed"})
		}
		return out, nil
	}
	return CoverageReview{}, fmt.Errorf("searchcheck: unknown review case")
}

// SummarizeReview verifies binding and computes only the frozen coverage count.
// It cannot verify the truth or independence of the reviewer's judgments and
// intentionally provides no overall quality-pass flag.
func SummarizeReview(answer []byte, review CoverageReview) (CoverageSummary, error) {
	expected, err := NewReview(review.CaseID, answer)
	if err != nil {
		return CoverageSummary{}, err
	}
	invalid := fmt.Errorf("searchcheck: invalid or mismatched coverage review")
	if expected.AnswerSHA256 != review.AnswerSHA256 || expected.ExpectationsSHA256 != review.ExpectationsSHA256 || len(expected.Facts) != len(review.Facts) {
		return CoverageSummary{}, invalid
	}
	ids := map[string]bool{}
	for _, f := range expected.Facts {
		ids[f.ID] = true
	}
	out := CoverageSummary{Coverage: Fraction{Denominator: uint64(len(expected.Facts))}}
	for _, f := range review.Facts {
		if !ids[f.ID] {
			return CoverageSummary{}, invalid
		}
		delete(ids, f.ID)
		switch f.Verdict {
		case "unreviewed":
			out.UnreviewedFacts++
		case "covered", "not_covered":
			if strings.TrimSpace(f.Reason) == "" {
				return CoverageSummary{}, invalid
			}
			if f.Verdict == "covered" {
				out.Coverage.Numerator++
			}
		default:
			return CoverageSummary{}, invalid
		}
	}
	out.CoverageReviewComplete = out.UnreviewedFacts == 0
	return out, nil
}
