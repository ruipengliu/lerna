package integration_test

import (
	"bytes"
	"context"
	"encoding/json"
	"sync/atomic"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

// 只观察受信 Driver 边界；Prepare、Start、Reconcile 都委托真实 GUI 驱动。
type originalGUIEncoding struct {
	domain.Driver
	encoded  json.RawMessage
	prepared atomic.Uint64
}

func (d *originalGUIEncoding) Prepare(ctx context.Context, sc rt.Scope, auth rt.Auth, in domain.InvokeInput, intent domain.ExecutionIntent, body []byte) (domain.PreparedRequest, error) {
	out, err := d.Driver.Prepare(ctx, sc, auth, in, intent, body)
	if err == nil {
		d.encoded = append(json.RawMessage{}, out.Encoded...)
		d.prepared.Add(1)
	}
	return out, err
}
func (d *originalGUIEncoding) Start(ctx context.Context, req domain.AttemptRequest, barrier func(context.Context) error) (domain.Fact, error) {
	if !bytes.Equal(req.Attempt.Prepared.Encoded, d.encoded) || req.Attempt.Prepared.Digest != api.Hash(d.encoded) {
		return domain.Fact{}, api.E("invalid_request", "original_GUI_bytes_changed")
	}
	return d.Driver.Start(ctx, req, barrier)
}
func (d *originalGUIEncoding) Reconcile(ctx context.Context, req domain.AttemptRequest) (domain.Fact, error) {
	if !bytes.Equal(req.Attempt.Prepared.Encoded, d.encoded) || req.Attempt.Prepared.Digest != api.Hash(d.encoded) {
		return domain.Fact{}, api.E("effect_unknown", "original_GUI_recovery_bytes_changed")
	}
	return d.Driver.Reconcile(ctx, req)
}

func TestDurablePreparedGUIKeepsOriginalBytesAcrossControlPreparationAndReopen(t *testing.T) {
	f := newPhoneGUIFixture(t)
	lease := f.acquire(t, f.ids[0])
	observed := f.observe(t, lease)
	driver := &originalGUIEncoding{Driver: &target.PhoneGUIDriver{Phones: f.phones}}
	authority := &finiteControlPreparation{executionAuthority: f.authority, f: f.executionFixture}
	authority.current = func() error { return api.E("dependency_unavailable", "original_control_not_ready") }
	bind := func() {
		t.Helper()
		service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: authority, Location: "device", Drivers: []domain.Driver{driver}, ResourceDriver: f.phones})
		if err != nil {
			t.Fatal(err)
		}
		f.registry = rt.NewRegistry()
		if err = service.Register(f.registry); err != nil {
			t.Fatal(err)
		}
		f.dispatcher.Registry = f.registry
	}
	bind()
	in := f.invocation(t, observed, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}})
	receipt := f.command(t, "execution.invoke", in.OperationID, in, nil)
	if receipt.Stage != "applied" {
		t.Fatalf("original invoke: %+v", receipt)
	}
	works, status, err := f.st.Claim(context.Background(), f.sc, api.NewID("worker"), []string{domain.RunJob}, 1, time.Minute)
	if err != nil || status != rt.Committed || len(works) != 1 {
		t.Fatalf("original claim: %+v %v", works, err)
	}
	handler, _ := f.registry.Job(domain.RunJob)
	if err = handler(context.Background(), f.st, f.sc, works[0]); !api.IsCode(err, "dependency_unavailable") {
		t.Fatalf("preparation fault: %v", err)
	}
	var before domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &before)
	if len(before.Attempts.Items) != 1 || before.Attempts.Items[0].Phase != "prepared" || before.Attempts.Items[0].RequestDigest != api.Hash(driver.encoded) {
		t.Fatalf("original preparation: %+v", before)
	}
	if err = rt.Finish(context.Background(), f.st, f.sc, []string{"execution"}, works[0], rt.Ready(time.Now().Add(-time.Second)), nil); err != nil {
		t.Fatal(err)
	}
	f.reopen(t)
	driver.Driver = &target.PhoneGUIDriver{Phones: f.phones}
	authority.current = nil
	bind()
	f.drain(t)
	var after domain.OperationView
	f.query(t, "execution.get", in.OperationID, domain.OperationIDInput{OperationID: in.OperationID}, &after)
	physical := f.physical(t, lease.ResourceID)
	if len(after.Attempts.Items) != 1 || after.Attempts.Items[0].AttemptID != before.Attempts.Items[0].AttemptID || after.Attempts.Items[0].RequestDigest != before.Attempts.Items[0].RequestDigest || after.Operation.Effect != "applied" || driver.prepared.Load() != 1 || physical.State.Screen != "notes" || len(physical.Attempts) != 1 || physical.Attempts[0].InputDigest != before.Attempts.Items[0].RequestDigest {
		t.Fatalf("original GUI encoding was lost or target repeated: %+v / %+v, prepare=%d", after, physical, driver.prepared.Load())
	}
	command := f.command(t, "execution.reconcile", in.OperationID, domain.ReconcileInput{OperationID: in.OperationID}, nil)
	if command.Stage != "applied" {
		t.Fatalf("original reconcile: %+v", command)
	}
	f.drain(t)
	if physical = f.physical(t, lease.ResourceID); len(physical.Attempts) != 1 {
		t.Fatalf("reconcile repeated target: %+v", physical)
	}
	original, err := f.dispatcher.Lookup(context.Background(), f.auth, receipt.CommandID)
	if err != nil || !api.Equal(original, receipt) {
		t.Fatalf("original invoke receipt changed: %+v %v", original, err)
	}
}
