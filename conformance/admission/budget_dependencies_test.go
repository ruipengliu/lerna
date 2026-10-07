package admission_test

import (
	"testing"

	v1 "github.com/ruipengliu/lerna/contracts/gen/go/lerna/v1"
	"github.com/ruipengliu/lerna/core/budget"
	"github.com/ruipengliu/lerna/core/content"
	"github.com/ruipengliu/lerna/core/durable"
	"github.com/ruipengliu/lerna/core/ledger"
	"github.com/ruipengliu/lerna/core/tasks"
	"github.com/ruipengliu/lerna/infra/sqlite"
	"google.golang.org/protobuf/proto"
)

// 包装真实适配器，只暴露消费方声明的能力。
type declaredBudgetStore struct{ budget.Store }
type declaredBudgetDecisions struct{ budget.Decisions }
type declaredBudgetUsageSource struct{ budget.UsageSource }

var (
	_ budget.Store                 = declaredBudgetStore{}
	_ budget.Decisions             = declaredBudgetDecisions{}
	_ budget.Store                 = (*sqlite.Store)(nil)
	_ budget.Decisions             = (*durable.Service)(nil)
	_ budget.UsageSource           = declaredBudgetUsageSource{}
	_ budget.UsageSource           = (*ledger.Service)(nil)
	_ budget.BillingEvidence       = (*content.Service)(nil)
	_ budget.CompletionAuthority   = (*tasks.Service)(nil)
	_ budget.CancellationAuthority = (*tasks.Service)(nil)
	_ budget.TaskClosingAuthority  = (*tasks.Service)(nil)
)

// 规则：R6、G3、G10
func TestDeclaredBudgetStoreAdjustsLimitAndPreservesHistoryAndReceipt(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	store, err := sqlite.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	decisions := durable.New(store, "u", "d")
	owner, err := budget.New(declaredBudgetStore{store}, declaredBudgetDecisions{decisions}, "u", "d", "host")
	if err != nil {
		t.Fatal(err)
	}
	owner.WithUsageSource(declaredBudgetUsageSource{f.h.Ledger}).WithBillingEvidence(f.h.Content).
		WithCompletionAuthority(f.h.Tasks).WithCancellationAuthority(f.h.Tasks).WithTaskClosingAuthority(f.h.Tasks)
	if err = owner.ValidateDependencies(); err != nil {
		t.Fatal(err)
	}
	before, err := owner.QueryBudget(f.ctx, f.caller, nil)
	if err != nil {
		t.Fatal(err)
	}
	cmd := &v1.AdjustBudgetLimitCommand{Header: header("declared-budget-limit"), ExpectedRef: before.Ref, Limit: 120, Reason: "user limit"}
	receipt, err := owner.AdjustLimit(f.ctx, f.caller, cmd)
	accepted(t, receipt, err)
	current, err := owner.QueryBudget(f.ctx, f.caller, nil)
	if err != nil || current.Limit != 120 || current.Reserved != 0 || current.Settled != 0 {
		t.Fatalf("current: %v %v", current, err)
	}
	original, err := owner.QueryBudgetVersion(f.ctx, f.caller, before.Ref)
	if err != nil || !proto.Equal(original, before) {
		t.Fatalf("history: %v %v", original, err)
	}
	replay, err := owner.AdjustLimit(f.ctx, f.caller, cmd)
	if err != nil || !proto.Equal(replay, receipt) {
		t.Fatalf("original receipt: %v %v", replay, err)
	}
}

// 规则：R6、G3
func TestBudgetConstructorRejectsMissingDependencies(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	store, err := sqlite.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	for _, test := range []struct {
		name      string
		store     budget.Store
		decisions budget.Decisions
		missing   string
	}{
		{name: "nil store", decisions: f.h.Durable, missing: "store"},
		{name: "typed nil store", store: (*declaredBudgetStore)(nil), decisions: f.h.Durable, missing: "store"},
		{name: "nil decisions", store: store, missing: "decisions"},
		{name: "typed nil decisions", store: store, decisions: (*declaredBudgetDecisions)(nil), missing: "decisions"},
	} {
		t.Run(test.name, func(t *testing.T) {
			owner, err := budget.New(test.store, test.decisions, "u", "d", "host")
			if owner != nil || err == nil || err.Error() != "missing required dependency: budget."+test.missing {
				t.Fatalf("construction: %v %v", owner, err)
			}
		})
	}
}

// 规则：R6、G3、G10
func TestBudgetCompleteAssemblyRejectsMissingAndTypedNilLinks(t *testing.T) {
	f := newFixture(t, 100, 80, false)
	store, err := sqlite.Open(f.path, "u", "d")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	owner, err := budget.New(declaredBudgetStore{store}, declaredBudgetDecisions{f.h.Durable}, "u", "d", "host")
	if err != nil {
		t.Fatal(err)
	}
	for _, link := range []struct {
		name            string
		connectNil      func()
		connectTypedNil func()
		connect         func()
	}{
		{"usageSource", func() { owner.WithUsageSource(nil) }, func() { owner.WithUsageSource((*declaredBudgetUsageSource)(nil)) }, func() { owner.WithUsageSource(declaredBudgetUsageSource{f.h.Ledger}) }},
		{"evidence", func() { owner.WithBillingEvidence(nil) }, func() { owner.WithBillingEvidence((*content.Service)(nil)) }, func() { owner.WithBillingEvidence(f.h.Content) }},
		{"completionAuthority", func() { owner.WithCompletionAuthority(nil) }, func() { owner.WithCompletionAuthority((*tasks.Service)(nil)) }, func() { owner.WithCompletionAuthority(f.h.Tasks) }},
		{"cancellationAuthority", func() { owner.WithCancellationAuthority(nil) }, func() { owner.WithCancellationAuthority((*tasks.Service)(nil)) }, func() { owner.WithCancellationAuthority(f.h.Tasks) }},
		{"taskClosingAuthority", func() { owner.WithTaskClosingAuthority(nil) }, func() { owner.WithTaskClosingAuthority((*tasks.Service)(nil)) }, func() { owner.WithTaskClosingAuthority(f.h.Tasks) }},
	} {
		t.Run(link.name, func(t *testing.T) {
			for _, connect := range []func(){link.connectNil, link.connectTypedNil} {
				connect()
				if err := owner.ValidateDependencies(); err == nil || err.Error() != "missing required dependency: budget."+link.name {
					t.Fatalf("completion: %v", err)
				}
			}
			link.connect()
		})
	}
	if err := owner.ValidateDependencies(); err != nil {
		t.Fatalf("complete assembly: %v", err)
	}
}
