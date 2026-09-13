package fetchoutput

import (
	"context"
	"lerna/answers"
	"lerna/artifacts"
	"lerna/fetch"
	wire "lerna/gen/harness/v1"
)

type cachedFailure struct {
	outcome fetch.Outcome
	sha     string
	size    uint64
}

func (r *FailureReader) remember(ref string, fact cachedFailure) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// Contexts have at most eight evidence references. Other callers may read
	// more, but this optimization must never retain an unbounded set of facts.
	if len(r.cache) >= 8 {
		return
	}
	if r.cache == nil {
		r.cache = map[string]cachedFailure{}
	}
	r.cache[ref] = fact
}
func (r *FailureReader) readCached(ctx context.Context, ref string) (fetch.Outcome, bool, error) {
	r.mu.RLock()
	fact, known := r.cache[ref]
	r.mu.RUnlock()
	if !known {
		return fetch.Outcome{}, false, nil
	}
	invalid := artifacts.Error("CONTENT_UNAVAILABLE")
	parsed, err := answers.ParseReference(ref)
	if err != nil || parsed.Namespace != r.adapter.binding.Namespace {
		return fetch.Outcome{}, true, invalid
	}
	binding := r.adapter.binding
	binding.Token = r.token
	binding.Recipient = r.capability.Location
	// READ checks process/disclose as well as discovery, before and after the
	// underlying integrity-checked storage read. GET alone is not sufficient.
	// No cached fact is returned if current authority or availability is lost.
	out, err := r.adapter.content.Call(ctx, binding, &wire.ContentRequest{Method: "READ", Ref: parsed, Purpose: r.capability.Purpose, Limit: 1})
	if err != nil {
		return fetch.Outcome{}, true, err
	}
	record := out.GetRecord()
	spec := record.GetSpec()
	if len(out.GetData()) != 1 || record.GetState() != "available" || record.GetRef() == nil || answers.Reference(record.Ref) != ref || spec.GetSha256() != fact.sha || spec.GetSize() != fact.size || spec.GetKind() != "artifact" || spec.GetResource() != r.capability.Resource || spec.GetPurpose() != r.capability.Purpose || spec.GetMediaType() != "application/json" {
		return fetch.Outcome{}, true, invalid
	}
	return fact.outcome, true, nil
}
