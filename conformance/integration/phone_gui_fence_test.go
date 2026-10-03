package integration_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

// 只延迟实际目标 Fence 已返回的事实交接，不预置资源停止或效果。
type delayedGUIFence struct {
	phones  *target.SimulatedPhones
	entered chan struct{}
	release chan struct{}
}

func (d delayedGUIFence) Fence(ctx context.Context, scope rt.Scope, lease domain.ResourceLease) (domain.StopFact, error) {
	fact, err := d.phones.Fence(ctx, scope, lease)
	if err != nil {
		return fact, err
	}
	close(d.entered)
	select {
	case <-d.release:
		return fact, nil
	case <-ctx.Done():
		return domain.StopFact{}, ctx.Err()
	}
}
func (d delayedGUIFence) Observe(ctx context.Context, scope rt.Scope, lease domain.ResourceLease, id string) (domain.Observation, error) {
	return d.phones.Observe(ctx, scope, lease, id)
}
func (d delayedGUIFence) ObservationSchema() api.Schema { return d.phones.ObservationSchema() }

func TestPublicGUIReconcileDuringActualTakeoverFencePreservesCurrentEpochFact(t *testing.T) {
	f := newPhoneGUIFixture(t)
	id := f.ids[0]
	lease := f.acquire(t, id)
	before := f.observe(t, lease)
	invoke := f.invocation(t, before, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}})
	f.phones.Fault = func(resource, point string) error {
		if resource == id && point == "applied" {
			return errors.New("fixture lost reply before concurrent owner fence")
		}
		return nil
	}
	unknown := f.act(t, invoke)
	if unknown.Operation.Effect != "unknown" || len(unknown.Attempts.Items) != 1 {
		t.Fatal("unknown original GUI attempt absent")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	entered, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{f.phones, &target.PhoneGUIDriver{Phones: f.phones}}, ResourceDriver: delayedGUIFence{phones: f.phones, entered: entered, release: release}})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = rt.NewRegistry()
	if err = service.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Registry = f.registry
	var current domain.ResourceLease
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &current)
	revision := current.Revision
	if r := f.command(t, "resource.takeover", id, domain.TakeoverInput{ResourceID: id, Reason: "user takeover while original effect is unknown"}, &revision); r.Stage != "applied" {
		t.Fatal(r)
	}
	works, status, err := f.st.Claim(ctx, f.sc, api.NewID("worker"), []string{domain.ResourceFenceJob}, 1, 30*time.Second)
	if err != nil || status != rt.Committed || len(works) != 1 {
		t.Fatalf("actual resource fence was not claimed: %s %v", status, err)
	}
	fenceHandler, ok := f.registry.Job(domain.ResourceFenceJob)
	if !ok {
		t.Fatal("original registered Fence handler missing")
	}
	done := make(chan error, 1)
	go func() { done <- fenceHandler(ctx, f.st, f.sc, works[0]) }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	physical := f.physical(t, id)
	if physical.ControlEpoch != 2 || physical.Automatic || len(physical.Attempts) != 1 {
		t.Fatal("physical Fence did not establish the current epoch independently")
	}
	if r := f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil); r.Stage != "applied" {
		t.Fatal(r)
	}
	f.drain(t)
	current = domain.ResourceLease{}
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &current)
	if current.InflightWrite != "" || current.ActuallyStopped || current.State != "unknown" {
		t.Fatalf("old epoch reconciliation claimed current Fence before its fact commit: %+v", current)
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	current = domain.ResourceLease{}
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &current)
	var recovered domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &recovered)
	if current.InflightWrite != "" || !current.ActuallyStopped || current.State != "released" || current.ControlEpoch != 2 || recovered.Operation.Effect != "applied" || !recovered.NewAttemptsClosed || len(recovered.Attempts.Items) != 1 || recovered.Attempts.Items[0].AttemptID != unknown.Attempts.Items[0].AttemptID {
		t.Fatalf("current epoch real Fence fact disappeared during old-effect reconciliation: lease=%+v operation=%+v", current, recovered)
	}
	f.evidence(t, "concurrent_current_epoch_actual_fence_and_original_effect_reconciliation", "click", invoke, recovered, &before, nil, physical)
}
