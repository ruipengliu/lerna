// Package replayfetch reads immutable, host-configured text fixtures. It has no
// HTTP client or fallback. Mode=fixed-replay cannot be used as live-source proof.
package replayfetch

import (
	"context"
	"crypto/sha256"
	"fmt"
	"lerna/fetch"
	"time"
	"unicode/utf8"
)

type Clock interface{ Now() (time.Time, error) }
type Entry struct {
	URL, SHA256 string
	Body        []byte
	// Empty preserves text/plain compatibility; JSON/HTML support search snapshots.
	MediaType string
	// Denied replays a host-declared denial, not an observed HTTP 403. It cannot
	// carry page bytes, a digest, or a media type.
	Denied bool
}
type Adapter struct {
	authority fetch.Authority
	clock     Clock
	entries   map[string]Entry
}

var _ fetch.Fetcher = (*Adapter)(nil)

func New(a fetch.Authority, c Clock, entries []Entry) (*Adapter, error) {
	if a == nil || c == nil || len(entries) == 0 || len(entries) > 16 {
		return nil, fetch.Invalid
	}
	owned := map[string]Entry{}
	for _, entry := range entries {
		if entry.URL == "" || len(entry.URL) > 4096 || len(entry.Body) > 1<<20 || !utf8.Valid(entry.Body) {
			return nil, fetch.Invalid
		}
		if entry.Denied {
			if len(entry.Body) != 0 || entry.SHA256 != "" || entry.MediaType != "" {
				return nil, fetch.Invalid
			}
		} else {
			if entry.SHA256 != fmt.Sprintf("%x", sha256.Sum256(entry.Body)) {
				return nil, fetch.Invalid
			}
			if entry.MediaType == "" {
				entry.MediaType = "text/plain"
			}
			if entry.MediaType != "text/plain" && entry.MediaType != "application/json" && entry.MediaType != "text/html" {
				return nil, fetch.Invalid
			}
		}
		if _, exists := owned[entry.URL]; exists {
			return nil, fetch.Invalid
		}
		entry.Body = append([]byte(nil), entry.Body...)
		owned[entry.URL] = entry
	}
	return &Adapter{a, c, owned}, nil
}
func (a *Adapter) Fetch(ctx context.Context, in fetch.Request) (fetch.Result, error) {
	empty := fetch.Result{Mode: "fixed-replay"}
	if in.MaxBytes < 1 || in.MaxBytes > 1<<20 || in.MaxRequests < 1 || in.MaxRequests > 5 || in.Timeout < time.Millisecond || in.Timeout > 5*time.Second {
		return empty, fetch.Invalid
	}
	ctx, cancel := context.WithTimeout(ctx, in.Timeout)
	defer cancel()
	entry, ok := a.entries[in.URL]
	if !ok {
		return empty, fetch.Denied
	}
	for _, phase := range []string{"request", "release"} {
		if ctx.Err() != nil {
			if ctx.Err() == context.Canceled {
				return empty, fetch.Cancelled
			}
			return empty, fetch.TimedOut
		}
		if err := fetch.CheckGuard(ctx); err != nil {
			return empty, err
		}
		if err := a.authority.Check(ctx, in.URL, phase); err != nil {
			return empty, fetch.Denied
		}
	}
	if entry.Denied {
		return empty, fetch.Denied
	}
	if int64(len(entry.Body)) > in.MaxBytes {
		return empty, fetch.TooLarge
	}
	now, err := a.clock.Now()
	if err != nil || now.IsZero() {
		return empty, fetch.Unavailable
	}
	if ctx.Err() != nil {
		if ctx.Err() == context.Canceled {
			return empty, fetch.Cancelled
		}
		return empty, fetch.TimedOut
	}
	return fetch.Result{Mode: "fixed-replay", RequestedURL: in.URL, FinalURL: in.URL, MediaType: entry.MediaType, SHA256: entry.SHA256, FetchedAt: now.UTC(), Body: append([]byte(nil), entry.Body...), Sources: []string{in.URL}}, nil
}
