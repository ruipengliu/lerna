// Package extractioncheck provides separately reported extraction evidence.
package extractioncheck

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"google.golang.org/protobuf/proto"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
)

//go:embed testdata/quality-v1.json
var dataset []byte

type qualityCase struct {
	ID, Text, Kind, Attribute, Value, Conditions, Assessment, Basis, Reason string
	Events, Formats                                                         []string
}
type QualityCaseResult struct {
	ID               string `json:"id"`
	Expected, Actual []extraction.Candidate
	Reason           string `json:"reason"`
	Exact            bool   `json:"exact"`
}
type QualityReport struct {
	Dataset                                          string `json:"dataset"`
	SHA256                                           string `json:"sha256"`
	Passed                                           bool   `json:"passed"`
	Correct, Outputs, Missing, FalsePositives, Exact int
	PositiveCases, NegativeCases, TotalCases         int
	Precision                                        *float64            `json:"precision"`
	Results                                          []QualityCaseResult `json:"results"`
	Limitations                                      string              `json:"limitations"`
}

// Quality uses only the embedded public synthetic v1 dataset, never caller
// supplied private materials. It measures candidate quality, not permissions,
// task recovery, general language understanding or model performance.
func Quality(ctx context.Context, extractor extraction.Extractor) (QualityReport, error) {
	var cases []qualityCase
	if extractor == nil {
		return QualityReport{}, fmt.Errorf("missing extractor")
	}
	if err := json.Unmarshal(dataset, &cases); err != nil {
		return QualityReport{}, err
	}
	hash := sha256.Sum256(dataset)
	r := QualityReport{Dataset: "local-extraction-v1", SHA256: hex.EncodeToString(hash[:]), PositiveCases: 6, NegativeCases: 8, TotalCases: 14, Limitations: "Fixed public synthetic corpus only; no generalized language, model, authorization or workflow quality claim."}
	if len(cases) != r.TotalCases {
		return r, fmt.Errorf("invalid fixed dataset")
	}
	for _, tc := range cases {
		if err := ctx.Err(); err != nil {
			return r, err
		}
		input := extraction.Input{About: "alice"}
		source := func(key string) *wire.MemorySource {
			fragment := "selection"
			method := "observed-choice"
			kind := "choice"
			if len(tc.Events) == 0 {
				fragment = "paragraph:1"
				method = "authenticated-note"
				kind = "note"
			}
			return &wire.MemorySource{Ref: &wire.ContentSource{Kind: kind, Key: key, Revision: 1}, Method: method, Fragment: &fragment}
		}
		if len(tc.Events) == 0 {
			input.Materials = []extraction.Material{{Source: source(tc.ID), Speaker: "alice", Text: tc.Text}}
		} else {
			if len(tc.Events) != len(tc.Formats) {
				return r, fmt.Errorf("invalid events")
			}
			for i, event := range tc.Events {
				input.Materials = append(input.Materials, extraction.Material{Source: source(tc.ID + "/" + event), Speaker: "alice", Choice: &extraction.Choice{Event: event, Task: "report", Format: tc.Formats[i]}})
			}
		}
		// Construct the independent expectation before calling a potentially
		// mutating implementation; labels come from the predeclared dataset.
		var expected []extraction.Candidate
		if tc.Kind != "" {
			c := extraction.Candidate{About: "alice", Kind: tc.Kind, Attribute: tc.Attribute, Value: tc.Value, Conditions: tc.Conditions, Confidence: &wire.MemoryConfidence{Assessment: tc.Assessment, Basis: tc.Basis, Method: "local-rules-v1"}}
			for _, m := range input.Materials {
				c.Sources = append(c.Sources, proto.Clone(m.Source).(*wire.MemorySource))
			}
			expected = []extraction.Candidate{c}
		}
		got, err := extractor.Extract(ctx, input)
		if err != nil {
			return r, fmt.Errorf("case %s extraction failed", tc.ID)
		}
		r.Outputs += len(got.Candidates)
		correct := false
		if len(expected) == 1 {
			for _, c := range got.Candidates {
				if !correct && sameCandidate(c, expected[0]) {
					r.Correct++
					correct = true
				}
			}
			if len(got.Candidates) == 0 {
				r.Missing++
			}
		} else if len(got.Candidates) > 0 {
			r.FalsePositives++
		}
		exact := len(got.Candidates) == len(expected) && (len(expected) == 0 || correct) && got.Reason == tc.Reason
		if exact {
			r.Exact++
		}
		r.Results = append(r.Results, QualityCaseResult{ID: tc.ID, Expected: expected, Actual: got.Candidates, Reason: got.Reason, Exact: exact})
	}
	if r.Outputs > 0 {
		p := float64(r.Correct) / float64(r.Outputs)
		r.Precision = &p
	}
	r.Passed = r.Correct == 6 && r.Outputs == 6 && r.Exact == 14 && r.FalsePositives == 0
	return r, nil
}
func sameCandidate(a, b extraction.Candidate) bool {
	if a.About != b.About || a.Kind != b.Kind || a.Attribute != b.Attribute || a.Value != b.Value || a.Conditions != b.Conditions || !proto.Equal(a.Confidence, b.Confidence) || len(a.Sources) != len(b.Sources) {
		return false
	}
	for i := range a.Sources {
		if !proto.Equal(a.Sources[i], b.Sources[i]) {
			return false
		}
	}
	return true
}
