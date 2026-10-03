//go:build integration

package recovery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/runtime"
)

func forOwner(data []byte, o contract.OwnerRef) []byte {
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	target := raw["target"].(map[string]any)
	target["tenant_id"] = o.TenantID
	target["owner_id"] = o.OwnerID
	result, _ := json.Marshal(raw)
	return result
}

func TestPGLockDeadlineRollsBackAndNormalControlStillCommits(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	locked := make(chan struct{})
	release := make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			if _, err := store.LockInput(ctx, tx, owner, "locked-input"); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("lock fixture failed")
	}
	original := command("lock-deadline", "locked-input", "normal control", nil, future())
	start := time.Now()
	_, err := h.Record(ctx, original, &principal)
	assertReason(t, err, "dependency_unavailable")
	if elapsed := time.Since(start); elapsed > 2500*time.Millisecond {
		t.Fatalf("lock deadline was not finite: %v", elapsed)
	}
	ref := contract.CommandRef{Owner: owner, CommandID: "lock-deadline"}
	result, err := contract.GetCommand(ctx, readWire(ref), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.AsNotFound(); !ok {
		t.Fatal("timed-out partial receipt became visible")
	}
	close(release)
	if err = <-holder; err != nil {
		t.Fatal(err)
	}
	out, err := h.Record(ctx, original, &principal)
	assertReceived(t, out, err)
	got, err := h.Observe(ctx, "locked-input", &principal)
	if err != nil || got.Input.Revision != 1 || got.Job.WorkRevision != 1 {
		t.Fatal("normal control failed after lock timeout")
	}
}

func TestPGOtherTenantAndOwnerProgressWhileOriginalKeyIsLocked(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	locked := make(chan struct{})
	release := make(chan struct{})
	holder := make(chan error, 1)
	go func() {
		holder <- store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
			if _, err := store.LockCommand(ctx, tx, contract.CommandRef{Owner: owner, CommandID: "same-key"}); err != nil {
				return err
			}
			close(locked)
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	select {
	case <-locked:
	case <-ctx.Done():
		t.Fatal("lock fixture failed")
	}
	// Both scopes use exactly the locked original command and object IDs. They
	// must finish while the first owner still holds its actual SQL key lock.
	results := make(chan error, 2)
	var workers sync.WaitGroup
	for _, scope := range []contract.OwnerRef{{TenantID: owner.TenantID, OwnerID: "owner-two"}, {TenantID: "tenant-two", OwnerID: owner.OwnerID}} {
		workers.Add(1)
		go func(scope contract.OwnerRef) {
			defer workers.Done()
			subject := principal
			subject.TenantID = scope.TenantID
			h := hostFor(store, scope, subject)
			out, err := h.Record(ctx, forOwner(command("same-key", "same-object", string(scope.TenantID)+"/"+string(scope.OwnerID), nil, future()), scope), &subject)
			if err != nil {
				results <- err
				return
			}
			received, ok := out.AsReceived()
			if !ok {
				results <- fmt.Errorf("cross-scope admission unconfirmed")
				return
			}
			if applied, ok := received.Receipt.AsApplied(); !ok || applied.Revision != "1" {
				results <- fmt.Errorf("cross-scope original aliased")
				return
			}
			got, err := h.Observe(ctx, "same-object", &subject)
			if err != nil {
				results <- err
				return
			}
			if got.Input.Text != string(scope.TenantID)+"/"+string(scope.OwnerID) || got.Job.WorkRevision != 1 {
				results <- fmt.Errorf("cross-scope responsibility leaked")
				return
			}
			results <- nil
		}(scope)
	}
	workers.Wait()
	close(results)
	close(release)
	if err := <-holder; err != nil {
		t.Fatal(err)
	}
	for err := range results {
		if err != nil {
			t.Fatal(err)
		}
	}
	control := hostFor(store, owner, principal)
	out, err := control.Record(ctx, command("same-key", "same-object", "normal control", nil, future()), &principal)
	assertReceived(t, out, err)
}
