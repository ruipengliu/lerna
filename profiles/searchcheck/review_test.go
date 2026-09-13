package searchcheck_test

import (
	"lerna/profiles/searchcheck"
	"testing"
)

func TestReviewCoverageUsesFrozenDenominator(t *testing.T) {
	raw := []byte(`{"answer":"Only an opening date."}`)
	review, err := searchcheck.NewReview("answerable", raw)
	if err != nil {
		t.Fatal(err)
	}
	review.Facts[0].Verdict = "covered"
	review.Facts[0].Reason = "Answer states the opening date with supporting acquired quotation."
	report, err := searchcheck.SummarizeReview(raw, review)
	if err != nil {
		t.Fatal(err)
	}
	if report.Coverage.Numerator != 1 || report.Coverage.Denominator != 2 || report.UnreviewedFacts != 1 || report.CoverageReviewComplete {
		t.Fatal("partial review became complete or denominator shrank")
	}
	review.Facts = review.Facts[:1]
	if _, err := searchcheck.SummarizeReview(raw, review); err == nil {
		t.Fatal("omitted required fact accepted")
	}
}

func TestReviewCannotMoveJudgmentsToAnotherAnswerOrRubric(t *testing.T) {
	raw := []byte(`{"answer":"a"}`)
	original, err := searchcheck.NewReview("answerable", raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := searchcheck.SummarizeReview([]byte(`{"answer":"b"}`), original); err == nil {
		t.Fatal("review reused for another answer")
	}
	for _, mutate := range []func(*searchcheck.CoverageReview){
		func(r *searchcheck.CoverageReview) { r.ExpectationsSHA256 = "another rubric" },
		func(r *searchcheck.CoverageReview) { r.Facts[1].ID = r.Facts[0].ID },
		func(r *searchcheck.CoverageReview) { r.Facts[0].Verdict = "covered" },
		func(r *searchcheck.CoverageReview) { r.Facts[0].Verdict = "pass" },
	} {
		r := original
		r.Facts = append([]searchcheck.FactReview(nil), original.Facts...)
		mutate(&r)
		if _, err := searchcheck.SummarizeReview(raw, r); err == nil {
			t.Fatal("invalid review accepted")
		}
	}
}

func TestReviewNoRequiredFactsRemainsNotApplicable(t *testing.T) {
	raw := []byte(`{"answer":"No cost established."}`)
	r, err := searchcheck.NewReview("insufficient", raw)
	if err != nil {
		t.Fatal(err)
	}
	report, err := searchcheck.SummarizeReview(raw, r)
	if err != nil {
		t.Fatal(err)
	}
	if report.Coverage.Numerator != 0 || report.Coverage.Denominator != 0 || report.UnreviewedFacts != 0 {
		t.Fatal("N/A was converted to a success rate")
	}
}
