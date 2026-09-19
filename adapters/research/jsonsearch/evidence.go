package jsonsearch

import (
	"context"
	"lerna/fetch"
	"lerna/websearch"
)

type Evidence interface {
	Read(context.Context, string) (fetch.Result, error)
}

// EvidenceReader parses an originally retained search service response without
// issuing another query. The injected evidence reader enforces current access.
type EvidenceReader struct{ evidence Evidence }

func NewEvidenceReader(e Evidence) (*EvidenceReader, error) {
	if e == nil {
		return nil, fetch.Invalid
	}
	return &EvidenceReader{e}, nil
}
func (r *EvidenceReader) Read(ctx context.Context, ref string) (websearch.Result, error) {
	acquired, err := r.evidence.Read(ctx, ref)
	if err != nil {
		return websearch.Result{}, err
	}
	return decodeResponse(acquired, 16, 1048576)
}
