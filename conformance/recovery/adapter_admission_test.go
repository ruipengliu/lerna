//go:build integration

package recovery_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

// admissionStore contains only ports actually consumed by both Host adapters.
type admissionStore interface {
	durablework.Storage
	runtime.TxRunner
	runtime.CommandStore
	runtime.JobStore
	Close() error
}
type controlledClock struct {
	admissionStore
	instant time.Time
}

func (c *controlledClock) Now(context.Context, runtime.Tx) (time.Time, error) { return c.instant, nil }

var admissionReopeners sync.Map

func reopenAdmissionStore(t *testing.T, store admissionStore) admissionStore {
	t.Helper()
	reopen, _ := admissionReopeners.Load(store)
	if reopen == nil {
		t.Fatal("missing adapter reopen registration")
	}
	return reopen.(func(*testing.T) admissionStore)(t)
}
func runAdmissionBehaviors(t *testing.T, newStore func(*testing.T) admissionStore) {
	behaviors := []struct {
		name string
		run  func(*testing.T, func(*testing.T) admissionStore)
	}{
		{"AdmissionCommitsFixedReceiptInputAndPendingJob", behaviorAdmissionCommitsFixedReceiptInputAndPendingJob},
		{"TransactionsRejectForeignOwnerDatabaseAndExpiredTokens", behaviorTransactionsRejectForeignOwnerDatabaseAndExpiredTokens},
		{"AdmissionPreservesZeroUnicodeScalar", behaviorAdmissionPreservesZeroUnicodeScalar},
		{"OriginalIdentitySurvivesRetransmissionAndRejectsChangedMeaning", behaviorOriginalIdentitySurvivesRetransmissionAndRejectsChangedMeaning},
		{"TransactionRollbackLeavesNoPartialAdmission", behaviorTransactionRollbackLeavesNoPartialAdmission},
		{"CommandStoreRetainsMinimalOriginalMetadata", behaviorCommandStoreRetainsMinimalOriginalMetadata},
		{"LostHostReplyAndClosedConnectionRecoverOriginal", behaviorLostHostReplyAndClosedConnectionRecoverOriginal},
		{"OriginalKeyPrecedesDeadlineAndNewExpiryIsFixed", behaviorOriginalKeyPrecedesDeadlineAndNewExpiryIsFixed},
		{"RecordEnforcesFixedRevisionPreconditionsAndStableJob", behaviorRecordEnforcesFixedRevisionPreconditionsAndStableJob},
		{"MaximumRevisionRefusesWithoutOverflow", behaviorMaximumRevisionRefusesWithoutOverflow},
		{"TrustedPermissionAndTenantOwnerIsolation", behaviorTrustedPermissionAndTenantOwnerIsolation},
		{"ConcurrentOriginalKeyHasOneDurableResponsibility", behaviorConcurrentOriginalKeyHasOneDurableResponsibility},
		{"ConcurrentNewCommandsSerializeObjectPrecondition", behaviorConcurrentNewCommandsSerializeObjectPrecondition},
	}
	for _, behavior := range behaviors {
		t.Run(behavior.name, func(t *testing.T) { behavior.run(t, newStore) })
	}
}
func TestPGSharedAdmissionBehaviors(t *testing.T) {
	runAdmissionBehaviors(t, func(t *testing.T) admissionStore { return database(t) })
}

func behaviorAdmissionCommitsFixedReceiptInputAndPendingJob(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	data := command("original", "input", "  e\u0301🌍  ", nil, future())
	result, err := h.Record(ctx, data, &principal)
	if err != nil {
		t.Fatal(err)
	}
	received, ok := result.AsReceived()
	if !ok {
		t.Fatalf("not received: %+v", result)
	}
	applied, ok := received.Receipt.AsApplied()
	if !ok || applied.Revision != "1" || applied.ObjectRef.Kind != "durable_work" {
		t.Fatalf("not applied revision1: %+v", applied)
	}
	observed, err := h.Observe(ctx, "input", &principal)
	if err != nil {
		t.Fatal(err)
	}
	if observed.Input.Text != "  e\u0301🌍  " || observed.Input.Revision != 1 || observed.Job.WorkRevision != 1 || observed.Job.CompletedRevision != 0 || observed.Job.State != "ready" || observed.Job.ID == "" {
		t.Fatalf("incomplete atomic admission: %+v", observed)
	}
	ref := contract.CommandRef{Owner: owner, CommandID: "original"}
	queried, err := contract.GetCommand(ctx, readWire(ref), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := queried.AsFound()
	if !ok {
		t.Fatalf("missing fixed receipt: %+v", queried)
	}
	if _, ok := found.Progress.AsNone(); !ok {
		t.Fatal("Host work must expose progress none")
	}
	original, _ := contract.Encode(received.Receipt)
	actual, _ := contract.Encode(found.Receipt)
	if string(actual) != string(original) {
		t.Fatalf("fixed receipt changed: %s", actual)
	}
}

func behaviorTransactionsRejectForeignOwnerDatabaseAndExpiredTokens(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
	other := newStore(t)
	ctx := contextFor(t)
	var captured runtime.Tx
	err := store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		captured = tx
		wrong := owner
		wrong.OwnerID = "another-owner"
		if _, err := store.LockInput(ctx, tx, wrong, "input"); !errors.Is(err, runtime.ErrScope) {
			t.Fatalf("wrong owner permitted: %v", err)
		}
		if _, err := other.LockInput(ctx, tx, owner, "input"); !errors.Is(err, runtime.ErrScope) {
			t.Fatalf("foreign database permitted: %v", err)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.LockInput(ctx, captured, owner, "input"); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("expired Tx permitted: %v", err)
	}
}

func behaviorAdmissionPreservesZeroUnicodeScalar(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
	h := hostFor(store, owner, principal)
	ctx := contextFor(t)
	result, err := h.Record(ctx, command("zero-scalar", "input", "before\x00after", nil, future()), &principal)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.AsReceived(); !ok {
		t.Fatal("valid scalar was not committed")
	}
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Text != "before\x00after" {
		t.Fatalf("exact scalar not preserved: %v", err)
	}
}

func behaviorOriginalIdentitySurvivesRetransmissionAndRejectsChangedMeaning(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
	ctx := contextFor(t)
	bob := principal
	bob.SubjectID = "bob"
	permissions := durablework.NewPermissions([]durablework.Permission{{Subject: principal, Owner: owner, Record: true, Read: true}, {Subject: bob, Owner: owner, Record: true, Read: true}})
	h := durablework.New(owner, store, store, store, store, permissions)
	cutoff := future()
	original := command("original", "input", "first", nil, cutoff)
	out, err := h.Record(ctx, original, &principal)
	first := assertReceived(t, out, err)
	out, err = h.Record(ctx, original, &principal)
	assertReceiptSame(t, first, assertReceived(t, out, err))
	modified := [][]byte{command("original", "input", "changed", nil, cutoff), command("original", "other", "first", nil, cutoff), command("original", "input", "first", nil, "2099-01-01T00:00:00.000000Z")}
	revision := contract.Revision("0")
	modified = append(modified, command("original", "input", "first", &revision, cutoff))
	for _, data := range modified {
		_, err = h.Record(ctx, data, &principal)
		assertReason(t, err, "idempotency_conflict")
	}
	_, err = h.Record(ctx, original, &bob)
	assertReason(t, err, "idempotency_conflict")
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Revision != 1 || got.Input.Text != "first" || got.Job.WorkRevision != 1 {
		t.Fatalf("conflict mutated original: %+v %v", got, err)
	}
	ref := contract.CommandRef{Owner: owner, CommandID: "original"}
	queried, err := contract.GetCommand(ctx, readWire(ref), &principal, permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := queried.AsFound()
	if !ok {
		t.Fatal("conflict replaced original")
	}
	assertReceiptSame(t, first, found.Receipt)
	// Observability changes are excluded from the immutable command meaning.
	var raw map[string]any
	_ = json.Unmarshal(original, &raw)
	raw["trace_context"] = map[string]any{"trace_id": "trace-two"}
	trace, _ := json.Marshal(raw)
	out, err = h.Record(ctx, trace, &principal)
	assertReceiptSame(t, first, assertReceived(t, out, err))
}

func behaviorTransactionRollbackLeavesNoPartialAdmission(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	ref := contract.CommandRef{Owner: owner, CommandID: "rolled-back"}
	abort := errors.New("controlled rollback")
	err := store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		if _, err := store.LockCommand(ctx, tx, ref); err != nil {
			return err
		}
		if _, err := store.LockInput(ctx, tx, owner, "rollback-input"); err != nil {
			return err
		}
		now, err := store.Now(ctx, tx)
		if err != nil {
			return err
		}
		if err = store.SaveInput(ctx, tx, owner, demo.Input{ID: "rollback-input", Revision: 1, Text: "not committed", CreatedAt: now, UpdatedAt: now}); err != nil {
			return err
		}
		object := contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "durable_work", ID: "rollback-input"}
		if _, err = store.Trigger(ctx, tx, object, "project", 1, now); err != nil {
			return err
		}
		receipt := contract.NewCommandReceiptApplied(contract.CommandReceiptApplied{CommandRef: ref, ObjectRef: object, Revision: "1"})
		if err = store.SaveCommand(ctx, tx, ref, runtime.CommandRecord{Digest: "test-digest", Receipt: receipt}); err != nil {
			return err
		}
		return abort
	})
	if !errors.Is(err, abort) {
		t.Fatal(err)
	}
	if _, err = h.Observe(ctx, "rollback-input", &principal); err == nil {
		t.Fatal("rolled-back input/Job is visible")
	}
	result, err := contract.GetCommand(ctx, readWire(ref), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := result.AsNotFound(); !ok {
		t.Fatal("rolled-back receipt became visible")
	}
	out, err := h.Record(ctx, command("rolled-back", "rollback-input", "normal control", nil, future()), &principal)
	assertReceived(t, out, err)
	got, err := h.Observe(ctx, "rollback-input", &principal)
	if err != nil || got.Input.Text != "normal control" || got.Input.Revision != 1 {
		t.Fatalf("normal control failed: %+v %v", got, err)
	}
}

func behaviorCommandStoreRetainsMinimalOriginalMetadata(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
	ctx := contextFor(t)
	h := hostFor(store, owner, principal)
	cutoff := future()
	out, err := h.Record(ctx, command("metadata", "input", "private demonstration text", nil, cutoff), &principal)
	assertReceived(t, out, err)
	err = store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		record, err := store.LockCommand(ctx, tx, contract.CommandRef{Owner: owner, CommandID: "metadata"})
		if err != nil {
			return err
		}
		if record == nil {
			t.Fatal("missing original metadata")
		}
		if record.Metadata.Version != "host-durable-work-1" || record.Metadata.Profile != "host" || record.Metadata.Method != "durable_work.record" || record.Metadata.Target.ID != "input" || record.Metadata.Subject.SubjectID != "alice" || record.Metadata.AcceptBefore != contract.Time(cutoff) {
			t.Fatalf("original metadata not retained: %+v", record.Metadata)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func behaviorOriginalKeyPrecedesDeadlineAndNewExpiryIsFixed(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
	ctx := contextFor(t)
	clock := &controlledClock{admissionStore: store, instant: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)}
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

func behaviorRecordEnforcesFixedRevisionPreconditionsAndStableJob(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
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

func behaviorMaximumRevisionRefusesWithoutOverflow(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
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

func behaviorTrustedPermissionAndTenantOwnerIsolation(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
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

func behaviorConcurrentOriginalKeyHasOneDurableResponsibility(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
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

func behaviorConcurrentNewCommandsSerializeObjectPrecondition(t *testing.T, newStore func(*testing.T) admissionStore) {
	store := newStore(t)
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

func behaviorLostHostReplyAndClosedConnectionRecoverOriginal(t *testing.T, newStore func(*testing.T) admissionStore) {
	setup := newStore(t)
	writer := reopenAdmissionStore(t, setup)
	ctx := contextFor(t)
	h := hostFor(writer, owner, principal)
	original := command("lost-reply", "input", "persisted", nil, future())
	out, err := h.Record(ctx, original, &principal)
	receipt := assertReceived(t, out, err)
	// The confirmed response is deliberately not delivered to any caller.
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	replacement := reopenAdmissionStore(t, setup)
	h = hostFor(replacement, owner, principal)
	out, err = h.Record(ctx, original, &principal)
	assertReceiptSame(t, receipt, assertReceived(t, out, err))
	got, err := h.Observe(ctx, "input", &principal)
	if err != nil || got.Input.Revision != 1 || got.Job.WorkRevision != 1 {
		t.Fatalf("reopen lost or duplicated work: %+v %v", got, err)
	}
	ref := contract.CommandRef{Owner: owner, CommandID: "lost-reply"}
	result, err := contract.GetCommand(ctx, readWire(ref), &principal, h.Permissions, h, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	found, ok := result.AsFound()
	if !ok {
		t.Fatal("reopened owner lost fixed receipt")
	}
	assertReceiptSame(t, receipt, found.Receipt)
}
