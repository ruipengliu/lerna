package durableworkdemo_test

import (
	"errors"
	"github.com/ruipengliu/lerna/contract"
	demo "github.com/ruipengliu/lerna/internal/durableworkdemo"
	"testing"
	"time"
)

func TestSchedulingConfigurationRejectsUnboundedOrUnknownPolicies(t *testing.T) {
	for _, modify := range []func(*demo.Policy){func(p *demo.Policy) { p.Identity = "" }, func(p *demo.Policy) { p.Lane = "arbitrary" }, func(p *demo.Policy) { p.ExecutionLimit = 0 }, func(p *demo.Policy) { p.ExecutionLimit = 25 * time.Hour }, func(p *demo.Policy) { p.MaxAttempts = 0 }, func(p *demo.Policy) { p.MaxAttempts = 65 }, func(p *demo.Policy) { p.BaseBackoff = 0 }, func(p *demo.Policy) { p.MaxBackoff = p.BaseBackoff - 1 }, func(p *demo.Policy) { p.MaxBackoff = 2 * time.Hour }, func(p *demo.Policy) { p.Recheck = 0 }, func(p *demo.Policy) { p.Recheck = 2 * time.Hour }, func(p *demo.Policy) { p.TransientFailures = -1 }, func(p *demo.Policy) { p.TransientFailures = 65 }, func(p *demo.Policy) { p.PermanentReason = "timeout_guess" }, func(p *demo.Policy) { p.Gate = &demo.GateCondition{ID: "gate", Revision: 0} }} {
		policy := demo.DefaultPolicy()
		modify(&policy)
		if _, err := demo.NewPolicies([]demo.PolicyBinding{{Owner: contract.OwnerRef{TenantID: "tenant", OwnerID: "owner"}, ObjectID: "input", Policy: policy}}); !errors.Is(err, demo.ErrPolicy) {
			t.Fatalf("unbounded policy accepted: %+v %v", policy, err)
		}
	}
}
func TestTrustedPolicySelectionCopiesExactBinding(t *testing.T) {
	owner := contract.OwnerRef{TenantID: "tenant", OwnerID: "owner"}
	policy := demo.DefaultPolicy()
	policy.Gate = &demo.GateCondition{ID: "gate", Revision: 2}
	bindings := []demo.PolicyBinding{{Owner: owner, ObjectID: "input", Policy: policy}}
	policies, err := demo.NewPolicies(bindings)
	if err != nil {
		t.Fatal(err)
	}
	bindings[0].Policy.Gate.Revision = 99
	got := policies.For(owner, "input")
	if got.Gate.Revision != 2 {
		t.Fatal("caller mutated trusted binding")
	}
	got.Gate.Revision = 77
	if policies.For(owner, "input").Gate.Revision != 2 {
		t.Fatal("selection leaked policy mutability")
	}
	other := owner
	other.OwnerID = "other"
	if policies.For(other, "input").Gate != nil || policies.For(owner, "other").Gate != nil {
		t.Fatal("policy escaped exact scope")
	}
	if _, err = demo.NewPolicies(append(bindings, bindings[0])); !errors.Is(err, demo.ErrPolicy) {
		t.Fatal("duplicate trusted binding accepted")
	}
}
