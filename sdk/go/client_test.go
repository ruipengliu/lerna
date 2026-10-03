package harness_test

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	harness "github.com/ruipengliu/lerna/sdk/go"
)

type fixtureInput struct {
	Value string `json:"value"`
}
type fixtureOutput struct {
	Saved string `json:"saved"`
}
type lostReply struct {
	calls   int
	command api.Command
	receipt api.Receipt
}

func (t *lostReply) Call(_ context.Context, kind string, payload json.RawMessage) (json.RawMessage, error) {
	switch kind {
	case "command":
		t.calls++
		if e := api.Decode(payload, &t.command); e != nil {
			return nil, e
		}
		digest, _ := api.Digest(t.command)
		t.receipt = api.Receipt{CommandID: t.command.CommandID, RequestDigest: digest, Stage: "applied", DecidedAt: api.Time(time.Now()), Output: api.Raw(fixtureOutput{"committed"})}
		return nil, errors.New("reply lost after committed decision")
	case "receipt_lookup":
		var q api.ReceiptLookup
		if e := api.Decode(payload, &q); e != nil {
			return nil, e
		}
		if q.CommandID != t.command.CommandID || q.LogicalServiceID != t.command.LogicalServiceID {
			return nil, api.E("not_found", "command_not_found")
		}
		return api.Raw(t.receipt), nil
	}
	return nil, errors.New("unsupported fixture")
}
func fixtureDiscovery(owner string) harness.Discovery {
	m := api.Contract[fixtureInput, fixtureOutput]("fixture.save", "fixture", "command", false, false)
	m.SchemaDigest, _ = api.Digest([]any{m.InputSchema, m.OutputSchema})
	return harness.Discovery{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, SchemaDigest: api.CoreDigest(), IdentityScope: api.Hash([]byte("current principal")), Methods: []api.MethodContract{m}}
}
func TestRestartQueriesOriginalCommittedCommand(t *testing.T) {
	ctx := context.Background()
	owner := api.NewID("srv")
	d := fixtureDiscovery(owner)
	path := filepath.Join(t.TempDir(), "journal")
	j, e := harness.OpenJournal(path, d.IdentityScope)
	if e != nil {
		t.Fatal(e)
	}
	transport := &lostReply{}
	client, e := harness.NewClient(transport, j, d)
	if e != nil {
		t.Fatal(e)
	}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: api.NewID("cmd"), Method: "fixture.save", TargetID: owner, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(fixtureInput{"accurate original"})}
	if _, e = client.Send(ctx, command); e == nil {
		t.Fatal("lost response unexpectedly succeeded")
	}
	if e = j.Close(); e != nil {
		t.Fatal(e)
	}
	j, e = harness.OpenJournal(path, d.IdentityScope)
	if e != nil {
		t.Fatal(e)
	}
	defer j.Close()
	client, e = harness.NewClient(transport, j, d)
	if e != nil {
		t.Fatal(e)
	}
	receipts, partial, e := client.Recover(ctx)
	if e != nil || partial || len(receipts) != 1 || receipts[0].CommandID != command.CommandID || transport.calls != 1 {
		t.Fatalf("receipts=%v partial=%v calls=%d err=%v", receipts, partial, transport.calls, e)
	}
	pending, partial, e := j.Pending(ctx, 10)
	if e != nil || partial || len(pending) != 0 {
		t.Fatal("final receipt not durable")
	}
	changed := command
	changed.ExpiresAt = api.Time(time.Now().Add(time.Hour))
	if _, e = client.Send(ctx, changed); !api.IsCode(e, "idempotency_conflict") {
		t.Fatalf("original deadline changed: %v", e)
	}
}

func TestOriginalMethodDecoderChangeStopsRecoveryBeforeResend(t *testing.T) {
	ctx := context.Background()
	owner := api.NewID("srv")
	d := fixtureDiscovery(owner)
	j, err := harness.OpenJournal(filepath.Join(t.TempDir(), "journal"), d.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	transport := &lostReply{}
	client, err := harness.NewClient(transport, j, d)
	if err != nil {
		t.Fatal(err)
	}
	original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: api.NewID("command"), Method: "fixture.save", TargetID: owner, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(fixtureInput{"original"})}
	if _, err = client.Send(ctx, original); err == nil {
		t.Fatal("expected lost response")
	}
	d.Methods[0].OutputSchema = api.Object(map[string]any{"changed": api.String()}, "changed")
	d.Methods[0].SchemaDigest, _ = api.Digest([]any{d.Methods[0].InputSchema, d.Methods[0].OutputSchema})
	client, err = harness.NewClient(transport, j, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err = client.Recover(ctx); !api.IsCode(err, "unsupported") {
		t.Fatalf("changed decoder recovery: %v", err)
	}
	if _, err = client.Receipt(ctx, original.CommandID); !api.IsCode(err, "unsupported") {
		t.Fatalf("changed decoder receipt: %v", err)
	}
	if transport.calls != 1 {
		t.Fatal("changed decoder created another physical command send")
	}
}

func TestTwoJournalHandlesCannotReplaceTheSameOriginalCommand(t *testing.T) {
	ctx := context.Background()
	owner := api.NewID("service")
	d := fixtureDiscovery(owner)
	path := filepath.Join(t.TempDir(), "shared-original")
	first, err := harness.OpenJournal(path, d.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := harness.OpenJournal(path, d.IdentityScope)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	for round := 0; round < 12; round++ {
		original := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: api.NewID("command"), Method: "fixture.save", TargetID: owner, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(fixtureInput{"first original"})}
		entries := [2]harness.Entry{}
		for i, text := range []string{"first original", "other original"} {
			c := original
			c.Payload = api.Raw(fixtureInput{text})
			digest, _ := api.Digest(c)
			entries[i] = harness.Entry{IdentityScope: d.IdentityScope, SchemaDigest: d.SchemaDigest, MethodSchemaDigest: d.Methods[0].SchemaDigest, Command: c, Digest: digest}
		}
		begin := make(chan struct{})
		errors := [2]error{}
		var wait sync.WaitGroup
		for i, journal := range []*harness.FileJournal{first, second} {
			wait.Add(1)
			go func(index int, j *harness.FileJournal) {
				defer wait.Done()
				<-begin
				errors[index] = j.Save(ctx, entries[index])
			}(i, journal)
		}
		close(begin)
		wait.Wait()
		winner := -1
		for i, err := range errors {
			if err == nil {
				if winner != -1 {
					t.Fatal("two different originals both replaced the durable key")
				}
				winner = i
			} else if !api.IsCode(err, "overloaded") && !api.IsCode(err, "idempotency_conflict") {
				t.Fatal(err)
			}
		}
		if winner == -1 {
			t.Fatal("neither bounded journal save acquired the original key")
		}
		stored, err := first.Read(ctx, original.CommandID)
		if err != nil || !api.Equal(stored.Command, entries[winner].Command) {
			t.Fatalf("durable winner was replaced: %v", err)
		}
		if err = second.Save(ctx, entries[1-winner]); !api.IsCode(err, "idempotency_conflict") {
			t.Fatalf("later conflicting original changed the key: %v", err)
		}
	}
}

type failedJournal struct{}

func (failedJournal) Save(context.Context, harness.Entry) error { return errors.New("disk full") }
func (failedJournal) Read(context.Context, string) (harness.Entry, error) {
	return harness.Entry{}, errors.New("disk full")
}
func (failedJournal) Pending(context.Context, int) ([]harness.Entry, bool, error) {
	return nil, false, errors.New("disk full")
}
func TestStorageFailurePreventsFirstSend(t *testing.T) {
	owner := api.NewID("srv")
	transport := &lostReply{}
	client, e := harness.NewClient(transport, failedJournal{}, fixtureDiscovery(owner))
	if e != nil {
		t.Fatal(e)
	}
	c := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: owner, CommandID: api.NewID("cmd"), Method: "fixture.save", TargetID: owner, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(fixtureInput{"x"})}
	if _, e = client.Send(context.Background(), c); e == nil || transport.calls != 0 {
		t.Fatal("sent before durable journal")
	}
}
