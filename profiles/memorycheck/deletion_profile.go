package memorycheck

import (
	"context"
	"fmt"
	"lerna/conformance"
)

const DeletionProfileID = "memory-deletion-v1"

func DeletionProfile() conformance.Profile {
	return conformance.Profile{ID: DeletionProfileID, Version: "1", Configuration: map[string]string{
		"entry":    "Memory SDK DELETE, LOOKUP, DELETION_STATUS and recovered QUERY/GET over authenticated local Protobuf transport",
		"storage":  "real independent SQLite Memory and authorization stores, reopened after cleanup",
		"cleanup":  "bounded cleanup worker sweeps fixed admission-comparisons consumer; events applied before durable acknowledgment",
		"limits":   "30s case; 5s service calls; 16-event cleanup batches; 512 retained revisions plus tombstones; 512 disclosure bindings; 512 operations per proof page; no external services",
		"recovery": "explicit private SQLite backup quarantine; current source and independently pinned Ed25519 authority, epoch, scope and minimum position; signed proof v2",
		"data":     "public synthetic preference; simulated trusted clock; no real model calls",
	}, Limitations: []string{
		"This profile covers local deletion, original receipt minimization, read release races, admission-comparison cleanup and governed backup recovery; it is not full ticket 19 acceptance.",
		"The local applied target means admission-comparisons only, not all local payloads. Replicas and derived/archive targets remain explicitly not_covered.",
		"The backup case uses actual pre-deletion data and a live current source with independently pinned trust; restart is orderly. Process-crash recovery is tested separately and is not claimed by this profile.",
	}, Cases: []conformance.Case{{ID: "sdk-delete-status-cleanup-reopen", Required: true, Evidence: "actual_sdk_authority_sqlite_consumer_and_reopen", Input: "create/read/delete/replay/cleanup/reopen/status", Expected: "authority=committed;admission-comparisons=applied@2;replicas=not_covered;derived=not_covered", Check: func(ctx context.Context) (string, error) {
		out, err := CheckDeletion(ctx)
		if err != nil {
			return "", err
		}
		r := out.Report
		return fmt.Sprintf("authority=%s;admission-comparisons=%s@%d;replicas=%s;derived=%s", r.Authority, r.Local[0].State, r.Local[0].Position, r.Replicas[0].State, r.DerivedArchives[0].State), nil
	}}, {ID: "deletion-after-record-load", Required: true, Evidence: "actual_sdk_signed_read_and_delete_at_store_read_boundary", Input: "original query bound; real record loaded; source deleted before release", Expected: "old body denied; original query binding retained", Check: func(ctx context.Context) (string, error) {
		if err := CheckDeletionReadRace(ctx); err != nil {
			return "", err
		}
		return "old body denied; original query binding retained", nil
	}}, {ID: "deletion-of-coverage-witness", Required: true, Evidence: "actual_sdk_signed_read_and_deleted_budget_witness", Input: "zero body query; loaded budget witness deleted before disclosure", Expected: "stale coverage denied; original query binding retained", Check: func(ctx context.Context) (string, error) {
		if err := CheckDeletionCoverageRace(ctx); err != nil {
			return "", err
		}
		return "stale coverage denied; original query binding retained", nil

	}}, {ID: "sdk-backup-recovery-governance", Required: true, Evidence: "actual_pre_delete_backup_live_authority_proofs_sdk_residency_revocation_and_reopen", Input: "copy real database; delete at current source; reconcile; query; enforce residency; reopen; revoke", Expected: "backup=sanitized@3;retained=1;missing=0;runtime=quarantined;current-policy=enforced;backup-report=applied@3;other-targets=not_covered", Check: func(ctx context.Context) (string, error) {
		p, err := CheckRecovery(ctx)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("backup=%s@%d;retained=%d;missing=%d;runtime=quarantined;current-policy=enforced;backup-report=applied@3;other-targets=not_covered", p.State, p.AppliedPosition, p.Retained, p.Missing), nil
	}}}}

}
