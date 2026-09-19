package sqlite_test

import (
	"context"
	"testing"

	"lerna/memory"
)

func TestRestoreCleanupReportRequiresActualBoundCleanup(t *testing.T) {
	current, restored, binding, ref := restoreFixture(t)
	ctx := context.Background()
	receipt, err := current.Delete(ctx, memory.Deletion{OperationID: "delete", Subject: "operator", SemanticSHA256: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", Ref: ref, Expected: 1})
	if err != nil {
		t.Fatal(err)
	}
	target, inspection, err := restored.CleanupTarget("controlled-backup", binding)
	if err != nil {
		t.Fatal(err)
	}
	reporter, err := memory.NewDeletionReporter(current, inspection, []memory.CleanupTarget{target, {Name: "unconnected-archive", Dimension: memory.DerivedCleanup}})
	if err != nil {
		t.Fatal(err)
	}
	event := memory.SourceEvent{Ref: ref, Kind: memory.SourceDeleted, Revision: receipt.Revision, Position: receipt.Position}
	pending, err := reporter.Status(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if pending.Derived[0].State != "pending" || pending.Derived[0].Position != 0 {
		t.Fatalf("backup reported cleanup before reconciliation: %+v", pending)
	}
	if _, err = restored.InspectRecovery(ctx, binding.Scope); err != memory.Missing {
		t.Fatalf("status inspection performed cleanup: %v", err)
	}
	if _, err = restored.Reconcile(ctx, binding); err != nil {
		t.Fatal(err)
	}
	done, err := reporter.Status(ctx, event)
	if err != nil {
		t.Fatal(err)
	}
	if done.Derived[0].State != "applied" || done.Derived[0].Position != 2 || done.Derived[1].State != "not_covered" || done.Replicas[0].State != "not_covered" || done.Local[0].State != "not_covered" {
		t.Fatalf("backup cleanup overstated its scope: %+v", done)
	}
	changed := *target.Binding
	changed.ConfigSHA256 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if _, err = inspection.InspectConsumer(ctx, changed); err != memory.IdentityConflict {
		t.Fatalf("unbound target accepted: %v", err)
	}
	// Independently replacing trust must not reuse the former trust's completion.
	replacement := recoveryBinding(t, current, 2)
	replaced, inspector, err := restored.CleanupTarget("controlled-backup", replacement)
	if err != nil {
		t.Fatal(err)
	}
	if replaced.Binding.ConfigSHA256 == target.Binding.ConfigSHA256 {
		t.Fatal("target omitted effective trust configuration")
	}
	if _, err = inspector.InspectConsumer(ctx, *replaced.Binding); err != memory.Missing {
		t.Fatalf("changed trust reused prior completion: %v", err)
	}
}
