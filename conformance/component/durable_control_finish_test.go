//go:build integration

package component_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	decision "github.com/ruipengliu/lerna/components/decision_engine"
	fixture "github.com/ruipengliu/lerna/conformance/internal/decisionfixture"
	"github.com/ruipengliu/lerna/contract"
	v "github.com/ruipengliu/lerna/contract/v1_1"
	"github.com/ruipengliu/lerna/runtime"
	"github.com/ruipengliu/lerna/runtime/workpool"
)

type finishControlMarkerKey struct{}
type finishControlMarker struct{ completed bool }

// This gate runs after the real callback has saved the completed Decision and
// completed its Job, but before the same native Core transaction commits.
// It changes neither the transaction token nor any business write.
type finishControlStore struct {
	decision.Store
	reached, resume chan struct{}
}

func (s *finishControlStore) Complete(ctx context.Context, tx runtime.Tx, claim runtime.Claim, now time.Time) error {
	if err := s.Store.Complete(ctx, tx, claim, now); err != nil {
		return err
	}
	if marker, ok := ctx.Value(finishControlMarkerKey{}).(*finishControlMarker); ok {
		marker.completed = true
	}
	return nil
}
func (s *finishControlStore) Within(ctx context.Context, owner contract.OwnerRef, fn func(context.Context, runtime.Tx) error) error {
	marker := &finishControlMarker{}
	return s.Store.Within(ctx, owner, func(txctx context.Context, tx runtime.Tx) error {
		txctx = context.WithValue(txctx, finishControlMarkerKey{}, marker)
		if err := fn(txctx, tx); err != nil {
			return err
		}
		if !marker.completed {
			return nil
		}
		close(s.reached)
		select {
		case <-s.resume:
			return nil
		case <-txctx.Done():
			return txctx.Err()
		}
	})
}

// Cancel first enters its actual owner transaction and competes for the pool
// lock already held by Finish. Only after native LockPool succeeds does this
// second hold allow a public read of committed completion before Stop writes.
type cancelControlPoolStore struct {
	decision.Store
	attempted, acquired, resume chan struct{}
	attemptedOnce, acquiredOnce sync.Once
}

func (s *cancelControlPoolStore) LockPool(ctx context.Context, tx runtime.Tx) (workpool.State, error) {
	s.attemptedOnce.Do(func() { close(s.attempted) })
	state, err := s.Store.LockPool(ctx, tx)
	if err != nil {
		return state, err
	}
	s.acquiredOnce.Do(func() { close(s.acquired) })
	select {
	case <-s.resume:
		return state, nil
	case <-ctx.Done():
		return state, ctx.Err()
	}
}

func TestDurableControlFinishCommitsBeforeConcurrentCancelPreservingOriginalFacts(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	w := fixture.NewWorld(t, ctx)
	scene := w.Scenario()
	reader := w.Service()
	original := acceptAccounting(t, ctx, reader, scene)
	finishBeforeConcurrentCancel(t, ctx, w, scene, reader, original)
}

func finishBeforeConcurrentCancel(t *testing.T, ctx context.Context, w *fixture.World, scene fixture.Scenario, reader *decision.Service, original v.CommandReceipt) {
	t.Helper()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finishStore := &finishControlStore{Store: w.Store(), reached: make(chan struct{}), resume: make(chan struct{})}
	cancelStore := &cancelControlPoolStore{Store: w.Store(), attempted: make(chan struct{}), acquired: make(chan struct{}), resume: make(chan struct{})}
	service := func(store decision.Store, worker string) *decision.Service {
		result, err := decision.New(decision.Config{Owner: v.OwnerRef{TenantID: scene.DecisionRef.TenantID, OwnerID: scene.DecisionRef.OwnerID}, Store: store, Authority: w.Source(), ControlAuthority: w.Source(), Source: w.Source(), Publisher: w.Source(), Component: scene.Request.Payload.ComponentRef, Worker: worker, Lease: 5 * time.Second, PoolControl: true})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	finisher, stopper := service(finishStore, "finish-before-control"), service(cancelStore, "concurrent-control")
	stop := sceneControl(t, ctx, w, scene, "2", "concurrent-finish-wins-control")
	raw := encode11(t, stop)
	work, err := finisher.Claim(ctx)
	if err != nil || work == nil {
		t.Fatal("actual Finish claim unavailable", err)
	}
	finished, stopped := make(chan struct{}), make(chan struct{})
	var finishErr, stopErr error
	var stopOutcome v.TransportOutcome
	var releaseFinishOnce, releaseCancelOnce sync.Once
	releaseFinish := func() { releaseFinishOnce.Do(func() { close(finishStore.resume) }) }
	releaseCancel := func() { releaseCancelOnce.Do(func() { close(cancelStore.resume) }) }
	finishStarted, cancelStarted := false, false
	// Before either actual goroutine starts, register its finite cleanup and both
	// concrete owner obligations. Never drop a scope after an unconfirmed join.
	t.Cleanup(func() {
		releaseFinish()
		releaseCancel()
		cancel()
		if !finishStarted {
			close(finished)
		}
		if !cancelStarted {
			close(stopped)
		}
		joinctx, done := context.WithTimeout(context.Background(), 3*time.Second)
		defer done()
		for _, exited := range []<-chan struct{}{finished, stopped} {
			select {
			case <-exited:
			case <-joinctx.Done():
				t.Error("concurrent control holder exit unconfirmed; retain exact scopes")
				return
			}
		}
	})
	if err := w.BorrowWorkerExit(finished); err != nil {
		t.Fatal(err)
	}
	if err := w.BorrowWorkerExit(stopped); err != nil {
		t.Fatal(err)
	}
	finishStarted = true
	go func() { defer close(finished); finishErr = finisher.RunClaim(ctx, work.Claim) }()
	select {
	case <-finishStore.reached:
	case <-finished:
		t.Fatal("Finish ended before actual completion transaction gate", finishErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	cancelStarted = true
	go func() { defer close(stopped); stopOutcome, stopErr = stopper.Cancel(ctx, raw, &scene.Subject) }()
	select {
	case <-cancelStore.attempted:
	case <-stopped:
		t.Fatal("Cancel did not enter the competing actual owner transaction", stopErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// A bounded mechanical observation proves Cancel cannot acquire Finish's
	// native pool lock while that completion transaction is still held.
	observation := time.NewTimer(25 * time.Millisecond)
	select {
	case <-cancelStore.acquired:
		observation.Stop()
		t.Fatal("Cancel acquired the held native Finish lock")
	case <-stopped:
		observation.Stop()
		t.Fatal("Cancel exited instead of competing", stopErr)
	case <-observation.C:
	case <-ctx.Done():
		observation.Stop()
		t.Fatal(ctx.Err())
	}
	releaseFinish()
	select {
	case <-finished:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if finishErr != nil {
		t.Fatal("actual Finish commit failed", finishErr)
	}
	select {
	case <-cancelStore.acquired:
	case <-stopped:
		t.Fatal("Cancel never acquired the lock after Finish committed", stopErr)
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	before := controlView(t, ctx, reader, scene)
	completed, ok := before.Decision.AsCompleted()
	if !ok || before.CurrentControl != nil {
		t.Fatal("Finish did not commit before the competing Stop")
	}
	frozen, err := v.Encode(before.Decision)
	if err != nil {
		t.Fatal(err)
	}
	permission, err := w.Source().Authorize(ctx, scene.Subject, scene.DecisionRef, "get", nil)
	if err != nil {
		t.Fatal(err)
	}
	proposal, err := w.Source().ReadPublished(ctx, completed.ProposalRef, permission)
	expectedProposal, encodeErr := v.Encode(completed.Proposal)
	if err != nil || encodeErr != nil || string(proposal) != string(expectedProposal) {
		t.Fatal("original completed Proposal bytes unavailable", errors.Join(err, encodeErr))
	}
	artifacts := make([][]byte, len(completed.ArtifactRefs))
	for index, ref := range completed.ArtifactRefs {
		artifacts[index], err = w.Source().ReadPublished(ctx, ref, permission)
		if err != nil || string(artifacts[index]) != "fixture result: alpha\n" {
			t.Fatal("original completed artifact unavailable", err)
		}
	}
	if len(artifacts) != 1 {
		t.Fatal("original normal completion lost its artifact")
	}
	releaseCancel()
	select {
	case <-stopped:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if stopErr != nil {
		t.Fatal("concurrent public Cancel failed", stopErr)
	}
	received, ok := stopOutcome.AsReceived()
	if !ok {
		t.Fatal("concurrent Cancel receipt unavailable")
	}
	applied, ok := received.Receipt.AsApplied()
	if !ok || applied.Revision != completed.Revision {
		t.Fatal("Stop changed completed object revision")
	}
	after := controlView(t, ctx, reader, scene)
	unchanged, err := v.Encode(after.Decision)
	if err != nil || string(unchanged) != string(frozen) || after.CurrentControl == nil {
		t.Fatal("concurrent Stop changed original completed facts", err)
	}
	command, err := reader.GetCommand(ctx, scene.CommandGetJSON, &scene.Subject)
	if err != nil {
		t.Fatal(err)
	}
	fixed, ok := command.AsFound()
	if !ok {
		t.Fatal("concurrent Stop hid original accepted receipt")
	}
	expected, err := v.Encode(original)
	if err != nil {
		t.Fatal(err)
	}
	actual, err := v.Encode(fixed.Receipt)
	if err != nil || string(expected) != string(actual) {
		t.Fatal("concurrent Stop rewrote original accepted receipt", err)
	}
	w.Reopen(ctx)
	after = controlView(t, ctx, w.Service(), scene)
	unchanged, err = v.Encode(after.Decision)
	if err != nil || string(unchanged) != string(frozen) || after.CurrentControl == nil {
		t.Fatal("reopen changed concurrent completion/control facts", err)
	}
	for index, ref := range completed.ArtifactRefs {
		restored, err := w.ReadArtifact(ctx, ref)
		if err != nil || string(restored) != string(artifacts[index]) {
			t.Fatal("reopen lost independently published original artifact", err)
		}
	}
	restored, err := w.ReadArtifact(ctx, completed.ProposalRef)
	if err != nil || string(restored) != string(proposal) {
		t.Fatal("reopen lost original completed Proposal", err)
	}
}
