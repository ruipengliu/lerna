package credentialcheck

import (
	"context"
	"lerna/conformance"
)

const LifecycleProfileID = "credential-lifecycle-v1"

func LifecycleProfile() conformance.Profile {
	p := conformance.Profile{ID: LifecycleProfileID, Version: "1", Configuration: map[string]string{
		"storage":       "real SQLite atomic ciphertext/journal; independent Linux key directory and immutable private backup archive",
		"authorization": "real Harness policy and local Grant; manage and use independently checked",
		"limits":        "64 credentials; 32 rotation/backup/retirement/renewal records each; 512KiB journal; 1MiB backup; 8 keys; one record per rotation step; 3 sends and 8 lookups per renewal",
		"transport":     "fixed HTTP/1 provider; 1s calls; no proxies, redirects, connection reuse or caller trace values",
		"failure_model": "five actual child-process exits; precise Store/KeySource fault injection; independent HTTP/SQLite provider effect truth",
	}, Limitations: []string{
		"Linux local reference adapters, simulated clock and public fixture material only; no user credential, paid model or external provider is used.",
		"The HTTP provider is a concrete simulated contract, not a claim that arbitrary OAuth providers support original-operation lookup or safe replay.",
		"Lookup absence is not a terminal provider fence. Exhausted or unsafe renewals remain needs_reconciliation and retain their original credential reference.",
		"Restore quarantine is an explicit trusted authorization workflow; AEAD alone does not detect a whole valid old database or host clone. The key directory must not roll back.",
		"The profile covers the listed lifecycle requirements; CLI, archive tombstone, late-renewal concurrency and transport tracing also have separate package tests.",
	}}
	add := func(id, kind string, check func(context.Context) error) {
		p.Cases = append(p.Cases, conformance.Case{ID: id, Evidence: kind, Required: true, Input: id, Expected: "verified", Check: func(ctx context.Context) (string, error) {
			if e := check(ctx); e != nil {
				return "", e
			}
			return "verified", nil
		}})
	}
	for _, mode := range []string{"repeat-operation", "bad-material", "activation-failure", "lost-commit", "missing-old-key", "damaged-ciphertext", "backup-retirement", "archive-clone", "damaged-backup", "rollback-quarantine", "late-write"} {
		add(mode, "real_storage_authority_and_target", func(ctx context.Context) error { return LifecycleCheck(ctx, mode) })
	}
	for _, mode := range []string{"lost-response", "unknown", "not-occurred", "unsafe", "target-mismatch", "policy-revoked", "during-rotation", "provider-change"} {
		add("renewal-"+mode, "independent_http_sqlite_provider_effect", func(ctx context.Context) error { return RenewalCheck(ctx, mode) })
	}
	for _, mode := range []string{"after-prepare", "before-activate", "after-activate", "before-row", "after-row"} {
		add("process-"+mode, "actual_child_process_exit_and_recovery", func(ctx context.Context) error { return LifecycleProcessCheck(ctx, mode) })
	}
	return p
}
