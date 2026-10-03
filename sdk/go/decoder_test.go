package harness_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

func TestRetainedOriginalDecoderQueriesReceiptAfterContractUpgrade(t *testing.T) {
	ctx := context.Background()
	owner := api.NewID("owner")
	old := fixtureDiscovery(owner)
	journal, err := harness.OpenJournal(filepath.Join(t.TempDir(), "original"), old.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer journal.Close()
	transport := &lostReply{}
	client, err := harness.NewClient(transport, journal, old)
	if err != nil {
		t.Fatal(err)
	}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: api.NewID("command"), Method: "fixture.save", TargetID: owner, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(fixtureInput{"original payload"})}
	if _, err = client.Send(ctx, command); err == nil {
		t.Fatal("fixture original reply not lost")
	}
	current := fixtureDiscovery(owner)
	current.Methods[0].OutputSchema = api.Object(map[string]any{"new_output": api.String()}, "new_output")
	current.Methods[0].SchemaDigest, _ = api.Digest([]any{current.Methods[0].InputSchema, current.Methods[0].OutputSchema})
	client, err = harness.NewClient(transport, journal, current)
	if err != nil {
		t.Fatal(err)
	}
	if err = client.RetainDecoder(old.Methods[0]); err != nil {
		t.Fatal(err)
	}
	items, partial, err := client.Recover(ctx)
	if err != nil || partial || len(items) != 1 || items[0].CommandID != command.CommandID || transport.calls != 1 {
		t.Fatalf("original decoder receipt recovery: %+v %t %v", items, partial, err)
	}
	entry, err := journal.Read(ctx, command.CommandID)
	if err != nil || entry.MethodSchemaDigest != old.Methods[0].SchemaDigest || !api.Equal(entry.Command, command) {
		t.Fatal("upgrade replaced original journal contract or command")
	}
	if _, err = client.Send(ctx, command); err != nil || transport.calls != 1 {
		t.Fatalf("same original send did not query receipt: %v", err)
	}
	changed := command
	changed.Payload = api.Raw(fixtureInput{"changed"})
	if _, err = client.Send(ctx, changed); !api.IsCode(err, "idempotency_conflict") {
		t.Fatalf("legacy decoder allowed changed arguments: %v", err)
	}
	bad := old.Methods[0]
	bad.SchemaDigest = api.Hash([]byte("unknown schema"))
	if err = client.RetainDecoder(bad); !api.IsCode(err, "unsupported") {
		t.Fatalf("unbound schema decoder installed: %v", err)
	}
	bad = old.Methods[0]
	bad.Owner = "other_owner"
	if err = client.RetainDecoder(bad); !api.IsCode(err, "unsupported") {
		t.Fatalf("wrong owner decoder installed: %v", err)
	}
}

type noRecoveryIO struct{ calls int }

func (t *noRecoveryIO) Call(context.Context, string, json.RawMessage) (json.RawMessage, error) {
	t.calls++
	return nil, api.E("dependency_unavailable", "unexpected_recovery_io")
}

func TestRetainedDecoderRejectsUnboundOriginalBeforeIO(t *testing.T) {
	for _, defect := range []string{"scope", "core", "method", "payload"} {
		t.Run(defect, func(t *testing.T) {
			ctx := context.Background()
			d := fixtureDiscovery(api.NewID("owner"))
			command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: d.LogicalServiceID, CommandID: api.NewID("command"), Method: d.Methods[0].Name, TargetID: d.LogicalServiceID, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(fixtureInput{"exact original"})}
			entry := harness.Entry{IdentityScope: d.IdentityScope, SchemaDigest: d.SchemaDigest, MethodSchemaDigest: d.Methods[0].SchemaDigest, Command: command}
			if defect == "core" {
				entry.SchemaDigest = api.Hash([]byte("other core"))
			}
			if defect == "method" {
				entry.MethodSchemaDigest = api.Hash([]byte("other method"))
			}
			if defect == "payload" {
				entry.Command.Payload = api.Raw(map[string]any{"other": "arbitrary"})
			}
			entry.Digest, _ = api.Digest(entry.Command)
			journal, err := harness.OpenJournal(filepath.Join(t.TempDir(), "retained"), d.IdentityScope)
			if err != nil {
				t.Fatal(err)
			}
			defer journal.Close()
			if err = journal.Save(ctx, entry); err != nil {
				t.Fatal(err)
			}
			original := d.Methods[0]
			d.Methods[0].OutputSchema = api.Object(map[string]any{"new_output": api.String()}, "new_output")
			d.Methods[0].SchemaDigest, _ = api.Digest([]any{d.Methods[0].InputSchema, d.Methods[0].OutputSchema})
			if defect == "scope" {
				d.IdentityScope = api.Hash([]byte("other authenticated scope"))
			}
			transport := &noRecoveryIO{}
			client, err := harness.NewClient(transport, journal, d)
			if err != nil {
				t.Fatal(err)
			}
			if err = client.RetainDecoder(original); err != nil {
				t.Fatal(err)
			}
			if _, _, err = client.Recover(ctx); err == nil || transport.calls != 0 {
				t.Fatalf("unbound original recovered: %v calls=%d", err, transport.calls)
			}
		})
	}
}
