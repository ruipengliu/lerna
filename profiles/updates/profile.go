package updates

import (
	"context"
	"lerna/conformance"
)

const ID = "task-updates-v1"

func Profile(executable string) (conformance.Profile, error) {
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{"storage": "SQLite + controlled text references; local binary SDK", "identity": "same authenticated task subject; separate input/append/revise/adjust rights", "limits": "32 updates; 16 interactions; 16 input refs; reply 4096B; 16 choice values; input 32KiB; IO/stop 1s; 100ms update control; 1 model worker; 3 work attempts", "model": "controlled only; no paid requests"}, Limitations: []string{"Trusted module creates waits; no public operation can create or approve authorization interactions.", "Expiry is derived from persisted deadline and trusted time; expired questions remain blocking until semantically invalidated.", "Source checks have finite local freshness, not cross-authority atomicity. No UI, transport push, distributed budget recovery or real device execution.", "Controlled-model and child-process evidence do not establish real-model interaction quality."}}
	for _, name := range names {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "task_input_contract", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			e := check(ctx, name)
			if e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, name := range advancedNames {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "task_input_concurrency", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := advanced(ctx, name); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	p.Cases = append(p.Cases, conformance.Case{ID: "v08-storage-upgrade", Required: true, Evidence: "old_encoder_sqlite", Input: "dea600f runtime gob", Expected: "verified", Check: func(ctx context.Context) (string, error) {
		if e := upgrade(ctx); e != nil {
			return "", e
		}
		return "verified", nil
	}})
	for _, point := range crashPoints {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "task_input_process_recovery", Input: point + "; child exit 73; reopen SQLite", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			e := recovery(ctx, executable, point)
			if e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	return p, nil
}
