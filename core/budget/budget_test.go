package budget_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/contracts/errs"
	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func setup(t *testing.T, userLimit, taskLimit int64) *durable.Domain {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "adj.db"), "adjudication", sqlite.LocalProfile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d := durable.NewDomain("adjudication", db, &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	if err := d.Write(context.Background(), "seed", func(tx *durable.Tx) error {
		if _, err := tx.Exec(`INSERT INTO budgets (user_id, budget_id, scope, task_id, unit, limit_amount, limit_version, used, held, closed)
			VALUES ('u', 'bu', 1, '', 'micro_usd', ?, 1, 0, 0, 0)`, userLimit); err != nil {
			return err
		}
		return budget.EnsureTaskBudget(tx, "u", "t", taskLimit)
	}); err != nil {
		t.Fatal(err)
	}
	return d
}

func reserve(d *durable.Domain, op string, billing *lernav1.Billing, sends, queries int32) error {
	return d.Write(context.Background(), "r", func(tx *durable.Tx) error {
		_, err := budget.Reserve(tx, budget.ReserveRequest{User: "u", TaskID: "t", AdmissionID: "a-" + op, OperationID: op,
			Billing: billing, SendQuota: sends, QueryQuota: queries})
		return err
	})
}

// 规则：准入-8、G10
func TestReserveNeedsKnownCeilingAndCapacityAtBothLevels(t *testing.T) {
	d := setup(t, 1000, 300)
	if err := reserve(d, "op0", &lernav1.Billing{Billed: true}, 1, 0); !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED) {
		t.Fatalf("unknown ceiling must be refused: %v", err)
	}
	priced := &lernav1.Billing{Billed: true, CeilingKnown: true, Ceiling: 100, Unit: budget.Unit}
	if err := reserve(d, "op1", priced, 2, 1); err != nil {
		t.Fatalf("300 fits the task budget: %v", err)
	}
	if err := reserve(d, "op2", priced, 1, 0); !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED) {
		t.Fatalf("the task budget is exhausted: %v", err)
	}
}

// 每个新的发送身份占用一次发送额度；额度用完就不能再开始。
//
// 规则：开始-5、G10
func TestOccupySendRespectsQuota(t *testing.T) {
	d := setup(t, 1000, 1000)
	priced := &lernav1.Billing{Billed: true, CeilingKnown: true, Ceiling: 10, Unit: budget.Unit}
	if err := reserve(d, "op", priced, 1, 1); err != nil {
		t.Fatal(err)
	}
	occupy := func(p lernav1.SendPurpose) error {
		return d.Write(context.Background(), "o", func(tx *durable.Tx) error { return budget.OccupySend(tx, "u", "op", p) })
	}
	if err := occupy(lernav1.SendPurpose_SEND_PURPOSE_EXECUTE); err != nil {
		t.Fatal(err)
	}
	if err := occupy(lernav1.SendPurpose_SEND_PURPOSE_EXECUTE); !errs.Is(err, lernav1.ErrorCode_ERROR_CODE_BUDGET_EXCEEDED) {
		t.Fatalf("second send must exceed the quota: %v", err)
	}
	if err := occupy(lernav1.SendPurpose_SEND_PURPOSE_QUERY); err != nil {
		t.Fatalf("query quota is separate: %v", err)
	}
}
