package durable_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/cmd/assembly"
	"github.com/ruipengliu/lerna/contracts/command"
	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"google.golang.org/protobuf/proto"
)

type backend interface {
	Submit(context.Context, *v1.Caller, *v1.SubmitGoalCommand, *v1.Ref) (*v1.CommandReceipt, error)
	QueryJob(context.Context, *v1.Caller, *v1.GlobalName) (*v1.Job, error)
	ExecuteJob(context.Context, *v1.Caller, *v1.JobCommand) (*v1.CommandReceipt, error)
}
type factory func(*testing.T) backend

func sqliteBackend(t *testing.T) backend {
	t.Helper()
	h, e := assembly.Open(filepath.Join(t.TempDir(), "jobs.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { h.Close() })
	return h.Durable
}
func jobCommand(id string) *v1.JobCommand {
	return &v1.JobCommand{Identity: &v1.CommandIdentity{UserId: "alice", IssuerId: "worker", TargetDomainId: "local", CommandId: id}, ContractVersion: 1, Action: "CLAIM", ProcessInstance: "process-a", AllowedTypes: []string{"DECIDE_GOAL"}, Limit: 1, LeaseMs: 30000}
}

var caller = &v1.Caller{UserId: "alice", IssuerId: "worker"}

// 规则：G3、G11
func TestSQLiteSemantics(t *testing.T) { runSemantics(t, sqliteBackend) }
func seed(t *testing.T, b backend) {
	t.Helper()
	_, e := b.Submit(context.Background(), caller, &v1.SubmitGoalCommand{Identity: jobCommand("goal").Identity, ContractVersion: 1, SchemaId: "lerna.v1.SubmitGoal", FingerprintVersion: 1, Goal: "goal"}, command.NewRef("alice", "local/content", "content", "lerna.v1.Content"))
	if e != nil {
		t.Fatal(e)
	}
}
func runSemantics(t *testing.T, newBackend factory) {
	t.Run("control invalidates worker", func(t *testing.T) { controlInvalidatesWorker(t, newBackend) })
	t.Run("takeover keeps endpoint", func(t *testing.T) { takeoverKeepsFixedEndpoint(t, newBackend) })
	t.Run("bounds and conflicts", func(t *testing.T) { boundsAndConflicts(t, newBackend) })
	t.Run("concurrent claims", func(t *testing.T) { concurrentClaims(t, newBackend) })
	t.Run("claim and renewal preserve original lease", func(t *testing.T) {
		b := newBackend(t)
		seed(t, b)
		c := jobCommand("claim")
		r, e := b.ExecuteJob(context.Background(), caller, c)
		if e != nil || len(r.Jobs) != 1 {
			t.Fatalf("claim: %v %v", r, e)
		}
		j := r.Jobs[0]
		if j.ClaimEpoch != 1 || j.Ref.Revision != 1 || j.State != "CLAIMED" || j.LeaseUntilUnixMs <= r.DecidedAtUnixMs || j.LeaseUntilUnixMs > r.DecidedAtUnixMs+30000 {
			t.Fatalf("job: %v", j)
		}
		c = jobCommand("renew")
		c.Action = "RENEW"
		c.JobRef = j.Ref
		c.ClaimEpoch = j.ClaimEpoch
		r, e = b.ExecuteJob(context.Background(), caller, c)
		if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
			t.Fatalf("renew %v %v", r, e)
		}
		again, e := b.ExecuteJob(context.Background(), caller, c)
		if e != nil || !proto.Equal(r, again) || r.Jobs[0].ClaimEpoch != 1 || r.Jobs[0].Ref.Revision != 1 {
			t.Fatalf("renew replay %v %v", again, e)
		}
	})

	t.Run("empty claim replay is immutable", func(t *testing.T) {
		b := newBackend(t)
		c := jobCommand("empty")
		r, e := b.ExecuteJob(context.Background(), caller, c)
		if e != nil {
			t.Fatal(e)
		}
		again, e := b.ExecuteJob(context.Background(), caller, c)
		if e != nil || !proto.Equal(r, again) || len(r.Jobs) != 0 || r.Decision != v1.Decision_DECISION_ACCEPTED {
			t.Fatalf("receipts %v %v %v", r, again, e)
		}
	})
}

// 规则：G3、G11
func controlInvalidatesWorker(t *testing.T, factory factory) {
	b := factory(t)
	seed(t, b)
	ctx := context.Background()
	r, e := b.ExecuteJob(ctx, caller, jobCommand("claim"))
	if e != nil {
		t.Fatal(e)
	}
	j := r.Jobs[0]
	c := jobCommand("block")
	c.Action = "CONTROL"
	c.Module = "sessions"
	c.JobRef = j.Ref
	c.NextState = "BLOCKED"
	c.WaitingReason = "proof missing"
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("block %v %v", r, e)
	}
	stale := jobCommand("stale")
	stale.Action = "PROGRESS"
	stale.JobRef = j.Ref
	stale.ClaimEpoch = j.ClaimEpoch
	stale.NextState = "COMPLETED"
	rejected, e := b.ExecuteJob(ctx, caller, stale)
	if e != nil || rejected.Decision != v1.Decision_DECISION_REJECTED || rejected.Error.Code != "STALE_CLAIM" {
		t.Fatalf("stale %v %v", rejected, e)
	}
	c = jobCommand("restore")
	c.Action = "CONTROL"
	c.Module = "sessions"
	c.JobRef = r.Jobs[0].Ref
	c.NextState = "READY"
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("restore %v %v", r, e)
	}
	c = jobCommand("close")
	c.Action = "CONTROL"
	c.Module = "sessions"
	c.JobRef = r.Jobs[0].Ref
	c.NextState = "CLOSED"
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED || r.Error.Code != "UNSUPPORTED_FEATURE" {
		t.Fatalf("close discarded undecided responsibility: %v %v", r, e)
	}
	current, e := b.QueryJob(ctx, caller, c.JobRef.Name)
	if e != nil || current.State != "READY" {
		t.Fatalf("close changed pending work: %v %v", current, e)
	}
	again, e := b.ExecuteJob(ctx, caller, stale)
	if e != nil || !proto.Equal(again, rejected) {
		t.Fatalf("rejection changed %v %v", again, e)
	}
}

// 规则：G3、G11
func TestPendingDecisionRejectsUnclaimedSnapshot(t *testing.T) {
	h, e := assembly.Open(filepath.Join(t.TempDir(), "jobs.db"), "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer h.Close()
	seed(t, h.Durable)
	jobs, e := h.Durable.Pending(context.Background(), caller)
	if e != nil || len(jobs) != 1 {
		t.Fatalf("pending %v %v", jobs, e)
	}
	e = h.Sessions.ProcessClaim(context.Background(), jobs[0])
	if e == nil {
		t.Fatal("unclaimed snapshot decided goal")
	}
	q, e := h.Durable.QueryReceipt(context.Background(), caller, jobCommand("goal").Identity)
	if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_SUBMITTED {
		t.Fatalf("receipt changed %v %v", q, e)
	}
	if e = h.Sessions.ProcessPending(context.Background(), caller); e != nil {
		t.Fatal(e)
	}
	q, e = h.Durable.QueryReceipt(context.Background(), caller, jobCommand("goal").Identity)
	if e != nil || q.State != v1.ReceiptQueryState_RECEIPT_QUERY_STATE_DECIDED {
		t.Fatalf("worker failed %v %v", q, e)
	}
}

// 规则：G3、G11
func takeoverKeepsFixedEndpoint(t *testing.T, factory factory) {
	b := factory(t)
	ctx := context.Background()
	c := jobCommand("schedule")
	c.Action = "ENQUEUE"
	c.Job = &v1.Job{Module: "ledger", JobType: "DELIVER_HANDOFF", ContractVersion: 1, Responsibility: jobCommand("responsibility").Identity, PurposeKey: "deliver", ExecutorEndpointId: "endpoint-original", LedgerDomainId: "ledger-original", SpecificationRef: command.NewRef("alice", "local/content", "content", "lerna.v1.Content")}
	r, e := b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("enqueue %v %v", r, e)
	}
	c = jobCommand("first")
	c.AllowedTypes = []string{"DELIVER_HANDOFF"}
	c.LeaseMs = 10
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || len(r.Jobs) != 1 {
		t.Fatalf("claim %v %v", r, e)
	}
	old := r.Jobs[0]
	time.Sleep(20 * time.Millisecond)
	c = jobCommand("second")
	c.ProcessInstance = "process-b"
	c.AllowedTypes = []string{"DELIVER_HANDOFF"}
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || len(r.Jobs) != 1 {
		t.Fatalf("takeover %v %v", r, e)
	}
	j := r.Jobs[0]
	if j.ClaimEpoch != 2 || j.Ref.Revision != 1 || j.ExecutorEndpointId != "endpoint-original" || j.LedgerDomainId != "ledger-original" || !proto.Equal(j.Responsibility, old.Responsibility) {
		t.Fatalf("responsibility changed %v", j)
	}
	c = jobCommand("old-progress")
	c.Action = "PROGRESS"
	c.JobRef = old.Ref
	c.ClaimEpoch = old.ClaimEpoch
	c.NextState = "COMPLETED"
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("old worker advanced %v %v", r, e)
	}
	c = jobCommand("new-progress")
	c.ProcessInstance = "process-b"
	c.Action = "PROGRESS"
	c.JobRef = j.Ref
	c.ClaimEpoch = j.ClaimEpoch
	c.NextState = "COMPLETED"
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("current worker failed %v %v", r, e)
	}
	c = jobCommand("reopen-completed")
	c.Action = "CONTROL"
	c.Module = "ledger"
	c.JobRef = r.Jobs[0].Ref
	c.NextState = "READY"
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED || r.Error.Code != "TERMINAL_JOB" {
		t.Fatalf("reopened terminal job: %v %v", r, e)
	}
}

func boundsAndConflicts(t *testing.T, factory factory) {
	b := factory(t)
	ctx := context.Background()
	c := jobCommand("bound")
	c.LeaseMs = 60001
	r, e := b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("unbounded lease %v %v", r, e)
	}
	c.LeaseMs = 1000
	if _, e = b.ExecuteJob(ctx, caller, c); e == nil {
		t.Fatal("changed command accepted")
	}
	c = jobCommand("empty-before-job")
	empty, e := b.ExecuteJob(ctx, caller, c)
	if e != nil {
		t.Fatal(e)
	}
	seed(t, b)
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || !proto.Equal(r, empty) {
		t.Fatalf("empty replay acquired new work %v %v", r, e)
	}
	c = jobCommand("claim")
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || len(r.Jobs) != 1 {
		t.Fatalf("claim %v %v", r, e)
	}
	j := r.Jobs[0]
	c = jobCommand("wait")
	c.Action = "PROGRESS"
	c.JobRef = j.Ref
	c.ClaimEpoch = j.ClaimEpoch
	c.NextState = "WAITING"
	c.ReadyAtUnixMs = r.DecidedAtUnixMs + 60000
	c.WaitingReason = "backoff"
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("wait %v %v", r, e)
	}
	r, e = b.ExecuteJob(ctx, caller, jobCommand("early"))
	if e != nil || len(r.Jobs) != 0 {
		t.Fatalf("ignored ready time %v %v", r, e)
	}
}
func concurrentClaims(t *testing.T, factory factory) {
	b := factory(t)
	seed(t, b)
	results := make(chan *v1.CommandReceipt, 2)
	errs := make(chan error, 2)
	for _, id := range []string{"a", "b"} {
		go func(id string) {
			c := jobCommand(id)
			c.ProcessInstance = id
			r, e := b.ExecuteJob(context.Background(), caller, c)
			results <- r
			errs <- e
		}(id)
	}
	total := 0
	for range 2 {
		r := <-results
		if e := <-errs; e != nil {
			t.Fatal(e)
		}
		total += len(r.Jobs)
	}
	if total != 1 {
		t.Fatalf("granted %d claims", total)
	}
}
