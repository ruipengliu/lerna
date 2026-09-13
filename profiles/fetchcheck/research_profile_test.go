package fetchcheck_test

import (
	"context"
	"encoding/json"
	"errors"
	"lerna/profiles/fetchcheck"
	"testing"
)

func TestFrozenResearchProfileRejectsInvalidOrCancelledRun(t *testing.T) {
	if _, err := fetchcheck.CheckFrozenResearch(context.Background(), "unknown"); err == nil {
		t.Fatal("unknown frozen case admitted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fetchcheck.CheckFrozenResearch(ctx, "answerable"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled profile returned %v", err)
	}
}

func TestReferenceResearchReplayPreservesModeAndReservation(t *testing.T) {
	for _, id := range []string{"answerable", "insufficient", "conflicting", "fetch_failed"} {
		t.Run(id, func(t *testing.T) {
			record, err := fetchcheck.CheckReferenceResearch(context.Background(), id, fetchcheck.ResearchConfig{Replay: true, MaxQueries: 128})
			if err != nil {
				t.Fatal(err)
			}
			want := uint64(2)
			if id == "conflicting" {
				want = 3
			}
			if record.Usage.SearchRequests != 0 || record.Usage.PageRequests != 0 || record.Usage.NetworkCharged != want || record.Usage.ModelRequests != 3 {
				t.Fatal("replay sent HTTP or escaped task accounting")
			}
			discoveries, failures := 0, 0
			for _, block := range record.Input.Blocks {
				var evidence struct {
					Mode     string
					Requests uint32
					Status   string
				}
				if json.Unmarshal([]byte(block.Text), &evidence) != nil || evidence.Mode != "fixed-replay" || evidence.Requests != 0 {
					t.Fatal("fixed material masqueraded as live acquisition")
				}
				if id == "fetch_failed" {
					switch block.Role {
					case "search-candidates":
						discoveries++
						if evidence.Status != "discovered" {
							t.Fatal("fixed discovery was lost")
						}
					case "external-evidence-gap":
						failures++
						if evidence.Status != "denied" {
							t.Fatal("fixed denial was lost")
						}
					default:
						t.Fatalf("fixed denial promoted to %s", block.Role)
					}
				}
			}
			if id == "fetch_failed" && (discoveries != 1 || failures != 1) {
				t.Fatal("replay must retain both discovery and page denial")
			}
		})
	}
}

func TestReferenceResearchReopensAnswerPhaseWithoutRepeatingActions(t *testing.T) {
	for _, id := range []string{"answerable", "conflicting", "fetch_failed"} {
		t.Run(id, func(t *testing.T) {
			record, err := fetchcheck.CheckReferenceResearch(context.Background(), id, fetchcheck.ResearchConfig{Reopen: true, MaxQueries: 128})
			if err != nil {
				t.Fatal(err)
			}
			pages := uint64(1)
			if id == "conflicting" {
				pages = 2
			}
			if record.Usage.QueryLimit != 128 || record.Usage.ActionQueries > 128 || record.Recovery == nil || record.Recovery.ResumedWorkerGeneration <= record.Recovery.OriginalWorkerGeneration || record.Recovery.ActionModelCallsAfterReopen != 0 {
				t.Fatal("reopen did not fence the old worker generation")
			}
			// Reopening loses the prior host's immutable fact cache. Each settled
			// operation is read by the action host and new answer host. The driver
			// reuses facts that it successfully committed within its own scope.
			if record.Usage.OutcomeQueries != 2*(pages+1) {
				t.Fatal("reopen reused the old host outcome cache or omitted query charges")
			}
			if record.Answer.Status != id || record.Usage.ModelRequests != 3 || record.Usage.SearchRequests != 1 || record.Usage.PageRequests != pages || record.Usage.NetworkCharged != pages+1 {
				t.Fatal("reopen replaced work or reset task budget")
			}
		})
	}
}

func TestDuckDuckGoResearchPublishesAnswerThroughOriginalTask(t *testing.T) {
	for _, mode := range []string{"http", "replay", "reopen"} {
		for _, id := range []string{"answerable", "insufficient", "conflicting", "fetch_failed"} {
			t.Run(mode+"/"+id, func(t *testing.T) {
				record, err := fetchcheck.CheckReferenceResearch(context.Background(), id, fetchcheck.ResearchConfig{MaxQueries: 128, SearchFormat: "duckduckgo-html", Replay: mode == "replay", Reopen: mode == "reopen"})
				if err != nil {
					t.Fatal(err)
				}
				pages := uint64(1)
				if id == "conflicting" {
					pages = 2
				}
				searches, fetches := uint64(1), pages
				if mode == "replay" {
					searches, fetches = 0, 0
				}
				if record.Answer.Status != id || record.Usage.SearchRequests != searches || record.Usage.PageRequests != fetches || record.Usage.NetworkCharged != pages+1 || record.Usage.ModelRequests != 3 || record.Usage.ActionQueries == 0 || record.Usage.ActionQueries > 128 {
					t.Fatalf("incomplete research: %+v", record)
				}
				if mode == "reopen" && (record.Recovery == nil || record.Recovery.ActionModelCallsAfterReopen != 0) {
					t.Fatal("reopen repeated actions")
				}
			})
		}
	}
}
