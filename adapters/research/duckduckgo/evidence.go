package duckduckgo

import (
	"context"
	"lerna/fetch"
	"lerna/websearch"
)

type Evidence interface {
	Read(context.Context, string) (fetch.Result, error)
}
type EvidenceReader struct {
	evidence Evidence
	limit    int
}

// The host must supply MaxResults from the original admitted search request.
// Parsing retained HTML does not dispatch another search or confer page access.
func NewEvidenceReader(e Evidence, maxResults int) (*EvidenceReader, error) {
	if e == nil || maxResults < 1 || maxResults > 16 {
		return nil, fetch.Invalid
	}
	return &EvidenceReader{e, maxResults}, nil
}
func (r *EvidenceReader) Read(ctx context.Context, ref string) (websearch.Result, error) {
	acquired, err := r.evidence.Read(ctx, ref)
	if err != nil {
		return websearch.Result{}, err
	}
	return decode(ctx, acquired, r.limit, 1048576)
}
