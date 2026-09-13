package fetchcheck

import (
	"context"
	"lerna/brain"
	"testing"
)

type wideReferenceModel struct{ alternativeEvidenceModel }

func (m *wideReferenceModel) Capabilities() brain.Capabilities {
	c := m.alternativeEvidenceModel.Capabilities()
	c.InputUpper = 224 * 1024
	c.ContextTokens = 256 * 1024
	return c
}

func TestReferenceReplayHonorsExplicitModelDisclosure(t *testing.T) {
	model := &wideReferenceModel{alternativeEvidenceModel{location: "external-provider"}}
	record, err := CheckReferenceResearch(context.Background(), "conflicting", ResearchConfig{Replay: true, MaxQueries: 128, AnswerModel: model, DiscloseTo: []string{"external-provider"}, ModelTokens: 256 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || record.AnswerModel != model.Capabilities() || record.Usage.ModelRequests != 3 || record.Usage.SearchRequests != 0 || record.Usage.PageRequests != 0 || len(record.Answer.Claims[0].Citations) != 2 {
		t.Fatalf("original model, replay or governed answer lost: %+v", record.Usage)
	}
}

func TestReferenceRejectsUnapprovedModelDisclosure(t *testing.T) {
	for _, test := range []struct {
		name      string
		locations []string
		tokens    uint64
	}{
		{"legacy", nil, 0},
		{"missing-location", nil, 256 * 1024},
		{"wrong-location", []string{"another-provider"}, 256 * 1024},
		{"insufficient-budget", []string{"external-provider"}, 32768},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := &wideReferenceModel{alternativeEvidenceModel{location: "external-provider"}}
			_, err := CheckReferenceResearch(context.Background(), "conflicting", ResearchConfig{Replay: true, MaxQueries: 128, AnswerModel: model, DiscloseTo: test.locations, ModelTokens: test.tokens})
			if err == nil || model.calls != 0 {
				t.Fatal("unapproved model dispatched")
			}
		})
	}
}

func TestReferenceDisclosurePreservesUnknownBudgetAfterReopen(t *testing.T) {
	model := &wideReferenceModel{alternativeEvidenceModel{location: "external-provider", unknown: true}}
	record, err := CheckReferenceResearch(context.Background(), "conflicting", ResearchConfig{Replay: true, Reopen: true, MaxQueries: 128, AnswerModel: model, DiscloseTo: []string{"external-provider"}, ModelTokens: 256 * 1024})
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || record.AnswerModel != model.Capabilities() || record.Usage.ModelRequests != 2 || record.Usage.ReservedModelRequests != 1 || record.Usage.ReservedModelTokens != 224*1024+512 || record.Usage.SearchRequests != 0 || record.Usage.PageRequests != 0 || record.Recovery == nil {
		t.Fatalf("reopen changed original model reservation: %+v", record)
	}
}
