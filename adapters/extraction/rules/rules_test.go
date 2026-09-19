package rules_test

import (
	"context"
	localextraction "lerna/adapters/extraction/rules"
	"lerna/extraction"
	wire "lerna/gen/harness/v1"
	"testing"
)

func TestExplicitPreferenceKeepsSourceAndScope(t *testing.T) {
	input := extraction.Input{About: "alice", Materials: []extraction.Material{{
		Source:  &wire.MemorySource{Ref: &wire.ContentSource{Kind: "note", Key: "statement", Revision: 7}, Method: "authenticated-note", Fragment: ptr("paragraph:1")},
		Speaker: "alice", Text: "回答时，我偏好简洁的说明。",
	}}}
	got, err := (localextraction.Rules{}).Extract(context.Background(), input)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Candidates) != 1 {
		t.Fatalf("candidates: %+v", got)
	}
	c := got.Candidates[0]
	if c.About != "alice" || c.Kind != "preference" || c.Attribute != "format" || c.Value != "concise" || c.Conditions != "answer" || c.Confidence.GetAssessment() != "explicit" || c.Confidence.GetBasis() != "本人明确陈述" || c.Confidence.GetMethod() != "local-rules-v1" {
		t.Fatalf("candidate: %+v", c)
	}
	if len(c.Sources) != 1 || c.Sources[0].GetRef().GetRevision() != 7 || c.Sources[0].GetRef().GetKey() != "statement" || c.Sources[0].GetFragment() != "paragraph:1" {
		t.Fatalf("sources: %+v", c.Sources)
	}
	// Candidate provenance owns its data after the source reader reuses a buffer.
	input.Materials[0].Source.Ref.Revision = 8
	if c.Sources[0].Ref.Revision != 7 {
		t.Fatal("source revision changed after extraction")
	}
}
func ptr(s string) *string { return &s }

func TestSupportedExplicitPreferences(t *testing.T) {
	for _, tc := range []struct{ text, attribute, value string }{
		{"回答时，我偏好详细的说明。", "format", "detailed"},
		{"回答时，我偏好中文。", "language", "zh"},
		{"回答时，我偏好英文。", "language", "en"},
	} {
		t.Run(tc.value, func(t *testing.T) {
			got, err := (localextraction.Rules{}).Extract(context.Background(), extraction.Input{About: "alice", Materials: []extraction.Material{{Source: &wire.MemorySource{Ref: &wire.ContentSource{Kind: "note", Key: "statement", Revision: 1}, Method: "authenticated-note", Fragment: ptr("paragraph:1")}, Speaker: "alice", Text: tc.text}}})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Candidates) != 1 || got.Candidates[0].Attribute != tc.attribute || got.Candidates[0].Value != tc.value || got.Candidates[0].Conditions != "answer" || got.Candidates[0].Kind != "preference" {
				t.Fatalf("candidate: %+v", got)
			}
		})
	}
}
