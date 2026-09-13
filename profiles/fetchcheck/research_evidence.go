package fetchcheck

import (
	"context"
	"lerna/adapters/fetchcontent"
	"lerna/adapters/fetchcontext"
	"lerna/adapters/taskcontent"
	"lerna/artifacts"
	"lerna/fetch"
	"lerna/tasks"
)

// Bind observations to the exact current decision, not a fresh query budget.
func meteredResearchEvidence(h *harness, port *tasks.ActionPort, run tasks.RunSnapshot) (*fetchcontent.Adapter, error) {
	return meteredResearchEvidenceAt(h, port, run, "local")
}

func meteredResearchEvidenceAt(h *harness, port *tasks.ActionPort, run tasks.RunSnapshot, recipient string) (*fetchcontent.Adapter, error) {
	content, err := taskcontent.New(h.content, port, tasks.QualificationOf(run))
	if err != nil {
		return nil, err
	}
	if recipient != "local" {
		return fetchcontent.New(content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: recipient}, h.evidenceConfig)
	}
	// The serial research host owns this cache. Independent fixture observers
	// and acquisition drivers must not prewarm task read lengths.
	if h.researchEvidence == nil {
		h.researchEvidence, err = fetchcontent.New(content, artifacts.Binding{Token: h.token, Namespace: "local", Location: "local", Recipient: "local"}, h.evidenceConfig)
		if err != nil {
			return nil, err
		}
	}
	return h.researchEvidence.WithContent(content)
}

// Compare the controlled failure artifact with the original operation facts at
// the actual read boundary, without adding another unmetered preflight read.
type researchFailures struct {
	reader   fetchcontext.FailureEvidence
	expected map[string]fetch.Outcome
}

func (r researchFailures) ReadFailure(ctx context.Context, ref string) (fetch.Outcome, error) {
	expected, ok := r.expected[ref]
	if !ok {
		return fetch.Outcome{}, fetch.Invalid
	}
	out, err := r.reader.ReadFailure(ctx, ref)
	if err != nil {
		return fetch.Outcome{}, err
	}
	if out != expected {
		return fetch.Outcome{}, fetch.Unavailable
	}
	return out, nil
}
