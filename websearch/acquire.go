package websearch

import (
	"context"
	"lerna/fetch"
)

// Acquirer retains the actual search response under the original admitted
// operation. It shares the acquisition ledger with page fetches; using distinct
// ledgers for one task would not enforce a combined network budget.
// The host must bind the intent fingerprint to the exact query and limits and
// check current task/source disclosure authority before passing this seam.
type Acquirer struct {
	searcher Searcher
	store    fetch.OutcomeStore
	evidence fetch.Evidence
	recovery *fetch.Acquirer
}

func NewAcquirer(s Searcher, store fetch.OutcomeStore, evidence fetch.Evidence) (*Acquirer, error) {
	if s == nil {
		return nil, fetch.Invalid
	}
	recovery, err := fetch.NewAcquirer(searchFetch{searcher: s}, store, evidence)
	if err != nil {
		return nil, err
	}
	return &Acquirer{s, store, evidence, recovery}, nil
}
func (a *Acquirer) Acquire(ctx context.Context, intent fetch.AttemptIntent, request Request) (fetch.Outcome, bool, error) {
	if request.MaxRequests != int(intent.MaxRequests) {
		return fetch.Outcome{}, false, fetch.Invalid
	}
	bound, err := fetch.NewAcquirer(searchFetch{searcher: a.searcher, request: request}, a.store, a.evidence)
	if err != nil {
		return fetch.Outcome{}, false, err
	}
	// Fetch's request envelope is only the reservation bound here. The private
	// bridge holds the actual typed search request, never an encoded fake URL.
	return bound.Acquire(ctx, intent, fetch.Request{MaxRequests: request.MaxRequests})
}
func (a *Acquirer) Recover(ctx context.Context, intent fetch.AttemptIntent) (fetch.Outcome, bool, error) {
	return a.recovery.Recover(ctx, intent)
}

type searchFetch struct {
	searcher Searcher
	request  Request
}

func (s searchFetch) Fetch(ctx context.Context, limit fetch.Request) (fetch.Result, error) {
	if s.request.MaxRequests < 1 || limit.MaxRequests != s.request.MaxRequests {
		return fetch.Result{}, fetch.Invalid
	}
	if err := fetch.CheckGuard(ctx); err != nil {
		return fetch.Result{}, err
	}
	result, err := s.searcher.Search(ctx, s.request)
	if denied := fetch.CheckGuard(ctx); denied != nil {
		return fetch.Result{Mode: result.Acquisition.Mode, Requests: result.Acquisition.Requests}, denied
	}
	return result.Acquisition, err
}
