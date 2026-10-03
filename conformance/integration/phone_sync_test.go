package integration_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

func TestPublicPhoneExecutionNeverDeniesAppliedRenameAfterSyncFailure(t *testing.T) {
	f := newExecutionFixture(t)
	root := t.TempDir()
	id := api.NewID("resource")
	phones, err := target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if phones != nil {
			if err := phones.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	bind := func() {
		service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{phones}, ResourceDriver: phones})
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
	holder, instance := api.NewID("holder"), api.NewID("instance")
	leaseUntil := api.Time(time.Now().Add(time.Minute))
	r := f.command(t, "resource.acquire", id, domain.AcquireInput{ResourceID: id, HolderID: holder, InstanceID: instance, LeaseUntil: leaseUntil}, nil)
	if r.Stage != "applied" {
		t.Fatalf("real resource acquisition %+v", r)
	}
	f.drain(t)
	observationID := api.NewID("observation")
	r = f.command(t, "resource.observe", id, domain.ObserveInput{ResourceID: id, ObservationID: observationID, HolderID: holder, InstanceID: instance, ControlEpoch: 1}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("original observation %+v", r)
	}
	f.drain(t)
	var observed domain.ObserveOutput
	f.query(t, "resource.observation.get", observationID, domain.ObservationIDInput{ObservationID: observationID}, &observed)
	if observed.Observation == nil {
		t.Fatal("actual phone observation missing")
	}
	invoke := f.invokeInput(t, false)
	var intent domain.ExecutionIntent
	body, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
	if err != nil || api.Decode(body, &intent) != nil {
		t.Fatal(err)
	}
	args := target.PhoneActionArguments{ResourceID: id, InstanceID: instance, ControlEpoch: 1, ObservationID: observationID, TargetVersion: observed.Observation.TargetVersion, ActionBefore: observed.Observation.ActionBefore, Action: "set_note", Value: "actual renamed phone note"}
	intent.ArgumentsRef = f.put(t, api.Raw(args))
	invoke.CapabilityRef = target.PhoneCapability().Ref
	intent.CapabilityRef = invoke.CapabilityRef
	intent.ResourceRefs = []api.ObjectRef{observed.Observation.ResourceRef}
	intent.ProcessedSourceRefs = []api.ContentRef{}
	invoke.IntentRef = f.put(t, api.Raw(intent))
	invoke.IntentHash, err = api.Digest(intent)
	if err != nil {
		t.Fatal(err)
	}
	armed := true
	phones.Fault = func(resource, point string) error {
		if armed && resource == id && point == "after_rename_before_directory_sync" {
			armed = false
			return syscall.EIO
		}
		return nil
	}
	r = f.command(t, "execution.invoke", invoke.OperationID, invoke, nil)
	if r.Stage != "applied" {
		t.Fatalf("original invoke %+v", r)
	}
	f.drain(t)
	var original domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &original)
	if armed || original.Operation.Effect != "unknown" || len(original.Attempts.Items) != 1 {
		t.Fatalf("sync failure did not retain accurate unknown original attempt %+v", original)
	}
	var lease domain.ResourceLease
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &lease)
	if lease.InflightWrite != original.Attempts.Items[0].AttemptID {
		t.Fatal("unknown effect prematurely released the resource writer")
	}
	disk, err := os.ReadFile(filepath.Join(root, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var physical struct {
		State    target.PhoneState `json:"state"`
		Attempts []struct {
			AttemptID string `json:"attempt_id"`
		} `json:"attempts"`
	}
	if err = json.Unmarshal(disk, &physical); err != nil || physical.State.Note != args.Value || len(physical.Attempts) != 1 || physical.Attempts[0].AttemptID != original.Attempts.Items[0].AttemptID {
		t.Fatalf("independent native bytes do not bind original applied effect: %v", err)
	}
	r = f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	f.drain(t)
	var reconciled domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &reconciled)
	if reconciled.Operation.Effect != "applied" || !reconciled.Operation.UsageFinal || reconciled.Operation.Attempts.TotalCount != 1 {
		t.Fatalf("public reconciliation falsely closed renamed effect %+v", reconciled)
	}
	lease = domain.ResourceLease{}
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &lease)
	if lease.InflightWrite != "" {
		t.Fatal("closed original effect did not release writer")
	}
	if err = phones.Close(); err != nil {
		t.Fatal(err)
	}
	phones = nil
	phones, err = target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	bind()
	r = f.command(t, "execution.reconcile", invoke.OperationID, domain.ReconcileInput{OperationID: invoke.OperationID}, nil)
	if r.Stage != "applied" {
		t.Fatal(r)
	}
	f.drain(t)
	var restarted domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &restarted)
	if restarted.Operation.Effect != "applied" || restarted.Operation.Attempts.TotalCount != 1 || restarted.Attempts.Items[0].AttemptID != physical.Attempts[0].AttemptID {
		t.Fatalf("restart replaced original physical action %+v", restarted)
	}
}
