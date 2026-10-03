//go:build integration

package recovery_test

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

type controlledClock struct {
	*postgres.Store
	instant time.Time
}

func (c *controlledClock) Now(context.Context, runtime.Tx) (time.Time, error) { return c.instant, nil }
func TestPGOriginalKeyPrecedesDeadlineAndNewExpiryIsFixed(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	clock := &controlledClock{Store: store, instant: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)}
	permissions := durablework.NewPermissions([]durablework.Permission{{Subject: principal, Owner: owner, Record: true, Read: true}})
	h := durablework.New(owner, store, store, store, clock, permissions)
	cutoff := "2026-10-03T02:00:00.000000Z"
	original := command("admitted", "input", "first", nil, cutoff)
	out, err := h.Record(ctx, original, &principal)
	first := assertReceived(t, out, err)
	clock.instant = time.Date(2026, 10, 3, 2, 0, 0, 0, time.UTC)
	out, err = h.Record(ctx, original, &principal)
	assertReceiptSame(t, first, assertReceived(t, out, err))
	expired := command("expired", "missing", "never written", nil, cutoff)
	out, err = h.Record(ctx, expired, &principal)
	receipt := assertReceived(t, out, err)
	rejected, ok := receipt.AsRejected()
	if !ok || rejected.Reason != "expired" {
		t.Fatal("new deadline boundary was not fixed expired")
	}
	clock.instant = time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)
	out, err = h.Record(ctx, expired, &principal)
	assertReceiptSame(t, receipt, assertReceived(t, out, err))
	if _, err = h.Observe(ctx, "missing", &principal); err == nil {
		t.Fatal("expired command created input/Job")
	}
	ref := contract.CommandRef{Owner: owner, CommandID: "expired"}
	result, err := contract.GetCommand(ctx, readWire(ref), &principal, permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := result.AsFound()
	if !ok {
		t.Fatal("expired rejection not durably queryable")
	}
	assertReceiptSame(t, receipt, found.Receipt)
	// Revoking record cannot bypass current authorization via the old key.
	h.Permissions = durablework.NewPermissions([]durablework.Permission{{Subject: principal, Owner: owner, Read: true}})
	_, err = h.Record(ctx, original, &principal)
	assertReason(t, err, "forbidden")
	result, err = contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: owner, CommandID: "admitted"}), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.AsFound(); !ok {
		t.Fatal("read permission lost immutable decision")
	}
}
func TestPGRecordEnforcesFixedRevisionPreconditionsAndStableJob(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	cutoff := future()
	zero := contract.Revision("0")
	invalid := command("missing-precondition", "input", "x", &zero, cutoff)
	out, err := h.Record(ctx, invalid, &principal)
	receipt := assertReceived(t, out, err)
	if rejected, ok := receipt.AsRejected(); !ok || rejected.Reason != "revision_changed" {
		t.Fatal("expected revision on missing object allowed")
	}
	out, err = h.Record(ctx, command("create", "input", "same", nil, cutoff), &principal)
	assertReceived(t, out, err)
	first, err := h.Observe(ctx, "input", &principal)
	if err != nil {
		t.Fatal(err)
	}
	out, err = h.Record(ctx, invalid, &principal)
	assertReceiptSame(t, receipt, assertReceived(t, out, err))
	out, err = h.Record(ctx, command("unconditional-update", "input", "x", nil, cutoff), &principal)
	if rejected, ok := assertReceived(t, out, err).AsRejected(); !ok || rejected.Reason != "revision_changed" {
		t.Fatal("missing update revision allowed")
	}
	one := contract.Revision("1")
	out, err = h.Record(ctx, command("update", "input", "same", &one, cutoff), &principal)
	if applied, ok := assertReceived(t, out, err).AsApplied(); !ok || applied.Revision != "2" {
		t.Fatal("explicit same-text update did not advance")
	}
	second, err := h.Observe(ctx, "input", &principal)
	if err != nil || second.Input.Revision != 2 || second.Job.WorkRevision != 2 || second.Job.ID != first.Job.ID || second.Job.CompletedRevision != 0 {
		t.Fatalf("lost stable responsibility: %+v %v", second, err)
	}
	out, err = h.Record(ctx, command("stale", "input", "x", &one, cutoff), &principal)
	if rejected, ok := assertReceived(t, out, err).AsRejected(); !ok || rejected.Reason != "revision_changed" {
		t.Fatal("stale revision allowed")
	}
}
func TestPGMaximumRevisionRefusesWithoutOverflow(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	err := store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		if _, err := store.LockInput(ctx, tx, owner, "maximum"); err != nil {
			return err
		}
		now, err := store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = store.SaveInput(ctx, tx, owner, demo.Input{ID: "maximum", Revision: math.MaxInt64, Text: "unchanged", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		_, err = store.Trigger(ctx, tx, contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "durable_work", ID: "maximum"}, "project", math.MaxInt64, now)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	maximum := contract.Revision(strconv.FormatInt(math.MaxInt64, 10))
	out, err := h.Record(ctx, command("overflow", "maximum", "changed", &maximum, future()), &principal)
	receipt := assertReceived(t, out, err)
	if rejected, ok := receipt.AsRejected(); !ok || rejected.Reason != "unsupported" {
		t.Fatal("unrepresentable revision was not refused")
	}
	got, err := h.Observe(ctx, "maximum", &principal)
	if err != nil || got.Input.Revision != math.MaxInt64 || got.Input.Text != "unchanged" || got.Job.WorkRevision != math.MaxInt64 {
		t.Fatal("revision overflow changed facts")
	}
}
func forOwner(data []byte, o contract.OwnerRef) []byte {
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	target := raw["target"].(map[string]any)
	target["tenant_id"] = o.TenantID
	target["owner_id"] = o.OwnerID
	result, _ := json.Marshal(raw)
	return result
}
func TestPGTrustedPermissionAndTenantOwnerIsolation(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	cutoff := future()
	for _, scope := range []contract.OwnerRef{owner, {TenantID: owner.TenantID, OwnerID: "owner-two"}, {TenantID: "tenant-two", OwnerID: owner.OwnerID}} {
		subject := principal
		subject.TenantID = scope.TenantID
		h := hostFor(store, scope, subject)
		out, err := h.Record(ctx, forOwner(command("same-key", "same-object", string(scope.TenantID)+"/"+string(scope.OwnerID), nil, cutoff), scope), &subject)
		assertReceived(t, out, err)
		got, err := h.Observe(ctx, "same-object", &subject)
		if err != nil || got.Input.Text != string(scope.TenantID)+"/"+string(scope.OwnerID) || got.Input.Revision != 1 {
			t.Fatal("tenant/owner isolation broken")
		}
		outsider := subject
		outsider.SubjectID = "outsider"
		for _, id := range []contract.ID{"same-key", "nonexistent"} {
			result, err := contract.GetCommand(ctx, readWire(contract.CommandRef{Owner: scope, CommandID: id}), &outsider, h.Permissions, h, time.Now)
			if err != nil {
				t.Fatal(err)
			}
			if rejected, ok := result.AsRejected(); !ok || rejected.Reason != "forbidden" {
				t.Fatal("query disclosed existence")
			}
		}
		delegated := subject
		delegated.DelegationChain = []contract.DelegatedSubject{{TenantID: scope.TenantID, SubjectID: "delegator"}}
		_, err = h.Record(ctx, forOwner(command("same-key", "same-object", "x", nil, cutoff), scope), &delegated)
		assertReason(t, err, "forbidden")
	}
	h := hostFor(store, owner, principal)
	_, err := h.Record(ctx, forOwner(command("wrong-owner", "input", "x", nil, cutoff), contract.OwnerRef{TenantID: owner.TenantID, OwnerID: "owner-two"}), &principal)
	assertReason(t, err, "forbidden")
	// Format rejection does not reserve an attacker-controlled original key.
	invalid := []byte(`{"command_id":"free-key"}`)
	_, err = h.Record(ctx, invalid, &principal)
	assertReason(t, err, "schema_invalid")
	out, err := h.Record(ctx, command("free-key", "format-control", "normal", nil, cutoff), &principal)
	assertReceived(t, out, err)
}
func TestPGConcurrentOriginalKeyHasOneDurableResponsibility(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	original := command("concurrent", "input", "one", nil, future())
	const callers = 24
	gate := make(chan struct{})
	receipts := make(chan contract.CommandReceipt, callers)
	failures := make(chan error, callers)
	var workers sync.WaitGroup
	for i := 0; i < callers; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			<-gate
			out, err := h.Record(ctx, original, &principal)
			if err != nil {
				failures <- err
				return
			}
			received, ok := out.AsReceived()
			if !ok {
				failures <- fmt.Errorf("unconfirmed result")
				return
			}
			receipts <- received.Receipt
		}()
	}
	close(gate)
	workers.Wait()
	close(receipts)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	var first *contract.CommandReceipt
	count := 0
	for receipt := range receipts {
		count++
		if first == nil {
			value := receipt
			first = &value
		} else {
			assertReceiptSame(t, *first, receipt)
		}
	}
	if count != callers {
		t.Fatalf("only %d/%d confirmed", count, callers)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Revision != 1 || got.Job.WorkRevision != 1 || got.Job.CompletedRevision != 0 {
		t.Fatalf("duplicate responsibility: %+v %v", got, err)
	}
}
func TestPGConcurrentNewCommandsSerializeObjectPrecondition(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	cutoff := future()
	out, err := h.Record(ctx, command("create", "input", "first", nil, cutoff), &principal)
	assertReceived(t, out, err)
	before, _ := h.Observe(ctx, "input", &principal)
	const callers = 12
	gate := make(chan struct{})
	receipts := make(chan contract.CommandReceipt, callers)
	failures := make(chan error, callers)
	var workers sync.WaitGroup
	revision := contract.Revision("1")
	for i := 0; i < callers; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			<-gate
			out, err := h.Record(ctx, command(fmt.Sprintf("update-%02d", i), "input", fmt.Sprintf("text-%02d", i), &revision, cutoff), &principal)
			if err != nil {
				failures <- err
				return
			}
			received, ok := out.AsReceived()
			if !ok {
				failures <- fmt.Errorf("unconfirmed result")
				return
			}
			receipts <- received.Receipt
		}(i)
	}
	close(gate)
	workers.Wait()
	close(receipts)
	close(failures)
	for err := range failures {
		t.Error(err)
	}
	applied, rejected := 0, 0
	for receipt := range receipts {
		if _, ok := receipt.AsApplied(); ok {
			applied++
		} else if value, ok := receipt.AsRejected(); ok && value.Reason == "revision_changed" {
			rejected++
		} else {
			t.Fatal("unexpected decision")
		}
	}
	if applied != 1 || rejected != callers-1 {
		t.Fatalf("applied=%d rejected=%d", applied, rejected)
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Revision != 2 || got.Job.WorkRevision != 2 || got.Job.ID != before.Job.ID {
		t.Fatalf("object/Job lock order lost update: %+v %v", got, err)
	}
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
