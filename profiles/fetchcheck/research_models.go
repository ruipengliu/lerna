package fetchcheck

import (
	"context"
	"encoding/json"
	"lerna/brain"
	"lerna/catalog"
	"lerna/fetch"
	"strconv"
)

type fetchActionModel struct {
	calls     int
	oversized bool
	exhaust   bool
}

func (*fetchActionModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "local-fetch-action-protocol-fixture", Version: "1", Location: "local", Text: true, Structured: true, HardBounds: true, InputUpper: 8192, ContextTokens: 32768}
}
func (m *fetchActionModel) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	m.calls++
	if in.Contract != brain.ActionContract || m.calls != 1 {
		return brain.Result{}, fetch.Invalid
	}
	var digest, args string
	for _, block := range in.Input.Blocks {
		if block.Role == "capability" {
			var row struct{ Ref catalog.Ref }
			if json.Unmarshal([]byte(block.Text), &row) == nil && row.Ref.Name == "web.fetch" {
				digest = row.Ref.Digest
			}
		}
		if block.Role == "input" {
			args = block.Text
		}
	}
	if digest == "" || args == "" {
		return brain.Result{}, fetch.Invalid
	}
	if m.oversized {
		var input map[string]any
		if json.Unmarshal([]byte(args), &input) != nil {
			return brain.Result{}, fetch.Invalid
		}
		input["max_bytes"] = 2048
		raw, _ := json.Marshal(input)
		args = string(raw)
	}
	actions := []brain.ProposedAction{{Key: "fetch", Capability: digest, DependsOn: []string{}, Arguments: args, ResourceVersion: 1}}
	if m.exhaust {
		for _, key := range []string{"second", "third"} {
			x := actions[0]
			x.Key = key
			actions = append(actions, x)
		}
	}
	raw, _ := json.Marshal(brain.ActionOutput{Kind: "actions", Actions: actions})
	return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 40}}, nil
}

type decisionFixtureModel struct {
	beforeReturn func() error
	evidence     bool
	// Selects a preregistered protocol response, not a semantic classifier.
	missingCost bool
}

func (decisionFixtureModel) Capabilities() brain.Capabilities {
	return brain.Capabilities{Model: "local-protocol-fixture", Location: "local", Version: "1", Text: true, Structured: true, HardBounds: true, InputUpper: 1, ContextTokens: 4096}
}
func (m decisionFixtureModel) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	if m.beforeReturn != nil {
		if err := m.beforeReturn(); err != nil {
			return brain.Result{}, err
		}
	}
	source := in.Input.Blocks[0].Ref
	raw, _ := json.Marshal(brain.Answer{Text: "Fixture answer", Sources: []string{source}})
	if m.evidence {
		if in.Contract != brain.EvidenceAnswerContract {
			return brain.Result{}, brain.Error("wrong evidence contract")
		}
		failureRefs := []string{}
		for _, block := range in.Input.Blocks {
			if block.Role == "external-evidence-gap" {
				failureRefs = append(failureRefs, block.Ref)
			}
		}
		if len(failureRefs) > 0 {
			claims := []brain.EvidenceClaim{}
			for _, block := range in.Input.Blocks {
				if block.Role != "external-evidence" {
					continue
				}
				var acquired struct{ Body, SHA256, FetchedAt string }
				if json.Unmarshal([]byte(block.Text), &acquired) != nil {
					return brain.Result{}, fetch.Invalid
				}
				claims = append(claims, brain.EvidenceClaim{Text: acquired.Body, Citations: []brain.EvidenceCitation{{Source: block.Ref, Start: 0, End: len(acquired.Body), Quote: acquired.Body, SHA256: acquired.SHA256, FetchedAt: acquired.FetchedAt}}})
			}
			text, scope := "The page could not be acquired; its contents remain unverified.", "This acquisition attempt only."
			if len(claims) > 0 || len(failureRefs) > 1 {
				text = "Some pages could not be acquired; only the successfully acquired material is included."
				scope = "Partial acquisition only; unavailable pages remain unverified."
			}
			raw, _ = json.Marshal(brain.EvidenceAnswer{Status: "fetch_failed", Text: text, Scope: scope, Claims: claims, Gaps: []brain.EvidenceGap{{Kind: "fetch_failed", Detail: "Observed acquisition failure.", Sources: failureRefs}}})
			return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 1, Output: 1}}, nil
		}
		if len(in.Input.Blocks) == 2 && in.Input.Blocks[0].Role == "external-evidence" && in.Input.Blocks[1].Role == "external-evidence" {
			claims := []brain.EvidenceClaim{}
			sources := []string{}
			for _, block := range in.Input.Blocks {
				var acquired struct{ Body, SHA256, FetchedAt string }
				if json.Unmarshal([]byte(block.Text), &acquired) != nil {
					return brain.Result{}, fetch.Invalid
				}
				sources = append(sources, block.Ref)
				claims = append(claims, brain.EvidenceClaim{Text: acquired.Body, Citations: []brain.EvidenceCitation{{Source: block.Ref, Start: 0, End: len(acquired.Body), Quote: acquired.Body, SHA256: acquired.SHA256, FetchedAt: acquired.FetchedAt}}})
			}
			raw, _ = json.Marshal(brain.EvidenceAnswer{Status: "conflicting", Text: "The two records give conflicting opening years; neither is established as authoritative.", Scope: "These two acquired records only; no basis to resolve their disagreement.", Claims: claims, Gaps: []brain.EvidenceGap{{Kind: "conflicting", Detail: "The records disagree.", Sources: sources}}})
			return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 1, Output: 1}}, nil
		}
		if in.Input.Blocks[0].Role == "search-candidates" {
			var discovery struct{ Candidates []any }
			if json.Unmarshal([]byte(in.Input.Blocks[0].Text), &discovery) != nil || discovery.Candidates == nil || len(discovery.Candidates) != 0 {
				return brain.Result{}, fetch.Invalid
			}
			raw, _ = json.Marshal(brain.EvidenceAnswer{Status: "insufficient", Text: "This search returned no candidates; the requested fact remains unverified.", Scope: "Only this search result, not a claim that the fact does not exist.", Claims: []brain.EvidenceClaim{}, Gaps: []brain.EvidenceGap{{Kind: "insufficient", Detail: "No candidate pages were discovered.", Sources: []string{source}}}})
			return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 1, Output: 1}}, nil
		}
		var acquired struct{ Body, SHA256, FetchedAt string }
		if err := json.Unmarshal([]byte(in.Input.Blocks[0].Text), &acquired); err != nil {
			return brain.Result{}, err
		}
		if m.missingCost {
			if acquired.Body != "Eastmere bridge record, edition 2026-01. The Eastmere footbridge crosses the canal. It opened in 2005. This record does not state its construction cost." {
				return brain.Result{}, fetch.Invalid
			}
			raw, _ = json.Marshal(brain.EvidenceAnswer{Status: "insufficient", Text: "The retrieved record does not establish the construction cost.", Scope: "The retrieved Eastmere record only; this does not establish that no cost exists.", Claims: []brain.EvidenceClaim{}, Gaps: []brain.EvidenceGap{{Kind: "insufficient", Detail: "The retrieved record omits the construction cost.", Sources: []string{source}}}})
			return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 1, Output: 1}}, nil
		}
		raw, _ = json.Marshal(brain.EvidenceAnswer{Status: "answerable", Text: "Fixture evidence answer", Scope: "Only the acquired source at its recorded time", Claims: []brain.EvidenceClaim{{Text: "Fixture claim", Citations: []brain.EvidenceCitation{{Source: source, Start: 0, End: len(acquired.Body), Quote: acquired.Body, SHA256: acquired.SHA256, FetchedAt: acquired.FetchedAt}}}}, Gaps: []brain.EvidenceGap{}})
	}
	return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 1, Output: 1}}, nil
}

// Local protocol fixture: chooses a page only from the supplied actual search
// result. It does not read test expectations and is not a model-quality judge.
type researchActionModel struct {
	calls         int
	searchBytes   uint32
	searchResults uint32
	searchTimeout uint32
	pageBytes     uint32
}

func (*researchActionModel) Capabilities() brain.Capabilities {
	return (&fetchActionModel{}).Capabilities()
}
func (m *researchActionModel) Generate(ctx context.Context, in brain.Request) (brain.Result, error) {
	m.calls++
	if in.Contract != brain.ActionContract || m.calls > 2 {
		return brain.Result{}, fetch.Invalid
	}
	name := "web.search"
	arguments := map[string]any{"query": "history", "max_results": 4, "max_bytes": 1024, "max_requests": 1, "timeout_ms": 1000}
	if m.searchResults != 0 {
		arguments["max_results"] = m.searchResults
	}
	if m.searchTimeout != 0 {
		arguments["timeout_ms"] = m.searchTimeout
	}
	if m.searchBytes != 0 {
		arguments["max_bytes"] = m.searchBytes
	}
	digests := map[string]string{}
	targets := []string{}
	for _, block := range in.Input.Blocks {
		if block.Role == "input" && name == "web.search" {
			var submitted struct{ Query string }
			if json.Unmarshal([]byte(block.Text), &submitted) != nil || submitted.Query == "" {
				return brain.Result{}, fetch.Invalid
			}
			arguments["query"] = submitted.Query
		}
		if block.Role == "capability" {
			var entry struct{ Ref catalog.Ref }
			if json.Unmarshal([]byte(block.Text), &entry) != nil {
				return brain.Result{}, fetch.Invalid
			}
			digests[entry.Ref.Name] = entry.Ref.Digest
		}
		if block.Role == "search-candidates" {
			var discovered struct{ Candidates []struct{ URL string } }
			if json.Unmarshal([]byte(block.Text), &discovered) != nil || len(discovered.Candidates) == 0 {
				return brain.Result{}, fetch.Invalid
			}
			name = "web.fetch"
			for _, candidate := range discovered.Candidates {
				targets = append(targets, candidate.URL)
			}
			pageBytes := m.pageBytes
			if pageBytes == 0 {
				pageBytes = 1024
			}
			arguments = map[string]any{"url": discovered.Candidates[0].URL, "max_bytes": pageBytes, "max_requests": 1, "timeout_ms": 1000}
		}
	}
	if digests[name] == "" {
		return brain.Result{}, fetch.Invalid
	}
	args, _ := json.Marshal(arguments)
	actions := []brain.ProposedAction{{Key: name, Capability: digests[name], DependsOn: []string{}, Arguments: string(args), ResourceVersion: 1}}
	if len(targets) > 1 {
		actions = nil
		for i, target := range targets {
			arguments["url"] = target
			raw, _ := json.Marshal(arguments)
			actions = append(actions, brain.ProposedAction{Key: name + "-" + strconv.Itoa(i), Capability: digests[name], DependsOn: []string{}, Arguments: string(raw), ResourceVersion: 1})
		}
	}
	raw, _ := json.Marshal(brain.ActionOutput{Kind: "actions", Actions: actions})
	return brain.Result{Content: raw, Finish: "stop", Usage: brain.Usage{Known: true, Input: 100, Output: 40}}, nil
}

// ResearchRecord is a snapshot of this profile's public fictional material.
// It is not a generic Content export API or a semantic quality verdict.
// The model boundary supplies the input and original output, not expectations.
// ResearchUsage reports recorded Core queries, not every internal Content read.
// NetworkCharged includes reservations for failures, rather than refunded usage.
// ReservedModel fields retain unsettled requests and their token upper bounds.
type ResearchUsage struct {
	ExecutionWaitMillis int64
	QueryLimit          uint32
	// OutcomeQueries is another subset of ActionQueries.
	OutcomeQueries uint64
	// ContentQueries is a subset of ActionQueries, not an additional budget.
	ContentQueries                                            uint64
	ReservedModelRequests, ReservedModelTokens                uint64
	ModelRequests, ModelTokens, ActionQueries, NetworkCharged uint64
	SearchRequests, PageRequests                              uint64
}

type ResearchRecovery struct {
	OriginalWorkerGeneration, ResumedWorkerGeneration uint64
	ActionModelCallsAfterReopen                       uint64
}

type ResearchRecord struct {
	UsageStatus  string `json:",omitempty"` // Failure snapshot or unavailable; empty preserves legacy success reports.
	SearchConfig searchProviderConfig
	AnswerModel  brain.Capabilities
	Recovery     *ResearchRecovery `json:",omitempty"`
	Usage        ResearchUsage
	Input        brain.Input
	ModelOutput  json.RawMessage
	Answer       brain.EvidenceAnswer
}

type recordingResearchModel struct {
	capabilities brain.Capabilities
	model        brain.Model
	input        brain.Input
	output       json.RawMessage
}

func (m *recordingResearchModel) Capabilities() brain.Capabilities {
	m.capabilities = m.model.Capabilities()
	return m.capabilities
}
func (m *recordingResearchModel) Generate(ctx context.Context, request brain.Request) (brain.Result, error) {
	raw, err := json.Marshal(request.Input)
	if err != nil {
		return brain.Result{}, err
	}
	if err = json.Unmarshal(raw, &m.input); err != nil {
		return brain.Result{}, err
	}
	result, err := m.model.Generate(ctx, request)
	if err != nil {
		return brain.Result{}, err
	}
	m.output = append(json.RawMessage(nil), result.Content...)
	return result, nil
}
