package executor

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type recoveryCalls struct {
	inner    harness.Transport
	commands atomic.Int64
	lookups  atomic.Int64
}

func (c *recoveryCalls) Call(ctx context.Context, kind string, raw json.RawMessage) (json.RawMessage, error) {
	if kind == "command" {
		c.commands.Add(1)
	}
	if kind == "receipt_lookup" {
		c.lookups.Add(1)
	}
	return c.inner.Call(ctx, kind, raw)
}

func TestTLSOldAdmissionJournalNewDialRecoversOriginalReceiptAndAttempt(t *testing.T) {
	f := newDeviceFixture(t)
	client, ctx := startTLSHostFor(t, f, 90*time.Second)
	config := client.Config
	originalDiscovery := client.SDK.Discovery
	originalDiscovery.Methods = append([]api.MethodContract{}, originalDiscovery.Methods...)
	raw, err := legacyContractsFS.ReadFile("legacy/3bfd14f.json")
	if err != nil {
		t.Fatal(err)
	}
	var contracts []api.MethodContract
	if err = api.Decode(raw, &contracts); err != nil {
		t.Fatal(err)
	}
	for i, method := range originalDiscovery.Methods {
		for _, historical := range contracts {
			if historical.Name == method.Name {
				originalDiscovery.Methods[i] = historical
			}
		}
	}
	originalDiscovery.MethodsDigest, err = api.DigestLimit(originalDiscovery.Methods, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := harness.NewClient(client.SDK.Transport, client.SDK.Journal, originalDiscovery)
	if err != nil {
		t.Fatal(err)
	}
	original := commandFor(f.b.EndpointID, apiID("command", f.b.BundleID+"/install"), "executor.admission.install", f.b.Intent.OperationID, f.b.StartBefore, f.b)
	legacy.Transport = &loseOriginalReply{inner: legacy.Transport, journal: legacy.Journal, commandID: original.CommandID}
	if _, err = legacy.Send(ctx, original); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("original delivery reply loss: %v", err)
	}
	before, err := legacy.Journal.Read(ctx, original.CommandID)
	if err != nil || before.Receipt != nil {
		t.Fatalf("original unresolved disk journal: %+v %v", before, err)
	}
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	client, err = Dial(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	calls := &recoveryCalls{inner: client.SDK.Transport}
	client.SDK.Transport = calls
	results, partial, err := client.SDK.Recover(ctx)
	if err != nil || partial || len(results) != 1 || results[0].CommandID != original.CommandID || results[0].Stage != "applied" {
		t.Fatalf("new Dial did not retain original receipt decoder: %+v %t %v", results, partial, err)
	}
	if calls.commands.Load() != 0 || calls.lookups.Load() != 1 {
		t.Fatal("original receipt recovery resent before lookup")
	}
	after, err := client.SDK.Journal.Read(ctx, original.CommandID)
	if err != nil || after.MethodSchemaDigest != before.MethodSchemaDigest || !api.Equal(after.Command, before.Command) || after.Digest != before.Digest {
		t.Fatal("upgrade replaced original command/schema/TTL")
	}
	changed := original
	changed.ExpiresAt = api.Time(time.Now().Add(time.Minute))
	if _, err = client.SDK.Send(ctx, changed); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("upgraded ID permitted changed TTL: %v", err)
	}
	if err = client.Prepare(ctx, f.b, func(_ context.Context, permission ContentPermission) ([]byte, error) {
		return f.raw[contentKey(permission.ContentRef)], nil
	}); err != nil {
		t.Fatal(err)
	}
	window := f.control(t, "active", "running", 1)
	invoke := frozenInvoke(f, window)
	var delivery ControlDelivery
	if _, err = f.h.Store.Read(ctx, f.h.Scope, Namespace+".controls", window.WindowID, 0, &delivery); err != nil {
		t.Fatal(err)
	}
	client.SDK.Transport = &loseOriginalReply{inner: client.SDK.Transport, journal: client.SDK.Journal, commandID: invoke.CommandID}
	if _, err = client.Dispatch(ctx, invoke, delivery); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("original accepted operation reply not lost: %v", err)
	}
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	client, err = Dial(ctx, config)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	calls = &recoveryCalls{inner: client.SDK.Transport}
	client.SDK.Transport = calls
	results, partial, err = client.SDK.Recover(ctx)
	if err != nil || partial || len(results) != 1 || results[0].CommandID != invoke.CommandID || calls.commands.Load() != 0 || calls.lookups.Load() != 1 {
		t.Fatalf("same accepted operation responsibility recovery: %+v %t %v", results, partial, err)
	}
	view := waitApplied(t, ctx, client, f.b.Intent.OperationID)
	if len(view.Attempts.Items) != 1 {
		t.Fatalf("new Dial duplicated physical attempt: %+v", view)
	}
	physical, err := os.ReadDir(filepath.Join(f.root, "files", ".harness", "journals"))
	if err != nil || len(physical) != 1 {
		t.Fatalf("independent physical journal: %d %v", len(physical), err)
	}
	actual, err := os.ReadFile(filepath.Join(f.root, "files", "reports", "result.txt"))
	if err != nil || string(actual) != "independent cloud-admitted file bytes\n" {
		t.Fatalf("exact target truth: %q %v", actual, err)
	}
	entry, err := client.SDK.Journal.Read(ctx, invoke.CommandID)
	if err != nil || !api.Equal(entry.Command, invoke) {
		t.Fatal("accepted operation command was replaced")
	}
	unknownJournal, err := harness.OpenJournal(filepath.Join(f.root, "unknown-contract"), client.SDK.Discovery.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer unknownJournal.Close()
	unknownEntry := before
	unknownEntry.MethodSchemaDigest = api.Hash([]byte("unregistered original method contract"))
	if err = unknownJournal.Save(context.Background(), unknownEntry); err != nil {
		t.Fatal(err)
	}
	savedJournal := client.SDK.Journal
	client.SDK.Journal = unknownJournal
	calls.commands.Store(0)
	calls.lookups.Store(0)
	_, _, err = client.SDK.Recover(context.Background())
	client.SDK.Journal = savedJournal
	if !api.IsCode(err, "unsupported") || calls.commands.Load() != 0 || calls.lookups.Load() != 0 {
		t.Fatalf("unknown original schema used actual TLS transport: %v", err)
	}
	wrongConfig := config
	wrongConfig.DatabaseID = api.NewID("database")
	wrong, err := Dial(ctx, wrongConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer wrong.Close()
	wrongCalls := &recoveryCalls{inner: wrong.SDK.Transport}
	wrong.SDK.Transport = wrongCalls
	if _, _, err = wrong.SDK.Recover(ctx); !api.IsCode(err, "forbidden") || wrongCalls.commands.Load() != 0 || wrongCalls.lookups.Load() != 0 {
		t.Fatalf("wrong device DB scope recovered original responsibility: %v", err)
	}
	t.Logf("EXECUTOR_UPGRADE_EVIDENCE %s", api.Raw(struct {
		DeviceDatabaseID      string `json:"device_database_id"`
		OperationID           string `json:"operation_id"`
		AdmissionCommandID    string `json:"admission_command_id"`
		AdmissionSchemaDigest string `json:"admission_schema_digest"`
		CommandID             string `json:"command_id"`
		AttemptID             string `json:"attempt_id"`
		OriginalTTL           string `json:"original_ttl"`
		PhysicalJournals      uint64 `json:"physical_journals"`
	}{config.DatabaseID, invoke.TargetID, original.CommandID, before.MethodSchemaDigest, invoke.CommandID, view.Attempts.Items[0].AttemptID, invoke.ExpiresAt, uint64(len(physical))}))
}
