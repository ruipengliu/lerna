package tasks

import (
	"context"
	"lerna/authorization"
)

type RecoverySource interface {
	ListRecoverable(context.Context, RecoveryQuery) (RunPage, error)
}

// Selector can order or omit candidates. References outside this page and
// duplicates are rejected. Selection holds no authoritative execution state.
type Selector interface{ Order([]Ref) ([]Ref, error) }
type SelectorFunc func([]Ref) ([]Ref, error)

func (f SelectorFunc) Order(refs []Ref) ([]Ref, error) { return f(refs) }

type Wakeup interface{ Wait(context.Context) error }
type WakeupFunc func(context.Context) error

func (f WakeupFunc) Wait(ctx context.Context) error { return f(ctx) }

type DispatchResult struct {
	Runs []RunSnapshot
	Next string
}

// Dispatch handles one bounded wake and one page, sequentially. Hosts may call
// Run concurrently up to the runner's admission bound. Another sweep starts at
// the beginning to discover inserts before a previous cursor.
func (r *Runner) Dispatch(ctx context.Context, source RecoverySource, selector Selector, wakeup Wakeup, q RecoveryQuery) (DispatchResult, error) {
	if source == nil || selector == nil || wakeup == nil || q.Limit < 1 || q.Limit > 1000 {
		return DispatchResult{}, failure(authorization.Invalid)
	}
	waitCtx, cancel := context.WithTimeout(ctx, r.limits.IOTimeout)
	defer cancel()
	if err := wakeup.Wait(waitCtx); err != nil {
		return DispatchResult{}, err
	}
	readCtx, readCancel := context.WithTimeout(ctx, r.limits.IOTimeout)
	defer readCancel()
	page, err := source.ListRecoverable(readCtx, q)
	if err != nil {
		return DispatchResult{}, err
	}
	if len(page.Runs) > q.Limit {
		return DispatchResult{}, failure(authorization.Invalid)
	}
	refs := make([]Ref, 0, len(page.Runs))
	byRef := map[Ref]RunSnapshot{}
	for _, s := range page.Runs {
		refs = append(refs, s.Task.Ref)
		byRef[s.Task.Ref] = s
	}
	order, err := selector.Order(refs)
	if err != nil {
		return DispatchResult{}, err
	}
	if len(order) > len(refs) {
		return DispatchResult{}, failure(authorization.Invalid)
	}
	seen := map[Ref]bool{}
	for _, ref := range order {
		if _, ok := byRef[ref]; !ok || seen[ref] {
			return DispatchResult{}, failure(authorization.Invalid)
		}
		seen[ref] = true
	}
	out := DispatchResult{Next: page.Next}
	for _, ref := range order {
		s, err := r.Run(ctx, byRef[ref])
		if authorization.Is(err, authorization.Conflict) {
			continue
		}
		if err != nil {
			return out, err
		}
		out.Runs = append(out.Runs, s)
	}
	return out, nil
}
