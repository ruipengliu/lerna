package extractioncheck_test

import (
	"context"
	"lerna/profiles/extractioncheck"
	"testing"
	"time"
)

func TestExtractedCandidateAutomaticallySavesToRealMemory(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := extractioncheck.CheckSave(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidatedCandidateMemoryIsActuallyDeleted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := extractioncheck.CheckSaveFault(ctx, "cleanup-after-invalidation"); err != nil {
		t.Fatal(err)
	}
}

func TestCleanupReconcilesLostReplyAndKeepsUnknownDeletionPending(t *testing.T) {
	for _, mode := range []string{"cleanup-lost-reply", "cleanup-before-delete"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := extractioncheck.CheckSaveFault(ctx, mode); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBoundedCleanupWorkerUsesDurableProgress(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := extractioncheck.CheckSaveFault(ctx, "cleanup-worker"); err != nil {
		t.Fatal(err)
	}
}

func TestAutomaticSaveReconcilesLostReplyAndPreservesUnknown(t *testing.T) {
	for _, mode := range []string{"lost-reply", "before-commit", "save-denied", "retain-revoked-before-plan", "source-invalidated-after-save"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := extractioncheck.CheckSaveFault(ctx, mode); err != nil {
				t.Fatal(err)
			}
		})
	}
}
