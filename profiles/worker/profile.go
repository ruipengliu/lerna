package worker

import (
	"context"
	"lerna/adapters/sqliteauth"
	"lerna/conformance"
	"os"
	"path/filepath"
)

const ID = "bounded-worker-v1"

func Profile(executable string) (conformance.Profile, error) {
	dir, err := os.MkdirTemp("", "lerna-worker-version-")
	if err != nil {
		return conformance.Profile{}, err
	}
	defer os.RemoveAll(dir)
	db, err := sqliteauth.Open(filepath.Join(dir, "version.db"))
	if err != nil {
		return conformance.Profile{}, err
	}
	version, err := db.Version(context.Background())
	db.Close()
	if err != nil {
		return conformance.Profile{}, err
	}
	p := conformance.Profile{ID: ID, Version: "1", Method: "contractcheck -profile " + ID, Configuration: map[string]string{
		"sqlite_runtime": version, "sqlite_driver": "modernc.org/sqlite v1.58.0", "storage": "real file; WAL; synchronous=FULL; shared authorization/runtime CAS",
		"clock": "controlled 2026-09-10; monotonic watermark; local trust only", "worker_limits": "lease 1s; renew 200ms; decision 1s; IO 1s; 3 attempts; 1 concurrent call per runner; 256 commits/task",
		"task_limits":    "100 tasks; page 10; 8 MiB runtime; 16 MiB combined snapshot; 64 KiB result; deadline 1m; test steps 1/3/8",
		"authorization":  "explicit task.execute continuous grant; creator subject; resource root; purpose task; location local; no administrator bypass",
		"timeout_case":   "20ms decision; deliberately non-cooperative in-process Brain retains its concurrency slot until return",
		"process_faults": "four disposable child exit(73) boundaries; each child limited to 10s",
	}, Limitations: []string{
		"Scripted Brain only; no actual model, external effects, GUI or answer-quality evidence.",
		"Temporary runtime fixture verifies common task behavior only; fixed identity is not a second authorization or durable backend.",
		"A Go goroutine cannot be forcibly killed: a non-cooperative Brain retains its runner slot; bounded caller return and rejection of late output are verified, not process isolation.",
		"Storage and wakeup implementations must honor context cancellation. Limits are per runner; deploy a bounded number of runners under one logical owner.",
		"WAITING has no public resume or budget-adjustment API in this slice. Records retained within finite capacity; no archive/cleanup.",
		"Process termination and controlled time do not prove power-loss, corruption or hostile-host resilience. No remote worker or owner failover.",
	}}
	for _, name := range checks {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "bounded_worker_contract", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := check(ctx, name); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	for _, point := range crashPoints {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "sqlite_worker_process_recovery", Input: "child exit at " + point + "; reopen original file", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := crashCheck(ctx, executable, point); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	return p, nil
}
