package extractioncheck_test

import (
	"context"
	"encoding/json"
	"lerna/adapters/localextraction"
	"lerna/profiles/extractioncheck"
	"testing"
)

func TestFixedLocalExtractionQuality(t *testing.T) {
	report, err := extractioncheck.Quality(context.Background(), localextraction.Rules{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(raw))
	if !report.Passed || report.Correct != 6 || report.Outputs != 6 || report.Exact != 14 || report.FalsePositives != 0 || report.Missing != 0 || report.Precision == nil || *report.Precision != 1 {
		t.Fatalf("fixed v1 failed: %+v", report)
	}
}
