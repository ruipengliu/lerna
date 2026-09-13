package answer

import (
	"context"
	"lerna/conformance"
)

const ID = "bounded-answer-v1"

func Profile(executable string) (conformance.Profile, error) {
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{"model": "controlled-text; actual provider tested separately", "storage": "SQLite + controlled files", "limits": "1-2 requests; 600 input + 100 output tokens per request; input 32KiB; result 8KiB; decision 45s; IO 1s; lease 10s; default model control poll 100ms; concurrency 1; retention 5min"}, Limitations: []string{"Controlled model supplies deterministic hard bounds; no model quality claim.", "Current source-policy checks precede Core CAS; separate policy authorities have finite freshness, no distributed atomic transaction.", "Crash before a response retains unresolved full budget, even if a dispatch did not actually occur. No automatic refund or hidden regeneration."}}
	for _, name := range names {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "bounded_answer_contract", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			e := check(ctx, name)
			if e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, name := range edgeNames {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "bounded_answer_race", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			e := edge(ctx, name)
			if e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, point := range crashPoints {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "answer_sqlite_process_recovery", Input: point + "; child exit 73; reopen original state", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			e := recovery(ctx, executable, point)
			if e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	return p, nil
}
