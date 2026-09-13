package extractioncheck

import (
	"context"
	"crypto/sha256"
	"fmt"
	"lerna/adapters/memoryauth"
	"lerna/adapters/sqlitememory"
	"lerna/extraction"
	"lerna/memory"
	"lerna/schema"
	"time"
)

func checkMemoryCleanup(ctx context.Context, h *harness, store *sqlitememory.Store, policy *memoryauth.Authority, schemas *schema.Registry, candidate, mode string) error {
	var backend memory.Store = store
	if mode == "cleanup-lost-reply" || mode == "cleanup-before-delete" {
		backend = cleanupStoreFault{Store: store, mode: mode}
	}
	service, err := memory.New(backend, policy, schemas, h.clock, memory.Config{Location: "local", Timeout: time.Second})
	if err != nil {
		return err
	}
	reporter, err := memory.NewDeletionReporter(store, store, nil)
	if err != nil {
		return err
	}
	service, err = service.WithDeletionReporter(reporter)
	if err != nil {
		return err
	}
	b := memory.Binding{Token: h.token, Namespace: "local", Subject: "operator", Location: "local", Recipient: "local"}
	cleaner, err := extraction.NewCleaner(h.candidates, service, b, "task", h.operation)
	if err != nil {
		return err
	}
	save, err := h.candidates.LookupSave(ctx, "local", "operator", candidate)
	if err != nil {
		return err
	}
	ref := memory.Ref{Namespace: save.Request.Ref.Namespace, Collection: save.Request.Ref.Collection, Key: save.Request.Ref.Key}
	retired, err := h.candidates.ListRetiredSaves(ctx, "local", "operator", "", 16)
	if err != nil || len(retired) != 1 || retired[0] != candidate {
		return fmt.Errorf("retired save discovery: %v", err)
	}
	if _, err = store.Read(ctx, ref, 1); err != nil {
		return fmt.Errorf("cleanup fixture did not contain persisted Memory: %v", err)
	}
	var result memory.DeletionInspection
	if mode == "cleanup-worker" {
		binding := extraction.CleanupBinding{Namespace: "local", Subject: "operator", Consumer: "local-memory-cleanup", ConfigSHA256: fmt.Sprintf("%x", sha256.Sum256([]byte("local/operator/personal/task/local-extracted/cleanup-v1/batch-1")))}
		worker, e := extraction.NewCleanupWorker(h.candidates, cleaner, binding, 1)
		if e != nil {
			return e
		}
		pass, e := worker.Run(ctx)
		if e != nil || len(pass.Attempts) != 1 || pass.Attempts[0].Error != "" || pass.Progress.Version != 1 || pass.Progress.After != candidate {
			return fmt.Errorf("cleanup worker first pass: %+v %v", pass, e)
		}
		result = pass.Attempts[0].Result
		// A replacement worker must load the persisted cursor, not start over.
		worker, e = extraction.NewCleanupWorker(h.candidates, cleaner, binding, 1)
		if e != nil {
			return e
		}
		empty, e := worker.Run(ctx)
		if e != nil || len(empty.Attempts) != 0 || empty.Progress.Version != 2 || empty.Progress.After != "" {
			return fmt.Errorf("cleanup worker cursor recovery: %+v %v", empty, e)
		}
		repeat, e := worker.Run(ctx)
		if e != nil || len(repeat.Attempts) != 1 || repeat.Attempts[0].Result.State != "committed" {
			return fmt.Errorf("cleanup worker next round: %+v %v", repeat, e)
		}
	} else {
		result, err = cleaner.Clean(ctx, retired[0])
	}
	if mode == "cleanup-before-delete" {
		if err != memory.Unavailable || result.State != "unknown" || result.Report != nil {
			return fmt.Errorf("unknown deletion reported complete: %s %v", result.State, err)
		}
		request, e := h.candidates.LookupCleanup(ctx, "local", "operator", candidate)
		if e != nil {
			return e
		}
		replay, e := cleaner.Clean(ctx, candidate)
		if e != nil || replay.State != "unknown" || replay.Report != nil {
			return fmt.Errorf("unknown deletion retry: %s %v", replay.State, e)
		}
		after, e := h.candidates.LookupCleanup(ctx, "local", "operator", candidate)
		if e != nil || after.OperationID != request.OperationID {
			return fmt.Errorf("unknown deletion replaced operation: %v", e)
		}
		if _, e = store.Read(ctx, ref, 1); e != nil {
			return fmt.Errorf("unknown deletion claimed missing body: %v", e)
		}
		changes, e := store.ReadChanges(ctx, "local", "personal", 0, 16)
		if e != nil || len(changes) != 1 {
			return fmt.Errorf("unexpected unknown deletion effect: %v", e)
		}
		return nil
	}
	if err != nil || result.State != "committed" || result.Report == nil || result.Report.Authority != "committed" {
		return fmt.Errorf("Memory cleanup: %s %v", result.State, err)
	}
	if _, err = store.Read(ctx, ref, 1); err != memory.Missing {
		return fmt.Errorf("Memory body not erased: %v", err)
	}
	request, err := h.candidates.LookupCleanup(ctx, "local", "operator", candidate)
	if err != nil {
		return err
	}
	replay, err := cleaner.Clean(ctx, candidate)
	if err != nil || replay.State != "committed" || replay.Report == nil || replay.Report.Event != result.Report.Event {
		return fmt.Errorf("cleanup replay changed result: %v", err)
	}
	after, err := h.candidates.LookupCleanup(ctx, "local", "operator", candidate)
	if err != nil || after.OperationID != request.OperationID {
		return fmt.Errorf("replacement cleanup operation: %v", err)
	}
	changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
	if err != nil || len(changes) != 2 {
		return fmt.Errorf("unexpected cleanup effects: %v", err)
	}
	if len(result.Report.Replicas) != 1 || result.Report.Replicas[0].State != "not_covered" || len(result.Report.Derived) != 1 || result.Report.Derived[0].State != "not_covered" {
		return fmt.Errorf("unconnected consumers reported cleaned")
	}
	return nil
}

// Keep the real admission, source state and SQLite implementation; only the
// precise deletion commit/return boundary is interrupted.
type cleanupStoreFault struct {
	*sqlitememory.Store
	mode string
}

func (s cleanupStoreFault) Delete(ctx context.Context, in memory.Deletion) (memory.Receipt, error) {
	if s.mode == "cleanup-before-delete" {
		return memory.Receipt{}, memory.Unavailable
	}
	if _, err := s.Store.Delete(ctx, in); err != nil {
		return memory.Receipt{}, err
	}
	return memory.Receipt{}, memory.Unavailable
}
