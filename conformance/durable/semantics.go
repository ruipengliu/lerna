// Package durable 是持久工作的语义测试，按"同一事务后端接口"组织：
// 任何事务后端（M1 的 SQLite，将来的 PostgreSQL）都用 Run 跑同一套用例。
package durable

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
)

// Backend 返回一个已建立持久工作表的空数据库。
type Backend func(t *testing.T) *sql.DB

// Clock 是可控的权威时钟。
type Clock struct{ T time.Time }

// Now 返回当前时间。
func (c *Clock) Now() time.Time { return c.T }

// Env 是一个用例的事务域和时钟。
type Env struct {
	D     *durable.Domain
	Clock *Clock
}

// NewEnv 在 backend 上建立事务域，登记测试用的工作类型。
func NewEnv(t *testing.T, b Backend) *Env {
	t.Helper()
	c := &Clock{T: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	d := durable.NewDomain("d1", b(t), c)
	d.HandleJob("test.job", func(context.Context, *durable.Claim) error { return nil })
	return &Env{D: d, Clock: c}
}

// Enqueue 登记一项工作。
func (e *Env) Enqueue(t *testing.T, purpose string, wait bool) {
	t.Helper()
	err := e.D.Write(context.Background(), "seed", func(tx *durable.Tx) error {
		_, err := tx.EnqueueJob(durable.JobSpec{Kind: "test.job", User: "u", Subject: purpose, PurposeKey: purpose, Wait: wait})
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// Claim 用 claimID 领取最多一项工作。
func (e *Env) Claim(t *testing.T, claimID, instance string) []durable.Claim {
	t.Helper()
	cs, err := e.D.ClaimJobs(context.Background(), claimID, instance, 1)
	if err != nil {
		t.Fatal(err)
	}
	return cs
}

// Tx 在写事务中执行 fn。
func (e *Env) Tx(t *testing.T, fn func(tx *durable.Tx) error) {
	t.Helper()
	if err := e.D.Write(context.Background(), "test", fn); err != nil {
		t.Fatal(err)
	}
}

// State 返回工作当前状态。
func (e *Env) State(t *testing.T, purpose string) durable.JobState {
	t.Helper()
	var st durable.JobState
	err := e.D.Read(context.Background(), func(tx *durable.Tx) error {
		j, err := tx.JobByPurpose("u", purpose)
		if j != nil {
			st = j.State
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func isStale(err error) bool { return errs.Is(err, lernav1.ErrorCode_ERROR_CODE_STALE_CLAIM) }

// Run 对 backend 运行全部持久工作语义用例。
func Run(t *testing.T, b Backend) {
	ctx := context.Background()

	t.Run("claim is atomic and grants a new epoch", func(t *testing.T) {
		e := NewEnv(t, b)
		e.Enqueue(t, "p", false)
		a := e.Claim(t, "c1", "proc-a")
		bb := e.Claim(t, "c2", "proc-b")
		if len(a) != 1 || len(bb) != 0 {
			t.Fatalf("only one claimer may hold the job: %v / %v", a, bb)
		}
		if a[0].Epoch != 1 {
			t.Fatalf("first claim epoch = %d", a[0].Epoch)
		}
	})

	t.Run("claim receipt replays the original list; empty is also fixed", func(t *testing.T) {
		e := NewEnv(t, b)
		empty := e.Claim(t, "c0", "proc-a")
		e.Enqueue(t, "p", false)
		if again := e.Claim(t, "c0", "proc-a"); len(empty) != 0 || len(again) != 0 {
			t.Fatalf("an empty claim result is fixed for its claim id: %v", again)
		}
		first := e.Claim(t, "c1", "proc-a")
		replay := e.Claim(t, "c1", "proc-a")
		if len(replay) != 1 || replay[0].Epoch != first[0].Epoch {
			t.Fatalf("replay must return the original claim: %v vs %v", replay, first)
		}
		// 拿回回执不等于恢复领取权：租约过期后原领取无法推进。
		e.Clock.T = e.Clock.T.Add(e.D.LeaseDuration + time.Second)
		replay = e.Claim(t, "c1", "proc-a")
		err := e.D.Advance(ctx, &replay[0], "late", func(*durable.Tx) (durable.Transition, error) { return durable.Done(), nil })
		if !isStale(err) {
			t.Fatalf("expired lease must not advance, got %v", err)
		}
	})

	t.Run("lease expiry takeover fences the old claimer", func(t *testing.T) {
		e := NewEnv(t, b)
		e.Enqueue(t, "p", false)
		old := e.Claim(t, "c1", "proc-a")[0]
		e.Clock.T = e.Clock.T.Add(e.D.LeaseDuration + time.Second)
		cur := e.Claim(t, "c2", "proc-b")
		if len(cur) != 1 || cur[0].Epoch != old.Epoch+1 || cur[0].Subject != old.Subject {
			t.Fatalf("takeover must keep the responsibility and bump the epoch: %v", cur)
		}
		for _, op := range []func() error{
			func() error { _, err := e.D.Renew(ctx, "r-old", &old); return err },
			func() error {
				return e.D.Advance(ctx, &old, "old", func(*durable.Tx) (durable.Transition, error) { return durable.Done(), nil })
			},
		} {
			if err := op(); !isStale(err) {
				t.Fatalf("old claimer must be fenced, got %v", err)
			}
		}
		if err := e.D.Advance(ctx, &cur[0], "cur", func(*durable.Tx) (durable.Transition, error) { return durable.Done(), nil }); err != nil {
			t.Fatal(err)
		}
		if st := e.State(t, "p"); st != durable.JobDone {
			t.Fatalf("state = %s", st)
		}
	})

	t.Run("renew keeps the epoch and a duplicate renew does not extend twice", func(t *testing.T) {
		e := NewEnv(t, b)
		e.Enqueue(t, "p", false)
		c := e.Claim(t, "c1", "proc-a")[0]
		e.Clock.T = e.Clock.T.Add(10 * time.Second)
		u1, err := e.D.Renew(ctx, "r1", &c)
		if err != nil {
			t.Fatal(err)
		}
		e.Clock.T = e.Clock.T.Add(10 * time.Second)
		u2, err := e.D.Renew(ctx, "r1", &c)
		if err != nil || !u1.Equal(u2) {
			t.Fatalf("duplicate renew must return the original deadline: %v %v %v", u1, u2, err)
		}
		if c.Epoch != 1 {
			t.Fatalf("renew must not bump the epoch")
		}
		if err := e.D.Advance(ctx, &c, "x", func(*durable.Tx) (durable.Transition, error) { return durable.Done(), nil }); err != nil {
			t.Fatalf("renewed claim must still advance: %v", err)
		}
	})

	t.Run("revision change invalidates the claim", func(t *testing.T) {
		e := NewEnv(t, b)
		e.Enqueue(t, "p", false)
		c := e.Claim(t, "c1", "proc-a")[0]
		e.Tx(t, func(tx *durable.Tx) error { return tx.ReviseJob("u", "p", []byte("v2")) })
		err := e.D.Advance(ctx, &c, "x", func(*durable.Tx) (durable.Transition, error) { return durable.Done(), nil })
		if !isStale(err) {
			t.Fatalf("old revision must not advance, got %v", err)
		}
	})

	t.Run("blocked keeps responsibility until explicitly resumed; sealed never reopens", func(t *testing.T) {
		e := NewEnv(t, b)
		e.Enqueue(t, "p", false)
		c := e.Claim(t, "c1", "proc-a")[0]
		if err := e.D.Advance(ctx, &c, "x", func(*durable.Tx) (durable.Transition, error) { return durable.Block("no proof"), nil }); err != nil {
			t.Fatal(err)
		}
		e.Clock.T = e.Clock.T.Add(time.Hour)
		if got := e.Claim(t, "c2", "proc-a"); len(got) != 0 {
			t.Fatalf("blocked job must not be claimed")
		}
		e.Tx(t, func(tx *durable.Tx) error { return tx.UnblockJob("u", "p") })
		if got := e.Claim(t, "c3", "proc-a"); len(got) != 1 {
			t.Fatalf("resumed job must be claimable")
		}
		e.Tx(t, func(tx *durable.Tx) error { return tx.SealJob("u", "p") })
		e.Clock.T = e.Clock.T.Add(time.Hour)
		e.Tx(t, func(tx *durable.Tx) error { return tx.WakeJob("u", "p") })
		if got := e.Claim(t, "c4", "proc-a"); len(got) != 0 || e.State(t, "p") != durable.JobSealed {
			t.Fatalf("sealed job must stay sealed")
		}
	})

	t.Run("purpose key deduplicates repeated notifications", func(t *testing.T) {
		e := NewEnv(t, b)
		e.Enqueue(t, "p", false)
		e.Enqueue(t, "p", false)
		var n int
		_ = e.D.Read(ctx, func(tx *durable.Tx) error { return tx.QueryRow(`SELECT COUNT(*) FROM jobs`).Scan(&n) })
		if n != 1 {
			t.Fatalf("jobs = %d, want 1", n)
		}
	})

	t.Run("waiting job is claimable only after wake or due time", func(t *testing.T) {
		e := NewEnv(t, b)
		e.Enqueue(t, "p", true)
		if got := e.Claim(t, "c1", "proc-a"); len(got) != 0 {
			t.Fatal("waiting job claimed before wake")
		}
		e.Tx(t, func(tx *durable.Tx) error { return tx.WakeJob("u", "p") })
		c := e.Claim(t, "c2", "proc-a")
		if len(c) != 1 {
			t.Fatal("woken job must be claimable")
		}
		at := e.Clock.T.Add(time.Minute)
		if err := e.D.Advance(ctx, &c[0], "x", func(*durable.Tx) (durable.Transition, error) { return durable.WaitUntil(at, "later"), nil }); err != nil {
			t.Fatal(err)
		}
		if got := e.Claim(t, "c3", "proc-a"); len(got) != 0 {
			t.Fatal("claimed before due time")
		}
		e.Clock.T = at
		if got := e.Claim(t, "c4", "proc-a"); len(got) != 1 {
			t.Fatal("due job must be claimable")
		}
	})
}
