package extractioncheck

import (
	"context"
	"lerna/extraction"
	"lerna/memory"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestCleanupWorkerContinuesPastFailedItemAndReturnsNextRound(t *testing.T) {
	checkCleanupFairness(t, false)
}

func TestCleanupWorkerBoundsOneUnavailableItem(t *testing.T) {
	checkCleanupFairness(t, true)
}

func checkCleanupFairness(t *testing.T, wait bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	h, err := fresh(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer h.destroy()
	store, _ := bindProcessSave(t, h, "")
	var ids []string
	for i := 0; i < 2; i++ {
		r, grant, e := h.request(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = h.client.Invoke(ctx, r, grant); e != nil {
			t.Fatal(e)
		}
		if _, e = h.exec.Run(ctx, r.OperationID); e != nil {
			t.Fatal(e)
		}
		if e = h.exec.Drain(ctx, 16); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, r.OperationID)
	}
	sort.Strings(ids)
	if count, e := h.candidates.InvalidateSource(ctx, extraction.SourceInvalidation{Namespace: "local", Kind: "note", Key: "one", ThroughRevision: 1}); e != nil || count != 2 {
		t.Fatalf("retirement: %d %v", count, e)
	}
	journal := &unavailableCleanupIntent{CleanupJournal: h.candidates, candidate: ids[0], enabled: true, wait: wait}
	cleaner := bindProcessCleanup(t, h, store, "", journal)
	b := extraction.CleanupBinding{Namespace: "local", Subject: "operator", Consumer: "fair-cleaner", ConfigSHA256: strings.Repeat("a", 64)}
	worker, err := extraction.NewCleanupWorker(h.candidates, cleaner, b, 1)
	if err != nil {
		t.Fatal(err)
	}
	first, err := worker.Run(ctx)
	if err != nil || len(first.Attempts) != 1 || first.Attempts[0].Candidate != ids[0] || first.Attempts[0].Error == "" || first.Attempts[0].Result.State == "committed" {
		t.Fatalf("failed item: %+v %v", first, err)
	}
	second, err := worker.Run(ctx)
	if err != nil || len(second.Attempts) != 1 || second.Attempts[0].Candidate != ids[1] || second.Attempts[0].Result.State != "committed" {
		t.Fatalf("later item starved: %+v %v", second, err)
	}
	if _, err = store.Read(ctx, memory.Ref{Namespace: "local", Collection: "personal", Key: ids[0]}, 1); err != nil {
		t.Fatalf("failed item body unexpectedly erased: %v", err)
	}
	if _, err = store.Read(ctx, memory.Ref{Namespace: "local", Collection: "personal", Key: ids[1]}, 1); err != memory.Missing {
		t.Fatalf("later item not erased: %v", err)
	}
	journal.enabled = false
	empty, err := worker.Run(ctx)
	if err != nil || len(empty.Attempts) != 0 || empty.Progress.After != "" {
		t.Fatalf("next round reset: %+v %v", empty, err)
	}
	worker, err = extraction.NewCleanupWorker(h.candidates, cleaner, b, 1)
	if err != nil {
		t.Fatal(err)
	}
	recovered, err := worker.Run(ctx)
	if err != nil || len(recovered.Attempts) != 1 || recovered.Attempts[0].Candidate != ids[0] || recovered.Attempts[0].Result.State != "committed" {
		t.Fatalf("failed item not retried: %+v %v", recovered, err)
	}
	if _, err = store.Read(ctx, memory.Ref{Namespace: "local", Collection: "personal", Key: ids[0]}, 1); err != memory.Missing {
		t.Fatalf("recovered item body not erased: %v", err)
	}
	changes, err := store.ReadChanges(ctx, "local", "personal", 0, 16)
	if err != nil || len(changes) != 4 {
		t.Fatalf("cleanup effects: %d %v", len(changes), err)
	}
}

// Interrupt only the durable cleanup-intent boundary. Later candidates still
// use the actual journal and Memory deletion; no successful result is stubbed.
type unavailableCleanupIntent struct {
	extraction.CleanupJournal
	candidate string
	enabled   bool
	wait      bool
}

func (j *unavailableCleanupIntent) ReserveCleanup(ctx context.Context, ns, subject, candidate string, in memory.DeleteRequest) error {
	if j.enabled && candidate == j.candidate {
		if j.wait {
			<-ctx.Done()
		}
		return memory.Unavailable
	}
	return j.CleanupJournal.ReserveCleanup(ctx, ns, subject, candidate, in)
}
