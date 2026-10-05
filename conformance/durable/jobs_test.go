package durable_test

import (
	"context"
	"path/filepath"
	"testing"

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
func TestControlInvalidatesWorker(t *testing.T) {
	b := sqliteBackend(t)
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
	if e != nil || r.Decision != v1.Decision_DECISION_ACCEPTED {
		t.Fatalf("close %v %v", r, e)
	}
	c.Identity.CommandId = "reopen"
	c.JobRef = r.Jobs[0].Ref
	c.NextState = "READY"
	r, e = b.ExecuteJob(ctx, caller, c)
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED {
		t.Fatalf("reopen %v %v", r, e)
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
