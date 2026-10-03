//go:build integration

package recovery_test

import (
	"context"
	"fmt"
	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/host/durablework"
)

func TestPGBoundedScanSkipsHeldObjectAndRetainsAllResponsibility(t *testing.T) {
	store := database(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	for i := 0; i < 5; i++ {
		id := fmt.Sprintf("input-%d", i)
		out, err := h.Record(ctx, command(id, id, "hello", nil, future()), &principal)
		assertReceived(t, out, err)
	}
	worker := durablework.NewWorker(owner, store, store, store, store)
	ready := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			if _, err := store.LockInput(ctx, tx, owner, "input-0"); err != nil {
				return err
			}
			close(ready)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	awaitStage(t, ctx, ready)
	batch, err := worker.Claim(ctx, "bounded", 1, time.Minute)
	if err != nil || len(batch) != 0 {
		t.Fatalf("scan exceeded single held candidate: %+v %v", batch, err)
	}
	batch, err = worker.Claim(ctx, "bounded", 2, time.Minute)
	if err != nil || len(batch) != 1 || batch[0].Input.ID != "input-1" {
		t.Fatalf("held object blocked normal candidate: %+v %v", batch, err)
	}
	close(release)
	if err = <-done; err != nil {
		t.Fatal(err)
	}
	seen := map[contract.ID]bool{}
	for len(batch) > 0 {
		for _, work := range batch {
			if seen[work.Claim.JobID] {
				t.Fatal("duplicated claim")
			}
			seen[work.Claim.JobID] = true
			if err = worker.Complete(ctx, work.Claim, durablework.Project(work)); err != nil {
				t.Fatal(err)
			}
		}
		batch, err = worker.Claim(ctx, "bounded", 2, time.Minute)
		if err != nil || len(batch) > 2 {
			t.Fatalf("unbounded claim: %d %v", len(batch), err)
		}
	}
	if len(seen) != 5 {
		t.Fatalf("lost responsibility: %d", len(seen))
	}
	for i := 0; i < 5; i++ {
		got, err := h.Observe(ctx, contract.ID(fmt.Sprintf("input-%d", i)), &principal)
		if err != nil || got.Job.CompletedRevision != 1 || got.Input.Revision != 1 || got.Projection.InputRevision != 1 {
			t.Fatalf("normal projection: %+v %v", got, err)
		}
	}
}

func TestPGClaimV2MigrationPreservesV1AndReportsExactArtifacts(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	versions, err := store.MigrationVersions(ctx)
	if err != nil || len(versions) != 3 || versions[0].Version != 1 || versions[0].Checksum != "sha256:f8d04d373b039a425b4f6d0a7b7dd4410971c00faf91cdba3f68a9204579127e" || versions[1].Version != 2 || versions[1].Checksum != "sha256:cdb7dea9f55ee8ac9201a943cecf8096108b48bc208409e1372bbf17295b2297" || versions[2].Version != 3 || versions[2].Checksum != postgres.MigrationV3Checksum() {
		t.Fatalf("v1/v2 metadata: %+v %v", versions, err)
	}
	t.Logf("applied migrations: %+v", versions)
	h := hostFor(store, owner, principal)
	out, err := h.Record(ctx, command("source", "input", "hello", nil, future()), &principal)
	assertReceived(t, out, err)
	adapter := reopen(t, store)
	worker := durablework.NewWorker(owner, adapter, adapter, adapter, adapter)
	batch, err := worker.Claim(ctx, "v2", 1, time.Minute)
	if err != nil || len(batch) != 1 {
		t.Fatalf("actual v2 claim: %+v %v", batch, err)
	}
	if err = worker.Complete(ctx, batch[0].Claim, durablework.Project(batch[0])); err != nil {
		t.Fatal(err)
	}
}
