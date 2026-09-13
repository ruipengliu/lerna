package fetchcheck

import (
	"context"
	"lerna/authorization"
	"lerna/brain"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
	"net/netip"
	"slices"
)

// RuntimeConfig is trusted host configuration, not model-generated arguments.
// URLs and Networks independently constrain search, pages and redirects.
// DiscloseTo authorizes processing/disclosure of this run's supplied query and
// acquired sources at these locations; it must not come from untrusted content.
type RuntimeConfig struct {
	SearchMaxResults                              uint32 // Zero retains four; host may select one through four.
	AnswerFromSearch                              bool   // Use provider summaries without requesting candidate pages.
	AnswerOutputTokens                            uint64 // Zero preserves recorded legacy 512-token runs.
	SearchTimeoutMS                               uint32 // Zero retains 1000ms; host may select up to 3000ms.
	PageMaxBytes                                  uint32 // Zero retains the 1024-byte reference limit.
	Goal, Query                                   string
	SearchEndpoint, SearchFormat, SearchRecipient string
	SearchMaxBytes                                uint32
	URLs                                          []string
	Networks                                      []netip.Prefix
	AllowLoopbackHTTP                             bool
	DiscloseTo                                    []string
	MaxQueries, NetworkLimit, MaxSteps            uint32
	ModelTokens                                   uint64
}

// SearchCredential resolves the host-owned search key; it is never persisted.
type SearchCredential func(context.Context) (string, error)

// RunResearch starts one governed research task in a fresh private directory.
// The caller owns the directory and model credentials. The report contains the
// actual input, model output and evidence; apply the caller's disclosure policy
// before exporting it. The action planner is the bounded protocol planner, while
// the answer model is supplied by the caller. This is not a quality benchmark.
func RunResearch(ctx context.Context, root string, cfg RuntimeConfig, model brain.Model, credentials ...SearchCredential) (ResearchRecord, error) {
	if len(credentials) > 1 || cfg.SearchFormat == "doubao" && (len(credentials) != 1 || credentials[0] == nil) {
		return ResearchRecord{}, fetch.Invalid
	}
	if cfg.SearchMaxResults > 4 || cfg.SearchTimeoutMS > 3000 {
		return ResearchRecord{}, fetch.Invalid
	}
	if model == nil || cfg.Goal == "" || cfg.Query == "" || cfg.MaxQueries < 1 || cfg.MaxQueries > 128 || cfg.NetworkLimit < 1 || cfg.NetworkLimit > 128 || cfg.MaxSteps < 1 || cfg.MaxSteps > 12 || cfg.ModelTokens < 1 || cfg.ModelTokens > 1048576 || cfg.SearchMaxBytes < 1 || cfg.SearchMaxBytes > 1<<20 || len(cfg.DiscloseTo) > 16 {
		return ResearchRecord{}, fetch.Invalid
	}
	if cfg.PageMaxBytes == 0 {
		cfg.PageMaxBytes = 1024
	}
	if cfg.PageMaxBytes > 1<<20 {
		return ResearchRecord{}, fetch.Invalid
	}
	cap := model.Capabilities()
	if err := validateResearchDisclosure(cap, cfg.ModelTokens, cfg.DiscloseTo, researchOutputLimit(cfg.AnswerOutputTokens)); err != nil {
		return ResearchRecord{}, err
	}
	cfg.URLs, cfg.Networks, cfg.DiscloseTo = slices.Clone(cfg.URLs), slices.Clone(cfg.Networks), slices.Clone(cfg.DiscloseTo)
	if cfg.SearchRecipient == "" || cfg.SearchRecipient != "local" && !slices.Contains(cfg.DiscloseTo, cfg.SearchRecipient) {
		return ResearchRecord{}, fetch.Denied
	}

	if cfg.SearchFormat != "json" && cfg.SearchFormat != "duckduckgo-html" && cfg.SearchFormat != "doubao" {
		return ResearchRecord{}, fetch.Invalid
	}
	h, err := openWithAcquisition(ctx, root, "", cfg.URLs, networkConfig{Networks: cfg.Networks, AllowLoopbackHTTP: cfg.AllowLoopbackHTTP}, cfg.PageMaxBytes)
	if err != nil {
		return ResearchRecord{}, err
	}
	defer h.close()
	h.answerFromSearch = cfg.AnswerFromSearch
	h.answerOutputTokens = cfg.AnswerOutputTokens
	h.searchFormat = cfg.SearchFormat
	if len(credentials) == 1 {
		h.searchCredential = credentials[0]
	}
	h.inputSources = []*wire.ContentSource{{Kind: "task-goal", Key: "inline", Revision: 1}}
	if err := authorizeResearchRecipients(ctx, h, cfg.DiscloseTo); err != nil {
		return ResearchRecord{}, err
	}
	if err := bindRunConfiguration(ctx, root, "research-host.json", true, runtimeManifest{1, h.token, cfg, cap}); err != nil {
		return ResearchRecord{}, err
	}
	return runPreparedResearch(ctx, h, researchRunSpec{PersistTask: true, Goal: cfg.Goal, Query: cfg.Query, SearchEndpoint: cfg.SearchEndpoint, SearchConfig: searchProviderConfig{MaxBytes: cfg.SearchMaxBytes, Recipient: cfg.SearchRecipient, TimeoutMS: cfg.SearchTimeoutMS, MaxResults: cfg.SearchMaxResults}, Queries: cfg.MaxQueries, NetworkLimit: cfg.NetworkLimit, Steps: cfg.MaxSteps, ModelTokens: cfg.ModelTokens, AnswerModel: model})
}

// Only used during new-host bootstrap. Recovery must retain current authority
// and source policy, never repeat this initial installation after revocation.
func authorizeResearchRecipients(ctx context.Context, h *harness, recipients []string) error {
	if len(recipients) == 0 {
		return nil
	}
	state, err := h.db.Load(ctx)
	if err != nil {
		return err
	}
	scope := &wire.AuthorizationScope{Resources: &wire.ResourceSelector{Selection: &wire.ResourceSelector_Exact{Exact: "root"}}, Actions: []string{"content.discover", "content.process", "content.disclose", "fetch.process", "fetch.disclose"}, Purposes: []string{"task"}, Locations: recipients, ExpiresUnix: state.State.Rules[0].Scope.ExpiresUnix}
	rules := append(state.State.Rules, &wire.PolicyRule{Id: "research-disclosure", Scope: scope})
	for _, command := range []*wire.AuthorizationCommand{
		{Change: &wire.AuthorizationCommand_ReplacePolicy{ReplacePolicy: &wire.ReplacePolicy{Rules: rules}}},
		{Change: &wire.AuthorizationCommand_IssueGrant{IssueGrant: &wire.LocalGrant{Id: "research-disclosure", Subject: "operator", Scope: scope, Mode: "continuous"}}},
	} {
		state, err = h.db.Load(ctx)
		if err != nil {
			return err
		}
		command.ExpectedRevision = state.State.Revision
		op, err := h.operation(ctx)
		if err != nil {
			return err
		}
		if _, err := h.auth.Execute(ctx, h.token, authorization.Mutation{Namespace: "local", OperationID: op, Command: command}); err != nil {
			return err
		}
	}
	policy, err := h.policyStore.Load(ctx)
	if err != nil {
		return err
	}
	for i := range policy.Rules {
		policy.Rules[i].Locations = append(policy.Rules[i].Locations, recipients...)
	}
	return h.policy.Replace(policy.Rules)
}
