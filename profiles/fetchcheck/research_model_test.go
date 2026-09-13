package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/brain"
	"testing"
)

// A separately supplied protocol implementation intentionally returns one
// claim with two quotations, rather than the built-in fixture's two claims.
type alternativeEvidenceModel struct {
	calls    int
	location string
	unknown  bool
}

func (m *alternativeEvidenceModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "alternative-local-protocol", Location: m.location, Version: "1", Text: true, Structured: true, HardBounds: true, InputUpper: 1, ContextTokens: 4096}
}
func (m *alternativeEvidenceModel) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	m.calls++
	citations := []brain.EvidenceCitation{}
	refs := []string{}
	for _, block := range in.Input.Blocks {
		var evidence struct{ Body, SHA256, FetchedAt string }
		if block.Role != "external-evidence" || json.Unmarshal([]byte(block.Text), &evidence) != nil {
			return brain.Result{}, brain.Error("unexpected candidate input")
		}
		refs = append(refs, block.Ref)
		citations = append(citations, brain.EvidenceCitation{Source: block.Ref, Start: 0, End: len(evidence.Body), Quote: evidence.Body, SHA256: evidence.SHA256, FetchedAt: evidence.FetchedAt})
	}
	raw, _ := json.Marshal(brain.EvidenceAnswer{Status: "conflicting", Text: "The register reports 2001, while the archive reports 2003.", Scope: "These records disagree; their editions do not establish the correct year.", Claims: []brain.EvidenceClaim{{Text: "The two source records give different opening years.", Citations: citations}}, Gaps: []brain.EvidenceGap{{Kind: "conflicting", Detail: "The disagreement is unresolved.", Sources: refs}}})
	return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: !m.unknown, Input: 2, Output: 3}}, nil
}
func TestResearchAcceptsReplaceableAnswerModelWithoutFixtureCardinality(t *testing.T) {
	model := &alternativeEvidenceModel{location: "local"}
	record, err := CheckReferenceResearch(context.Background(), "conflicting", ResearchConfig{MaxQueries: 128, AnswerModel: model})
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || len(record.Answer.Claims) != 1 || len(record.Answer.Claims[0].Citations) != 2 || record.Usage.ModelRequests != 3 || record.Usage.ContentQueries == 0 || record.Usage.OutcomeQueries == 0 || record.Usage.ContentQueries >= record.Usage.ActionQueries || record.AnswerModel.Model != "alternative-local-protocol" {
		t.Fatal("reference host replaced the chosen model or imposed fixture output shape")
	}
}
func TestResearchRejectsUnsupportedModelLocationBeforeDispatch(t *testing.T) {
	model := &alternativeEvidenceModel{location: "remote"}
	if _, err := CheckFrozenResearchWithAnswerModel(context.Background(), "conflicting", model); err == nil || model.calls != 0 {
		t.Fatal("local reference assembly silently allowed remote model processing")
	}
}

func TestResearchReplacementPreservesUnknownUsage(t *testing.T) {
	model := &alternativeEvidenceModel{location: "local", unknown: true}
	record, err := CheckReferenceResearch(context.Background(), "conflicting", ResearchConfig{MaxQueries: 128, AnswerModel: model})
	if err != nil {
		t.Fatal(err)
	}
	if model.calls != 1 || record.Usage.ModelRequests != 2 || record.Usage.ReservedModelRequests != 1 || record.Usage.ReservedModelTokens != 2560 {
		t.Fatal("unknown answer usage was lost or relabelled as known")
	}
}

func TestResearchAnswerLedgerReadsShareTaskQuota(t *testing.T) {
	record, err := CheckFrozenResearch(context.Background(), "fetch_failed")
	if err != nil {
		t.Fatal(err)
	}
	// The action host observes each terminal outcome once; the driver reuses its own
	// successfully committed finite facts after a current execution guard. Content
	// still checks current authority, including search disclosure at network
	// boundaries. WatchGuard may perform additional reads while HTTP waits, so
	// elapsed time must not be encoded as an exact query count. The loopback
	// path requires at least 62 total / 54 Content observations, with eight
	// other charged observations and the unchanged original ceiling of 64.
	if record.Usage.OutcomeQueries != 2 || record.Usage.ActionQueries < 62 || record.Usage.ActionQueries > 64 || record.Usage.ContentQueries+8 != record.Usage.ActionQueries {
		t.Fatalf("answer ledger reads missing or duplicated: outcome=%d total=%d", record.Usage.OutcomeQueries, record.Usage.ActionQueries)
	}
}

func TestResearchReplayDoesNotRepeatOriginalInputValidationAtPublication(t *testing.T) {
	record, err := CheckFrozenResearchReplay(context.Background(), "fetch_failed")
	if err != nil {
		t.Fatal(err)
	}
	// Fixed replay has no timed HTTP watch observations. The original input
	// is checked by the publication port; the evidence context must not add
	// a second GET of that input at the same publication boundary.
	// Retained discovery now supplies failure scope: its six governed reads
	// increase the prior 55/47 baseline without rereading the original input.
	if record.Usage.ActionQueries != 61 || record.Usage.ContentQueries != 53 || record.Usage.OutcomeQueries != 2 {
		t.Fatalf("publication duplicated original-input observation: total=%d content=%d outcome=%d", record.Usage.ActionQueries, record.Usage.ContentQueries, record.Usage.OutcomeQueries)
	}
}

type failedEvaluationModel struct {
	alternativeEvidenceModel
	input brain.Input
}

func (m *failedEvaluationModel) Generate(_ context.Context, in brain.Request) (brain.Result, error) {
	m.input = in.Input
	m.calls++
	return brain.Result{}, brain.Error("MODEL_UNAVAILABLE")
}
func TestReferenceFailureRetainsOriginalUsage(t *testing.T) { checkReferenceFailureUsage(t, false) }
func TestReopenedReferenceFailureRetainsOriginalUsage(t *testing.T) {
	checkReferenceFailureUsage(t, true)
}
func checkReferenceFailureUsage(t *testing.T, reopen bool) {
	m := &failedEvaluationModel{alternativeEvidenceModel: alternativeEvidenceModel{location: "local"}}
	record, err := CheckReferenceResearch(context.Background(), "answerable", ResearchConfig{Reopen: reopen, MaxQueries: 128, SearchMaxBytes: 4096, AnswerModel: m})
	if err == nil || m.calls != 1 || record.UsageStatus != "snapshot" || record.SearchConfig.MaxBytes != 4096 || record.Usage.NetworkCharged != 2 || record.Usage.SearchRequests != 1 || record.Usage.PageRequests != 1 || record.Usage.ActionQueries == 0 || record.Usage.ModelRequests+record.Usage.ReservedModelRequests != 3 {
		t.Fatalf("failed evaluation lost usage: %+v err=%v calls=%d", record, err, m.calls)
	}
}

func TestFailureAnswerReceivesGovernedDiscovery(t *testing.T) {
	m := &failedEvaluationModel{alternativeEvidenceModel: alternativeEvidenceModel{location: "local"}}
	_, err := CheckReferenceResearch(context.Background(), "fetch_failed", ResearchConfig{MaxQueries: 128, AnswerModel: m})
	if err == nil || m.calls != 1 {
		t.Fatal("did not reach model")
	}
	search, gap := false, false
	for _, b := range m.input.Blocks {
		search = search || b.Role == "search-candidates"
		gap = gap || b.Role == "external-evidence-gap"
	}
	if !search || !gap {
		t.Fatal("answer lacks actual discovery or page failure observation")
	}
}
