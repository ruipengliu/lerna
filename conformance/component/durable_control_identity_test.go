//go:build integration

package component_test

import (
	"context"
	"errors"
	"testing"
	"time"

	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
)

func TestDurableControlOriginalCommandPrecedesMethodAndClosedBindingConflicts(t *testing.T) {
	for _, first := range []string{"decide", "cancel"} {
		t.Run(first, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			s := w.Service()
			stop := sceneControl(t, ctx, w, scene, "2", scene.Request.CommandID)
			var original v.CommandReceipt
			if first == "decide" {
				original = acceptAccounting(t, ctx, s, scene)
			} else {
				original = controlReceipt(t, ctx, s, stop, scene.Subject)
			}
			var err error
			if first == "decide" {
				_, err = s.Cancel(ctx, encode11(t, stop), &scene.Subject)
			} else {
				_, err = s.Decide(ctx, encode11(t, scene.Request), &scene.Subject)
			}
			var conflict *v.ContractError
			if !errors.As(err, &conflict) || conflict.Code != "idempotency_conflict" {
				t.Fatal("same original command key changed method instead of conflicting", err)
			}
			if first == "decide" {
				stop.CommandID = "fresh-cancel-command"
				if _, ok := controlReceipt(t, ctx, s, stop, scene.Subject).AsApplied(); !ok {
					t.Fatal("fresh valid cancel failed")
				}
			}
			baseline := controlView(t, ctx, s, scene)
			bytes, err := v.Encode(baseline.Decision)
			if err != nil {
				t.Fatal(err)
			}
			for _, changed := range []bool{false, true} {
				late := scene.Request
				late.CommandID = "fresh-late-same"
				expected := v.ErrorCode("decision_cancelled")
				if changed {
					late.CommandID = "fresh-late-different"
					late.Payload.Limits.MaxRuleSteps = "1"
					expected = "decision_mismatch"
				}
				out, err := s.Decide(ctx, encode11(t, late), &scene.Subject)
				if err != nil {
					t.Fatal(err)
				}
				received, ok := out.AsReceived()
				if !ok {
					t.Fatal("late closed binding receipt unavailable")
				}
				rejected, ok := received.Receipt.AsRejected()
				if !ok || rejected.Reason != expected {
					t.Fatal("closed identity did not take precedence", expected)
				}
			}
			// A legitimately issued low-revision proof for a different identity
			// is a binding conflict before it is a revision-order conflict.
			wrong := stop
			wrong.CommandID = "signed-wrong-binding-low-control"
			wrong.Payload.TaskRef.ID = "different-original-task"
			wrong.Payload.DecisionInputDigest = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
			wrong.Payload.ControlBasis, err = w.Source().IssueControl(ctx, fixture.ControlClaim{Subject: scene.Subject, DecisionRef: scene.DecisionRef, TaskRef: wrong.Payload.TaskRef, InputDigest: wrong.Payload.DecisionInputDigest, ControlRevision: "0", ValidUntil: stop.AcceptBefore})
			if err != nil {
				t.Fatal(err)
			}
			wrongReceipt := controlReceipt(t, ctx, s, wrong, scene.Subject)
			refused, ok := wrongReceipt.AsRejected()
			if !ok || refused.Reason != "decision_mismatch" {
				t.Fatal("wrong original identity was classified by lower control first")
			}
			w.Reopen(ctx)
			s = w.Service()
			after := controlView(t, ctx, s, scene)
			current, err := v.Encode(after.Decision)
			if err != nil || string(current) != string(bytes) {
				t.Fatal("binding conflicts rewrote closed identity", err)
			}
			if step, err := s.Step(ctx); err != nil || step.Processed != 0 {
				t.Fatal("late mismatching work revived a Job", err)
			}
			pool, err := s.ObservePool(ctx)
			if err != nil {
				t.Fatal(err)
			}
			if pool.Active["ordinary"] != 0 || pool.Queued["ordinary"] != 0 {
				t.Fatal("binding conflicts allocated responsibility")
			}
			query, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			command, ok := query.AsFound()
			if !ok {
				t.Fatal("original command unavailable")
			}
			want, err := v.Encode(original)
			if err != nil {
				t.Fatal(err)
			}
			got, err := v.Encode(command.Receipt)
			if err != nil || string(want) != string(got) {
				t.Fatal("cross-method conflict changed original receipt", err)
			}
		})
	}
}
