package fetchcheck

import (
	"context"
	"testing"
)

func TestResearchAnswerPreservesPartialFailureInEitherOrder(t *testing.T) {
	for _, mode := range []string{"first_page_denied", "second_page_denied"} {
		t.Run(mode, func(t *testing.T) {
			record, err := runResearchTask(context.Background(), mode, nil)
			if err != nil {
				t.Fatal(err)
			}
			pages, gaps := 0, 0
			for _, block := range record.Input.Blocks {
				if block.Role == "external-evidence" {
					pages++
				}
				if block.Role == "external-evidence-gap" {
					gaps++
				}
			}
			if pages != 1 || gaps != 1 {
				t.Fatal("answer input lost a successful page or a failed source")
			}
			answer := record.Answer
			if answer.Status != "fetch_failed" || len(answer.Claims) != 1 || len(answer.Gaps) != 1 || len(answer.Gaps[0].Sources) != 1 || len(answer.Claims[0].Citations) != 1 {
				t.Fatal("published partial answer omitted acquired evidence or its failure gap")
			}
			if answer.Claims[0].Citations[0].Source == answer.Gaps[0].Sources[0] {
				t.Fatal("failed source became a page claim")
			}
			if record.Usage.SearchRequests != 1 || record.Usage.PageRequests != 2 || record.Usage.NetworkCharged != 3 || record.Usage.ModelRequests != 3 {
				t.Fatal("partial failure lost task accounting")
			}
		})
	}
}

func TestResearchAnswerRetainsEveryFailedPage(t *testing.T) {
	record, err := runResearchTask(context.Background(), "both_pages_denied", nil)
	if err != nil {
		t.Fatal(err)
	}
	failures := map[string]bool{}
	discoveries := 0
	for _, block := range record.Input.Blocks {
		switch block.Role {
		case "external-evidence-gap":
			failures[block.Ref] = true
		case "search-candidates":
			discoveries++
		default:
			t.Fatalf("failed page promoted to %s", block.Role)
		}
	}
	if len(record.Input.Blocks) != 3 || len(failures) != 2 || discoveries != 1 || record.Answer.Status != "fetch_failed" || len(record.Answer.Claims) != 0 || len(record.Answer.Gaps) != 1 || len(record.Answer.Gaps[0].Sources) != 2 {
		t.Fatal("one failed source masked the other")
	}
	if record.Answer.Gaps[0].Sources[0] == record.Answer.Gaps[0].Sources[1] {
		t.Fatal("duplicated failure identity")
	}
	for _, ref := range record.Answer.Gaps[0].Sources {
		if !failures[ref] {
			t.Fatal("answer substituted discovery for an actual page failure")
		}
	}
	if record.Usage.PageRequests != 2 || record.Usage.NetworkCharged != 3 {
		t.Fatal("failed page reservation was lost")
	}
}
