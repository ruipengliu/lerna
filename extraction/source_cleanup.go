package extraction

import (
	"context"
	"encoding/hex"
	"lerna/memory"
	"time"
)

// FencedSource is the latest durable cutoff, not a lossless event log. Cursor
// is opaque scheduling metadata; revisions updated behind it are revisited on
// the next pass. Discovering a fence requires no retained candidate body.
type FencedSource struct {
	Cursor       string
	Invalidation SourceInvalidation
}
type SourceFenceDiscovery interface {
	ListSourceFences(context.Context, string, string, int) ([]FencedSource, error)
}
type SourceCleanupStore interface {
	SourceFenceDiscovery
	CleanupProgressStore
}
type SourceCleanupSink interface {
	// Complete covers only this configured sink, never other stores/replicas.
	ApplySource(context.Context, SourceInvalidation) (bool, error)
}
type SourceCleanupAttempt struct {
	Source   SourceInvalidation
	Complete bool
	Error    string
}
type SourceCleanupPass struct {
	Attempts []SourceCleanupAttempt
	Progress CleanupProgress
}
type SourceCleanupWorker struct {
	store   SourceCleanupStore
	sink    SourceCleanupSink
	binding CleanupBinding
	limit   int
}

func sourceCursor(s string) bool {
	raw, err := hex.DecodeString(s)
	return err == nil && len(raw) == 32 && hex.EncodeToString(raw) == s
}
func NewSourceCleanupWorker(store SourceCleanupStore, sink SourceCleanupSink, b CleanupBinding, limit int) (*SourceCleanupWorker, error) {
	if store == nil || sink == nil || b.Namespace == "" || len(b.Namespace) > 256 || b.Subject == "" || len(b.Subject) > 256 || b.Consumer == "" || len(b.Consumer) > 256 || !sourceCursor(b.ConfigSHA256) || limit < 1 || limit > 16 {
		return nil, memory.Invalid
	}
	return &SourceCleanupWorker{store, sink, b, limit}, nil
}

// Run is one bounded host wake. Each sink persists its own invalidation and
// erasure facts; this checkpoint only prevents failed items from starving
// others. Unknown sink/checkpoint replies are retried from durable state.
func (w *SourceCleanupWorker) Run(ctx context.Context) (SourceCleanupPass, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := w.store.LoadCleanupProgress(ctx, w.binding)
	if err != nil && err != memory.Missing {
		return SourceCleanupPass{}, err
	}
	if p.After != "" && !sourceCursor(p.After) {
		return SourceCleanupPass{}, memory.Invalid
	}
	items, err := w.store.ListSourceFences(ctx, w.binding.Namespace, p.After, w.limit)
	if err != nil {
		return SourceCleanupPass{}, err
	}
	if len(items) > w.limit {
		return SourceCleanupPass{}, memory.Unavailable
	}
	out := SourceCleanupPass{Progress: p}
	after := ""
	previous := p.After
	for _, item := range items {
		in := item.Invalidation
		if !sourceCursor(item.Cursor) || item.Cursor <= previous || in.Namespace != w.binding.Namespace || in.Kind == "" || in.Key == "" || in.ThroughRevision == 0 {
			return out, memory.Unavailable
		}
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) < 2*time.Second {
			if len(out.Attempts) == 0 {
				return out, memory.Unavailable
			}
			break
		}
		attempt, stop := context.WithTimeout(ctx, time.Second)
		complete, e := w.sink.ApplySource(attempt, in)
		stop()
		result := SourceCleanupAttempt{Source: in, Complete: complete && e == nil}
		if e != nil {
			result.Error = string(memory.Unavailable)
		}
		out.Attempts = append(out.Attempts, result)
		after, previous = item.Cursor, item.Cursor
	}
	if err = w.store.AdvanceCleanupProgress(ctx, w.binding, p.Version, after); err != nil {
		return out, err
	}
	out.Progress = CleanupProgress{Version: p.Version + 1, After: after}
	return out, nil
}
