// Package fetch defines the bounded acquisition seam used by controlled
// execution. A Fetcher is trusted host infrastructure, not a user endpoint;
// task admission, current authorization and evidence retention remain required.
package fetch

import (
	"context"
	"errors"
	"time"
)

var (
	Invalid       = errors.New("fetch invalid")
	Denied        = errors.New("fetch denied")
	Unavailable   = errors.New("fetch unavailable")
	TooLarge      = errors.New("fetch response too large")
	Expired       = errors.New("fetch content expired")
	Unsupported   = errors.New("fetch unsupported response")
	LimitExceeded = errors.New("fetch request limit exceeded")
	TimedOut      = errors.New("fetch timed out")
	Cancelled     = errors.New("fetch cancelled")
)

type Request struct {
	URL         string
	MaxBytes    int64
	MaxRequests int
	Timeout     time.Duration
}

// Result carries actual acquisition facts. On failure only Mode and Requests are
// returned; incomplete or unauthorized response bytes are never evidence.
// Requests counts dispatched HTTP attempts, including failed responses.
// Fixed replay always has Requests=0 and HTTPStatus=0; it is not live HTTP.
// A crash before this result returns still requires the caller's original
// durable budget reservation; zero returned usage is not a refund.
type Result struct {
	// Mode is http or fixed-replay; empty denotes legacy HTTP evidence.
	Mode                                      string
	RequestedURL, FinalURL, MediaType, SHA256 string
	FetchedAt                                 time.Time
	HTTPStatus, Requests                      int
	Body                                      []byte
	// Sources lists every dispatched URL in order, including redirects.
	Sources []string
}

type Fetcher interface {
	Fetch(context.Context, Request) (Result, error)
}

// Authority checks one current acquisition phase against host-bound identity,
// purpose and processing/disclosure locations. Success is never a durable grant.
type Authority interface {
	Check(context.Context, string, string) error
}

// Evidence retains acquisition facts through independently authorized Content.
// The caller allocates and durably binds the save operation before acquisition;
// Lookup recovers that operation without performing another network request.
type Evidence interface {
	Save(context.Context, string, Result) (string, error)
	Lookup(context.Context, string) (string, error)
	Read(context.Context, string) (Result, error)
}
