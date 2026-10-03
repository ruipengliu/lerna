//go:build integration

package recovery_test

import (
	"context"
	"errors"
	"os/exec"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
)

// Both adapters execute the same process crash script at the approved Host
// admission, public command.get and Host observation seams.
func TestProcessCrashAdmissionRecovery(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			for _, gate := range []string{"writes_staged_before_commit", "commit_confirmed_before_host_reply"} {
				t.Run(gate, func(t *testing.T) {
					t.Run("SIGKILL", func(t *testing.T) { runProcessAdmission(t, backend, gate, true) })
					t.Run("NormalReply", func(t *testing.T) { runProcessAdmission(t, backend, gate, false) })
				})
			}
		})
	}
}
func runProcessAdmission(t *testing.T, backend, gate string, crash bool) {
	t.Helper()
	cfg := processFixture(t, backend, gate)
	cfg.Gate = gate
	cfg.Raw = command("process-original", "input", "hello", nil, "2026-10-03T02:00:00.000000Z")
	child := startHostProcess(t, cfg)
	child.event(t, gate)
	var receipt contract.CommandReceipt
	if crash {
		child.kill(t)
	} else {
		child.send(t, "release", nil, nil)
		var reply processFrame
		if err := readProcessFrame(child.ctx, child.reply, &reply); err != nil {
			t.Fatal(err)
		}
		if reply.Scenario != cfg.Scenario || reply.Stage != "host_reply" || reply.Generation != cfg.Generation || !reply.Now.Equal(cfg.Now) || reply.Outcome == nil {
			t.Fatal("invalid business reply frame")
		}
		receipt = assertReceived(t, *reply.Outcome, nil)
		child.wait(t, false)
	}
	// SIGKILL+Wait or a clean Close has released SQLite's unique writer before
	// this independent Store opens. No private table or second writer is used.
	_, h, worker := processObserver(t, cfg)
	ctx := contextFor(t)
	result, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "process-original"}), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if crash && gate == "writes_staged_before_commit" {
		if _, ok := result.AsNotFound(); !ok {
			t.Fatal("definitely uncommitted command acquired a receipt")
		}
		// The exact original creation must still be its first application. Partial
		// input/Job leakage would violate its creation/Trigger preconditions.
		out, err := h.Record(ctx, cfg.Raw, &principal)
		receipt = assertReceived(t, out, err)
	} else {
		found, ok := result.AsFound()
		if !ok {
			t.Fatal("confirmed commit was lost with the Host reply")
		}
		if !crash {
			assertReceiptSame(t, receipt, found.Receipt)
		}
		receipt = found.Receipt
	}
	applied, ok := receipt.AsApplied()
	if !ok || applied.CommandRef != (contract.CommandRef{Owner: owner, CommandID: "process-original"}) || applied.Revision != "1" || applied.ObjectRef.ID != "input" {
		t.Fatal("original creation was not applied at revision1")
	}
	before, err := h.Observe(ctx, "input", &principal)
	if err != nil {
		t.Fatal(err)
	}
	if before.Input.ID != "input" || before.Input.Text != "hello" || before.Input.Revision != 1 || before.Job.ID == "" || before.Job.WorkRevision != 1 || before.Job.CompletedRevision != 0 || before.Job.State != "ready" || before.Projection != nil {
		t.Fatalf("incorrect recovered admission: %+v", before)
	}
	out, err := h.Record(ctx, cfg.Raw, &principal)
	assertReceiptSame(t, receipt, assertReceived(t, out, err))
	after, err := h.Observe(ctx, "input", &principal)
	if err != nil || after.Job.ID != before.Job.ID || after.Input.Revision != 1 || after.Job.WorkRevision != 1 {
		t.Fatalf("original replay duplicated responsibility: %+v %v", after, err)
	}
	// Scanning all responsibilities through the real worker also rules out an
	// additional object/stage Job invisible to the single-object observation.
	batch, err := worker.Claim(ctx, "recovery-observer", 64, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Claim.JobID != before.Job.ID {
		t.Fatalf("recovery did not retain one original Job: %+v %v", batch, err)
	}
	t.Logf("recovered backend=%s gate=%s crash=%t original=process-original object=input revision=1 job=%s fixed=applied", backend, gate, crash, before.Job.ID)
}

func TestSQLiteStoragePortConfirmationLossRecoversOriginal(t *testing.T) {
	for _, lose := range []bool{true, false} {
		name := "NormalConfirmation"
		if lose {
			name = "RealCommitStoragePortConfirmationLoss"
		}
		t.Run(name, func(t *testing.T) {
			cfg := processFixture(t, "sqlite", name)
			store, h, _ := processObserver(t, cfg)
			if lose {
				h.Runner = storagePortConfirmationLoss{TxRunner: store}
			}
			original := command("storage-port-original", "input", "hello", nil, "2026-10-03T02:00:00.000000Z")
			out, err := h.Record(contextFor(t), original, &principal)
			if err != nil {
				t.Fatal(err)
			}
			var normal contract.CommandReceipt
			if lose {
				unknown, ok := out.AsCommitUnknown()
				if !ok || unknown.CommandRef != (contract.CommandRef{Owner: owner, CommandID: "storage-port-original"}) || unknown.NextAction != "query_or_retransmit_original" {
					t.Fatal("real Host lost uncertainty or original identity")
				}
				if _, err := contract.Encode(out); err != nil {
					t.Fatal(err)
				}
			} else {
				normal = assertReceived(t, out, nil)
			}
			// Actual callback errors remain certain errors; the decorator only
			// withholds a successful transaction confirmation.
			_, conflict := h.Record(contextFor(t), command("storage-port-original", "input", "changed", nil, "2026-10-03T02:00:00.000000Z"), &principal)
			assertReason(t, conflict, "idempotency_conflict")
			if err := store.Close(); err != nil {
				t.Fatal(err)
			}
			_, replacement, worker := processObserver(t, cfg)
			ctx := contextFor(t)
			result, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "storage-port-original"}), &principal, replacement.Permissions, replacement, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			found, ok := result.AsFound()
			if !ok {
				t.Fatal("real storage-port commit was not recoverable")
			}
			if applied, ok := found.Receipt.AsApplied(); !ok || applied.Revision != "1" {
				t.Fatal("storage-port confirmation loss changed original creation")
			}
			if !lose {
				assertReceiptSame(t, normal, found.Receipt)
			}
			before, err := replacement.Observe(ctx, "input", &principal)
			if err != nil || before.Input.Text != "hello" || before.Input.Revision != 1 || before.Job.WorkRevision != 1 || before.Job.CompletedRevision != 0 || before.Job.State != "ready" {
				t.Fatalf("real commit lost facts: %+v %v", before, err)
			}
			replay, err := replacement.Record(ctx, original, &principal)
			assertReceiptSame(t, found.Receipt, assertReceived(t, replay, err))
			after, err := replacement.Observe(ctx, "input", &principal)
			if err != nil || after.Input.Revision != 1 || after.Job.ID != before.Job.ID || after.Job.WorkRevision != 1 {
				t.Fatalf("storage-port replay duplicated responsibility: %+v %v", after, err)
			}
			batch, err := worker.Claim(ctx, "normal-recovery", 64, time.Minute)
			if err != nil || len(batch) != 1 || batch[0].Claim.JobID != before.Job.ID {
				t.Fatalf("not one original Job: %+v %v", batch, err)
			}
			startWork(t, worker, batch[0])
			if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
				t.Fatal(err)
			}
			t.Logf("SQLite boundary=real-commit/storage-port-confirmation lose=%t original=storage-port-original revision=1 job=%s; native Commit error branch not covered", lose, before.Job.ID)
		})
	}
}

func TestProcessClaimTakeoverRejectsBufferedOldCompletion(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			cfg := processFixture(t, backend, "claim-takeover")
			cfg.Gate = "worker"
			seedProcessInput(t, cfg)
			old := startHostProcess(t, cfg)
			message := old.event(t, "work_computed")
			assertProcessWork(t, message, 1, 1)
			old.kill(t)
			cfg.Generation = 2
			// The fixture owner advances to its independently fixed T1. The
			// worker message is checked against T0+lease, never trusted as now.
			if !message.Work.Claim.LeaseUntil.Equal(cfg.Now.Add(time.Minute)) {
				t.Fatal("worker lease differs from owner fixture clock")
			}
			cfg.Now = cfg.Now.Add(time.Minute)
			one := contract.Revision("1")
			cfg.Raw = command("process-update", "input", "world", &one, "2026-10-03T02:00:00.000000Z")
			successor := startHostProcess(t, cfg)
			current := successor.event(t, "work_computed")
			assertProcessWork(t, current, 2, 2)
			if current.Work.Claim.JobID != message.Work.Claim.JobID {
				t.Fatal("takeover changed Job identity")
			}
			successor.send(t, "complete", message.Work, message.Projection)
			rejected := successor.event(t, "late_completion_rejected")
			observed := rejected.Observation
			if observed == nil || observed.Input.Revision != 2 || observed.Job.ID != message.Work.Claim.JobID || observed.Job.WorkRevision != 2 || observed.Job.CompletedRevision != 0 || observed.Job.State != "leased" || observed.Projection != nil {
				t.Fatalf("late message changed successor facts: %+v", observed)
			}
			successor.send(t, "complete_current", current.Work, current.Projection)
			successor.event(t, "completed")
			successor.wait(t, false)
			_, h, worker := processObserver(t, cfg)
			observedAfter, err := h.Observe(contextFor(t), "input", &principal)
			if err != nil || observedAfter.Input.Revision != 2 || observedAfter.Input.Text != "world" || observedAfter.Job.ID != message.Work.Claim.JobID || observedAfter.Job.WorkRevision != 2 || observedAfter.Job.CompletedRevision != 2 || observedAfter.Job.State != "done" || observedAfter.Projection == nil || observedAfter.Projection.InputRevision != 2 || observedAfter.Projection.TextDigest != "sha256:486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7" {
				t.Fatalf("successor lost new progress: %+v %v", observedAfter, err)
			}
			assertProcessOriginalReceipts(t, h, true)
			batch, err := worker.Claim(contextFor(t), "after-successor", 64, time.Minute)
			if err != nil || len(batch) != 0 {
				t.Fatalf("done original has extra responsibility: %+v %v", batch, err)
			}
		})
	}
}
func seedProcessInput(t *testing.T, cfg processConfig) {
	t.Helper()
	store, h, _ := processObserver(t, cfg)
	out, err := h.Record(contextFor(t), command("process-source", "input", "hello", nil, "2026-10-03T02:00:00.000000Z"), &principal)
	assertReceived(t, out, err)
	if err = store.Close(); err != nil {
		t.Fatal(err)
	}
}
func assertProcessWork(t *testing.T, message processFrame, revision, epoch int64) {
	t.Helper()
	if message.Work == nil || message.Projection == nil {
		t.Fatal("worker did not deliver actual computation")
	}
	work := message.Work
	text, digest := "hello", "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if revision == 2 {
		text, digest = "world", "sha256:486ea46224d1bb4fb680f34f7c9ad96a8f24ec88be73ea8e5a6c65260e9cb8a7"
	}
	if work.Input.ID != "input" || work.Input.Text != text || work.Input.Revision != revision || work.Claim.ClaimedRevision != revision || work.Claim.Epoch != epoch || work.Claim.Object.ID != "input" || work.Claim.Phase != "project" || message.Projection.InputRevision != revision || message.Projection.TextDigest != digest {
		t.Fatalf("incorrect real worker message: %+v", message)
	}
}

// This paired control uses the same real child Claim/Project/message transport
// while the old Claim is still valid, without a replacement epoch.
func TestProcessClaimNormalCompletion(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			cfg := processFixture(t, backend, "claim-normal")
			cfg.Gate = "worker"
			seedProcessInput(t, cfg)
			child := startHostProcess(t, cfg)
			message := child.event(t, "work_computed")
			assertProcessWork(t, message, 1, 1)
			child.send(t, "complete", message.Work, message.Projection)
			child.event(t, "completed")
			child.wait(t, false)
			_, h, _ := processObserver(t, cfg)
			observed, err := h.Observe(contextFor(t), "input", &principal)
			if err != nil || observed.Input.Revision != 1 || observed.Job.ID != message.Work.Claim.JobID || observed.Job.CompletedRevision != 1 || observed.Job.State != "done" || observed.Projection == nil || observed.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
				t.Fatalf("valid message did not complete: %+v %v", observed, err)
			}
			assertProcessOriginalReceipts(t, h, false)
		})
	}
}

// A's computation is transported before A is killed. A current independent
// Host consumes that buffered message at the still-valid T0, so this makes no
// claim that a killed process executes again. Both real commit orders retain
// revision2 on the original Job.
func TestProcessBufferedOldRevisionAndNewTriggerBothOrders(t *testing.T) {
	for _, backend := range []string{"postgres", "sqlite"} {
		t.Run(backend, func(t *testing.T) {
			for _, triggerFirst := range []bool{true, false} {
				name := "completion-first"
				if triggerFirst {
					name = "new-work-first"
				}
				t.Run(name, func(t *testing.T) {
					cfg := processFixture(t, backend, name)
					cfg.Gate = "worker"
					seedProcessInput(t, cfg)
					child := startHostProcess(t, cfg)
					message := child.event(t, "work_computed")
					assertProcessWork(t, message, 1, 1)
					child.kill(t)
					_, h, worker := processObserver(t, cfg)
					ctx := contextFor(t)
					trigger := func() {
						one := contract.Revision("1")
						out, err := h.Record(ctx, command("process-update", "input", "world", &one, "2026-10-03T02:00:00.000000Z"), &principal)
						if applied, ok := assertReceived(t, out, err).AsApplied(); !ok || applied.Revision != "2" {
							t.Fatal("new trigger did not advance revision")
						}
					}
					if triggerFirst {
						trigger()
					}
					if err := worker.Complete(ctx, message.Work.Claim, *message.Projection); err != nil {
						t.Fatal(err)
					}
					if !triggerFirst {
						trigger()
					}
					observed, err := h.Observe(ctx, "input", &principal)
					if err != nil || observed.Input.Revision != 2 || observed.Input.Text != "world" || observed.Job.ID != message.Work.Claim.JobID || observed.Job.WorkRevision != 2 || observed.Job.CompletedRevision != 1 || observed.Job.State != "ready" || observed.Projection == nil || observed.Projection.InputRevision != 1 || observed.Projection.TextDigest != "sha256:2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824" {
						t.Fatalf("old completion erased new work: %+v %v", observed, err)
					}
					batch, err := worker.Claim(ctx, "new-revision", 64, time.Minute)
					if err != nil || len(batch) != 1 || batch[0].Claim.JobID != message.Work.Claim.JobID || batch[0].Claim.ClaimedRevision != 2 || batch[0].Claim.Epoch != 2 {
						t.Fatalf("new revision not reclaimable: %+v %v", batch, err)
					}
					startWork(t, worker, batch[0])
					current := processFrame{Work: &batch[0]}
					result := durablework.Project(batch[0])
					current.Projection = &result
					assertProcessWork(t, current, 2, 2)
					if err := worker.Complete(ctx, batch[0].Claim, result); err != nil {
						t.Fatal(err)
					}
					final, err := h.Observe(ctx, "input", &principal)
					if err != nil || final.Job.ID != message.Work.Claim.JobID || final.Job.WorkRevision != 2 || final.Job.CompletedRevision != 2 || final.Job.State != "done" || final.Projection == nil || *final.Projection != result {
						t.Fatalf("new revision did not complete: %+v %v", final, err)
					}
					assertProcessOriginalReceipts(t, h, true)
				})
			}
		})
	}
}
func assertProcessOriginalReceipts(t *testing.T, h *durablework.Host, updated bool) {
	t.Helper()
	ids := []contract.ID{"process-source"}
	if updated {
		ids = append(ids, "process-update")
	}
	for index, id := range ids {
		ctx := contextFor(t)
		result, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: id}), &principal, h.Permissions, h, time.Now)
		if err != nil {
			t.Fatal(err)
		}
		found, ok := result.AsFound()
		if !ok {
			t.Fatal("worker handoff lost original fixed receipt")
		}
		revision := contract.Revision("1")
		if index == 1 {
			revision = "2"
		}
		if applied, ok := found.Receipt.AsApplied(); !ok || applied.Revision != revision || applied.ObjectRef.ID != "input" {
			t.Fatal("handoff changed original decision")
		}
	}
}

// The child must stop a blocked inherited pipe at the real transaction
// deadline, independently of the parent's longer process kill deadline.
func TestProcessHarnessStopsBlockedPipeAtTransactionDeadline(t *testing.T) {
	cfg := processFixture(t, "sqlite", "blocked-precommit-pipe")
	cfg.Gate = "writes_staged_before_commit"
	cfg.Raw = command("bounded-original", "input", "hello", nil, "2026-10-03T02:00:00.000000Z")
	child := startHostProcess(t, cfg)
	child.event(t, cfg.Gate)
	physical, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	select {
	case err := <-child.done:
		child.waited = true
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			t.Fatalf("blocked child did not fail at transaction deadline: %v %s", err, child.output.String())
		}
	case <-physical.Done():
		t.Fatal("child pipe ignored finite transaction context")
	}
	_, h, _ := processObserver(t, cfg)
	result, err := contract.GetCommand(contextFor(t), readWire(contract.CommandRef{Owner: owner, CommandID: "bounded-original"}), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.AsNotFound(); !ok {
		t.Fatal("timed-out blocked callback acquired fixed receipt")
	}
	// The released writer can still perform the exact original normal control.
	out, err := h.Record(contextFor(t), cfg.Raw, &principal)
	if applied, ok := assertReceived(t, out, err).AsApplied(); !ok || applied.Revision != "1" {
		t.Fatal("blocked-pipe timeout lost original control")
	}
}
