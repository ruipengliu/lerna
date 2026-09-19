package control

import (
	"context"
	sqliteauth "lerna/adapters/authorization/sqlite"
	"lerna/conformance"
	"os"
	"path/filepath"
)

const ID = "task-control-v1"

func Profile(executable string) (conformance.Profile, error) {
	dir, err := os.MkdirTemp("", "lerna-control-version-")
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
		"sqlite_runtime": version, "sqlite_driver": "modernc.org/sqlite v1.58.0", "storage": "real SQLite file; WAL; synchronous=FULL; shared authorization/runtime CAS",
		"clock": "controlled 2026-09-10 with trusted watermark; local scope", "control_limits": "32 operations/task; 32 observations/task; 4 disposition checks/task; poll 10ms; stop wait 30ms; IO 1s",
		"worker_limits":  "lease 1s; renew 200ms; decision 1s; IO 1s; attempts 3; concurrent 1",
		"authorization":  "separate task.pause/task.cancel/task.resume/task.reconcile; current creator subject and resource; no administrator bypass",
		"source":         "cooperative/uncooperative scripted Brain and controlled disposition fixture; no real external actions",
		"process_faults": "five independent child processes; exit(73); each child <=10s; independent fixture source state",
		"legacy_fixture": "6395a00 runtime partition; SHA256 1e58f437139dad584501cf009e7529fea4b1ed8a462a3613386b8b7cc275898a",
	}, Limitations: []string{
		"No real external API, GUI, model or cancellation reliability claim. Effect evidence is supplied by a trusted, explicitly enabled fixture source.",
		"No remote control delivery, owner handoff, resource takeover, budget adjustment, subtask control or terminal reopening.",
		"Go cannot kill non-cooperative in-process Brain code. Late observers are tied to bounded outstanding calls; actual stop is required for applied control.",
		"Disposition adapters must verify their own facts and respect context deadlines. Process termination is not evidence of remote effect cancellation.",
		"Control and disposition records have finite retained capacity. No archive, cleanup or automatic refund of uncertain target/disposition attempts.",
		"Controlled time and process exits do not prove power-loss, hostile-host, disk-corruption or backup-rollback safety.",
	}}
	for _, name := range checks {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "durable_control_contract", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := check(ctx, name); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	for _, point := range crashPoints {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "sqlite_control_process_recovery", Input: point + "; child exit; reopen same file", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := crashCheck(ctx, executable, point); err != nil {
				return "not verified", err
			}
			return "verified", nil
		}})
	}
	return p, nil
}
