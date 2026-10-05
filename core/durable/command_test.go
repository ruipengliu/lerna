package durable_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type fixedClock struct{ t time.Time }

func (c *fixedClock) Now() time.Time { return c.t }

func newDomain(t *testing.T) (*durable.Domain, *fixedClock) {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "d.db"), "test", sqlite.LocalProfile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`CREATE TABLE facts (k TEXT PRIMARY KEY, v TEXT)`); err != nil {
		t.Fatal(err)
	}
	c := &fixedClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	return durable.NewDomain("d1", db, c), c
}

func identity(id string) *lernav1.CommandIdentity {
	return &lernav1.CommandIdentity{UserId: "u", IssuerId: "i", TargetDomainId: "d1", CommandId: id}
}

func registerPut(d *durable.Domain) {
	d.HandleCommand("test.put", func() proto.Message { return &lernav1.GlobalName{} },
		func(_ context.Context, tx *durable.Tx, in durable.Incoming) (durable.Outcome, error) {
			p := in.Payload.(*lernav1.GlobalName)
			if _, err := tx.Exec(`INSERT INTO facts (k, v) VALUES (?, ?)`, p.GetLocalId(), p.GetObjectKind()); err != nil {
				return durable.Outcome{}, err
			}
			if p.GetObjectKind() == "reject" {
				return durable.Outcome{}, errs.New(lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT, "business rejection")
			}
			return durable.Outcome{Result: p}, nil
		})
}

func count(t *testing.T, d *durable.Domain, q string) int {
	t.Helper()
	var n int
	if err := d.Read(context.Background(), func(tx *durable.Tx) error { return tx.QueryRow(q).Scan(&n) }); err != nil {
		t.Fatal(err)
	}
	return n
}

// 规则：G3
func TestRejectionIsDurableAndLeavesNoBusinessWrites(t *testing.T) {
	d, _ := newDomain(t)
	registerPut(d)
	env, _ := durable.NewEnvelope(identity("c1"), "test.put", &lernav1.GlobalName{LocalId: "k", ObjectKind: "reject"})
	r, err := d.Execute(context.Background(), env)
	if err != nil || r.GetDecision() != lernav1.Decision_DECISION_REJECTED {
		t.Fatalf("want REJECTED decision, got %v %v", r, err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM facts`); n != 0 {
		t.Fatalf("partial business write survived a rejection")
	}
	r2, err := d.Execute(context.Background(), env)
	if err != nil || !proto.Equal(r, r2) {
		t.Fatalf("rejection must be replayed unchanged: %v %v", r2, err)
	}
}

// 规则：G3
func TestNonBusinessErrorsWriteNoDecision(t *testing.T) {
	d, _ := newDomain(t)
	registerPut(d)
	env, _ := durable.NewEnvelope(identity("c1"), "test.put", &lernav1.GlobalName{LocalId: "k"})
	env.Payload = []byte{0xff, 0xff}
	if _, err := d.Execute(context.Background(), env); err == nil {
		t.Fatal("undecodable command must fail")
	}
	q, _ := d.QueryReceipt(context.Background(), identity("c1"))
	if q.GetOutcome() != lernav1.ReceiptQueryOutcome_RECEIPT_QUERY_OUTCOME_NOT_FOUND {
		t.Fatalf("decode failure is not a business rejection; got %s", q.GetOutcome())
	}
	env2, _ := durable.NewEnvelope(identity("c2"), "test.put", &lernav1.GlobalName{LocalId: "k"})
	env2.Identity.TargetDomainId = "elsewhere"
	if _, err := d.Execute(context.Background(), env2); !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_INVALID_INPUT) {
		t.Fatalf("misrouted command must be refused, got %v", err)
	}
}

// 规则：G3、G11
func TestStaleClaimCannotAdvance(t *testing.T) {
	d, c := newDomain(t)
	d.HandleJob("test.job", func(context.Context, *durable.Claim) error { return nil })
	ctx := context.Background()
	if err := d.Write(ctx, "seed", func(tx *durable.Tx) error {
		_, err := tx.EnqueueJob(durable.JobSpec{Kind: "test.job", User: "u", Subject: "s", PurposeKey: "p"})
		return err
	}); err != nil {
		t.Fatal(err)
	}
	old, err := d.ClaimJobs(ctx, "claim-1", "proc-1", 1)
	if err != nil || len(old) != 1 {
		t.Fatalf("claim: %v %v", old, err)
	}
	c.t = c.t.Add(d.LeaseDuration + time.Second)
	cur, err := d.ClaimJobs(ctx, "claim-2", "proc-2", 1)
	if err != nil || len(cur) != 1 || cur[0].Epoch != old[0].Epoch+1 {
		t.Fatalf("expired lease must be taken over with a new epoch: %v %v", cur, err)
	}
	err = d.Advance(ctx, &old[0], "late", func(tx *durable.Tx) (durable.Transition, error) {
		_, err := tx.Exec(`INSERT INTO facts (k, v) VALUES ('late', 'write')`)
		return durable.Done(), err
	})
	if !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_STALE_CLAIM) {
		t.Fatalf("old claim must be fenced, got %v", err)
	}
	if n := count(t, d, `SELECT COUNT(*) FROM facts`); n != 0 {
		t.Fatalf("fenced write leaked")
	}
	if err := d.Advance(ctx, &cur[0], "current", func(*durable.Tx) (durable.Transition, error) { return durable.Done(), nil }); err != nil {
		t.Fatalf("current claim must advance: %v", err)
	}
}
