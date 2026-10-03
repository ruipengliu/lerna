package execution_test

import (
	"context"
	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
	"testing"
	"time"
)

func TestThreeIndependentSimulatedPhonesObserveActObserveVerify(t *testing.T) {
	ids := []string{api.NewID("resource"), api.NewID("resource"), api.NewID("resource")}
	phones, err := target.NewSimulatedPhones(t.TempDir(), ids)
	if err != nil {
		t.Fatal(err)
	}
	defer phones.Close()
	sc := rt.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("executor")}
	for index, id := range ids {
		lease := domain.ResourceLease{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), ControlEpoch: 1, State: "held", LeaseUntil: api.Time(time.Now().Add(time.Minute))}
		if _, err = phones.Fence(context.Background(), sc, lease); err != nil {
			t.Fatal(err)
		}
		before, err := phones.Observe(context.Background(), sc, lease, api.NewID("observation"))
		if err != nil {
			t.Fatal(err)
		}
		args := target.PhoneActionArguments{ResourceID: id, InstanceID: lease.InstanceID, ControlEpoch: 1, ObservationID: before.ObservationID, TargetVersion: before.TargetVersion, ActionBefore: before.ActionBefore, Action: "set_note", Value: "phone-specific note"}
		p, err := phones.Prepare(context.Background(), sc, rt.Auth{}, domain.InvokeInput{}, domain.ExecutionIntent{}, api.Raw(args))
		if err != nil {
			t.Fatal(err)
		}
		operationID := api.NewID("operation")
		attemptID := api.NewID("attempt")
		fact, err := phones.Start(context.Background(), domain.AttemptRequest{Scope: sc, Invoke: domain.InvokeInput{OperationID: operationID}, Attempt: domain.Attempt{AttemptID: attemptID, Prepared: p}}, func(context.Context) error { return nil })
		if err != nil || fact.Effect != "applied" {
			t.Fatalf("phone %d %+v %v", index, fact, err)
		}
		after, err := phones.Observe(context.Background(), sc, lease, api.NewID("observation"))
		if err != nil {
			t.Fatal(err)
		}
		var state target.PhoneState
		if err = api.Decode(after.Data, &state); err != nil || state.Note != "phone-specific note" || state.Version != 2 {
			t.Fatalf("actual phone %d %+v %v", index, state, err)
		}
	}
}

func TestSimulatedPhoneRejectsOldObservationAndFencesOwnerTakeover(t *testing.T) {
	id := api.NewID("resource")
	root := t.TempDir()
	phones, err := target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	sc := rt.Scope{TenantID: api.NewID("tenant"), OwnerID: api.NewID("executor")}
	lease := domain.ResourceLease{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), ControlEpoch: 1, State: "held", LeaseUntil: api.Time(time.Now().Add(time.Minute))}
	phones.Fence(context.Background(), sc, lease)
	before, err := phones.Observe(context.Background(), sc, lease, api.NewID("observation"))
	if err != nil {
		t.Fatal(err)
	}
	args := target.PhoneActionArguments{ResourceID: id, InstanceID: lease.InstanceID, ControlEpoch: 1, ObservationID: before.ObservationID, TargetVersion: before.TargetVersion, ActionBefore: before.ActionBefore, Action: "set_note", Value: "stale action"}
	p, err := phones.Prepare(context.Background(), sc, rt.Auth{}, domain.InvokeInput{}, domain.ExecutionIntent{}, api.Raw(args))
	if err != nil {
		t.Fatal(err)
	}
	phones.HumanChange(context.Background(), id, "payment")
	entered := false
	_, err = phones.Start(context.Background(), domain.AttemptRequest{Scope: sc, Invoke: domain.InvokeInput{OperationID: api.NewID("operation")}, Attempt: domain.Attempt{AttemptID: api.NewID("attempt"), Prepared: p}}, func(context.Context) error { entered = true; return nil })
	if !api.IsCode(err, "revision_conflict") || entered {
		t.Fatalf("stale observation reached entry %v entered=%v", err, entered)
	}
	lease.ControlEpoch = 2
	lease.TakeoverRequested = true
	lease.State = "unknown"
	stop, err := phones.Fence(context.Background(), sc, lease)
	if err != nil || !stop.ActuallyStopped {
		t.Fatalf("takeover stop %+v %v", stop, err)
	}
	phones.Close()
	phones, err = target.NewSimulatedPhones(root, []string{id})
	if err != nil {
		t.Fatal(err)
	}
	defer phones.Close()
	_, err = phones.Start(context.Background(), domain.AttemptRequest{Scope: sc, Invoke: domain.InvokeInput{OperationID: api.NewID("operation")}, Attempt: domain.Attempt{AttemptID: api.NewID("attempt"), Prepared: p}}, func(context.Context) error { t.Fatal("old epoch reached barrier"); return nil })
	if !api.IsCode(err, "revision_conflict") {
		t.Fatalf("old epoch survived restart %v", err)
	}
}
