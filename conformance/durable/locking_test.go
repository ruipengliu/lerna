package durable_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

// 规则：G3、G11
func TestSQLiteLeaseTimeAfterLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locking.db")
	a, e := sqlite.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	b, e := sqlite.Open(path, "alice", "local")
	if e != nil {
		t.Fatal(e)
	}
	defer b.Close()
	leaseTimeAfterLock(t, durable.New(a, "alice", "local"), durable.New(b, "alice", "local"), a.Transaction)
}

// leaseTimeAfterLock 同一事务后端提供两个连接与占锁事务；不读取内部表。
func leaseTimeAfterLock(t *testing.T, first, second backend, transaction func(context.Context, string, func(context.Context) error) error) {
	ctx := context.Background()
	seed(t, first)
	c := jobCommand("claim")
	c.LeaseMs = 200
	r, e := first.ExecuteJob(ctx, caller, c)
	if e != nil || len(r.Jobs) != 1 {
		t.Fatalf("claim %v %v", r, e)
	}
	j := r.Jobs[0]
	c = jobCommand("renew-waiting-on-lock")
	c.Action = "RENEW"
	c.JobRef = j.Ref
	c.ClaimEpoch = j.ClaimEpoch
	locked := make(chan struct{})
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- transaction(ctx, "durable.jobs", func(context.Context) error { close(locked); <-release; return nil })
	}()
	<-locked
	result := make(chan *v1.CommandReceipt, 1)
	fail := make(chan error, 1)
	go func() { r, e := second.ExecuteJob(ctx, caller, c); result <- r; fail <- e }()
	time.Sleep(300 * time.Millisecond)
	close(release)
	if e := <-done; e != nil {
		t.Fatal(e)
	}
	r = <-result
	e = <-fail
	if e != nil || r.Decision != v1.Decision_DECISION_REJECTED || r.Error.Code != "STALE_CLAIM" {
		t.Fatalf("expired while awaiting lock: %v %v", r, e)
	}
}
