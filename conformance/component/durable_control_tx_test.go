//go:build integration

package component_test

import (
	"context"
	"errors"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

var controlRollback = errors.New("injected rollback after actual Stop save")

// Both faults surround actual owner writes and COMMIT/ROLLBACK. No business
// state is fabricated, and no private table supplies the assertions.
type controlTransactionBoundary struct {
	decision.Store
	rollback          bool
	loseReply         bool
	committedBoundary bool
}

func (s *controlTransactionBoundary) SaveStop(ctx context.Context, tx runtime.Tx, stop decision.DecisionStop) error {
	if err := s.Store.SaveStop(ctx, tx, stop); err != nil {
		return err
	}
	if s.rollback {
		s.rollback = false
		return controlRollback
	}
	if s.loseReply {
		s.loseReply = false
		s.committedBoundary = true
	}
	return nil
}
func (s *controlTransactionBoundary) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	err := s.Store.Within(ctx, owner, fn)
	if s.committedBoundary {
		s.committedBoundary = false
		if err == nil {
			return runtime.ErrCommitUnknown
		}
	}
	return err
}

func TestDurableControlStopReceiptAndJobShareTransactionRecovery(t *testing.T) {
	for _, phase := range []string{"rollback", "commit_reply_lost"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			store := &controlTransactionBoundary{Store: w.Store()}
			s, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: store, Authority: w.Source(), ControlAuthority: w.Source(), Source: w.Source(), Publisher: w.Source(), Component: scene.Request.Payload.ComponentRef, Worker: "control-transaction", Lease: 5 * time.Second, PoolControl: true})
			if err != nil {
				t.Fatal(err)
			}
			accepted := acceptAccounting(t, ctx, s, scene)
			stop := sceneControl(t, ctx, w, scene, "2", "transaction-stop")
			store.rollback = phase == "rollback"
			store.loseReply = phase == "commit_reply_lost"
			out, err := s.Cancel(ctx, encode11(t, stop), &scene.Subject)
			var stale *runtime.Claim
			if phase == "rollback" {
				if !errors.Is(err, controlRollback) {
					t.Fatal("actual Stop transaction did not roll back with original cause", err)
				}
				view := controlView(t, ctx, s, scene)
				if _, ok := view.Decision.AsAccepted(); !ok || view.CurrentControl != nil {
					t.Fatal("rolled-back Stop changed admitted facts")
				}
				claimed, err := s.Claim(ctx)
				if err != nil || claimed == nil {
					t.Fatal("rollback split Job from original responsibility", err)
				}
				stale = &claimed.Claim
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := out.AsCommitUnknown(); !ok {
					t.Fatal("actual Stop commit reply loss was not unknown")
				}
				view := controlView(t, ctx, s, scene)
				if _, ok := view.Decision.AsCancelled(); !ok || view.CurrentControl == nil {
					t.Fatal("committed Stop and terminal were split")
				}
				pool, err := s.ObservePool(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if pool.Active["ordinary"] != 0 || pool.Queued["ordinary"] != 0 {
					t.Fatal("committed Stop did not close the original Job")
				}
			}
			applied := controlReceipt(t, ctx, s, stop, scene.Subject)
			if _, ok := applied.AsApplied(); !ok {
				t.Fatal("original cancel key did not resolve to applied")
			}
			if stale != nil {
				if err := s.RunClaim(ctx, *stale); !errors.Is(err, runtime.ErrClaim) {
					t.Fatal("retried Stop did not fence the actual prior claim", err)
				}
			}
			w.Reopen(ctx)
			s = w.Service()
			view := controlView(t, ctx, s, scene)
			closed, ok := view.Decision.AsCancelled()
			if !ok || view.CurrentControl == nil || closed.Usage.RuleStarts != "0" || closed.Usage.Cost.IntegerValue != "0" {
				t.Fatal("transaction recovery revived work or charged a rule")
			}
			replayed := controlReceipt(t, ctx, s, stop, scene.Subject)
			want, err := v.Encode(applied)
			if err != nil {
				t.Fatal(err)
			}
			got, err := v.Encode(replayed)
			if err != nil || string(want) != string(got) {
				t.Fatal("reopen changed resolved original cancel receipt", err)
			}
			query, err := v.DecodeCommand(scene.CommandGetJSON)
			if err != nil {
				t.Fatal(err)
			}
			query.Target.ID = stop.CommandID
			query.Payload.CommandRef.CommandID = stop.CommandID
			command, err := s.GetCommand(ctx, encode11(t, query), &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			fixed, ok := command.AsFound()
			if !ok {
				t.Fatal("resolved cancel receipt not publicly queryable")
			}
			got, err = v.Encode(fixed.Receipt)
			if err != nil || string(want) != string(got) {
				t.Fatal("command query changed resolved cancellation", err)
			}
			original, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			before, ok := original.AsFound()
			if !ok {
				t.Fatal("original acceptance unavailable")
			}
			want, err = v.Encode(accepted)
			if err != nil {
				t.Fatal(err)
			}
			got, err = v.Encode(before.Receipt)
			if err != nil || string(want) != string(got) {
				t.Fatal("Stop transaction changed original acceptance", err)
			}
			if step, err := s.Step(ctx); err != nil || step.Processed != 0 {
				t.Fatal("transaction recovery left runnable work", err)
			}
		})
	}
}
