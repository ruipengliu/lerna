package searchcheck_test

import (
	"encoding/json"
	"lerna/profiles/searchcheck"
	"os"
	"testing"
)

func savedSemanticCase(t *testing.T, id string) (json.RawMessage, json.RawMessage) {
	t.Helper()
	raw, err := os.ReadFile("../../docs/implementation/evidence/22-frozen-snapshots/" + id + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		Record struct{ Answer, Input json.RawMessage }
	}
	if err = json.Unmarshal(raw, &saved); err != nil {
		t.Fatal(err)
	}
	return saved.Record.Answer, saved.Record.Input
}

func TestSemanticReviewKeepsUnsupportedCitationInDenominator(t *testing.T) {
	answer, input := savedSemanticCase(t, "answerable")
	review, err := searchcheck.NewSemanticReview("answerable", answer, input)
	if err != nil {
		t.Fatal(err)
	}
	for i := range review.Checks {
		if review.Checks[i].ID == "citation:0:0" {
			review.Checks[i].Verdict = "fail"
			review.Checks[i].Reason = "The recorded bridge details do not establish the placeholder 'Fixture claim'."
		}
	}
	summary, err := searchcheck.SummarizeSemanticReview(answer, input, review)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CitationSupport.Numerator != 0 || summary.CitationSupport.Denominator != 1 || summary.ReviewComplete || len(summary.FailedChecks) != 1 {
		t.Fatalf("unsupported or unreviewed checks disappeared: %+v", summary)
	}
	review.Checks = review.Checks[1:]
	if _, err := searchcheck.SummarizeSemanticReview(answer, input, review); err == nil {
		t.Fatal("omitted judgment accepted")
	}
}

func TestSemanticReviewBindsEvidenceAndKeepsAbsentCitationsNotApplicable(t *testing.T) {
	answer, input := savedSemanticCase(t, "insufficient")
	review, err := searchcheck.NewSemanticReview("insufficient", answer, input)
	if err != nil {
		t.Fatal(err)
	}
	summary, err := searchcheck.SummarizeSemanticReview(answer, input, review)
	if err != nil {
		t.Fatal(err)
	}
	if summary.CitationSupport.Denominator != 0 || summary.CitationSupport.Numerator != 0 || summary.ReviewComplete {
		t.Fatal("no citations became a quality pass")
	}
	if _, err := searchcheck.SummarizeSemanticReview(answer, append(input, ' '), review); err == nil {
		t.Fatal("review reused for changed input bytes")
	}
	review.Checks[0].Verdict = "pass"
	if _, err := searchcheck.SummarizeSemanticReview(answer, input, review); err == nil {
		t.Fatal("judgment without explanation accepted")
	}
}

func TestSemanticReviewRejectsDuplicateJudgmentsAndFabricatedQuote(t *testing.T) {
	answer, input := savedSemanticCase(t, "conflicting")
	review, err := searchcheck.NewSemanticReview("conflicting", answer, input)
	if err != nil {
		t.Fatal(err)
	}
	review.Checks[1].ID = review.Checks[0].ID
	if _, err := searchcheck.SummarizeSemanticReview(answer, input, review); err == nil {
		t.Fatal("duplicated citation review hid an unreviewed citation")
	}
	var modified map[string]any
	if err := json.Unmarshal(answer, &modified); err != nil {
		t.Fatal(err)
	}
	claim := modified["claims"].([]any)[0].(map[string]any)
	citation := claim["citations"].([]any)[0].(map[string]any)
	citation["quote"] = "The opening year is definitely 1999."
	raw, err := json.Marshal(modified)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := searchcheck.NewSemanticReview("conflicting", raw, input); err == nil {
		t.Fatal("fabricated quotation reached semantic scoring")
	}
}
