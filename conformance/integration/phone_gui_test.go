package integration_test

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	target "github.com/ruipengliu/lerna/adapters/execution"
	"github.com/ruipengliu/lerna/api"
	domain "github.com/ruipengliu/lerna/internal/execution"
	rt "github.com/ruipengliu/lerna/runtime"
)

// 公开 Dispatcher + 实际数据库 + 独立手机文件目标。远端 Content/授权按已授权
// fixture seam 替代；授权拒绝发生在原真实 StartBarrier，不预置 Operation/目标结果。
type phoneGUIFixture struct {
	*executionFixture
	phones *target.SimulatedPhones
	root   string
	ids    []string
}

type nativePhoneTruth struct {
	ControlEpoch uint64 `json:"control_epoch"`
	Automatic    bool   `json:"automatic"`
	State        struct {
		Screen         string `json:"screen"`
		Note           string `json:"note"`
		WiFi           bool   `json:"wifi"`
		Version        uint64 `json:"version"`
		FocusedControl string `json:"focused_control"`
		ScrollOffset   uint64 `json:"scroll_offset"`
	} `json:"state"`
	Attempts []struct {
		AttemptID   string `json:"attempt_id"`
		OperationID string `json:"operation_id"`
		InputDigest string `json:"input_digest"`
	} `json:"attempts"`
}

func newPhoneGUIFixture(t *testing.T) *phoneGUIFixture {
	t.Helper()
	f := &phoneGUIFixture{executionFixture: newExecutionFixture(t), root: t.TempDir(), ids: []string{api.NewID("resource"), api.NewID("resource"), api.NewID("resource")}}
	var err error
	f.phones, err = target.NewSimulatedPhones(f.root, f.ids)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if f.phones != nil {
			if err := f.phones.Close(); err != nil {
				t.Error(err)
			}
		}
	})
	f.bind(t)
	return f
}

func (f *phoneGUIFixture) bind(t *testing.T) {
	t.Helper()
	service, err := domain.New(domain.Config{OwnerID: f.sc.OwnerID, Content: f.content, Authority: f.authority, Location: "device", Drivers: []domain.Driver{f.phones, &target.PhoneGUIDriver{Phones: f.phones}}, ResourceDriver: f.phones})
	if err != nil {
		t.Fatal(err)
	}
	f.registry = rt.NewRegistry()
	if err = service.Register(f.registry); err != nil {
		t.Fatal(err)
	}
	f.dispatcher.Registry = f.registry
}

func (f *phoneGUIFixture) acquire(t *testing.T, id string) domain.ResourceLease {
	t.Helper()
	r := f.command(t, "resource.acquire", id, domain.AcquireInput{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), LeaseUntil: api.Time(time.Now().Add(time.Minute))}, nil)
	if r.Stage != "applied" {
		t.Fatalf("resource acquisition %+v", r)
	}
	f.drain(t)
	var lease domain.ResourceLease
	f.query(t, "resource.get", id, domain.ResourceInput{ResourceID: id}, &lease)
	if !lease.ActuallyStopped || lease.State != "held" {
		t.Fatalf("resource not ready %+v", lease)
	}
	return lease
}

func (f *phoneGUIFixture) observe(t *testing.T, lease domain.ResourceLease) domain.Observation {
	t.Helper()
	id := api.NewID("observation")
	r := f.command(t, "resource.observe", lease.ResourceID, domain.ObserveInput{ResourceID: lease.ResourceID, ObservationID: id, HolderID: lease.HolderID, InstanceID: lease.InstanceID, ControlEpoch: lease.ControlEpoch}, nil)
	if r.Stage != "accepted" {
		t.Fatalf("observe %+v", r)
	}
	f.drain(t)
	var result domain.ObserveOutput
	f.query(t, "resource.observation.get", id, domain.ObservationIDInput{ObservationID: id}, &result)
	if !result.Ready || result.Observation == nil {
		t.Fatalf("missing accurate observation %+v", result)
	}
	return *result.Observation
}

func (f *phoneGUIFixture) invocation(t *testing.T, observed domain.Observation, args target.PhoneGUIArguments) domain.InvokeInput {
	t.Helper()
	args.ResourceID = observed.ResourceRef.ObjectID
	args.InstanceID = observed.InstanceID
	args.ControlEpoch = observed.ControlEpoch
	args.ObservationID = observed.ObservationID
	if args.TargetVersion == "" {
		args.TargetVersion = observed.TargetVersion
	}
	args.ActionBefore = observed.ActionBefore
	invoke := f.invokeInput(t, false)
	var intent domain.ExecutionIntent
	bytes, err := f.content.ReadBytes(context.Background(), f.sc, f.auth, invoke.IntentRef, "prepare", "device")
	if err != nil || api.Decode(bytes, &intent) != nil {
		t.Fatal(err)
	}
	invoke.CapabilityRef = target.PhoneGUICapability().Ref
	intent.CapabilityRef = invoke.CapabilityRef
	intent.ArgumentsRef = f.put(t, api.Raw(args))
	intent.ResourceRefs = []api.ObjectRef{observed.ResourceRef}
	intent.ProcessedSourceRefs = []api.ContentRef{}
	intent.CostBound = []api.Amount{{Unit: "USD", Value: "0"}}
	invoke.IntentRef = f.put(t, api.Raw(intent))
	invoke.IntentHash, err = api.Digest(intent)
	if err != nil {
		t.Fatal(err)
	}
	return invoke
}

func (f *phoneGUIFixture) act(t *testing.T, invoke domain.InvokeInput) domain.OperationView {
	t.Helper()
	if r := f.command(t, "execution.invoke", invoke.OperationID, invoke, nil); r.Stage != "applied" {
		t.Fatalf("GUI invoke rejected %+v", r)
	}
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	return view
}

func (f *phoneGUIFixture) physical(t *testing.T, id string) nativePhoneTruth {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(f.root, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var truth nativePhoneTruth
	if err = json.Unmarshal(body, &truth); err != nil {
		t.Fatal(err)
	}
	return truth
}

func TestPublicGUIAllGesturesThreeDevicesObserveActionObserveVerify(t *testing.T) {
	f := newPhoneGUIFixture(t)
	for index, id := range f.ids {
		lease := f.acquire(t, id)
		text := fmt.Sprintf("设备 %d 的独立准确笔记\nsecond line", index)
		steps := []struct {
			args                  target.PhoneGUIArguments
			screen, note, focused string
			scroll                uint64
		}{
			{args: target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}}, screen: "notes"},
			{args: target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 240}}, screen: "notes", focused: "note_editor"},
			{args: target.PhoneGUIArguments{Action: "input", Text: &text}, screen: "notes", note: text, focused: "note_editor"},
			{args: target.PhoneGUIArguments{Action: "swipe", From: &target.PhonePoint{X: 180, Y: 400}, To: &target.PhonePoint{X: 180, Y: 140}}, screen: "notes", note: text, focused: "note_editor", scroll: 260},
			{args: target.PhoneGUIArguments{Action: "back"}, screen: "notes", note: text, scroll: 260},
			{args: target.PhoneGUIArguments{Action: "back"}, screen: "home", note: text},
		}
		for position, step := range steps {
			before := f.observe(t, lease)
			var tree target.PhoneState
			if err := api.Decode(before.Data, &tree); err != nil || tree.UI == nil || tree.UI.Width != 360 || tree.UI.Height != 640 || len(tree.UI.Controls) == 0 {
				t.Fatalf("device %d position %d usable tree absent %v", index, position, err)
			}
			invoke := f.invocation(t, before, step.args)
			view := f.act(t, invoke)
			if view.Operation.Effect != "applied" || view.Operation.ExecutionState != "closed" || view.Operation.Attempts.TotalCount != 1 || len(view.Attempts.Items) != 1 || !view.Operation.UsageFinal {
				t.Fatalf("device %d gesture %s public facts %+v", index, step.args.Action, view)
			}
			after := f.observe(t, lease)
			var observed target.PhoneState
			if err := api.Decode(after.Data, &observed); err != nil || observed.Screen != step.screen || observed.Note != step.note || observed.FocusedControl != step.focused || observed.ScrollOffset != step.scroll || observed.Version != uint64(position+2) {
				t.Fatalf("device %d gesture %s current UI %+v %v", index, step.args.Action, observed, err)
			}
			truth := f.physical(t, id)
			if truth.State.Screen != step.screen || truth.State.Note != step.note || truth.State.FocusedControl != step.focused || truth.State.ScrollOffset != step.scroll || truth.State.Version != uint64(position+2) || len(truth.Attempts) != position+1 || truth.Attempts[position].AttemptID != view.Attempts.Items[0].AttemptID || truth.Attempts[position].OperationID != invoke.OperationID {
				t.Fatalf("device %d gesture %s independent target truth %+v", index, step.args.Action, truth)
			}
		}
	}
}

func TestPublicGUIRejectsForgedOldObservationBeforePhysicalEntry(t *testing.T) {
	f := newPhoneGUIFixture(t)
	id := f.ids[0]
	lease := f.acquire(t, id)
	old := f.observe(t, lease)
	if err := f.phones.HumanChange(context.Background(), id, "home"); err != nil {
		t.Fatal(err)
	}
	// 原 ID/窗口保持旧观察，但伪造当前版本；不能只在目标处比较当前版本而绕过原观察。
	invoke := f.invocation(t, old, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}, TargetVersion: "2"})
	view := f.act(t, invoke)
	truth := f.physical(t, id)
	if view.Operation.Effect != "not_started" || !view.NewAttemptsClosed || truth.State.Screen != "home" || truth.State.Version != 2 || len(truth.Attempts) != 0 {
		t.Fatalf("forged old observation reached real exit: %+v physical=%+v", view, truth)
	}
}

func TestPublicGUIPermissionLimitsRemainAtOriginalResourceAndStartBarrier(t *testing.T) {
	f := newPhoneGUIFixture(t)
	id := f.ids[0]
	unauthorized := f.auth
	unauthorized.Roles = []string{"viewer"}
	command := api.Command{Protocol: api.Protocol, Profile: api.Profile, LogicalServiceID: f.sc.OwnerID, CommandID: api.NewID("command"), Method: "resource.acquire", TargetID: id, ExpiresAt: api.Time(time.Now().Add(time.Minute)), Payload: api.Raw(domain.AcquireInput{ResourceID: id, HolderID: api.NewID("holder"), InstanceID: api.NewID("instance"), LeaseUntil: api.Time(time.Now().Add(time.Minute))})}
	denied, err := f.dispatcher.Command(context.Background(), unauthorized, api.Raw(command))
	if err != nil || denied.Stage != "rejected" || denied.Error == nil || denied.Error.Code != "forbidden" {
		t.Fatalf("unprivileged resource acquired: %+v %v", denied, err)
	}
	lease := f.acquire(t, id)
	before := f.observe(t, lease)
	invoke := f.invocation(t, before, target.PhoneGUIArguments{Action: "click", Point: &target.PhonePoint{X: 180, Y: 160}})
	if r := f.command(t, "execution.invoke", invoke.OperationID, invoke, nil); r.Stage != "applied" {
		t.Fatal(r)
	}
	f.authority.denied = true
	f.drain(t)
	var view domain.OperationView
	f.query(t, "execution.get", invoke.OperationID, domain.OperationIDInput{OperationID: invoke.OperationID}, &view)
	truth := f.physical(t, id)
	if view.Operation.Effect != "not_started" || !view.Operation.UsageFinal || !view.NewAttemptsClosed || truth.State.Screen != "home" || truth.State.Version != 1 || len(truth.Attempts) != 0 {
		t.Fatalf("revoked original use altered target: %+v physical=%+v", view, truth)
	}
}
