package executioncheck

import (
	"context"
	"lerna/conformance"
)

const ID = "synchronous-execution-v1"

func Profile(executable string) (conformance.Profile, error) {
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{"storage": "SQLite authority with separate execution/task/content partitions; independent SQLite target + file content", "binding": "local binary Capability SDK; fixed authenticated host, subject and Worker; no network identity claim", "capability": "counter.add@1 / sqlite-counter@1; exact JSON Schema 2020-12; exclusive versioned target", "limits": "32 operations; 8 reports/op; 64 outbox items; 4 inspections/op; 1 local Driver slot; one operation per budgeted worker generation; 3 task attempts; 1s IO/Driver; input 32KiB, output/evidence each 8KiB", "model": "none"}, Limitations: []string{"Only exclusively controlled local simulated targets; no shared-resource takeover or arbitrary external exactly-once.", "Missing target operation remains UNKNOWN, even after a crash before actual dispatch. No automatic Start resend or quota refund.", "Local host supplies peer binding; no actual mTLS/gRPC/WebSocket deployment or real business API/model call.", "Late effects are retained; source/permission failures may make evidence unavailable without undoing the effect. Cross-authority content freshness is finite."}}
	for _, name := range names {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "synchronous_execution_contract", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := check(ctx, name); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, name := range faultNames {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "execution_fault_boundary", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := faultCheck(ctx, name); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	p.Cases = append(p.Cases, conformance.Case{ID: "v09-runtime-upgrade", Required: true, Evidence: "old_encoder_partition_upgrade", Input: "19a323b runtime gob", Expected: "verified", Check: func(ctx context.Context) (string, error) {
		if e := upgrade(ctx); e != nil {
			return "", e
		}
		return "verified", nil
	}})
	for _, point := range crashPoints {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "independent_sqlite_process_recovery", Input: point + "; exit 73; reopen authority and target", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := recovery(ctx, executable, point); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	return p, nil
}
