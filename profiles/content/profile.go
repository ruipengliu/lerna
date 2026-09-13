package content

import (
	"context"
	"lerna/conformance"
)

const ID = "controlled-content-v1"

func Profile(executable string) (conformance.Profile, error) {
	p := conformance.Profile{ID: ID, Version: "1", Configuration: map[string]string{"storage": "SQLite shared content partition; owner-only root-confined files; Linux flock; no cross-store atomic transaction", "clock": "controlled 2026-09-10; trusted local authority time", "limits": "object 1MiB; inline 16B; total 2MiB; chunk 64KiB; 32 records; 64 files; batch 16; IO 1s; retention 1h", "scope": "local continuous authorization; trusted source configuration; no remote binding or production memory consumers"}}
	p.Limitations = []string{"Local continuous authorization and trusted source-policy fixture; no remote authentication, synchronization, or single-use business consumption.", "Linux os.Root/flock reference adapter; 1 MiB bounded upload buffer and serialized directory operations; context cannot forcibly interrupt a stuck kernel disk syscall.", "Partial-stage crash uses a fault adapter writing an actual partial staging file; other boundaries exit after real adapter/SQLite operations.", "Minimal reference/policy locator metadata retained under trusted deployment policy; no external copy or physical-media erasure claim."}
	for _, name := range names {
		p.Cases = append(p.Cases, conformance.Case{ID: name, Required: true, Evidence: "controlled_content_sqlite_files", Input: name, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := check(ctx, name); err != nil {
				return "", err
			}
			return "verified", nil
		}})
	}
	for _, point := range crashPoints {
		p.Cases = append(p.Cases, conformance.Case{ID: point, Required: true, Evidence: "content_process_recovery", Input: point + "; child exit 73; reopen original SQLite and directory", Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if err := recovery(ctx, executable, point); err != nil {
				return "", err
			}
			return "verified", nil
		}})
	}
	return p, nil
}
