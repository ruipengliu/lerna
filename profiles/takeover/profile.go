package takeover

import (
	"context"
	"lerna/conformance"
)

const ID = "resource-control-v1"

func Profile(executable string) (conformance.Profile, error) {
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{"storage": "SQLite authority Format2 exact descriptor partitions + independent SQLite fenced target", "binding": "binary local Capability SDK; trusted static resource scopes; add/scale/sync entries", "limits": "8 scopes; 16 participants/scope; 32 descriptor partitions; 32 control ops/scope; 8 control checks; 1s IO; 3s lease; 1m window; 32 invocations/partition; 4 invocation checks; 4MiB execution store", "model": "none"}, Limitations: []string{"Simulated local API target only; no GUI, real network deployment or arbitrary third-party fencing.", "Target control generation and business version are enforced atomically at every mutation; unsupported or unconfirmed fencing stays ACCEPTED.", "Possibly sent controls are observed, never blindly resent; exhausted original invocation budgets remain held.", "Static scope mapping is frozen before first invocation. Legacy descriptors remain separate; live remapping and authority failover are unsupported."}}
	for _, group := range []struct {
		names []string
		run   func(context.Context, string) error
	}{{caseNames, check}, {boundaryNames, boundary}, {concurrencyNames, concurrency}} {
		for _, name := range group.names {
			p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "durable_resource_control_contract", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
				if e := group.run(ctx, name); e != nil {
					return "", e
				}
				return "verified", nil
			}})
		}
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
