//go:build integration

package component_test

import (
	"context"
	"errors"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
)

// Each test supplies a fixed deadline at most three seconds away. The
// runtime polling timer deliberately permits only one-second polling delays;
// waiting for this separate observation boundary uses its own finite timer.
func controlWaitUntil(ctx context.Context, until time.Time) error {
	delay := time.Until(until) + time.Millisecond
	if delay <= 0 {
		return ctx.Err()
	}
	if delay > 5*time.Second {
		return errors.New("control observation exceeds finite test window")
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// The real immutable Snapshot read finishes before this port delays its reply.
// This models an in-flight finite I/O window, not a production provider call.
type delayedControlSnapshot struct{ decision.Source }

func (s delayedControlSnapshot) ReadSnapshot(ctx context.Context, ref v.SnapshotRef, permit decision.Permission, cap int64) (decision.Snapshot, error) {
	snapshot, err := s.Source.ReadSnapshot(ctx, ref, permit, cap)
	if err != nil {
		return snapshot, err
	}
	<-ctx.Done()
	return snapshot, ctx.Err()
}

func TestDurableControlSeparatesAcceptBeforeExecutionAndContextDeadlines(t *testing.T) {
	for _, name := range []string{"accept_before", "execution_deadline", "resource_context"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			w := fixture.NewWorld(t, ctx)
			scene := w.Scenario()
			scene.Request.Payload.Limits.MaxRuleSteps = "2"
			scene.Request.Payload.Limits.MaxCost.IntegerValue = "2"
			deadline := time.Now().UTC().Add(time.Second).Truncate(time.Microsecond)
			if name == "accept_before" {
				scene.Request.AcceptBefore = v.Time(deadline.Format("2006-01-02T15:04:05.000000Z"))
			}
			if name == "execution_deadline" {
				scene.Request.Payload.Deadline = v.Time(deadline.Format("2006-01-02T15:04:05.000000Z"))
			}
			var source decision.Source = w.Source()
			if name != "accept_before" {
				source = delayedControlSnapshot{Source: w.Source()}
			}
			s, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: w.Store(), Authority: w.Source(), ControlAuthority: w.Source(), Source: source, Publisher: w.Source(), Component: scene.Request.Payload.ComponentRef, Worker: "deadline-original", Lease: 2 * time.Second, PoolControl: true})
			if err != nil {
				t.Fatal(err)
			}
			receipt := acceptAccounting(t, ctx, s, scene)
			if name == "accept_before" {
				if err = controlWaitUntil(ctx, deadline); err != nil {
					t.Fatal(err)
				}
				// First-admission cutoff does not become an execution deadline.
				if step, err := s.Step(ctx); err != nil || step.Processed != 1 {
					t.Fatal("accepted work was stopped by admission cutoff", err)
				}
				view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
				if err != nil {
					t.Fatal(err)
				}
				found, ok := view.AsFound()
				if !ok {
					t.Fatal("accepted result unavailable")
				}
				if _, ok = found.Decision.AsCompleted(); !ok {
					t.Fatal("before-deadline accepted work did not finish after accept_before")
				}
				fresh := scene.Request
				fresh.CommandID = "after-accept-cutoff"
				out, err := s.Decide(ctx, encode11(t, fresh), &scene.Subject)
				if err != nil {
					t.Fatal(err)
				}
				received, ok := out.AsReceived()
				if !ok {
					t.Fatal("late admission receipt unavailable")
				}
				rejected, ok := received.Receipt.AsRejected()
				if !ok || rejected.Reason != "expired" {
					t.Fatal("fresh key bypassed original admission cutoff")
				}
			} else {
				work, err := s.Claim(ctx)
				if err != nil || work == nil {
					t.Fatal("current work claim", err)
				}
				invocation := ctx
				var stop context.CancelFunc = func() {}
				if name == "resource_context" {
					invocation, stop = context.WithTimeout(ctx, 150*time.Millisecond)
				}
				err = s.RunClaim(invocation, work.Claim)
				stop()
				if name == "execution_deadline" && !errors.Is(err, runtime.ErrClaim) {
					t.Fatal("execution deadline failed to fence delayed worker", err)
				}
				if name == "resource_context" && !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal("single invocation was not bounded by its resource context", err)
				}
				w.Reopen(ctx)
				s = w.ServiceFor(v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, "deadline-replacement", 3*time.Second)
				if name == "execution_deadline" {
					if _, err = s.Maintain(ctx); err != nil {
						t.Fatal(err)
					}
					view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
					if err != nil {
						t.Fatal(err)
					}
					found, ok := view.AsFound()
					if !ok {
						t.Fatal("deadline result unavailable")
					}
					failed, ok := found.Decision.AsFailed()
					if !ok || failed.Failure != "deadline_elapsed" || failed.Usage.RuleStarts != "1" || failed.Usage.Cost.IntegerValue != "1" || failed.Usage.RuleSteps != "0" || failed.Usage.MeasurementsComplete {
						t.Fatal("execution deadline lost unknown original invocation or revived work")
					}
					if next, err := s.Claim(ctx); err != nil || next != nil {
						t.Fatal("deadline responsibility acquired a replacement claim", err)
					}
				} else {
					if err = controlWaitUntil(ctx, work.Claim.LeaseUntil); err != nil {
						t.Fatal(err)
					}
					if step, err := s.Step(ctx); err != nil || step.Processed != 1 {
						t.Fatal("single context timeout permanently closed original responsibility", err)
					}
					view, err := s.Get(ctx, scene.GetJSON, &scene.Subject)
					if err != nil {
						t.Fatal(err)
					}
					found, ok := view.AsFound()
					if !ok {
						t.Fatal("replacement result unavailable")
					}
					completed, ok := found.Decision.AsCompleted()
					if !ok || completed.Usage.RuleStarts != "2" || completed.Usage.Cost.IntegerValue != "2" || completed.Usage.RuleSteps != "1" || completed.Usage.MeasurementsComplete {
						t.Fatal("replacement reset budget or claimed complete prior measurement")
					}
				}
			}
			w.Reopen(ctx)
			s = w.Service()
			query, err := s.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
			if err != nil {
				t.Fatal(err)
			}
			command, ok := query.AsFound()
			if !ok {
				t.Fatal("original receipt unavailable after temporal recovery")
			}
			want, err := v.Encode(receipt)
			if err != nil {
				t.Fatal(err)
			}
			got, err := v.Encode(command.Receipt)
			if err != nil || string(want) != string(got) {
				t.Fatal("temporal boundary rewrote accepted receipt", err)
			}
			if step, err := s.Step(ctx); err != nil || step.Processed != 0 {
				t.Fatal("reopened terminal temporal result revived work", err)
			}
		})
	}
}
