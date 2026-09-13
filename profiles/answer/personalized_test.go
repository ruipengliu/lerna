package answer

import (
	"context"
	"lerna/brain"
	"testing"
)

func TestGovernedMemoryChangesPublishedAnswer(t *testing.T) {
	for _, tc := range []struct {
		style      string
		applicable bool
		expected   string
	}{{"concise", true, "Memory, Brain, Execution"}, {"detailed", true, "The three systems are Memory, Brain, and Execution."}, {"detailed", false, "Memory, Brain, Execution"}} {
		t.Run(tc.style+"/"+tc.expected, func(t *testing.T) {
			report, e := RunPersonalizedAnswer(context.Background(), tc.style, tc.applicable, nil)
			if e != nil {
				t.Fatal(e)
			}
			if report.State != "COMPLETED" || report.Answer != tc.expected || report.MemorySources != 1 || report.ReadAllocated != 1 {
				t.Fatalf("published answer: %+v", report)
			}
		})
	}
}

func TestMissingPreferenceIsPublishedWithMetadataLineage(t *testing.T) {
	r, err := RunPersonalizedAnswer(context.Background(), "missing", true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "COMPLETED" || r.Answer != "Memory, Brain, Execution (no recorded preference)." || r.MissingSources != 1 || r.MemorySources != 0 || r.ReadAllocated != 1 {
		t.Fatalf("missing preference answer: %+v", r)
	}
}

// Provider output ceilings must be respected by the complete publication path.
func TestPersonalizedAnswerSupports512TokenProvider(t *testing.T) {
	r, err := RunPersonalizedAnswer(context.Background(), "detailed", true, boundedAnswerModel{})
	if err != nil {
		t.Fatal(err)
	}
	if r.State != "COMPLETED" || r.MemorySources != 1 || r.ReadAllocated != 1 {
		t.Fatalf("publication: %+v", r)
	}
}

type boundedAnswerModel struct{ personalizedModel }

func (m boundedAnswerModel) Generate(ctx context.Context, req brain.Request) (brain.Result, error) {
	if req.MaxOutput > 512 {
		return brain.Result{}, brain.Error("MODEL_LIMIT_UNSUPPORTED")
	}
	return m.personalizedModel.Generate(ctx, req)
}
func (m boundedAnswerModel) Capabilities() brain.Capabilities {
	c := m.personalizedModel.Capabilities()
	c.ContextTokens = 4096
	return c
}
