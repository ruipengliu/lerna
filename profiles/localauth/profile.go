package localauth

import (
	"context"
	"fmt"
	"lerna/adapters/sqliteauth"
	"lerna/conformance"
	"os"
	"path/filepath"
)

const ID = "local-auth-v1"

func Profile(executable string) (conformance.Profile, error) {
	root, err := os.MkdirTemp("", "lerna-sqlite-version-")
	if err != nil {
		return conformance.Profile{}, err
	}
	defer os.RemoveAll(root)
	store, err := sqliteauth.Open(filepath.Join(root, "version.db"))
	if err != nil {
		return conformance.Profile{}, err
	}
	version, err := store.Version(context.Background())
	store.Close()
	if err != nil {
		return conformance.Profile{}, err
	}
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{
		"storage": "SQLite file; WAL; synchronous=FULL; isolated snapshots with compare-and-swap commit", "sqlite_runtime": version, "sqlite_driver": "modernc.org/sqlite v1.58.0", "clock": "controlled test clock anchored at 2026-09-10T00:00:00Z; no remote-time claim", "credential_ttl": "24h", "grant_max_ttl": "1h", "window_ttl": "1m", "receipt_retention": "2m", "evaluation_limits": "32 rules; 128 resources; depth 16; work 4096 (budget case:32); elapsed 1s",
	}, Method: "contractcheck -profile local-auth-v1", Limitations: []string{"Local identity and authority only; credentials stay in a trusted in-process binding.", "Controlled clock tests do not prove hostile-host or remote offline time integrity.", "Crash probes terminate separate processes at commit boundaries using disposable databases; no power-loss or filesystem-corruption claim.", "No cross-node signatures, delegation chains, single-use consumption, task control, browser login or real task execution."}}
	for _, name := range []string{"bootstrap-reopen", "missing-configuration", "continuous-use", "cross-subject", "forged-identity", "cross-namespace", "outside-resource", "outside-action", "outside-purpose", "outside-location", "hard-deny", "grant-expiry", "time-rollback", "unknown-constraint", "disabled-principal", "unsupported-single-use", "execution-is-not-issuance", "issuer-containment", "selector-set", "unregistered-tree", "evaluation-budget", "idempotency-and-conflict", "current-auth-before-receipt", "tampered-window", "window-cleanup", "concurrent-revision"} {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "local_authorization_sqlite", Input: name + " on an isolated SQLite file", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := check(ctx, name); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	for _, name := range []string{"before-commit", "lost-commit-reply", "before-cleanup", "after-cleanup"} {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "sqlite_process_recovery", Input: fmt.Sprintf("terminate child at %s; reopen database in parent and verify through SDK", name), Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if executable == "" {
				return "not verified", fmt.Errorf("crash probe executable required")
			}
			if err := crashCheck(ctx, executable, name); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	p.Cases = append(p.Cases, conformance.Case{ID: "cross-node-and-single-use", Evidence: "distributed_authorization", Availability: conformance.Unsupported, Input: "requires subsequent grant and node tickets", Expected: "separate verified profile"})
	return p, nil
}
