// Package websearch defines source discovery, distinct from capability catalog
// lookup and acquired page evidence. Host execution must authorize the query,
// reserve the original task budget and govern retention before using a Searcher.
package websearch

import (
	"context"
	"lerna/fetch"
	"time"
)

type Request struct {
	Query       string
	MaxResults  int
	MaxBytes    int64
	MaxRequests int
	Timeout     time.Duration
}
type Candidate struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Snippet string `json:"snippet"`
	Summary string `json:"summary,omitempty"`
}
type Result struct {
	Candidates []Candidate
	// Acquisition describes the search response, NOT any candidate page.
	// Its actual bytes and timestamp require independent retention authorization.
	// Failures release only Mode and Requests, never partial candidates or body.
	Acquisition fetch.Result
}
type Searcher interface {
	Search(context.Context, Request) (Result, error)
}
