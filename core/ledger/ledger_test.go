package ledger_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	lernav1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/contracts/ports"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/infra/sqlite"
)

type clock struct{ t time.Time }

func (c *clock) Now() time.Time { return c.t }

func setup(t *testing.T) *durable.Domain {
	t.Helper()
	db, err := sqlite.Open(filepath.Join(t.TempDir(), "ledger.db"), "ledger", sqlite.LocalProfile)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	d := durable.NewDomain("ledger", db, &clock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)})
	(&ledger.Module{Domain: d, Endpoint: "local"}).Register()
	return d
}

func deliver(t *testing.T, d *durable.Domain, handoff string, it *lernav1.OperationIntent) *lernav1.Receipt {
	t.Helper()
	env, err := durable.NewEnvelope(&lernav1.CommandIdentity{UserId: "u", IssuerId: "adjudication", TargetDomainId: "ledger", CommandId: handoff},
		ports.CommandAcceptIntent, it)
	if err != nil {
		t.Fatal(err)
	}
	r, err := d.Execute(context.Background(), env)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func intent(endpoint string) *lernav1.OperationIntent {
	return &lernav1.OperationIntent{UserId: "u", OperationId: "op1", TaskId: "t", AdmissionRef: "a1", LedgerDomainId: "ledger",
		ExecutorEndpointId: endpoint, Capability: &lernav1.CapabilityDeclaration{CapabilityId: "api.put"}, SendQuota: 1}
}

// 重复投递按原交接标识返回原决定；指定给其他执行端点的意图被拒绝，不改派。
//
// 规则：R7、G11
func TestAcceptIntentIsIdempotentAndKeepsTheEndpoint(t *testing.T) {
	d := setup(t)
	r1 := deliver(t, d, "intent:op1", intent("local"))
	r2 := deliver(t, d, "intent:op1", intent("local"))
	if r1.GetDecision() != lernav1.Decision_DECISION_ACCEPTED || r1.GetCommitPosition() != r2.GetCommitPosition() {
		t.Fatalf("redelivery must return the original decision: %v / %v", r1, r2)
	}
	other := deliver(t, d, "intent:op1-elsewhere", intent("phone"))
	if other.GetDecision() != lernav1.Decision_DECISION_REJECTED {
		t.Fatalf("an intent for another endpoint must be rejected, got %v", other)
	}
	var n int
	_ = d.Read(context.Background(), func(tx *durable.Tx) error { return tx.QueryRow(`SELECT COUNT(*) FROM operations`).Scan(&n) })
	if n != 1 {
		t.Fatalf("operations = %d, want 1", n)
	}
}
