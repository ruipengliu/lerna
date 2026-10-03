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
	admissionReopeners.Store(store, func(t *testing.T) admissionStore { return reopen(t, store) })
	if err = store.Migrate(contextFor(t)); err != nil {
		t.Fatal(err)
	}
	return store
}
func hostFor(store admissionStore, o contract.OwnerRef, subject contract.SubjectBinding) *durablework.Host {
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
