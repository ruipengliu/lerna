package wsbinding_test

import (
	"context"
	"lerna/adapters/sqliteauth"
	"lerna/adapters/wsbinding"
	"lerna/authorization"
	wire "lerna/gen/harness/v1"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReliableInboxPrefixSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "auth.db")
	open := func() (*sqliteauth.Store, *wsbinding.Journal) {
		db, e := sqliteauth.Open(path)
		if e != nil {
			t.Fatal(e)
		}
		a, e := authorization.New(db, authorization.SystemClock{}, authorization.Config{CredentialTTL: time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 8, MaxResources: 16, MaxDepth: 8, MaxWork: 128, EvaluationTimeout: time.Second})
		if e != nil {
			t.Fatal(e)
		}
		j, e := wsbinding.NewJournal(a, "cloud/operator", wsbinding.JournalConfig{Records: 16, Bytes: 65536, Reorder: 4, Retention: time.Minute, Attempts: 3})
		if e != nil {
			t.Fatal(e)
		}
		return db, j
	}
	db, j := open()
	// Bootstrap is performed once by the trusted host.
	a, _ := authorization.New(db, authorization.SystemClock{}, authorization.Config{CredentialTTL: time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 8, MaxResources: 16, MaxDepth: 8, MaxWork: 128, EvaluationTimeout: time.Second})
	if _, e := a.Bootstrap(ctx, "local", "admin"); e != nil {
		t.Fatal(e)
	}
	msg := func(seq uint64, id string) *wire.WSReliable {
		return &wire.WSReliable{Generation: 1, Position: seq, MessageId: id, Request: &wire.CapabilityRequest{MessageId: id, Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: "operation"}}}
	}
	if n, e := j.Receive(ctx, msg(2, "two")); e != nil || n != 0 {
		t.Fatalf("gap acknowledged: %d %v", n, e)
	}
	db.Close()
	db, j = open()
	defer db.Close()
	if n, e := j.Receive(ctx, msg(1, "one")); e != nil || n != 2 {
		t.Fatalf("prefix: %d %v", n, e)
	}
	if n, e := j.Receive(ctx, msg(2, "two")); e != nil || n != 2 {
		t.Fatalf("duplicate: %d %v", n, e)
	}
	bad := msg(2, "two")
	bad.Request.Body = &wire.CapabilityRequest_GetInvocation{GetInvocation: "other"}
	if _, e := j.Receive(ctx, bad); !authorization.Is(e, authorization.IdentityConflict) {
		t.Fatalf("changed payload: %v", e)
	}
	pending, e := j.Unconsumed(ctx)
	if e != nil || len(pending) != 2 {
		t.Fatalf("pending: %d %v", len(pending), e)
	}
}

func TestReliableOutboxAndReplyAreAtomic(t *testing.T) {
	ctx := context.Background()
	db, e := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a, e := authorization.New(db, authorization.SystemClock{}, authorization.Config{CredentialTTL: time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 8, MaxResources: 16, MaxDepth: 8, MaxWork: 128, EvaluationTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Bootstrap(ctx, "local", "admin"); e != nil {
		t.Fatal(e)
	}
	j, e := wsbinding.NewJournal(a, "peer", wsbinding.JournalConfig{Records: 16, Bytes: 65536, Reorder: 4, Retention: time.Minute, Attempts: 3})
	if e != nil {
		t.Fatal(e)
	}
	req := &wire.CapabilityRequest{MessageId: "intent", Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: "operation"}}
	m, e := j.Prepare(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	same, e := j.Prepare(ctx, req)
	if e != nil || same.Position != m.Position {
		t.Fatal("intent duplicated", e)
	}
	batch, e := j.Replay(ctx, 1, 0)
	if e != nil || len(batch) != 1 {
		t.Fatal("missing outbox", e)
	}
	if e = j.Acknowledge(ctx, 1, 2); !authorization.Is(e, authorization.Invalid) {
		t.Fatal("future ack", e)
	}
	if _, e = j.Receive(ctx, m); e != nil {
		t.Fatal(e)
	}
	reply := &wire.CapabilityResponse{MessageId: "reply", ReplyTo: "intent", Namespace: "local", Body: &wire.CapabilityResponse_Failure{Failure: &wire.CapabilityFailure{Code: "NOT_FOUND"}}}
	fail := func(tx authorization.RuntimeTransaction) error {
		tx.SetData([]byte("partial"))
		return &authorization.Error{Code: authorization.Unavailable}
	}
	if e = j.Complete(ctx, m, reply, fail); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal(e)
	}
	pending, e := j.Unconsumed(ctx)
	if e != nil || len(pending) != 1 {
		t.Fatal("consumed failed business", e)
	}
	if e = j.Complete(ctx, m, reply, func(tx authorization.RuntimeTransaction) error {
		if len(tx.Data()) != 0 {
			t.Fatal("partial business state")
		}
		tx.SetData([]byte("accepted"))
		return nil
	}); e != nil {
		t.Fatal(e)
	}
	pending, e = j.Unconsumed(ctx)
	if e != nil || len(pending) != 0 {
		t.Fatal("not consumed", e)
	}
	batch, e = j.Replay(ctx, 1, 0)
	if e != nil || len(batch) != 2 {
		t.Fatal("missing reply", e)
	}
}

type journalClock struct{ now time.Time }

func (c *journalClock) Now() (time.Time, error) { return c.now, nil }
func TestReliableExpiryClosesStreamAndRetainsOperation(t *testing.T) {
	ctx := context.Background()
	clock := &journalClock{time.Now()}
	db, e := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a, e := authorization.New(db, clock, authorization.Config{CredentialTTL: time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 8, MaxResources: 16, MaxDepth: 8, MaxWork: 128, EvaluationTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Bootstrap(ctx, "local", "admin"); e != nil {
		t.Fatal(e)
	}
	j, e := wsbinding.NewJournal(a, "peer", wsbinding.JournalConfig{Records: 16, Bytes: 65536, Reorder: 4, Retention: time.Second, Attempts: 3})
	if e != nil {
		t.Fatal(e)
	}
	req := &wire.CapabilityRequest{MessageId: "old", Namespace: "local", Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{Invocation: &wire.CapabilityInvocation{OperationId: "original"}, GrantMaterial: "sensitive"}}}
	m, e := j.Prepare(ctx, req)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = j.Receive(ctx, m); e != nil {
		t.Fatal(e)
	}
	clock.now = clock.now.Add(2 * time.Second)
	if _, e = j.Replay(ctx, 1, 0); !authorization.Is(e, authorization.Expired) {
		t.Fatal(e)
	}
	generation, e := j.CloseSending(ctx)
	if e != nil || generation != 2 {
		t.Fatal(generation, e)
	}
	if e = j.CloseReceiving(ctx, 2); e != nil {
		t.Fatal(e)
	}
	if _, e = j.Receive(ctx, m); !authorization.Is(e, authorization.Expired) {
		t.Fatal("old stream revived", e)
	}
	if _, e = j.Prepare(ctx, req); !authorization.Is(e, authorization.Expired) {
		t.Fatal("old message revived", e)
	}
	recovery, e := j.Recovery(ctx)
	if e != nil || len(recovery) != 2 {
		t.Fatal("lost recovery obligation", e)
	}
	for _, r := range recovery {
		if r.OperationID != "original" || r.State != "UNKNOWN" {
			t.Fatal(r)
		}
	}
	pending, e := j.Unconsumed(ctx)
	if e != nil || len(pending) != 0 {
		t.Fatal("old action scheduled", e)
	}
}

func TestReliableCapacityAndAcknowledgementBounds(t *testing.T) {
	ctx := context.Background()
	db, e := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a, e := authorization.New(db, authorization.SystemClock{}, authorization.Config{CredentialTTL: time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 8, MaxResources: 16, MaxDepth: 8, MaxWork: 128, EvaluationTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Bootstrap(ctx, "local", "admin"); e != nil {
		t.Fatal(e)
	}
	j, e := wsbinding.NewJournal(a, "peer", wsbinding.JournalConfig{Records: 2, Bytes: 4096, Reorder: 4, Retention: time.Minute, Attempts: 2})
	if e != nil {
		t.Fatal(e)
	}
	request := func(id string) *wire.CapabilityRequest {
		return &wire.CapabilityRequest{MessageId: id, Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: "op"}}
	}
	if _, e = j.Prepare(ctx, request("one")); e != nil {
		t.Fatal(e)
	}
	if e = j.Acknowledge(ctx, 1, 1); !authorization.Is(e, authorization.Invalid) {
		t.Fatal("unsent ack", e)
	}
	if _, e = j.Prepare(ctx, request("two")); e != nil {
		t.Fatal(e)
	}
	if _, e = j.Prepare(ctx, request("three")); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("capacity", e)
	}
	status, e := j.Status(ctx)
	if e != nil || status.Outbox != 2 || status.Allocated != 2 || status.Confirmed != 0 {
		t.Fatal(status, e)
	}
	for i := 0; i < 2; i++ {
		batch, e := j.Replay(ctx, 1, 0)
		if e != nil || len(batch) != 2 {
			t.Fatal(e)
		}
		for _, m := range batch {
			if e = j.Attempt(ctx, m); e != nil {
				t.Fatal(e)
			}
		}
	}
	batch, e := j.Replay(ctx, 1, 0)
	if e != nil {
		t.Fatal(e)
	}
	if e = j.Attempt(ctx, batch[0]); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("retry limit", e)
	}
	if e = j.Acknowledge(ctx, 1, 2); e != nil {
		t.Fatal(e)
	}
	status, e = j.Status(ctx)
	if e != nil || status.Confirmed != 2 || status.Outbox != 2 {
		t.Fatal("ack deleted recovery evidence", status, e)
	}
	bounded, e := wsbinding.NewJournal(a, "byte-bound", wsbinding.JournalConfig{Records: 2, Bytes: 4096, Reorder: 4, Retention: time.Minute, Attempts: 2})
	if e != nil {
		t.Fatal(e)
	}
	large := request("large")
	large.Body = &wire.CapabilityRequest_GetInvocation{GetInvocation: strings.Repeat("x", 4096)}
	if _, e = bounded.Prepare(ctx, large); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("byte budget", e)
	}
	if _, e = bounded.Receive(ctx, &wire.WSReliable{Generation: 1, Position: 5, MessageId: "gap", Request: request("gap")}); !authorization.Is(e, authorization.Unavailable) {
		t.Fatal("reorder budget", e)
	}
	status, e = bounded.Status(ctx)
	if e != nil || status.Outbox != 0 || status.Inbox != 0 || status.Received != 0 {
		t.Fatal("partial bounded commit", status, e)
	}
}

func TestReliableDeniedPayloadDoesNotBlockFollowingPosition(t *testing.T) {
	ctx := context.Background()
	db, e := sqliteauth.Open(filepath.Join(t.TempDir(), "auth.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	a, e := authorization.New(db, authorization.SystemClock{}, authorization.Config{CredentialTTL: time.Hour, GrantTTL: time.Hour, WindowTTL: time.Minute, ReceiptRetention: time.Hour, MaxRules: 8, MaxResources: 16, MaxDepth: 8, MaxWork: 128, EvaluationTimeout: time.Second})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = a.Bootstrap(ctx, "local", "admin"); e != nil {
		t.Fatal(e)
	}
	j, e := wsbinding.NewJournal(a, "peer", wsbinding.JournalConfig{Records: 16, Bytes: 65536, Reorder: 4, Retention: time.Minute, Attempts: 3})
	if e != nil {
		t.Fatal(e)
	}
	one, e := j.Prepare(ctx, &wire.CapabilityRequest{MessageId: "denied", Namespace: "local", Body: &wire.CapabilityRequest_Invoke{Invoke: &wire.InvokeCapability{Invocation: &wire.CapabilityInvocation{OperationId: "original"}}}})
	if e != nil {
		t.Fatal(e)
	}
	two, e := j.Prepare(ctx, &wire.CapabilityRequest{MessageId: "healthy", Namespace: "local", Body: &wire.CapabilityRequest_GetInvocation{GetInvocation: "other"}})
	if e != nil {
		t.Fatal(e)
	}
	marker, e := j.Omit(ctx, one)
	if e != nil {
		t.Fatal(e)
	}
	if marker.Request != nil || len(marker.OmittedSha256) != 32 {
		t.Fatal("disposition leaked payload", marker)
	}
	if _, e = j.Receive(ctx, two); e != nil {
		t.Fatal(e)
	}
	if n, e := j.Receive(ctx, marker); e != nil || n != 2 {
		t.Fatal("disposition did not fill gap", n, e)
	}
	pending, e := j.Unconsumed(ctx)
	if e != nil || len(pending) != 1 || pending[0].MessageId != "healthy" {
		t.Fatal("denied action scheduled", pending, e)
	}
	if n, e := j.Receive(ctx, one); e != nil || n != 2 {
		t.Fatal("late original", n, e)
	}
	pending, e = j.Unconsumed(ctx)
	if e != nil || len(pending) != 1 {
		t.Fatal("late original reactivated", e)
	}
}
