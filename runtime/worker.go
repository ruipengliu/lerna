package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ruipengliu/lerna/api"
)

// Worker 先预留真实处理容量再领取。每实例 holder 不复用，等待责任不占 goroutine。
type Worker struct {
	Store       Store
	Registry    *Registry
	Scopes      []Scope
	Kinds       []string
	Concurrency int
	Lease       time.Duration
	Poll        time.Duration
	Logger      *slog.Logger
}

func (w *Worker) Run(ctx context.Context) error {
	if w.Concurrency < 1 || w.Concurrency > 64 || len(w.Scopes) == 0 || len(w.Scopes) > 100 || len(w.Kinds) == 0 || w.Lease < time.Second || w.Poll < time.Millisecond {
		return api.E("invalid_request", "invalid_worker_limits")
	}
	logger := w.Logger
	if logger == nil {
		logger = slog.Default()
	}
	holder := api.NewID("boot")
	slots := make(chan struct{}, w.Concurrency)
	var active sync.WaitGroup
	t := time.NewTicker(w.Poll)
	defer t.Stop()
	defer active.Wait()
	index := 0
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
		for visited := 0; visited < len(w.Scopes); visited++ {
			scope := w.Scopes[index%len(w.Scopes)]
			index++
			reserved := false
			select {
			case slots <- struct{}{}:
				reserved = true
			default:
			}
			if !reserved {
				break
			}
			works, status, err := w.Store.Claim(ctx, scope, holder, w.Kinds, 1, w.Lease)
			if err != nil || status != Committed || len(works) == 0 {
				<-slots
				if err != nil && !errors.Is(err, context.Canceled) {
					logger.Warn("job claim failed", "status", status)
				}
				continue
			}
			work := works[0]
			h, ok := w.Registry.Job(work.Job.Kind)
			if !ok {
				<-slots
				logger.Error("job recovery handler missing", "kind", work.Job.Kind)
				continue
			}
			active.Add(1)
			go func() {
				defer active.Done()
				defer func() { <-slots }()
				if err := w.runOwned(ctx, scope, work, h); err != nil && !errors.Is(err, context.Canceled) {
					logger.Warn("job remains recoverable", "kind", work.Job.Kind, "claim_lost", errors.Is(err, ErrClaimLost))
				}
			}()
		}
	}
}

// Drain 一轮扫描供确定验收与 CLI 使用；不改变领域完成判断。
func Drain(ctx context.Context, store Store, scope Scope, registry *Registry, max int) error {
	if max < 1 || max > 10000 {
		return api.E("invalid_request", "invalid_drain_limit")
	}
	holder := api.NewID("boot")
	for i := 0; i < max; i++ {
		works, status, err := store.Claim(ctx, scope, holder, registry.JobKinds(), 1, 30*time.Second)
		if err != nil {
			return err
		}
		if status == CommitUnknown {
			return ErrCommitUnknown
		}
		if len(works) == 0 {
			return nil
		}
		h, ok := registry.Job(works[0].Job.Kind)
		if !ok {
			return api.E("unsupported", "recovery_handler_missing")
		}
		if err = h(ctx, store, scope, works[0]); err != nil {
			return fmt.Errorf("job %s: %w", works[0].Job.Kind, err)
		}
	}
	return api.E("overloaded", "drain_limit_reached")
}
