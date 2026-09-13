package extractioncheck_test

import (
	"context"
	"lerna/profiles/extractioncheck"
	"testing"
	"time"
)

func TestCoreAndCapabilitySDKExecuteLocalExtraction(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := extractioncheck.CheckTask(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredCandidateKeepsConfirmedExecutionFact(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := extractioncheck.CheckRetiredTask(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestTaskChecksIndependentExtractionPermissions(t *testing.T) {
	for _, action := range []string{"memory.source.read", "memory.extract", "memory.candidate.retain", "memory.candidate.disclose"} {
		t.Run(action, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := extractioncheck.CheckTaskPermission(ctx, action); err != nil {
				t.Fatal(err)
			}
		})
	}
}
