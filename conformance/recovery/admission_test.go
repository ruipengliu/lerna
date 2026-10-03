//go:build integration

package recovery_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/postgres"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
)

var configurations sync.Map
var owner = contract.OwnerRef{TenantID: "tenant-one", OwnerID: "owner-one"}
var principal = contract.SubjectBinding{TenantID: "tenant-one", SubjectID: "alice", DelegationChain: []contract.DelegatedSubject{}}

func contextFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func database(t *testing.T) *postgres.Store {
	t.Helper()
	dsn := os.Getenv("LERNA_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Fatal("LERNA_TEST_POSTGRES_DSN is required (dedicated test database)")
	}
	var nonce [12]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		t.Fatal(err)
	}
	cfg := postgres.Config{DSN: dsn, Schema: "lerna_test_" + hex.EncodeToString(nonce[:]), TransactionTimeout: 3 * time.Second, StatementTimeout: 2 * time.Second, LockTimeout: time.Second}
	store, err := postgres.Open(contextFor(t), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = store.CreateSchema(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.DropTestSchema(ctx); err != nil {
			t.Error(err)
		}
		store.Close()
	})
	configurations.Store(store, cfg)
	if err = store.Migrate(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return store
}
func hostFor(store *postgres.Store, o contract.OwnerRef, subject contract.SubjectBinding) *durablework.Host {
	return durablework.New(o, store, store, store, store, durablework.NewPermissions([]durablework.Permission{{Subject: subject, Owner: o, Record: true, Read: true}}))
}
func command(id, object, text string, revision *contract.Revision, deadline string) []byte {
	value := contract.CommandEnvelope{ContractVersion: "host-durable-work-1", Profile: "host", Method: "durable_work.record", CommandID: contract.ID(id), Target: contract.ObjectRef{TenantID: owner.TenantID, OwnerID: owner.OwnerID, Kind: "durable_work", ID: contract.ID(object)}, ExpectedRevision: revision, AcceptBefore: contract.Time(deadline)}
	value.Payload, _ = json.Marshal(map[string]string{"text": text})
	data, _ := json.Marshal(value)
	return data
}
func future() string { return time.Now().Add(time.Hour).UTC().Format("2006-01-02T15:04:05.000000Z") }
func readWire(ref contract.CommandRef) []byte {
	data, _ := json.Marshal(contract.CommandGetRequest{ContractVersion: "1.0.0", Profile: "command", Method: "command.get", CommandID: "query", Target: contract.CommandTarget{TenantID: ref.Owner.TenantID, OwnerID: ref.Owner.OwnerID, Kind: "command", ID: ref.CommandID}, Payload: contract.CommandGetPayload{CommandRef: ref}, AcceptBefore: contract.Time(future())})
	return data
}
func TestPGAdmissionCommitsFixedReceiptInputAndPendingJob(t *testing.T) {
	store := database(t)
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

func TestPGTransactionsRejectForeignOwnerDatabaseAndExpiredTokens(t *testing.T) {
	store := database(t)
	other := database(t)
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

func TestPGAdmissionPreservesZeroUnicodeScalar(t *testing.T) {
	store := database(t)
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

func assertReceived(t *testing.T, out contract.TransportOutcome, err error) contract.CommandReceipt {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
	received, ok := out.AsReceived()
	if !ok {
		t.Fatal("expected confirmed receipt")
	}
	return received.Receipt
}
func assertReason(t *testing.T, err error, reason contract.ErrorCode) {
	t.Helper()
	var classified *contract.ContractError
	if !errors.As(err, &classified) || classified.Code != reason {
		t.Fatalf("got %v want %s", err, reason)
	}
}
func assertReceiptSame(t *testing.T, left, right contract.CommandReceipt) {
	t.Helper()
	a, _ := contract.Encode(left)
	b, _ := contract.Encode(right)
	if string(a) != string(b) {
		t.Fatalf("fixed receipt changed: %s != %s", a, b)
	}
}
func TestPGOriginalIdentitySurvivesRetransmissionAndRejectsChangedMeaning(t *testing.T) {
	store := database(t)
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

func reopen(t *testing.T, original *postgres.Store) *postgres.Store {
	t.Helper()
	cfg, _ := configurations.Load(original)
	store, err := postgres.Open(contextFor(t), cfg.(postgres.Config))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	return store
}
func TestPGLostHostReplyAndClosedConnectionRecoverOriginal(t *testing.T) {
	setup := database(t)
	writer := reopen(t, setup)
	ctx := contextFor(t)
	h := hostFor(writer, owner, principal)
	original := command("lost-reply", "input", "persisted", nil, future())
	out, err := h.Record(ctx, original, &principal)
	receipt := assertReceived(t, out, err)
	// The confirmed response is deliberately not delivered to any caller.
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	replacement := reopen(t, setup)
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
func TestPGTransactionRollbackLeavesNoPartialAdmission(t *testing.T) {
	store := database(t)
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
func TestPGEffectiveDurabilitySettingsAndIdempotentMigration(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	if err := store.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	err := store.Within(ctx, owner, func(ctx context.Context, tx runtime.Tx) error {
		settings, err := store.Settings(ctx, tx)
		if err != nil {
			return err
		}
		if settings.Isolation != "read committed" || settings.SynchronousCommit != "on" || settings.StatementTimeout != "2s" || settings.LockTimeout != "1s" || !strings.HasPrefix(settings.ServerVersion, "18.6 ") && settings.ServerVersion != "18.6" {
			t.Fatalf("unexpected real PG settings: %+v", settings)
		}
		t.Logf("effective PG settings: %+v", settings)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPGNilTransactionIsRejectedWithoutPanic(t *testing.T) {
	store := database(t)
	ctx := contextFor(t)
	if _, err := store.Now(ctx, nil); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("nil clock Tx accepted: %v", err)
	}
	if _, err := store.Settings(ctx, nil); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("nil settings Tx accepted: %v", err)
	}
}

func TestPGCommandStoreRetainsMinimalOriginalMetadata(t *testing.T) {
	store := database(t)
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
