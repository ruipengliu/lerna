package governance_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/ruipengliu/lerna/adapters/platform"
	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

// 故障只替换 Tx 外原用量证明边界；命令、预留和所有账务事实仍由实际数据库与 Dispatcher 维护。
type fallibleUsageVerifier struct {
	err    error
	source api.ObjectRef
	usage  api.UsageSnapshot
}

func (v *fallibleUsageVerifier) Verify(_ context.Context, _ runtime.Scope, source api.ObjectRef, usage api.UsageSnapshot) error {
	if v.err != nil {
		return v.err
	}
	if !api.Equal(source, v.source) || !api.Equal(usage, v.usage) {
		return api.E("forbidden", "actual_usage_mismatch")
	}
	return nil
}

func TestOriginalSettlementAndLeaseReportSurviveVerifierDependencyFaults(t *testing.T) {
	dbFault := errors.New("original verifier persistence boundary failed")
	for _, fault := range []error{api.E("dependency_unavailable", "original_source_temporarily_unavailable"), api.E("effect_unknown", "original_source_unresolved"), context.DeadlineExceeded, dbFault} {
		for _, kind := range []string{"settle", "lease_report"} {
			t.Run(kind+"/"+fault.Error(), func(t *testing.T) {
				v := &fallibleUsageVerifier{err: fault}
				f := environment(t, governance.Options{UsageVerifier: v})
				original, beforeGrant, readState := admitUsageCommand(t, f, v, kind)
				jobs, status, err := f.store.Claim(f.ctx, f.scope, api.NewID("worker"), []string{"governance." + kind}, 1, time.Minute)
				if err != nil || status != runtime.Committed || len(jobs) != 1 {
					t.Fatalf("actual original job %s %v", status, err)
				}
				handler, ok := f.registry.Job("governance." + kind)
				if !ok {
					t.Fatal("registered usage worker missing")
				}
				if err = handler(f.ctx, f.store, f.scope, jobs[0]); !errors.Is(err, fault) {
					t.Fatalf("verification dependency lost original cause: %v; want %v", err, fault)
				}
				accepted, err := f.dispatcher.Lookup(f.ctx, f.auth, original.CommandID)
				if err != nil || accepted.Stage != "accepted" {
					t.Fatalf("temporary source failure decided original command %+v %v", accepted, err)
				}
				if actual := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: beforeGrant.Grant.GrantID}); !api.Equal(actual, beforeGrant) {
					t.Fatal("transient verifier changed payment, reservation or once authority")
				}
				v.err = nil
				if err = handler(f.ctx, f.store, f.scope, jobs[0]); err != nil {
					t.Fatal(err)
				}
				applied, err := f.dispatcher.Lookup(f.ctx, f.auth, original.CommandID)
				if err != nil || applied.Stage != "applied" {
					t.Fatalf("original usage did not recover %+v %v", applied, err)
				}
				settled := readState()
				replayed, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(original))
				if err != nil || !api.Equal(replayed, applied) || !api.Equal(readState(), settled) {
					t.Fatal("original usage recovery paid or settled twice")
				}
			})
		}
	}
}

func TestForgedSettlementAndLeaseReportRemainPermanentlyRejected(t *testing.T) {
	for _, kind := range []string{"settle", "lease_report"} {
		t.Run(kind, func(t *testing.T) {
			v := &fallibleUsageVerifier{err: api.E("forbidden", "original_usage_forged")}
			f := environment(t, governance.Options{UsageVerifier: v})
			original, beforeGrant, _ := admitUsageCommand(t, f, v, kind)
			drain(t, f, "governance."+kind)
			decision, err := f.dispatcher.Lookup(f.ctx, f.auth, original.CommandID)
			if err != nil || decision.Stage != "rejected" || !api.IsCode(decision.Error, "forbidden") {
				t.Fatalf("forged proof not closed %+v %v", decision, err)
			}
			v.err = nil
			replayed, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(original))
			if err != nil || !api.Equal(replayed, decision) {
				t.Fatal("forgery became a fresh usage responsibility")
			}
			if actual := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: beforeGrant.Grant.GrantID}); !api.Equal(actual, beforeGrant) {
				t.Fatal("forged settlement changed actual ledger")
			}
		})
	}
}

func admitUsageCommand(t *testing.T, f *fixture, v *fallibleUsageVerifier, kind string) (api.Command, governance.GrantRecord, func() any) {
	t.Helper()
	grant := issue(t, f, "once")
	use := useRequest(f, grant)
	var commandID api.Command
	var receipt api.Receipt
	var readState func() any
	if kind == "settle" {
		_, used := command(t, f, "grant.use", use.UseID, use, nil)
		if used.Stage != "applied" {
			t.Fatalf("actual use %+v", used)
		}
		v.source = use.TargetRef
		v.usage = api.UsageSnapshot{SourceRef: use.TargetRef, UsageRevision: 1, UsageDigest: api.Hash([]byte("fixed exact usage")), Cumulative: []api.Amount{{Unit: "USD", Value: "1"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{ref(t, f, "usage")}}
		f.auth.Roles = append(f.auth.Roles, "usage_reporter")
		commandID, receipt = command(t, f, "grant.use.settle", use.UseID, governance.SettleRequest{UseID: use.UseID, Usage: v.usage}, nil)
		readState = func() any {
			return query[governance.UseSettlement](t, f, "grant.settlement.read", governance.IDInput{ID: use.UseID})
		}
	} else {
		keys, err := platform.NewDevelopmentKey(f.scope.TenantID, f.scope.OwnerID, []string{"grant_use", "grant_lease"})
		if err != nil {
			t.Fatal(err)
		}
		f.svc.Ports.Proof = localProof{ring: keys}
		endpoint, instance, id := api.NewID("endpoint"), api.NewID("instance"), api.NewID("lease")
		use.TargetRef.OwnerID = endpoint
		_, allocated := command(t, f, "grant.lease.allocate", id, governance.LeaseAllocate{LeaseID: id, EndpointID: endpoint, InstanceID: instance, Scope: use, Limits: []api.Amount{{Unit: "USD", Value: "2"}}, ExpiresAt: use.StartBefore, CostMode: "strict"}, nil)
		if allocated.Stage != "applied" {
			t.Fatalf("actual allocation %+v", allocated)
		}
		v.source = api.ObjectRef{TenantID: f.scope.TenantID, OwnerID: endpoint, ObjectID: id, Revision: 1}
		v.usage = api.UsageSnapshot{SourceRef: v.source, UsageRevision: 1, UsageDigest: api.Hash([]byte("fixed exact device usage")), Cumulative: []api.Amount{{Unit: "USD", Value: "1"}}, SpendingClosed: true, UsageFinal: true, ProofRefs: []api.ContentRef{ref(t, f, "usage")}}
		f.auth.Roles = append(f.auth.Roles, "usage_reporter")
		commandID, receipt = command(t, f, "grant.lease.report", id, governance.LeaseReport{LeaseRef: f.scope.Ref(id, 1), EndpointID: endpoint, InstanceID: instance, Usage: v.usage, ClosureRef: api.ObjectRef{TenantID: f.scope.TenantID, OwnerID: endpoint, ObjectID: api.NewID("closure"), Revision: 1}}, nil)
		readState = func() any { return query[governance.GrantLease](t, f, "grant.lease.read", governance.IDInput{ID: id}) }
	}
	if receipt.Stage != "accepted" {
		t.Fatalf("original usage admission %+v", receipt)
	}
	return commandID, query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: grant.GrantID}), readState
}
