package governance_test

import (
	"testing"
	"time"

	"github.com/ruipengliu/lerna/api"
	"github.com/ruipengliu/lerna/internal/governance"
	"github.com/ruipengliu/lerna/runtime"
)

func TestOfflineLeaseRejectsOnlineDelegationBeforeOriginalOnceReservation(t *testing.T) {
	f := environment(t, governance.Options{})
	grant := issue(t, f, "once")
	use := useRequest(f, grant)
	use.TargetKind, use.TargetRef = "delegation", f.scope.Ref(api.NewID("delegation"), 1)
	leaseID := api.NewID("lease")
	in := governance.LeaseAllocate{LeaseID: leaseID, EndpointID: api.NewID("executor"), InstanceID: api.NewID("instance"), Scope: use, Limits: use.RequestedUnits, ExpiresAt: use.StartBefore, CostMode: "strict"}
	status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
		_, err := f.svc.AllocateLeaseTx(f.ctx, tx, f.auth, in)
		return err
	})
	if status != runtime.RolledBack || !api.IsCode(err, "invalid_request") {
		t.Fatalf("offline host accepted an online delegation: %s %v", status, err)
	}
	_, receipt := command(t, f, "grant.lease.allocate", leaseID, in, nil)
	if receipt.Stage != "rejected" {
		t.Fatalf("public offline lease accepted online delegation: %+v", receipt)
	}
	q := api.Query{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.scope.OwnerID, QueryID: api.NewID("query"), Method: "grant.lease.read", TargetID: leaseID, Payload: api.Raw(governance.IDInput{ID: leaseID})}
	if _, err = f.dispatcher.Query(f.ctx, f.auth, api.Raw(q)); !api.IsCode(err, "not_found") {
		t.Fatalf("refused offline lease left responsibility: %v", err)
	}
	current := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: grant.GrantID})
	if current.OnceConsumed || len(current.Reserved) != 0 {
		t.Fatalf("offline refusal spent online authority: %+v", current)
	}
	use.StartBefore = api.Time(time.Now().Add(time.Minute))
	_, allowed := command(t, f, "grant.use", use.UseID, use, nil)
	var original governance.UseReceipt
	if err = api.Decode(allowed.Output, &original); err != nil || allowed.Stage != "applied" || original.Decision != "allowed" {
		t.Fatalf("refused offline lease consumed original online once: %+v %v", allowed, err)
	}
}

// 这里只验证在线Grant原Use责任；真实远端账单验签由collaboration专项证明。
func TestOnlineDelegationUseKeepsOriginalOnceBudgetAndLateSettlement(t *testing.T) {
	f := environment(t, governance.Options{})
	grant := issue(t, f, "once")
	input := useRequest(f, grant)
	input.TargetKind, input.TargetRef = "delegation", f.scope.Ref(api.NewID("delegation"), 1)
	checked := query[governance.UseReceipt](t, f, "grant.check", input)
	if checked.Decision != "allowed" || checked.TargetKind != "delegation" {
		t.Fatalf("online original delegation check: %+v", checked)
	}
	before := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: grant.GrantID})
	if before.OnceConsumed || len(before.Reserved) != 0 {
		t.Fatalf("checking spent original once or budget: %+v", before)
	}
	originalCommand, receipt := command(t, f, "grant.use", input.UseID, input, nil)
	var use governance.UseReceipt
	if err := api.Decode(receipt.Output, &use); err != nil || receipt.Stage != "applied" || use.Decision != "allowed" || !api.Equal(use.Reserved, input.RequestedUnits) || use.TargetRef != input.TargetRef {
		t.Fatalf("original online use: %+v %+v %v", receipt, use, err)
	}
	read := query[governance.UseReceipt](t, f, "grant.use.get", governance.IDInput{ID: input.UseID})
	if !api.Equal(read, use) {
		t.Fatalf("original use query changed: %+v", read)
	}
	again, err := f.dispatcher.Command(f.ctx, f.auth, api.Raw(originalCommand))
	if err != nil || !api.Equal(again, receipt) {
		t.Fatalf("original command replay changed reservation: %+v %v", again, err)
	}
	for _, method := range []string{"grant.use", "grant.check", "grant.use.get"} {
		m, ok := f.registry.Method(method)
		if !ok {
			t.Fatalf("missing %s", method)
		}
		v, err := api.NewValidator(m.Contract.OutputSchema)
		if err != nil || v.Validate(api.Raw(use)) != nil {
			t.Fatalf("closed %s output rejected original delegation: %v", method, err)
		}
		bad := use
		bad.TargetKind = "undeclared_target"
		if v.Validate(api.Raw(bad)) == nil {
			t.Fatalf("%s allowed an undeclared target kind", method)
		}
	}
	foreign := input
	foreign.UseID, foreign.TargetRef = api.NewID("use"), f.scope.Ref(api.NewID("delegation"), 1)
	_, denied := command(t, f, "grant.use", foreign.UseID, foreign, nil)
	var refused governance.UseReceipt
	if err := api.Decode(denied.Output, &refused); err != nil || refused.Decision != "denied" || refused.Reason != "once_consumed" {
		t.Fatalf("new child resurrected original once: %+v %v", refused, err)
	}
	settle := func(revision uint64, closed bool, fee string) governance.UseSettlement {
		t.Helper()
		// 受信夹具只给Gov原费用事实；不把它声称为已经验签的远端Agent账单。
		usage := api.UsageSnapshot{SourceRef: input.TargetRef, UsageRevision: revision, UsageDigest: api.Hash([]byte(fee)), Cumulative: []api.Amount{{Unit: "USD", Value: fee}}, SpendingClosed: closed, UsageFinal: closed, ProofRefs: []api.ContentRef{}}
		var out governance.UseSettlement
		status, err := f.store.Within(f.ctx, f.scope, []string{governance.Namespace}, func(tx runtime.Tx) error {
			var err error
			out, err = f.svc.ApplySettlementTx(f.ctx, tx, governance.SettleRequest{UseID: input.UseID, Usage: usage})
			return err
		})
		if status != runtime.Committed || err != nil {
			t.Fatalf("original late usage %d: %s %v", revision, status, err)
		}
		return out
	}
	unknown := settle(1, false, "0.2")
	if unknown.SpendingClosed || unknown.UsageFinal || !api.Equal(unknown.RemainingReserved, []api.Amount{{Unit: "USD", Value: "1.8"}}) {
		t.Fatalf("unknown usage released original residual: %+v", unknown)
	}
	known := settle(2, true, "0.3")
	if !known.SpendingClosed || !known.UsageFinal || len(known.RemainingReserved) != 0 {
		t.Fatalf("known final usage failed to settle original reservation: %+v", known)
	}
	final := query[governance.GrantRecord](t, f, "grant.read", governance.IDInput{ID: grant.GrantID})
	if !final.OnceConsumed || !api.Equal(final.Reserved, []api.Amount{{Unit: "USD", Value: "0"}}) || !api.Equal(final.Spent, []api.Amount{{Unit: "USD", Value: "0.3"}}) {
		t.Fatalf("late original settlement changed once or ledger: %+v", final)
	}
}
