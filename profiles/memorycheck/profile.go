package memorycheck

import (
	"context"
	"lerna/adapters/sqlitememory"
	"lerna/conformance"
	"lerna/memory"
	"lerna/memory/storecontract"
	"os"
	"path/filepath"
)

const ProfileID = "long-term-memory-v1"

func Profile() conformance.Profile {
	p := conformance.Profile{ID: ProfileID, Version: "1", Configuration: map[string]string{
		"storage":       "real independent SQLite authorization and memory stores, WAL/FULL; shared replaceable Store contract",
		"authorization": "current Harness policy + Grant; P-256 JWS single-use reads; fixed trusted collection and source policy",
		"entry":         "native Protobuf Memory SDK over in-process trusted Transport",
		"limits":        "512 revisions and disclosures; 16KiB document; 512-row snapshot; 32 results; 65536 encoded record bytes; 5s service calls; 10s child timeout",
		"clock":         "fixed simulated trusted clock; no user data, external model or network provider",
	}, Limitations: []string{
		"This profile proves the listed local reference behaviors, not semantic retrieval quality, personalization effects, remote synchronization or deletion propagation.",
		"Peer metadata is simulated trusted transport input; this profile does not prove TLS enrollment or network protocol bindings.",
		"Source rules are trusted local configuration, not live external source discovery; source-unavailability and SDK-malformation checks also have separate package tests.",
		"Shared Store checks alone do not prove durability; five separate real process exits exercise authorization, commit and disclosure recovery.",
		"Restoring an old whole authority/database requires trusted quarantine; this profile does not automatically prove snapshot freshness.",
	}}
	add := func(id, evidence string, check func(context.Context) error) {
		p.Cases = append(p.Cases, conformance.Case{ID: id, Evidence: evidence, Required: true, Input: id, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := check(ctx); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, name := range []string{"fact", "preference", "inference", "experience", "schema-rejection", "residency", "revocation", "empty-binding", "budget", "exact-history", "concurrent-read", "invalid-operation"} {
		add(name, "real_sdk_sqlite_and_signed_authority", func(ctx context.Context) error { return Check(ctx, name) })
	}
	for _, name := range storecontract.Cases() {
		add("store-"+name, "shared_store_contract_on_sqlite", func(ctx context.Context) error {
			return storecontract.Check(ctx, func(context.Context) (memory.QueryStore, func() error, error) {
				root, e := os.MkdirTemp("", "memory-store-contract-")
				if e != nil {
					return nil, nil, e
				}
				s, e := sqlitememory.Open(filepath.Join(root, "memory.db"))
				if e != nil {
					os.RemoveAll(root)
					return nil, nil, e
				}
				return s, func() error { e := s.Close(); os.RemoveAll(root); return e }, nil
			}, name)
		})
	}
	for _, mode := range []string{"before-commit", "after-commit", "after-reserve", "before-binding", "after-binding"} {
		add("process-"+mode, "actual_child_exit_and_reopened_databases", func(ctx context.Context) error { return ProcessCheck(ctx, mode) })
	}
	return p
}
