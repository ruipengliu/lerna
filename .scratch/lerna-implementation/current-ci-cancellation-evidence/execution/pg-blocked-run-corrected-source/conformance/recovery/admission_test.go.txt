//go:build integration

package recovery_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/ruipengliu/lerna/contract"
	"github.com/ruipengliu/lerna/host/durablework"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"github.com/ruipengliu/lerna/runtime"
	"strings"
	"testing"
	"time"
)

var owner = contract.OwnerRef{TenantID: "tenant-one", OwnerID: "owner-one"}
var principal = contract.SubjectBinding{TenantID: "tenant-one", SubjectID: "alice", DelegationChain: []contract.DelegatedSubject{}}

func contextFor(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	t.Cleanup(cancel)
	return ctx
}
func database(t *testing.T) *ownedFixture { return newOwnedFixture(t, "postgres", true) }
func rawHostFor(store admissionStore, o contract.OwnerRef, subject contract.SubjectBinding) *durablework.Host {
	return durablework.New(o, store, store, store, store, durablework.NewPermissions([]durablework.Permission{{Subject: subject, Owner: o, Record: true, Read: true}}))
}

// Normal fixture assembly explicitly installs one durable finite pool for the
// given owner. It is not a runtime nil fallback; negative/competition suites
// use rawHostFor and install their own declared shared pool.
func fixturePool(host *durablework.Host) {
	repo, ok := host.Repository.(demo.PoolRepository)
	if !ok {
		panic("mandatory pool adapter missing")
	}
	runner, ok := host.Repository.(runtime.TxRunner)
	if !ok {
		panic("mandatory real fixture runner missing")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err := runner.Within(ctx, host.Owner, func(ctx context.Context, tx runtime.Tx) error {
		_, err := repo.LockPool(ctx, tx)
		if err == nil {
			return nil
		}
		if !errors.Is(err, demo.ErrPoolMissing) {
			return err
		}
		sum := sha256.Sum256([]byte(string(host.Owner.TenantID) + "/" + string(host.Owner.OwnerID)))
		cfg := demo.DefaultPool(contract.ID("fixture-"+hex.EncodeToString(sum[:16])), []contract.OwnerRef{host.Owner})
		cfg.Limits[0].Queue = 4096
		cfg.Limits[0].Concurrent = 64
		cfg.Quotas[0].Concurrent = 64
		now, err := host.Clock.Now(ctx, tx)
		if err != nil {
			return err
		}
		return repo.InstallPool(ctx, tx, cfg, 0, now)
	})
	if err != nil {
		panic(fmt.Sprintf("mandatory explicit fixture pool: %v", err))
	}
}
func hostFor(store admissionStore, o contract.OwnerRef, subject contract.SubjectBinding) *durablework.Host {
	h := rawHostFor(store, o, subject)
	fixturePool(h)
	return h
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

func TestPGEffectiveDurabilitySettingsAndIdempotentMigration(t *testing.T) {
	fixture := database(t)
	store := fixture.PG()
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
	fixture := database(t)
	store := fixture.PG()
	ctx := contextFor(t)
	if _, err := store.Now(ctx, nil); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("nil clock Tx accepted: %v", err)
	}
	if _, err := store.Settings(ctx, nil); !errors.Is(err, runtime.ErrScope) {
		t.Fatalf("nil settings Tx accepted: %v", err)
	}
}
