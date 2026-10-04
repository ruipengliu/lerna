//go:build integration

package component_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

func TestDurableControlCancelsWithoutOrdinaryCapacityAndFencesOldClaim(t *testing.T) {
	for _, name := range []string{"quota_zero", "saturated", "started"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			s := w.Service()
			owner := contract.OwnerRef{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}
			cfg := workpool.Default("fixture-decision-pool", []contract.OwnerRef{owner})
			for i := range cfg.Limits {
				if cfg.Limits[i].Lane == "ordinary" {
					cfg.Limits[i].Concurrent = 1
				}
			}
			for i := range cfg.Quotas {
				if cfg.Quotas[i].Lane == "ordinary" {
					cfg.Quotas[i].Concurrent = 1
					if name == "quota_zero" {
						cfg.Quotas[i].Concurrent = 0
					}
				}
			}
			if err := s.InstallPool(ctx, cfg, 1); err != nil {
				t.Fatal(err)
			}
			receipt := acceptAccounting(t, ctx, s, scene)
			var work *runtime.Claim
			if name != "quota_zero" {
				claimed, err := s.Claim(ctx)
				if err != nil || claimed == nil {
					t.Fatal("normal permitted claim", err)
				}
				work = &claimed.Claim
				if name == "started" {
					if _, err = s.Start(ctx, *work); err != nil {
						t.Fatal(err)
					}
				}
			}
			if name == "saturated" {
				second := w.AdditionalScenario(ctx, scene.DecisionRef.OwnerID, "full-pool-queued", time.Now().Add(15*time.Second))
				acceptAccounting(t, ctx, s, second)
				if claim, err := s.Claim(ctx); err != nil || claim != nil {
					t.Fatal("full pool unexpectedly had capacity", err)
				}
				stop := sceneControl(t, ctx, w, second, "2", "cancel-full-pool-queued")
				if _, ok := controlReceipt(t, ctx, s, stop, second.Subject).AsApplied(); !ok {
					t.Fatal("full ordinary pool blocked direct cancel")
				}
				view := controlView(t, ctx, s, second)
				if _, ok := view.Decision.AsCancelled(); !ok {
					t.Fatal("queued full-pool responsibility was not closed")
				}
			}
			stop := sceneControl(t, ctx, w, scene, "2", "cancel-capacity-"+v.ID(name))
			if _, ok := controlReceipt(t, ctx, s, stop, scene.Subject).AsApplied(); !ok {
				t.Fatal("ordinary execution capacity blocked cancel")
			}
			if work != nil {
				if err := s.RunClaim(ctx, *work); !errors.Is(err, runtime.ErrClaim) {
					t.Fatal("old claim crossed durable stop", err)
				}
			}
			view := controlView(t, ctx, s, scene)
			closed, ok := view.Decision.AsCancelled()
			if !ok || closed.Input == nil {
				t.Fatal("cancel lost original admitted input")
			}
			wantStarts := v.Revision("0")
			if name == "started" {
				wantStarts = "1"
			}
			if closed.Usage.RuleStarts != wantStarts || closed.Usage.Cost.IntegerValue != wantStarts {
				t.Fatal("cancel charged another rule or erased original start")
			}
			fixed, err := v.Encode(view.Decision)
			if err != nil {
				t.Fatal(err)
			}
			w.Reopen(ctx)
			s = w.Service()
			if claim, err := s.Claim(ctx); err != nil || claim != nil {
				t.Fatal("reopen revived cancelled claim", err)
			}
			pool, err := s.ObservePool(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if pool.Active["ordinary"] != 0 || pool.Queued["ordinary"] != 0 {
				t.Fatal("cancel retained ordinary responsibility")
			}
			after := controlView(t, ctx, s, scene)
			bytes, err := v.Encode(after.Decision)
			if err != nil || string(bytes) != string(fixed) {
				t.Fatal("capacity recovery rewrote closed facts", err)
			}
			query, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			command, ok := query.AsFound()
			if !ok {
				t.Fatal("original accepted command unavailable")
			}
			want, err := v.Encode(receipt)
			if err != nil {
				t.Fatal(err)
			}
			got, err := v.Encode(command.Receipt)
			if err != nil || string(want) != string(got) {
				t.Fatal("cancel changed original acceptance", err)
			}
			for i := range cfg.Quotas {
				if cfg.Quotas[i].Lane == "ordinary" {
					cfg.Quotas[i].Concurrent = 1
				}
			}
			if err = s.InstallPool(ctx, cfg, 2); err != nil {
				t.Fatal(err)
			}
			normal := w.AdditionalScenario(ctx, scene.DecisionRef.OwnerID, "capacity-counterpart", time.Now().Add(15*time.Second))
			acceptAccounting(t, ctx, s, normal)
			if step, err := s.Step(ctx); err != nil || step.Processed != 1 {
				t.Fatal("released capacity did not serve normal counterpart", err)
			}
			if _, ok := controlView(t, ctx, s, normal).Decision.AsCompleted(); !ok {
				t.Fatal("normal counterpart incomplete")
			}
		})
	}
}

func TestDurableControlDeadlineMaintenanceClosesBeyondOnePageAtZeroQuota(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	s := w.Service()
	member := v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: "non-anchor-expiry"}
	cfg := workpool.Default("fixture-decision-pool", []contract.OwnerRef{{TenantID: contract.ID(scene.DecisionRef.TenantID), OwnerID: contract.ID(scene.DecisionRef.OwnerID)}, {TenantID: contract.ID(member.TenantID), OwnerID: contract.ID(member.OwnerID)}})
	for i := range cfg.Limits {
		if cfg.Limits[i].Lane == "ordinary" {
			cfg.Limits[i].Queue = 65 // finite capacity for exactly one page plus one.
		}
	}
	for i := range cfg.Quotas {
		if cfg.Quotas[i].Lane == "ordinary" {
			cfg.Quotas[i].Concurrent = 0
		}
	}
	if err := s.InstallPool(ctx, cfg, 1); err != nil {
		t.Fatal(err)
	}
	other := w.ServiceFor(member, "non-anchor-maintenance", time.Second)
	deadline := time.Now().UTC().Add(3 * time.Second).Truncate(time.Microsecond)
	scenes := make([]fixture.Scenario, 0, 65)
	for i := range 65 {
		item := w.AdditionalScenario(ctx, member.OwnerID, v.ID(fmt.Sprintf("paged-expiry-%02d", i)), deadline)
		acceptAccounting(t, ctx, other, item)
		scenes = append(scenes, item)
	}
	if err := controlWaitUntil(ctx, deadline); err != nil {
		t.Fatal(err)
	}
	// The anchor visits every registered owner, independent of ordinary quota.
	// Two bounded pages suffice; no unbounded drain loop hides remaining work.
	for range 2 {
		if _, err := s.Maintain(ctx); err != nil {
			t.Fatal(err)
		}
	}
	for _, item := range scenes {
		view := controlView(t, ctx, other, item)
		failed, ok := view.Decision.AsFailed()
		if !ok || failed.Failure != "deadline_elapsed" || failed.Usage.RuleStarts != "0" || failed.Usage.Cost.IntegerValue != "0" {
			t.Fatal("non-anchor responsibility survived bounded expiry pages")
		}
	}
	w.Reopen(ctx)
	s = w.Service()
	other = w.ServiceFor(member, "non-anchor-reopened", time.Second)
	for _, item := range scenes {
		if _, ok := controlView(t, ctx, other, item).Decision.AsFailed(); !ok {
			t.Fatal("reopen revived expired paged responsibility")
		}
	}
	step, err := s.Step(ctx)
	if err != nil || step.Processed != 0 || step.WaitFor <= 0 {
		t.Fatal("zero-capacity expiry left runnable work or a busy wake", err)
	}
	pool, err := s.ObservePool(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if pool.Active["ordinary"] != 0 || pool.Queued["ordinary"] != 0 {
		t.Fatal("expired page retained ordinary pool responsibility")
	}
}
