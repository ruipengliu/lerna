package catalogcheck

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	sqlitecontext "lerna/adapters/context/sqlite"
	"lerna/adapters/execution/simworkflow"
	"lerna/contextassembly"
	"lerna/memory"
	"lerna/tasks"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestLegacyActionCheckpointMigratesWithoutReplacingWork(t *testing.T) {
	checkLegacyActionCheckpoint(t, "live")
}

func TestRetiredLegacyActionCheckpointPreservesOriginalEffect(t *testing.T) {
	checkLegacyActionCheckpoint(t, "retired")
}

func TestTamperedLegacyActionComparisonCannotMigrateOrStart(t *testing.T) {
	checkLegacyActionCheckpoint(t, "tampered")
}

func TestActionHostMigratesRetiredLegacyCheckpointAutomatically(t *testing.T) {
	checkLegacyActionCheckpoint(t, "background-retired")
}

func checkLegacyActionCheckpoint(t *testing.T, mode string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
	point := "reassembled-invoked"
	retired := mode == "retired" || mode == "background-retired"
	if retired {
		point = "reassembled-effect"
	}
	child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT="+point)
	output, err := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatalf("checkpoint: %v %s", err, output)
	}
	cp, err := readActionContext(root)
	if err != nil {
		t.Fatal(err)
	}
	store, err := sqlitecontext.Open(filepath.Join(root, cp.Namespace, "context.db"))
	if err != nil {
		t.Fatal(err)
	}
	cp.ContextDigests = make(map[uint32][32]byte)
	for _, n := range cp.BoundContexts {
		key := cp.Binding.Key
		key.Decision = uint64(n)
		snapshot, err := store.Read(ctx, key)
		if err != nil {
			store.Close()
			t.Fatal(err)
		}
		cp.ContextDigests[n] = sha256.Sum256(snapshot.Document)
		if retired {
			if err = store.Retire(ctx, key); err != nil {
				store.Close()
				t.Fatal(err)
			}
		}
	}
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
	digest := cp.ContextDigests[uint32(cp.Binding.Key.Decision)]
	if mode == "tampered" {
		digest[0] ^= 1
	}
	cp.Format, cp.BoundContexts, cp.SnapshotSHA = 1, nil, &digest
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "action-context.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if mode == "background-retired" {
		observeActionCheckpointMaintenance(t, ctx, root, cp)
	}
	var report ActionRecoveryReport
	if retired {
		report, err = RestorePersonalizedInvocation(ctx, root, false)
	} else {
		report, err = RestorePersonalizedTask(ctx, root)
	}
	if mode == "tampered" {
		if err == nil {
			t.Fatal("tampered legacy digest accepted")
		}
		unchanged, e := os.ReadFile(filepath.Join(root, "action-context.json"))
		if e != nil || !bytes.Equal(unchanged, raw) {
			t.Fatal("invalid comparison erased without verification")
		}
		defs, e := simworkflow.Definitions()
		if e != nil {
			t.Fatal(e)
		}
		h, e := openRuntime(ctx, filepath.Join(root, cp.Namespace), cp.Token, defs[0], false)
		if e != nil {
			t.Fatal(e)
		}
		defer h.close()
		h.clock.advance(cp.Now.Sub(h.now()))
		invocation, e := h.services[cp.Request.DescriptorSHA256].GetInvocation(ctx, cp.Request.OperationID)
		if e != nil {
			t.Fatal(e)
		}
		target, e := h.target.Snapshot(ctx, cp.SelectedRecord)
		if e != nil {
			t.Fatal(e)
		}
		if invocation.Started || target.State != "submitted" || target.Ledger != 1000 {
			t.Fatal("tampered checkpoint changed the original target")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	if report.ModelCalls != 0 || report.Ledger != 995 || report.ReadAllocated != 2 || !report.OriginalIdentity {
		t.Fatalf("migration replaced original work: %+v", report)
	}
	if retired {
		if !report.WasStarted || !report.Started || report.Effect != "CONFIRMED" {
			t.Fatalf("migration lost original effect: %+v", report)
		}
		store, e := sqlitecontext.Open(filepath.Join(root, cp.Namespace, "context.db"))
		if e != nil {
			t.Fatal(e)
		}
		defer store.Close()
		if e = store.VerifyCheckpoint(ctx, cp.Binding.Key); e != contextassembly.Invalidated {
			t.Fatalf("migration resurrected context: %v", e)
		}
	} else if report.TaskState != "COMPLETED" || report.ContextSnapshots != 2 {
		t.Fatalf("migration lost original completion: %+v", report)
	}
	migrated, err := os.ReadFile(filepath.Join(root, "action-context.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(migrated, []byte(`"SnapshotSHA"`)) || bytes.Contains(migrated, []byte(`"ContextDigests"`)) {
		t.Fatal("legacy action file retained body comparisons")
	}
}

func observeActionCheckpointMaintenance(t *testing.T, ctx context.Context, root string, cp actionContextCheckpoint) {
	t.Helper()
	defs, err := simworkflow.Definitions()
	if err != nil {
		t.Fatal(err)
	}
	h, err := openRuntime(ctx, filepath.Join(root, cp.Namespace), cp.Token, defs[0], false)
	if err != nil {
		t.Fatal(err)
	}
	defer h.close()
	h.clock.advance(cp.Now.Sub(h.now()))
	port, err := h.work.Actions(tasks.ActionLimits{MaxOperations: 12, MaxQueries: 128, MaxCorrections: 2, InputTokens: (&actionScript{}).Capabilities().InputUpper, OutputTokens: 1024})
	if err != nil {
		t.Fatal(err)
	}
	before, err := h.core.Load(ctx, cp.Baseline.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	a := &actionHost{h: h, f: &fixture{root: root}, location: cp.Location}
	p, err := a.personalizeBindings(ctx, port, cp.Baseline, "", false, &cp.Binding, false)
	if err != nil {
		t.Fatal(err)
	}
	defer p.close()
	// Only the host lifecycle is running; no Restore or migration method is called.
	wait, stop := context.WithTimeout(ctx, 5*time.Second)
	defer stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		got, err := readActionContext(root)
		if err != nil {
			t.Fatal(err)
		}
		if got.Format == 2 {
			if got.SnapshotSHA != nil || len(got.ContextDigests) != 0 {
				t.Fatal("host retained comparison")
			}
			break
		}
		select {
		case <-wait.Done():
			t.Fatal("host did not migrate retired legacy checkpoint")
		case <-tick.C:
		}
	}

	for {
		position, e := p.checkpoints.InspectConsumer(ctx, p.checkpointBinding)
		if e == nil && position == 2 {
			break
		}
		if e != nil && e != memory.Missing {
			t.Fatal(e)
		}
		select {
		case <-wait.Done():
			t.Fatalf("checkpoint history not confirmed: %d %v", position, e)
		case <-tick.C:
		}
	}
	if err = p.cleanup.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(cp)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, "action-context.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = p.checkpoints.InspectConsumer(ctx, p.checkpointBinding); err != memory.Missing {
		t.Fatalf("late action checkpoint reused prior completion: %v", err)
	}
	if result, err := p.checkpoints.Run(ctx); err != nil || result.Position != 2 || result.Applied != 0 {
		t.Fatalf("late action checkpoint replaced original events: %+v %v", result, err)
	}
	if position, err := p.checkpoints.InspectConsumer(ctx, p.checkpointBinding); err != nil || position != 2 {
		t.Fatalf("late action checkpoint not confirmed: %d %v", position, err)
	}
	after, err := h.core.Load(ctx, cp.Baseline.Task.Ref)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatal("maintenance changed original Core facts")
	}
}

func TestActionCheckpointDoesNotDuplicateSnapshotDigests(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	root := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	child := exec.CommandContext(ctx, executable, "-test.run=^TestPersonalizedActionCrashProbe$")
	child.Env = append(os.Environ(), "HARNESS_ACTION_CONTEXT_PROBE="+root, "HARNESS_ACTION_CONTEXT_POINT=reassembled-invoked")
	output, err := child.CombinedOutput()
	var exited *exec.ExitError
	if !errors.As(err, &exited) || exited.ExitCode() != 73 {
		t.Fatalf("checkpoint: %v %s", err, output)
	}
	raw, err := os.ReadFile(filepath.Join(root, "action-context.json"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte(`"SnapshotSHA"`)) || bytes.Contains(raw, []byte(`"ContextDigests"`)) {
		t.Fatal("checkpoint retained original body comparisons")
	}
	report, err := RestorePersonalizedTask(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if report.TaskState != "COMPLETED" || report.ModelCalls != 0 || report.Ledger != 995 || report.ReadAllocated != 2 || report.ContextSnapshots != 2 || !report.OriginalIdentity {
		t.Fatalf("recovery changed original action: %+v", report)
	}
}
