package asynccheck

import (
	"context"
	"lerna/conformance"
)

const ID = "async-recovery-v1"

func Profile(executable string) (conformance.Profile, error) {
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{"storage": "SQLite authority + independent SQLite jobs + controlled content", "binding": "local binary Capability SDK; authenticated host progress port", "limits": "32 operations/cancels; 8 reports/op; 64 outbox; 16 evidence/op; 4 inspections; 1 cancel dispatch; 1s IO; 1s polling; 3s recovery lease; 1m recovery window", "model": "none"}, Limitations: []string{"Private simulated target only; no shared-resource takeover, network deployment or paid service.", "Unknown sends are never automatically resent; missing random handles and exhausted budgets remain unresolved.", "Conflicting terminal evidence is retained and blocks further starts; no automatic conflict resolution or terminal task reopening.", "Handles are bounded non-secret opaque locators in the reference target, never public protocol fields."}}
	for _, name := range caseNames {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "durable_async_contract", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := check(ctx, name); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, point := range crashPoints {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "independent_sqlite_process_recovery", Input: point + "; exit 73; reopen", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := recovery(ctx, executable, point); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	return p, nil
}
