package answer

import (
	"context"
	"testing"
)

func TestCurrentMemoryControlsActualAnswerPublication(t *testing.T) {
	for _, phase := range []string{"model", "publication"} {
		for _, change := range []string{"related", "unrelated", "revoke"} {
			t.Run(phase+"/"+change, func(t *testing.T) {
				report, e := RunPersonalizedInvalidation(context.Background(), phase, change)
				if e != nil {
					t.Fatal(e)
				}
				want := change == "unrelated"
				if report.Published != want || report.ModelCalls != 1 || report.ReadAllocated != 1 || (report.ValidationError == "") != want {
					t.Fatalf("stale publication outcome %+v", report)
				}
				if phase == "publication" && !report.OutputSaved {
					t.Fatal("did not reach output-before-publication boundary")
				}
			})
		}
	}
}

func TestDeletedMemoryBlocksAnswerPublication(t *testing.T) {
	for _, phase := range []string{"model", "publication"} {
		t.Run(phase, func(t *testing.T) {
			r, err := RunPersonalizedInvalidation(context.Background(), phase, "delete")
			if err != nil {
				t.Fatal(err)
			}
			if r.Published || r.ValidationError == "" || r.ModelCalls != 1 || r.ReadAllocated != 1 {
				t.Fatalf("deleted source allowed publication or replaced original read: %+v", r)
			}
			if phase == "publication" && !r.OutputSaved {
				t.Fatal("deletion did not follow durable output")
			}
		})
	}
}
