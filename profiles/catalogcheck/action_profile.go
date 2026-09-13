package catalogcheck

import (
	"context"
	"encoding/json"
	"fmt"
	"lerna/conformance"
)

const ActionProfileID = "api-brain-v1"

func ActionProfile() conformance.Profile {
	p := conformance.Profile{ID: ActionProfileID, Version: "1", Configuration: map[string]string{"model": "deterministic fixture, no external calls", "directory": "full 1008 API authority directory, namespace is user task constraint; six exact schemas per decision", "runtime": "one Task; SQLite Core, real grants, actual stateful execution APIs", "limits": "single 4 requests/65536 tokens/3 operations/32 queries/120s; multi 12/262144/12/128/600s; batch8; correction2; model output1024; input32768 bytes; response8192 bytes"}, Limitations: []string{"Offline strategy fixtures are not real-model accuracy evidence.", "One simulated business namespace verifies the loop; no 90%/95% thousand-API quality claim.", "Real Ark adapter retains 224K hard input reservation: incompatible with the single-step token envelope; no increased acceptance budget.", "Catalog search uses user goal and declared target namespace, never expected capability identity; reference executes serialized dependency-ready actions."}}
	for _, tc := range []struct {
		mode, state, final    string
		decisions, operations int
		first                 bool
	}{
		{"single", "COMPLETED", "authorized", 1, 1, true}, {"multistep", "COMPLETED", "authorized", 2, 2, true}, {"batch", "COMPLETED", "authorized", 1, 2, true}, {"missing", "WAITING", "draft", 1, 0, true}, {"invalid-then-correct", "COMPLETED", "authorized", 3, 2, false}, {"read-only-correction", "COMPLETED", "authorized", 3, 2, false}, {"wrong-write", "FAILED", "rejected", 1, 1, false}, {"no-progress", "WAITING", "draft", 3, 0, false},
		{"recover-dispatch", "COMPLETED", "authorized", 2, 2, true}, {"recover-record", "COMPLETED", "authorized", 2, 2, true}, {"revoked-record", "WAITING", "draft", 1, 0, true}, {"missing-resume", "COMPLETED", "authorized", 3, 2, true},
	} {
		p.Cases = append(p.Cases, conformance.Case{ID: tc.mode, Required: true, Evidence: "same_task_model_catalog_core_execution_effect", Input: tc.mode, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			r, e := RunActionCase(ctx, tc.mode, nil)
			if e != nil {
				return "", e
			}
			if r.State != tc.state || r.FinalState != tc.final || r.Decisions != tc.decisions || r.Operations != tc.operations || r.FirstCorrect != tc.first || r.DirectorySize != 1008 {
				return "", fmt.Errorf("unexpected action outcome: %+v", r)
			}
			if r.State == "COMPLETED" && r.Ledger != 997 {
				return "", fmt.Errorf("wrong accounting effect")
			}
			_, e = json.Marshal(r)
			return "verified", e
		}})
	}
	return p
}
