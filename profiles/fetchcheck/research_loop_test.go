package fetchcheck

import (
	"context"
	"lerna/profiles/searchcheck"
	"testing"
)

func TestOneTaskSearchesThenFetchesActualCandidate(t *testing.T) { runSearchActionTask(t, "acquired") }
func runSearchActionTask(t *testing.T, mode string) {
	t.Helper()
	if _, err := runResearchTask(context.Background(), mode, nil); err != nil {
		t.Fatal(err)
	}
}
func runReferenceResearchTask(t *testing.T, material searchcheck.Case) ResearchRecord {
	t.Helper()
	record, err := CheckReferenceResearch(context.Background(), material.ID, ResearchConfig{MaxQueries: 128})
	if err != nil {
		t.Fatal(err)
	}
	return record
}
func TestOneTaskReportsDeniedPageAsEvidenceGap(t *testing.T) { runSearchActionTask(t, "page_denied") }

func TestOneTaskRejectsUnauthorizedSearchCandidate(t *testing.T) {
	runSearchActionTask(t, "candidate_denied")
}

func TestConcurrentResearchTasksPreserveBudgets(t *testing.T) {
	for _, name := range []string{"a", "b", "c", "d"} {
		t.Run(name, func(t *testing.T) { t.Parallel(); runSearchActionTask(t, "acquired") })
	}
}

func TestOneTaskReportsEmptySearchWithoutFetching(t *testing.T) {
	runSearchActionTask(t, "empty_search")
}

func TestOneTaskPreservesBothConflictingSources(t *testing.T) { runSearchActionTask(t, "conflicting") }
