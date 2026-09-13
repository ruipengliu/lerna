package extraction

import (
	"context"
	"encoding/hex"
	"lerna/memory"
	"time"
)

type CleanupScheduling interface {
	CleanupDiscovery
	CleanupProgressStore
}
type CleanupAttempt struct {
	Candidate, Error string
	Result           memory.DeletionInspection
}
type CleanupPass struct {
	Attempts []CleanupAttempt
	Progress CleanupProgress
}
type CleanupWorker struct {
	store   CleanupScheduling
	cleaner *Cleaner
	binding CleanupBinding
	limit   int
}

func NewCleanupWorker(store CleanupScheduling, cleaner *Cleaner, binding CleanupBinding, limit int) (*CleanupWorker, error) {
	hash, err := hex.DecodeString(binding.ConfigSHA256)
	if store == nil || cleaner == nil || binding.Namespace != cleaner.binding.Namespace || binding.Subject != cleaner.binding.Subject || binding.Consumer == "" || len(binding.Consumer) > 256 || err != nil || len(hash) != 32 || hex.EncodeToString(hash) != binding.ConfigSHA256 || limit < 1 || limit > 16 {
		return nil, memory.Invalid
	}
	return &CleanupWorker{store, cleaner, binding, limit}, nil
}

// Run is one explicit bounded pass, not a background loop. Advance only after
// attempts: a crash leaves the old cursor and subsequent passes reconcile each
// original deletion. Pending/error items do not starve later candidates; empty
// pages restart the next pass at the beginning. Consumer cleanup status remains
// the Memory report, never inferred from cursor advancement.
func (w *CleanupWorker) Run(ctx context.Context) (CleanupPass, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	p, err := w.store.LoadCleanupProgress(ctx, w.binding)
	if err != nil && err != memory.Missing {
		return CleanupPass{}, err
	}
	ids, err := w.store.ListRetiredSaves(ctx, w.binding.Namespace, w.binding.Subject, p.After, w.limit)
	if err != nil {
		return CleanupPass{}, err
	}
	if len(ids) > w.limit {
		return CleanupPass{}, memory.Unavailable
	}
	out := CleanupPass{Progress: p}
	after := ""
	for _, id := range ids {
		if id == "" || (after != "" && id <= after) || id <= p.After {
			return out, memory.Unavailable
		}
		if ctx.Err() != nil {
			return out, memory.Unavailable
		}
		// Reserve time for the durable checkpoint even when one dependency is
		// unavailable until its deadline. A slow item must not consume the full
		// pass and force every replacement worker back onto that same item.
		deadline, _ := ctx.Deadline()
		if time.Until(deadline) < 2*time.Second {
			if len(out.Attempts) == 0 {
				return out, memory.Unavailable
			}
			break
		}
		attempt, stop := context.WithTimeout(ctx, time.Second)
		result, e := w.cleaner.Clean(attempt, id)
		stop()
		item := CleanupAttempt{Candidate: id, Result: result}
		if e != nil {
			item.Error = string(memory.Unavailable)
			if e == memory.Denied || e == memory.Conflict || e == memory.IdentityConflict || e == memory.AdmissionExpired {
				item.Error = e.Error()
			}
		}
		out.Attempts = append(out.Attempts, item)
		after = id
	}
	if err = w.store.AdvanceCleanupProgress(ctx, w.binding, p.Version, after); err != nil {
		return out, err
	}
	out.Progress = CleanupProgress{Version: p.Version + 1, After: after}
	return out, nil
}
